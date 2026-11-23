package model

import "time"

// AuditEvent records an auditable management operation.
type AuditEvent struct {
	ID               string    `gorm:"primaryKey;size:64" json:"id"`
	ActorID          string    `gorm:"index;size:64;not null" json:"actor_id"`
	ActorUsername    string    `gorm:"index;size:128" json:"actor_username"`
	ActorDisplayName string    `gorm:"-" json:"actor_display_name,omitempty"`
	Action        string    `gorm:"index;idx_audit_events_resource,priority:1;size:64;not null" json:"action"`
	ResourceType  string    `gorm:"index;idx_audit_events_resource,priority:2;size:64;not null" json:"resource_type"`
	ResourceID    string    `gorm:"index;idx_audit_events_resource,priority:3;size:64" json:"resource_id,omitempty"`
	ResourceName  string    `gorm:"size:255" json:"resource_name,omitempty"`
	// Operation 是业务动作键（例如 issue_credential）。为空表示历史记录或尚未归类，
	// 读取方应回退到 Action 展示，不要据此判断操作类型。
	Operation string `gorm:"index;size:64;not null;default:''" json:"operation,omitempty"`
	// ActorChannel 记录调用来源：admin_console（浏览器后台）、ai_api（AI 令牌）或空（未知）。
	ActorChannel string `gorm:"index:idx_audit_events_channel;size:32;not null;default:''" json:"actor_channel,omitempty"`
	// TargetLabel 是写入时生成的可读操作对象名，例如 47.107.41.233:22 · root。
	TargetLabel string    `gorm:"size:255" json:"target_label,omitempty"`
	Phase       string    `gorm:"index;index:idx_audit_events_phase_intent,priority:1;size:16;not null;default:''" json:"phase,omitempty"`
	Result      string    `gorm:"index;size:32;not null;default:''" json:"result,omitempty"`
	IntentID    string    `gorm:"index;index:idx_audit_events_phase_intent,priority:2;size:64;not null;default:''" json:"intent_id,omitempty"`
	RequestID   string    `gorm:"index;size:64;not null;default:''" json:"request_id,omitempty"`
	StatusCode  int       `gorm:"not null;default:0" json:"status_code,omitempty"`
	Detail      string    `gorm:"type:text" json:"detail,omitempty"`
	ClientIP    string    `gorm:"size:64" json:"client_ip,omitempty"`
	CreatedBy   string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

func (AuditEvent) TableName() string { return "audit_events" }
