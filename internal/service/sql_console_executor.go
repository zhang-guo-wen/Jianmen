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
	"time"
	"unicode/utf8"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

const (
	sqlConsoleTimeout        = 30 * time.Second
	sqlConsoleMaxRows        = 500
	sqlConsoleMaxColumns     = 256
	sqlConsoleMaxCellBytes   = 64 * 1024
	sqlConsoleMaxResultBytes = 4 * 1024 * 1024
)

type SQLConsoleExecution struct {
	Columns      []string
	Rows         [][]any
	RowsAffected int64
	Truncated    bool
}

type SQLConsoleExecutor interface {
	Execute(context.Context, model.DatabaseAccount, string, string, bool) (SQLConsoleExecution, error)
}

type DatabaseSQLConsoleExecutor struct{}

func NewDatabaseSQLConsoleExecutor() *DatabaseSQLConsoleExecutor {
	return &DatabaseSQLConsoleExecutor{}
}

func (e *DatabaseSQLConsoleExecutor) Execute(
	ctx context.Context,
	account model.DatabaseAccount,
	database, statement string,
	readOnly bool,
) (SQLConsoleExecution, error) {
	if ctx == nil {
		return SQLConsoleExecution{}, errors.New("execute SQL: nil context")
	}
	executionContext, cancel := context.WithTimeout(ctx, sqlConsoleTimeout)
	defer cancel()
	db, err := openSQLConsoleDatabase(account, strings.TrimSpace(database))
	if err != nil {
		return SQLConsoleExecution{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	if err := db.PingContext(executionContext); err != nil {
		return SQLConsoleExecution{}, fmt.Errorf("connect database: %w", err)
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
		affected = 0
	}
	return SQLConsoleExecution{RowsAffected: affected}, nil
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
