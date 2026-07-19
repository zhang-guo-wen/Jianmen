package model

import "time"

const (
	AuditOutcomeConnecting = "connecting"
	AuditOutcomeActive     = "active"
	AuditOutcomeSucceeded  = "succeeded"
	AuditOutcomeFailed     = "failed"
	AuditOutcomeDenied     = "denied"
	AuditOutcomeTerminated = "terminated"

	RecordingStatusNone      = "none"
	RecordingStatusPending   = "pending"
	RecordingStatusUploading = "uploading"
	RecordingStatusReady     = "ready"
	RecordingStatusFailed    = "failed"
)

// AuditSession is the authoritative metadata record for a proxied connection.
// Secrets and recording object keys are deliberately stored elsewhere.
type AuditSession struct {
	ID              string     `gorm:"primaryKey;size:64" json:"id"`
	UserSessionID   string     `gorm:"index;size:64" json:"user_session_id,omitempty"`
	UserID          string     `gorm:"index:idx_audit_sessions_user_started,priority:1;size:64" json:"user_id"`
	Username        string     `gorm:"index;size:128" json:"username"`
	Protocol        string     `gorm:"index:idx_audit_sessions_protocol_started,priority:1;size:32" json:"protocol"`
	ProtocolSubtype string     `gorm:"size:64" json:"protocol_subtype,omitempty"`
	ResourceType    string     `gorm:"index:idx_audit_sessions_resource_started,priority:1;size:64" json:"resource_type,omitempty"`
	ResourceID      string     `gorm:"index:idx_audit_sessions_resource_started,priority:2;size:64" json:"resource_id,omitempty"`
	HostID          string     `gorm:"index;size:64" json:"host_id,omitempty"`
	AccountID       string     `gorm:"index;size:64" json:"account_id,omitempty"`
	AccessRequestID string     `gorm:"index;size:64" json:"access_request_id,omitempty"`
	TargetName      string     `gorm:"size:255" json:"target_name"`
	TargetAddress   string     `gorm:"size:255" json:"target_address"`
	AccountName     string     `gorm:"size:128" json:"account_name"`
	AccountUsername string     `gorm:"size:128" json:"account_username"`
	ClientIP        string     `gorm:"size:128" json:"client_ip"`
	StartedAt       time.Time  `gorm:"index:idx_audit_sessions_protocol_started,priority:2;index:idx_audit_sessions_user_started,priority:2;index:idx_audit_sessions_session_started,priority:2;index:idx_audit_sessions_resource_started,priority:3" json:"started_at"`
	EndedAt         *time.Time `gorm:"index:idx_audit_sessions_cleanup,priority:2" json:"ended_at,omitempty"`
	State           string     `gorm:"index;size:32" json:"state"`
	Outcome         string     `gorm:"index;size:32" json:"outcome"`
	FailureCode     string     `gorm:"index;size:64" json:"failure_code,omitempty"`
	FailureMessage  string     `gorm:"type:text" json:"failure_message,omitempty"`
	PolicySnapshot  string     `gorm:"type:text" json:"policy_snapshot,omitempty"`
	RecordingStatus string     `gorm:"index;size:32" json:"recording_status"`
	ReplayDir       string     `gorm:"size:512" json:"-"` // Legacy SSH spool path; never expose through APIs.
	CleanupStatus   string     `gorm:"index:idx_audit_sessions_cleanup,priority:1;size:16;not null;default:ready" json:"-"`
	CleanupAt       *time.Time `gorm:"index" json:"-"`
	CleanupError    string     `gorm:"type:text" json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (AuditSession) TableName() string { return "audit_sessions" }
