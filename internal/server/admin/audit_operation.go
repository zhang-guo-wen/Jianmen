package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"jianmen/internal/model"
	"jianmen/internal/pkg/apiresp"
)

var errOperationAuditUnavailable = errors.New("operation audit unavailable")

// 业务动作键。前端按这些键展示中文动作名，新增键时必须同步更新
// web/src/i18n 的 audit.operation.* 文案；未收录的键会退回通用「动词:资源」展示。
const (
	operationIssueCredential     = "issue_credential"
	operationIssueSession        = "issue_session"
	operationRefreshAIToken      = "refresh_ai_token"
	operationTestConnection      = "test_connection"
	operationGrantAccess         = "grant_access"
	operationRevokeAccess        = "revoke_access"
	operationUpdateSettings      = "update_settings"
	operationUpdatePreferences   = "update_preferences"
	operationIssueTerminalTicket = "issue_terminal_ticket"
	operationOpenSQLConsole      = "open_sql_console"
	operationExecuteSQL          = "execute_sql"
	operationCloseSQLConsole     = "close_sql_console"
	operationDisconnectSession   = "disconnect_session"
	operationRevealPlatformPassword = "reveal_platform_password"
)

// 调用来源：区分 AI 令牌调用和浏览器后台操作，便于筛掉高频 AI 调用。
const (
	actorChannelAdminConsole = "admin_console"
	actorChannelAIAPI        = "ai_api"
)

type operationAuditMetadataKey struct{}

type operationAuditMetadata struct {
	mu     sync.Mutex
	values map[string]string
}

func withOperationAuditMetadata(ctx context.Context) context.Context {
	return context.WithValue(ctx, operationAuditMetadataKey{}, &operationAuditMetadata{
		values: make(map[string]string),
	})
}

func setOperationAuditMetadata(r *http.Request, values map[string]string) {
	metadata, _ := r.Context().Value(operationAuditMetadataKey{}).(*operationAuditMetadata)
	if metadata == nil {
		return
	}
	metadata.mu.Lock()
	defer metadata.mu.Unlock()
	for key, value := range values {
		metadata.values[key] = value
	}
}

func operationAuditMetadataValues(r *http.Request) map[string]string {
	metadata, _ := r.Context().Value(operationAuditMetadataKey{}).(*operationAuditMetadata)
	if metadata == nil {
		return nil
	}
	metadata.mu.Lock()
	defer metadata.mu.Unlock()
	result := make(map[string]string, len(metadata.values))
	for key, value := range metadata.values {
		result[key] = value
	}
	return result
}

// auditResponseWriter captures the final status without changing the response API.
// 失败响应额外保留有限长度的错误正文，用于在操作日志里显示失败原因。
type auditResponseWriter struct {
	http.ResponseWriter
	status    int
	errorBody []byte
}

// maxAuditErrorBodyBytes 限制错误正文抓取量，避免大响应进入审计写入路径。
const maxAuditErrorBodyBytes = 2048

func (w *auditResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	w.captureErrorBody(body)
	return w.ResponseWriter.Write(body)
}

func (w *auditResponseWriter) captureErrorBody(body []byte) {
	if w.status < http.StatusBadRequest || len(w.errorBody) >= maxAuditErrorBodyBytes {
		return
	}
	remaining := maxAuditErrorBodyBytes - len(w.errorBody)
	if len(body) > remaining {
		body = body[:remaining]
	}
	w.errorBody = append(w.errorBody, body...)
}

func (w *auditResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func isAuditableMutation(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/audit/")
	default:
		return false
	}
}

// withOperationAudit persists an intent before a mutation is allowed to run.
// The intent is the durable trace anchor; the result is appended after the
// handler without pretending that an already committed mutation can be rolled
// back by HTTP middleware.
func (s *Server) withOperationAudit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAuditableMutation(r) {
			next(w, r)
			return
		}
		r = r.WithContext(withOperationAuditMetadata(r.Context()))
		intentID, err := s.recordOperationIntent(r)
		if err != nil {
			logger := s.logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Error("operation audit gate failed", "action", operationAction(r), "path", r.URL.Path, "error", err)
			s.writeErrorText(w, r, http.StatusServiceUnavailable, "operation audit unavailable")
			return
		}

		aw := &auditResponseWriter{ResponseWriter: w}
		next(aw, r)
		s.recordOperationResult(r, aw.statusCode(), intentID, operationErrorMessage(aw.errorBody))
	}
}

func (s *Server) recordOperationIntent(r *http.Request) (string, error) {
	event := s.newOperationAuditEvent(r)
	event.ID = model.NewID()
	event.Phase = "intent"
	event.Result = "pending"
	event.RequestID = apiresp.RequestID(r.Context())
	aiTokenID := aiTokenIDFromRequest(r)
	detailMap := map[string]any{
		"method":     r.Method,
		"path":       r.URL.Path,
		"phase":      "intent",
		"result":     "pending",
		"request_id": apiresp.RequestID(r.Context()),
		"user_agent": r.UserAgent(),
	}
	if aiTokenID != "" {
		detailMap["ai_token_id"] = aiTokenID
	}
	detail, err := json.Marshal(detailMap)
	if err != nil {
		return "", err
	}
	event.Detail = string(detail)
	if err := s.createOperationAuditEvent(r, event); err != nil {
		return "", err
	}
	return event.ID, nil
}

func (s *Server) recordOperation(r *http.Request, status int) {
	s.recordOperationResult(r, status, "", "")
}

func (s *Server) recordOperationResult(r *http.Request, status int, intentID, errorMessage string) {
	event := s.newOperationAuditEvent(r)
	event.Phase = "result"
	event.Result = operationResult(status)
	event.IntentID = strings.TrimSpace(intentID)
	event.RequestID = apiresp.RequestID(r.Context())
	event.StatusCode = status
	aiTokenID := aiTokenIDFromRequest(r)
	detailMap := map[string]any{
		"method":     r.Method,
		"path":       r.URL.Path,
		"phase":      "result",
		"status":     status,
		"result":     operationResult(status),
		"intent_id":  intentID,
		"request_id": apiresp.RequestID(r.Context()),
		"user_agent": r.UserAgent(),
	}
	if aiTokenID != "" {
		detailMap["ai_token_id"] = aiTokenID
	}
	if message := clampAuditErrorMessage(errorMessage); message != "" {
		detailMap["error_message"] = message
	}
	for key, value := range operationAuditMetadataValues(r) {
		detailMap[key] = value
		if key == operationAuditTargetLabelKey {
			event.TargetLabel = trimOperationAuditLabel(value)
		}
	}
	detail, err := json.Marshal(detailMap)
	if err == nil {
		event.Detail = string(detail)
	}
	if err := s.createOperationAuditEvent(r, event); err != nil {
		logger := s.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("failed to write operation audit result", "action", event.Action, "path", r.URL.Path, "intent_id", intentID, "error", err)
	}
}

// recordReadOperation 为敏感只读动作（例如查看平台账号密码）补一条审计结果行。
// 只读动作没有 intent 行；调用方必须在返回敏感数据之前调用本方法，
// 写入失败时禁止把密码返回给客户端。
func (s *Server) recordReadOperation(r *http.Request, status int, operation, targetLabel string) error {
	event := s.newOperationAuditEvent(r)
	event.Action = "read"
	event.Operation = operation
	event.Phase = "result"
	event.Result = operationResult(status)
	event.StatusCode = status
	event.RequestID = apiresp.RequestID(r.Context())
	event.TargetLabel = trimOperationAuditLabel(targetLabel)
	detailMap := map[string]any{
		"method":     r.Method,
		"path":       r.URL.Path,
		"phase":      "result",
		"status":     status,
		"result":     operationResult(status),
		"request_id": apiresp.RequestID(r.Context()),
		"user_agent": r.UserAgent(),
	}
	if aiTokenID := aiTokenIDFromRequest(r); aiTokenID != "" {
		detailMap["ai_token_id"] = aiTokenID
	}
	detail, err := json.Marshal(detailMap)
	if err != nil {
		return err
	}
	event.Detail = string(detail)
	return s.createOperationAuditEvent(r, event)
}

func (s *Server) newOperationAuditEvent(r *http.Request) *model.AuditEvent {
	return &model.AuditEvent{
		ActorID:       userIDFromRequest(r),
		ActorUsername: usernameFromRequest(r),
		Action:        operationAction(r),
		Operation:     operationKey(r),
		ActorChannel:  operationActorChannel(r),
		ResourceType:  operationResourceType(r.URL.Path),
		ResourceID:    operationResourceID(r.URL.Path),
		ResourceName:  r.URL.Path,
		ClientIP:      requestClientIP(r),
	}
}

func (s *Server) createOperationAuditEvent(r *http.Request, event *model.AuditEvent) error {
	if s.audit == nil {
		return errOperationAuditUnavailable
	}
	ctx, cancel := detachedAuditWriteContext(r.Context())
	defer cancel()
	if err := s.audit.CreateAuditEvent(ctx, event); err != nil {
		return err
	}
	return nil
}

func operationResult(status int) string {
	if status >= 200 && status < 400 {
		return "success"
	}
	return "failure"
}

func operationAction(r *http.Request) string {
	path := strings.ToLower(r.URL.Path)
	if strings.Contains(path, "/refresh") {
		return "refresh"
	}
	if strings.Contains(path, "/revoke") || strings.Contains(path, "/disconnect") {
		return "revoke"
	}
	if strings.Contains(path, "/test") ||
		strings.Contains(path, "/check") ||
		strings.Contains(path, "/diagnostics/") {
		return "test"
	}
	switch r.Method {
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return strings.ToLower(r.Method)
	}
}

func operationResourceType(path string) string {
	parts := splitAPIPath(path)
	if len(parts) == 0 {
		return "api"
	}
	if parts[0] == "ai" && len(parts) > 2 && parts[1] == "resources" {
		return parts[2]
	}
	if parts[0] == "db" && len(parts) > 1 {
		return "db/" + parts[1]
	}
	if parts[0] == "rbac" && len(parts) > 1 {
		return "rbac/" + parts[1]
	}
	return parts[0]
}

func operationResourceID(path string) string {
	parts := splitAPIPath(path)
	if len(parts) < 2 {
		return ""
	}
	index := 1
	if parts[0] == "ai" && len(parts) > 2 && parts[1] == "resources" {
		index = 3
	} else if parts[0] == "db" || parts[0] == "rbac" {
		index = 2
	}
	if len(parts) <= index || isOperationPathPart(parts[index]) {
		return ""
	}
	return parts[index]
}

func splitAPIPath(path string) []string {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/"), "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func isOperationPathPart(value string) bool {
	switch strings.ToLower(value) {
	case "test", "check", "accounts", "databases", "queries", "permissions", "members",
		"provision-account", "preferences", "tokens", "catalog", "effective":
		return true
	default:
		return false
	}
}

// operationAuditTargetLabelKey 是处理器写入可读操作对象名的元数据键。
const operationAuditTargetLabelKey = "target_label"

// maxAuditErrorMessageRunes 限制失败原因长度，避免把整段错误页写进审计。
const maxAuditErrorMessageRunes = 200

// operationErrorMessage 从失败响应正文里取出可读原因。
// 后台错误统一是 {error:{message}}；纯文本错误按原文截断；其余不落库。
func operationErrorMessage(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if message := clampAuditErrorMessage(envelope.Error.Message); message != "" {
			return message
		}
	}
	if bytes.HasPrefix(body, []byte("{")) || bytes.HasPrefix(body, []byte("[")) {
		return ""
	}
	return clampAuditErrorMessage(string(body))
}

func clampAuditErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	runes := []rune(message)
	if len(runes) <= maxAuditErrorMessageRunes {
		return message
	}
	return string(runes[:maxAuditErrorMessageRunes])
}

// operationKey 把 HTTP 方法和路径归成业务动作键，供列表直接展示「做了什么」。
func operationKey(r *http.Request) string {
	path := strings.ToLower(strings.TrimSuffix(r.URL.Path, "/"))
	switch {
	case strings.HasPrefix(path, "/api/ai/resources/"):
		switch {
		case strings.HasSuffix(path, "/credentials"):
			return operationIssueCredential
		case strings.HasSuffix(path, "/session"):
			return operationIssueSession
		}
	case path == "/api/ai/auth/refresh":
		return operationRefreshAIToken
	case strings.HasPrefix(path, "/api/sql-console/sessions"):
		switch {
		case strings.HasSuffix(path, "/execute"):
			return operationExecuteSQL
		case r.Method == http.MethodDelete:
			return operationCloseSQLConsole
		case r.Method == http.MethodPost && path == "/api/sql-console/sessions":
			return operationOpenSQLConsole
		}
	case strings.HasPrefix(path, "/api/db/accounts/test"),
		strings.HasPrefix(path, "/api/targets/test-connection"),
		strings.HasPrefix(path, "/api/containers/test"):
		return operationTestConnection
	case strings.HasPrefix(path, "/api/web-terminal/tickets"):
		return operationIssueTerminalTicket
	case strings.HasPrefix(path, "/api/system-settings"):
		return operationUpdateSettings
	case strings.HasPrefix(path, "/api/me/preferences"):
		return operationUpdatePreferences
	case strings.HasPrefix(path, "/api/online-sessions/") && strings.HasSuffix(path, "/disconnect"):
		return operationDisconnectSession
	case strings.HasPrefix(path, "/api/connection-passwords"):
		return operationIssueCredential
	case strings.HasPrefix(path, "/api/resource-grants"):
		if r.Method == http.MethodDelete {
			return operationRevokeAccess
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			return operationGrantAccess
		}
	}
	return operationVerb(r.Method) + ":" + operationFamily(r.URL.Path)
}

func operationVerb(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return strings.ToLower(method)
	}
}

// operationFamily 返回资源族名（单数），前端据此把「动词:资源」翻成中文。
func operationFamily(path string) string {
	parts := splitAPIPath(path)
	if len(parts) == 0 {
		return "api"
	}
	switch parts[0] {
	case "ai":
		if len(parts) > 1 {
			switch parts[1] {
			case "resources":
				if len(parts) > 2 {
					return parts[2]
				}
			case "tokens":
				return "ai_token"
			}
		}
	case "db":
		if len(parts) > 1 {
			switch parts[1] {
			case "instances":
				return "database_instance"
			case "accounts":
				return "database_account"
			case "connections":
				return "database_connection"
			}
		}
	case "rbac":
		if len(parts) > 1 {
			switch parts[1] {
			case "roles":
				return "role"
			case "permissions":
				return "permission"
			case "user-roles":
				return "user_role"
			case "role-permissions":
				return "role_permission"
			}
		}
	case "me":
		return "preference"
	case "users":
		return "user"
	case "hosts":
		return "host"
	case "targets":
		return "target"
	case "resource-grants":
		return "resource_grant"
	case "resource-groups":
		return "resource_group"
	case "user-groups":
		return "user_group"
	case "applications":
		return "application"
	case "containers":
		return "container_endpoint"
	case "platform-accounts":
		return "platform_account"
	case "temporary-accounts":
		return "temporary_account"
	case "connection-passwords":
		return "connection_password"
	case "online-sessions":
		return "online_session"
	case "system-settings":
		return "system_setting"
	case "web-terminal":
		return "web_terminal"
	}
	return parts[0]
}

// operationActorChannel 区分 AI 令牌调用与浏览器后台操作。
func operationActorChannel(r *http.Request) string {
	if aiTokenIDFromRequest(r) != "" {
		return actorChannelAIAPI
	}
	if userIDFromRequest(r) != "" {
		return actorChannelAdminConsole
	}
	return ""
}

// trimOperationAuditLabel 把可读对象名裁到列宽，避免超长标签写库失败。
func trimOperationAuditLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 255 {
		return value
	}
	return string(runes[:255])
}
