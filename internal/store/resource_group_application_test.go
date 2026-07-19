package store

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"

	"gorm.io/gorm"
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
	if _, err := store.AddPlatformAccount(context.Background(), model.PlatformAccount{
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

func TestCreateManagedApplicationAtomicallyCreatesCreatorGrant(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	creator := model.User{ID: "creator-1", Username: "creator", Status: "active"}
	if err := db.Create(&creator).Error; err != nil {
		t.Fatalf("create creator: %v", err)
	}
	repository := NewDBStore(db)
	application, err := repository.CreateManagedApplication(context.Background(), model.Application{
		Name: "console", Address: "http://127.0.0.1:8080/", EntryPath: "/",
		InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080,
		ListenPort: 47110, Status: "active",
	}, creator.ID)
	if err != nil {
		t.Fatalf("create managed application: %v", err)
	}
	var grantCount int64
	if err := db.Model(&model.ResourceGrant{}).
		Where("principal_type = ? AND principal_id = ? AND resource_type = ? AND resource_id = ? AND effect = ?",
			"user", creator.ID, model.ResourceTypeApplication, application.ID, model.PermissionEffectAllow).
		Count(&grantCount).Error; err != nil {
		t.Fatalf("count creator grants: %v", err)
	}
	if grantCount != 1 {
		t.Fatalf("creator grant count = %d, want 1", grantCount)
	}
}

func TestCreateManagedApplicationGrantFailureRollsBackApplication(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	creator := model.User{ID: "creator-rollback", Username: "creator-rollback", Status: "active"}
	if err := db.Create(&creator).Error; err != nil {
		t.Fatalf("create creator: %v", err)
	}
	const callbackName = "test:fail_application_creator_grant"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "ResourceGrant" {
			tx.AddError(errors.New("injected creator grant failure"))
		}
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}
	defer db.Callback().Create().Remove(callbackName)

	repository := NewDBStore(db)
	_, err = repository.CreateManagedApplication(context.Background(), model.Application{
		Name: "orphan", Address: "http://127.0.0.1:8080/", EntryPath: "/",
		InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080,
		ListenPort: 47110, Status: "active",
	}, creator.ID)
	if err == nil {
		t.Fatal("CreateManagedApplication() succeeded despite creator grant failure")
	}
	var applicationCount int64
	if err := db.Model(&model.Application{}).Count(&applicationCount).Error; err != nil {
		t.Fatalf("count applications: %v", err)
	}
	if applicationCount != 0 {
		t.Fatalf("application count = %d, want 0 after transaction rollback", applicationCount)
	}
	var resourceCount int64
	if err := db.Model(&model.Resource{}).Where("type = ?", model.ResourceTypeApplication).Count(&resourceCount).Error; err != nil {
		t.Fatalf("count application resources: %v", err)
	}
	if resourceCount != 0 {
		t.Fatalf("application resource count = %d, want 0 after transaction rollback", resourceCount)
	}
}
