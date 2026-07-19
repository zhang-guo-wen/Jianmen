package admin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

type operationAuditTestRepository struct {
	adminAuditRepository
	createErr error
	events    []model.AuditEvent
}

func (r *operationAuditTestRepository) CreateAuditEvent(_ context.Context, event *model.AuditEvent) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.events = append(r.events, *event)
	return nil
}

type loginAuditTestRepository struct {
	adminAuditRepository
	createErr error
	logs      []model.LoginAuditLog
}

func (r *loginAuditTestRepository) CreateLoginAuditLog(_ context.Context, entry *model.LoginAuditLog) error {
	r.logs = append(r.logs, *entry)
	return r.createErr
}

func TestOperationAuditGateStopsMutationWhenIntentWriteFails(t *testing.T) {
	repository := &operationAuditTestRepository{createErr: errors.New("injected audit failure")}
	server := &Server{audit: repository}
	called := false
	handler := server.withOperationAudit(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	handler(response, httptest.NewRequest(http.MethodPost, "/api/hosts", nil))

	if called {
		t.Fatal("mutation handler ran after the audit intent write failed")
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestOperationAuditGatePersistsIntentBeforeSuccessfulMutation(t *testing.T) {
	repository := &operationAuditTestRepository{}
	server := &Server{audit: repository}
	handler := server.withOperationAudit(func(w http.ResponseWriter, _ *http.Request) {
		if len(repository.events) != 1 || !strings.Contains(repository.events[0].Detail, `"phase":"intent"`) {
			t.Fatalf("mutation ran without a persisted audit intent: %#v", repository.events)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	handler(response, httptest.NewRequest(http.MethodDelete, "/api/hosts/host-1", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if len(repository.events) != 2 {
		t.Fatalf("audit events = %d, want intent and result", len(repository.events))
	}
	if !strings.Contains(repository.events[1].Detail, `"phase":"result"`) ||
		!strings.Contains(repository.events[1].Detail, repository.events[0].ID) {
		t.Fatalf("result event is not linked to intent: %#v", repository.events)
	}
}

func TestSuccessfulLoginFailsClosedBeforeSessionWhenAuditWriteFails(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	passwordHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "audit-gated-user", Username: "audit-gated-user", PasswordHash: passwordHash, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	audit := &loginAuditTestRepository{
		adminAuditRepository: server.audit,
		createErr:            errors.New("injected login audit failure"),
	}
	server.audit = audit

	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
		`{"username":"audit-gated-user","password":"correct-password","captcha_payload":"verified"}`,
	))
	response := httptest.NewRecorder()
	server.handleLogin(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatalf("login audit failure returned cookies: %#v", response.Result().Cookies())
	}
	var sessions int64
	if err := db.Model(&model.AdminSession{}).Where("user_id = ?", user.ID).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("persisted browser sessions = %d, want 0", sessions)
	}
	if len(audit.logs) != 1 || audit.logs[0].Outcome != "success" {
		t.Fatalf("login audit attempts = %#v", audit.logs)
	}
}

func TestFailedLoginAuditRemainsBestEffort(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	passwordHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{
		ID: "rejected-user", Username: "rejected-user", PasswordHash: passwordHash, Status: "active",
	}).Error; err != nil {
		t.Fatal(err)
	}
	audit := &loginAuditTestRepository{
		adminAuditRepository: server.audit,
		createErr:            errors.New("injected login audit failure"),
	}
	server.audit = audit

	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
		`{"username":"rejected-user","password":"wrong-password","captcha_payload":"verified"}`,
	))
	response := httptest.NewRecorder()
	server.handleLogin(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
	if len(audit.logs) != 1 || audit.logs[0].Outcome != "failure" {
		t.Fatalf("login audit attempts = %#v", audit.logs)
	}
}

func TestAIRefreshRouteIsAuditGatedWithoutPlaintextTokens(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	user := model.User{ID: "refresh-user", Username: "refresh-user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	temporaryAccount := model.TemporaryAccount{
		ID: "refresh-temporary", SessionID: "rf001", Type: model.TemporaryAccountTypeAI,
		Username: "refresh-audit-user", AuthorizedUserID: user.ID, Status: "active", StartsAt: time.Now().UTC(),
	}
	if err := db.Create(&temporaryAccount).Error; err != nil {
		t.Fatal(err)
	}
	const refreshToken = "jm_ai_rt_plaintext_must_not_be_audited"
	token := model.AIAccessToken{
		ID: "refresh-token-id", UserID: user.ID, TemporaryAccountID: temporaryAccount.ID, Name: "refresh audit",
		AccessTokenHash:  service.HashAIAccessToken("old-access"),
		RefreshTokenHash: service.HashAIAccessToken(refreshToken),
		AccessExpiresAt:  time.Now().UTC().Add(time.Hour),
		RefreshExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if err := db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/ai/auth/refresh",
		bytes.NewBufferString(`{"refresh_token":"`+refreshToken+`"}`),
	)
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var issued struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeTestData(t, response.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	var events []model.AuditEvent
	if err := db.Order("created_at ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Action != "refresh" {
		t.Fatalf("refresh audit events = %#v", events)
	}
	for _, event := range events {
		if strings.Contains(event.Detail, refreshToken) ||
			strings.Contains(event.Detail, issued.AccessToken) ||
			strings.Contains(event.Detail, issued.RefreshToken) {
			t.Fatalf("refresh audit exposed plaintext token: %#v", event)
		}
	}
}
