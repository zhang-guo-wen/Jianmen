package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	PermissionEffectAllow       = "allow"
	PermissionEffectDeny        = "deny"
	ResourceTypeGroup           = "resource_group"
	ResourceTypeHost            = "host"
	ResourceTypeHostAccount     = "host_account"
	ResourceTypeDatabaseAccount = "database_account"
)

type UserPublicKey struct {
	ID          string     `gorm:"primaryKey;size:64" json:"id"`
	UserID      string     `gorm:"index;size:64;not null" json:"user_id"`
	Name        string     `gorm:"size:128" json:"name,omitempty"`
	PublicKey   string     `gorm:"type:text;not null" json:"public_key"`
	Fingerprint string     `gorm:"index;size:128" json:"fingerprint,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	User        User       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type Role struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:128;not null" json:"name"`
	Description string    `gorm:"type:text" json:"description,omitempty"`
	Builtin     bool      `json:"builtin"`
	Status      string    `gorm:"size:32;not null;default:active" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Permission struct {
	ID           string    `gorm:"primaryKey;size:64" json:"id"`
	Name         string    `gorm:"index;size:128" json:"name,omitempty"`
	Action       string    `gorm:"index;size:128" json:"action"`
	ResourceType string    `gorm:"index;size:64" json:"resource_type,omitempty"`
	ResourceID   string    `gorm:"index;size:64" json:"resource_id,omitempty"`
	Effect       string    `gorm:"size:16;not null;default:allow" json:"effect"`
	Description  string    `gorm:"type:text" json:"description,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RolePermission struct {
	ID           string     `gorm:"primaryKey;size:64" json:"id"`
	RoleID       string     `gorm:"uniqueIndex:idx_role_permissions_pair;size:64;not null" json:"role_id"`
	PermissionID string     `gorm:"uniqueIndex:idx_role_permissions_pair;size:64;not null" json:"permission_id"`
	CreatedAt    time.Time  `json:"created_at"`
	Role         Role       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	Permission   Permission `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type UserRole struct {
	ID        string     `gorm:"primaryKey;size:64" json:"id"`
	UserID    string     `gorm:"uniqueIndex:idx_user_roles_pair;size:64;not null" json:"user_id"`
	RoleID    string     `gorm:"uniqueIndex:idx_user_roles_pair;size:64;not null" json:"role_id"`
	ExpiresAt *time.Time `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	User      User       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	Role      Role       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type Resource struct {
	ID         string    `gorm:"primaryKey;size:64" json:"id"`
	Type       string    `gorm:"index;size:64;not null" json:"type"`
	Name       string    `gorm:"size:255" json:"name,omitempty"`
	ParentID   string    `gorm:"index;size:64" json:"parent_id,omitempty"`
	Attributes string    `gorm:"type:text" json:"attributes,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ResourceGroup struct {
	ID           string    `gorm:"primaryKey;size:64" json:"id"`
	Name         string    `gorm:"uniqueIndex;size:128;not null" json:"name"`
	ResourceType string    `gorm:"index;size:64" json:"resource_type,omitempty"`
	Description  string    `gorm:"type:text" json:"description,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ResourceGroupMember struct {
	ID           string        `gorm:"primaryKey;size:64" json:"id"`
	GroupID      string        `gorm:"uniqueIndex:idx_resource_group_members_pair;size:64;not null" json:"group_id"`
	ResourceType string        `gorm:"uniqueIndex:idx_resource_group_members_pair;size:64;not null" json:"resource_type"`
	ResourceID   string        `gorm:"uniqueIndex:idx_resource_group_members_pair;size:64;not null" json:"resource_id"`
	CreatedAt    time.Time     `json:"created_at"`
	Group        ResourceGroup `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type Host struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	Name      string    `gorm:"size:255;not null" json:"name"`
	Address   string    `gorm:"index;size:255;not null" json:"address"`
	Port      int       `gorm:"not null;default:22" json:"port"`
	Protocol  string    `gorm:"size:32;not null;default:ssh" json:"protocol"`
	Labels    string    `gorm:"type:text" json:"labels,omitempty"`
	Status    string    `gorm:"index;size:32;not null;default:active" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type HostAccount struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	HostID        string    `gorm:"index;size:64;not null" json:"host_id"`
	Username      string    `gorm:"size:128;not null" json:"username"`
	AuthType      string    `gorm:"size:32" json:"auth_type,omitempty"`
	CredentialRef string    `gorm:"size:255" json:"credential_ref,omitempty"`
	IsPrivileged  bool      `json:"is_privileged"`
	Status        string    `gorm:"index;size:32;not null;default:active" json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Host          Host      `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type SessionCommand struct {
	ID         string     `gorm:"primaryKey;size:64" json:"id"`
	SessionID  string     `gorm:"index;size:64;not null" json:"session_id"`
	ChannelID  string     `gorm:"index;size:64" json:"channel_id,omitempty"`
	Seq        int64      `gorm:"index" json:"seq"`
	Command    string     `gorm:"type:text" json:"command"`
	OutputRef  string     `gorm:"size:512" json:"output_ref,omitempty"`
	Preview    string     `gorm:"type:text" json:"preview,omitempty"`
	Confidence string     `gorm:"size:32" json:"confidence,omitempty"`
	StartedAt  *time.Time `gorm:"index" json:"started_at,omitempty"`
	EndedAt    *time.Time `gorm:"index" json:"ended_at,omitempty"`
	OffsetMs   uint32     `json:"offset_ms,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	Session    Session    `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type SessionFileEvent struct {
	ID        string     `gorm:"primaryKey;size:64" json:"id"`
	SessionID string     `gorm:"index;size:64;not null" json:"session_id"`
	ChannelID string     `gorm:"index;size:64" json:"channel_id,omitempty"`
	Seq       int64      `gorm:"index" json:"seq"`
	Action    string     `gorm:"index;size:64" json:"action"`
	Path      string     `gorm:"type:text" json:"path,omitempty"`
	Path2     string     `gorm:"type:text" json:"path2,omitempty"`
	Handle    string     `gorm:"size:128" json:"handle,omitempty"`
	Offset    int64      `json:"offset,omitempty"`
	Size      int64      `json:"size,omitempty"`
	Result    string     `gorm:"index;size:32" json:"result,omitempty"`
	ErrorCode uint32     `json:"error_code,omitempty"`
	StartedAt *time.Time `gorm:"index" json:"started_at,omitempty"`
	EndedAt   *time.Time `gorm:"index" json:"ended_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	Session   Session    `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type Recording struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	SessionID     string    `gorm:"index;size:64;not null" json:"session_id"`
	Kind          string    `gorm:"index;size:64;not null" json:"kind"`
	Path          string    `gorm:"type:text;not null" json:"path"`
	Size          int64     `json:"size"`
	SHA256        string    `gorm:"size:128" json:"sha256,omitempty"`
	HashChainHead string    `gorm:"size:128" json:"hash_chain_head,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	Session       Session   `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type TemporaryAccount struct {
	ID          string     `gorm:"primaryKey;size:64" json:"id"`
	Username    string     `gorm:"uniqueIndex;size:128;not null" json:"username"`
	DisplayName string     `gorm:"size:128" json:"display_name,omitempty"`
	Status      string     `gorm:"index;size:32;not null;default:active" json:"status"`
	ExpiresAt   *time.Time `gorm:"index" json:"expires_at,omitempty"`
	CreatedBy   string     `gorm:"index;size:64" json:"created_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type TemporaryCredential struct {
	ID                 string           `gorm:"primaryKey;size:64" json:"id"`
	TemporaryAccountID string           `gorm:"index;size:64;not null" json:"temporary_account_id"`
	Type               string           `gorm:"size:32;not null" json:"type"`
	PublicKey          string           `gorm:"type:text" json:"public_key,omitempty"`
	SecretHash         string           `gorm:"size:255" json:"-"`
	Fingerprint        string           `gorm:"index;size:128" json:"fingerprint,omitempty"`
	ExpiresAt          *time.Time       `gorm:"index" json:"expires_at,omitempty"`
	RevokedAt          *time.Time       `gorm:"index" json:"revoked_at,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	TemporaryAccount   TemporaryAccount `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type TemporaryAccountGrant struct {
	ID                 string           `gorm:"primaryKey;size:64" json:"id"`
	TemporaryAccountID string           `gorm:"index;size:64;not null" json:"temporary_account_id"`
	UserID             string           `gorm:"index;size:64" json:"user_id,omitempty"`
	Action             string           `gorm:"index;size:128" json:"action,omitempty"`
	ResourceType       string           `gorm:"index;size:64" json:"resource_type,omitempty"`
	ResourceID         string           `gorm:"index;size:64" json:"resource_id,omitempty"`
	StartsAt           *time.Time       `gorm:"index" json:"starts_at,omitempty"`
	ExpiresAt          *time.Time       `gorm:"index" json:"expires_at,omitempty"`
	CreatedBy          string           `gorm:"index;size:64" json:"created_by,omitempty"`
	RevokedAt          *time.Time       `gorm:"index" json:"revoked_at,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	TemporaryAccount   TemporaryAccount `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type AuditLog struct {
	ID           string    `gorm:"primaryKey;size:64" json:"id"`
	ActorUserID  string    `gorm:"index;size:64" json:"actor_user_id,omitempty"`
	Action       string    `gorm:"index;size:128;not null" json:"action"`
	ResourceType string    `gorm:"index;size:64" json:"resource_type,omitempty"`
	ResourceID   string    `gorm:"index;size:64" json:"resource_id,omitempty"`
	Result       string    `gorm:"index;size:32" json:"result,omitempty"`
	IP           string    `gorm:"size:128" json:"ip,omitempty"`
	UserAgent    string    `gorm:"size:255" json:"user_agent,omitempty"`
	Detail       string    `gorm:"type:text" json:"detail,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func AllModels() []any {
	return []any{
		&User{},
		&UserPublicKey{},
		&Role{},
		&Permission{},
		&RolePermission{},
		&UserRole{},
		&Resource{},
		&ResourceGroup{},
		&ResourceGroupMember{},
		&Host{},
		&HostAccount{},
		&Session{},
		&SessionCommand{},
		&SessionFileEvent{},
		&Recording{},
		&TemporaryAccount{},
		&TemporaryCredential{},
		&TemporaryAccountGrant{},
		&AuditLog{},
	}
}

func ensureID(id *string) error {
	if *id == "" {
		*id = NewID()
	}
	return nil
}

func (m *UserPublicKey) BeforeCreate(_ *gorm.DB) error       { return ensureID(&m.ID) }
func (m *Role) BeforeCreate(_ *gorm.DB) error                { return ensureID(&m.ID) }
func (m *Permission) BeforeCreate(_ *gorm.DB) error          { return ensureID(&m.ID) }
func (m *RolePermission) BeforeCreate(_ *gorm.DB) error      { return ensureID(&m.ID) }
func (m *UserRole) BeforeCreate(_ *gorm.DB) error            { return ensureID(&m.ID) }
func (m *Resource) BeforeCreate(_ *gorm.DB) error            { return ensureID(&m.ID) }
func (m *ResourceGroup) BeforeCreate(_ *gorm.DB) error       { return ensureID(&m.ID) }
func (m *ResourceGroupMember) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
func (m *Host) BeforeCreate(_ *gorm.DB) error                { return ensureID(&m.ID) }
func (m *HostAccount) BeforeCreate(_ *gorm.DB) error         { return ensureID(&m.ID) }
func (m *SessionCommand) BeforeCreate(_ *gorm.DB) error      { return ensureID(&m.ID) }
func (m *SessionFileEvent) BeforeCreate(_ *gorm.DB) error    { return ensureID(&m.ID) }
func (m *Recording) BeforeCreate(_ *gorm.DB) error           { return ensureID(&m.ID) }
func (m *TemporaryAccount) BeforeCreate(_ *gorm.DB) error    { return ensureID(&m.ID) }
func (m *TemporaryCredential) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
func (m *TemporaryAccountGrant) BeforeCreate(_ *gorm.DB) error {
	return ensureID(&m.ID)
}
func (m *AuditLog) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
