package dbproxy

import (
	"strings"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/util"
)

func TestResolveAccountRejectsHostCompactPrefix(t *testing.T) {
	gateway := &Gateway{}
	_, err := gateway.resolveAccount(util.FullUsername(util.PrefixHost, 1, 1))
	if err == nil {
		t.Fatal("expected host compact prefix to be rejected")
	}
	if !strings.Contains(err.Error(), "expected D") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveAccountRejectsDisabledDatabaseAssets(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.DatabaseInstance{
		ID:       "db-disabled-account",
		Name:     "db-disabled-account",
		Protocol: "mysql",
		Address:  "127.0.0.1",
		Port:     3306,
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if err := db.Create(&model.DatabaseAccount{
		ID:          "dbacct-disabled",
		InstanceID:  "db-disabled-account",
		UniqueName:  "dbacct-disabled",
		Username:    "app",
		Status:      "disabled",
		ResourceSeq: 1,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixDatabase, 1),
	}).Error; err != nil {
		t.Fatalf("create disabled account: %v", err)
	}

	gateway := &Gateway{db: db}
	if _, err := gateway.resolveAccount(util.FullUsername(util.PrefixDatabase, 1, 1)); err == nil {
		t.Fatal("disabled database account resolved successfully")
	}

	if err := db.Create(&model.DatabaseInstance{
		ID:       "db-disabled-instance",
		Name:     "db-disabled-instance",
		Protocol: "mysql",
		Address:  "127.0.0.1",
		Port:     3307,
		Status:   "disabled",
	}).Error; err != nil {
		t.Fatalf("create disabled instance: %v", err)
	}
	if err := db.Create(&model.DatabaseAccount{
		ID:          "dbacct-active-on-disabled",
		InstanceID:  "db-disabled-instance",
		UniqueName:  "dbacct-active-on-disabled",
		Username:    "app",
		Status:      "active",
		ResourceSeq: 2,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixDatabase, 2),
	}).Error; err != nil {
		t.Fatalf("create active account on disabled instance: %v", err)
	}
	_, err = gateway.resolveAccount(util.FullUsername(util.PrefixDatabase, 2, 1))
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled database instance error = %v, want disabled", err)
	}
}
