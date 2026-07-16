package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/model"
)

func TestResourceGroupIncludesAndMaintainsAllResourceContainerCounts(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	server.superAdminIDs["u-admin"] = true

	group := model.ResourceGroup{ID: "group-prod", Name: "prod", GroupType: model.ResourceGroupTypeResource}
	user := model.User{ID: "owner-1", Username: "owner", Status: "active"}
	resources := []any{
		&model.Host{ID: "host-1", Name: "host", Address: "127.0.0.1", Port: 22, GroupName: group.Name, Status: "active"},
		&model.DatabaseInstance{ID: "db-1", Name: "database", Protocol: "mysql", Address: "127.0.0.1", Port: 3306, GroupName: group.Name, Status: "active"},
		&model.Application{ID: "app-1", Name: "application", AppGroup: group.Name, ListenPort: 47110, InternalScheme: "http", InternalHost: "127.0.0.1", InternalPort: 8080, Status: "active"},
		&model.PlatformAccount{ID: "platform-1", Name: "platform", PlatformName: "GitLab", GroupName: group.Name, Username: "gitlab-user", OwnerID: user.ID, Visibility: "private", Status: "active"},
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	for _, resource := range resources {
		if err := db.Create(resource).Error; err != nil {
			t.Fatalf("create resource %T: %v", resource, err)
		}
	}

	req := asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/resource-groups?group_type=resource", nil))
	rec := httptest.NewRecorder()
	server.handleResourceGroups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []struct {
			ID               string `json:"id"`
			HostCount        int64  `json:"host_count"`
			DatabaseCount    int64  `json:"database_count"`
			ApplicationCount int64  `json:"application_count"`
			PlatformCount    int64  `json:"platform_count"`
		} `json:"items"`
	}
	if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("groups = %#v", page.Items)
	}
	item := page.Items[0]
	if item.HostCount != 1 || item.DatabaseCount != 1 || item.ApplicationCount != 1 || item.PlatformCount != 1 {
		t.Fatalf("unexpected counts: %#v", item)
	}

	req = asTestSuperAdmin(httptest.NewRequest(http.MethodPut, "/api/resource-groups/"+group.ID, strings.NewReader(`{"name":"production"}`)))
	rec = httptest.NewRecorder()
	server.handleResourceGroups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d; body=%s", rec.Code, rec.Body.String())
	}
	assertGroupValues := func(want string) {
		t.Helper()
		var host model.Host
		var database model.DatabaseInstance
		var application model.Application
		var platform model.PlatformAccount
		if err := db.First(&host, "id = ?", "host-1").Error; err != nil || host.GroupName != want {
			t.Fatalf("host group = %q, err=%v", host.GroupName, err)
		}
		if err := db.First(&database, "id = ?", "db-1").Error; err != nil || database.GroupName != want {
			t.Fatalf("database group = %q, err=%v", database.GroupName, err)
		}
		if err := db.First(&application, "id = ?", "app-1").Error; err != nil || application.AppGroup != want {
			t.Fatalf("application group = %q, err=%v", application.AppGroup, err)
		}
		if err := db.First(&platform, "id = ?", "platform-1").Error; err != nil || platform.GroupName != want {
			t.Fatalf("platform group = %q, err=%v", platform.GroupName, err)
		}
	}
	assertGroupValues("production")

	req = asTestSuperAdmin(httptest.NewRequest(http.MethodDelete, "/api/resource-groups/"+group.ID, nil))
	rec = httptest.NewRecorder()
	server.handleResourceGroups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d; body=%s", rec.Code, rec.Body.String())
	}
	assertGroupValues("")
}
