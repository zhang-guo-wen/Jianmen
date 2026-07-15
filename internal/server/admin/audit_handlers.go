package admin

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"jianmen/internal/rbac"
	"jianmen/internal/server/dbproxy"
	"jianmen/internal/store"
)

func (s *Server) handleAuditSSH(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionAuditView) {
		s.forbidden(w, r)
		return
	}
	params := store.AuditListParams{
		Protocol: "ssh,sftp",
		Search:   strings.ToLower(firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("search"))),
		Date:     r.URL.Query().Get("date"),
	}
	params.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	params.Size, _ = strconv.Atoi(firstNonEmpty(r.URL.Query().Get("page_size"), r.URL.Query().Get("size")))

	items, total, err := s.store.ListAuditSessions(params)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "total": total,
		"page": params.Page, "page_size": params.Size,
	})
}

func (s *Server) handleAuditDB(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionDBAuditView) {
		s.forbidden(w, r)
		return
	}
	params := store.AuditListParams{
		Protocol: "mysql,postgres,redis",
		Search:   strings.ToLower(firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("search"))),
		Date:     r.URL.Query().Get("date"),
	}
	protocolFilter := r.URL.Query().Get("protocol")
	if protocolFilter != "" {
		params.Protocol = protocolFilter
	}
	params.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	params.Size, _ = strconv.Atoi(firstNonEmpty(r.URL.Query().Get("page_size"), r.URL.Query().Get("size")))

	items, total, err := s.store.ListAuditSessions(params)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "total": total,
		"page": params.Page, "page_size": params.Size,
	})
}

func (s *Server) handleAuditArtifact(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/audit/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
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
		s.writeErrorText(w, r, http.StatusNotFound, "audit session not found")
		return
	}
	action := rbac.ActionAuditView
	switch strings.ToLower(session.Protocol) {
	case "mysql", "postgres", "postgresql", "redis", "db", "database":
		action = rbac.ActionDBAuditView
	}
	if !s.requirePermission(r, action) {
		s.forbidden(w, r)
		return
	}

	switch {
	case artifact == "":
		s.writeJSON(w, r, http.StatusOK, session)
	case artifact == "commands" && (protocol == "ssh" || protocol == "sftp"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.store.ListAuditSSHCommands(sessionID, store.PageOpts{Limit: limit, Offset: offset})
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "files" && (protocol == "ssh" || protocol == "sftp"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.store.ListAuditSFTPEvents(sessionID, store.PageOpts{Limit: limit, Offset: offset})
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "file-summary" && (protocol == "ssh" || protocol == "sftp"):
		summaryPath := filepath.Join(session.ReplayDir, "files-summary.json")
		if _, err := os.Stat(summaryPath); err != nil {
			s.writeJSON(w, r, http.StatusOK, []any{})
			return
		}
		s.writeJSONFile(w, r, summaryPath)
	case artifact == "replay" && (protocol == "ssh" || protocol == "sftp"):
		replayPath := session.ReplayDir
		if replayPath == "" {
			s.writeErrorText(w, r, http.StatusNotFound, "no replay available")
			return
		}
		s.writeTextFile(w, r, filepath.Join(replayPath, "terminal.cast"), "application/x-asciicast; charset=utf-8")
	case artifact == "queries" && (protocol == "db" || protocol == "mysql" || protocol == "postgres" || protocol == "redis"):
		items, err := s.store.ListAuditDBQueryEvents(sessionID)
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		queryProtocol := session.Protocol
		if queryProtocol == "" {
			queryProtocol = protocol
		}
		events := make([]dbproxy.DBQueryEvent, 0, len(items)*2)
		for i, q := range items {
			seq := int64(i)
			ts := q.Timestamp.UnixMilli()
			events = append(events, dbproxy.DBQueryEvent{
				Type: "query_started", ConnectionID: sessionID, Seq: seq,
				Protocol: queryProtocol, SQL: q.SQLText, QueryKind: q.QueryKind,
				StartedAt: ts,
			}, dbproxy.DBQueryEvent{
				Type: "query_finished", ConnectionID: sessionID, Seq: seq,
				Protocol: queryProtocol, SQL: q.SQLText, QueryKind: q.QueryKind,
				StartedAt: ts, CompletedAt: ts, DurationMs: q.DurationMs, Status: "success",
			})
		}
		s.writeJSON(w, r, http.StatusOK, events)
	default:
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
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
