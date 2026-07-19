package store

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestDatabaseInstanceDefaultsUpstreamTLSDisabled(t *testing.T) {
	repository, db := newDatabaseTLSTestStore(t)
	view, err := repository.AddDatabaseInstance(context.Background(), DatabaseInstanceInput{
		Name: "orders", Protocol: "mysql", Address: "192.0.2.10", Port: 3306,
	})
	if err != nil {
		t.Fatalf("add database instance: %v", err)
	}
	if view.TLSMode != dbtls.ModeDisable {
		t.Fatalf("new database TLS mode = %q, want %q", view.TLSMode, dbtls.ModeDisable)
	}
	var stored model.DatabaseInstance
	if err := db.First(&stored, "id = ?", view.ID).Error; err != nil {
		t.Fatalf("load database instance: %v", err)
	}
	if stored.TLSMode != dbtls.ModeDisable {
		t.Fatalf("stored database TLS mode = %q, want %q", stored.TLSMode, dbtls.ModeDisable)
	}
}

func TestDatabaseInstanceUpdateWithoutTLSModePreservesPolicyAndCA(t *testing.T) {
	repository, db := newDatabaseTLSTestStore(t)
	caPEM := testDatabaseCAPEM(t)
	created, err := repository.AddDatabaseInstance(context.Background(), DatabaseInstanceInput{
		Name: "orders", Protocol: "mysql", Address: "db.internal", Port: 3306,
		TLSMode: dbtls.ModeVerifyFull, TLSServerName: "db.internal", TLSCAPEM: &caPEM,
	})
	if err != nil {
		t.Fatalf("add database instance: %v", err)
	}
	var before model.DatabaseInstance
	if err := db.First(&before, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("load database instance before update: %v", err)
	}
	updated, err := repository.UpdateDatabaseInstance(context.Background(), created.ID, DatabaseInstanceInput{
		Name: "renamed", Protocol: "mysql", Address: "db.internal", Port: 3306,
		Status: "active",
	})
	if err != nil {
		t.Fatalf("update database instance: %v", err)
	}
	if updated.TLSMode != dbtls.ModeVerifyFull || updated.TLSServerName != "db.internal" || !updated.HasTLSCA {
		t.Fatalf(
			"updated TLS policy = mode %q, server name %q, has CA %t",
			updated.TLSMode,
			updated.TLSServerName,
			updated.HasTLSCA,
		)
	}
	var stored model.DatabaseInstance
	if err := db.First(&stored, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("load database instance: %v", err)
	}
	if stored.TLSMode != dbtls.ModeVerifyFull ||
		stored.TLSServerName != before.TLSServerName ||
		stored.TLSCAPEM != before.TLSCAPEM {
		t.Fatalf(
			"stored TLS policy was changed: mode %q, server name preserved %t, CA preserved %t",
			stored.TLSMode,
			stored.TLSServerName == before.TLSServerName,
			stored.TLSCAPEM == before.TLSCAPEM,
		)
	}
}

func newDatabaseTLSTestStore(t *testing.T) (*DBStore, *gorm.DB) {
	t.Helper()
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return NewDBStore(db), db
}

func testDatabaseCAPEM(t *testing.T) string {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	certificate := server.Certificate()
	if certificate == nil {
		t.Fatal("test TLS server has no certificate")
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}))
}
