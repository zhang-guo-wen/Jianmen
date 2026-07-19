package webrdp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/objectstore"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func (h *Handler) AuditList(
	w http.ResponseWriter,
	r *http.Request,
	subject AuthenticatedSubject,
) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	params, page, size, err := auditListParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	params.Protocol = "rdp"
	params.Page = 1
	params.Size = 200
	visible := make([]store.AuditSessionView, 0)
	for {
		items, total, listErr := h.audit.ListAuditSessions(r.Context(), params)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to list RDP audit sessions")
			return
		}
		for _, item := range items {
			resourceID := firstNonEmpty(item.ResourceID, item.AccountID)
			allowed, authErr := h.authorizer.AuthorizeConnection(
				r.Context(), subject.UserID, []string{rbac.ActionRDPRecordingView},
				model.ResourceTypeHostAccount, resourceID,
			)
			if authErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to authorize RDP audit")
				return
			}
			if allowed {
				visible = append(visible, item)
			}
		}
		if int64(params.Page*params.Size) >= total {
			break
		}
		params.Page++
	}
	total := len(visible)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": visible[start:end], "total": total, "page": page, "page_size": size,
	})
}

func (h *Handler) AuditItem(
	w http.ResponseWriter,
	r *http.Request,
	subject AuthenticatedSubject,
) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/audit/rdp/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "recording" || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusNotFound, "RDP recording not found")
		return
	}
	session, err := h.audit.GetAuditSession(r.Context(), parts[0])
	if err != nil || !strings.EqualFold(session.Protocol, "rdp") {
		writeError(w, http.StatusNotFound, "RDP recording not found")
		return
	}
	resourceID := firstNonEmpty(session.ResourceID, session.AccountID)
	allowed, err := h.authorizer.AuthorizeConnection(
		r.Context(), subject.UserID, []string{rbac.ActionRDPRecordingView},
		model.ResourceTypeHostAccount, resourceID,
	)
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "RDP recording is not authorized")
		return
	}
	if session.RecordingStatus != model.RecordingStatusReady {
		writeError(w, http.StatusNotFound, "RDP recording is not ready")
		return
	}
	artifact, err := h.audit.AuditArtifactBySession(
		r.Context(), session.ID, model.AuditArtifactKindRecording,
	)
	if err != nil || artifact.Status != model.RecordingStatusReady {
		writeError(w, http.StatusNotFound, "RDP recording not found")
		return
	}
	info, err := h.objects.Stat(r.Context(), artifact.ObjectKey)
	if err != nil {
		if errors.Is(err, objectstore.ErrNotFound) {
			writeError(w, http.StatusNotFound, "RDP recording not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to inspect RDP recording")
		return
	}
	if err := validateRecordingMetadata(artifact, info); err != nil {
		h.logRecordingIntegrityFailure(session.ID, err)
		writeError(w, http.StatusConflict, "RDP recording integrity check failed")
		return
	}
	reader, err := h.objects.Open(r.Context(), artifact.ObjectKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open RDP recording")
		return
	}
	defer reader.Close()
	if err := verifyRecordingIntegrity(r.Context(), reader, artifact); err != nil {
		h.logRecordingIntegrityFailure(session.ID, err)
		writeError(w, http.StatusConflict, "RDP recording integrity check failed")
		return
	}
	contentType := firstNonEmpty(artifact.ContentType, info.ContentType, "application/octet-stream")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `inline; filename="recording.guac"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if info.ETag != "" {
		w.Header().Set("ETag", `"`+strings.Trim(info.ETag, `"`)+`"`)
	}
	http.ServeContent(w, r, "recording.guac", info.LastModified, reader)
}

func validateRecordingMetadata(
	artifact model.AuditArtifact,
	info objectstore.Info,
) error {
	if artifact.SizeBytes <= 0 || info.Size != artifact.SizeBytes {
		return fmt.Errorf(
			"recording size mismatch: object=%d index=%d",
			info.Size,
			artifact.SizeBytes,
		)
	}
	digest, err := hex.DecodeString(strings.TrimSpace(artifact.SHA256))
	if err != nil || len(digest) != sha256.Size {
		return errors.New("recording index has an invalid SHA-256 digest")
	}
	return nil
}

func verifyRecordingIntegrity(
	ctx context.Context,
	reader objectstore.Reader,
	artifact model.AuditArtifact,
) error {
	expected, _ := hex.DecodeString(strings.TrimSpace(artifact.SHA256))
	hash := sha256.New()
	size, err := io.Copy(hash, &contextReader{ctx: ctx, reader: reader})
	if err != nil {
		return fmt.Errorf("hash recording object: %w", err)
	}
	if size != artifact.SizeBytes ||
		subtle.ConstantTimeCompare(hash.Sum(nil), expected) != 1 {
		return errors.New("recording object SHA-256 does not match its audit index")
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind verified recording object: %w", err)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(payload []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(payload)
}

func (h *Handler) logRecordingIntegrityFailure(sessionID string, err error) {
	if h.logger != nil {
		h.logger.Error(
			"RDP recording integrity verification failed",
			"session_id",
			sessionID,
			"error",
			err,
		)
	}
}

func auditListParams(r *http.Request) (store.AuditListParams, int, int, error) {
	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	size, _ := strconv.Atoi(firstNonEmpty(query.Get("page_size"), query.Get("size")))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	params := store.AuditListParams{
		Search:    strings.TrimSpace(firstNonEmpty(query.Get("q"), query.Get("search"))),
		UserID:    strings.TrimSpace(query.Get("user_id")),
		AccountID: strings.TrimSpace(query.Get("account_id")),
		Outcome:   strings.TrimSpace(firstNonEmpty(query.Get("outcome"), query.Get("result"))),
	}
	var err error
	params.StartedFrom, err = parseOptionalTime(firstNonEmpty(query.Get("from"), query.Get("started_from")))
	if err != nil {
		return store.AuditListParams{}, 0, 0, err
	}
	params.StartedTo, err = parseOptionalTime(firstNonEmpty(query.Get("to"), query.Get("started_to")))
	if err != nil {
		return store.AuditListParams{}, 0, 0, err
	}
	if params.StartedFrom != nil && params.StartedTo != nil &&
		!params.StartedFrom.Before(*params.StartedTo) {
		return store.AuditListParams{}, 0, 0, errors.New("audit time range is invalid")
	}
	return params, page, size, nil
}

func parseOptionalTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("audit time must use RFC3339")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
