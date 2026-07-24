package admin

import (
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
		s.recordOperationResult(r, aw.statusCode(), intentID)
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
	s.recordOperationResult(r, status, "")
}

func (s *Server) recordOperationResult(r *http.Request, status int, intentID string) {
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
	for key, value := range operationAuditMetadataValues(r) {
		detailMap[key] = value
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

func (s *Server) newOperationAuditEvent(r *http.Request) *model.AuditEvent {
	return &model.AuditEvent{
		ActorID:       userIDFromRequest(r),
		ActorUsername: usernameFromRequest(r),
		Action:        operationAction(r),
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
	case "test", "check", "accounts", "databases", "queries", "permissions", "members", "provision-account":
		return true
	default:
		return false
	}
}
