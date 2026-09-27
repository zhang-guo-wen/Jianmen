package model

import "time"

// AuditSFTPEvent is an SFTP file operation audit record.
type AuditSFTPEvent struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index" json:"timestamp"`
	Action         string    `gorm:"size:32" json:"action"`
	Path           string    `gorm:"size:1024" json:"path"`
	Size           int64     `json:"size,omitempty"`
	Result         string    `gorm:"size:32" json:"result"`
	CreationAudit
}

func (AuditSFTPEvent) TableName() string { return "audit_sftp_events" }
