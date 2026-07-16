package model

import (
	"time"

	"gorm.io/gorm"
)

// AIAccessToken stores hashed access and refresh credentials issued to an AI client.
type AIAccessToken struct {
	ID               string     `gorm:"primaryKey;size:64" json:"id"`
	UserID           string     `gorm:"index;size:64;not null" json:"user_id"`
	Name             string     `gorm:"size:128;not null" json:"name"`
	AccessTokenHash  string     `gorm:"uniqueIndex;size:64;not null" json:"-"`
	RefreshTokenHash string     `gorm:"uniqueIndex;size:64;not null" json:"-"`
	AccessExpiresAt  time.Time  `gorm:"index;not null" json:"access_expires_at"`
	RefreshExpiresAt time.Time  `gorm:"index;not null" json:"refresh_expires_at"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
	RevokedAt        *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	User             User       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

func (m *AIAccessToken) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
