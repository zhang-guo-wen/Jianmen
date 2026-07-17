package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	TemporaryAccountTypeUser = "temporary_user"
	TemporaryAccountTypeAI   = "ai_user"
)

// TemporaryAccount is a non-login principal used for bounded grants and AI access.
type TemporaryAccount struct {
	ID               string     `gorm:"primaryKey;size:64" json:"id"`
	SessionID        string     `gorm:"uniqueIndex;size:128;not null" json:"session_id"`
	Type             string     `gorm:"index;size:32;not null" json:"type"`
	Username         string     `gorm:"uniqueIndex;size:128;not null" json:"username"`
	AuthorizedUserID string     `gorm:"index;size:64" json:"authorized_user_id,omitempty"`
	Status           string     `gorm:"index;size:32;not null;default:active" json:"status"`
	StartsAt         time.Time  `gorm:"index" json:"starts_at"`
	ExpiresAt        *time.Time `gorm:"index" json:"expires_at,omitempty"`
	Remark           string     `gorm:"type:text" json:"remark,omitempty"`
	CreatedBy        string     `gorm:"index;size:64" json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
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

func (m *TemporaryAccount) BeforeCreate(_ *gorm.DB) error      { return ensureID(&m.ID) }
func (m *TemporaryCredential) BeforeCreate(_ *gorm.DB) error   { return ensureID(&m.ID) }
func (m *TemporaryAccountGrant) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
