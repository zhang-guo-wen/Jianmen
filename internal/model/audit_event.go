package model

import "time"

// AuditEvent records an auditable management operation.
type AuditEvent struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	ActorID       string    `gorm:"index;size:64;not null" json:"actor_id"`
	ActorUsername string    `gorm:"index;size:128" json:"actor_username"`
	Action        string    `gorm:"index;idx_audit_events_resource,priority:1;size:64;not null" json:"action"`
	ResourceType  string    `gorm:"index;idx_audit_events_resource,priority:2;size:64;not null" json:"resource_type"`
	ResourceID    string    `gorm:"index;idx_audit_events_resource,priority:3;size:64" json:"resource_id,omitempty"`
	ResourceName  string    `gorm:"size:255" json:"resource_name,omitempty"`
	Phase         string    `gorm:"index;size:16;not null;default:''" json:"phase,omitempty"`
	Result        string    `gorm:"index;size:32;not null;default:''" json:"result,omitempty"`
	IntentID      string    `gorm:"index;size:64;not null;default:''" json:"intent_id,omitempty"`
	RequestID     string    `gorm:"index;size:64;not null;default:''" json:"request_id,omitempty"`
	StatusCode    int       `gorm:"not null;default:0" json:"status_code,omitempty"`
	Detail        string    `gorm:"type:text" json:"detail,omitempty"`
	ClientIP      string    `gorm:"size:64" json:"client_ip,omitempty"`
	CreatedBy     string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
}

func (AuditEvent) TableName() string { return "audit_events" }
