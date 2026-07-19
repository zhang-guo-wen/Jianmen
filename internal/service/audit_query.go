package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

const (
	AuditQueryActionView   = "audit:view"
	AuditQueryDBActionView = "db_audit:view"
	AuditQuerySessionView  = "session:view"

	AuditDBQueryDefaultPageSize     = 50
	AuditDBQueryMaxPageSize         = 100
	AuditDBQuerySQLPreviewByteLimit = 64 * 1024
	AuditDBQuerySQLTruncatedMarker  = "\n/* [TRUNCATED SQL PREVIEW] */"
)

var (
	ErrAuditQueryForbidden       = errors.New("audit query forbidden")
	ErrAuditQueryInvalidProtocol = errors.New("invalid audit query protocol")
	ErrAuditArtifactUnavailable  = errors.New("audit artifact unavailable")
	ErrAuditSessionNotFound      = ErrAuditArtifactUnavailable
)

// AuditQueryRepository is deliberately limited to audit read models. It is
// owned by the query service rather than an application-wide store.
type AuditQueryRepository interface {
	ListAuditSessions(context.Context, AuditSessionListParams) ([]AuditSessionListItem, int64, error)
	GetAuditSessionAccessMetadata(context.Context, string) (AuditSessionAccessMetadata, error)
	GetFullAuditSession(context.Context, string) (AuditSession, error)
	ListSSHCommands(context.Context, string, Page) ([]AuditSSHCommand, int64, error)
	ListSFTPEvents(context.Context, string, Page) ([]AuditSFTPEvent, int64, error)
	ListDBQueryPreviews(context.Context, string, AuditDBQueryPreviewParams) ([]AuditDBQueryPreview, int64, error)
	ListAuditEvents(context.Context, AuditEventListParams) ([]AuditEvent, int64, error)
	ListLoginAuditLogs(context.Context, LoginAuditListParams) ([]LoginAuditLog, int64, error)
}

// AuditQueryAuthorizer has no resource grant concern: audit access is global
// and its protocol/state policy is enforced by AuditQueryService.
type AuditQueryAuthorizer interface {
	AuthorizeAuditQuery(context.Context, string, []string) (bool, error)
}

type AuditQueryService struct {
	repository AuditQueryRepository
	authorizer AuditQueryAuthorizer
}

func NewAuditQueryService(repository AuditQueryRepository, authorizer AuditQueryAuthorizer) (*AuditQueryService, error) {
	switch {
	case isNilAuditQueryDependency(repository):
		return nil, errors.New("audit query repository is required")
	case isNilAuditQueryDependency(authorizer):
		return nil, errors.New("audit query authorizer is required")
	default:
		return &AuditQueryService{repository: repository, authorizer: authorizer}, nil
	}
}

func isNilAuditQueryDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (s *AuditQueryService) ListSSH(ctx context.Context, userID string, params AuditSessionListParams) ([]AuditSessionListItem, int64, error) {
	if err := s.authorize(ctx, userID, AuditQueryActionView); err != nil {
		return nil, 0, err
	}
	params.Protocol = "ssh,sftp"
	items, total, err := s.repository.ListAuditSessions(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list SSH audit sessions: %w", err)
	}
	if err := auditQueryContextError(ctx, "list SSH audit sessions"); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *AuditQueryService) ListDB(ctx context.Context, userID string, params AuditSessionListParams) ([]AuditSessionListItem, int64, error) {
	if err := auditQueryContextError(ctx, "list database audit sessions"); err != nil {
		return nil, 0, err
	}
	protocol, err := normalizeDBAuditListProtocol(params.Protocol)
	if err != nil {
		return nil, 0, err
	}
	if err := s.authorize(ctx, userID, AuditQueryDBActionView); err != nil {
		return nil, 0, err
	}
	params.Protocol = protocol
	items, total, err := s.repository.ListAuditSessions(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list database audit sessions: %w", err)
	}
	if err := auditQueryContextError(ctx, "list database audit sessions"); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *AuditQueryService) ListOperations(ctx context.Context, userID string, params AuditEventListParams) ([]AuditEvent, int64, error) {
	if err := s.authorize(ctx, userID, AuditQueryActionView); err != nil {
		return nil, 0, err
	}
	items, total, err := s.repository.ListAuditEvents(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit operations: %w", err)
	}
	if err := auditQueryContextError(ctx, "list audit operations"); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *AuditQueryService) ListLogins(ctx context.Context, userID string, params LoginAuditListParams) ([]LoginAuditLog, int64, error) {
	if err := s.authorize(ctx, userID, AuditQueryActionView); err != nil {
		return nil, 0, err
	}
	items, total, err := s.repository.ListLoginAuditLogs(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list login audit logs: %w", err)
	}
	if err := auditQueryContextError(ctx, "list login audit logs"); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// AuthorizedSession first reads non-sensitive access metadata, then loads the
// full session only after protocol, state, and permission checks succeed.
func (s *AuditQueryService) AuthorizedSession(ctx context.Context, userID, requestedProtocol, sessionID string) (AuditSession, error) {
	requestedFamily, ok := requestedAuditProtocolFamily(requestedProtocol)
	if !ok {
		return AuditSession{}, ErrAuditArtifactUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	metadata, err := s.repository.GetAuditSessionAccessMetadata(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrAuditArtifactUnavailable) {
			return AuditSession{}, ErrAuditArtifactUnavailable
		}
		return AuditSession{}, fmt.Errorf("get audit session access metadata: %w", err)
	}
	if err := auditQueryContextError(ctx, "get audit session access metadata"); err != nil {
		return AuditSession{}, err
	}
	actualFamily, actions, ok := auditSessionAccessPolicy(metadata)
	if !ok || requestedFamily != actualFamily {
		return AuditSession{}, ErrAuditArtifactUnavailable
	}
	if err := s.authorize(ctx, userID, actions...); err != nil {
		if errors.Is(err, ErrAuditQueryForbidden) {
			return AuditSession{}, ErrAuditArtifactUnavailable
		}
		return AuditSession{}, err
	}
	session, err := s.repository.GetFullAuditSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrAuditArtifactUnavailable) {
			return AuditSession{}, ErrAuditArtifactUnavailable
		}
		return AuditSession{}, fmt.Errorf("get full audit session: %w", err)
	}
	if err := auditQueryContextError(ctx, "get full audit session"); err != nil {
		return AuditSession{}, err
	}
	if !sameAuditSessionAccessMetadata(metadata, session) {
		return AuditSession{}, ErrAuditArtifactUnavailable
	}
	session.ProtocolFamily = actualFamily
	return session, nil
}

func (s *AuditQueryService) SSHCommands(ctx context.Context, userID, protocol, sessionID string, page Page) ([]AuditSSHCommand, int64, error) {
	session, err := s.AuthorizedSession(ctx, userID, protocol, sessionID)
	if err != nil {
		return nil, 0, err
	}
	if session.ProtocolFamily != AuditProtocolFamilySSH {
		return nil, 0, ErrAuditArtifactUnavailable
	}
	items, total, err := s.repository.ListSSHCommands(ctx, sessionID, page)
	if err != nil {
		return nil, 0, fmt.Errorf("list SSH audit commands: %w", err)
	}
	if err := auditQueryContextError(ctx, "list SSH audit commands"); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *AuditQueryService) SFTPEvents(ctx context.Context, userID, protocol, sessionID string, page Page) ([]AuditSFTPEvent, int64, error) {
	session, err := s.AuthorizedSession(ctx, userID, protocol, sessionID)
	if err != nil {
		return nil, 0, err
	}
	if session.ProtocolFamily != AuditProtocolFamilySSH {
		return nil, 0, ErrAuditArtifactUnavailable
	}
	items, total, err := s.repository.ListSFTPEvents(ctx, sessionID, page)
	if err != nil {
		return nil, 0, fmt.Errorf("list SFTP audit events: %w", err)
	}
	if err := auditQueryContextError(ctx, "list SFTP audit events"); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *AuditQueryService) DBQueryEvents(ctx context.Context, userID, protocol, sessionID string, params AuditDBQueryPreviewParams) ([]AuditDBQueryEvent, int64, error) {
	session, err := s.AuthorizedSession(ctx, userID, protocol, sessionID)
	if err != nil {
		return nil, 0, err
	}
	if session.ProtocolFamily != AuditProtocolFamilyDB {
		return nil, 0, ErrAuditArtifactUnavailable
	}
	items, total, err := s.repository.ListDBQueryPreviews(ctx, sessionID, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list database audit query previews: %w", err)
	}
	if err := auditQueryContextError(ctx, "list database audit query previews"); err != nil {
		return nil, 0, err
	}
	queryProtocol := session.Protocol
	if queryProtocol == "" {
		queryProtocol = protocol
	}
	events := make([]AuditDBQueryEvent, 0, len(items)*2)
	for i, query := range items {
		seq := int64(params.Offset + i)
		ts := query.Timestamp.UnixMilli()
		sqlPreview, detail := AuditDBQuerySQLPreview(query)
		events = append(events,
			AuditDBQueryEvent{Type: "query_started", ConnectionID: sessionID, Seq: seq, Protocol: queryProtocol, SQL: sqlPreview, QueryKind: query.QueryKind, Detail: detail, StartedAt: ts},
			AuditDBQueryEvent{Type: "query_finished", ConnectionID: sessionID, Seq: seq, Protocol: queryProtocol, QueryKind: query.QueryKind, StartedAt: ts, CompletedAt: ts, DurationMs: query.DurationMs, Status: "success"},
		)
	}
	return events, total, nil
}

func (s *AuditQueryService) authorize(ctx context.Context, userID string, actions ...string) error {
	if err := auditQueryContextError(ctx, "authorize audit query"); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return ErrAuditQueryForbidden
	}
	allowed, err := s.authorizer.AuthorizeAuditQuery(ctx, userID, actions)
	if err != nil {
		return fmt.Errorf("authorize audit query: %w", err)
	}
	if err := auditQueryContextError(ctx, "authorize audit query"); err != nil {
		return err
	}
	if !allowed {
		return ErrAuditQueryForbidden
	}
	return nil
}

func auditQueryContextError(ctx context.Context, operation string) error {
	if ctx == nil {
		return fmt.Errorf("%s: %w", operation, errors.New("audit query context is required"))
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func normalizeDBAuditListProtocol(protocol string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "":
		return "mysql,postgres,postgresql,redis", nil
	case "mysql":
		return "mysql", nil
	case "postgres", "postgresql":
		return "postgres,postgresql", nil
	case "redis":
		return "redis", nil
	default:
		return "", ErrAuditQueryInvalidProtocol
	}
}

func requestedAuditProtocolFamily(protocol string) (AuditProtocolFamily, bool) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "ssh", "sftp":
		return AuditProtocolFamilySSH, true
	case "db", "database", "mysql", "postgres", "postgresql", "redis":
		return AuditProtocolFamilyDB, true
	default:
		return "", false
	}
}

func storedAuditProtocolFamily(protocol string) (AuditProtocolFamily, bool) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "ssh", "sftp":
		return AuditProtocolFamilySSH, true
	case "db", "database", "mysql", "postgres", "postgresql", "redis":
		return AuditProtocolFamilyDB, true
	default:
		return "", false
	}
}

func auditSessionAccessPolicy(metadata AuditSessionAccessMetadata) (AuditProtocolFamily, []string, bool) {
	family, ok := storedAuditProtocolFamily(metadata.Protocol)
	if !ok {
		return "", nil, false
	}
	action := AuditQueryActionView
	if family == AuditProtocolFamilyDB {
		action = AuditQueryDBActionView
	}
	switch strings.ToLower(strings.TrimSpace(metadata.State)) {
	case "started":
		return family, []string{AuditQuerySessionView, action}, true
	case "ended":
		return family, []string{action}, true
	default:
		return "", nil, false
	}
}

func sameAuditSessionAccessMetadata(metadata AuditSessionAccessMetadata, session AuditSession) bool {
	return strings.TrimSpace(session.ID) == strings.TrimSpace(metadata.ID) &&
		strings.EqualFold(strings.TrimSpace(session.Protocol), strings.TrimSpace(metadata.Protocol)) &&
		strings.EqualFold(strings.TrimSpace(session.ProtocolSubtype), strings.TrimSpace(metadata.ProtocolSubtype)) &&
		strings.EqualFold(strings.TrimSpace(session.State), strings.TrimSpace(metadata.State))
}

func AuditDBQuerySQLPreview(query AuditDBQueryPreview) (string, map[string]any) {
	storedBytes := query.SQLStoredBytes
	if storedBytes <= 0 {
		storedBytes = int64(len(query.SQLText))
	}
	originalBytes := query.OriginalSQLBytes
	if originalBytes <= 0 {
		originalBytes = storedBytes
	}
	markerRequired := query.SQLTruncated || storedBytes > AuditDBQuerySQLPreviewByteLimit
	contentLimit := AuditDBQuerySQLPreviewByteLimit
	if markerRequired {
		contentLimit -= len(AuditDBQuerySQLTruncatedMarker)
	}
	if !utf8.ValidString(query.SQLText) {
		markerRequired = true
		contentLimit = AuditDBQuerySQLPreviewByteLimit - len(AuditDBQuerySQLTruncatedMarker)
	}
	preview, previewChanged := AuditDBQueryUTF8Prefix(query.SQLText, contentLimit)
	previewTruncated := previewChanged || int64(len(preview)) < storedBytes
	if markerRequired {
		preview += AuditDBQuerySQLTruncatedMarker
	}
	return preview, map[string]any{
		"sql_truncated": query.SQLTruncated || previewTruncated, "sql_audit_truncated": query.SQLTruncated,
		"sql_preview_truncated": previewTruncated, "sql_original_bytes": originalBytes,
		"sql_stored_bytes": storedBytes, "sql_preview_bytes": len(preview),
	}
}

func AuditDBQueryUTF8Prefix(value string, byteLimit int) (string, bool) {
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
