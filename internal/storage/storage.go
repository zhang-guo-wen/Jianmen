package storage

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"jianmen/internal/model"
)

var sqliteMemoryDSNSequence atomic.Uint64

type Driver string

const (
	DriverSQLite   Driver = "sqlite"
	DriverMySQL    Driver = "mysql"
	DriverPostgres Driver = "postgres"
)

type Config struct {
	Driver          Driver        `json:"driver"`
	DSN             string        `json:"dsn"`
	MaxOpenConns    int           `json:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
}

func Open(cfg Config) (*gorm.DB, error) {
	driver := normalizeDriver(cfg.Driver)
	if driver == "" {
		driver = DriverSQLite
	}

	dialector, err := dialectorFor(driver, strings.TrimSpace(cfg.DSN))
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", driver, err)
	}
	if err := configurePool(db, driver, cfg); err != nil {
		return nil, err
	}
	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("storage: nil database")
	}
	if err := db.AutoMigrate(&model.ResourceSequence{}); err != nil {
		return fmt.Errorf("auto migrate sequences: %w", err)
	}
	if db.Migrator().HasTable(&model.UserSession{}) {
		if err := repairUserSessions(db); err != nil {
			return fmt.Errorf("repair user sessions before auto migrate: %w", err)
		}
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	return nil
}

func dialectorFor(driver Driver, dsn string) (gorm.Dialector, error) {
	switch driver {
	case DriverSQLite:
		if dsn == "" {
			dsn = "data/bastion.db"
		}
		if err := ensureSQLiteDir(dsn); err != nil {
			return nil, err
		}
		return sqlite.Open(sqliteDSNWithForeignKeys(dsn)), nil
	case DriverMySQL:
		if dsn == "" {
			return nil, errors.New("storage: mysql dsn is required")
		}
		return mysql.Open(dsn), nil
	case DriverPostgres:
		if dsn == "" {
			return nil, errors.New("storage: postgres dsn is required")
		}
		return postgres.Open(dsn), nil
	default:
		return nil, fmt.Errorf("storage: unsupported driver %q", driver)
	}
}

func sqliteDSNWithForeignKeys(dsn string) string {
	if strings.TrimSpace(dsn) == ":memory:" {
		dsn = fmt.Sprintf("file:jianmen-memory-%d?mode=memory&cache=shared", sqliteMemoryDSNSequence.Add(1))
	}

	base, query, hasQuery := strings.Cut(dsn, "?")
	parts := make([]string, 0, 4)
	if hasQuery && query != "" {
		parts = strings.Split(query, "&")
	}
	filtered := parts[:0]
	for _, part := range parts {
		if isSQLiteForeignKeysPragma(part) {
			continue
		}
		filtered = append(filtered, part)
	}
	parts = append(filtered, "_pragma=foreign_keys(1)")
	return base + "?" + strings.Join(parts, "&")
}

func isSQLiteForeignKeysPragma(part string) bool {
	key, value, hasValue := strings.Cut(part, "=")
	if !hasValue {
		return false
	}
	decodedKey, err := url.QueryUnescape(key)
	if err != nil || !strings.EqualFold(decodedKey, "_pragma") {
		return false
	}
	decodedValue, err := url.QueryUnescape(value)
	if err != nil {
		return false
	}
	return len(decodedValue) >= len("foreign_keys()") &&
		strings.EqualFold(decodedValue[:len("foreign_keys(")], "foreign_keys(") &&
		decodedValue[len(decodedValue)-1] == ')'
}

func ensureSQLiteDir(dsn string) error {
	if isMemoryDSN(dsn) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(dsn)), "file:") {
		return nil
	}
	dir := filepath.Dir(dsn)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: create sqlite directory %q: %w", dir, err)
	}
	return nil
}

func configurePool(db *gorm.DB, driver Driver, cfg Config) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("storage: get sql db: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	} else if driver == DriverSQLite && isMemoryDSN(cfg.DSN) {
		sqlDB.SetMaxOpenConns(1)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	return nil
}

func normalizeDriver(driver Driver) Driver {
	switch Driver(strings.ToLower(strings.TrimSpace(string(driver)))) {
	case "", DriverSQLite, "sqlite3":
		return DriverSQLite
	case DriverMySQL:
		return DriverMySQL
	case DriverPostgres, "postgresql":
		return DriverPostgres
	default:
		return Driver(strings.ToLower(strings.TrimSpace(string(driver))))
	}
}

func isMemoryDSN(dsn string) bool {
	dsn = strings.ToLower(strings.TrimSpace(dsn))
	return dsn == ":memory:" || strings.Contains(dsn, "mode=memory") || strings.Contains(dsn, "file::memory:")
}
