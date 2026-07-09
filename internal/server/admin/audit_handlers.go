package admin

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func (s *Server) handleAuditSSH(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionSessionView) {
		s.forbidden(w)
		return
	}
	params := store.AuditListParams{
		Protocol: "ssh,sftp",
		Search:   strings.ToLower(r.URL.Query().Get("search")),
		Date:     r.URL.Query().Get("date"),
	}
	params.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	params.Size, _ = strconv.Atoi(r.URL.Query().Get("size"))

	items, total, err := s.store.ListAuditSessions(params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total,
		"page": params.Page, "size": params.Size,
	})
}

func (s *Server) handleAuditDB(w http.ResponseWriter, r *http.Request) {
	params := store.AuditListParams{
		Protocol: "mysql,postgres,redis",
		Search:   strings.ToLower(r.URL.Query().Get("search")),
		Date:     r.URL.Query().Get("date"),
	}
	protocolFilter := r.URL.Query().Get("protocol")
	if protocolFilter != "" {
		params.Protocol = protocolFilter
	}
	params.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	params.Size, _ = strconv.Atoi(r.URL.Query().Get("size"))

	items, total, err := s.store.ListAuditSessions(params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total,
		"page": params.Page, "size": params.Size,
	})
}

func (s *Server) handleAuditArtifact(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/audit/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	protocol := parts[0]
	sessionID := parts[1]
	var artifact string
	if len(parts) == 3 {
		artifact = parts[2]
	}

	session, err := s.store.GetAuditSession(sessionID)
	if err != nil {
		writeErrorText(w, http.StatusNotFound, "audit session not found")
		return
	}

	switch {
	case artifact == "":
		writeJSON(w, http.StatusOK, session)
	case artifact == "commands" && (protocol == "ssh" || protocol == "sftp"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.store.ListAuditSSHCommands(sessionID, store.PageOpts{Limit: limit, Offset: offset})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "files" && (protocol == "ssh" || protocol == "sftp"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.store.ListAuditSFTPEvents(sessionID, store.PageOpts{Limit: limit, Offset: offset})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "file-summary" && (protocol == "ssh" || protocol == "sftp"):
		summaryPath := filepath.Join(session.ReplayDir, "files-summary.json")
		if _, err := os.Stat(summaryPath); err != nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSONFile(w, summaryPath)
	case artifact == "replay" && (protocol == "ssh" || protocol == "sftp"):
		replayPath := session.ReplayDir
		if replayPath == "" {
			writeErrorText(w, http.StatusNotFound, "no replay available")
			return
		}
		writeTextFile(w, filepath.Join(replayPath, "terminal.cast"), "application/x-asciicast; charset=utf-8")
	case artifact == "queries" && (protocol == "mysql" || protocol == "postgres" || protocol == "redis"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.store.ListAuditDBQueries(sessionID, store.PageOpts{Limit: limit, Offset: offset})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
	default:
		writeErrorText(w, http.StatusNotFound, "not found")
	}
}

func pageFromQuery(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size <= 0 {
		size = 500
	}
	if page <= 0 {
		page = 1
	}
	return size, (page - 1) * size
}
