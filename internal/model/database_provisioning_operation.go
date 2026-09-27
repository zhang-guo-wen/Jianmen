package model

import "time"

type DatabaseProvisioningOperation struct {
	ID                   string         `gorm:"primaryKey;size:64" json:"-"`
	Kind                 string         `gorm:"uniqueIndex:idx_dpo_actor_kind_idem_active,priority:2;index:idx_database_provisioning_kind_stage,priority:1;size:32;not null;default:create;check:chk_database_provisioning_kind,kind IN ('create','deprovision')" json:"-"`
	InstanceID           string         `gorm:"index:idx_database_provisioning_instance;size:64;not null" json:"-"`
	ActorID              string         `gorm:"uniqueIndex:idx_dpo_actor_kind_idem_active,priority:1;size:64;not null;default:''" json:"-"`
	IdempotencyKey       *string        `gorm:"uniqueIndex:idx_dpo_actor_kind_idem_active,priority:3;size:128;check:chk_database_provisioning_idempotency_key,idempotency_key IS NULL OR length(trim(idempotency_key)) > 0" json:"-"`
	CanonicalRequestHash string         `gorm:"size:64;not null;default:''" json:"-"`
	AdminAccountID       string         `gorm:"index;size:64;not null" json:"-"`
	UpstreamUsername     string         `gorm:"uniqueIndex:idx_dpo_upstream_username_active,priority:1;size:32;not null" json:"-"`
	Password             EncryptedField `gorm:"type:text;not null" json:"-"`
	Host                 string         `gorm:"size:255;not null" json:"-"`
	GrantsJSON           string         `gorm:"type:text;not null" json:"-"`
	GroupName            string         `gorm:"size:128;not null;default:''" json:"-"`
	Remark               string         `gorm:"type:text;not null" json:"-"`
	ExpiresAt            *time.Time     `gorm:"index" json:"-"`
	Stage                string         `gorm:"index:idx_database_provisioning_work,priority:1;index:idx_database_provisioning_kind_stage,priority:2;size:32;not null;default:reserved;check:chk_database_provisioning_stage,stage IN ('reserved','create_started','create_uncertain','upstream_created','grant_started','activation_pending','cleanup_required','cleanup_in_progress','not_created','active_managed','deprovision_requested','drop_started','drop_uncertain','dropped')" json:"-"`
	CleanupStatus        string         `gorm:"index:idx_database_provisioning_work,priority:2;size:32;not null;default:none;check:chk_database_provisioning_cleanup,cleanup_status IN ('none','required','in_progress','failed')" json:"-"`
	TerminalAt           *time.Time     `gorm:"index" json:"-"`
	ActiveRetainedAt     *time.Time     `gorm:"index" json:"-"`
	LastError            string         `gorm:"size:64;not null;default:''" json:"-"`
	AttemptCount         int            `gorm:"not null;default:0;check:chk_database_provisioning_attempts,attempt_count >= 0" json:"-"`
	LastAttemptAt        *time.Time     `gorm:"index" json:"-"`
	Revision             int64          `gorm:"not null;default:1;check:chk_database_provisioning_revision,revision > 0" json:"-"`
	LeaseOwner           string         `gorm:"size:64;not null;default:''" json:"-"`
	LeaseToken           string         `gorm:"size:64;not null;default:''" json:"-"`
	LeaseExpiresAt       *time.Time     `gorm:"index:idx_database_provisioning_work,priority:3" json:"-"`
	FullAudit
	Instance *DatabaseInstance `gorm:"foreignKey:InstanceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;" json:"-"`
}
