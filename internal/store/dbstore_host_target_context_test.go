package store

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/util"
)

func TestDBStoreHostReadAndCreateHonorCancelledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	repository := NewDBStore(db)
	if err := db.Create(&model.Host{
		ID: "host-read", Name: "read-host", Address: "127.0.0.1", Port: 22, Status: "active",
	}).Error; err != nil {
		t.Fatalf("create base host: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repository.Host(ctx, "host-read"); !errors.Is(err, context.Canceled) {
		t.Fatalf("host read with canceled context = %v, want %v", err, context.Canceled)
	}

	if _, err := repository.AddHost(ctx, HostRecord{
		ID:      "host-cancelled",
		Address: "10.0.0.1",
		Port:    22,
	}); err == nil {
		t.Fatalf("add host with canceled context = nil, want error")
	} else if !errors.Is(err, context.Canceled) {
		t.Fatalf("add host with canceled context = %v, want %v", err, context.Canceled)
	}

	var hostCount int64
	if err := db.Model(&model.Host{}).Where("id = ?", "host-cancelled").Count(&hostCount).Error; err != nil {
		t.Fatalf("count canceled host: %v", err)
	}
	if hostCount != 0 {
		t.Fatalf("canceled add host should not create host row, got %d", hostCount)
	}

	var resourceCount int64
	if err := db.Model(&model.Resource{}).
		Where("type = ? AND resource_id = ?", model.ResourceTypeHost, "host-cancelled").
		Count(&resourceCount).Error; err != nil {
		t.Fatalf("count canceled host resource: %v", err)
	}
	if resourceCount != 0 {
		t.Fatalf("canceled add host should not create host resource, got %d", resourceCount)
	}
}

func TestDBStoreTargetReadAndCreateHonorCancelledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	repository := NewDBStore(db)
	if err := db.Create(&model.Host{
		ID: "host-readonly", Name: "readonly-host", Address: "127.0.0.2", Port: 22, Status: "active",
	}).Error; err != nil {
		t.Fatalf("create base host: %v", err)
	}
	if err := db.Create(&model.HostAccount{
		ID:          "target-read",
		HostID:      "host-readonly",
		Name:        "existing",
		Username:    "root",
		Status:      "active",
		ResourceSeq: 1,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixHost, 1),
	}).Error; err != nil {
		t.Fatalf("create base host account: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repository.Target(ctx, "target-read"); !errors.Is(err, context.Canceled) {
		t.Fatalf("target read with canceled context = %v, want %v", err, context.Canceled)
	}

	if _, err := repository.AddTarget(ctx, config.Target{
		ID:                    "target-cancelled",
		HostID:                "host-added-by-target-write",
		Host:                  "10.0.0.2",
		Port:                  22,
		Username:              "ubuntu",
		Password:              "secret",
		Name:                  "canceled target",
		InsecureIgnoreHostKey: true,
	}); err == nil {
		t.Fatalf("add target with canceled context = nil, want error")
	} else if !errors.Is(err, context.Canceled) {
		t.Fatalf("add target with canceled context = %v, want %v", err, context.Canceled)
	}

	var accountCount int64
	if err := db.Model(&model.HostAccount{}).Where("id = ?", "target-cancelled").Count(&accountCount).Error; err != nil {
		t.Fatalf("count canceled target: %v", err)
	}
	if accountCount != 0 {
		t.Fatalf("canceled add target should not create target row, got %d", accountCount)
	}

	var hostCount int64
	if err := db.Model(&model.Host{}).Where("id = ?", "host-added-by-target-write").Count(&hostCount).Error; err != nil {
		t.Fatalf("count canceled target host: %v", err)
	}
	if hostCount != 0 {
		t.Fatalf("canceled add target should not create host row, got %d", hostCount)
	}

	var resourceCount int64
	if err := db.Model(&model.Resource{}).
		Where("type = ? AND resource_id = ?", model.ResourceTypeHostAccount, "target-cancelled").
		Count(&resourceCount).Error; err != nil {
		t.Fatalf("count canceled target resource: %v", err)
	}
	if resourceCount != 0 {
		t.Fatalf("canceled add target should not create target resource, got %d", resourceCount)
	}
}
