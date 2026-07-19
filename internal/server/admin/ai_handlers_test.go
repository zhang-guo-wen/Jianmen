package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

func TestAIAccessTokenIssueRotateAndRevoke(t *testing.T) {
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
		DocsURL      string `json:"docs_url"`
		DocsContent  string `json:"docs_content"`
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
	if strings.Contains(issued.CopyPrompt, issued.AccessToken) || strings.Contains(issued.CopyPrompt, issued.RefreshToken) || !strings.Contains(issued.CopyPrompt, "<access_token>") || !strings.Contains(issued.CopyPrompt, "<refresh_token>") || !strings.Contains(issued.CopyPrompt, "[https://public.example.test/api/ai/docs](https://public.example.test/api/ai/docs)") {
		t.Fatalf("unexpected copy prompt: %q", issued.CopyPrompt)
	}
	if strings.Contains(issued.FullPrompt, issued.AccessToken) || strings.Contains(issued.FullPrompt, issued.RefreshToken) || !strings.Contains(issued.FullPrompt, "# Jianmen AI Bastion API") || !strings.Contains(issued.FullPrompt, "Base URL: https://public.example.test") {
		t.Fatalf("full prompt does not contain AI documentation: %q", issued.FullPrompt)
	}
	if issued.DocsURL != "https://public.example.test/api/ai/docs" || !strings.Contains(issued.DocsContent, "# Jianmen AI Bastion API") {
		t.Fatalf("unexpected AI documentation payload: url=%q content=%q", issued.DocsURL, issued.DocsContent)
	}
	var saved model.AIAccessToken
	if err := db.First(&saved, "id = ?", issued.ID).Error; err != nil {
		t.Fatalf("load token: %v", err)
	}
	var temporaryAccount model.TemporaryAccount
	if err := db.First(&temporaryAccount, "id = ?", saved.TemporaryAccountID).Error; err != nil {
		t.Fatalf("load AI temporary account: %v", err)
	}
	if len(temporaryAccount.SessionID) != 5 {
		t.Fatalf("AI temporary session ID length = %d, want 5: %q", len(temporaryAccount.SessionID), temporaryAccount.SessionID)
	}
	if temporaryAccount.ExpiresAt != nil {
		t.Fatalf("permanent AI authorization should not expire: %v", temporaryAccount.ExpiresAt)
	}
	if strings.Contains(response.Body.String(), saved.AccessTokenHash) || strings.Contains(response.Body.String(), saved.RefreshTokenHash) {
		t.Fatal("response exposed token hashes")
	}
	if saved.AccessTokenHash == "" || saved.RefreshTokenHash == "" {
		t.Fatal("database did not retain token hashes")
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
			HasSecret bool `json:"has_secret"`
		}
		if err := decodeTestData(t, detailResponse.Body.Bytes(), &detail); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		if detail.HasSecret || strings.Contains(detailResponse.Body.String(), "access_token") || strings.Contains(detailResponse.Body.String(), "refresh_token") || strings.Contains(detailResponse.Body.String(), issued.AccessToken) || strings.Contains(detailResponse.Body.String(), issued.RefreshToken) {
			t.Fatalf("token detail exposed a secret: %s", detailResponse.Body.String())
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
	if saved.AccessTokenHash == service.HashAIAccessToken(issued.AccessToken) || saved.RefreshTokenHash == service.HashAIAccessToken(issued.RefreshToken) {
		t.Fatal("database did not replace token hashes")
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

func TestHandleAITokenCreationRollsBackTemporaryAccountWhenTokenInsertFails(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "ai-user", Username: "ai-user", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_ai_token_insert BEFORE INSERT ON ai_access_tokens BEGIN SELECT RAISE(ABORT, 'injected AI token failure'); END;`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	request := withTestUser(httptest.NewRequest(
		http.MethodPost,
		"/api/ai/tokens",
		bytes.NewBufferString(`{"name":"atomic agent","access_ttl_seconds":3600,"refresh_ttl_seconds":86400,"permanent":true}`),
	), "ai-user", "ai-user")
	response := httptest.NewRecorder()
	server.handleAITokens(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", response.Code, response.Body.String())
	}
	for _, table := range []string{"temporary_accounts", "ai_access_tokens"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after token insert failure = %d, want 0", table, count)
		}
	}
}

func TestAIResourcesRequireCurrentRBACGrantAndIssueSessionCredential(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "ai-resource-user", rbac.ActionSessionConnect)
	if err := db.Create(&model.ResourceGrant{PrincipalType: "user", PrincipalID: "ai-resource-user", ResourceType: model.ResourceTypeHostAccount, ResourceID: "ai-account", Effect: model.PermissionEffectAllow}).Error; err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if err := db.Create(&model.TemporaryAccount{ID: "ai-temporary", SessionID: "ai001", Type: "ai", Username: "ai-resource-user", AuthorizedUserID: "ai-resource-user", Status: "active", StartsAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("create AI temporary account: %v", err)
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
	if err := db.Create(&model.AIAccessToken{ID: "ai-token", UserID: "ai-resource-user", TemporaryAccountID: "ai-temporary", Name: "agent", AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash, AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt}).Error; err != nil {
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

func TestAIResourceSessionPreservesHostDatabaseAndRedisUsernameSemantics(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	const userID = "ai-resource-prefix-user"
	if err := db.Create(&model.User{
		ID: userID, Username: userID, Status: "active", IsSuperAdmin: true,
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	host := model.Host{ID: "prefix-host", Name: "prefix-host", Address: "10.0.0.20", Port: 22, Status: "active"}
	hostAccount := model.HostAccount{
		ID: "prefix-host-account", HostID: host.ID, Name: "root", Username: "root",
		AuthType: "password", Password: model.NewEncryptedField("host-secret"),
		Status: "active", ResourceSeq: 1, ResourceID: "H001",
	}
	mysqlInstance := model.DatabaseInstance{
		ID: "prefix-mysql", Name: "prefix-mysql", Protocol: "mysql",
		Address: "10.0.0.21", Port: 3306, TLSMode: "disable", Status: "active",
	}
	redisInstance := model.DatabaseInstance{
		ID: "prefix-redis", Name: "prefix-redis", Protocol: "redis",
		Address: "10.0.0.22", Port: 6379, TLSMode: "disable", Status: "active",
	}
	mysqlAccount := model.DatabaseAccount{
		ID: "prefix-mysql-account", InstanceID: mysqlInstance.ID, UniqueName: "prefix-mysql-account",
		Username: "mysql-user", Password: model.NewEncryptedField("mysql-secret"),
		Status: "active", ResourceSeq: 1, ResourceID: "D001",
	}
	redisAccount := model.DatabaseAccount{
		ID: "prefix-redis-account", InstanceID: redisInstance.ID, UniqueName: "prefix-redis-account",
		Username: "redis-user", Password: model.NewEncryptedField("redis-secret"),
		Status: "active", ResourceSeq: 2, ResourceID: "D002",
	}
	for _, value := range []any{&host, &hostAccount, &mysqlInstance, &redisInstance, &mysqlAccount, &redisAccount} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("create resource %T: %v", value, err)
		}
	}

	tests := []struct {
		name         string
		resourceType string
		resourceID   string
		wantPrefix   string
	}{
		{name: "host", resourceType: model.ResourceTypeHostAccount, resourceID: hostAccount.ID, wantPrefix: "HH001"},
		{name: "database", resourceType: model.ResourceTypeDatabaseAccount, resourceID: mysqlAccount.ID, wantPrefix: "DD001"},
		{name: "redis", resourceType: model.ResourceTypeDatabaseAccount, resourceID: redisAccount.ID, wantPrefix: "RD002"},
	}
	var sharedSessionID string
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := withTestUser(
				httptest.NewRequest(http.MethodPost, "/api/ai/resources/"+test.resourceType+"/"+test.resourceID+"/session", nil),
				userID,
				userID,
			)
			response := httptest.NewRecorder()
			server.issueAIResourceSession(response, request, test.resourceType, test.resourceID)
			if response.Code != http.StatusCreated {
				t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
			}
			var body struct {
				CompactUsername string `json:"compact_username"`
				SessionID       string `json:"session_id"`
			}
			if err := decodeTestData(t, response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if !strings.HasPrefix(body.CompactUsername, test.wantPrefix) {
				t.Fatalf("compact username = %q, want prefix %q", body.CompactUsername, test.wantPrefix)
			}
			if sharedSessionID == "" {
				sharedSessionID = body.SessionID
			}
			if body.SessionID != sharedSessionID ||
				body.CompactUsername != test.wantPrefix+sharedSessionID {
				t.Fatalf("session response = %#v, shared session = %q", body, sharedSessionID)
			}
		})
	}
}

func TestAIAndUserSessionEntrypointsShareAtomicPermanentSession(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	const userID = "shared-session-user"
	if err := db.Create(&model.User{
		ID: userID, Username: userID, Status: "active", IsSuperAdmin: true,
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	host := model.Host{ID: "shared-host", Name: "shared-host", Address: "10.0.0.30", Port: 22, Status: "active"}
	account := model.HostAccount{
		ID: "shared-host-account", HostID: host.ID, Name: "root", Username: "root",
		AuthType: "password", Password: model.NewEncryptedField("host-secret"),
		Status: "active", ResourceSeq: 3, ResourceID: "H003",
	}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create host account: %v", err)
	}
	creation, err := service.NewUserSessionCreationService(store.NewDBStore(db), server.authorization)
	if err != nil {
		t.Fatalf("new shared user session creation service: %v", err)
	}
	server.userSessionCreation = creation

	const callers = 16
	type responseResult struct {
		status int
		body   string
	}
	results := make([]responseResult, callers)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := 0; index < callers; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			recorder := httptest.NewRecorder()
			if index%2 == 0 {
				request := withTestUser(
					httptest.NewRequest(http.MethodPost, "/api/ai/resources/host_account/"+account.ID+"/session", nil),
					userID,
					userID,
				)
				server.issueAIResourceSession(recorder, request, model.ResourceTypeHostAccount, account.ID)
			} else {
				request := withTestUser(
					httptest.NewRequest(http.MethodPost, "/api/user-sessions", bytes.NewBufferString(`{"target_id":"`+account.ID+`"}`)),
					userID,
					userID,
				)
				server.handleUserSessions(recorder, request)
			}
			results[index] = responseResult{status: recorder.Code, body: recorder.Body.String()}
		}()
	}
	close(start)
	group.Wait()

	var sharedSessionID string
	for index, result := range results {
		if result.status != http.StatusCreated {
			t.Fatalf("request %d status = %d; body=%s", index, result.status, result.body)
		}
		var body struct {
			SessionID string `json:"session_id"`
		}
		if err := decodeTestData(t, []byte(result.body), &body); err != nil {
			t.Fatalf("decode request %d response: %v", index, err)
		}
		if sharedSessionID == "" {
			sharedSessionID = body.SessionID
		}
		if body.SessionID == "" || body.SessionID != sharedSessionID {
			t.Fatalf("request %d session ID = %q, want %q", index, body.SessionID, sharedSessionID)
		}
	}
	var count int64
	if err := db.Model(&model.UserSession{}).
		Where("user_id = ? AND type = ? AND status = ?", userID, "permanent", "active").
		Count(&count).Error; err != nil {
		t.Fatalf("count active permanent sessions: %v", err)
	}
	if count != 1 {
		t.Fatalf("active permanent session count = %d, want 1", count)
	}
}

func TestAIAccessTokenReissueRotatesExistingTokenInPlace(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "ai-reissue-user", Username: "ai-reissue-user", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	issueRequest := withTestUser(
		httptest.NewRequest(http.MethodPost, "/api/ai/tokens", bytes.NewBufferString(`{"name":"reissue-agent","permanent":true}`)),
		"ai-reissue-user",
		"ai-reissue-user",
	)
	issueResponse := httptest.NewRecorder()
	server.handleAITokens(issueResponse, issueRequest)
	if issueResponse.Code != http.StatusCreated {
		t.Fatalf("issue status = %d; body=%s", issueResponse.Code, issueResponse.Body.String())
	}
	var original struct {
		ID           string `json:"id"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeTestData(t, issueResponse.Body.Bytes(), &original); err != nil {
		t.Fatalf("decode issued token: %v", err)
	}

	reissueRequest := withTestUser(
		httptest.NewRequest(http.MethodPost, "/api/ai/tokens/"+original.ID+"/reissue", nil),
		"ai-reissue-user",
		"ai-reissue-user",
	)
	reissueResponse := httptest.NewRecorder()
	server.handleAIToken(reissueResponse, reissueRequest)
	if reissueResponse.Code != http.StatusOK {
		t.Fatalf("reissue status = %d; body=%s", reissueResponse.Code, reissueResponse.Body.String())
	}
	var reissued struct {
		ID           string `json:"id"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeTestData(t, reissueResponse.Body.Bytes(), &reissued); err != nil {
		t.Fatalf("decode reissued token: %v", err)
	}
	if reissued.ID != original.ID || reissued.AccessToken == "" || reissued.RefreshToken == "" ||
		reissued.AccessToken == original.AccessToken || reissued.RefreshToken == original.RefreshToken {
		t.Fatalf("unexpected reissued credentials: %#v", reissued)
	}
	if _, err := server.aiTokens.AuthenticateAIAccessToken(
		reissueRequest.Context(),
		service.HashAIAccessToken(original.AccessToken),
		time.Now().UTC(),
	); err == nil {
		t.Fatal("old access token remained valid after reissue")
	}
	oldRefreshRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/ai/auth/refresh",
		bytes.NewBufferString(`{"refresh_token":"`+original.RefreshToken+`"}`),
	)
	oldRefreshResponse := httptest.NewRecorder()
	server.handleAIRefresh(oldRefreshResponse, oldRefreshRequest)
	if oldRefreshResponse.Code != http.StatusUnauthorized {
		t.Fatalf("old refresh status = %d, want 401; body=%s", oldRefreshResponse.Code, oldRefreshResponse.Body.String())
	}
	var count int64
	if err := db.Model(&model.AIAccessToken{}).Where("user_id = ?", "ai-reissue-user").Count(&count).Error; err != nil {
		t.Fatalf("count AI tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("AI token count = %d, want one in-place rotated token", count)
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
