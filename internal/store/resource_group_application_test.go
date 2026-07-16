package store

import (
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
	if _, err := store.AddApplication("app", "http", "127.0.0.1", 8080, 47110, "applications", ""); err != nil {
		t.Fatalf("add application: %v", err)
	}
	owner := model.User{ID: "owner-1", Username: "owner", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := store.AddPlatformAccount(model.PlatformAccount{
		Name: "gitlab", PlatformName: "GitLab", Username: "gitlab-user",
		GroupName: "platforms", OwnerID: owner.ID, Visibility: "private", Status: "active",
	}); err != nil {
		t.Fatalf("add platform account: %v", err)
	}

	for _, name := range []string{"applications", "platforms"} {
		var group model.ResourceGroup
		if err := db.Where("name = ? AND group_type = ?", name, model.ResourceGroupTypeResource).First(&group).Error; err != nil {
			t.Fatalf("resource group %q was not created: %v", name, err)
		}
	}
}
