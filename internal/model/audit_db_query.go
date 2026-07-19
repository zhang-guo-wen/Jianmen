package model

import "time"

// AuditDBQuery is a SQL or Redis command audit record.
type AuditDBQuery struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index" json:"timestamp"`
	SQLText        string    `gorm:"type:text" json:"sql_text"`
	QueryKind      string    `gorm:"size:32" json:"query_kind,omitempty"`
	DurationMs     int64     `json:"duration_ms,omitempty"`
}

func (AuditDBQuery) TableName() string { return "audit_db_queries" }
