package admin

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
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
		Search: strings.ToLower(firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("search"))),
		Date:   r.URL.Query().Get("date"),
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
		s.writeAuditArtifactUnavailable(w, r)
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
	case strings.HasPrefix(artifact, "commands/") && session.ProtocolFamily == service.AuditProtocolFamilySSH:
		s.handleAuditSSHCommandDetail(w, r, session, artifact)
	case artifact == "commands" && session.ProtocolFamily == service.AuditProtocolFamilySSH:
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
	case artifact == "files" && session.ProtocolFamily == service.AuditProtocolFamilySSH:
		limit, offset := pageFromQuery(r)
		items, total, err := s.auditQuery.SFTPEvents(r.Context(), userIDFromRequest(r), protocol, sessionID, service.Page{Limit: limit, Offset: offset})
		if err != nil {
			s.writeAuditQueryError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "file-summary" && session.ProtocolFamily == service.AuditProtocolFamilySSH:
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
	case artifact == "replay" && session.ProtocolFamily == service.AuditProtocolFamilySSH:
		replayPath := session.ReplayDir
		if replayPath == "" {
			s.writeErrorText(w, r, http.StatusNotFound, "no replay available")
			return
		}
		s.writeAuditTextFile(w, r, filepath.Join(replayPath, "terminal.cast"), "application/x-asciicast; charset=utf-8")
	case strings.HasPrefix(artifact, "queries/") && session.ProtocolFamily == service.AuditProtocolFamilyDB:
		s.handleAuditDBQueryDetail(w, r, protocol, sessionID, artifact)
	case artifact == "queries" && session.ProtocolFamily == service.AuditProtocolFamilyDB:
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
		s.writeAuditArtifactUnavailable(w, r)
	}
}

func (s *Server) handleAuditSSHCommandDetail(
	w http.ResponseWriter,
	r *http.Request,
	session service.AuditSession,
	artifactPath string,
) {
	parts := strings.Split(artifactPath, "/")
	if len(parts) != 3 || parts[0] != "commands" || session.ReplayDir == "" {
		s.writeAuditArtifactUnavailable(w, r)
		return
	}
	seq, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || seq <= 0 {
		s.writeAuditArtifactUnavailable(w, r)
		return
	}
	command, err := readRecordedSSHCommand(
		filepath.Join(session.ReplayDir, "commands.jsonl"),
		seq,
	)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.writeAuditArtifactUnavailable(w, r)
			return
		}
		s.writeAuditFileError(w, r, err)
		return
	}
	switch parts[2] {
	case "detail":
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			"seq":                 command.Seq,
			"command":             command.Command,
			"output_preview":      command.Preview,
			"output_bytes":        command.OutputBytes,
			"audit_data_redacted": command.AuditDataRedacted,
			"confidence":          command.Confidence,
			"started_at":          command.StartedAt,
			"ended_at":            command.EndedAt,
		})
	case "output":
		if command.OutputBytes <= 0 {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
			return
		}
		s.writeAuditFileSection(
			w,
			r,
			filepath.Join(session.ReplayDir, "commands-data.bin"),
			command.OutputOffset,
			command.OutputBytes,
			"text/plain; charset=utf-8",
			command.AuditDataRedacted,
		)
	default:
		s.writeAuditArtifactUnavailable(w, r)
	}
}

func readRecordedSSHCommand(path string, seq int64) (recordedSSHCommand, error) {
	file, err := os.Open(path)
	if err != nil {
		return recordedSSHCommand{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var command recordedSSHCommand
		if json.Unmarshal(scanner.Bytes(), &command) == nil && command.Seq == seq {
			return command, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return recordedSSHCommand{}, err
	}
	return recordedSSHCommand{}, os.ErrNotExist
}

func (s *Server) handleAuditDBQueryDetail(
	w http.ResponseWriter,
	r *http.Request,
	protocol,
	sessionID,
	artifactPath string,
) {
	parts := strings.Split(artifactPath, "/")
	if len(parts) != 3 || parts[0] != "queries" || strings.TrimSpace(parts[1]) == "" {
		s.writeAuditArtifactUnavailable(w, r)
		return
	}
	session, artifact, err := s.auditQuery.DBQueryArtifact(
		r.Context(),
		userIDFromRequest(r),
		protocol,
		sessionID,
		parts[1],
	)
	if err != nil {
		s.writeAuditQueryError(w, r, err)
		return
	}
	if parts[2] == "detail" {
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			"query_id":            artifact.ID,
			"sql_bytes":           artifact.SQLLogBytes,
			"parameter_bytes":     artifact.ParameterLogBytes,
			"audit_data_redacted": artifact.AuditDataRedacted,
			"has_sql":             artifact.SQLLogBytes > 0,
			"has_parameters":      artifact.ParameterLogBytes > 0,
		})
		return
	}
	if session.ReplayDir == "" {
		s.writeAuditArtifactUnavailable(w, r)
		return
	}
	var offset, length int64
	contentType := "application/octet-stream"
	switch parts[2] {
	case "sql":
		offset, length = artifact.SQLLogOffset, artifact.SQLLogBytes
		contentType = "text/plain; charset=utf-8"
	case "parameters":
		offset, length = artifact.ParameterLogOffset, artifact.ParameterLogBytes
	default:
		s.writeAuditArtifactUnavailable(w, r)
		return
	}
	if offset < 0 || length <= 0 {
		s.writeAuditArtifactUnavailable(w, r)
		return
	}
	s.writeAuditFileSection(
		w,
		r,
		filepath.Join(session.ReplayDir, "query-data.bin"),
		offset,
		length,
		contentType,
		artifact.AuditDataRedacted,
	)
}

func (s *Server) writeAuditFileSection(
	w http.ResponseWriter,
	r *http.Request,
	path string,
	offset,
	length int64,
	contentType string,
	redacted bool,
) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.writeAuditArtifactUnavailable(w, r)
			return
		}
		s.writeAuditFileError(w, r, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || offset > info.Size() || length > info.Size()-offset {
		if err == nil {
			err = errors.New("database audit artifact section is incomplete")
		}
		s.writeAuditFileError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("X-Audit-Data-Redacted", strconv.FormatBool(redacted))
	w.WriteHeader(http.StatusOK)
	_, err = io.CopyN(w, io.NewSectionReader(file, offset, length), length)
	if err != nil && s.logger != nil && r.Context().Err() == nil {
		s.logger.Error("stream audit file section", "error", err)
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
	case errors.Is(err, service.ErrAuditQueryInvalidProtocol):
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid audit protocol")
	case errors.Is(err, service.ErrAuditArtifactUnavailable):
		s.writeAuditArtifactUnavailable(w, r)
	case errors.Is(err, service.ErrAuditQueryForbidden):
		s.forbidden(w, r)
	default:
		if s.logger != nil {
			s.logger.Error("audit query failed", "error", err)
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, "audit query unavailable")
	}
}

func (s *Server) writeAuditArtifactUnavailable(w http.ResponseWriter, r *http.Request) {
	s.writeErrorText(w, r, http.StatusNotFound, "audit session unavailable")
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
	} else if size > 1000 {
		size = 1000
	}
	if page <= 0 {
		page = 1
	}
	const maxPage = 1_000_000
	if page > maxPage {
		page = maxPage
	}
	return size, (page - 1) * size
}

type recordedSSHCommand struct {
	Seq               int64  `json:"seq"`
	OffsetMs          int64  `json:"offset_ms"`
	Command           string `json:"command"`
	Preview           string `json:"preview"`
	OutputOffset      int64  `json:"output_offset"`
	OutputBytes       int64  `json:"output_bytes"`
	AuditDataRedacted bool   `json:"audit_data_redacted"`
	Confidence        string `json:"confidence"`
	StartedAt         int64  `json:"started_at"`
	EndedAt           int64  `json:"ended_at"`
}

type auditSSHCommandOutput struct {
	Seq               int64  `json:"seq"`
	OffsetMs          int64  `json:"offset_ms"`
	Command           string `json:"command"`
	Output            string `json:"output"`
	OutputBytes       int64  `json:"output_bytes"`
	AuditDataRedacted bool   `json:"audit_data_redacted"`
	Confidence        string `json:"confidence"`
	StartedAt         int64  `json:"started_at"`
	EndedAt           int64  `json:"ended_at"`
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
				Seq:               recorded.Seq,
				OffsetMs:          recorded.OffsetMs,
				Command:           recorded.Command,
				Output:            recorded.Preview,
				OutputBytes:       recorded.OutputBytes,
				AuditDataRedacted: recorded.AuditDataRedacted,
				Confidence:        recorded.Confidence,
				StartedAt:         recorded.StartedAt,
				EndedAt:           recorded.EndedAt,
			})
		}
		total++
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
