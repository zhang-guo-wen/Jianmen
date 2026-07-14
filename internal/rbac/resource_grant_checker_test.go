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
	for _, accountID := range []string{"ha-resource", "ha-account"} {
		allowed, err := checker.HasGrant("u1", model.ResourceTypeHostAccount, accountID)
		if err != nil {
			t.Fatalf("HasGrant(%s): %v", accountID, err)
		}
		if !allowed {
			t.Fatalf("HasGrant(%s) = false, want true", accountID)
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
