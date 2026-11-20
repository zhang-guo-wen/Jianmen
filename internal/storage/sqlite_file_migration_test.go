package storage

import (
	"path/filepath"
	"testing"

	"jianmen/internal/model"
)

// 全新 SQLite 文件库必须能完整执行版本化迁移（包括重建 database_accounts 的 009 迁移）。
func TestMigrateFreshSQLiteFileDatabase(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(Config{Driver: DriverSQLite, DSN: filepath.Join(dir, "fresh.db"), MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	defer sqlDB.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate fresh sqlite file database: %v", err)
	}
	for _, table := range []any{&model.DatabaseAccount{}, &model.PlatformAccount{}, &model.User{}} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("expected migrated table for %T", table)
		}
	}
}

// 临时目录不可用时，迁移重建整表仍应成功：临时存储走内存，不依赖系统临时目录。
func TestMigrateFreshSQLiteFileDatabaseWithoutUsableTempDir(t *testing.T) {
	dir := t.TempDir()
	unusableTemp := filepath.Join(dir, "missing-temp-dir") // 故意不创建
	t.Setenv("TEMP", unusableTemp)
	t.Setenv("TMP", unusableTemp)

	db, err := Open(Config{Driver: DriverSQLite, DSN: filepath.Join(dir, "fresh.db"), MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	defer sqlDB.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate with unusable temp dir: %v", err)
	}
}
