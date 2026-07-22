package service

import "time"

type AuditProtocolFamily string

const (
	AuditProtocolFamilySSH AuditProtocolFamily = "ssh"
	AuditProtocolFamilyDB  AuditProtocolFamily = "db"
)

type AuditSessionListParams struct {
	Protocol, Search, Date string
	Page, Size             int
}

type Page struct{ Limit, Offset int }

type AuditDBQueryPreviewParams struct {
	Search        string
	Limit, Offset int
}

type AuditEventListParams struct {
	Search, Action, ResourceType, Date string
	Page, Size                         int
}

type LoginAuditListParams struct {
	Search, Outcome, Date string
	Page, Size            int
}

type AuditSessionAccessMetadata struct {
	ID              string
	Protocol        string
	ProtocolSubtype string
	State           string
}

type AuditSessionListItem struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id,omitempty"`
	Username        string `json:"username"`
	Protocol        string `json:"protocol"`
	ProtocolSubtype string `json:"protocol_subtype,omitempty"`
	ResourceType    string `json:"resource_type,omitempty"`
	ResourceID      string `json:"resource_id,omitempty"`
	HostID          string `json:"host_id,omitempty"`
	AccountID       string `json:"account_id,omitempty"`
	TargetName      string `json:"target_name"`
	TargetAddress   string `json:"target_address,omitempty"`
	AccountName     string `json:"account_name,omitempty"`
	AccountUsername string `json:"account_username,omitempty"`
	ClientIP        string `json:"client_ip"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at,omitempty"`
	State           string `json:"state"`
	Outcome         string `json:"outcome,omitempty"`
	FailureCode     string `json:"failure_code,omitempty"`
	FailureMessage  string `json:"failure_message,omitempty"`
	RecordingStatus string `json:"recording_status,omitempty"`
	HasReplay       bool   `json:"has_replay"`
	LogCount        int64  `json:"log_count"`
	SessionID       string `json:"session_id,omitempty"`
}

type AuditSession struct {
	ID              string              `json:"id"`
	UserSessionID   string              `json:"user_session_id,omitempty"`
	UserID          string              `json:"user_id"`
	Username        string              `json:"username"`
	Protocol        string              `json:"protocol"`
	ProtocolSubtype string              `json:"protocol_subtype,omitempty"`
	ResourceType    string              `json:"resource_type,omitempty"`
	ResourceID      string              `json:"resource_id,omitempty"`
	HostID          string              `json:"host_id,omitempty"`
	AccountID       string              `json:"account_id,omitempty"`
	TargetName      string              `json:"target_name"`
	TargetAddress   string              `json:"target_address"`
	AccountName     string              `json:"account_name"`
	AccountUsername string              `json:"account_username"`
	ClientIP        string              `json:"client_ip"`
	StartedAt       time.Time           `json:"started_at"`
	EndedAt         *time.Time          `json:"ended_at,omitempty"`
	State           string              `json:"state"`
	Outcome         string              `json:"outcome"`
	FailureCode     string              `json:"failure_code,omitempty"`
	FailureMessage  string              `json:"failure_message,omitempty"`
	PolicySnapshot  string              `json:"policy_snapshot,omitempty"`
	RecordingStatus string              `json:"recording_status"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	ReplayDir       string              `json:"-"`
	ProtocolFamily  AuditProtocolFamily `json:"-"`
}

type AuditSSHCommand struct {
	ID             string    `json:"id"`
	AuditSessionID string    `json:"audit_session_id"`
	Timestamp      time.Time `json:"timestamp"`
	Command        string    `json:"command"`
}

type AuditSFTPEvent struct {
	ID             string    `json:"id"`
	AuditSessionID string    `json:"audit_session_id"`
	Timestamp      time.Time `json:"timestamp"`
	Action         string    `json:"action"`
	Path           string    `json:"path"`
	Size           int64     `json:"size,omitempty"`
	Result         string    `json:"result"`
}

type AuditDBQueryPreview struct {
	ID, AuditSessionID, SQLText, QueryKind       string
	Timestamp                                    time.Time
	SQLStoredBytes, OriginalSQLBytes, DurationMs int64
	SQLTruncated                                 bool
}

type AuditEvent struct {
	ID            string    `json:"id"`
	ActorID       string    `json:"actor_id"`
	ActorUsername string    `json:"actor_username"`
	Action        string    `json:"action"`
	ResourceType  string    `json:"resource_type"`
	ResourceID    string    `json:"resource_id,omitempty"`
	ResourceName  string    `json:"resource_name,omitempty"`
	Detail        string    `json:"detail,omitempty"`
	ClientIP      string    `json:"client_ip,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type LoginAuditLog struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id,omitempty"`
	Username  string    `json:"username"`
	Outcome   string    `json:"outcome"`
	Reason    string    `json:"reason,omitempty"`
	ClientIP  string    `json:"client_ip"`
	UserAgent string    `json:"user_agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditDBQueryEvent struct {
	Type         string         `json:"type"`
	ConnectionID string         `json:"connection_id"`
	Seq          int64          `json:"seq"`
	Protocol     string         `json:"protocol"`
	SQL          string         `json:"sql,omitempty"`
	QueryKind    string         `json:"query_kind,omitempty"`
	Detail       map[string]any `json:"detail,omitempty"`
	StartedAt    int64          `json:"started_at,omitempty"`
	CompletedAt  int64          `json:"completed_at,omitempty"`
	DurationMs   int64          `json:"duration_ms,omitempty"`
	Status       string         `json:"status,omitempty"`
}
