package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/pkg/apiresp"
)

// auditResponseWriter captures the final status without changing the response API.
type auditResponseWriter struct {
	http.ResponseWriter
	status int
}

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
	return w.ResponseWriter.Write(body)
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

func (s *Server) recordOperation(r *http.Request, status int) {
	if s.store == nil {
		return
	}
	event := &model.AuditEvent{
		ActorID:       userIDFromRequest(r),
		ActorUsername: usernameFromRequest(r),
		Action:        operationAction(r),
		ResourceType:  operationResourceType(r.URL.Path),
		ResourceID:    operationResourceID(r.URL.Path),
		ResourceName:  r.URL.Path,
		ClientIP:      requestClientIP(r),
	}
	detail, err := json.Marshal(map[string]any{
		"method":     r.Method,
		"path":       r.URL.Path,
		"status":     status,
		"result":     operationResult(status),
		"request_id": apiresp.RequestID(r.Context()),
		"user_agent": r.UserAgent(),
	})
	if err == nil {
		event.Detail = string(detail)
	}
	if err := s.store.CreateAuditEvent(event); err != nil {
		logger := s.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("failed to write operation audit log", "action", event.Action, "path", r.URL.Path, "error", err)
	}
}

func operationResult(status int) string {
	if status >= 200 && status < 400 {
		return "success"
	}
	return "failure"
}

func operationAction(r *http.Request) string {
	path := strings.ToLower(r.URL.Path)
	if strings.Contains(path, "/revoke") || strings.Contains(path, "/disconnect") {
		return "revoke"
	}
	if strings.Contains(path, "/test") || strings.Contains(path, "/check") {
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
	if parts[0] == "db" || parts[0] == "rbac" {
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
	case "test", "check", "accounts", "databases", "queries", "permissions", "members", "provision-account":
		return true
	default:
		return false
	}
}
