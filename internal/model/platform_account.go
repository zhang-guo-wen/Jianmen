package model

import "time"

// PlatformAccount represents a credential for an external web platform
// (Jenkins, GitLab, Jira, etc.).
type PlatformAccount struct {
	ID           string         `gorm:"primaryKey;size:64" json:"id"`
	Name         string         `gorm:"size:255" json:"name"`
	PlatformName string         `gorm:"index;size:128;not null" json:"platform_name"`
	URL          string         `gorm:"size:512" json:"url,omitempty"`
	Category     string         `gorm:"index;size:64" json:"category,omitempty"`
	GroupName    string         `gorm:"size:128" json:"group,omitempty"`
	Username     string         `gorm:"size:255;not null" json:"username"`
	Password     EncryptedField `gorm:"type:text" json:"-"`
	TOTPSecret   EncryptedField `gorm:"type:text" json:"-"`
	Remark       string         `gorm:"type:text" json:"remark,omitempty"`
	OwnerID      string         `gorm:"index;index:idx_platform_accounts_owner_visibility,priority:1;size:64;not null" json:"owner_id"`
	Visibility   string         `gorm:"index;index:idx_platform_accounts_owner_visibility,priority:2;size:32;not null;default:private" json:"visibility"`
	Status       string         `gorm:"index;size:32;not null;default:active" json:"status"`
	ExpiresAt    *time.Time     `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Owner        User           `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

// PlatformAccountShare grants a user or role access to a platform account.
type PlatformAccountShare struct {
	ID                string          `gorm:"primaryKey;size:64" json:"id"`
	PlatformAccountID string          `gorm:"index;idx_platform_shares_account,priority:1;size:64;not null" json:"platform_account_id"`
	UserID            string          `gorm:"index;idx_platform_shares_user_role,priority:1;size:64" json:"user_id,omitempty"`
	RoleID            string          `gorm:"index;idx_platform_shares_user_role,priority:2;size:64" json:"role_id,omitempty"`
	AccessLevel       string          `gorm:"size:32;not null;default:view" json:"access_level"`
	ExpiresAt         *time.Time      `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	PlatformAccount   PlatformAccount `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	User              User            `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	Role              Role            `gorm:"foreignKey:RoleID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
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
