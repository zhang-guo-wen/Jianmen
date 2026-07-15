package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func TestVisibleResourcesUseContainerAndAccountGrants(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAccessTestData(t, db)
	request := asTestUser(httptest.NewRequest(http.MethodGet, "/api/hosts", nil), "u1", "alice")

	hosts, err := server.visibleHosts(request, server.store.Hosts())
	if err != nil {
		t.Fatalf("visibleHosts: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("visible hosts = %d, want 2", len(hosts))
	}

	targets, err := server.visibleTargets(request, server.store.Targets())
	if err != nil {
		t.Fatalf("visibleTargets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("visible targets = %d, want 2", len(targets))
	}
	for _, target := range targets {
		if target.ID == "ha-hidden" {
			t.Fatal("account without grant was visible")
		}
	}

	instances, err := server.visibleDatabaseInstances(request, server.store.DatabaseInstances())
	if err != nil {
		t.Fatalf("visibleDatabaseInstances: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("visible database instances = %d, want 2: %#v", len(instances), instances)
	}
	for _, instance := range instances {
		if instance.ID == "db-account-only" && instance.CanManage {
			t.Fatal("account-only database grant incorrectly provided instance management access")
		}
	}
}

func TestAccountGrantAllowsViewingButNotContainerManagement(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAccessTestData(t, db)
	request := asTestUser(httptest.NewRequest(http.MethodPut, "/api/targets/ha-account", nil), "u1", "alice")

	targets, err := server.visibleTargets(request, server.store.Targets())
	if err != nil {
		t.Fatalf("visibleTargets: %v", err)
	}
	var accountOnly *store.TargetView
	for index := range targets {
		if targets[index].ID == "ha-account" {
			accountOnly = &targets[index]
		}
	}
	if accountOnly == nil {
		t.Fatal("directly granted host account was not visible")
	}
	if accountOnly.CanManage {
		t.Fatal("direct host account grant incorrectly provided management access")
	}
	recorder := httptest.NewRecorder()
	if server.requireHostAccountManagement(recorder, request, accountOnly.ID) {
		t.Fatal("direct host account grant was accepted as host management authorization")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("host account management status = %d, want 403", recorder.Code)
	}

	dbRequest := asTestUser(httptest.NewRequest(http.MethodPut, "/api/db/accounts/dba-account-only", nil), "u1", "alice")
	allDBAccounts, err := server.store.InstanceAccounts("db-account-only")
	if err != nil {
		t.Fatalf("InstanceAccounts: %v", err)
	}
	dbAccounts, err := server.visibleDatabaseAccounts(dbRequest, allDBAccounts)
	if err != nil {
		t.Fatalf("visibleDatabaseAccounts: %v", err)
	}
	if len(dbAccounts) != 1 || dbAccounts[0].CanManage {
		t.Fatalf("unexpected account-only database visibility: %#v", dbAccounts)
	}
	dbRecorder := httptest.NewRecorder()
	if server.requireDatabaseAccountManagement(dbRecorder, dbRequest, "dba-account-only") {
		t.Fatal("direct database account grant was accepted as instance management authorization")
	}
	if dbRecorder.Code != http.StatusForbidden {
		t.Fatalf("database account management status = %d, want 403", dbRecorder.Code)
	}
}

func TestApplicationsAreFilteredByResourceGrant(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "app-user", rbac.ActionAppView)
	server.rbacChecker = rbac.NewChecker(db)
	applications := []model.Application{
		{ID: "app-visible", Name: "visible", InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080, ListenPort: 47120, Status: "active"},
		{ID: "app-hidden", Name: "hidden", InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8081, ListenPort: 47121, Status: "active"},
	}
	if err := db.Create(&applications).Error; err != nil {
		t.Fatalf("create applications: %v", err)
	}
	grant := model.ResourceGrant{PrincipalType: "user", PrincipalID: "app-user", ResourceType: model.ResourceTypeApplication, ResourceID: "app-visible", Effect: model.PermissionEffectAllow}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatalf("create application grant: %v", err)
	}
	request := withTestUser(httptest.NewRequest(http.MethodGet, "/api/applications", nil), "app-user", "app-user")
	recorder := httptest.NewRecorder()
	server.handleApplications(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list applications status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var page struct {
		Items []store.ApplicationView `json:"items"`
		Total int                     `json:"total"`
	}
	if err := decodeTestData(t, recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode applications: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "app-visible" || !page.Items[0].CanManage {
		t.Fatalf("unexpected visible applications: %#v", page)
	}
}

func TestCreateApplicationAutomaticallyGrantsCreator(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "app-creator", Username: "creator", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	body := bytes.NewBufferString(`{"name":"created-app","scheme":"http","host":"127.0.0.1","port":8080,"listen_port":47130}`)
	request := asTestUser(httptest.NewRequest(http.MethodPost, "/api/applications", body), "app-creator", "creator")
	recorder := httptest.NewRecorder()
	server.handleCreateApplication(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create application status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var created store.ApplicationView
	if err := decodeTestData(t, recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode application: %v", err)
	}
	var count int64
	db.Model(&model.ResourceGrant{}).Where("principal_id = ? AND resource_type = ? AND resource_id = ?", "app-creator", model.ResourceTypeApplication, created.ID).Count(&count)
	if count != 1 {
		t.Fatalf("creator application grant count = %d, want 1", count)
	}
}
func TestCreateHostAutomaticallyGrantsCreator(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "u1", Username: "alice", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	body := bytes.NewBufferString(`{"name":"created","address":"10.0.0.9","port":22}`)
	request := asTestUser(httptest.NewRequest(http.MethodPost, "/api/hosts", body), "u1", "alice")
	recorder := httptest.NewRecorder()

	server.handleCreateHost(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create host status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var created store.HostView
	if err := decodeTestData(t, recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode host: %v", err)
	}
	var count int64
	db.Model(&model.ResourceGrant{}).
		Where("principal_type = ? AND principal_id = ? AND resource_type = ? AND resource_id = ? AND effect = ?", "user", "u1", model.ResourceTypeHost, created.ID, model.PermissionEffectAllow).
		Count(&count)
	if count != 1 {
		var grants []model.ResourceGrant
		db.Find(&grants)
		t.Fatalf("creator grant count = %d, want 1; created=%#v grants=%#v", count, created, grants)
	}
}

func TestCreateResourceGrantRejectsResourceUserCannotDelegate(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAccessTestData(t, db)

	allowedBody := bytes.NewBufferString(`{"principal_type":"user","principal_id":"u2","resource_type":"host","resource_id":"host-container","effect":"allow"}`)
	allowedRequest := asTestUser(httptest.NewRequest(http.MethodPost, "/api/resource-grants", allowedBody), "u1", "alice")
	allowedRecorder := httptest.NewRecorder()
	server.createResourceGrant(allowedRecorder, allowedRequest)
	if allowedRecorder.Code != http.StatusCreated {
		t.Fatalf("delegate owned host status = %d, body=%s", allowedRecorder.Code, allowedRecorder.Body.String())
	}

	deniedBody := bytes.NewBufferString(`{"principal_type":"user","principal_id":"u2","resource_type":"host","resource_id":"host-hidden","effect":"allow"}`)
	deniedRequest := asTestUser(httptest.NewRequest(http.MethodPost, "/api/resource-grants", deniedBody), "u1", "alice")
	deniedRecorder := httptest.NewRecorder()
	server.createResourceGrant(deniedRecorder, deniedRequest)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("delegate hidden host status = %d, want 403; body=%s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
}

func seedResourceAccessTestData(t *testing.T, db *gorm.DB) {
	t.Helper()
	items := []any{
		&model.User{ID: "u1", Username: "alice", Status: "active"},
		&model.User{ID: "u2", Username: "bob", Status: "active"},
		&model.Host{ID: "host-container", Name: "container", Address: "10.0.0.1", Port: 22},
		&model.Host{ID: "host-account", Name: "account", Address: "10.0.0.2", Port: 22},
		&model.Host{ID: "host-hidden", Name: "hidden", Address: "10.0.0.3", Port: 22},
		&model.HostAccount{ID: "ha-container", HostID: "host-container", Username: "root", Status: "active", ResourceID: "1001"},
		&model.HostAccount{ID: "ha-account", HostID: "host-account", Username: "deploy", Status: "active", ResourceID: "1002"},
		&model.HostAccount{ID: "ha-hidden", HostID: "host-account", Username: "hidden", Status: "active", ResourceID: "1003"},
		&model.DatabaseInstance{ID: "db-visible", Name: "visible-db", Protocol: "mysql", Address: "10.0.1.1", Port: 3306},
		&model.DatabaseInstance{ID: "db-hidden", Name: "hidden-db", Protocol: "mysql", Address: "10.0.1.2", Port: 3306},
		&model.DatabaseInstance{ID: "db-account-only", Name: "account-only-db", Protocol: "mysql", Address: "10.0.1.3", Port: 3306},
		&model.DatabaseAccount{ID: "dba-visible", InstanceID: "db-visible", UniqueName: "db-visible-user", Username: "app", Status: "active", ResourceID: "2001"},
		&model.DatabaseAccount{ID: "dba-account-only", InstanceID: "db-account-only", UniqueName: "db-account-only-user", Username: "readonly", Status: "active", ResourceID: "2002"},
		&model.ResourceGrant{ID: "grant-host-container", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "host-container", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "grant-host-account", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "ha-account", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "grant-db-container", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeDatabaseInstance, ResourceID: "db-visible", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "grant-db-account", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeDatabaseAccount, ResourceID: "dba-account-only", Effect: model.PermissionEffectAllow},
	}
	for _, item := range items {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("create %T: %v", item, err)
		}
	}
}

func asTestUser(request *http.Request, userID, username string) *http.Request {
	ctx := context.WithValue(request.Context(), ctxKeyUserID, userID)
	ctx = context.WithValue(ctx, ctxKeyUsername, username)
	return request.WithContext(ctx)
}
