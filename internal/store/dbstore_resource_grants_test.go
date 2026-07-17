package store

import (
	"context"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

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
