package model

import (
	"time"

	"gorm.io/gorm"
)

// AIAccessToken stores hashed access and refresh credentials issued to an AI client.
type AIAccessToken struct {
	ID                 string           `gorm:"primaryKey;size:64" json:"id"`
	UserID             string           `gorm:"index;size:64;not null" json:"user_id"`
	TemporaryAccountID string           `gorm:"index;size:64" json:"temporary_account_id,omitempty"`
	Name               string           `gorm:"size:128;not null" json:"name"`
	AccessTokenHash    string           `gorm:"uniqueIndex:idx_ai_tokens_access_hash_deleted,priority:1;size:64;not null" json:"-"`
	RefreshTokenHash   string           `gorm:"uniqueIndex:idx_ai_tokens_refresh_hash_deleted,priority:1;size:64;not null" json:"-"`
	AccessExpiresAt    time.Time        `gorm:"index;not null" json:"access_expires_at"`
	RefreshExpiresAt   time.Time        `gorm:"index;not null" json:"refresh_expires_at"`
	LastUsedAt         *time.Time       `json:"last_used_at,omitempty"`
	RevokedAt          *time.Time       `gorm:"index" json:"revoked_at,omitempty"`
	FullAudit
	User               User             `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	TemporaryAccount   TemporaryAccount `gorm:"foreignKey:TemporaryAccountID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"-"`
}

func (m *AIAccessToken) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&m.ID)
}
