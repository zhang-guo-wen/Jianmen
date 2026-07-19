package storage

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const webRDPAuditMigrationVersion = "202607190003"

// These schema types freeze the tables changed by the Web RDP migration.
// Historical migrations must not use the current runtime models because doing
// so would silently install future columns when replayed on a fresh database.
type hostBeforeWebRDP struct {
	ID        string `gorm:"primaryKey;size:64"`
	Name      string `gorm:"size:255;not null"`
	Address   string `gorm:"index;index:idx_hosts_address_port,priority:1;size:255;not null"`
	Port      int    `gorm:"index:idx_hosts_address_port,priority:2;not null;default:22"`
	GroupName string `gorm:"size:128"`
	Remark    string `gorm:"type:text"`
	Status    string `gorm:"index;size:32;not null;default:active"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (hostBeforeWebRDP) TableName() string { return "hosts" }

type hostAccountBeforeWebRDP struct {
	ID                    string               `gorm:"primaryKey;size:64"`
	HostID                string               `gorm:"index;index:idx_host_accounts_host_username,priority:1;index:idx_host_accounts_host_status,priority:1;size:64;not null"`
	Name                  string               `gorm:"size:128;not null;default:''"`
	Username              string               `gorm:"index:idx_host_accounts_host_username,priority:2;size:128;not null"`
	AuthType              string               `gorm:"size:32"`
	Password              model.EncryptedField `gorm:"type:text"`
	PrivateKeyPEM         model.EncryptedField `gorm:"type:text"`
	Passphrase            model.EncryptedField `gorm:"type:text"`
	InsecureIgnoreHostKey bool                 `gorm:"not null;default:false"`
	HostKeyFingerprint    string               `gorm:"size:128"`
	KnownHostsPath        string               `gorm:"size:255"`
	Status                string               `gorm:"index;index:idx_host_accounts_host_status,priority:2;index:idx_host_accounts_status_expires,priority:1;size:32;not null;default:active"`
	ResourceSeq           int                  `gorm:"index;not null;default:0"`
	ResourceID            string               `gorm:"uniqueIndex;size:4"`
	GroupName             string               `gorm:"size:128"`
	Remark                string               `gorm:"type:text"`
	ExpiresAt             *time.Time           `gorm:"index;index:idx_host_accounts_status_expires,priority:2"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
	Host                  hostBeforeWebRDP `gorm:"foreignKey:HostID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

func (hostAccountBeforeWebRDP) TableName() string { return "host_accounts" }

type webSocketTicketBeforeWebRDP struct {
	ID         string     `gorm:"primaryKey;size:64"`
	SessionID  string     `gorm:"index;size:64;not null"`
	TargetID   string     `gorm:"index;size:64;not null"`
	SecretHash string     `gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt  time.Time  `gorm:"index;not null"`
	ConsumedAt *time.Time `gorm:"index"`
	CreatedAt  time.Time
}

func (webSocketTicketBeforeWebRDP) TableName() string { return "websocket_tickets" }

type webSocketTicketWebRDPSchema struct {
	ID           string     `gorm:"primaryKey;size:64"`
	SessionID    string     `gorm:"index;size:64;not null"`
	Purpose      string     `gorm:"index;size:32;not null;default:web-terminal"`
	TargetID     string     `gorm:"index;size:64;not null"`
	ConnectionID string     `gorm:"index;size:64"`
	SecretHash   string     `gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt    time.Time  `gorm:"index;not null"`
	ConsumedAt   *time.Time `gorm:"index"`
	CreatedAt    time.Time
}

func (webSocketTicketWebRDPSchema) TableName() string { return "websocket_tickets" }

type auditSessionBeforeWebRDP struct {
	ID              string    `gorm:"primaryKey;size:64"`
	UserSessionID   string    `gorm:"index;size:64"`
	UserID          string    `gorm:"index;size:64"`
	Username        string    `gorm:"index;size:128"`
	Protocol        string    `gorm:"index:idx_audit_sessions_protocol_started,priority:1;size:32"`
	ProtocolSubtype string    `gorm:"size:64"`
	TargetName      string    `gorm:"size:255"`
	TargetAddress   string    `gorm:"size:255"`
	AccountName     string    `gorm:"size:128"`
	AccountUsername string    `gorm:"size:128"`
	ClientIP        string    `gorm:"size:128"`
	StartedAt       time.Time `gorm:"index:idx_audit_sessions_protocol_started,priority:2;index:idx_audit_sessions_user_started,priority:2;index:idx_audit_sessions_session_started,priority:2"`
	EndedAt         *time.Time
	State           string `gorm:"size:32"`
	ReplayDir       string `gorm:"size:512"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (auditSessionBeforeWebRDP) TableName() string { return "audit_sessions" }

// auditSessionWebRDPSchema freezes the audit session columns owned by
// 202607190003 so later lease fields are installed only by their own migration.
type auditSessionWebRDPSchema struct {
	ID              string     `gorm:"primaryKey;size:64"`
	UserSessionID   string     `gorm:"index;size:64"`
	UserID          string     `gorm:"index:idx_audit_sessions_user_started,priority:1;size:64"`
	Username        string     `gorm:"index;size:128"`
	Protocol        string     `gorm:"index:idx_audit_sessions_protocol_started,priority:1;size:32"`
	ProtocolSubtype string     `gorm:"size:64"`
	ResourceType    string     `gorm:"index:idx_audit_sessions_resource_started,priority:1;size:64"`
	ResourceID      string     `gorm:"index:idx_audit_sessions_resource_started,priority:2;size:64"`
	HostID          string     `gorm:"index;size:64"`
	AccountID       string     `gorm:"index;size:64"`
	AccessRequestID string     `gorm:"index;size:64"`
	TargetName      string     `gorm:"size:255"`
	TargetAddress   string     `gorm:"size:255"`
	AccountName     string     `gorm:"size:128"`
	AccountUsername string     `gorm:"size:128"`
	ClientIP        string     `gorm:"size:128"`
	StartedAt       time.Time  `gorm:"index:idx_audit_sessions_protocol_started,priority:2;index:idx_audit_sessions_user_started,priority:2;index:idx_audit_sessions_session_started,priority:2;index:idx_audit_sessions_resource_started,priority:3"`
	EndedAt         *time.Time `gorm:"index:idx_audit_sessions_cleanup,priority:2"`
	State           string     `gorm:"index;size:32"`
	Outcome         string     `gorm:"index;size:32"`
	FailureCode     string     `gorm:"index;size:64"`
	FailureMessage  string     `gorm:"type:text"`
	PolicySnapshot  string     `gorm:"type:text"`
	RecordingStatus string     `gorm:"index;size:32"`
	ReplayDir       string     `gorm:"size:512"`
	CleanupStatus   string     `gorm:"index:idx_audit_sessions_cleanup,priority:1;size:16;not null;default:ready"`
	CleanupAt       *time.Time `gorm:"index"`
	CleanupError    string     `gorm:"type:text"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (auditSessionWebRDPSchema) TableName() string { return "audit_sessions" }

func metadataModelsBeforeWebRDP() []any {
	return []any{
		&model.User{},
		&model.AdminSession{},
		&webSocketTicketBeforeWebRDP{},
		&model.SystemInitialization{},
		&model.UserPublicKey{},
		&model.Role{},
		&model.Permission{},
		&model.RolePermission{},
		&model.UserRole{},
		&model.Resource{},
		&model.ResourceSequence{},
		&model.ResourceGroup{},
		&model.UserGroup{},
		&model.UserGroupMember{},
		&model.UserPreference{},
		&model.ConnectionPassword{},
		&model.AIAccessToken{},
		&model.ResourceGrant{},
		&hostBeforeWebRDP{},
		&hostAccountBeforeWebRDP{},
		&model.DatabaseInstance{},
		&model.DatabaseAccount{},
		&model.Application{},
		&model.ContainerEndpoint{},
		&model.Session{},
		&model.UserSession{},
		&model.TemporaryAccount{},
		&model.TemporaryCredential{},
		&model.TemporaryAccountGrant{},
		&model.PlatformAccount{},
		&model.AuditEvent{},
		&model.LoginAuditLog{},
		&auditSessionBeforeWebRDP{},
		&model.AuditSSHCommand{},
		&auditDBQueryBeforeLargePayload{},
		&model.AuditSFTPEvent{},
	}
}

func migrateWebRDPAuditSchema(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&model.Host{}, &model.HostAccount{}); err != nil {
		return fmt.Errorf("migrate RDP host account fields: %w", err)
	}
	if err := tx.AutoMigrate(&webSocketTicketWebRDPSchema{}); err != nil {
		return fmt.Errorf("migrate scoped websocket tickets: %w", err)
	}
	if err := tx.Model(&webSocketTicketWebRDPSchema{}).
		Where("purpose IS NULL OR purpose = ''").
		Update("purpose", "web-terminal").Error; err != nil {
		return fmt.Errorf("backfill websocket ticket purpose: %w", err)
	}
	if err := tx.AutoMigrate(
		&auditSessionWebRDPSchema{},
		&model.AuditArtifact{},
		&model.AuditRDPChannelEvent{},
		&model.AccessRequest{},
	); err != nil {
		return fmt.Errorf("migrate RDP audit and approval schema: %w", err)
	}
	return nil
}
