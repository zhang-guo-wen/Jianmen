package rbac

import (
	"testing"

	"jianmen/internal/model"
)

func TestResourceGrantCheckerMatchesResourceAndAccountGroups(t *testing.T) {
	db := newTestDB(t)
	models := []any{
		&model.User{ID: "u1", Username: "user", Status: "active"},
		&model.Host{ID: "h1", Name: "host", Address: "127.0.0.1", Port: 22, GroupName: "prod"},
		&model.HostAccount{ID: "ha-resource", HostID: "h1", Username: "root", Status: "active", ResourceID: "0001"},
		&model.HostAccount{ID: "ha-account", HostID: "h1", Username: "deploy", Status: "active", ResourceID: "0002", GroupName: "ops"},
		&model.Application{ID: "app1", Name: "console", AppGroup: "prod", ListenPort: 18080, InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080, Status: "active"},
		&model.PlatformAccount{ID: "platform1", Name: "gitlab", PlatformName: "GitLab", GroupName: "prod", Username: "admin", OwnerID: "u1", Visibility: "private", Status: "active"},
		&model.ResourceGroup{ID: "rg-prod", Name: "prod", GroupType: model.ResourceGroupTypeResource},
		&model.ResourceGroup{ID: "ag-ops", Name: "ops", GroupType: model.ResourceGroupTypeAccount},
		&model.ResourceGrant{ID: "grant-resource", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeGroup, ResourceID: "rg-prod", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "grant-account", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeAccountGroup, ResourceID: "ag-ops", Effect: model.PermissionEffectAllow},
	}
	for _, item := range models {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("create %T: %v", item, err)
		}
	}

	checker := NewResourceGrantChecker(db)
	checks := []struct {
		resourceType string
		resourceID   string
	}{
		{model.ResourceTypeHostAccount, "ha-resource"},
		{model.ResourceTypeHostAccount, "ha-account"},
		{model.ResourceTypeApplication, "app1"},
		{model.ResourceTypePlatformAccount, "platform1"},
	}
	for _, check := range checks {
		allowed, err := checker.HasGrant("u1", check.resourceType, check.resourceID)
		if err != nil {
			t.Fatalf("HasGrant(%s, %s): %v", check.resourceType, check.resourceID, err)
		}
		if !allowed {
			t.Fatalf("HasGrant(%s, %s) = false, want true", check.resourceType, check.resourceID)
		}
	}
}

func TestResourceGrantCheckerDenyOverridesGroupAllow(t *testing.T) {
	db := newTestDB(t)
	models := []any{
		&model.User{ID: "u1", Username: "user", Status: "active"},
		&model.Host{ID: "h1", Name: "host", Address: "127.0.0.1", Port: 22, GroupName: "prod"},
		&model.HostAccount{ID: "ha1", HostID: "h1", Username: "root", Status: "active", ResourceID: "0001"},
		&model.ResourceGroup{ID: "rg-prod", Name: "prod", GroupType: model.ResourceGroupTypeResource},
		&model.ResourceGrant{ID: "grant-resource", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeGroup, ResourceID: "rg-prod", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "deny-account", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "ha1", Effect: model.PermissionEffectDeny},
	}
	for _, item := range models {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("create %T: %v", item, err)
		}
	}

	allowed, err := NewResourceGrantChecker(db).HasGrant("u1", model.ResourceTypeHostAccount, "ha1")
	if err != nil {
		t.Fatalf("HasGrant: %v", err)
	}
	if allowed {
		t.Fatal("HasGrant = true, want deny to override group allow")
	}
}

func TestResourceGrantCheckerContainerGrantIncludesFutureAccounts(t *testing.T) {
	db := newTestDB(t)
	models := []any{
		&model.User{ID: "u1", Username: "user", Status: "active"},
		&model.Host{ID: "h1", Name: "host", Address: "127.0.0.1", Port: 22},
		&model.DatabaseInstance{ID: "db1", Name: "database", Protocol: "mysql", Address: "127.0.0.1", Port: 3306},
		&model.ResourceGrant{ID: "grant-host", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "grant-db", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeDatabaseInstance, ResourceID: "db1", Effect: model.PermissionEffectAllow},
	}
	for _, item := range models {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("create %T: %v", item, err)
		}
	}

	// Accounts are created after the container grants; inheritance must remain dynamic.
	if err := db.Create(&model.HostAccount{ID: "ha1", HostID: "h1", Username: "root", Status: "active", ResourceID: "0001"}).Error; err != nil {
		t.Fatalf("create host account: %v", err)
	}
	if err := db.Create(&model.DatabaseAccount{ID: "dba1", InstanceID: "db1", UniqueName: "db-user", Username: "app", Status: "active", ResourceID: "0002"}).Error; err != nil {
		t.Fatalf("create database account: %v", err)
	}

	checker := NewResourceGrantChecker(db)
	checks := []struct {
		resourceType string
		resourceID   string
	}{
		{model.ResourceTypeHost, "h1"},
		{model.ResourceTypeHostAccount, "ha1"},
		{model.ResourceTypeDatabaseInstance, "db1"},
		{model.ResourceTypeDatabaseAccount, "dba1"},
	}
	for _, check := range checks {
		allowed, err := checker.HasGrant("u1", check.resourceType, check.resourceID)
		if err != nil {
			t.Fatalf("HasGrant(%s, %s): %v", check.resourceType, check.resourceID, err)
		}
		if !allowed {
			t.Fatalf("HasGrant(%s, %s) = false, want true", check.resourceType, check.resourceID)
		}
	}
}
