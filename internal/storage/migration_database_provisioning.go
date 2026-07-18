package storage

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

// databaseAccountBeforeProvisioning is the exact account schema owned by
// migrations through 008. Later model fields belong exclusively to migration
// 009, so rerunning an earlier migration cannot create future columns or an
// FK to a table that does not exist yet.
type databaseAccountBeforeProvisioning struct {
	ID          string               `gorm:"primaryKey;size:64"`
	InstanceID  string               `gorm:"index;index:idx_database_accounts_instance_username,priority:1;index:idx_database_accounts_instance_status,priority:1;size:64;not null"`
	UniqueName  string               `gorm:"uniqueIndex;size:128;not null"`
	Username    string               `gorm:"index:idx_database_accounts_instance_username,priority:2;size:128;not null"`
	Password    model.EncryptedField `gorm:"type:text"`
	GroupName   string               `gorm:"size:128"`
	Remark      string               `gorm:"type:text"`
	ExpiresAt   *time.Time           `gorm:"index;index:idx_database_accounts_status_expires,priority:2"`
	Status      string               `gorm:"index;index:idx_database_accounts_instance_status,priority:2;index:idx_database_accounts_status_expires,priority:1;size:32;not null;default:active"`
	ResourceSeq int                  `gorm:"index;not null;default:0"`
	ResourceID  string               `gorm:"uniqueIndex;size:4"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Instance    model.DatabaseInstance `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

func (databaseAccountBeforeProvisioning) TableName() string {
	return "database_accounts"
}

func metadataModelsBeforeDatabaseProvisioning() []any {
	current := model.AllModels()
	beforeProvisioning := make([]any, 0, len(current))
	for _, item := range current {
		if _, isDatabaseAccount := item.(*model.DatabaseAccount); isDatabaseAccount {
			beforeProvisioning = append(beforeProvisioning, &databaseAccountBeforeProvisioning{})
			continue
		}
		beforeProvisioning = append(beforeProvisioning, item)
	}
	return beforeProvisioning
}

func migrateDatabaseAccountInstanceUsernameUniqueness(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&databaseAccountBeforeProvisioning{}) {
		return nil
	}
	if err := rejectDuplicateDatabaseAccounts(tx); err != nil {
		return err
	}
	const indexName = "uidx_database_accounts_instance_username"
	if tx.Migrator().HasIndex(&databaseAccountBeforeProvisioning{}, indexName) {
		return nil
	}
	if err := tx.Exec("CREATE UNIQUE INDEX uidx_database_accounts_instance_username ON database_accounts (instance_id, username)").Error; err != nil {
		return fmt.Errorf("create database account instance username index: %w", err)
	}
	return nil
}

// databaseProvisioningOperationSchema owns the physical 009 table. MySQL 8
// rejects a default on TEXT columns, so Remark remains required TEXT without
// a database default; the runtime model always writes its empty default.
type databaseProvisioningOperationSchema struct {
	ID                   string               `gorm:"primaryKey;size:64"`
	Kind                 string               `gorm:"size:32;not null;default:create;check:chk_database_provisioning_kind,kind IN ('create','deprovision')"`
	InstanceID           string               `gorm:"size:64;not null"`
	ActorID              string               `gorm:"size:64;not null;default:''"`
	IdempotencyKey       *string              `gorm:"size:128;check:chk_database_provisioning_idempotency_key,idempotency_key IS NULL OR length(trim(idempotency_key)) > 0"`
	CanonicalRequestHash string               `gorm:"size:64;not null;default:''"`
	AdminAccountID       string               `gorm:"size:64;not null"`
	UpstreamUsername     string               `gorm:"size:32;not null"`
	Password             model.EncryptedField `gorm:"type:text;not null"`
	Host                 string               `gorm:"size:255;not null"`
	GrantsJSON           string               `gorm:"type:text;not null"`
	GroupName            string               `gorm:"size:128;not null;default:''"`
	Remark               string               `gorm:"type:text;not null"`
	ExpiresAt            *time.Time
	Stage                string `gorm:"size:32;not null;default:reserved;check:chk_database_provisioning_stage,stage IN ('reserved','create_started','create_uncertain','upstream_created','grant_started','activation_pending','cleanup_required','cleanup_in_progress','not_created','active_managed','deprovision_requested','drop_started','drop_uncertain','dropped')"`
	CleanupStatus        string `gorm:"size:32;not null;default:none;check:chk_database_provisioning_cleanup,cleanup_status IN ('none','required','in_progress','failed')"`
	TerminalAt           *time.Time
	ActiveRetainedAt     *time.Time
	LastError            string `gorm:"size:64;not null;default:''"`
	AttemptCount         int    `gorm:"not null;default:0;check:chk_database_provisioning_attempts,attempt_count >= 0"`
	LastAttemptAt        *time.Time
	Revision             int64  `gorm:"not null;default:1;check:chk_database_provisioning_revision,revision > 0"`
	LeaseOwner           string `gorm:"size:64;not null;default:''"`
	LeaseToken           string `gorm:"size:64;not null;default:''"`
	LeaseExpiresAt       *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (databaseProvisioningOperationSchema) TableName() string {
	return "database_provisioning_operations"
}

// databaseAccountProvisioningSchema adds only the 009-owned account fields.
// Keeping the runtime association out of this migration schema avoids GORM
// recursively applying the runtime operation model (whose MySQL TEXT default
// is intentionally not used for the physical migration table).
type databaseAccountProvisioningSchema struct {
	ID                      string  `gorm:"primaryKey;size:64"`
	Managed                 bool    `gorm:"index:idx_database_accounts_managed_status,priority:1;not null;default:false;check:chk_database_accounts_managed_consistency,((managed = false AND upstream_host = '' AND provisioning_operation_id IS NULL) OR (managed = true AND upstream_host <> '' AND provisioning_operation_id IS NOT NULL))"`
	UpstreamHost            string  `gorm:"size:255;not null;default:''"`
	Status                  string  `gorm:"index:idx_database_accounts_managed_status,priority:2;size:32;not null;default:active"`
	ProvisioningOperationID *string `gorm:"uniqueIndex:uidx_database_accounts_provisioning_operation;size:64"`
}

func (databaseAccountProvisioningSchema) TableName() string {
	return "database_accounts"
}

// databaseProvisioningOperationAdminAccountLink adds the lifecycle FK that
// cannot be expressed by the operation model without exposing an unnecessary
// association to the rest of the application.
type databaseProvisioningOperationAdminAccountLink struct {
	ID             string                             `gorm:"primaryKey;size:64"`
	AdminAccountID string                             `gorm:"size:64;not null"`
	AdminAccount   *databaseAccountProvisioningSchema `gorm:"foreignKey:AdminAccountID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
}

func (databaseProvisioningOperationAdminAccountLink) TableName() string {
	return "database_provisioning_operations"
}

type databaseProvisioningOperationInstanceLink struct {
	ID         string                  `gorm:"primaryKey;size:64"`
	InstanceID string                  `gorm:"size:64;not null"`
	Instance   *model.DatabaseInstance `gorm:"foreignKey:InstanceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
}

func (databaseProvisioningOperationInstanceLink) TableName() string {
	return "database_provisioning_operations"
}

type databaseAccountProvisioningOperationLink struct {
	ID                      string                               `gorm:"primaryKey;size:64"`
	ProvisioningOperationID *string                              `gorm:"size:64"`
	ProvisioningOperation   *databaseProvisioningOperationSchema `gorm:"foreignKey:ProvisioningOperationID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT;"`
}

func (databaseAccountProvisioningOperationLink) TableName() string {
	return "database_accounts"
}

func migrateDatabaseProvisioningSaga(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&databaseProvisioningOperationSchema{}); err != nil {
		return fmt.Errorf("migrate provisioning operation table: %w", err)
	}
	if err := tx.AutoMigrate(&databaseAccountProvisioningSchema{}); err != nil {
		return fmt.Errorf("migrate managed database account fields: %w", err)
	}
	if err := tx.AutoMigrate(&databaseAccountProvisioningOperationLink{}); err != nil {
		return fmt.Errorf("add managed account operation restriction: %w", err)
	}
	if err := tx.AutoMigrate(&databaseAccountBeforeProvisioning{}, &databaseAccountProvisioningSchema{}); err != nil {
		return fmt.Errorf("restore database account indexes: %w", err)
	}
	if err := tx.AutoMigrate(&databaseProvisioningOperationInstanceLink{}); err != nil {
		return fmt.Errorf("add provisioning instance restriction: %w", err)
	}
	if err := tx.AutoMigrate(&databaseProvisioningOperationAdminAccountLink{}); err != nil {
		return fmt.Errorf("add provisioning administrator restriction: %w", err)
	}
	if err := createDatabaseAccountIndexes(tx); err != nil {
		return err
	}
	return createDatabaseProvisioningIndexes(tx)
}

func createDatabaseAccountIndexes(tx *gorm.DB) error {
	for _, index := range []struct {
		name string
		sql  string
	}{
		{name: "idx_database_accounts_instance_id", sql: "CREATE INDEX idx_database_accounts_instance_id ON database_accounts (instance_id)"},
		{name: "idx_database_accounts_instance_username", sql: "CREATE INDEX idx_database_accounts_instance_username ON database_accounts (instance_id, username)"},
		{name: "uidx_database_accounts_instance_username", sql: "CREATE UNIQUE INDEX uidx_database_accounts_instance_username ON database_accounts (instance_id, username)"},
		{name: "idx_database_accounts_instance_status", sql: "CREATE INDEX idx_database_accounts_instance_status ON database_accounts (instance_id, status)"},
		{name: "idx_database_accounts_expires_at", sql: "CREATE INDEX idx_database_accounts_expires_at ON database_accounts (expires_at)"},
		{name: "idx_database_accounts_status", sql: "CREATE INDEX idx_database_accounts_status ON database_accounts (status)"},
		{name: "idx_database_accounts_status_expires", sql: "CREATE INDEX idx_database_accounts_status_expires ON database_accounts (status, expires_at)"},
		{name: "idx_database_accounts_resource_seq", sql: "CREATE INDEX idx_database_accounts_resource_seq ON database_accounts (resource_seq)"},
		{name: "idx_database_accounts_managed_status", sql: "CREATE INDEX idx_database_accounts_managed_status ON database_accounts (managed, status)"},
		{name: "uidx_database_accounts_provisioning_operation", sql: "CREATE UNIQUE INDEX uidx_database_accounts_provisioning_operation ON database_accounts (provisioning_operation_id)"},
	} {
		if tx.Migrator().HasIndex(&databaseAccountProvisioningSchema{}, index.name) {
			continue
		}
		if err := tx.Exec(index.sql).Error; err != nil {
			return fmt.Errorf("create database account index %s: %w", index.name, err)
		}
	}
	return nil
}

func createDatabaseProvisioningIndexes(tx *gorm.DB) error {
	for _, index := range []struct {
		name string
		sql  string
	}{
		{name: "idx_database_provisioning_instance", sql: "CREATE INDEX idx_database_provisioning_instance ON database_provisioning_operations (instance_id)"},
		{name: "idx_database_provisioning_admin_account_id", sql: "CREATE INDEX idx_database_provisioning_admin_account_id ON database_provisioning_operations (admin_account_id)"},
		{name: "idx_database_provisioning_work", sql: "CREATE INDEX idx_database_provisioning_work ON database_provisioning_operations (stage, cleanup_status, lease_expires_at)"},
		{name: "idx_database_provisioning_kind_stage", sql: "CREATE INDEX idx_database_provisioning_kind_stage ON database_provisioning_operations (kind, stage)"},
		{name: "uidx_database_provisioning_actor_kind_idempotency", sql: "CREATE UNIQUE INDEX uidx_database_provisioning_actor_kind_idempotency ON database_provisioning_operations (actor_id, kind, idempotency_key)"},
		{name: "uidx_database_provisioning_username", sql: "CREATE UNIQUE INDEX uidx_database_provisioning_username ON database_provisioning_operations (upstream_username)"},
		{name: "idx_database_provisioning_expires_at", sql: "CREATE INDEX idx_database_provisioning_expires_at ON database_provisioning_operations (expires_at)"},
		{name: "idx_database_provisioning_terminal_at", sql: "CREATE INDEX idx_database_provisioning_terminal_at ON database_provisioning_operations (terminal_at)"},
		{name: "idx_database_provisioning_active_retained_at", sql: "CREATE INDEX idx_database_provisioning_active_retained_at ON database_provisioning_operations (active_retained_at)"},
		{name: "idx_database_provisioning_last_attempt_at", sql: "CREATE INDEX idx_database_provisioning_last_attempt_at ON database_provisioning_operations (last_attempt_at)"},
		{name: "idx_database_provisioning_created_at", sql: "CREATE INDEX idx_database_provisioning_created_at ON database_provisioning_operations (created_at)"},
	} {
		if tx.Migrator().HasIndex(&databaseProvisioningOperationSchema{}, index.name) {
			continue
		}
		if err := tx.Exec(index.sql).Error; err != nil {
			return fmt.Errorf("create provisioning index %s: %w", index.name, err)
		}
	}
	return nil
}
