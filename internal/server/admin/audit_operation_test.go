package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

func TestRecordOperationPersistsMutationMetadata(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	s := &Server{
		cfg:    &config.Config{},
		db:     db,
		audit:  store.NewDBStore(db),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	r := httptest.NewRequest("PUT", "/api/db/instances/instance-1", nil)
	r.RemoteAddr = "192.0.2.10:54321"
	ctx := context.WithValue(r.Context(), ctxKeyUserID, "user-1")
	ctx = context.WithValue(ctx, ctxKeyUsername, "alice")
	s.recordOperation(r.WithContext(ctx), 200)

	var event model.AuditEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load audit event: %v", err)
	}
	if event.ActorID != "user-1" || event.Action != "update" || event.ResourceType != "db/instances" || event.ResourceID != "instance-1" {
		t.Fatalf("event = %+v", event)
	}
	if event.ClientIP != "192.0.2.10" || event.Detail == "" {
		t.Fatalf("event metadata = %+v", event)
	}
	if event.Phase != "result" || event.Result != "success" || event.StatusCode != http.StatusOK {
		t.Fatalf("structured audit result = %+v", event)
	}
}

func TestOperationActionClassifiesDiagnosticsAsTest(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/system-settings/diagnostics/object-storage",
		nil,
	)
	if action := operationAction(request); action != "test" {
		t.Fatalf("operationAction() = %q, want test", action)
	}
}

func TestOperationKeyMapsBusinessActions(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{http.MethodPost, "/api/ai/resources/host_account/acc-1/credentials", operationIssueCredential},
		{http.MethodPost, "/api/ai/resources/database_account/db-1/session", operationIssueSession},
		{http.MethodPost, "/api/ai/auth/refresh", operationRefreshAIToken},
		{http.MethodPost, "/api/db/accounts/test/acc-1", operationTestConnection},
		{http.MethodPost, "/api/targets/test-connection", operationTestConnection},
		{http.MethodPost, "/api/containers/test", operationTestConnection},
		{http.MethodPost, "/api/web-terminal/tickets", operationIssueTerminalTicket},
		{http.MethodPost, "/api/sql-console/sessions", operationOpenSQLConsole},
		{http.MethodPost, "/api/sql-console/sessions/s-1/execute", operationExecuteSQL},
		{http.MethodDelete, "/api/sql-console/sessions/s-1", operationCloseSQLConsole},
		{http.MethodPost, "/api/resource-grants", operationGrantAccess},
		{http.MethodPost, "/api/resource-grants/batch", operationGrantAccess},
		{http.MethodDelete, "/api/resource-grants/g-1", operationRevokeAccess},
		{http.MethodPut, "/api/system-settings", operationUpdateSettings},
		{http.MethodPut, "/api/me/preferences", operationUpdatePreferences},
		{http.MethodPost, "/api/online-sessions/s-1/disconnect", operationDisconnectSession},
		{http.MethodPost, "/api/connection-passwords", operationIssueCredential},
		// 通用回退：动词:资源族
		{http.MethodPost, "/api/users", "create:user"},
		{http.MethodPut, "/api/hosts/host-1", "update:host"},
		{http.MethodDelete, "/api/db/instances/i-1", "delete:database_instance"},
		{http.MethodPatch, "/api/rbac/roles/r-1", "update:role"},
		{http.MethodPost, "/api/user-groups", "create:user_group"},
	}
	for _, item := range cases {
		request := httptest.NewRequest(item.method, item.path, nil)
		if got := operationKey(request); got != item.want {
			t.Errorf("%s %s operationKey() = %q, want %q", item.method, item.path, got, item.want)
		}
	}
}

func TestOperationActorChannelSeparatesAIFromConsole(t *testing.T) {
	aiRequest := httptest.NewRequest(http.MethodPost, "/api/ai/resources/host_account/acc-1/credentials", nil)
	aiContext := contextWithString(aiRequest.Context(), ctxKeyUserID, "user-1")
	aiContext = contextWithString(aiContext, ctxKeyAITokenID, "token-1")
	if got := operationActorChannel(aiRequest.WithContext(aiContext)); got != actorChannelAIAPI {
		t.Fatalf("AI request channel = %q, want %q", got, actorChannelAIAPI)
	}

	consoleRequest := httptest.NewRequest(http.MethodPost, "/api/users", nil)
	consoleContext := context.WithValue(consoleRequest.Context(), ctxKeyUserID, "user-2")
	if got := operationActorChannel(consoleRequest.WithContext(consoleContext)); got != actorChannelAdminConsole {
		t.Fatalf("console request channel = %q, want %q", got, actorChannelAdminConsole)
	}

	anonymous := httptest.NewRequest(http.MethodPost, "/api/users", nil)
	if got := operationActorChannel(anonymous); got != "" {
		t.Fatalf("anonymous request channel = %q, want empty", got)
	}
}

func TestRecordOperationStoresBusinessActionChannelAndTargetLabel(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	s := &Server{
		cfg:    &config.Config{},
		db:     db,
		audit:  store.NewDBStore(db),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	handler := s.withOperationAudit(func(w http.ResponseWriter, r *http.Request) {
		setOperationAuditMetadata(r, map[string]string{
			operationAuditTargetLabelKey: "47.107.41.233:22 · root",
		})
		w.WriteHeader(http.StatusCreated)
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/ai/resources/host_account/acc-1/credentials",
		nil,
	)
	r := request.WithContext(contextWithString(request.Context(), ctxKeyUserID, "user-1"))
	r = r.WithContext(contextWithString(r.Context(), ctxKeyUsername, "alice"))
	r = r.WithContext(contextWithString(r.Context(), ctxKeyAITokenID, "token-1"))
	response := httptest.NewRecorder()
	handler(response, r)

	var event model.AuditEvent
	if err := db.Where("phase = ?", "result").First(&event).Error; err != nil {
		t.Fatalf("load audit event: %v", err)
	}
	if event.Operation != operationIssueCredential {
		t.Fatalf("operation = %q, want %q", event.Operation, operationIssueCredential)
	}
	if event.ActorChannel != actorChannelAIAPI {
		t.Fatalf("actor channel = %q, want %q", event.ActorChannel, actorChannelAIAPI)
	}
	if event.TargetLabel != "47.107.41.233:22 · root" {
		t.Fatalf("target label = %q", event.TargetLabel)
	}
	if event.Action != "create" {
		t.Fatalf("legacy action must stay untouched, got %q", event.Action)
	}
}

func TestTrimOperationAuditLabelKeepsColumnWidth(t *testing.T) {
	if got := trimOperationAuditLabel("  a · b  "); got != "a · b" {
		t.Fatalf("trimOperationAuditLabel() = %q", got)
	}
	long := strings.Repeat("测", 300)
	if got := trimOperationAuditLabel(long); len([]rune(got)) != 255 {
		t.Fatalf("long label runes = %d, want 255", len([]rune(got)))
	}
}

func TestOperationErrorMessageExtractsReadableReason(t *testing.T) {
	envelope := `{"code":504,"error":{"code":"upstream_timeout","message":"连接超时","details":null},"request_id":"r-1"}`
	if got := operationErrorMessage([]byte(envelope)); got != "连接超时" {
		t.Fatalf("operationErrorMessage() = %q, want 连接超时", got)
	}
	if got := operationErrorMessage([]byte("  upstream closed the connection  ")); got != "upstream closed the connection" {
		t.Fatalf("plain text reason = %q", got)
	}
	// 成功响应、空响应和结构化但无 message 的正文都不该被当成原因。
	if got := operationErrorMessage([]byte(`{"items":[]}`)); got != "" {
		t.Fatalf("structured body without message = %q", got)
	}
	if got := operationErrorMessage(nil); got != "" {
		t.Fatalf("empty body = %q", got)
	}
	long := strings.Repeat("错", 500)
	if got := operationErrorMessage([]byte(long)); len([]rune(got)) != maxAuditErrorMessageRunes {
		t.Fatalf("long reason runes = %d, want %d", len([]rune(got)), maxAuditErrorMessageRunes)
	}
}

func TestRecordOperationStoresFailureReason(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	s := &Server{
		cfg:    &config.Config{},
		db:     db,
		audit:  store.NewDBStore(db),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	handler := s.withOperationAudit(func(w http.ResponseWriter, r *http.Request) {
		s.writeErrorText(w, r, http.StatusBadGateway, "上游连接超时")
	})
	request := httptest.NewRequest(http.MethodPost, "/api/targets/test-connection", nil)
	ctx := context.WithValue(request.Context(), ctxKeyUserID, "user-1")
	handler(httptest.NewRecorder(), request.WithContext(ctx))

	var event model.AuditEvent
	if err := db.Where("phase = ?", "result").First(&event).Error; err != nil {
		t.Fatalf("load audit event: %v", err)
	}
	if event.Result != "failure" || event.StatusCode != http.StatusBadGateway {
		t.Fatalf("event = %#v", event)
	}
	if event.Operation != operationTestConnection {
		t.Fatalf("operation = %q, want %q", event.Operation, operationTestConnection)
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(event.Detail), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail["error_message"] != "上游连接超时" {
		t.Fatalf("error_message = %v", detail["error_message"])
	}
}

func TestHostIdentityRefreshAuditRecordsOnlyFingerprintEvidence(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	s := &Server{
		cfg:    &config.Config{},
		db:     db,
		audit:  store.NewDBStore(db),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	handler := s.withOperationAudit(func(w http.ResponseWriter, r *http.Request) {
		setOperationAuditMetadata(r, map[string]string{
			"old_fingerprint":      "SHA256:old",
			"expected_fingerprint": "SHA256:expected",
			"actual_fingerprint":   "SHA256:actual",
		})
		w.WriteHeader(http.StatusConflict)
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/hosts/host-1/refresh-identity",
		nil,
	)
	ctx := context.WithValue(request.Context(), ctxKeyUserID, "operator")
	ctx = context.WithValue(ctx, ctxKeyUsername, "alice")
	response := httptest.NewRecorder()
	handler(response, request.WithContext(ctx))

	var events []model.AuditEvent
	if err := db.Order("created_at ASC").Find(&events).Error; err != nil {
		t.Fatalf("load audit events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("audit event count = %d, want 2", len(events))
	}
	result := events[1]
	if result.Action != "refresh" || result.ResourceType != "hosts" || result.ResourceID != "host-1" {
		t.Fatalf("refresh audit identity = %#v", result)
	}
	if events[0].Phase != "intent" || events[0].Result != "pending" ||
		result.Phase != "result" || result.Result != "failure" ||
		result.IntentID != events[0].ID || result.StatusCode != http.StatusConflict {
		t.Fatalf("refresh structured audit pair = %#v", events)
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(result.Detail), &detail); err != nil {
		t.Fatalf("decode audit detail: %v", err)
	}
	if detail["old_fingerprint"] != "SHA256:old" ||
		detail["expected_fingerprint"] != "SHA256:expected" ||
		detail["actual_fingerprint"] != "SHA256:actual" {
		t.Fatalf("refresh audit detail = %#v", detail)
	}
	if _, leaked := detail["known_hosts"]; leaked {
		t.Fatalf("refresh audit leaked known_hosts: %#v", detail)
	}
}
