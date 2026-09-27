package rbac

import (
	"context"
	"fmt"
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
	for index := 0; index < 101; index++ {
		id := fmt.Sprintf("ha-%03d", index)
		if err := db.Create(&model.HostAccount{ID: id, HostID: "h1", Username: id, ResourceID: fmt.Sprintf("%04d", index), Status: "active"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.ResourceGrant{ID: "deny-many", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "ha-100", Effect: model.PermissionEffectDeny}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Host{ID: "h-none", Name: "h-none", Address: "127.0.0.2", Port: 22}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HostAccount{ID: "ha-none", HostID: "h-none", Username: "none", ResourceID: "none", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	checker := NewResourceGrantChecker(db)
	small := []BatchAuthorizationRequest{{ResourceType: model.ResourceTypeHostAccount, ResourceID: "ha-000"}}
	large := make([]BatchAuthorizationRequest, 0, 102)
	for index := 0; index < 101; index++ {
		large = append(large, BatchAuthorizationRequest{ResourceType: model.ResourceTypeHostAccount, ResourceID: fmt.Sprintf("ha-%03d", index)})
	}
	large = append(large, BatchAuthorizationRequest{ResourceType: model.ResourceTypeHostAccount, ResourceID: "ha-none"})
	countQueries := func(requests []BatchAuthorizationRequest) ([]bool, int) {
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
	if !gotSmall[0] || !gotLarge[99] {
		t.Fatal("parent/group grant was not inherited")
	}
	if gotLarge[100] {
		t.Fatal("explicit deny did not override allow")
	}
	if gotLarge[101] {
		t.Fatal("resource without a grant was allowed")
	}
	if largeQueries > smallQueries+1 {
		t.Fatalf("batch authorization SQL grew with resources: small=%d large=%d", smallQueries, largeQueries)
	}
}

func TestBatchGrantResourceAndAccountGroupsDoNotCrossPlatformAccount(t *testing.T) {
	db := newTestDB(t)
	for _, value := range []any{
		&model.User{ID: "u1", Username: "u1"},
		&model.PlatformAccount{ID: "p1", Name: "p1", PlatformName: "git", GroupName: "shared", Username: "root", OwnerID: "u1", Status: "active"},
		&model.ResourceGroup{ID: "resource-shared", Name: "shared", GroupType: model.ResourceGroupTypeResource},
		&model.ResourceGroup{ID: "account-shared", Name: "shared", GroupType: model.ResourceGroupTypeAccount},
		&model.ResourceGrant{ID: "resource-grant", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeGroup, ResourceID: "resource-shared", Effect: model.PermissionEffectAllow},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	request := []BatchAuthorizationRequest{{ResourceType: model.ResourceTypePlatformAccount, ResourceID: "p1"}}
	grants, err := NewResourceGrantChecker(db).BatchGrantsContext(context.Background(), "u1", request)
	if err != nil {
		t.Fatal(err)
	}
	if grants[0] {
		t.Fatal("resource group incorrectly granted platform account")
	}
	if err := db.Create(&model.ResourceGrant{ID: "account-grant", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeAccountGroup, ResourceID: "account-shared", Effect: model.PermissionEffectAllow}).Error; err != nil {
		t.Fatal(err)
	}
	grants, err = NewResourceGrantChecker(db).BatchGrantsContext(context.Background(), "u1", request)
	if err != nil {
		t.Fatal(err)
	}
	if !grants[0] {
		t.Fatal("account group did not grant platform account")
	}
	seedRBAC(t, db, "u2", []model.Permission{{ID: "platform-view", Action: "platform:view", Effect: model.PermissionEffectAllow}, {ID: "platform-resource", Action: "platform:view", ResourceType: model.ResourceTypeGroup, ResourceID: "resource-shared", Effect: model.PermissionEffectAllow}})
	actions, err := NewChecker(db).BatchActionDecisionsContext(context.Background(), "u2", []BatchAuthorizationRequest{{ResourceType: model.ResourceTypePlatformAccount, ResourceID: "p1", Actions: []string{"platform:view"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !actions[0].Allowed {
		t.Fatal("resource-group action permission no longer matches platform account")
	}
}

func TestBatchGrantsSeparatelyCoverParentResourceAndAccountGroups(t *testing.T) {
	db := newTestDB(t)
	values := []any{
		&model.User{ID: "u1", Username: "u1"},
		&model.Host{ID: "parent-host", Name: "parent", Address: "10.0.0.1", Port: 22},
		&model.Host{ID: "resource-host", Name: "resource", Address: "10.0.0.2", Port: 22, GroupName: "prod"},
		&model.Host{ID: "account-host", Name: "account", Address: "10.0.0.3", Port: 22},
		&model.HostAccount{ID: "parent-account", HostID: "parent-host", Username: "p", ResourceID: "p001", Status: "active"},
		&model.HostAccount{ID: "resource-account", HostID: "resource-host", Username: "r", ResourceID: "r001", Status: "active"},
		&model.HostAccount{ID: "account-account", HostID: "account-host", Username: "a", ResourceID: "a001", GroupName: "ops", Status: "active"},
		&model.ResourceGroup{ID: "resource-group", Name: "prod", GroupType: model.ResourceGroupTypeResource},
		&model.ResourceGroup{ID: "account-group", Name: "ops", GroupType: model.ResourceGroupTypeAccount},
		&model.ResourceGrant{ID: "parent-grant", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "parent-host", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "resource-grant", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeGroup, ResourceID: "resource-group", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "account-grant", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeAccountGroup, ResourceID: "account-group", Effect: model.PermissionEffectAllow},
	}
	for _, value := range values {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	requests := []BatchAuthorizationRequest{{ResourceType: model.ResourceTypeHostAccount, ResourceID: "parent-account"}, {ResourceType: model.ResourceTypeHostAccount, ResourceID: "resource-account"}, {ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-account"}}
	got, err := NewResourceGrantChecker(db).BatchGrantsContext(context.Background(), "u1", requests)
	if err != nil {
		t.Fatal(err)
	}
	for index, allowed := range got {
		if !allowed {
			t.Fatalf("separate grant path %d was denied: %#v", index, got)
		}
	}
}

func TestBatchGrantsLoadDirectUserGroupAndTemporarySources(t *testing.T) {
	db := newTestDB(t)
	values := []any{
		&model.User{ID: "u1", Username: "u1"},
		&model.Host{ID: "h1", Name: "h1", Address: "10.0.1.1", Port: 22},
		&model.HostAccount{ID: "direct", HostID: "h1", Username: "direct", ResourceID: "d001", Status: "active"},
		&model.HostAccount{ID: "group", HostID: "h1", Username: "group", ResourceID: "g001", Status: "active"},
		&model.HostAccount{ID: "temporary", HostID: "h1", Username: "temporary", ResourceID: "t001", Status: "active"},
		&model.UserGroup{ID: "ug1", Name: "operators"}, &model.UserGroupMember{ID: "ugm1", GroupID: "ug1", UserID: "u1"},
		&model.ResourceGrant{ID: "direct-grant", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "direct", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "group-grant", PrincipalType: "user_group", PrincipalID: "ug1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "group", Effect: model.PermissionEffectAllow},
		&model.TemporaryAccount{ID: "ta1", SessionID: "session-1", Type: model.TemporaryAccountTypeUser, Username: "temporary-1", Status: "active"},
		&model.TemporaryAccountGrant{ID: "tag1", TemporaryAccountID: "ta1", UserID: "u1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "temporary"},
	}
	for _, value := range values {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	requests := []BatchAuthorizationRequest{{ResourceType: model.ResourceTypeHostAccount, ResourceID: "direct"}, {ResourceType: model.ResourceTypeHostAccount, ResourceID: "group"}, {ResourceType: model.ResourceTypeHostAccount, ResourceID: "temporary"}}
	got, err := NewResourceGrantChecker(db).BatchGrantsContext(context.Background(), "u1", requests)
	if err != nil {
		t.Fatal(err)
	}
	for index, allowed := range got {
		if !allowed {
			t.Fatalf("grant source %d did not authorize: %#v", index, got)
		}
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
	if !got[0].Allowed || !got[1].Denied {
		t.Fatalf("unexpected decisions: %#v", got)
	}
}

func TestBatchActionDecisionsAreIndexAlignedAndBoundedForHundredResources(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "view", Action: "host:view", Effect: model.PermissionEffectAllow},
		{ID: "delete", Action: "host:delete", Effect: model.PermissionEffectAllow},
		{ID: "deny-delete", Action: "host:delete", ResourceType: model.ResourceTypeHost, ResourceID: "h-000", Effect: model.PermissionEffectDeny},
	})
	for index := 0; index < 100; index++ {
		id := fmt.Sprintf("h-%03d", index)
		if err := db.Create(&model.Host{ID: id, Name: id, Address: fmt.Sprintf("10.0.0.%d", index+1), Port: 22}).Error; err != nil {
			t.Fatal(err)
		}
	}
	checker := NewChecker(db)
	count := func(requests []BatchAuthorizationRequest) ([]BatchActionDecision, int) {
		queries := 0
		name := "batch_action_count"
		if err := db.Callback().Query().Before("gorm:query").Register(name, func(*gorm.DB) { queries++ }); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Query().Remove(name)
		got, err := checker.BatchActionDecisionsContext(context.Background(), "u1", requests)
		if err != nil {
			t.Fatal(err)
		}
		return got, queries
	}
	small := []BatchAuthorizationRequest{{ResourceType: model.ResourceTypeHost, ResourceID: "h-000", Actions: []string{"host:view"}}}
	large := make([]BatchAuthorizationRequest, 0, 101)
	large = append(large, BatchAuthorizationRequest{ResourceType: model.ResourceTypeHost, ResourceID: "h-000", Actions: []string{"host:view"}})
	large = append(large, BatchAuthorizationRequest{ResourceType: model.ResourceTypeHost, ResourceID: "h-000", Actions: []string{"host:delete"}})
	for index := 1; index < 100; index++ {
		large = append(large, BatchAuthorizationRequest{ResourceType: model.ResourceTypeHost, ResourceID: fmt.Sprintf("h-%03d", index), Actions: []string{"host:view"}})
	}
	_, smallQueries := count(small)
	got, largeQueries := count(large)
	if !got[0].Allowed || got[1].Allowed || !got[1].Denied {
		t.Fatalf("same-resource action decisions leaked: %#v", got[:2])
	}
	if largeQueries > smallQueries+1 {
		t.Fatalf("batch action SQL grew with distinct resource IDs: small=%d large=%d", smallQueries, largeQueries)
	}
}

func TestBatchPermissionGroupFactsMatchLegacyCheckerSemantics(t *testing.T) {
	db := newTestDB(t)
	for _, value := range []any{
		&model.User{ID: "owner", Username: "owner"},
		&model.ResourceGroup{ID: "resource-shared", Name: "shared", GroupType: model.ResourceGroupTypeResource},
		&model.ResourceGroup{ID: "account-shared", Name: "shared", GroupType: model.ResourceGroupTypeAccount},
		&model.Host{ID: "host-shared", Name: "host", Address: "10.10.0.1", Port: 22, GroupName: "shared"},
		&model.HostAccount{ID: "host-account-shared", HostID: "host-shared", Username: "root", ResourceID: "ha-shared", Status: "active"},
		&model.DatabaseInstance{ID: "db-shared", Name: "db", Protocol: "mysql", Address: "10.10.0.2", Port: 3306, GroupName: "shared"},
		&model.DatabaseAccount{ID: "db-account-shared", InstanceID: "db-shared", UniqueName: "app", Username: "app", ResourceID: "da-shared", Status: "active"},
		&model.Application{ID: "app-shared", Name: "app", AppGroup: "shared", ListenPort: 18081, InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8081, Status: "active"},
		&model.PlatformAccount{ID: "platform-shared", Name: "platform", PlatformName: "git", GroupName: "shared", Username: "admin", OwnerID: "owner", Status: "active"},
		&model.ContainerEndpoint{ID: "container-shared", Name: "container", Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI, Address: "unix:///var/run/docker.sock", GroupName: "shared", Status: "active"},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("create %T: %v", value, err)
		}
	}

	requests := []BatchAuthorizationRequest{
		{ResourceType: model.ResourceTypeHost, ResourceID: "host-shared"},
		{ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-shared"},
		{ResourceType: model.ResourceTypeDatabaseInstance, ResourceID: "db-shared"},
		{ResourceType: model.ResourceTypeDatabaseAccount, ResourceID: "db-account-shared"},
		{ResourceType: model.ResourceTypeApplication, ResourceID: "app-shared"},
		{ResourceType: model.ResourceTypePlatformAccount, ResourceID: "platform-shared"},
		{ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: "container-shared"},
	}

	for _, test := range []struct {
		name    string
		userID  string
		groupID string
		action  string
	}{
		{name: "permission references account-group id", userID: "u-account-group", groupID: "account-shared", action: "resource:view-account"},
		{name: "permission references same-name resource-group id", userID: "u-resource-group", groupID: "resource-shared", action: "resource:view-resource"},
	} {
		t.Run(test.name, func(t *testing.T) {
			seedRBAC(t, db, test.userID, []model.Permission{
				{ID: "allow-" + test.userID, Action: test.action, Effect: model.PermissionEffectAllow},
				{
					ID:           "deny-" + test.userID,
					Action:       test.action,
					ResourceType: model.ResourceTypeGroup,
					ResourceID:   test.groupID,
					Effect:       model.PermissionEffectDeny,
				},
			})
			batchRequests := append([]BatchAuthorizationRequest(nil), requests...)
			for index := range batchRequests {
				batchRequests[index].Actions = []string{test.action}
			}
			checker := NewChecker(db)
			got, err := checker.BatchActionDecisionsContext(context.Background(), test.userID, batchRequests)
			if err != nil {
				t.Fatal(err)
			}
			for index, request := range batchRequests {
				globalAllowed, err := checker.HasPermissionContext(context.Background(), test.userID, test.action, "", "")
				if err != nil {
					t.Fatal(err)
				}
				resourceDenied, err := checker.HasDenyContext(context.Background(), test.userID, test.action, request.ResourceType, request.ResourceID)
				if err != nil {
					t.Fatal(err)
				}
				want := BatchActionDecision{
					ActionAllowed: globalAllowed,
					Allowed:       globalAllowed && !resourceDenied,
					Denied:        globalAllowed && resourceDenied,
				}
				if got[index] != want {
					t.Fatalf("%s/%s batch=%#v single-derived=%#v", request.ResourceType, request.ResourceID, got[index], want)
				}
				if request.ResourceType == model.ResourceTypeContainerEndpoint && resourceDenied {
					t.Fatal("legacy Permission group matching unexpectedly included container endpoint")
				}
				if request.ResourceType != model.ResourceTypeContainerEndpoint && !resourceDenied {
					t.Fatalf("legacy Permission group matching missed %s", request.ResourceType)
				}
			}
		})
	}
}

func TestBatchActionOnlyDenyAndAlternativeActionsMatchSingleChecks(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "allow-view", Action: "resource:view", Effect: model.PermissionEffectAllow},
		{ID: "deny-view", Action: "resource:view", Effect: model.PermissionEffectDeny},
		{ID: "allow-connect", Action: "resource:connect", Effect: model.PermissionEffectAllow},
		{ID: "deny-connect-h1", Action: "resource:connect", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectDeny},
	})
	for _, id := range []string{"h1", "h2"} {
		if err := db.Create(&model.Host{ID: id, Name: id, Address: id, Port: 22}).Error; err != nil {
			t.Fatal(err)
		}
	}
	requests := []BatchAuthorizationRequest{
		{ResourceType: model.ResourceTypeHost, ResourceID: "h1", Actions: []string{"resource:view"}},
		{ResourceType: model.ResourceTypeHost, ResourceID: "h1", Actions: []string{"resource:view", "resource:connect"}},
		{ResourceType: model.ResourceTypeHost, ResourceID: "h2", Actions: []string{"resource:view", "resource:connect"}},
		{ResourceType: model.ResourceTypeHost, ResourceID: "h2", Actions: []string{"resource:view", "missing"}},
	}
	checker := NewChecker(db)
	got, err := checker.BatchActionDecisionsContext(context.Background(), "u1", requests)
	if err != nil {
		t.Fatal(err)
	}
	for index, request := range requests {
		want := BatchActionDecision{}
		for _, action := range normalizedBatchActions(request.Actions) {
			globalAllowed, err := checker.HasPermissionContext(context.Background(), "u1", action, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if !globalAllowed {
				continue
			}
			want.ActionAllowed = true
			resourceDenied, err := checker.HasDenyContext(context.Background(), "u1", action, request.ResourceType, request.ResourceID)
			if err != nil {
				t.Fatal(err)
			}
			if resourceDenied {
				want.Denied = true
				continue
			}
			want.Allowed = true
		}
		if want.Allowed {
			want.Denied = false
		}
		if got[index] != want {
			t.Fatalf("request[%d]=%#v batch=%#v single-derived=%#v", index, request, got[index], want)
		}
	}
	if got[0].ActionAllowed {
		t.Fatalf("action-only deny did not override allow: %#v", got[0])
	}
	if !got[1].ActionAllowed || got[1].Allowed || !got[1].Denied {
		t.Fatalf("resource deny classification was lost: %#v", got[1])
	}
	if !got[2].Allowed {
		t.Fatalf("allowed alternative action was rejected: %#v", got[2])
	}
}
