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
	"jianmen/internal/store"

	"gorm.io/gorm"
)

type operationAuditTestRepository struct {
	adminAuditRepository
	createErr error
	failCalls map[int]error
	calls     int
	attempts  []model.AuditEvent
	events    []model.AuditEvent
}

func (r *operationAuditTestRepository) CreateAuditEvent(_ context.Context, event *model.AuditEvent) error {
	r.calls++
	r.attempts = append(r.attempts, *event)
	err := r.createErr
	if callErr := r.failCalls[r.calls]; callErr != nil {
		err = callErr
	}
	if err != nil {
		return err
	}
	r.events = append(r.events, *event)
	return nil
}

type loginAuditTestRepository struct {
	adminAuditRepository
	createErr error
	failCalls map[int]error
	calls     int
	attempts  []model.LoginAuditLog
	logs      []model.LoginAuditLog
}

func (r *loginAuditTestRepository) CreateLoginAuditLog(_ context.Context, entry *model.LoginAuditLog) error {
	r.calls++
	r.attempts = append(r.attempts, *entry)
	err := r.createErr
	if callErr := r.failCalls[r.calls]; callErr != nil {
		err = callErr
	}
	if err != nil {
		return err
	}
	r.logs = append(r.logs, *entry)
	return nil
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

func TestLoginIntentAuditFailureLeavesStateAndSessionUntouched(t *testing.T) {
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
	if err := db.First(&user, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if user.MySQLNativeHash != "" || user.LastLoginAt != nil {
		t.Fatalf("login state changed before audit intent: hash=%q last_login=%v", user.MySQLNativeHash, user.LastLoginAt)
	}
	if len(audit.logs) != 0 || len(audit.attempts) != 1 || audit.attempts[0].Outcome != loginAuditOutcomePending {
		t.Fatalf("login audit persistence=%#v attempts=%#v", audit.logs, audit.attempts)
	}
}

func TestLoginPersistsPendingThenSuccessBeforeCookie(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	passwordHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "two-phase-user", Username: "two-phase-user", PasswordHash: passwordHash, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	audit := &loginAuditTestRepository{adminAuditRepository: server.audit}
	server.audit = audit

	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
		`{"username":"two-phase-user","password":"correct-password","captcha_payload":"verified"}`,
	))
	response := httptest.NewRecorder()
	server.handleLogin(response, request)

	if response.Code != http.StatusOK || len(response.Result().Cookies()) != 1 {
		t.Fatalf("login response = %d cookies=%#v body=%s", response.Code, response.Result().Cookies(), response.Body.String())
	}
	if len(audit.logs) != 2 ||
		audit.logs[0].Outcome != loginAuditOutcomePending ||
		audit.logs[1].Outcome != "success" ||
		audit.logs[0].Phase != "intent" ||
		audit.logs[0].Result != loginAuditOutcomePending ||
		audit.logs[1].Phase != "result" ||
		audit.logs[1].Result != "success" ||
		audit.logs[1].IntentID != audit.logs[0].ID ||
		audit.logs[1].Reason != "" ||
		audit.logs[1].StatusCode != http.StatusOK {
		t.Fatalf("two-phase login audit = %#v", audit.logs)
	}
	if err := db.First(&user, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if user.MySQLNativeHash == "" || user.LastLoginAt == nil {
		t.Fatalf("successful login state = hash=%q last_login=%v", user.MySQLNativeHash, user.LastLoginAt)
	}
	var activeSessions int64
	if err := db.Model(&model.AdminSession{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Count(&activeSessions).Error; err != nil {
		t.Fatal(err)
	}
	if activeSessions != 1 {
		t.Fatalf("active browser sessions = %d, want 1", activeSessions)
	}
}

func TestLoginResultAuditFailureRevokesSessionAndNeverPersistsSuccess(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	passwordHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "result-failure-user", Username: "result-failure-user", PasswordHash: passwordHash, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	audit := &loginAuditTestRepository{
		adminAuditRepository: server.audit,
		failCalls:            map[int]error{2: errors.New("injected success audit failure")},
	}
	server.audit = audit

	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
		`{"username":"result-failure-user","password":"correct-password","captcha_payload":"verified"}`,
	))
	response := httptest.NewRecorder()
	server.handleLogin(response, request)

	if response.Code != http.StatusServiceUnavailable || len(response.Result().Cookies()) != 0 {
		t.Fatalf("login response = %d cookies=%#v body=%s", response.Code, response.Result().Cookies(), response.Body.String())
	}
	var activeSessions, allSessions int64
	if err := db.Model(&model.AdminSession{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Count(&activeSessions).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AdminSession{}).Where("user_id = ?", user.ID).Count(&allSessions).Error; err != nil {
		t.Fatal(err)
	}
	if activeSessions != 0 || allSessions != 1 {
		t.Fatalf("browser sessions active=%d total=%d, want revoked compensation", activeSessions, allSessions)
	}
	for _, entry := range audit.logs {
		if entry.Outcome == "success" {
			t.Fatalf("persisted false success login audit: %#v", audit.logs)
		}
	}
	if len(audit.logs) != 2 || audit.logs[0].Outcome != loginAuditOutcomePending || audit.logs[1].Outcome != "failure" {
		t.Fatalf("persisted login audit = %#v; attempts=%#v", audit.logs, audit.attempts)
	}
}

func TestLoginSessionCreationFailureAppendsFailureResult(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	passwordHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "session-failure-user", Username: "session-failure-user", PasswordHash: passwordHash, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	failingSessions, err := service.NewBrowserSessionService(failCreateBrowserSessionRepository{
		BrowserSessionRepository: store.NewDBStore(db),
	})
	if err != nil {
		t.Fatal(err)
	}
	server.browserSessions = failingSessions
	replaceTestAdminAuth(t, server, db, failingSessions)
	audit := &loginAuditTestRepository{adminAuditRepository: server.audit}
	server.audit = audit

	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
		`{"username":"session-failure-user","password":"correct-password","captcha_payload":"verified"}`,
	))
	response := httptest.NewRecorder()
	server.handleLogin(response, request)

	if response.Code != http.StatusInternalServerError || len(response.Result().Cookies()) != 0 {
		t.Fatalf("login response = %d cookies=%#v body=%s", response.Code, response.Result().Cookies(), response.Body.String())
	}
	if len(audit.logs) != 2 ||
		audit.logs[0].Outcome != loginAuditOutcomePending ||
		audit.logs[1].Outcome != "failure" ||
		!strings.Contains(audit.logs[1].Reason, "session_create_failed") {
		t.Fatalf("login audit = %#v", audit.logs)
	}
	var sessions int64
	if err := db.Model(&model.AdminSession{}).Where("user_id = ?", user.ID).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("browser sessions = %d, want 0", sessions)
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
	if len(audit.logs) != 0 || len(audit.attempts) != 1 || audit.attempts[0].Outcome != "failure" {
		t.Fatalf("login audit persistence=%#v attempts=%#v", audit.logs, audit.attempts)
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
	if len(events) != 2 {
		t.Fatalf("refresh audit events = %#v", events)
	}
	var intent, result *model.AuditEvent
	for _, event := range events {
		event := event
		if strings.Contains(event.Detail+event.ActorID+event.ResourceID, refreshToken) ||
			strings.Contains(event.Detail+event.ActorID+event.ResourceID, issued.AccessToken) ||
			strings.Contains(event.Detail+event.ActorID+event.ResourceID, issued.RefreshToken) {
			t.Fatalf("refresh audit exposed plaintext token: %#v", event)
		}
		if strings.Contains(event.Detail, `"phase":"intent"`) {
			intent = &event
		}
		if strings.Contains(event.Detail, `"phase":"result"`) {
			result = &event
		}
	}
	fingerprint := service.HashAIAccessToken(refreshToken)
	if intent == nil || intent.ActorID != fingerprint || intent.ResourceType != "ai_access_token" || intent.ResourceID != fingerprint {
		t.Fatalf("refresh intent identity = %#v", intent)
	}
	if result == nil || result.ActorID != user.ID || result.ResourceID != token.ID ||
		!strings.Contains(result.Detail, fingerprint) || !strings.Contains(result.Detail, intent.ID) {
		t.Fatalf("refresh result identity = %#v intent=%#v", result, intent)
	}
}

func TestAIRefreshIntentFailureDoesNotRotateToken(t *testing.T) {
	server, db, refreshToken, token := newAIRefreshAuditTestServer(t)
	audit := &operationAuditTestRepository{
		adminAuditRepository: server.audit,
		createErr:            errors.New("injected refresh intent failure"),
	}
	server.audit = audit

	request := httptest.NewRequest(http.MethodPost, "/api/ai/auth/refresh", bytes.NewBufferString(
		`{"refresh_token":"`+refreshToken+`"}`,
	))
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable ||
		strings.Contains(response.Body.String(), "access_token") ||
		strings.Contains(response.Body.String(), "refresh_token") {
		t.Fatalf("refresh response = %d body=%s", response.Code, response.Body.String())
	}
	var stored model.AIAccessToken
	if err := db.First(&stored, "id = ?", token.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.RefreshTokenHash != token.RefreshTokenHash {
		t.Fatal("refresh token rotated after audit intent failure")
	}
	if len(audit.attempts) != 1 || audit.attempts[0].ResourceID != service.HashAIAccessToken(refreshToken) {
		t.Fatalf("refresh audit attempts = %#v", audit.attempts)
	}
}

func TestAIRefreshResultAuditFailureDoesNotReleaseRotatedPlaintext(t *testing.T) {
	server, _, refreshToken, token := newAIRefreshAuditTestServer(t)
	audit := &operationAuditTestRepository{
		adminAuditRepository: server.audit,
		failCalls:            map[int]error{2: errors.New("injected refresh result failure")},
	}
	server.audit = audit

	request := httptest.NewRequest(http.MethodPost, "/api/ai/auth/refresh", bytes.NewBufferString(
		`{"refresh_token":"`+refreshToken+`"}`,
	))
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable ||
		strings.Contains(response.Body.String(), "access_token") ||
		strings.Contains(response.Body.String(), "refresh_token") {
		t.Fatalf("refresh response = %d body=%s", response.Code, response.Body.String())
	}
	if len(audit.events) != 2 ||
		!strings.Contains(audit.events[0].Detail, `"phase":"intent"`) ||
		!strings.Contains(audit.events[1].Detail, `"reason":"success_audit_failed"`) ||
		audit.events[1].ResourceID != token.ID {
		t.Fatalf("refresh persisted audit = %#v attempts=%#v", audit.events, audit.attempts)
	}
	for _, event := range audit.attempts {
		if strings.Contains(event.Detail, refreshToken) {
			t.Fatalf("refresh audit attempt exposed plaintext: %#v", event)
		}
	}
}

func TestAIResourceCredentialAndSessionRoutesFailClosedAfterTokenIdentity(t *testing.T) {
	server, db, accessToken, userID := newAIResourceAuditGateTestServer(t)
	for _, suffix := range []string{"credentials", "session"} {
		t.Run(suffix, func(t *testing.T) {
			audit := &operationAuditTestRepository{
				adminAuditRepository: server.audit,
				createErr:            errors.New("injected AI resource audit failure"),
			}
			server.audit = audit
			path := "/api/ai/resources/host_account/account-under-audit/" + suffix
			request := httptest.NewRequest(http.MethodPost, path, nil)
			request.Header.Set("Authorization", "Bearer "+accessToken)
			response := httptest.NewRecorder()
			server.routes().ServeHTTP(response, request)

			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
			}
			if len(audit.attempts) != 1 {
				t.Fatalf("audit attempts = %#v", audit.attempts)
			}
			event := audit.attempts[0]
			if event.ActorID != userID || event.ResourceType != model.ResourceTypeHostAccount ||
				event.ResourceID != "account-under-audit" || strings.Contains(event.Detail, accessToken) {
				t.Fatalf("AI resource audit identity = %#v", event)
			}
		})
	}
	var passwords, sessions int64
	if err := db.Model(&model.ConnectionPassword{}).Count(&passwords).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.UserSession{}).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if passwords != 0 || sessions != 0 {
		t.Fatalf("credentials/sessions issued despite audit failure: passwords=%d sessions=%d", passwords, sessions)
	}
}

func TestAIResourceOperationPathUsesActualResourceIdentity(t *testing.T) {
	path := "/api/ai/resources/database_account/database-account-1/credentials"
	if got := operationResourceType(path); got != model.ResourceTypeDatabaseAccount {
		t.Fatalf("resource type = %q, want %q", got, model.ResourceTypeDatabaseAccount)
	}
	if got := operationResourceID(path); got != "database-account-1" {
		t.Fatalf("resource id = %q, want database-account-1", got)
	}
}

func newAIRefreshAuditTestServer(t *testing.T) (*Server, *gorm.DB, string, model.AIAccessToken) {
	t.Helper()
	server, db := newAdminDBTestServer(t)
	user := model.User{ID: "refresh-failure-user", Username: "refresh-failure-user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	temporaryAccount := model.TemporaryAccount{
		ID: "refresh-failure-temporary", SessionID: "rff01", Type: model.TemporaryAccountTypeAI,
		Username: "refresh-failure-ai", AuthorizedUserID: user.ID, Status: "active", StartsAt: time.Now().UTC(),
	}
	if err := db.Create(&temporaryAccount).Error; err != nil {
		t.Fatal(err)
	}
	const refreshToken = "jm_ai_rt_refresh_failure_plaintext"
	token := model.AIAccessToken{
		ID: "refresh-failure-token", UserID: user.ID, TemporaryAccountID: temporaryAccount.ID, Name: "refresh failure",
		AccessTokenHash:  service.HashAIAccessToken("refresh-failure-access"),
		RefreshTokenHash: service.HashAIAccessToken(refreshToken),
		AccessExpiresAt:  time.Now().UTC().Add(time.Hour), RefreshExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if err := db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	return server, db, refreshToken, token
}

func newAIResourceAuditGateTestServer(t *testing.T) (*Server, *gorm.DB, string, string) {
	t.Helper()
	server, db := newAdminDBTestServer(t)
	const userID = "resource-audit-user"
	user := model.User{ID: userID, Username: userID, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	temporaryAccount := model.TemporaryAccount{
		ID: "resource-audit-temporary", SessionID: "rag01", Type: model.TemporaryAccountTypeAI,
		Username: "resource-audit-ai", AuthorizedUserID: userID, Status: "active", StartsAt: time.Now().UTC(),
	}
	if err := db.Create(&temporaryAccount).Error; err != nil {
		t.Fatal(err)
	}
	issued, err := service.IssueAIAccessToken(time.Now().UTC(), time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token := model.AIAccessToken{
		ID: "resource-audit-token", UserID: userID, TemporaryAccountID: temporaryAccount.ID, Name: "resource audit",
		AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash,
		AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt,
	}
	if err := db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	return server, db, issued.AccessToken, userID
}
