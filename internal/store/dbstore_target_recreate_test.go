package store

import (
	"context"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/storage"
)

// TestRecreateHostAccountWithSameIDAfterDelete 覆盖删除后重建同 ID 账号：
// 软删行 active_marker = NULL 不再占用 id 唯一性（设计文档「停用后可重建」）。
func TestRecreateHostAccountWithSameIDAfterDelete(t *testing.T) {
	// 初始化加密主密钥（EncryptedField 写入需要）
	if _, err := crypto.Init(t.TempDir()); err != nil {
		t.Fatalf("crypto init: %v", err)
	}
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.Host{
		ID: "web-01", Name: "web-01", Address: "127.0.0.1",
		Port: 22, Protocol: "ssh", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}

	repository := NewDBStore(db)
	ctx := context.Background()
	account := config.Target{
		ID: "web-01-admin", HostID: "web-01", Host: "127.0.0.1",
		Port: 22, Protocol: "ssh", Username: "admin", Password: "secret",
	}

	if _, err := repository.AddTarget(ctx, account); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := repository.DeleteTarget(ctx, account.ID); err != nil {
		t.Fatalf("delete target: %v", err)
	}
	// 软删行仍保留在主表中（墓碑）
	var tombstones int64
	if err := db.Table("host_accounts").Where("id = ? AND active_marker IS NULL", account.ID).Count(&tombstones).Error; err != nil {
		t.Fatalf("count tombstones: %v", err)
	}
	if tombstones != 1 {
		t.Fatalf("tombstone count = %d, want 1", tombstones)
	}

	// 删除后重建同 ID 必须成功
	if _, err := repository.AddTarget(ctx, account); err != nil {
		t.Fatalf("recreate target with same id: %v", err)
	}
	if _, err := repository.Target(ctx, account.ID); err != nil {
		t.Fatalf("load recreated target: %v", err)
	}
	var activeCount int64
	if err := db.Table("host_accounts").Where("id = ? AND active_marker = 1", account.ID).Count(&activeCount).Error; err != nil {
		t.Fatalf("count active row: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active count = %d, want 1", activeCount)
	}
}
