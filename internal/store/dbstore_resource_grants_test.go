package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestDBStoreEnsureResourceGrantConcurrent(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "resource-grants.db"))
	db, err := storage.Open(storage.Config{
		Driver:       storage.DriverSQLite,
		DSN:          "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
		MaxOpenConns: 16,
		MaxIdleConns: 16,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sql database: %v", err)
		}
	})

	repository := NewDBStore(db)
	grant := model.ResourceGrant{
		PrincipalType: "user",
		PrincipalID:   "user-1",
		ResourceType:  model.ResourceTypeHost,
		ResourceID:    "host-1",
		Effect:        model.PermissionEffectAllow,
	}
	const workers = 16
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errs <- repository.EnsureResourceGrant(context.Background(), grant)
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ensure resource grant: %v", err)
		}
	}

	var count int64
	if err := db.Model(&model.ResourceGrant{}).
		Where("principal_type = ? AND principal_id = ? AND resource_type = ? AND resource_id = ? AND effect = ?",
			grant.PrincipalType, grant.PrincipalID, grant.ResourceType, grant.ResourceID, grant.Effect).
		Count(&count).Error; err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if count != 1 {
		t.Fatalf("resource grant count = %d, want 1", count)
	}
}

func TestDBStoreSearchResourceGrantsIncludesApplicationAndContainerNames(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	items := []any{
		&model.User{ID: "u1", Username: "alice", Status: "active"},
		&model.Application{ID: "app-1", Name: "Grafana Portal", Address: "http://grafana.local", InternalScheme: "http", InternalHost: "grafana.local", InternalPort: 3000, ListenPort: 47101, Status: "active"},
		&model.ContainerEndpoint{ID: "container-1", Name: "Production Docker", Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI, Address: "tcp://docker.local:2375", Status: "active"},
		&model.ResourceGrant{ID: "grant-app", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeApplication, ResourceID: "app-1", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "grant-container", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: "container-1", Effect: model.PermissionEffectAllow},
	}
	for _, item := range items {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("create %T: %v", item, err)
		}
	}
	store := NewDBStore(db)

	appGrants, err := store.SearchResourceGrants(context.Background(), "grafana")
	if err != nil {
		t.Fatalf("search application grant: %v", err)
	}
	if len(appGrants) != 1 || appGrants[0].ID != "grant-app" {
		t.Fatalf("application grants = %#v", appGrants)
	}
	containerGrants, err := store.SearchResourceGrants(context.Background(), "production docker")
	if err != nil {
		t.Fatalf("search container grant: %v", err)
	}
	if len(containerGrants) != 1 || containerGrants[0].ID != "grant-container" {
		t.Fatalf("container grants = %#v", containerGrants)
	}
}

func TestDBStoreResourceGrantReferenceChecksGroupType(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.ResourceGroup{ID: "group-1", Name: "ops", GroupType: model.ResourceGroupTypeAccount}).Error; err != nil {
		t.Fatalf("create resource group: %v", err)
	}
	store := NewDBStore(db)

	exists, err := store.ResourceGrantResourceExists(context.Background(), model.ResourceTypeGroup, "group-1")
	if err != nil {
		t.Fatalf("check resource group: %v", err)
	}
	if exists {
		t.Fatal("account group must not match resource_group type")
	}
	exists, err = store.ResourceGrantResourceExists(context.Background(), model.ResourceTypeAccountGroup, "group-1")
	if err != nil {
		t.Fatalf("check account group: %v", err)
	}
	if !exists {
		t.Fatal("account group should match account_group type")
	}
}
