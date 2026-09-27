package admin

import (
	"net/http"
	"strings"

	"jianmen/internal/service"
)

func (s *Server) handleAuditOperations(w http.ResponseWriter, r *http.Request) {
	params := service.AuditEventListParams{
		Search:       strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("search"))),
		Action:       strings.TrimSpace(r.URL.Query().Get("action")),
		ResourceType: strings.TrimSpace(r.URL.Query().Get("resource_type")),
		Date:         strings.TrimSpace(r.URL.Query().Get("date")),
		Page:         positiveIntRequestQuery(r, "page", 1),
		Size:         positiveIntRequestQuery(r, "page_size", 50),
	}
	items, total, err := s.auditQuery.ListOperations(r.Context(), userIDFromRequest(r), params)
	if err != nil {
		s.writeAuditQueryError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": params.Page, "page_size": params.Size,
	})
}

func (s *Server) handleAuditLogins(w http.ResponseWriter, r *http.Request) {
	params := service.LoginAuditListParams{
		Search:  strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("search"))),
		Outcome: strings.TrimSpace(r.URL.Query().Get("outcome")),
		Date:    strings.TrimSpace(r.URL.Query().Get("date")),
		Page:    positiveIntRequestQuery(r, "page", 1),
		Size:    positiveIntRequestQuery(r, "page_size", 50),
	}
	items, total, err := s.auditQuery.ListLogins(r.Context(), userIDFromRequest(r), params)
	if err != nil {
		s.writeAuditQueryError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": params.Page, "page_size": params.Size,
	})
}
