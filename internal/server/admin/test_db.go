package admin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
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
		s.handleTestDBConnectionPayload(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/db/accounts/test/")
	if id == "" || strings.Contains(id, "/") {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	target, err := s.databaseManagement.SavedAccountProbe(r.Context(), userIDFromRequest(r), id)
	if err != nil {
		s.writeDatabaseManagementError(w, r, err)
		return
	}
	s.writeDatabaseProbeResult(w, r, databaseInstanceRecordToModel(target.Instance), target.Username, target.Password)
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
	target, err := s.databaseManagement.PayloadProbe(r.Context(), userIDFromRequest(r), strings.TrimSpace(payload.InstanceID), strings.TrimSpace(payload.Username), payload.Password)
	if err != nil {
		s.writeDatabaseManagementError(w, r, err)
		return
	}
	s.writeDatabaseProbeResult(w, r, databaseInstanceRecordToModel(target.Instance), target.Username, target.Password)
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
		if strings.Contains(message, "requires tls") || strings.Contains(message, "verified tls") || strings.Contains(message, "tls is required") {
			return "database connection requires TLS"
		}
	}
	if err == nil {
		return ""
	}
	return "database connection test failed"
}
