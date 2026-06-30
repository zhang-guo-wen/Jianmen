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

type Resource struct {
	ID         string    `gorm:"primaryKey;size:64" json:"id"`
	Type       string    `gorm:"index;uniqueIndex:idx_resources_type_resource_id,priority:1;size:64;not null" json:"type"`
	ResourceID string    `gorm:"uniqueIndex:idx_resources_type_resource_id,priority:2;size:64" json:"resource_id"`
	Name       string    `gorm:"size:255" json:"name,omitempty"`
	ParentID   string    `gorm:"index;size:64" json:"parent_id,omitempty"`
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
	ResourceType string        `gorm:"uniqueIndex:idx_resource_group_members_pair;index:idx_resource_group_members_resource,priority:1;size:64;not null" json:"resource_type"`
	ResourceID   string        `gorm:"uniqueIndex:idx_resource_group_members_pair;index:idx_resource_group_members_resource,priority:2;size:64;not null" json:"resource_id"`
	CreatedAt    time.Time     `json:"created_at"`
	Group        ResourceGroup `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type Host struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	Name      string    `gorm:"size:255;not null" json:"name"`
	Address   string    `gorm:"index;index:idx_hosts_address_port,priority:1;size:255;not null" json:"address"`
	Port      int       `gorm:"index:idx_hosts_address_port,priority:2;not null;default:22" json:"port"`
	GroupName string    `gorm:"size:128" json:"group"`
	Remark    string    `gorm:"type:text" json:"remark,omitempty"`
	Status    string    `gorm:"index;size:32;not null;default:active" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type HostAccount struct {
	ID                    string         `gorm:"primaryKey;size:64" json:"id"`
	HostID                string         `gorm:"index;index:idx_host_accounts_host_username,priority:1;index:idx_host_accounts_host_status,priority:1;size:64;not null" json:"host_id"`
	Username              string         `gorm:"index:idx_host_accounts_host_username,priority:2;size:128;not null" json:"username"`
	AuthType              string         `gorm:"size:32" json:"auth_type,omitempty"`
	Password              EncryptedField `gorm:"type:text" json:"-"`
	PrivateKeyPEM         EncryptedField `gorm:"type:text" json:"-"`
	Passphrase            EncryptedField `gorm:"type:text" json:"-"`
	InsecureIgnoreHostKey bool           `gorm:"not null;default:false" json:"insecure_ignore_host_key"`
	HostKeyFingerprint    string         `gorm:"size:128" json:"host_key_fingerprint,omitempty"`
	KnownHostsPath        string         `gorm:"size:255" json:"known_hosts_path,omitempty"`
	Status                string         `gorm:"index;index:idx_host_accounts_host_status,priority:2;index:idx_host_accounts_status_expires,priority:1;size:32;not null;default:active" json:"status"`
	ResourceSeq           int            `gorm:"index;not null;default:0" json:"resource_seq"`
	ResourceID            string         `gorm:"uniqueIndex;size:4" json:"resource_id"`
	GroupName             string         `gorm:"size:128" json:"group"`
	Remark                string         `gorm:"type:text" json:"remark,omitempty"`
	ExpiresAt             *time.Time     `gorm:"index;index:idx_host_accounts_status_expires,priority:2" json:"expires_at"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
	Host                  Host           `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type DatabaseInstance struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	Name      string    `gorm:"uniqueIndex;size:255;not null" json:"name"`
	Protocol  string    `gorm:"index:idx_database_instances_endpoint,priority:1;size:32;not null;default:mysql" json:"protocol"`
	Address   string    `gorm:"index:idx_database_instances_endpoint,priority:2;size:255;not null" json:"address"`
	Port      int       `gorm:"index:idx_database_instances_endpoint,priority:3;not null;default:3306" json:"port"`
	GroupName string    `gorm:"size:128" json:"group"`
	Remark    string    `gorm:"type:text" json:"remark,omitempty"`
	Status    string    `gorm:"index;size:32;not null;default:active" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DatabaseAccount struct {
	ID          string           `gorm:"primaryKey;size:64" json:"id"`
	InstanceID  string           `gorm:"index;index:idx_database_accounts_instance_username,priority:1;index:idx_database_accounts_instance_status,priority:1;size:64;not null" json:"instance_id"`
	UniqueName  string           `gorm:"uniqueIndex;size:128;not null" json:"unique_name"`
	Username    string           `gorm:"index:idx_database_accounts_instance_username,priority:2;size:128;not null" json:"username"`
	Password    EncryptedField   `gorm:"type:text" json:"-"`
	GroupName   string           `gorm:"size:128" json:"group"`
	Remark      string           `gorm:"type:text" json:"remark,omitempty"`
	ExpiresAt   *time.Time       `gorm:"index;index:idx_database_accounts_status_expires,priority:2" json:"expires_at,omitempty"`
	Status      string           `gorm:"index;index:idx_database_accounts_instance_status,priority:2;index:idx_database_accounts_status_expires,priority:1;size:32;not null;default:active" json:"status"`
	ResourceSeq int              `gorm:"index;not null;default:0" json:"resource_seq"`
	ResourceID  string           `gorm:"uniqueIndex;size:4" json:"resource_id"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Instance    DatabaseInstance `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
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
	ExpiresAt          *time.Time       `gorm:"index;index:idx_temporary_credentials_validity,priority:2" json:"expires_at,omitempty"`
	RevokedAt          *time.Time       `gorm:"index;index:idx_temporary_credentials_validity,priority:1" json:"revoked_at,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	TemporaryAccount   TemporaryAccount `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

type TemporaryAccountGrant struct {
	ID                 string           `gorm:"primaryKey;size:64" json:"id"`
	TemporaryAccountID string           `gorm:"index;size:64;not null" json:"temporary_account_id"`
	UserID             string           `gorm:"index;index:idx_temporary_grants_match,priority:1;size:64" json:"user_id,omitempty"`
	Action             string           `gorm:"index;index:idx_temporary_grants_match,priority:2;size:128" json:"action,omitempty"`
	ResourceType       string           `gorm:"index;index:idx_temporary_grants_match,priority:3;size:64" json:"resource_type,omitempty"`
	ResourceID         string           `gorm:"index;index:idx_temporary_grants_match,priority:4;size:64" json:"resource_id,omitempty"`
	StartsAt           *time.Time       `gorm:"index" json:"starts_at,omitempty"`
	ExpiresAt          *time.Time       `gorm:"index;index:idx_temporary_grants_match,priority:5" json:"expires_at,omitempty"`
	CreatedBy          string           `gorm:"index;size:64" json:"created_by,omitempty"`
	RevokedAt          *time.Time       `gorm:"index;index:idx_temporary_grants_match,priority:6" json:"revoked_at,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	TemporaryAccount   TemporaryAccount `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
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
		&ResourceGroupMember{},
		&Host{},
		&HostAccount{},
		&DatabaseInstance{},
		&DatabaseAccount{},
		&Session{},
		&UserSession{},
		&TemporaryAccount{},
		&TemporaryCredential{},
		&TemporaryAccountGrant{},
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
func (m *DatabaseInstance) BeforeCreate(_ *gorm.DB) error    { return ensureID(&m.ID) }
func (m *DatabaseAccount) BeforeCreate(_ *gorm.DB) error     { return ensureID(&m.ID) }
func (m *UserSession) BeforeCreate(_ *gorm.DB) error         { return ensureID(&m.ID) }
func (m *TemporaryAccount) BeforeCreate(_ *gorm.DB) error    { return ensureID(&m.ID) }
func (m *TemporaryCredential) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
func (m *TemporaryAccountGrant) BeforeCreate(_ *gorm.DB) error {
	return ensureID(&m.ID)
}
