package model

import (
	"time"

	"gorm.io/gorm"
)

// ConnectionPassword is a short-lived, reusable bastion password bound to one resource.
type ConnectionPassword struct {
	ID                 string     `gorm:"primaryKey;size:64"`
	UserID             string     `gorm:"index:idx_connection_passwords_lookup,priority:1;size:64;not null"`
	ResourceType       string     `gorm:"index:idx_connection_passwords_lookup,priority:2;size:64;not null"`
	ResourceID         string     `gorm:"index:idx_connection_passwords_lookup,priority:3;size:64;not null"`
	TemporaryAccountID string     `gorm:"index;size:64"`
	SecretHash         string     `gorm:"size:255;not null"`
	MySQLNativeHash    string     `gorm:"size:40"`
	ExpiresAt          time.Time  `gorm:"index:idx_connection_passwords_lookup,priority:4;not null"`
	RevokedAt          *time.Time `gorm:"index"`
	FullAudit
}

func (m *ConnectionPassword) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&m.ID)
}
