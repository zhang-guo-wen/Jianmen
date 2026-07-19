package model

import "time"

// AuditDBQuery is a SQL or Redis command audit record.
type AuditDBQuery struct {
	ID             string    `gorm:"primaryKey;size:64;index:idx_audit_db_queries_session_timestamp_id,priority:3" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null;index:idx_audit_db_queries_session_timestamp_id,priority:1" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index;index:idx_audit_db_queries_session_timestamp_id,priority:2" json:"timestamp"`
	// 4294967295 maps to LONGTEXT on MySQL and TEXT on PostgreSQL/SQLite.
	// The configurable client limit can reach 16 MiB, one byte beyond the
	// MySQL MEDIUMTEXT maximum; audit redaction is separately bounded.
	SQLText          string `gorm:"size:4294967295" json:"sql_text"`
	OriginalSQLBytes int64  `gorm:"column:original_sql_bytes;not null;default:0" json:"sql_original_bytes"`
	SQLTruncated     bool   `gorm:"column:sql_truncated;not null;default:false" json:"sql_audit_truncated"`
	QueryKind        string `gorm:"size:32" json:"query_kind,omitempty"`
	DurationMs       int64  `json:"duration_ms,omitempty"`
}

func (AuditDBQuery) TableName() string { return "audit_db_queries" }
