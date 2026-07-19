package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
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
	ErrAuditQueryForbidden  = errors.New("audit query forbidden")
	ErrAuditSessionNotFound = errors.New("audit session not found")
)

// AuditQueryRepository is deliberately limited to audit read models. It is
// owned by the query service rather than an application-wide store.
type AuditQueryRepository interface {
	ListAuditSessions(context.Context, AuditSessionListParams) ([]AuditSessionListItem, int64, error)
	GetAuditSession(context.Context, string) (AuditSession, error)
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
	return items, total, nil
}

func (s *AuditQueryService) ListDB(ctx context.Context, userID string, params AuditSessionListParams) ([]AuditSessionListItem, int64, error) {
	if err := s.authorize(ctx, userID, AuditQueryDBActionView); err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(params.Protocol) == "" {
		params.Protocol = "mysql,postgres,redis"
	}
	items, total, err := s.repository.ListAuditSessions(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list database audit sessions: %w", err)
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
	return items, total, nil
}

// AuthorizedSession obtains a session only after a caller has one of the
// possible permissions for the requested protocol. A second policy check uses
// the authoritative session protocol and state.
func (s *AuditQueryService) AuthorizedSession(ctx context.Context, userID, requestedProtocol, sessionID string) (AuditSession, error) {
	requestedAction := auditActionForProtocol(requestedProtocol)
	if err := s.authorize(ctx, userID, AuditQuerySessionView, requestedAction); err != nil {
		return AuditSession{}, err
	}
	session, err := s.repository.GetAuditSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		if errors.Is(err, ErrAuditSessionNotFound) {
			return AuditSession{}, ErrAuditSessionNotFound
		}
		return AuditSession{}, fmt.Errorf("get audit session: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return AuditSession{}, err
	}
	action := auditActionForProtocol(session.Protocol)
	if session.State == "started" {
		err = s.authorize(ctx, userID, AuditQuerySessionView, action)
	} else {
		err = s.authorize(ctx, userID, action)
	}
	if err != nil {
		return AuditSession{}, err
	}
	return session, nil
}

func (s *AuditQueryService) SSHCommands(ctx context.Context, userID, protocol, sessionID string, page Page) ([]AuditSSHCommand, int64, error) {
	if _, err := s.AuthorizedSession(ctx, userID, protocol, sessionID); err != nil {
		return nil, 0, err
	}
	items, total, err := s.repository.ListSSHCommands(ctx, sessionID, page)
	if err != nil {
		return nil, 0, fmt.Errorf("list SSH audit commands: %w", err)
	}
	return items, total, nil
}

func (s *AuditQueryService) SFTPEvents(ctx context.Context, userID, protocol, sessionID string, page Page) ([]AuditSFTPEvent, int64, error) {
	if _, err := s.AuthorizedSession(ctx, userID, protocol, sessionID); err != nil {
		return nil, 0, err
	}
	items, total, err := s.repository.ListSFTPEvents(ctx, sessionID, page)
	if err != nil {
		return nil, 0, fmt.Errorf("list SFTP audit events: %w", err)
	}
	return items, total, nil
}

func (s *AuditQueryService) DBQueryEvents(ctx context.Context, userID, protocol, sessionID string, params AuditDBQueryPreviewParams) ([]AuditDBQueryEvent, int64, error) {
	session, err := s.AuthorizedSession(ctx, userID, protocol, sessionID)
	if err != nil {
		return nil, 0, err
	}
	items, total, err := s.repository.ListDBQueryPreviews(ctx, sessionID, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list database audit query previews: %w", err)
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
	if err := contextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return ErrAuditQueryForbidden
	}
	allowed, err := s.authorizer.AuthorizeAuditQuery(ctx, userID, actions)
	if err != nil {
		return fmt.Errorf("authorize audit query: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if !allowed {
		return ErrAuditQueryForbidden
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("audit query context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("audit query context: %w", err)
	}
	return nil
}

func auditActionForProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "mysql", "postgres", "postgresql", "redis", "db", "database":
		return AuditQueryDBActionView
	default:
		return AuditQueryActionView
	}
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

type AuditSessionListParams struct {
	Protocol, Search, Date string
	Page, Size             int
}
type Page struct{ Limit, Offset int }
type AuditDBQueryPreviewParams struct {
	Search        string
	Limit, Offset int
}
type AuditEventListParams struct {
	Search, Action, ResourceType, Date string
	Page, Size                         int
}
type LoginAuditListParams struct {
	Search, Outcome, Date string
	Page, Size            int
}

type AuditSessionListItem struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id,omitempty"`
	Username        string `json:"username"`
	Protocol        string `json:"protocol"`
	ProtocolSubtype string `json:"protocol_subtype,omitempty"`
	ResourceType    string `json:"resource_type,omitempty"`
	ResourceID      string `json:"resource_id,omitempty"`
	HostID          string `json:"host_id,omitempty"`
	AccountID       string `json:"account_id,omitempty"`
	TargetName      string `json:"target_name"`
	TargetAddress   string `json:"target_address,omitempty"`
	AccountName     string `json:"account_name,omitempty"`
	AccountUsername string `json:"account_username,omitempty"`
	ClientIP        string `json:"client_ip"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at,omitempty"`
	State           string `json:"state"`
	Outcome         string `json:"outcome,omitempty"`
	FailureCode     string `json:"failure_code,omitempty"`
	FailureMessage  string `json:"failure_message,omitempty"`
	RecordingStatus string `json:"recording_status,omitempty"`
	HasReplay       bool   `json:"has_replay"`
	LogCount        int64  `json:"log_count"`
}

type AuditSession struct {
	ID              string     `json:"id"`
	UserSessionID   string     `json:"user_session_id,omitempty"`
	UserID          string     `json:"user_id"`
	Username        string     `json:"username"`
	Protocol        string     `json:"protocol"`
	ProtocolSubtype string     `json:"protocol_subtype,omitempty"`
	ResourceType    string     `json:"resource_type,omitempty"`
	ResourceID      string     `json:"resource_id,omitempty"`
	HostID          string     `json:"host_id,omitempty"`
	AccountID       string     `json:"account_id,omitempty"`
	AccessRequestID string     `json:"access_request_id,omitempty"`
	TargetName      string     `json:"target_name"`
	TargetAddress   string     `json:"target_address"`
	AccountName     string     `json:"account_name"`
	AccountUsername string     `json:"account_username"`
	ClientIP        string     `json:"client_ip"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	State           string     `json:"state"`
	Outcome         string     `json:"outcome"`
	FailureCode     string     `json:"failure_code,omitempty"`
	FailureMessage  string     `json:"failure_message,omitempty"`
	PolicySnapshot  string     `json:"policy_snapshot,omitempty"`
	RecordingStatus string     `json:"recording_status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ReplayDir       string     `json:"-"`
}

type AuditSSHCommand struct {
	ID             string    `json:"id"`
	AuditSessionID string    `json:"audit_session_id"`
	Timestamp      time.Time `json:"timestamp"`
	Command        string    `json:"command"`
}
type AuditSFTPEvent struct {
	ID             string    `json:"id"`
	AuditSessionID string    `json:"audit_session_id"`
	Timestamp      time.Time `json:"timestamp"`
	Action         string    `json:"action"`
	Path           string    `json:"path"`
	Size           int64     `json:"size,omitempty"`
	Result         string    `json:"result"`
}
type AuditDBQueryPreview struct {
	ID, AuditSessionID, SQLText, QueryKind       string
	Timestamp                                    time.Time
	SQLStoredBytes, OriginalSQLBytes, DurationMs int64
	SQLTruncated                                 bool
}
type AuditEvent struct {
	ID            string    `json:"id"`
	ActorID       string    `json:"actor_id"`
	ActorUsername string    `json:"actor_username"`
	Action        string    `json:"action"`
	ResourceType  string    `json:"resource_type"`
	ResourceID    string    `json:"resource_id,omitempty"`
	ResourceName  string    `json:"resource_name,omitempty"`
	Detail        string    `json:"detail,omitempty"`
	ClientIP      string    `json:"client_ip,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}
type LoginAuditLog struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id,omitempty"`
	Username  string    `json:"username"`
	Outcome   string    `json:"outcome"`
	Reason    string    `json:"reason,omitempty"`
	ClientIP  string    `json:"client_ip"`
	UserAgent string    `json:"user_agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditDBQueryEvent struct {
	Type         string         `json:"type"`
	ConnectionID string         `json:"connection_id"`
	Seq          int64          `json:"seq"`
	Protocol     string         `json:"protocol"`
	SQL          string         `json:"sql,omitempty"`
	QueryKind    string         `json:"query_kind,omitempty"`
	Detail       map[string]any `json:"detail,omitempty"`
	StartedAt    int64          `json:"started_at,omitempty"`
	CompletedAt  int64          `json:"completed_at,omitempty"`
	DurationMs   int64          `json:"duration_ms,omitempty"`
	Status       string         `json:"status,omitempty"`
}
