package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
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
	if len(instances) != 1 || instances[0].ID != "db-visible" {
		t.Fatalf("unexpected database instances: %#v", instances)
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
		&model.DatabaseAccount{ID: "dba-visible", InstanceID: "db-visible", UniqueName: "db-visible-user", Username: "app", Status: "active", ResourceID: "2001"},
		&model.ResourceGrant{ID: "grant-host-container", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "host-container", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "grant-host-account", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "ha-account", Effect: model.PermissionEffectAllow},
		&model.ResourceGrant{ID: "grant-db-container", PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeDatabaseInstance, ResourceID: "db-visible", Effect: model.PermissionEffectAllow},
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
