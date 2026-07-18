package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := browserSessionFromRequest(r)
	if !ok || s.browserSessions == nil {
		s.writeErrorText(w, r, http.StatusUnauthorized, "missing browser session")
		return
	}
	if err := s.browserSessions.Revoke(r.Context(), session.SessionID); err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to revoke browser session")
		return
	}
	clearBrowserSessionCookie(w, r, s.cfg.Admin.PublicURL)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWebTerminalTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		TargetID string `json:"target_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&request); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	request.TargetID = strings.TrimSpace(request.TargetID)
	if request.TargetID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "target_id is required")
		return
	}
	session, ok := browserSessionFromRequest(r)
	if !ok || s.browserSessions == nil {
		s.writeErrorText(w, r, http.StatusUnauthorized, "missing browser session")
		return
	}
	user := model.User{ID: session.UserID, Username: usernameFromRequest(r)}
	allowed, err := s.authorizeConnection(r.Context(), user.ID, rbac.ActionSessionConnect, model.ResourceTypeHostAccount, request.TargetID)
	if err != nil || !allowed {
		s.writeErrorText(w, r, http.StatusForbidden, "connection is not authorized")
		return
	}
	target, err := s.resolveWebTerminalTarget(r.Context(), user, request.TargetID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "target not found")
		return
	}
	ticket, err := s.browserSessions.CreateWebSocketTicket(r.Context(), session, target.ID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to create websocket ticket")
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]string{"ticket": ticket, "target_id": target.ID})
}
