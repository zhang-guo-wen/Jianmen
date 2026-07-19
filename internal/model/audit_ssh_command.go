package model

import "time"

// AuditSSHCommand is a shell command inferred from terminal input.
type AuditSSHCommand struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index" json:"timestamp"`
	Command        string    `gorm:"type:text" json:"command"`
}

func (AuditSSHCommand) TableName() string { return "audit_ssh_commands" }
