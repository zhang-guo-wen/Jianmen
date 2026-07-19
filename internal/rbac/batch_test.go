package rbac

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func TestBatchGrantDecisionsUseBoundedSQLAndHonorInheritanceAndDeny(t *testing.T) {
	db := newTestDB(t)
	models := []any{
		&model.User{ID: "u1", Username: "u1"},
		&model.Host{ID: "h1", Name: "h1", Address: "127.0.0.1", Port: 22, GroupName: "prod"},
		&model.ResourceGroup{ID: "rg", Name: "prod", GroupType: model.ResourceGroupTypeResource},
		&model.ResourceGrant{ID: "parent", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "group", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeGroup, ResourceID: "rg", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "deny", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "ha-deny", Effect: model.PermissionEffectDeny},
	}
	for _, value := range models {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, id := range []string{"ha-1", "ha-2", "ha-deny", "ha-none"} {
		if err := db.Create(&model.HostAccount{ID: id, HostID: "h1", Username: id, ResourceID: string(rune('a' + index)), Status: "active"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	checker := NewResourceGrantChecker(db)
	small := []BatchAuthorizationRequest{{ResourceType: model.ResourceTypeHostAccount, ResourceID: "ha-1"}}
	large := make([]BatchAuthorizationRequest, 0, 64)
	for index := 0; index < 64; index++ {
		id := "ha-1"
		if index%4 == 1 {
			id = "ha-2"
		}
		if index%4 == 2 {
			id = "ha-deny"
		}
		if index%4 == 3 {
			id = "ha-none"
		}
		large = append(large, BatchAuthorizationRequest{ResourceType: model.ResourceTypeHostAccount, ResourceID: id})
	}
	countQueries := func(requests []BatchAuthorizationRequest) (map[string]bool, int) {
		count := 0
		callback := "batch_count"
		if err := db.Callback().Query().Before("gorm:query").Register(callback, func(*gorm.DB) { count++ }); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Query().Remove(callback)
		got, err := checker.BatchGrantsContext(context.Background(), "u1", requests)
		if err != nil {
			t.Fatal(err)
		}
		return got, count
	}
	gotSmall, smallQueries := countQueries(small)
	gotLarge, largeQueries := countQueries(large)
	if !gotSmall[BatchResourceKey(model.ResourceTypeHostAccount, "ha-1")] || !gotLarge[BatchResourceKey(model.ResourceTypeHostAccount, "ha-2")] {
		t.Fatal("parent/group grant was not inherited")
	}
	if gotLarge[BatchResourceKey(model.ResourceTypeHostAccount, "ha-deny")] {
		t.Fatal("explicit deny did not override allow")
	}
	if largeQueries > smallQueries+1 {
		t.Fatalf("batch authorization SQL grew with resources: small=%d large=%d", smallQueries, largeQueries)
	}
}

func TestBatchActionDecisionsHonorResourceDenyWithoutPerResourceSQL(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{{ID: "view", Action: "host:view", Effect: model.PermissionEffectAllow}, {ID: "deny", Action: "host:view", ResourceType: model.ResourceTypeHost, ResourceID: "h2", Effect: model.PermissionEffectDeny}})
	for _, id := range []string{"h1", "h2", "h3"} {
		if err := db.Create(&model.Host{ID: id, Name: id, Address: id, Port: 22}).Error; err != nil {
			t.Fatal(err)
		}
	}
	requests := []BatchAuthorizationRequest{{ResourceType: model.ResourceTypeHost, ResourceID: "h1", Actions: []string{"host:view"}}, {ResourceType: model.ResourceTypeHost, ResourceID: "h2", Actions: []string{"host:view"}}, {ResourceType: model.ResourceTypeHost, ResourceID: "h3", Actions: []string{"host:view"}}}
	got, err := NewChecker(db).BatchActionDecisionsContext(context.Background(), "u1", requests)
	if err != nil {
		t.Fatal(err)
	}
	if !got[BatchResourceKey(model.ResourceTypeHost, "h1")].Allowed || !got[BatchResourceKey(model.ResourceTypeHost, "h2")].Denied {
		t.Fatalf("unexpected decisions: %#v", got)
	}
}
