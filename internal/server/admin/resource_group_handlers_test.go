package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/model"
)

func TestAccountResourceGroupIncludesAndMaintainsPlatformAccounts(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	group := model.ResourceGroup{ID: "group-prod", Name: "prod", GroupType: model.ResourceGroupTypeAccount}
	user := model.User{ID: "owner-1", Username: "owner", Status: "active"}
	platform := model.PlatformAccount{ID: "platform-1", Name: "platform", PlatformName: "GitLab", GroupName: group.Name, Username: "gitlab-user", OwnerID: user.ID, Status: "active"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := db.Create(&platform).Error; err != nil {
		t.Fatalf("create platform account: %v", err)
	}

	req := asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/resource-groups?group_type=account", nil))
	rec := httptest.NewRecorder()
	server.handleResourceGroups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []struct {
			ID            string `json:"id"`
			AccountCount  int64  `json:"account_count"`
			PlatformCount int64  `json:"platform_count"`
		} `json:"items"`
	}
	if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].AccountCount != 1 || page.Items[0].PlatformCount != 1 {
		t.Fatalf("unexpected counts: %#v", page.Items)
	}

	req = asTestSuperAdmin(httptest.NewRequest(http.MethodPut, "/api/resource-groups/"+group.ID, strings.NewReader(`{"name":"production"}`)))
	rec = httptest.NewRecorder()
	server.handleResourceGroups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var updated model.PlatformAccount
	if err := db.First(&updated, "id = ?", platform.ID).Error; err != nil || updated.GroupName != "production" {
		t.Fatalf("platform group = %q, err=%v", updated.GroupName, err)
	}

	req = asTestSuperAdmin(httptest.NewRequest(http.MethodDelete, "/api/resource-groups/"+group.ID, nil))
	rec = httptest.NewRecorder()
	server.handleResourceGroups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if err := db.First(&updated, "id = ?", platform.ID).Error; err != nil || updated.GroupName != "" {
		t.Fatalf("platform group after delete = %q, err=%v", updated.GroupName, err)
	}
}
