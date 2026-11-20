package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"jianmen/internal/rbac"
)

var ErrAuditBatchSearchTooLong = errors.New("audit batch search too long")

type AuditBatchListItem struct {
	AuditSessionListItem
	BatchID      string `json:"batch_id"`
	SessionCount int64  `json:"session_count"`
	FailureCount int64  `json:"failure_count"`
}

type AuditBatchLogItem struct {
	ID             string    `json:"id"`
	AuditSessionID string    `json:"audit_session_id"`
	Kind           string    `json:"kind"`
	Timestamp      time.Time `json:"timestamp"`
	Protocol       string    `json:"protocol,omitempty"`
	Text           string    `json:"text"`
	Action         string    `json:"action,omitempty"`
	Status         string    `json:"status,omitempty"`
	DurationMs     int64     `json:"duration_ms,omitempty"`
	Rows           *int64    `json:"rows,omitempty"`
	RowsAffected   *int64    `json:"rows_affected,omitempty"`
	ErrorCode      string    `json:"error_code,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	SQLTruncated   bool      `json:"sql_truncated,omitempty"`
	HasFullLog     bool      `json:"has_full_log,omitempty"`
}

type AuditBatchFilter struct {
	Protocol       string
	BatchID        string
	IncludeStarted bool
}

type AuditBatchQueryRepository interface {
	ListAuditBatches(context.Context, AuditBatchFilter, AuditSessionListParams) ([]AuditBatchListItem, int64, error)
	ListAuditBatchLogs(context.Context, AuditBatchFilter, AuditDBQueryPreviewParams) ([]AuditBatchLogItem, int64, error)
}

func validAuditBatchID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *AuditQueryService) batchAccess(ctx context.Context, userID, scope, id, dbProtocol string, detail bool) (AuditBatchFilter, error) {
	var protocol, action string
	switch scope {
	case "ssh":
		protocol, action = "ssh,sftp", AuditQueryActionView
	case "db":
		action = AuditQueryDBActionView
		var err error
		protocol, err = normalizeDBAuditListProtocol(dbProtocol)
		if err != nil {
			return AuditBatchFilter{}, err
		}
	default:
		return AuditBatchFilter{}, ErrAuditArtifactUnavailable
	}
	if detail && !validAuditBatchID(id) {
		return AuditBatchFilter{}, ErrAuditArtifactUnavailable
	}
	actions := []string{action}
	if scope == "db" {
		// 新批次入口识别目录权限，不改变旧单会话附件的授权规则。
		actions = append(actions, rbac.ActionDBAuditView)
	}
	if err := s.authorize(ctx, userID, actions...); err != nil {
		if detail && errors.Is(err, ErrAuditQueryForbidden) {
			return AuditBatchFilter{}, ErrAuditArtifactUnavailable
		}
		return AuditBatchFilter{}, err
	}
	// 授权器的动作列表是任意匹配；协议权限已通过，在线访问需独立检查。
	err := s.authorize(ctx, userID, AuditQuerySessionView)
	if err != nil && !errors.Is(err, ErrAuditQueryForbidden) {
		return AuditBatchFilter{}, err
	}
	return AuditBatchFilter{Protocol: protocol, BatchID: id, IncludeStarted: err == nil}, nil
}

func normalizeBatchSearch(search string) (string, error) {
	search = strings.TrimSpace(search)
	if len(search) > 1024 || utf8.RuneCountInString(search) > 256 || strings.ContainsRune(search, 0) {
		return "", ErrAuditBatchSearchTooLong
	}
	return strings.ToLower(search), nil
}

func (s *AuditQueryService) ListBatches(ctx context.Context, userID, scope string, params AuditSessionListParams) ([]AuditBatchListItem, int64, error) {
	filter, err := s.batchAccess(ctx, userID, scope, "", params.Protocol, false)
	if err != nil {
		return nil, 0, err
	}
	params.Search, err = normalizeBatchSearch(params.Search)
	if err != nil {
		return nil, 0, err
	}
	repo, ok := s.repository.(AuditBatchQueryRepository)
	if !ok {
		return nil, 0, errors.New("audit batch repository unavailable")
	}
	return repo.ListAuditBatches(ctx, filter, params)
}

func (s *AuditQueryService) BatchLogs(ctx context.Context, userID, scope, batchID string, params AuditDBQueryPreviewParams) ([]AuditBatchLogItem, int64, error) {
	filter, err := s.batchAccess(ctx, userID, scope, batchID, "", true)
	if err != nil {
		return nil, 0, err
	}
	params.Search, err = normalizeBatchSearch(params.Search)
	if err != nil {
		return nil, 0, err
	}
	repo, ok := s.repository.(AuditBatchQueryRepository)
	if !ok {
		return nil, 0, errors.New("audit batch repository unavailable")
	}
	items, total, err := repo.ListAuditBatchLogs(ctx, filter, params)
	if err != nil || scope != "db" {
		return items, total, err
	}
	// 只有原详情权限可用时，才对批次预览提供原始附件入口。
	if err := s.authorize(ctx, userID, AuditQueryDBActionView); err != nil {
		if !errors.Is(err, ErrAuditQueryForbidden) {
			return nil, 0, err
		}
		for i := range items {
			items[i].HasFullLog = false
		}
	}
	return items, total, nil
}

func (s *AuditQueryService) batchSessions(ctx context.Context, userID, scope string, params AuditSessionListParams) ([]AuditSessionListItem, int64, error) {
	filter, err := s.batchAccess(ctx, userID, scope, params.BatchID, params.Protocol, true)
	if err != nil {
		return nil, 0, err
	}
	params.Protocol = filter.Protocol
	params.BatchIncludeStarted = filter.IncludeStarted
	params.Search, err = normalizeBatchSearch(params.Search)
	if err != nil {
		return nil, 0, err
	}
	return s.repository.ListAuditSessions(ctx, params)
}
