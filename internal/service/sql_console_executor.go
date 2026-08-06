package service

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

const (
	sqlConsoleTimeout           = 30 * time.Second
	sqlConsoleMaxRows           = 500
	sqlConsoleMaxColumns        = 256
	sqlConsoleMaxCellBytes      = 64 * 1024
	sqlConsoleMaxResultBytes    = 4 * 1024 * 1024
	sqlConsoleMetadataMaxTables = 500
)

type SQLConsoleExecution struct {
	Columns           []string
	Rows              [][]any
	RowsAffected      int64
	RowsAffectedKnown bool
	Truncated         bool
}

type SQLConsoleExecutor interface {
	Connect(context.Context, model.DatabaseAccount) (SQLConsoleConnection, error)
}

type SQLConsoleConnection interface {
	Databases() []string
	DefaultDatabase() string
	Metadata(context.Context, string) (SQLConsoleMetadata, error)
	Execute(context.Context, string, string, bool) (SQLConsoleExecution, error)
	Close() error
}

type DatabaseSQLConsoleExecutor struct{}

func NewDatabaseSQLConsoleExecutor() *DatabaseSQLConsoleExecutor {
	return &DatabaseSQLConsoleExecutor{}
}

func (e *DatabaseSQLConsoleExecutor) Connect(
	ctx context.Context,
	account model.DatabaseAccount,
) (SQLConsoleConnection, error) {
	if ctx == nil {
		return nil, errors.New("connect SQL console: nil context")
	}
	connectContext, cancel := context.WithTimeout(ctx, sqlConsoleTimeout)
	defer cancel()
	defaultDatabase := defaultSQLConsoleDatabase(account.Instance.Protocol)
	db, err := openSQLConsoleDatabase(account, defaultDatabase)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(connectContext); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	databases, err := listSQLConsoleDatabases(connectContext, db, account.Instance.Protocol)
	if err != nil {
		db.Close()
		return nil, err
	}
	defaultDatabase = selectDefaultSQLConsoleDatabase(defaultDatabase, databases, account.Instance.Protocol)
	return &databaseSQLConsoleConnection{
		account:         account,
		databases:       databases,
		defaultDatabase: defaultDatabase,
		pools:           map[string]*sql.DB{databasePoolKey(defaultSQLConsoleDatabase(account.Instance.Protocol)): db},
	}, nil
}

type databaseSQLConsoleConnection struct {
	mu              sync.Mutex
	account         model.DatabaseAccount
	databases       []string
	defaultDatabase string
	pools           map[string]*sql.DB
	closed          bool
}

func (c *databaseSQLConsoleConnection) Databases() []string {
	return append([]string(nil), c.databases...)
}

func (c *databaseSQLConsoleConnection) DefaultDatabase() string {
	return c.defaultDatabase
}

func (c *databaseSQLConsoleConnection) Execute(
	ctx context.Context,
	database, statement string,
	readOnly bool,
) (SQLConsoleExecution, error) {
	if ctx == nil {
		return SQLConsoleExecution{}, errors.New("execute SQL: nil context")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return SQLConsoleExecution{}, errors.New("SQL console connection is closed")
	}
	executionContext, cancel := context.WithTimeout(ctx, sqlConsoleTimeout)
	defer cancel()
	db, err := c.databasePool(executionContext, strings.TrimSpace(database))
	if err != nil {
		return SQLConsoleExecution{}, err
	}
	if readOnly {
		return querySQLConsole(executionContext, db, statement)
	}
	result, err := db.ExecContext(executionContext, statement)
	if err != nil {
		return SQLConsoleExecution{}, fmt.Errorf("execute statement: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return SQLConsoleExecution{}, nil
	}
	return SQLConsoleExecution{RowsAffected: affected, RowsAffectedKnown: true}, nil
}

func (c *databaseSQLConsoleConnection) databasePool(ctx context.Context, database string) (*sql.DB, error) {
	key := databasePoolKey(database)
	if db := c.pools[key]; db != nil {
		return db, nil
	}
	db, err := openSQLConsoleDatabase(c.account, database)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	c.pools[key] = db
	return db, nil
}

// Metadata 查询数据库表结构与列类型(只读,不建审计会话)。
func (c *databaseSQLConsoleConnection) Metadata(ctx context.Context, database string) (SQLConsoleMetadata, error) {
	if ctx == nil {
		return SQLConsoleMetadata{}, errors.New("query metadata: nil context")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return SQLConsoleMetadata{}, errors.New("SQL console connection is closed")
	}
	metadataContext, cancel := context.WithTimeout(ctx, sqlConsoleTimeout)
	defer cancel()
	db, err := c.databasePool(metadataContext, strings.TrimSpace(database))
	if err != nil {
		return SQLConsoleMetadata{}, err
	}
	tablesSQL, columnsSQL := metadataStatementSQL(c.account.Instance.Protocol)
	tablesRows, err := querySQLConsoleRows(metadataContext, db, tablesSQL)
	if err != nil {
		return SQLConsoleMetadata{}, fmt.Errorf("query metadata tables: %w", err)
	}
	columnsRows, err := querySQLConsoleRows(metadataContext, db, columnsSQL)
	if err != nil {
		return SQLConsoleMetadata{}, fmt.Errorf("query metadata columns: %w", err)
	}
	return parseMetadataTables(c.account.Instance.Protocol, tablesRows, columnsRows), nil
}

// querySQLConsoleRows 执行只读查询并返回原始行数据(复用现有查询行扫描模式)。
func querySQLConsoleRows(ctx context.Context, db *sql.DB, statement string) ([][]any, error) {
	transaction, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin read-only query: %w", err)
	}
	defer transaction.Rollback()
	rows, err := transaction.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read result columns: %w", err)
	}
	result := make([][]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("scan result row: %w", err)
		}
		for index := range values {
			normalized, _, _ := normalizeSQLConsoleValue(values[index])
			values[index] = normalized
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read result rows: %w", err)
	}
	return result, nil
}

func (c *databaseSQLConsoleConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var closeErr error
	for _, db := range c.pools {
		if err := db.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	c.pools = nil
	return closeErr
}

func databasePoolKey(database string) string {
	return strings.TrimSpace(database)
}

func defaultSQLConsoleDatabase(protocol string) string {
	if strings.EqualFold(strings.TrimSpace(protocol), "postgres") || strings.EqualFold(strings.TrimSpace(protocol), "postgresql") {
		return "postgres"
	}
	return ""
}

func listSQLConsoleDatabases(ctx context.Context, db *sql.DB, protocol string) ([]string, error) {
	statement := "SHOW DATABASES"
	if strings.EqualFold(strings.TrimSpace(protocol), "postgres") || strings.EqualFold(strings.TrimSpace(protocol), "postgresql") {
		statement = "SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate AND has_database_privilege(datname, 'CONNECT') ORDER BY datname"
	}
	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()
	databases := make([]string, 0)
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return nil, fmt.Errorf("read database name: %w", err)
		}
		if database = strings.TrimSpace(database); database != "" {
			databases = append(databases, database)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read databases: %w", err)
	}
	return databases, nil
}

func selectDefaultSQLConsoleDatabase(current string, databases []string, protocol string) string {
	for _, database := range databases {
		if database == current {
			return current
		}
	}
	if strings.EqualFold(strings.TrimSpace(protocol), "mysql") {
		systemDatabases := map[string]struct{}{
			"information_schema": {}, "mysql": {}, "performance_schema": {}, "sys": {},
		}
		for _, database := range databases {
			if _, system := systemDatabases[strings.ToLower(database)]; !system {
				return database
			}
		}
	}
	if len(databases) > 0 {
		return databases[0]
	}
	return ""
}

func openSQLConsoleDatabase(account model.DatabaseAccount, database string) (*sql.DB, error) {
	instance := account.Instance
	port := effectiveDatabasePort(instance)
	if strings.TrimSpace(instance.Address) == "" || port < 1 || port > 65535 {
		return nil, ErrSQLConsoleUnavailable
	}
	address := net.JoinHostPort(instance.Address, fmt.Sprintf("%d", port))
	tlsConfig, err := sqlConsoleTLSConfig(instance, address)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(instance.Protocol)) {
	case "mysql":
		config := mysqlDriver.NewConfig()
		config.User = account.Username
		config.Passwd = account.Password.GetPlaintext()
		config.Net = "tcp"
		config.Addr = address
		config.DBName = database
		config.ParseTime = true
		config.Timeout = 10 * time.Second
		config.ReadTimeout = sqlConsoleTimeout
		config.WriteTimeout = sqlConsoleTimeout
		config.MultiStatements = false
		config.TLS = tlsConfig
		connector, connectorErr := mysqlDriver.NewConnector(config)
		if connectorErr != nil {
			return nil, fmt.Errorf("configure mysql connection: %w", connectorErr)
		}
		return sql.OpenDB(connector), nil
	case "postgres", "postgresql":
		if database == "" {
			database = "postgres"
		}
		config, parseErr := pgx.ParseConfig("")
		if parseErr != nil {
			return nil, fmt.Errorf("configure postgres connection: %w", parseErr)
		}
		config.Host = instance.Address
		config.Port = uint16(port)
		config.User = account.Username
		config.Password = account.Password.GetPlaintext()
		config.Database = database
		config.TLSConfig = tlsConfig
		config.ConnectTimeout = 10 * time.Second
		config.Fallbacks = nil
		config.RuntimeParams["application_name"] = "jianmen-sql-console"
		return stdlib.OpenDB(*config), nil
	default:
		return nil, ErrSQLConsoleUnsupported
	}
}

func sqlConsoleTLSConfig(instance model.DatabaseInstance, address string) (*tls.Config, error) {
	mode, err := dbtls.NormalizeMode(instance.TLSMode)
	if err != nil {
		return nil, fmt.Errorf("configure database TLS: %w", err)
	}
	if mode == dbtls.ModeDisable {
		return nil, nil
	}
	config, err := dbtls.ClientConfig(
		dbtls.Config{Mode: mode, ServerName: instance.TLSServerName, CAPEM: instance.TLSCAPEM},
		address,
	)
	if err != nil {
		return nil, fmt.Errorf("configure database TLS: %w", err)
	}
	return config, nil
}

func effectiveDatabasePort(instance model.DatabaseInstance) int {
	if instance.Port > 0 {
		return instance.Port
	}
	if strings.EqualFold(instance.Protocol, "postgres") || strings.EqualFold(instance.Protocol, "postgresql") {
		return 5432
	}
	return 3306
}

func querySQLConsole(ctx context.Context, db *sql.DB, statement string) (SQLConsoleExecution, error) {
	transaction, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SQLConsoleExecution{}, fmt.Errorf("begin read-only query: %w", err)
	}
	defer transaction.Rollback()
	rows, err := transaction.QueryContext(ctx, statement)
	if err != nil {
		return SQLConsoleExecution{}, fmt.Errorf("execute query: %w", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return SQLConsoleExecution{}, fmt.Errorf("read result columns: %w", err)
	}
	if len(columns) > sqlConsoleMaxColumns {
		return SQLConsoleExecution{}, errors.New("query result has too many columns")
	}
	result := SQLConsoleExecution{Columns: columns, Rows: make([][]any, 0)}
	totalBytes := 0
	for rows.Next() {
		if len(result.Rows) >= sqlConsoleMaxRows || totalBytes >= sqlConsoleMaxResultBytes {
			result.Truncated = true
			break
		}
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return SQLConsoleExecution{}, fmt.Errorf("scan result row: %w", err)
		}
		for index := range values {
			normalized, size, truncated := normalizeSQLConsoleValue(values[index])
			values[index] = normalized
			totalBytes += size
			result.Truncated = result.Truncated || truncated
		}
		result.Rows = append(result.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return SQLConsoleExecution{}, fmt.Errorf("read result rows: %w", err)
	}
	return result, nil
}

type SQLConsoleMetadata struct {
	Tables []SQLConsoleTableMeta `json:"tables"`
}

type SQLConsoleTableMeta struct {
	Name    string                 `json:"name"`
	Detail  string                 `json:"detail"`
	Columns []SQLConsoleColumnMeta `json:"columns"`
}

type SQLConsoleColumnMeta struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// isPostgresProtocol 判断协议是否为 PostgreSQL(兼容 postgresql 写法)。
func isPostgresProtocol(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(protocol), "postgres") ||
		strings.EqualFold(strings.TrimSpace(protocol), "postgresql")
}

// metadataStatementSQL 返回按协议区分元数据查询语句(统一 information_schema)。
func metadataStatementSQL(protocol string) (tablesSQL, columnsSQL string) {
	schemaExpr := "DATABASE()"
	tablesCatalog := "information_schema.TABLES"
	positionExpr := "ORDINAL_POSITION"
	if isPostgresProtocol(protocol) {
		schemaExpr = "current_schema()"
		tablesCatalog = "information_schema.tables"
		positionExpr = "ordinal_position"
	}
	tablesSQL = "SELECT table_name, '' AS detail FROM " + tablesCatalog +
		" WHERE table_schema = " + schemaExpr + " ORDER BY table_name"
	columnsSQL = "SELECT table_name, column_name, data_type FROM information_schema.columns" +
		" WHERE table_schema = " + schemaExpr + " ORDER BY table_name, " + positionExpr
	return tablesSQL, columnsSQL
}

// parseMetadataTables 将两轮查询结果组装为元数据,表数超过上限时截断。
func parseMetadataTables(protocol string, tablesRows [][]any, columnsRows [][]any) SQLConsoleMetadata {
	limit := sqlConsoleMetadataMaxTables
	if len(tablesRows) > limit {
		tablesRows = tablesRows[:limit]
	}
	meta := SQLConsoleMetadata{Tables: make([]SQLConsoleTableMeta, 0, len(tablesRows))}
	index := make(map[string]int, len(tablesRows))
	for _, row := range tablesRows {
		name, _ := row[0].(string)
		detail, _ := row[1].(string)
		meta.Tables = append(meta.Tables, SQLConsoleTableMeta{Name: name, Detail: detail})
		index[name] = len(meta.Tables) - 1
	}
	for _, row := range columnsRows {
		table, _ := row[0].(string)
		column, _ := row[1].(string)
		columnType, _ := row[2].(string)
		if tableIndex, ok := index[table]; ok {
			meta.Tables[tableIndex].Columns = append(
				meta.Tables[tableIndex].Columns,
				SQLConsoleColumnMeta{Name: column, Type: columnType},
			)
		}
	}
	return meta
}

func normalizeSQLConsoleValue(value any) (any, int, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, 0, false
	case bool:
		return typed, len(fmt.Sprint(typed)), false
	case int64:
		return typed, len(fmt.Sprint(typed)), false
	case float64:
		return typed, len(fmt.Sprint(typed)), false
	case time.Time:
		text := typed.UTC().Format(time.RFC3339Nano)
		return text, len(text), false
	case string:
		text, truncated := AuditDBQueryUTF8Prefix(typed, sqlConsoleMaxCellBytes)
		return text, len(text), truncated
	case []byte:
		raw := append([]byte(nil), typed...)
		if utf8.Valid(raw) {
			text, truncated := AuditDBQueryUTF8Prefix(string(raw), sqlConsoleMaxCellBytes)
			return text, len(text), truncated
		}
		truncated := len(raw) > sqlConsoleMaxCellBytes
		if truncated {
			raw = raw[:sqlConsoleMaxCellBytes]
		}
		encoded := "base64:" + base64.StdEncoding.EncodeToString(raw)
		return encoded, len(encoded), truncated
	default:
		text := fmt.Sprint(value)
		text, truncated := AuditDBQueryUTF8Prefix(text, sqlConsoleMaxCellBytes)
		return text, len(text), truncated
	}
}
