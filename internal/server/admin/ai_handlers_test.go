package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

func TestHandleAITokensIssueRotateAndRevoke(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "ai-user", Username: "ai-user", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	request := withTestUser(httptest.NewRequest(http.MethodPost, "/api/ai/tokens", bytes.NewBufferString(`{"name":"ops agent","access_ttl_seconds":3600,"refresh_ttl_seconds":86400,"permanent":true}`)), "ai-user", "ai-user")
	request.Header.Set("Origin", "https://public.example.test")
	response := httptest.NewRecorder()
	server.handleAITokens(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("issue status = %d; body=%s", response.Code, response.Body.String())
	}
	var issued struct {
		ID           string `json:"id"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Prompt       string `json:"prompt"`
		CopyPrompt   string `json:"copy_prompt"`
		FullPrompt   string `json:"full_prompt"`
	}
	if err := decodeTestData(t, response.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	if issued.ID == "" || issued.AccessToken == "" || issued.RefreshToken == "" {
		t.Fatalf("missing issued credential: %#v", issued)
	}
	if issued.Prompt != "\u6388\u6743 AI \u4f7f\u7528\u5f53\u524d\u7528\u6237\u7684\u8d44\u6e90\u7684\u6743\u9650\u3002" {
		t.Fatalf("unexpected prompt: %q", issued.Prompt)
	}
	if !strings.Contains(issued.CopyPrompt, issued.AccessToken) || !strings.Contains(issued.CopyPrompt, issued.RefreshToken) || !strings.Contains(issued.CopyPrompt, "[https://public.example.test/api/ai/docs](https://public.example.test/api/ai/docs)") {
		t.Fatalf("unexpected copy prompt: %q", issued.CopyPrompt)
	}
	if !strings.Contains(issued.FullPrompt, "# Jianmen AI Bastion API") || !strings.Contains(issued.FullPrompt, "Base URL: https://public.example.test") {
		t.Fatalf("full prompt does not contain AI documentation: %q", issued.FullPrompt)
	}
	var saved model.AIAccessToken
	if err := db.First(&saved, "id = ?", issued.ID).Error; err != nil {
		t.Fatalf("load token: %v", err)
	}
	var temporaryAccount model.TemporaryAccount
	if err := db.First(&temporaryAccount, "id = ?", saved.TemporaryAccountID).Error; err != nil {
		t.Fatalf("load AI temporary account: %v", err)
	}
	if temporaryAccount.ExpiresAt != nil {
		t.Fatalf("permanent AI authorization should not expire: %v", temporaryAccount.ExpiresAt)
	}
	if strings.Contains(response.Body.String(), saved.AccessTokenHash) || strings.Contains(response.Body.String(), saved.RefreshTokenHash) {
		t.Fatal("response exposed token hashes")
	}
	if saved.AccessToken.GetPlaintext() != issued.AccessToken || saved.RefreshToken.GetPlaintext() != issued.RefreshToken {
		t.Fatal("database did not retain encrypted token values")
	}

	listRequest := withTestUser(httptest.NewRequest(http.MethodGet, "/api/ai/tokens", nil), "ai-user", "ai-user")
	listResponse := httptest.NewRecorder()
	server.handleAITokens(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), "ops agent") {
		t.Fatalf("list response = %d; body=%s", listResponse.Code, listResponse.Body.String())
	}
	if strings.Contains(listResponse.Body.String(), issued.AccessToken) || strings.Contains(listResponse.Body.String(), issued.RefreshToken) {
		t.Fatal("token list exposed plaintext credentials")
	}
	for attempt := 0; attempt < 2; attempt++ {
		detailRequest := withTestUser(httptest.NewRequest(http.MethodGet, "/api/ai/tokens/"+issued.ID, nil), "ai-user", "ai-user")
		detailResponse := httptest.NewRecorder()
		server.handleAIToken(detailResponse, detailRequest)
		if detailResponse.Code != http.StatusOK {
			t.Fatalf("detail attempt %d status = %d; body=%s", attempt, detailResponse.Code, detailResponse.Body.String())
		}
		var detail struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			HasSecret    bool   `json:"has_secret"`
		}
		if err := decodeTestData(t, detailResponse.Body.Bytes(), &detail); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		if !detail.HasSecret || detail.AccessToken != issued.AccessToken || detail.RefreshToken != issued.RefreshToken {
			t.Fatalf("unexpected repeatable token detail: %#v", detail)
		}
	}

	refreshRequest := httptest.NewRequest(http.MethodPost, "/api/ai/auth/refresh", bytes.NewBufferString(`{"refresh_token":"`+issued.RefreshToken+`"}`))
	refreshResponse := httptest.NewRecorder()
	server.handleAIRefresh(refreshResponse, refreshRequest)
	if refreshResponse.Code != http.StatusOK {
		t.Fatalf("refresh status = %d; body=%s", refreshResponse.Code, refreshResponse.Body.String())
	}
	var refreshed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeTestData(t, refreshResponse.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	if refreshed.AccessToken == issued.AccessToken || refreshed.RefreshToken == issued.RefreshToken {
		t.Fatal("refresh did not rotate credentials")
	}
	if err := db.First(&saved, "id = ?", issued.ID).Error; err != nil {
		t.Fatalf("reload refreshed token: %v", err)
	}
	if saved.AccessToken.GetPlaintext() != refreshed.AccessToken || saved.RefreshToken.GetPlaintext() != refreshed.RefreshToken {
		t.Fatal("refreshed plaintext credentials were not retained")
	}
	oldRefresh := httptest.NewRecorder()
	server.handleAIRefresh(oldRefresh, httptest.NewRequest(http.MethodPost, "/api/ai/auth/refresh", bytes.NewBufferString(`{"refresh_token":"`+issued.RefreshToken+`"}`)))
	if oldRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("old refresh status = %d; body=%s", oldRefresh.Code, oldRefresh.Body.String())
	}

	revokeRequest := withTestUser(httptest.NewRequest(http.MethodDelete, "/api/ai/tokens/"+issued.ID, nil), "ai-user", "ai-user")
	revokeResponse := httptest.NewRecorder()
	server.handleAIToken(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d; body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}
}

func TestAIResourcesRequireCurrentRBACGrantAndIssueSessionCredential(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "ai-resource-user", rbac.ActionSessionConnect)
	if err := db.Create(&model.ResourceGrant{PrincipalType: "user", PrincipalID: "ai-resource-user", ResourceType: model.ResourceTypeHostAccount, ResourceID: "ai-account", Effect: model.PermissionEffectAllow}).Error; err != nil {
		t.Fatalf("create grant: %v", err)
	}
	host := model.Host{ID: "ai-host", Name: "ai-host", Address: "10.0.0.10", Port: 22, Status: "active"}
	account := model.HostAccount{ID: "ai-account", HostID: host.ID, Name: "root account", Username: "root", AuthType: "password", Password: model.NewEncryptedField("target-secret"), Status: "active", ResourceID: "A001"}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	issued, err := service.IssueAIAccessToken(time.Now().UTC(), time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if err := db.Create(&model.AIAccessToken{ID: "ai-token", UserID: "ai-resource-user", Name: "agent", AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash, AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt}).Error; err != nil {
		t.Fatalf("create AI token: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/ai/resources", nil)
	request.Header.Set("Authorization", "Bearer "+issued.AccessToken)
	response := httptest.NewRecorder()
	server.withAIToken(server.handleAIResources)(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "ai-account") {
		t.Fatalf("resource list status = %d; body=%s", response.Code, response.Body.String())
	}

	credentialRequest := httptest.NewRequest(http.MethodPost, "/api/ai/resources/host_account/ai-account/credentials", nil)
	credentialRequest.Header.Set("Authorization", "Bearer "+issued.AccessToken)
	credentialResponse := httptest.NewRecorder()
	server.withAIToken(server.handleAIResources)(credentialResponse, credentialRequest)
	if credentialResponse.Code != http.StatusCreated {
		t.Fatalf("credential status = %d; body=%s", credentialResponse.Code, credentialResponse.Body.String())
	}
	var credential struct {
		Password string `json:"password"`
	}
	if err := decodeTestData(t, credentialResponse.Body.Bytes(), &credential); err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	if credential.Password == "" || credential.Password == "target-secret" {
		t.Fatalf("unexpected credential: %#v", credential)
	}

	sessionRequest := httptest.NewRequest(http.MethodPost, "/api/ai/resources/host_account/ai-account/session", nil)
	sessionRequest.Header.Set("Authorization", "Bearer "+issued.AccessToken)
	sessionResponse := httptest.NewRecorder()
	server.withAIToken(server.handleAIResources)(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusCreated || !strings.Contains(sessionResponse.Body.String(), "compact_username") {
		t.Fatalf("session response = %d; body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
}

func TestHandleAIDocsIsPublicMarkdown(t *testing.T) {
	server, _ := newAdminDBTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/ai/docs", nil)
	request.Host = "bastion.example.test"
	response := httptest.NewRecorder()
	server.handleAIDocs(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("docs response = %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	if !strings.Contains(response.Body.String(), "bastion.example.test") || !strings.Contains(response.Body.String(), "/api/ai/auth/refresh") {
		t.Fatalf("docs missing base URL or refresh endpoint")
	}
}
