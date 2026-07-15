package admin

import (
	"errors"
	"net/http"
	"strings"

	"jianmen/internal/online"
	"jianmen/internal/rbac"
)

func (s *Server) handleOnlineSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionSessionView) {
		s.forbidden(w, r)
		return
	}

	items := s.onlineSessions.List()
	resourceType := strings.TrimSpace(r.URL.Query().Get("resource_type"))
	resourceID := strings.TrimSpace(r.URL.Query().Get("resource_id"))
	if resourceType != "" || resourceID != "" {
		filtered := make([]online.Session, 0, len(items))
		for _, item := range items {
			if resourceType != "" && item.ResourceType != resourceType {
				continue
			}
			if resourceID != "" && item.ResourceID != resourceID {
				continue
			}
			filtered = append(filtered, item)
		}
		items = filtered
	}

	resp := paginateSlice(items, r, func(item online.Session, q string) bool {
		return strings.Contains(strings.ToLower(item.Instance), q) ||
			strings.Contains(strings.ToLower(item.Protocol), q) ||
			strings.Contains(strings.ToLower(item.ProtocolSubtype), q) ||
			strings.Contains(strings.ToLower(item.Account), q) ||
			strings.Contains(strings.ToLower(item.Operator), q)
	})
	s.writeJSON(w, r, http.StatusOK, resp)
}

func (s *Server) handleOnlineSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionSessionDisconnect) {
		s.forbidden(w, r)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/online-sessions/"), "/")
	if id == "" || strings.Contains(id, "/") {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid online session id")
		return
	}
	if err := s.onlineSessions.Disconnect(id); err != nil {
		if errors.Is(err, online.ErrSessionNotFound) {
			s.writeErrorText(w, r, http.StatusNotFound, "online session not found")
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
