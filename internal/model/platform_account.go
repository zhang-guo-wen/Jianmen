package model

import "time"

// PlatformAccount represents a credential for an external web platform
// (Jenkins, GitLab, Jira, etc.).
type PlatformAccount struct {
	ID           string         `gorm:"primaryKey;size:64" json:"id"`
	Name         string         `gorm:"size:255" json:"name"`
	PlatformName string         `gorm:"index;size:128;not null" json:"platform_name"`
	URL          string         `gorm:"size:512" json:"url,omitempty"`
	GroupName    string         `gorm:"size:128" json:"group,omitempty"`
	Username     string         `gorm:"size:255;not null" json:"username"`
	Password     EncryptedField `gorm:"type:text" json:"-"`
	TOTPSecret   EncryptedField `gorm:"type:text" json:"-"`
	Remark       string         `gorm:"type:text" json:"remark,omitempty"`
	OwnerID      string         `gorm:"index;size:64;not null" json:"owner_id"`
	Status       string         `gorm:"index;size:32;not null;default:active" json:"status"`
	ExpiresAt    *time.Time     `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Owner        User           `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

// AuditEvent records an auditable management operation.
type AuditEvent struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	ActorID       string    `gorm:"index;size:64;not null" json:"actor_id"`
	ActorUsername string    `gorm:"index;size:128" json:"actor_username"`
	Action        string    `gorm:"index;idx_audit_events_resource,priority:1;size:64;not null" json:"action"`
	ResourceType  string    `gorm:"index;idx_audit_events_resource,priority:2;size:64;not null" json:"resource_type"`
	ResourceID    string    `gorm:"index;idx_audit_events_resource,priority:3;size:64" json:"resource_id,omitempty"`
	ResourceName  string    `gorm:"size:255" json:"resource_name,omitempty"`
	Detail        string    `gorm:"type:text" json:"detail,omitempty"`
	ClientIP      string    `gorm:"size:64" json:"client_ip,omitempty"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
}
