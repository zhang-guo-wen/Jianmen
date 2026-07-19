package admin

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

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

	items, total, err := s.audit.ListAuditSessions(params)
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

	items, total, err := s.audit.ListAuditSessions(params)
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

	session, err := s.audit.GetAuditSession(sessionID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "audit session not found")
		return
	}
	action := rbac.ActionAuditView
	switch strings.ToLower(session.Protocol) {
	case "mysql", "postgres", "postgresql", "redis", "db", "database":
		action = rbac.ActionDBAuditView
	}
	var allowed bool
	if session.State == "started" {
		allowed = s.requireAnyPermission(r, rbac.ActionSessionView, action)
	} else {
		allowed = s.requirePermission(r, action)
	}
	if !allowed {
		s.forbidden(w, r)
		return
	}

	switch {
	case artifact == "":
		s.writeJSON(w, r, http.StatusOK, session)
	case artifact == "commands" && (protocol == "ssh" || protocol == "sftp"):
		limit, offset := pageFromQuery(r)
		if session.ReplayDir != "" {
			items, total, fileErr := readAuditSSHCommandPage(filepath.Join(session.ReplayDir, "commands.jsonl"), limit, offset)
			if fileErr == nil {
				s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": total})
				return
			}
			if !os.IsNotExist(fileErr) {
				s.writeErrorText(w, r, http.StatusInternalServerError, fileErr.Error())
				return
			}
		}
		items, total, err := s.audit.ListAuditSSHCommands(sessionID, store.PageOpts{Limit: limit, Offset: offset})
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "files" && (protocol == "ssh" || protocol == "sftp"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.audit.ListAuditSFTPEvents(sessionID, store.PageOpts{Limit: limit, Offset: offset})
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
		page, pageSize, offset := auditDBQueryPageFromQuery(r)
		items, total, err := s.audit.ListAuditDBQueryPreviews(
			r.Context(),
			sessionID,
			store.AuditDBQueryPreviewParams{
				Search: strings.ToLower(firstNonEmpty(
					r.URL.Query().Get("q"),
					r.URL.Query().Get("search"),
				)),
				Limit:  pageSize,
				Offset: offset,
			},
		)
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
			seq := int64(offset + i)
			ts := q.Timestamp.UnixMilli()
			sqlPreview, previewDetail := auditDBQuerySQLPreview(q)
			events = append(events, dbproxy.DBQueryEvent{
				Type: "query_started", ConnectionID: sessionID, Seq: seq,
				Protocol: queryProtocol, SQL: sqlPreview, QueryKind: q.QueryKind,
				Detail: previewDetail, StartedAt: ts,
			}, dbproxy.DBQueryEvent{
				Type: "query_finished", ConnectionID: sessionID, Seq: seq,
				Protocol: queryProtocol, QueryKind: q.QueryKind,
				StartedAt: ts, CompletedAt: ts, DurationMs: q.DurationMs, Status: "success",
			})
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			"items": events, "total": total,
			"page": page, "page_size": pageSize,
		})
	default:
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
	}
}

const (
	auditDBQueryDefaultPageSize     = 50
	auditDBQueryMaxPageSize         = 100
	auditDBQuerySQLPreviewByteLimit = 64 * 1024
	auditDBQuerySQLTruncatedMarker  = "\n/* [TRUNCATED SQL PREVIEW] */"
)

func auditDBQueryPageFromQuery(r *http.Request) (int, int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(firstNonEmpty(
		r.URL.Query().Get("page_size"),
		r.URL.Query().Get("size"),
	))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = auditDBQueryDefaultPageSize
	}
	if pageSize > auditDBQueryMaxPageSize {
		pageSize = auditDBQueryMaxPageSize
	}
	maxInt := int(^uint(0) >> 1)
	if page > maxInt/pageSize {
		page = maxInt / pageSize
	}
	return page, pageSize, (page - 1) * pageSize
}

func auditDBQuerySQLPreview(query store.AuditDBQueryPreview) (string, map[string]any) {
	storedBytes := query.SQLStoredBytes
	if storedBytes <= 0 {
		storedBytes = int64(len(query.SQLText))
	}
	originalBytes := query.OriginalSQLBytes
	if originalBytes <= 0 {
		originalBytes = storedBytes
	}

	markerRequired := query.SQLTruncated || storedBytes > auditDBQuerySQLPreviewByteLimit
	contentLimit := auditDBQuerySQLPreviewByteLimit
	if markerRequired {
		contentLimit -= len(auditDBQuerySQLTruncatedMarker)
	}
	invalidUTF8 := !utf8.ValidString(query.SQLText)
	if invalidUTF8 {
		markerRequired = true
		contentLimit = auditDBQuerySQLPreviewByteLimit - len(auditDBQuerySQLTruncatedMarker)
	}
	preview, previewChanged := auditDBQueryUTF8Prefix(query.SQLText, contentLimit)
	previewTruncated := previewChanged || int64(len(preview)) < storedBytes
	if markerRequired {
		preview += auditDBQuerySQLTruncatedMarker
	}

	detail := map[string]any{
		"sql_truncated":         query.SQLTruncated || previewTruncated,
		"sql_audit_truncated":   query.SQLTruncated,
		"sql_preview_truncated": previewTruncated,
		"sql_original_bytes":    originalBytes,
		"sql_stored_bytes":      storedBytes,
		"sql_preview_bytes":     len(preview),
	}
	return preview, detail
}

func auditDBQueryUTF8Prefix(value string, byteLimit int) (string, bool) {
	if byteLimit <= 0 {
		return "", value != ""
	}
	changed := false
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "\uFFFD")
		changed = true
	}
	if len(value) <= byteLimit {
		return value, changed
	}
	end := byteLimit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end], true
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

type recordedSSHCommand struct {
	Seq        int64  `json:"seq"`
	OffsetMs   int64  `json:"offset_ms"`
	Command    string `json:"command"`
	Preview    string `json:"preview"`
	Confidence string `json:"confidence"`
	StartedAt  int64  `json:"started_at"`
	EndedAt    int64  `json:"ended_at"`
}

type auditSSHCommandOutput struct {
	Seq        int64  `json:"seq"`
	OffsetMs   int64  `json:"offset_ms"`
	Command    string `json:"command"`
	Output     string `json:"output"`
	Confidence string `json:"confidence"`
	StartedAt  int64  `json:"started_at"`
	EndedAt    int64  `json:"ended_at"`
}

func readAuditSSHCommandPage(path string, limit, offset int) ([]auditSSHCommandOutput, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	items := make([]auditSSHCommandOutput, 0, limit)
	total := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var recorded recordedSSHCommand
		if err := json.Unmarshal(scanner.Bytes(), &recorded); err != nil {
			continue
		}
		if total >= offset && len(items) < limit {
			items = append(items, auditSSHCommandOutput{
				Seq:        recorded.Seq,
				OffsetMs:   recorded.OffsetMs,
				Command:    recorded.Command,
				Output:     recorded.Preview,
				Confidence: recorded.Confidence,
				StartedAt:  recorded.StartedAt,
				EndedAt:    recorded.EndedAt,
			})
		}
		total++
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
