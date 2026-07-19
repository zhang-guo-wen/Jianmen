package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/pkg/apiresp"
)

const aiRefreshAuditActor = "ai-refresh-credential"

func (s *Server) recordAIRefreshIntent(r *http.Request, fingerprint string) (string, error) {
	event := newAIRefreshAuditEvent(r, fingerprint, nil)
	event.ID = model.NewID()
	detail, err := json.Marshal(map[string]any{
		"method":            r.Method,
		"path":              r.URL.Path,
		"phase":             "intent",
		"result":            "pending",
		"token_fingerprint": fingerprint,
		"request_id":        apiresp.RequestID(r.Context()),
		"user_agent":        r.UserAgent(),
	})
	if err != nil {
		return "", err
	}
	event.Detail = string(detail)
	if err := s.createOperationAuditEvent(r, event); err != nil {
		return "", err
	}
	return event.ID, nil
}

func (s *Server) recordAIRefreshResult(
	r *http.Request,
	intentID string,
	fingerprint string,
	token *model.AIAccessToken,
	status int,
	reason string,
) error {
	event := newAIRefreshAuditEvent(r, fingerprint, token)
	detail := map[string]any{
		"method":            r.Method,
		"path":              r.URL.Path,
		"phase":             "result",
		"status":            status,
		"result":            operationResult(status),
		"intent_id":         intentID,
		"token_fingerprint": fingerprint,
		"request_id":        apiresp.RequestID(r.Context()),
		"user_agent":        r.UserAgent(),
	}
	if token != nil {
		detail["token_id"] = token.ID
		detail["user_id"] = token.UserID
	}
	if strings.TrimSpace(reason) != "" {
		detail["reason"] = strings.TrimSpace(reason)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	event.Detail = string(encoded)
	return s.createOperationAuditEvent(r, event)
}

func newAIRefreshAuditEvent(r *http.Request, fingerprint string, token *model.AIAccessToken) *model.AuditEvent {
	event := &model.AuditEvent{
		ActorID:       fingerprint,
		ActorUsername: aiRefreshAuditActor,
		Action:        "refresh",
		ResourceType:  "ai_access_token",
		ResourceID:    fingerprint,
		ResourceName:  r.URL.Path,
		ClientIP:      requestClientIP(r),
	}
	if token != nil {
		event.ActorID = token.UserID
		event.ActorUsername = token.User.Username
		event.ResourceID = token.ID
		event.ResourceName = token.Name
	}
	return event
}
