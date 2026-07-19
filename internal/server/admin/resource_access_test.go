package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func TestResourceAccessHasNoDirectDatabaseOrAggregateStoreQueries(t *testing.T) {
	source, err := os.ReadFile("resource_access.go")
	if err != nil {
		t.Fatalf("read resource access source: %v", err)
	}
	for _, forbidden := range []string{"s.db", "s.store.HostAccounts", "s.store.InstanceAccounts"} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("resource access still contains forbidden dependency %q", forbidden)
		}
	}

	aggregateStore, err := os.ReadFile("../../store/store.go")
	if err != nil {
		t.Fatalf("read aggregate store source: %v", err)
	}
	for _, forbidden := range []string{"\tHostAccounts(", "\tInstanceAccounts("} {
		if strings.Contains(string(aggregateStore), forbidden) {
			t.Fatalf("aggregate store still contains resource access method %q", forbidden)
		}
	}
}

func TestVisibleResourcesUseContainerAndAccountGrants(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAccessTestData(t, db)
	for _, action := range []string{rbac.ActionHostView, rbac.ActionHostUpdate, rbac.ActionTargetView, rbac.ActionDBProxyView, rbac.ActionDBProxyUpdate} {
		seedGlobalAction(t, db, "u1", action)
	}
	request := asTestUser(httptest.NewRequest(http.MethodGet, "/api/hosts", nil), "u1", "alice")

	hosts, err := server.visibleHosts(request, server.store.Hosts())
	if err != nil {
		t.Fatalf("visibleHosts: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("visible hosts = %d, want 2", len(hosts))
	}
	for _, host := range hosts {
		switch host.ID {
		case "host-container":
			if host.AccountCount != 1 || !host.CanManage {
				t.Fatalf("container-granted host = %#v, want one visible account with management", host)
			}
		case "host-account":
			if host.AccountCount != 1 || host.CanManage {
				t.Fatalf("account-granted host = %#v, want one visible account without management", host)
			}
		}
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
		switch instance.ID {
		case "db-visible":
			if instance.AccountCount != 1 || !instance.CanManage {
				t.Fatalf("container-granted database = %#v, want one visible account with management", instance)
			}
		case "db-account-only":
			if instance.AccountCount != 1 || instance.CanManage {
				t.Fatalf("account-granted database container = %#v, want one visible account without container management", instance)
			}
		}
	}
}

func TestHandleDBInstancesConnectableUsesDatabaseConnectGrants(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAccessTestData(t, db)
	seedGlobalAction(t, db, "u1", rbac.ActionDBConnect)

	request := asTestUser(
		httptest.NewRequest(http.MethodGet, "/api/db/instances?connectable=true&page_size=20", nil),
		"u1",
		"alice",
	)
	recorder := httptest.NewRecorder()
	server.handleDBInstances(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("connectable database instances status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var page struct {
		Items []store.DatabaseInstanceView `json:"items"`
		Total int                          `json:"total"`
	}
	if err := decodeTestData(t, recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode connectable database instances: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("connectable database instances = %#v, want container/account-granted instances", page)
	}
	for _, instance := range page.Items {
		if instance.ID == "db-hidden" {
			t.Fatalf("hidden database instance appeared in connectable list: %#v", page.Items)
		}
	}
}

func TestHandleDBAccountsPaginatesSearchesAndFiltersVisibleResources(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAccessTestData(t, db)
	role := model.Role{ID: "role-db-list", Name: "role-db-list", Status: "active"}
	permission := model.Permission{ID: "perm-db-list", Action: rbac.ActionDBProxyView, Effect: model.PermissionEffectAllow}
	for _, value := range []any{&role, &permission, &model.UserRole{ID: "ur-db-list", UserID: "u1", RoleID: role.ID}, &model.RolePermission{ID: "rp-db-list", RoleID: role.ID, PermissionID: permission.ID}} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("seed database list permission: %v", err)
		}
	}

	request := asTestUser(httptest.NewRequest(http.MethodGet, "/api/db/accounts?page=2&page_size=1", nil), "u1", "alice")
	recorder := httptest.NewRecorder()
	server.handleDBAccounts(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list database accounts status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var page struct {
		Items    []databaseAccountResourceView `json:"items"`
		Total    int                           `json:"total"`
		Page     int                           `json:"page"`
		PageSize int                           `json:"page_size"`
	}
	if err := decodeTestData(t, recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode database accounts: %v", err)
	}
	if page.Total != 2 || page.Page != 2 || page.PageSize != 1 || len(page.Items) != 1 || page.Items[0].ID != "dba-account-only" {
		t.Fatalf("unexpected paged database accounts: %#v", page)
	}
	if page.Items[0].InstanceName != "account-only-db" || page.Items[0].InstanceAddress != "10.0.1.3:3306" {
		t.Fatalf("missing instance metadata: %#v", page.Items[0])
	}

	searchRequest := asTestUser(httptest.NewRequest(http.MethodGet, "/api/db/accounts?q=10.0.1.3", nil), "u1", "alice")
	searchRecorder := httptest.NewRecorder()
	server.handleDBAccounts(searchRecorder, searchRequest)
	if searchRecorder.Code != http.StatusOK {
		t.Fatalf("search database accounts status = %d, body=%s", searchRecorder.Code, searchRecorder.Body.String())
	}
	if err := decodeTestData(t, searchRecorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode searched database accounts: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "dba-account-only" {
		t.Fatalf("unexpected searched database accounts: %#v", page)
	}
}

func TestAccountGrantAllowsViewingButNotContainerManagement(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAccessTestData(t, db)
	seedGlobalAction(t, db, "u1", rbac.ActionTargetView)
	seedGlobalAction(t, db, "u1", rbac.ActionDBProxyView)
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
	if server.requireResourceAction(recorder, request, rbac.ActionTargetUpdate, model.ResourceTypeHostAccount, accountOnly.ID) {
		t.Fatal("direct host account grant was accepted as host management authorization")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("host account management status = %d, want 403", recorder.Code)
	}

	dbRequest := asTestUser(httptest.NewRequest(http.MethodPut, "/api/db/accounts/dba-account-only", nil), "u1", "alice")
	allDBAccounts, err := server.resourceAccess.ListDatabaseAccountsByInstance(request.Context(), "db-account-only")
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
	if server.requireResourceAction(dbRecorder, dbRequest, rbac.ActionDBProxyUpdate, model.ResourceTypeDatabaseAccount, "dba-account-only") {
		t.Fatal("direct database account grant was accepted as instance management authorization")
	}
	if dbRecorder.Code != http.StatusForbidden {
		t.Fatalf("database account management status = %d, want 403", dbRecorder.Code)
	}
}

func TestApplicationsAreFilteredByResourceGrant(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "app-user", rbac.ActionAppView)
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
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "app-visible" || page.Items[0].CanManage {
		t.Fatalf("unexpected visible applications: %#v", page)
	}
}

func TestApplicationResourcePermissionDenyOverridesActionAndGrant(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAuthorizationUser(t, db, "resource-deny-user")
	application := model.Application{
		ID:             "application-denied",
		Name:           "denied",
		InternalScheme: "http",
		InternalHost:   "127.0.0.1",
		InternalPort:   8080,
		ListenPort:     47120,
		Status:         "active",
	}
	if err := db.Create(&application).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	seedResourceActionPolicy(t, db, "resource-deny-user", rbac.ActionAppView, model.ResourceTypeApplication, application.ID)
	seedResourceGrant(t, db, "resource-deny-user", model.ResourceTypeApplication, application.ID)

	request := withTestUser(httptest.NewRequest(http.MethodGet, "/api/applications/"+application.ID, nil), "resource-deny-user", "resource-deny-user")
	recorder := httptest.NewRecorder()
	server.handleApplication(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("application GET with explicit deny status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreationAndConnectionPathsHonorResourcePermissionDeny(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	userID := "resource-create-deny-user"
	seedResourceAuthorizationUser(t, db, userID)
	host := model.Host{ID: "host-create-denied", Name: "host", Address: "10.0.0.10", Port: 22}
	account := model.HostAccount{ID: "host-account-create-denied", HostID: host.ID, Username: "deploy", Status: "active", ResourceID: "5001"}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create host account: %v", err)
	}
	seedResourceActionPolicy(t, db, userID, rbac.ActionTargetCreate, model.ResourceTypeHost, host.ID)
	seedResourceGrant(t, db, userID, model.ResourceTypeHost, host.ID)
	seedResourceActionPolicy(t, db, userID, rbac.ActionTargetCreate, model.ResourceTypeHostAccount, account.ID)
	seedResourceGrant(t, db, userID, model.ResourceTypeHostAccount, account.ID)

	createRequest := withTestUser(httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewBufferString(`{"host_id":"host-create-denied","host":"10.0.0.10","port":22,"username":"new-deploy","password":"secret"}`)), userID, userID)
	createRecorder := httptest.NewRecorder()
	server.handleCreateTarget(createRecorder, createRequest)
	if createRecorder.Code != http.StatusForbidden {
		t.Fatalf("host account creation with parent deny status = %d, want 403; body=%s", createRecorder.Code, createRecorder.Body.String())
	}

	specifiedHostRequest := withTestUser(httptest.NewRequest(http.MethodPost, "/api/targets/test", bytes.NewBufferString(`{"host_id":"host-create-denied","host":"10.0.0.10","port":22,"username":"deploy","password":"secret"}`)), userID, userID)
	specifiedHostRecorder := httptest.NewRecorder()
	server.handleTestConnection(specifiedHostRecorder, specifiedHostRequest)
	if specifiedHostRecorder.Code != http.StatusForbidden {
		t.Fatalf("specified host connection test with parent deny status = %d, want 403; body=%s", specifiedHostRecorder.Code, specifiedHostRecorder.Body.String())
	}

	existingAccountRequest := withTestUser(httptest.NewRequest(http.MethodPost, "/api/targets/test", bytes.NewBufferString(`{"id":"host-account-create-denied"}`)), userID, userID)
	existingAccountRecorder := httptest.NewRecorder()
	server.handleTestConnection(existingAccountRecorder, existingAccountRequest)
	if existingAccountRecorder.Code != http.StatusForbidden {
		t.Fatalf("existing host account connection test with account deny status = %d, want 403; body=%s", existingAccountRecorder.Code, existingAccountRecorder.Body.String())
	}
}

func TestDatabaseCreationProvisionAndTestHonorResourcePermissionDeny(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	userID := "database-create-deny-user"
	seedResourceAuthorizationUser(t, db, userID)
	instance := model.DatabaseInstance{ID: "database-create-denied", Name: "database", Protocol: "mysql", Address: "10.0.0.11", Port: 3306, Status: "active"}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create database instance: %v", err)
	}
	seedResourceActionPolicy(t, db, userID, rbac.ActionDBProxyCreate, model.ResourceTypeDatabaseInstance, instance.ID)
	seedResourceGrant(t, db, userID, model.ResourceTypeDatabaseInstance, instance.ID)

	createRequest := withTestUser(httptest.NewRequest(http.MethodPost, "/api/db/instances/"+instance.ID+"/accounts", bytes.NewBufferString(`{"username":"app","password":"secret"}`)), userID, userID)
	createRecorder := httptest.NewRecorder()
	server.handleDBInstance(createRecorder, createRequest)
	if createRecorder.Code != http.StatusForbidden {
		t.Fatalf("database account creation with instance deny status = %d, want 403; body=%s", createRecorder.Code, createRecorder.Body.String())
	}

	provisionRequest := withTestUser(httptest.NewRequest(http.MethodPost, "/api/db/instances/"+instance.ID+"/provision-account", bytes.NewBufferString(`{"admin_account_id":"missing","new_username":"app","password":"secret"}`)), userID, userID)
	provisionRecorder := httptest.NewRecorder()
	server.handleDBInstance(provisionRecorder, provisionRequest)
	if provisionRecorder.Code != http.StatusForbidden {
		t.Fatalf("database account provisioning with instance deny status = %d, want 403; body=%s", provisionRecorder.Code, provisionRecorder.Body.String())
	}

	testRequest := withTestUser(httptest.NewRequest(http.MethodPost, "/api/db/accounts/test", bytes.NewBufferString(`{"instance_id":"database-create-denied","username":"app","password":"secret"}`)), userID, userID)
	testRecorder := httptest.NewRecorder()
	server.handleTestDBConnection(testRecorder, testRequest)
	if testRecorder.Code != http.StatusForbidden {
		t.Fatalf("database connection test with instance deny status = %d, want 403; body=%s", testRecorder.Code, testRecorder.Body.String())
	}
}

func TestResourceListsFailClosedWithoutAuthenticatedIdentity(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		handle func(*Server, http.ResponseWriter, *http.Request)
	}{
		{name: "hosts", path: "/api/hosts", handle: (*Server).handleHosts},
		{name: "host accounts", path: "/api/targets", handle: (*Server).handleTargets},
		{name: "database instances", path: "/api/db/instances", handle: (*Server).handleDBInstances},
		{name: "database accounts", path: "/api/db/accounts", handle: (*Server).handleDBAccounts},
		{name: "applications", path: "/api/applications", handle: (*Server).handleApplications},
		{name: "platform accounts", path: "/api/platform-accounts", handle: (*Server).handlePlatformAccounts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := newAdminDBTestServer(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			tt.handle(server, recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("unauthenticated list status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestConnectableHostListUsesConnectionActionWithoutTargetView(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAccessTestData(t, db)
	seedConnectionAction(t, db, "connect-only-user", rbac.ActionSessionConnect)
	seedResourceGrant(t, db, "connect-only-user", model.ResourceTypeHostAccount, "ha-account")

	request := withTestUser(httptest.NewRequest(http.MethodGet, "/api/targets?connectable=true", nil), "connect-only-user", "connect-only-user")
	recorder := httptest.NewRecorder()
	server.handleTargets(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("connectable host list status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var page struct {
		Items []store.TargetView `json:"items"`
	}
	if err := decodeTestData(t, recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode connectable host list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "ha-account" {
		t.Fatalf("unexpected connectable host list: %#v", page.Items)
	}
}

func TestConnectableDatabaseListUsesDBConnectWithoutView(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAccessTestData(t, db)
	seedConnectionAction(t, db, "db-connect-only-user", rbac.ActionDBConnect)
	seedResourceGrant(t, db, "db-connect-only-user", model.ResourceTypeDatabaseAccount, "dba-account-only")

	request := withTestUser(httptest.NewRequest(http.MethodGet, "/api/db/instances/db-account-only/accounts?connectable=true", nil), "db-connect-only-user", "db-connect-only-user")
	recorder := httptest.NewRecorder()
	server.handleDBInstance(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("connectable database list status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var page struct {
		Items []store.DatabaseAccountView `json:"items"`
	}
	if err := decodeTestData(t, recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode connectable database list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "dba-account-only" {
		t.Fatalf("unexpected connectable database list: %#v", page.Items)
	}
}

func TestHostAccountResourcePermissionDenyBlocksActionsAndList(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAuthorizationUser(t, db, "host-account-deny-user")
	host := model.Host{ID: "host-resource-denied", Name: "host", Address: "10.0.0.4", Port: 22}
	account := model.HostAccount{ID: "host-account-resource-denied", HostID: host.ID, Username: "deploy", Status: "active", ResourceID: "3001"}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create host account: %v", err)
	}
	for _, action := range []string{rbac.ActionTargetView, rbac.ActionTargetUpdate, rbac.ActionTargetDelete} {
		seedResourceActionPolicy(t, db, "host-account-deny-user", action, model.ResourceTypeHostAccount, account.ID)
	}
	seedResourceGrant(t, db, "host-account-deny-user", model.ResourceTypeHostAccount, account.ID)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		request := withTestUser(httptest.NewRequest(method, "/api/targets/"+account.ID, bytes.NewBufferString(`{"username":"changed"}`)), "host-account-deny-user", "host-account-deny-user")
		recorder := httptest.NewRecorder()
		server.handleTarget(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("host account %s with explicit deny status = %d, want 403; body=%s", method, recorder.Code, recorder.Body.String())
		}
	}

	listRequest := withTestUser(httptest.NewRequest(http.MethodGet, "/api/targets", nil), "host-account-deny-user", "host-account-deny-user")
	listRecorder := httptest.NewRecorder()
	server.handleTargets(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("host account list status = %d, body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var page struct {
		Items []store.TargetView `json:"items"`
	}
	if err := decodeTestData(t, listRecorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode host account list: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("explicitly denied host account appeared in list: %#v", page.Items)
	}
}

func TestDatabaseAccountResourcePermissionDenyBlocksActionsAndList(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedResourceAuthorizationUser(t, db, "database-account-deny-user")
	instance := model.DatabaseInstance{ID: "database-resource-denied", Name: "database", Protocol: "mysql", Address: "10.0.1.4", Port: 3306, Status: "active"}
	account := model.DatabaseAccount{ID: "database-account-resource-denied", InstanceID: instance.ID, UniqueName: "database-user", Username: "app", Status: "active", ResourceID: "4001"}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create database instance: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create database account: %v", err)
	}
	for _, action := range []string{rbac.ActionDBProxyView, rbac.ActionDBProxyUpdate, rbac.ActionDBProxyDelete} {
		seedResourceActionPolicy(t, db, "database-account-deny-user", action, model.ResourceTypeDatabaseAccount, account.ID)
	}
	seedResourceGrant(t, db, "database-account-deny-user", model.ResourceTypeDatabaseAccount, account.ID)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		request := withTestUser(httptest.NewRequest(method, "/api/db/accounts/"+account.ID, bytes.NewBufferString(`{"username":"changed"}`)), "database-account-deny-user", "database-account-deny-user")
		recorder := httptest.NewRecorder()
		server.handleDBAccount(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("database account %s with explicit deny status = %d, want 403; body=%s", method, recorder.Code, recorder.Body.String())
		}
	}

	listRequest := withTestUser(httptest.NewRequest(http.MethodGet, "/api/db/accounts", nil), "database-account-deny-user", "database-account-deny-user")
	listRecorder := httptest.NewRecorder()
	server.handleDBAccounts(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("database account list status = %d, body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var page struct {
		Items []databaseAccountResourceView `json:"items"`
	}
	if err := decodeTestData(t, listRecorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode database account list: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("explicitly denied database account appeared in list: %#v", page.Items)
	}
}

func seedResourceAuthorizationUser(t *testing.T, db *gorm.DB, userID string) {
	t.Helper()
	if err := db.Create(&model.User{ID: userID, Username: userID, Status: "active"}).Error; err != nil {
		t.Fatalf("create authorization user: %v", err)
	}
}

func seedResourceActionPolicy(t *testing.T, db *gorm.DB, userID, action, resourceType, resourceID string) {
	t.Helper()
	key := strings.NewReplacer(":", "-", "/", "-").Replace(userID + "-" + action + "-" + resourceID)
	role := model.Role{ID: "role-" + key, Name: "role-" + key, Status: "active"}
	allow := model.Permission{Action: action, Effect: model.PermissionEffectAllow}
	deny := model.Permission{ID: "deny-" + key, Action: action, ResourceType: resourceType, ResourceID: resourceID, Effect: model.PermissionEffectDeny}
	if err := db.Where(
		"action = ? AND resource_type = '' AND resource_id = '' AND effect = ?",
		action, model.PermissionEffectAllow,
	).FirstOrCreate(&allow).Error; err != nil {
		t.Fatalf("create shared action permission: %v", err)
	}
	for _, value := range []any{&role, &deny, &model.UserRole{ID: "user-role-" + key, UserID: userID, RoleID: role.ID}} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("create resource action policy: %v", err)
		}
	}
	for _, permission := range []model.Permission{allow, deny} {
		if err := db.Create(&model.RolePermission{ID: "role-permission-" + key + "-" + permission.ID, RoleID: role.ID, PermissionID: permission.ID}).Error; err != nil {
			t.Fatalf("create role permission: %v", err)
		}
	}
}

func seedResourceGrant(t *testing.T, db *gorm.DB, userID, resourceType, resourceID string) {
	t.Helper()
	if err := db.Create(&model.ResourceGrant{PrincipalType: "user", PrincipalID: userID, ResourceType: resourceType, ResourceID: resourceID, Effect: model.PermissionEffectAllow}).Error; err != nil {
		t.Fatalf("create resource grant: %v", err)
	}
}

func seedGlobalAction(t *testing.T, db *gorm.DB, userID, action string) {
	t.Helper()
	key := strings.NewReplacer(":", "-", "/", "-").Replace(userID + "-" + action)
	role := model.Role{ID: "global-role-" + key, Name: "global-role-" + key, Status: "active"}
	permission := model.Permission{ID: "global-permission-" + key, Action: action, Effect: model.PermissionEffectAllow}
	for _, value := range []any{&role, &permission, &model.UserRole{ID: "global-user-role-" + key, UserID: userID, RoleID: role.ID}, &model.RolePermission{ID: "global-role-permission-" + key, RoleID: role.ID, PermissionID: permission.ID}} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("create global action policy: %v", err)
		}
	}
}

func TestCreateApplicationAutomaticallyGrantsCreator(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "app-creator", Username: "creator", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	body := bytes.NewBufferString(`{"address":"http://127.0.0.1:8080/nacos/#/login?namespace="}`)
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
	if created.Name != "127.0.0.1" || created.Address != "http://127.0.0.1:8080/nacos/#/login?namespace=" || created.EntryPath != "/nacos/#/login?namespace=" {
		t.Fatalf("unexpected parsed application: %#v", created)
	}
	if created.InternalScheme != "http" || created.InternalHost != "127.0.0.1" || created.InternalPort != 8080 || created.ListenPort != 47110 {
		t.Fatalf("unexpected application endpoint: %#v", created)
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
		&model.DatabaseAccount{ID: "dba-account-hidden", InstanceID: "db-account-only", UniqueName: "db-account-hidden-user", Username: "hidden", Status: "active", ResourceID: "2003"},
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
