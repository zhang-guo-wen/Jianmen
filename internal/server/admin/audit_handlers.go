package admin

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"jianmen/internal/service"
)

func (s *Server) handleAuditSSH(w http.ResponseWriter, r *http.Request) {
	params := service.AuditSessionListParams{
		Protocol: "ssh,sftp",
		Search:   strings.ToLower(firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("search"))),
		Date:     r.URL.Query().Get("date"),
	}
	params.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	params.Size, _ = strconv.Atoi(firstNonEmpty(r.URL.Query().Get("page_size"), r.URL.Query().Get("size")))

	items, total, err := s.auditQuery.ListSSH(r.Context(), userIDFromRequest(r), params)
	if err != nil {
		s.writeAuditQueryError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "total": total,
		"page": params.Page, "page_size": params.Size,
	})
}

func (s *Server) handleAuditDB(w http.ResponseWriter, r *http.Request) {
	params := service.AuditSessionListParams{
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

	items, total, err := s.auditQuery.ListDB(r.Context(), userIDFromRequest(r), params)
	if err != nil {
		s.writeAuditQueryError(w, r, err)
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

	session, err := s.auditQuery.AuthorizedSession(r.Context(), userIDFromRequest(r), protocol, sessionID)
	if err != nil {
		s.writeAuditQueryError(w, r, err)
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
				s.writeAuditFileError(w, r, fileErr)
				return
			}
		}
		items, total, err := s.auditQuery.SSHCommands(r.Context(), userIDFromRequest(r), protocol, sessionID, service.Page{Limit: limit, Offset: offset})
		if err != nil {
			s.writeAuditQueryError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "files" && (protocol == "ssh" || protocol == "sftp"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.auditQuery.SFTPEvents(r.Context(), userIDFromRequest(r), protocol, sessionID, service.Page{Limit: limit, Offset: offset})
		if err != nil {
			s.writeAuditQueryError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "file-summary" && (protocol == "ssh" || protocol == "sftp"):
		if session.ReplayDir == "" {
			s.writeJSON(w, r, http.StatusOK, []any{})
			return
		}
		summaryPath := filepath.Join(session.ReplayDir, "files-summary.json")
		if _, err := os.Stat(summaryPath); err != nil {
			s.writeJSON(w, r, http.StatusOK, []any{})
			return
		}
		s.writeAuditJSONFile(w, r, summaryPath)
	case artifact == "replay" && (protocol == "ssh" || protocol == "sftp"):
		replayPath := session.ReplayDir
		if replayPath == "" {
			s.writeErrorText(w, r, http.StatusNotFound, "no replay available")
			return
		}
		s.writeAuditTextFile(w, r, filepath.Join(replayPath, "terminal.cast"), "application/x-asciicast; charset=utf-8")
	case artifact == "queries" && (protocol == "db" || protocol == "mysql" || protocol == "postgres" || protocol == "redis"):
		page, pageSize, offset := auditDBQueryPageFromQuery(r)
		items, total, err := s.auditQuery.DBQueryEvents(
			r.Context(),
			userIDFromRequest(r),
			protocol,
			sessionID,
			service.AuditDBQueryPreviewParams{
				Search: strings.ToLower(firstNonEmpty(
					r.URL.Query().Get("q"),
					r.URL.Query().Get("search"),
				)),
				Limit:  pageSize,
				Offset: offset,
			},
		)
		if err != nil {
			s.writeAuditQueryError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			"items": items, "total": total,
			"page": page, "page_size": pageSize,
		})
	default:
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
	}
}

const (
	auditDBQueryDefaultPageSize     = service.AuditDBQueryDefaultPageSize
	auditDBQueryMaxPageSize         = service.AuditDBQueryMaxPageSize
	auditDBQuerySQLPreviewByteLimit = service.AuditDBQuerySQLPreviewByteLimit
	auditDBQuerySQLTruncatedMarker  = service.AuditDBQuerySQLTruncatedMarker
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

func (s *Server) writeAuditQueryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrAuditQueryForbidden):
		s.forbidden(w, r)
	case errors.Is(err, service.ErrAuditSessionNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, "audit session not found")
	default:
		if s.logger != nil {
			s.logger.Error("audit query failed", "error", err)
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, "audit query unavailable")
	}
}

func (s *Server) writeAuditJSONFile(w http.ResponseWriter, r *http.Request, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.writeErrorText(w, r, http.StatusNotFound, "not found")
			return
		}
		s.writeAuditFileError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(raw)
}

func (s *Server) writeAuditTextFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.writeErrorText(w, r, http.StatusNotFound, "not found")
			return
		}
		s.writeAuditFileError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(raw)
}

func (s *Server) writeAuditFileError(w http.ResponseWriter, r *http.Request, err error) {
	if s.logger != nil {
		s.logger.Error("audit artifact read failed", "error", err)
	}
	s.writeErrorText(w, r, http.StatusInternalServerError, "audit artifact unavailable")
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
