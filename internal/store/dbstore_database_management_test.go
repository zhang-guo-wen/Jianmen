package store

import (
	"context"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"

	"gorm.io/gorm"
)

func TestCreateDatabaseInstanceWithCreatorGrantRollsBackOnCreatorFailureAndCancellation(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repository := NewDBStore(db)
	input := DatabaseInstanceInput{Name: "orders", Protocol: "mysql", Address: "127.0.0.1", Port: 3306}
	if _, err := repository.CreateDatabaseInstanceWithCreatorGrant(context.Background(), input, "missing"); err == nil {
		t.Fatal("missing creator created database instance")
	}
	assertNoDatabaseInstances(t, db)
	if err := db.Create(&model.User{ID: "creator", Username: "creator", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.CreateDatabaseInstanceWithCreatorGrant(ctx, input, "creator"); err == nil {
		t.Fatal("cancelled create succeeded")
	}
	assertNoDatabaseInstances(t, db)
}

func assertNoDatabaseInstances(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int64
	if err := db.Model(&model.DatabaseInstance{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("database instance count = %d, want 0", count)
	}
}
