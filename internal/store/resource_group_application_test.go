package store

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestApplicationsAndPlatformsCreateResourceGroups(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	store := NewDBStore(db)
	if _, err := store.AddApplication(context.Background(), ApplicationInput{Name: "app", Address: "http://127.0.0.1:8080/", EntryPath: "/", InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080, ListenPort: 47110, AppGroup: "applications"}); err != nil {
		t.Fatalf("add application: %v", err)
	}
	owner := model.User{ID: "owner-1", Username: "owner", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := store.AddPlatformAccount(model.PlatformAccount{
		Name: "gitlab", PlatformName: "GitLab", Username: "gitlab-user",
		GroupName: "platforms", OwnerID: owner.ID, Status: "active",
	}); err != nil {
		t.Fatalf("add platform account: %v", err)
	}

	for _, expected := range []struct{ name, groupType string }{{"applications", model.ResourceGroupTypeResource}, {"platforms", model.ResourceGroupTypeAccount}} {
		var group model.ResourceGroup
		if err := db.Where("name = ? AND group_type = ?", expected.name, expected.groupType).First(&group).Error; err != nil {
			t.Fatalf("resource group %q was not created: %v", expected.name, err)
		}
	}
}

func TestApplicationQueriesHonorsCanceledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	store := NewDBStore(db)
	if err := db.Create(&model.Application{
		ID:             "app-ctx",
		Name:           "context-app",
		Address:        "http://127.0.0.1:8080/",
		EntryPath:      "/",
		InternalScheme: "http",
		InternalHost:   "127.0.0.1",
		InternalPort:   8080,
		ListenPort:     47110,
		Status:         "active",
	}).Error; err != nil {
		t.Fatalf("seed application: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Application(ctx, "app-ctx")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Application canceled context error = %v, want %v", err, context.Canceled)
	}
}

func TestApplicationWriteOperationsHonorsCanceledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	store := NewDBStore(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.AddApplication(ctx, ApplicationInput{
		Name: "canceled-add", Address: "http://127.0.0.1:8080/", EntryPath: "/",
		InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080, ListenPort: 47110,
	}); !errors.Is(err, context.Canceled) {
		t.Fatal("AddApplication with canceled context did not return error")
	}
	var count int64
	if err := db.Model(&model.Application{}).Count(&count).Error; err != nil {
		t.Fatalf("count applications: %v", err)
	}
	if count != 0 {
		t.Fatalf("applications should not be created when context is canceled, count=%d", count)
	}

	created := model.Application{
		ID:             "app-update",
		Name:           "before",
		Address:        "http://127.0.0.1:8080/",
		EntryPath:      "/",
		InternalScheme: "http",
		InternalHost:   "127.0.0.1",
		InternalPort:   8080,
		ListenPort:     47110,
		Status:         "active",
	}
	if err := db.Create(&created).Error; err != nil {
		t.Fatalf("seed application for update/delete: %v", err)
	}
	if _, err := store.UpdateApplication(ctx, "app-update", ApplicationInput{
		Address:        "http://127.0.0.2:8081/",
		InternalScheme: "http",
		InternalHost:   "127.0.0.2",
		InternalPort:   8081,
		ListenPort:     47110,
	}); !errors.Is(err, context.Canceled) {
		t.Fatal("UpdateApplication with canceled context did not return error")
	}
	var updated model.Application
	if err := db.First(&updated, "id = ?", "app-update").Error; err != nil {
		t.Fatalf("query updated application: %v", err)
	}
	if updated.InternalHost != "127.0.0.1" || updated.InternalPort != 8080 {
		t.Fatalf("application should not update when context is canceled, app=%#v", updated)
	}
	if err := store.DeleteApplication(ctx, "app-update"); !errors.Is(err, context.Canceled) {
		t.Fatal("DeleteApplication with canceled context did not return error")
	}
	if err := db.First(&updated, "id = ?", "app-update").Error; err != nil {
		t.Fatalf("application should still exist after canceled delete: %v", err)
	}
}
