package admin

import (
	"net/http"
	"strings"

	"jianmen/internal/service"
)

func (s *Server) handleAuditBatches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/audit/batches/"), "/")
	page, size, offset := auditDBQueryPageFromQuery(r)
	query := r.URL.Query()
	search := firstNonEmpty(query.Get("q"), query.Get("search"))
	var items any
	var total int64
	var err error
	switch {
	case len(parts) == 1:
		items, total, err = s.auditQuery.ListBatches(r.Context(), userIDFromRequest(r), parts[0], service.AuditSessionListParams{Protocol: query.Get("protocol"), Date: query.Get("date"), Search: search, Page: page, Size: size})
	case len(parts) == 3 && parts[2] == "logs":
		items, total, err = s.auditQuery.BatchLogs(r.Context(), userIDFromRequest(r), parts[0], parts[1], service.AuditDBQueryPreviewParams{Search: search, Limit: size, Offset: offset})
	default:
		s.writeAuditArtifactUnavailable(w, r)
		return
	}
	if err != nil {
		s.writeAuditQueryError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
}
