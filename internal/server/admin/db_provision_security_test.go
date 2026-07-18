package admin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

func TestHandleDBDatabasesRequiresConnectPermissionOnAdminAccount(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	instance, account := seedDatabaseProvisioningSecurityFixture(t, db, "db-list-user")
	seedGlobalAction(t, db, "db-list-user", rbac.ActionDBProxyView)
	provisioning := &fakeDatabaseProvisioningService{databases: []string{"app"}}
	server.databaseProvisioning = provisioning

	request := asTestUser(
		httptest.NewRequest(
			http.MethodGet,
			"/api/db/instances/"+instance.ID+"/databases?admin_account_id="+account.ID,
			nil,
		),
		"db-list-user",
		"operator",
	)
	recorder := httptest.NewRecorder()
	server.handleDBDatabases(recorder, request, instance.ID)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if provisioning.listCalls != 0 {
		t.Fatalf("service used administrator credentials without account permission: calls=%d", provisioning.listCalls)
	}
}

func TestHandleDBDatabasesDoesNotExposeServiceDetails(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	instance, account := seedDatabaseProvisioningSecurityFixture(t, db, "db-list-user")
	seedGlobalAction(t, db, "db-list-user", rbac.ActionDBProxyView)
	grantDatabaseAccountConnection(t, db, "db-list-user", account.ID)
	server.databaseProvisioning = &fakeDatabaseProvisioningService{
		listErr: errors.New("upstream echoed password=top-secret"),
	}

	request := asTestUser(
		httptest.NewRequest(
			http.MethodGet,
			"/api/db/instances/"+instance.ID+"/databases?admin_account_id="+account.ID,
			nil,
		),
		"db-list-user",
		"operator",
	)
	recorder := httptest.NewRecorder()
	server.handleDBDatabases(recorder, request, instance.ID)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "top-secret") ||
		!strings.Contains(recorder.Body.String(), "database operation failed") {
		t.Fatalf("unsafe database error response: %s", recorder.Body.String())
	}
}

func TestHandleDBProvisionAccountRejectsClientSuppliedPassword(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	instance, adminAccount := seedDatabaseProvisioningSecurityFixture(t, db, "db-create-user")
	seedGlobalAction(t, db, "db-create-user", rbac.ActionDBProxyCreate)
	grantDatabaseAccountConnection(t, db, "db-create-user", adminAccount.ID)
	provisioning := &fakeDatabaseProvisioningService{}
	server.databaseProvisioning = provisioning

	body := `{"admin_account_id":"` + adminAccount.ID +
		`","password":"client-secret","host":"10.0.0.8",` +
		`"grants":[{"database":"app","privilege":"read"}]}`
	request := asTestUser(
		httptest.NewRequest(
			http.MethodPost,
			"/api/db/instances/"+instance.ID+"/provision-account",
			bytes.NewBufferString(body),
		),
		"db-create-user",
		"operator",
	)
	recorder := httptest.NewRecorder()
	server.handleDBProvisionAccount(recorder, request, instance.ID)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if provisioning.provisionCalls != 0 {
		t.Fatalf("client-supplied password reached provisioning service: %d", provisioning.provisionCalls)
	}
	if strings.Contains(recorder.Body.String(), "client-secret") {
		t.Fatalf("password rejection exposed the supplied secret: %s", recorder.Body.String())
	}
}

func TestHandleDBProvisionAccountRequiresValidIdempotencyKey(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	instance, adminAccount := seedDatabaseProvisioningSecurityFixture(t, db, "db-create-user")
	seedGlobalAction(t, db, "db-create-user", rbac.ActionDBProxyCreate)
	grantDatabaseAccountConnection(t, db, "db-create-user", adminAccount.ID)
	provisioning := &fakeDatabaseProvisioningService{}
	server.databaseProvisioning = provisioning
	body := `{"admin_account_id":"` + adminAccount.ID + `","host":"10.0.0.8","grants":[{"database":"app","privilege":"read"}]}`
	for _, key := range []string{"", "short", "valid-key-but-has-space 001", "valid-key-but-has/slash"} {
		request := asTestUser(httptest.NewRequest(http.MethodPost, "/api/db/instances/"+instance.ID+"/provision-account", bytes.NewBufferString(body)), "db-create-user", "operator")
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		recorder := httptest.NewRecorder()
		server.handleDBProvisionAccount(recorder, request, instance.ID)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("key %q status = %d, want 400; body=%s", key, recorder.Code, recorder.Body.String())
		}
	}
	if provisioning.provisionCalls != 0 {
		t.Fatalf("invalid key reached service: %d", provisioning.provisionCalls)
	}
}

func TestHandleDBProvisionAccountRejectsClientSuppliedUpstreamUsername(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	instance, adminAccount := seedDatabaseProvisioningSecurityFixture(t, db, "db-create-user")
	seedGlobalAction(t, db, "db-create-user", rbac.ActionDBProxyCreate)
	grantDatabaseAccountConnection(t, db, "db-create-user", adminAccount.ID)
	provisioning := &fakeDatabaseProvisioningService{}
	server.databaseProvisioning = provisioning

	body := `{"admin_account_id":"` + adminAccount.ID +
		`","new_username":"existing_admin","host":"10.0.0.8",` +
		`"grants":[{"database":"app","privilege":"read"}]}`
	request := asTestUser(
		httptest.NewRequest(
			http.MethodPost,
			"/api/db/instances/"+instance.ID+"/provision-account",
			bytes.NewBufferString(body),
		),
		"db-create-user",
		"operator",
	)
	recorder := httptest.NewRecorder()
	server.handleDBProvisionAccount(recorder, request, instance.ID)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if provisioning.provisionCalls != 0 {
		t.Fatalf("client-controlled upstream identity reached service: %d", provisioning.provisionCalls)
	}
}

func TestHandleDBProvisionAccountReturnsOnlyLocalAccountResource(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	instance, adminAccount := seedDatabaseProvisioningSecurityFixture(t, db, "db-create-user")
	seedGlobalAction(t, db, "db-create-user", rbac.ActionDBProxyCreate)
	grantDatabaseAccountConnection(t, db, "db-create-user", adminAccount.ID)
	provisioning := &fakeDatabaseProvisioningService{
		provisionResult: service.ProvisionDatabaseAccountResult{
			Account: service.ProvisionedDatabaseAccount{
				ID: "local-account", InstanceID: instance.ID, Username: "app",
				Status: "active", ResourceID: "D001",
			},
		},
	}
	server.databaseProvisioning = provisioning

	body := `{"admin_account_id":"` + adminAccount.ID +
		`","host":"10.0.0.8",` +
		`"grants":[{"database":"app","privilege":"read"}]}`
	request := asTestUser(
		httptest.NewRequest(
			http.MethodPost,
			"/api/db/instances/"+instance.ID+"/provision-account",
			bytes.NewBufferString(body),
		),
		"db-create-user",
		"operator",
	)
	request.Header.Set("Idempotency-Key", "create-account-response-001")
	recorder := httptest.NewRecorder()
	server.handleDBProvisionAccount(recorder, request, instance.ID)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", recorder.Code, recorder.Body.String())
	}
	if provisioning.provisionCalls != 1 ||
		provisioning.provisionRequest.InstanceID != instance.ID ||
		provisioning.provisionRequest.Host != "10.0.0.8" ||
		provisioning.provisionRequest.Actor.UserID != "db-create-user" {
		t.Fatalf("handler did not delegate parsed request: %#v", provisioning.provisionRequest)
	}
	response := recorder.Body.String()
	if strings.Contains(response, "generated_password") ||
		strings.Contains(response, "client-secret") ||
		!strings.Contains(response, `"resource_id":"D001"`) {
		t.Fatalf("unsafe provisioning response: %s", response)
	}
}

func TestHandleDBProvisionAccountReportsPersistentCleanupWithoutDetails(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	instance, adminAccount := seedDatabaseProvisioningSecurityFixture(t, db, "db-create-user")
	seedGlobalAction(t, db, "db-create-user", rbac.ActionDBProxyCreate)
	grantDatabaseAccountConnection(t, db, "db-create-user", adminAccount.ID)
	server.databaseProvisioning = &fakeDatabaseProvisioningService{
		provisionErr: errors.Join(
			service.ErrDatabaseProvisioningCleanupRequired,
			errors.New("DROP USER app password=top-secret"),
		),
	}

	body := `{"admin_account_id":"` + adminAccount.ID +
		`","host":"10.0.0.8",` +
		`"grants":[{"database":"app","privilege":"read"}]}`
	request := asTestUser(
		httptest.NewRequest(
			http.MethodPost,
			"/api/db/instances/"+instance.ID+"/provision-account",
			bytes.NewBufferString(body),
		),
		"db-create-user",
		"operator",
	)
	request.Header.Set("Idempotency-Key", "create-account-cleanup-001")
	recorder := httptest.NewRecorder()
	server.handleDBProvisionAccount(recorder, request, instance.ID)
	if recorder.Code != http.StatusInternalServerError ||
		!strings.Contains(recorder.Body.String(), "cleanup is pending") {
		t.Fatalf("cleanup response = status %d body %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "top-secret") ||
		strings.Contains(recorder.Body.String(), "DROP USER") {
		t.Fatalf("cleanup response leaked details: %s", recorder.Body.String())
	}
}

func TestWriteDBStoreErrorDoesNotExposeStorageDetails(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/db/instances", nil)
	recorder := httptest.NewRecorder()
	writeDBStoreError(recorder, request, errors.New("sqlite password=top-secret"))
	if strings.Contains(recorder.Body.String(), "top-secret") ||
		!strings.Contains(recorder.Body.String(), "invalid database request") {
		t.Fatalf("unsafe database store response: %s", recorder.Body.String())
	}
}

type fakeDatabaseProvisioningService struct {
	databases        []string
	listErr          error
	provisionResult  service.ProvisionDatabaseAccountResult
	provisionErr     error
	listCalls        int
	provisionCalls   int
	provisionRequest service.ProvisionDatabaseAccountRequest
}

func (f *fakeDatabaseProvisioningService) ListDatabases(
	context.Context,
	service.ListProvisioningDatabasesRequest,
) ([]string, error) {
	f.listCalls++
	return f.databases, f.listErr
}

func (f *fakeDatabaseProvisioningService) Provision(
	_ context.Context,
	request service.ProvisionDatabaseAccountRequest,
) (service.ProvisionDatabaseAccountResult, error) {
	f.provisionCalls++
	f.provisionRequest = request
	return f.provisionResult, f.provisionErr
}

func seedDatabaseProvisioningSecurityFixture(
	t *testing.T,
	db *gorm.DB,
	userID string,
) (model.DatabaseInstance, model.DatabaseAccount) {
	t.Helper()
	if err := db.Create(&model.User{
		ID: userID, Username: userID, Status: "active",
	}).Error; err != nil {
		t.Fatalf("create provisioning user: %v", err)
	}
	instance := model.DatabaseInstance{
		Name: "orders", Protocol: "mysql", Address: "127.0.0.1", Port: 3306, Status: "active",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create database instance: %v", err)
	}
	account := model.DatabaseAccount{
		InstanceID: instance.ID,
		UniqueName: "admin-" + userID,
		Username:   "administrator",
		Password:   model.NewEncryptedField("admin-secret"),
		Status:     "active",
		ResourceID: "D001",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create database account: %v", err)
	}
	return instance, account
}

func grantDatabaseAccountConnection(t *testing.T, db *gorm.DB, userID, accountID string) {
	t.Helper()
	seedGlobalAction(t, db, userID, rbac.ActionDBConnect)
	seedResourceGrant(t, db, userID, model.ResourceTypeDatabaseAccount, accountID)
}
