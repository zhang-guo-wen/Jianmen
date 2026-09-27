package rbac

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func TestCheckerRejectsInactiveAuthorizationLinksAndTargets(t *testing.T) {
	tests := []struct {
		name  string
		table string
		id    string
	}{
		{name: "user", table: "users", id: "u1"},
		{name: "role", table: "roles", id: "r-u1"},
		{name: "action permission", table: "permissions", id: "p-action"},
		{name: "resource permission", table: "permissions", id: "p-resource"},
		{name: "host account", table: "host_accounts", id: "target-root"},
		{name: "parent host", table: "hosts", id: "host-target-root"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			seedRBAC(t, db, "u1", []model.Permission{
				{ID: "p-action", Action: "session:connect", Effect: model.PermissionEffectAllow},
				{ID: "p-resource", ResourceType: model.ResourceTypeHostAccount, ResourceID: "target-root", Effect: model.PermissionEffectAllow},
			})
			seedActiveHostAccount(t, db, "target-root")

			assertPermission(t, NewChecker(db), "u1", "session:connect", model.ResourceTypeHostAccount, "target-root", true)
			tombstoneRBACRecord(t, db, test.table, test.id)
			assertPermission(t, NewChecker(db), "u1", "session:connect", model.ResourceTypeHostAccount, "target-root", false)
		})
	}
}

func TestCheckerRejectsInactiveResourceGroup(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "p-action", Action: "session:connect", Effect: model.PermissionEffectAllow},
		{ID: "p-group", ResourceType: model.ResourceTypeGroup, ResourceID: "rg-prod", Effect: model.PermissionEffectAllow},
	})
	for _, value := range []any{
		&model.ResourceGroup{ID: "rg-prod", Name: "prod", GroupType: model.ResourceGroupTypeResource},
		&model.Host{ID: "host-prod", Name: "host-prod", Address: "10.0.0.1", Port: 22, GroupName: "prod", Status: "active"},
		&model.HostAccount{ID: "account-prod", HostID: "host-prod", Username: "root", Status: "active"},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("create %T: %v", value, err)
		}
	}

	checker := NewChecker(db)
	assertPermission(t, checker, "u1", "session:connect", model.ResourceTypeHostAccount, "account-prod", true)
	tombstoneRBACRecord(t, db, "resource_groups", "rg-prod")
	assertPermission(t, checker, "u1", "session:connect", model.ResourceTypeHostAccount, "account-prod", false)
}

func TestBatchActionDecisionsRejectInactiveTargetOrParent(t *testing.T) {
	tests := []struct {
		name  string
		table string
		id    string
	}{
		{name: "target", table: "host_accounts", id: "target-root"},
		{name: "parent", table: "hosts", id: "host-target-root"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			seedRBAC(t, db, "u1", []model.Permission{
				{ID: "p-action", Action: "session:connect", Effect: model.PermissionEffectAllow},
			})
			seedActiveHostAccount(t, db, "target-root")

			requests := []BatchAuthorizationRequest{{
				ResourceType: model.ResourceTypeHostAccount,
				ResourceID:   "target-root",
				Actions:      []string{"session:connect"},
			}}
			check := func(wantAllowed, wantActionAllowed bool) {
				t.Helper()
				got, err := NewChecker(db).BatchActionDecisionsContext(context.Background(), "u1", requests)
				if err != nil {
					t.Fatalf("batch action check: %v", err)
				}
				if len(got) != 1 ||
					got[0].Allowed != wantAllowed ||
					got[0].ActionAllowed != wantActionAllowed {
					t.Fatalf(
						"batch action check = %#v, want allowed=%v actionAllowed=%v",
						got,
						wantAllowed,
						wantActionAllowed,
					)
				}
			}
			check(true, true)
			tombstoneRBACRecord(t, db, test.table, test.id)
			check(false, true)
		})
	}
}

func TestResourceGrantCheckerRejectsInactiveGrantSources(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		tombTable string
		tombID    string
	}{
		{name: "direct grant", source: "direct", tombTable: "resource_grants", tombID: "direct-grant"},
		{name: "direct principal", source: "direct", tombTable: "users", tombID: "u1"},
		{name: "user group", source: "group", tombTable: "user_groups", tombID: "ug1"},
		{name: "user group grant", source: "group", tombTable: "resource_grants", tombID: "group-grant"},
		{name: "temporary account", source: "temporary", tombTable: "temporary_accounts", tombID: "ta1"},
		{name: "temporary grant", source: "temporary", tombTable: "temporary_account_grants", tombID: "tag1"},
		{name: "resource group", source: "resource_group", tombTable: "resource_groups", tombID: "rg-prod"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			for _, value := range []any{
				&model.User{ID: "u1", Username: "u1", Status: "active"},
				&model.Host{ID: "host-prod", Name: "host-prod", Address: "10.0.0.1", Port: 22, GroupName: "prod", Status: "active"},
				&model.HostAccount{ID: "account-prod", HostID: "host-prod", Username: "root", Status: "active"},
			} {
				if err := db.Create(value).Error; err != nil {
					t.Fatalf("create %T: %v", value, err)
				}
			}

			switch test.source {
			case "direct":
				if err := db.Create(&model.ResourceGrant{
					ID: "direct-grant", PrincipalType: "user", PrincipalID: "u1",
					ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-prod", Effect: model.PermissionEffectAllow,
				}).Error; err != nil {
					t.Fatalf("create direct grant: %v", err)
				}
			case "group":
				for _, value := range []any{
					&model.UserGroup{ID: "ug1", Name: "operators"},
					&model.UserGroupMember{ID: "ugm1", GroupID: "ug1", UserID: "u1"},
					&model.ResourceGrant{
						ID: "group-grant", PrincipalType: "user_group", PrincipalID: "ug1",
						ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-prod", Effect: model.PermissionEffectAllow,
					},
				} {
					if err := db.Create(value).Error; err != nil {
						t.Fatalf("create %T: %v", value, err)
					}
				}
			case "temporary":
				for _, value := range []any{
					&model.TemporaryAccount{ID: "ta1", SessionID: "session-1", Type: model.TemporaryAccountTypeUser, Username: "temporary", Status: "active"},
					&model.TemporaryAccountGrant{ID: "tag1", TemporaryAccountID: "ta1", UserID: "u1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-prod"},
				} {
					if err := db.Create(value).Error; err != nil {
						t.Fatalf("create %T: %v", value, err)
					}
				}
			case "resource_group":
				for _, value := range []any{
					&model.ResourceGroup{ID: "rg-prod", Name: "prod", GroupType: model.ResourceGroupTypeResource},
					&model.ResourceGrant{
						ID: "resource-group-grant", PrincipalType: "user", PrincipalID: "u1",
						ResourceType: model.ResourceTypeGroup, ResourceID: "rg-prod", Effect: model.PermissionEffectAllow,
					},
				} {
					if err := db.Create(value).Error; err != nil {
						t.Fatalf("create %T: %v", value, err)
					}
				}
			}

			assertResourceGrant(t, db, model.ResourceTypeHostAccount, "account-prod", true)
			tombstoneRBACRecord(t, db, test.tombTable, test.tombID)
			assertResourceGrant(t, db, model.ResourceTypeHostAccount, "account-prod", false)
		})
	}
}

func TestResourceGrantCheckerRejectsInactiveAccountOrParent(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		resourceID   string
		grantID      string
		tombTable    string
		tombID       string
		values       func() []any
	}{
		{
			name: "host account", resourceType: model.ResourceTypeHostAccount, resourceID: "ha1", grantID: "grant-ha",
			tombTable: "host_accounts", tombID: "ha1",
			values: func() []any {
				return []any{
					&model.Host{ID: "h1", Name: "h1", Address: "10.0.0.1", Port: 22, Status: "active"},
					&model.HostAccount{ID: "ha1", HostID: "h1", Username: "root", Status: "active"},
				}
			},
		},
		{
			name: "host parent", resourceType: model.ResourceTypeHostAccount, resourceID: "ha1", grantID: "grant-ha",
			tombTable: "hosts", tombID: "h1",
			values: func() []any {
				return []any{
					&model.Host{ID: "h1", Name: "h1", Address: "10.0.0.1", Port: 22, Status: "active"},
					&model.HostAccount{ID: "ha1", HostID: "h1", Username: "root", Status: "active"},
				}
			},
		},
		{
			name: "database account", resourceType: model.ResourceTypeDatabaseAccount, resourceID: "da1", grantID: "grant-da",
			tombTable: "database_accounts", tombID: "da1",
			values: func() []any {
				return []any{
					&model.DatabaseInstance{ID: "db1", Name: "db1", Protocol: "mysql", Address: "10.0.0.2", Port: 3306, Status: "active"},
					&model.DatabaseAccount{ID: "da1", InstanceID: "db1", UniqueName: "app", Username: "app", Status: "active"},
				}
			},
		},
		{
			name: "database parent", resourceType: model.ResourceTypeDatabaseAccount, resourceID: "da1", grantID: "grant-da",
			tombTable: "database_instances", tombID: "db1",
			values: func() []any {
				return []any{
					&model.DatabaseInstance{ID: "db1", Name: "db1", Protocol: "mysql", Address: "10.0.0.2", Port: 3306, Status: "active"},
					&model.DatabaseAccount{ID: "da1", InstanceID: "db1", UniqueName: "app", Username: "app", Status: "active"},
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			if err := db.Create(&model.User{ID: "u1", Username: "u1", Status: "active"}).Error; err != nil {
				t.Fatalf("create user: %v", err)
			}
			for _, value := range test.values() {
				if err := db.Create(value).Error; err != nil {
					t.Fatalf("create %T: %v", value, err)
				}
			}
			if err := db.Create(&model.ResourceGrant{
				ID: test.grantID, PrincipalType: "user", PrincipalID: "u1",
				ResourceType: test.resourceType, ResourceID: test.resourceID, Effect: model.PermissionEffectAllow,
			}).Error; err != nil {
				t.Fatalf("create resource grant: %v", err)
			}

			assertResourceGrant(t, db, test.resourceType, test.resourceID, true)
			tombstoneRBACRecord(t, db, test.tombTable, test.tombID)
			assertResourceGrant(t, db, test.resourceType, test.resourceID, false)
		})
	}
}

func assertResourceGrant(t *testing.T, db *gorm.DB, resourceType, resourceID string, want bool) {
	t.Helper()

	got, err := NewResourceGrantChecker(db).HasGrant("u1", resourceType, resourceID)
	if err != nil {
		t.Fatalf("single grant check: %v", err)
	}
	if got != want {
		t.Fatalf("single grant check = %v, want %v", got, want)
	}
	batch, err := NewResourceGrantChecker(db).BatchGrantsContext(context.Background(), "u1", []BatchAuthorizationRequest{{
		ResourceType: resourceType,
		ResourceID:   resourceID,
	}})
	if err != nil {
		t.Fatalf("batch grant check: %v", err)
	}
	if len(batch) != 1 || batch[0] != want {
		t.Fatalf("batch grant check = %#v, want [%v]", batch, want)
	}
}

func tombstoneRBACRecord(t *testing.T, db *gorm.DB, table, id string) {
	t.Helper()

	result := db.Table(table).Where("id = ?", id).Update("active_marker", nil)
	if result.Error != nil {
		t.Fatalf("tombstone %s/%s: %v", table, id, result.Error)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("tombstone %s/%s affected %d rows, want 1", table, id, result.RowsAffected)
	}
}
