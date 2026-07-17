package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	PermissionEffectAllow        = "allow"
	PermissionEffectDeny         = "deny"
	ResourceTypeGroup            = "resource_group"
	ResourceTypeHost             = "host"
	ResourceTypeHostAccount      = "host_account"
	ResourceTypeDatabaseAccount  = "database_account"
	ResourceTypeDatabaseInstance = "database_instance"
	ResourceTypeApplication      = "application"

	ResourceTypeContainerEndpoint = "container_endpoint"
	ResourceTypePlatformAccount   = "platform_account"
)

type UserPublicKey struct {
	ID          string     `gorm:"primaryKey;size:64" json:"id"`
	UserID      string     `gorm:"index;index:idx_user_public_keys_user_revoked,priority:1;size:64;not null" json:"user_id"`
	Name        string     `gorm:"size:128" json:"name,omitempty"`
	PublicKey   string     `gorm:"type:text;not null" json:"public_key"`
	Fingerprint string     `gorm:"index;size:128" json:"fingerprint,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `gorm:"index;index:idx_user_public_keys_user_revoked,priority:2" json:"revoked_at,omitempty"`
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
	Action       string    `gorm:"index;index:idx_permissions_action_resource,priority:1;size:128" json:"action"`
	ResourceType string    `gorm:"index;index:idx_permissions_action_resource,priority:2;size:64" json:"resource_type,omitempty"`
	ResourceID   string    `gorm:"index;index:idx_permissions_action_resource,priority:3;size:64" json:"resource_id,omitempty"`
	Effect       string    `gorm:"index:idx_permissions_action_resource,priority:4;size:16;not null;default:allow" json:"effect"`
	Description  string    `gorm:"type:text" json:"description,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RolePermission struct {
	ID           string     `gorm:"primaryKey;size:64" json:"id"`
	RoleID       string     `gorm:"uniqueIndex:idx_role_permissions_pair;size:64;not null" json:"role_id"`
	PermissionID string     `gorm:"uniqueIndex:idx_role_permissions_pair;index;size:64;not null" json:"permission_id"`
	CreatedAt    time.Time  `json:"created_at"`
	Role         Role       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	Permission   Permission `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type UserRole struct {
	ID        string     `gorm:"primaryKey;size:64" json:"id"`
	UserID    string     `gorm:"uniqueIndex:idx_user_roles_pair;index:idx_user_roles_user_expiry,priority:1;size:64;not null" json:"user_id"`
	RoleID    string     `gorm:"uniqueIndex:idx_user_roles_pair;index;size:64;not null" json:"role_id"`
	ExpiresAt *time.Time `gorm:"index;index:idx_user_roles_user_expiry,priority:2" json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	User      User       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	Role      Role       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type Application struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	Name           string    `gorm:"size:255;not null" json:"name"`
	AppGroup       string    `gorm:"size:128" json:"group"`
	ListenPort     int       `gorm:"uniqueIndex;not null" json:"listen_port"`
	Address        string    `gorm:"size:2048;not null;default:''" json:"address"`
	EntryPath      string    `gorm:"size:2048;not null;default:/" json:"entry_path"`
	InternalScheme string    `gorm:"size:8;not null;default:http" json:"internal_scheme"`
	InternalHost   string    `gorm:"size:255;not null" json:"internal_host"`
	InternalPort   int       `gorm:"not null;default:80" json:"internal_port"`
	Remark         string    `gorm:"type:text" json:"remark,omitempty"`
	Status         string    `gorm:"index;size:32;not null;default:active" json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
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
		&ResourceSequence{},
		&ResourceGroup{},
		&UserGroup{},
		&UserGroupMember{},
		&UserPreference{},
		&ConnectionPassword{},
		&AIAccessToken{},
		&ResourceGrant{},
		&Host{},
		&HostAccount{},
		&DatabaseInstance{},
		&DatabaseAccount{},
		&Application{},
		&ContainerEndpoint{},
		&Session{},
		&UserSession{},
		&TemporaryAccount{},
		&TemporaryCredential{},
		&TemporaryAccountGrant{},
		&PlatformAccount{},
		&AuditEvent{},
		&AuditSession{},
		&AuditSSHCommand{},
		&AuditDBQuery{},
		&AuditSFTPEvent{},
	}
}

func ensureID(id *string) error {
	if *id == "" {
		*id = NewID()
	}
	return nil
}

func (m *UserPublicKey) BeforeCreate(_ *gorm.DB) error     { return ensureID(&m.ID) }
func (m *Role) BeforeCreate(_ *gorm.DB) error              { return ensureID(&m.ID) }
func (m *Permission) BeforeCreate(_ *gorm.DB) error        { return ensureID(&m.ID) }
func (m *RolePermission) BeforeCreate(_ *gorm.DB) error    { return ensureID(&m.ID) }
func (m *UserRole) BeforeCreate(_ *gorm.DB) error          { return ensureID(&m.ID) }
func (m *Application) BeforeCreate(_ *gorm.DB) error       { return ensureID(&m.ID) }
func (m *ContainerEndpoint) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
func (m *UserSession) BeforeCreate(_ *gorm.DB) error       { return ensureID(&m.ID) }
func (m *PlatformAccount) BeforeCreate(_ *gorm.DB) error   { return ensureID(&m.ID) }
func (m *AuditEvent) BeforeCreate(_ *gorm.DB) error        { return ensureID(&m.ID) }
func (m *AuditSession) BeforeCreate(_ *gorm.DB) error      { return ensureID(&m.ID) }
func (m *AuditSSHCommand) BeforeCreate(_ *gorm.DB) error   { return ensureID(&m.ID) }
func (m *AuditDBQuery) BeforeCreate(_ *gorm.DB) error      { return ensureID(&m.ID) }
func (m *AuditSFTPEvent) BeforeCreate(_ *gorm.DB) error    { return ensureID(&m.ID) }
