package store

import (
	"context"
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
	if _, err := store.AddApplication(ApplicationInput{Name: "app", Address: "http://127.0.0.1:8080/", EntryPath: "/", InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080, ListenPort: 47110, AppGroup: "applications"}); err != nil {
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
