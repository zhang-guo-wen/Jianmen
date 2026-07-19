package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/store"
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

func TestCreatePlatformAccountCleanupIgnoresCancelledRequestContext(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "creator", Username: "creator", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	baseRequest := httptest.NewRequest(http.MethodPost, "/api/platform-accounts", bytes.NewBufferString(
		`{"name":"Git","platform_name":"Git","url":"https://git.example.test","username":"alice"}`,
	))
	requestCtx, cancelRequest := context.WithCancel(baseRequest.Context())
	defer cancelRequest()
	request := asTestUser(baseRequest.WithContext(requestCtx), "creator", "creator")
	repository := &cancelAfterPlatformAccountCreateRepository{
		adminPlatformAccountRepository: server.platformAccounts,
		cancelRequest:                  cancelRequest,
	}
	server.platformAccounts = repository

	recorder := httptest.NewRecorder()
	server.handleCreatePlatformAccount(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if !repository.deleteCalled {
		t.Fatal("platform account cleanup was not called")
	}
	if repository.deleteContextErr != nil {
		t.Fatalf("cleanup context error = %v, want request cancellation detached", repository.deleteContextErr)
	}
	if !repository.deleteHadDeadline {
		t.Fatal("cleanup context has no deadline")
	}
	now := time.Now()
	if repository.deleteDeadline.Before(now) || repository.deleteDeadline.After(now.Add(5*time.Second)) {
		t.Fatalf("cleanup deadline = %v, want a live deadline no more than 5 seconds away", repository.deleteDeadline)
	}

	var accountCount int64
	if err := db.Model(&model.PlatformAccount{}).Where("owner_id = ?", "creator").Count(&accountCount).Error; err != nil {
		t.Fatalf("count platform accounts: %v", err)
	}
	if accountCount != 0 {
		t.Fatalf("platform account count = %d, want compensating delete", accountCount)
	}
	var resourceCount int64
	if err := db.Model(&model.Resource{}).
		Where("type = ?", model.ResourceTypePlatformAccount).
		Count(&resourceCount).Error; err != nil {
		t.Fatalf("count platform resources: %v", err)
	}
	if resourceCount != 0 {
		t.Fatalf("platform resource count = %d, want compensating delete", resourceCount)
	}
}

type cancelAfterPlatformAccountCreateRepository struct {
	adminPlatformAccountRepository
	cancelRequest     context.CancelFunc
	deleteCalled      bool
	deleteContextErr  error
	deleteDeadline    time.Time
	deleteHadDeadline bool
}

func (r *cancelAfterPlatformAccountCreateRepository) AddPlatformAccount(
	ctx context.Context,
	account model.PlatformAccount,
) (store.PlatformAccountView, error) {
	view, err := r.adminPlatformAccountRepository.AddPlatformAccount(ctx, account)
	if err == nil {
		r.cancelRequest()
	}
	return view, err
}

func (r *cancelAfterPlatformAccountCreateRepository) DeletePlatformAccount(ctx context.Context, id string) error {
	r.deleteCalled = true
	r.deleteContextErr = ctx.Err()
	r.deleteDeadline, r.deleteHadDeadline = ctx.Deadline()
	return r.adminPlatformAccountRepository.DeletePlatformAccount(ctx, id)
}
