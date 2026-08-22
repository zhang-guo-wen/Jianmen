package model

import (
	"strings"
	"time"
)

const (
	AuditDBQueryStatusSuccess      = "success"
	AuditDBQueryStatusError        = "error"
	AuditDBQueryStatusUnknown      = "unknown"
	AuditDBQueryStatusPolicyDenied = "policy_denied"
)

// AuditDBQuery is a SQL or Redis command audit record.
type AuditDBQuery struct {
	ID             string    `gorm:"primaryKey;size:64;index:idx_audit_db_queries_session_timestamp_id,priority:3" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null;index:idx_audit_db_queries_session_timestamp_id,priority:1" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index;index:idx_audit_db_queries_session_timestamp_id,priority:2" json:"timestamp"`
	// 4294967295 maps to LONGTEXT on MySQL and TEXT on PostgreSQL/SQLite.
	// The configurable wire limit can reach 256 MiB. SQLText remains a bounded
	// redacted summary, so large streamed PostgreSQL statements do not land here.
	SQLText            string `gorm:"size:4294967295" json:"sql_text"`
	OriginalSQLBytes   int64  `gorm:"column:original_sql_bytes;not null;default:0" json:"sql_original_bytes"`
	SQLTruncated       bool   `gorm:"column:sql_truncated;not null;default:false" json:"sql_audit_truncated"`
	SQLLogOffset       int64  `gorm:"column:sql_log_offset;not null;default:0" json:"-"`
	SQLLogBytes        int64  `gorm:"column:sql_log_bytes;not null;default:0" json:"sql_log_bytes"`
	ParameterLogOffset int64  `gorm:"column:parameter_log_offset;not null;default:0" json:"-"`
	ParameterLogBytes  int64  `gorm:"column:parameter_log_bytes;not null;default:0" json:"parameter_log_bytes"`
	AuditDataRedacted  bool   `gorm:"column:audit_data_redacted;not null;default:false" json:"audit_data_redacted"`
	QueryKind          string `gorm:"size:32" json:"query_kind,omitempty"`
	DurationMs         int64  `json:"duration_ms,omitempty"`
	Status             string `gorm:"size:32;not null;default:unknown" json:"status"`
	ErrorCode          string `gorm:"size:64" json:"error_code,omitempty"`
	ErrorMessage       string `gorm:"size:1024" json:"error_message,omitempty"`
	RowsAffected       *int64 `json:"rows_affected,omitempty"`
	Rows               *int64 `json:"rows,omitempty"`
	CreationAudit
}

type AuditDBQueryResult struct {
	DurationMs   int64
	Status       string
	ErrorCode    string
	ErrorMessage string
	RowsAffected *int64
	Rows         *int64
}

func (AuditDBQuery) TableName() string { return "audit_db_queries" }

func NormalizeAuditDBQueryStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case AuditDBQueryStatusSuccess:
		return AuditDBQueryStatusSuccess
	case AuditDBQueryStatusError:
		return AuditDBQueryStatusError
	case AuditDBQueryStatusPolicyDenied:
		return AuditDBQueryStatusPolicyDenied
	default:
		return AuditDBQueryStatusUnknown
	}
}
