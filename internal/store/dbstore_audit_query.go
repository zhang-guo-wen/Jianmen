package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"

	"gorm.io/gorm"
)

const auditDBQueryPreviewMaxCharacters = 64 * 1024
const auditDBQueryPreviewMaxPageSize = 100

// AuditDBQueryPreview is the bounded projection used by database audit detail
// pages. SQLText is only a prefix of the persisted audit text; SQLStoredBytes
// records the byte length of the complete persisted value.
type AuditDBQueryPreview struct {
	ID               string
	AuditSessionID   string
	Timestamp        time.Time
	SQLText          string
	SQLStoredBytes   int64
	OriginalSQLBytes int64 `gorm:"column:original_sql_bytes"`
	SQLTruncated     bool  `gorm:"column:sql_truncated"`
	QueryKind        string
	DurationMs       int64
	Status           string
	ErrorCode        string
	ErrorMessage     string
	RowsAffected     *int64
	Rows             *int64
}

// AuditDBQueryPreviewParams controls the bounded database query audit list.
type AuditDBQueryPreviewParams struct {
	Search string
	Limit  int
	Offset int
}

func (s *DBStore) CreateAuditDBQuery(ctx context.Context, query *model.AuditDBQuery) error {
	if ctx == nil {
		return fmt.Errorf("create database audit query: nil context")
	}
	return s.db.WithContext(ctx).Create(query).Error
}

func (s *DBStore) UpdateAuditDBQueryDuration(ctx context.Context, id string, durationMs int64) error {
	if ctx == nil {
		return errors.New("update database query audit: nil context")
	}
	if durationMs < 0 {
		durationMs = 0
	}
	result := s.db.WithContext(ctx).Model(&model.AuditDBQuery{}).
		Where("id = ?", strings.TrimSpace(id)).
		Update("duration_ms", durationMs)
	if result.Error != nil {
		return fmt.Errorf("update database query audit: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("update database query audit %q: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}

func (s *DBStore) CompleteAuditDBQuery(
	ctx context.Context,
	id string,
	result model.AuditDBQueryResult,
) error {
	if ctx == nil {
		return errors.New("complete database query audit: nil context")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("complete database query audit: empty id")
	}
	if result.DurationMs < 0 {
		result.DurationMs = 0
	}
	updates := map[string]any{
		"duration_ms":   result.DurationMs,
		"status":        model.NormalizeAuditDBQueryStatus(result.Status),
		"error_code":    strings.TrimSpace(result.ErrorCode),
		"error_message": result.ErrorMessage,
		"rows_affected": result.RowsAffected,
		"rows":          result.Rows,
	}
	update := s.db.WithContext(ctx).
		Model(&model.AuditDBQuery{}).
		Where("id = ?", id).
		Updates(updates)
	if update.Error != nil {
		return fmt.Errorf("complete database query audit: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("complete database query audit %q: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}

func (s *DBStore) ListAuditDBQueryPreviews(
	ctx context.Context,
	sessionID string,
	params AuditDBQueryPreviewParams,
) ([]AuditDBQueryPreview, int64, error) {
	if ctx == nil {
		return nil, 0, fmt.Errorf("list database audit query previews: nil context")
	}
	q := s.db.WithContext(ctx).
		Model(&model.AuditDBQuery{}).
		Where("audit_session_id = ?", sessionID)
	if search := strings.ToLower(strings.TrimSpace(params.Search)); search != "" {
		q = q.Where(
			"LOWER(SUBSTR(sql_text, 1, ?)) LIKE ? ESCAPE '!'",
			auditDBQueryPreviewMaxCharacters,
			"%"+escapeAuditDBQueryLikePattern(search)+"%",
		)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var queries []AuditDBQueryPreview
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > auditDBQueryPreviewMaxPageSize {
		params.Limit = auditDBQueryPreviewMaxPageSize
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	sqlStoredBytesExpression := "OCTET_LENGTH(sql_text)"
	if s.db.Dialector.Name() == "sqlite" {
		sqlStoredBytesExpression = "LENGTH(CAST(sql_text AS BLOB))"
	}
	selectColumns := fmt.Sprintf(
		"id, audit_session_id, timestamp, SUBSTR(sql_text, 1, ?) AS sql_text, %s AS sql_stored_bytes, "+
			"original_sql_bytes, sql_truncated, query_kind, duration_ms, status, error_code, error_message, "+
			"rows_affected, %s AS %s",
		sqlStoredBytesExpression,
		quoteAuditDBQueryColumn(s.db, "rows"),
		quoteAuditDBQueryColumn(s.db, "rows"),
	)
	if err := q.
		Select(selectColumns, auditDBQueryPreviewMaxCharacters).
		Order("timestamp ASC, id ASC").
		Offset(params.Offset).
		Limit(params.Limit).
		Scan(&queries).Error; err != nil {
		return nil, 0, err
	}
	return queries, total, nil
}

func quoteAuditDBQueryColumn(db *gorm.DB, column string) string {
	var quoted strings.Builder
	db.Dialector.QuoteTo(&quoted, column)
	return quoted.String()
}

func escapeAuditDBQueryLikePattern(value string) string {
	return strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(value)
}
