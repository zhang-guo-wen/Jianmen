package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"jianmen/internal/model"
)

func TestPlatformAccountModelDefaultsPlatformNameToURL(t *testing.T) {
	account := platformAccountModel(platformAccountPayload{URL: "https://git.example.com"}, "owner-1")
	if account.PlatformName != account.URL {
		t.Fatalf("platform name = %q, want URL %q", account.PlatformName, account.URL)
	}
}

func TestPlatformAccountModelKeepsOptionalCustomPlatformName(t *testing.T) {
	account := platformAccountModel(platformAccountPayload{URL: "https://git.example.com", PlatformName: "????"}, "owner-1")
	if account.PlatformName != "????" {
		t.Fatalf("platform name = %q, want custom name", account.PlatformName)
	}
}

func TestCreatePlatformAccountRollsBackWhenCreatorGrantFails(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "creator", Username: "creator", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_platform_creator_grant
		BEFORE INSERT ON resource_grants
		BEGIN SELECT RAISE(ABORT, 'injected resource grant failure'); END;`).Error; err != nil {
		t.Fatalf("create grant failure trigger: %v", err)
	}

	request := asTestUser(
		httptest.NewRequest(http.MethodPost, "/api/platform-accounts", bytes.NewBufferString(
			`{"name":"Git","platform_name":"Git","url":"https://git.example.test","username":"alice"}`,
		)),
		"creator",
		"creator",
	)
	recorder := httptest.NewRecorder()
	server.handleCreatePlatformAccount(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}

	var accountCount int64
	if err := db.Model(&model.PlatformAccount{}).Where("owner_id = ?", "creator").Count(&accountCount).Error; err != nil {
		t.Fatalf("count platform accounts: %v", err)
	}
	if accountCount != 0 {
		t.Fatalf("platform account count = %d, want rollback", accountCount)
	}
	var resourceCount int64
	if err := db.Model(&model.Resource{}).
		Where("type = ?", model.ResourceTypePlatformAccount).
		Count(&resourceCount).Error; err != nil {
		t.Fatalf("count platform resources: %v", err)
	}
	if resourceCount != 0 {
		t.Fatalf("platform resource count = %d, want rollback", resourceCount)
	}
}
