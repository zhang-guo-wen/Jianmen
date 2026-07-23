package model

import "time"

// AuditRDPChannelEvent records channel metadata only. Clipboard contents and
// transferred file contents are intentionally never stored in the database.
type AuditRDPChannelEvent struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index;not null" json:"timestamp"`
	Channel        string    `gorm:"index;size:32;not null" json:"channel"`
	Direction      string    `gorm:"size:32" json:"direction,omitempty"`
	Operation      string    `gorm:"size:32" json:"operation,omitempty"`
	Bytes          int64     `json:"bytes,omitempty"`
	Outcome        string    `gorm:"size:32;not null" json:"outcome"`
	Reason         string    `gorm:"size:255" json:"reason,omitempty"`
	CreationAudit
}
