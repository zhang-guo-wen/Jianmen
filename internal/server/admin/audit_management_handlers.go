package admin

import (
	"net/http"
	"strings"

	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func (s *Server) handleAuditOperations(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionAuditView) {
		s.forbidden(w, r)
		return
	}
	params := store.AuditEventListParams{
		Search:       strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("search"))),
		Action:       strings.TrimSpace(r.URL.Query().Get("action")),
		ResourceType: strings.TrimSpace(r.URL.Query().Get("resource_type")),
		Date:         strings.TrimSpace(r.URL.Query().Get("date")),
		Page:         positiveIntRequestQuery(r, "page", 1),
		Size:         positiveIntRequestQuery(r, "page_size", 50),
	}
	items, total, err := s.audit.ListAuditEvents(r.Context(), params)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": params.Page, "page_size": params.Size,
	})
}

func (s *Server) handleAuditLogins(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionAuditView) {
		s.forbidden(w, r)
		return
	}
	params := store.LoginAuditListParams{
		Search:  strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("search"))),
		Outcome: strings.TrimSpace(r.URL.Query().Get("outcome")),
		Date:    strings.TrimSpace(r.URL.Query().Get("date")),
		Page:    positiveIntRequestQuery(r, "page", 1),
		Size:    positiveIntRequestQuery(r, "page_size", 50),
	}
	items, total, err := s.audit.ListLoginAuditLogs(r.Context(), params)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": params.Page, "page_size": params.Size,
	})
}
