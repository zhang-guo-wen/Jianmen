package model

import "time"

// AuditSession 审计会话元数据，所有协议共用。
type AuditSession struct {
	ID            string     `gorm:"primaryKey;size:64" json:"id"`
	UserSessionID string     `gorm:"index;size:64" json:"user_session_id,omitempty"`
	UserID        string     `gorm:"index;size:64" json:"user_id"`
	Username      string     `gorm:"index;size:128" json:"username"`
	Protocol      string     `gorm:"index:idx_audit_sessions_protocol_started,priority:1;size:32" json:"protocol"`
	TargetName    string     `gorm:"size:255" json:"target_name"`
	AccountName   string     `gorm:"size:128" json:"account_name"`
	ClientIP      string     `gorm:"size:128" json:"client_ip"`
	StartedAt     time.Time  `gorm:"index:idx_audit_sessions_protocol_started,priority:2;index:idx_audit_sessions_user_started,priority:2;index:idx_audit_sessions_session_started,priority:2" json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	State         string     `gorm:"size:32" json:"state"`
	ReplayDir     string     `gorm:"size:512" json:"replay_dir,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (AuditSession) TableName() string { return "audit_sessions" }

// AuditSSHCommand 从终端输入推断出的 shell 命令。
type AuditSSHCommand struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index" json:"timestamp"`
	Command        string    `gorm:"type:text" json:"command"`
}

func (AuditSSHCommand) TableName() string { return "audit_ssh_commands" }

// AuditDBQuery SQL 或 Redis 命令记录。
type AuditDBQuery struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index" json:"timestamp"`
	SQLText        string    `gorm:"type:text" json:"sql_text"`
	QueryKind      string    `gorm:"size:32" json:"query_kind,omitempty"`
	DurationMs     int64     `json:"duration_ms,omitempty"`
}

func (AuditDBQuery) TableName() string { return "audit_db_queries" }

// AuditSFTPEvent SFTP 文件操作事件。
type AuditSFTPEvent struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index" json:"timestamp"`
	Action         string    `gorm:"size:32" json:"action"`
	Path           string    `gorm:"size:1024" json:"path"`
	Size           int64     `json:"size,omitempty"`
	Result         string    `gorm:"size:32" json:"result"`
}

func (AuditSFTPEvent) TableName() string { return "audit_sftp_events" }
