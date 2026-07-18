package admin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/server/dbproxy"
)

// handleTestDBConnection handles POST /api/db/accounts/test and POST /api/db/accounts/test/{id}.
func (s *Server) handleTestDBConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.TrimSuffix(r.URL.Path, "/") == "/api/db/accounts/test" {
		if !s.requirePermission(r, rbac.ActionDBProxyCreate) {
			s.forbidden(w, r)
			return
		}
		s.handleTestDBConnectionPayload(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/db/accounts/test/")
	if id == "" || strings.Contains(id, "/") {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	var account model.DatabaseAccount
	if err := s.db.Preload("Instance").First(&account, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "account not found")
		return
	}
	allowed, err := s.authorizeConnection(r.Context(), userIDFromRequest(r), rbac.ActionDBConnect, model.ResourceTypeDatabaseAccount, account.ID)
	if err != nil {
		s.logger.Warn("database account test authorization failed", "account", account.ID, "error", err)
		s.forbidden(w, r)
		return
	}
	if !allowed {
		s.forbidden(w, r)
		return
	}
	if account.Status == "disabled" {
		s.writeErrorText(w, r, http.StatusForbidden, "account is disabled")
		return
	}
	if account.ExpiresAt != nil && time.Now().UTC().After(*account.ExpiresAt) {
		s.writeErrorText(w, r, http.StatusForbidden, "account has expired")
		return
	}
	if account.Instance.Status == "disabled" {
		s.writeErrorText(w, r, http.StatusForbidden, "database instance is disabled")
		return
	}
	s.writeDatabaseProbeResult(w, r, account.Instance, account.Username, account.Password.GetPlaintext())
}

type testDBConnectionPayload struct {
	InstanceID string `json:"instance_id"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

func (s *Server) handleTestDBConnectionPayload(w http.ResponseWriter, r *http.Request) {
	var payload testDBConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid json body")
		return
	}
	payload.InstanceID = strings.TrimSpace(payload.InstanceID)
	payload.Username = strings.TrimSpace(payload.Username)
	if payload.InstanceID == "" || payload.Password == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "instance_id and password are required")
		return
	}

	var instance model.DatabaseInstance
	if err := s.db.First(&instance, "id = ?", payload.InstanceID).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "instance not found")
		return
	}
	if !s.requireResourceAction(w, r, rbac.ActionDBProxyCreate, model.ResourceTypeDatabaseInstance, instance.ID) {
		return
	}
	if instance.Status == "disabled" {
		s.writeErrorText(w, r, http.StatusForbidden, "database instance is disabled")
		return
	}
	s.writeDatabaseProbeResult(w, r, instance, payload.Username, payload.Password)
}

func (s *Server) writeDatabaseProbeResult(w http.ResponseWriter, r *http.Request, instance model.DatabaseInstance, username, password string) {
	started := time.Now()
	err := dbproxy.ProbeDatabaseAuthentication(r.Context(), instance, username, password)
	response := map[string]any{"ok": err == nil, "latency_ms": time.Since(started).Milliseconds()}
	if err != nil {
		logger := s.logger
		if logger == nil {
			logger = slog.Default()
		}
		publicMessage := databaseProbeErrorMessage(err)
		logger.Warn("database authentication probe failed", "protocol", instance.Protocol, "instance", instance.ID, "reason", publicMessage)
		response["error"] = publicMessage
	}
	s.writeJSON(w, r, http.StatusOK, response)
}

func databaseProbeErrorMessage(err error) string {
	for current := err; current != nil; current = errors.Unwrap(current) {
		message := strings.ToLower(current.Error())
		if strings.Contains(message, "requires tls") ||
			strings.Contains(message, "verified tls") ||
			strings.Contains(message, "tls is required") {
			return "database connection requires TLS"
		}
	}
	if err == nil {
		return ""
	}
	return "database connection test failed"
}
