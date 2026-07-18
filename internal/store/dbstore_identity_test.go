package store

import (
	"context"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestDBStoreIdentitySubjectPersistsSuperAdministratorFlag(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	user := model.User{
		ID:           "u-admin",
		Username:     "admin",
		Status:       "active",
		IsSuperAdmin: true,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	subject, found, err := NewDBStore(db).FindIdentitySubject(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find identity subject: %v", err)
	}
	if !found {
		t.Fatal("identity subject not found")
	}
	if subject.ID != user.ID || subject.Username != user.Username || !subject.SuperAdmin || subject.Status != "active" {
		t.Fatalf("identity subject = %#v", subject)
	}

	var persisted model.User
	if err := db.First(&persisted, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !persisted.IsSuperAdmin {
		t.Fatal("is_super_admin was not persisted")
	}
}

func TestDBStoreIdentitySubjectMissing(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	subject, found, err := NewDBStore(db).FindIdentitySubject(context.Background(), "missing")
	if err != nil {
		t.Fatalf("find identity subject: %v", err)
	}
	if found || subject.ID != "" {
		t.Fatalf("identity subject = %#v found=%t", subject, found)
	}
}
