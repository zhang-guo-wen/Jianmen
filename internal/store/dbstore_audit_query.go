package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
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
}

// AuditDBQueryPreviewParams controls the bounded database query audit list.
type AuditDBQueryPreviewParams struct {
	Search string
	Limit  int
	Offset int
}

func (s *DBStore) CreateAuditDBQuery(query *model.AuditDBQuery) error {
	return s.db.Create(query).Error
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
			"original_sql_bytes, sql_truncated, query_kind, duration_ms",
		sqlStoredBytesExpression,
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

func escapeAuditDBQueryLikePattern(value string) string {
	return strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(value)
}
