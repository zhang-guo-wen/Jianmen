package model

import "time"

// LoginAuditLog records both successful and rejected admin login attempts.
type LoginAuditLog struct {
	ID         string    `gorm:"primaryKey;size:64" json:"id"`
	UserID     string    `gorm:"index;size:64" json:"user_id,omitempty"`
	Username   string    `gorm:"index;size:128;not null" json:"username"`
	Phase      string    `gorm:"index;size:16;not null;default:''" json:"phase,omitempty"`
	Result     string    `gorm:"index;size:32;not null;default:''" json:"result,omitempty"`
	IntentID   string    `gorm:"index;size:64;not null;default:''" json:"intent_id,omitempty"`
	RequestID  string    `gorm:"index;size:64;not null;default:''" json:"request_id,omitempty"`
	StatusCode int       `gorm:"not null;default:0" json:"status_code,omitempty"`
	Outcome    string    `gorm:"index;size:32;not null" json:"outcome"`
	Reason     string    `gorm:"size:128" json:"reason,omitempty"`
	ClientIP   string    `gorm:"index;size:128" json:"client_ip"`
	UserAgent  string    `gorm:"size:512" json:"user_agent,omitempty"`
	CreatedBy  string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

func (LoginAuditLog) TableName() string { return "audit_login_logs" }
