package model

import (
	"time"

	"gorm.io/gorm"
)

// ConnectionPassword is a short-lived, one-time bastion password bound to one resource.
type ConnectionPassword struct {
	ID              string     `gorm:"primaryKey;size:64"`
	UserID          string     `gorm:"index:idx_connection_passwords_lookup,priority:1;size:64;not null"`
	ResourceType    string     `gorm:"index:idx_connection_passwords_lookup,priority:2;size:64;not null"`
	ResourceID      string     `gorm:"index:idx_connection_passwords_lookup,priority:3;size:64;not null"`
	SecretHash      string     `gorm:"size:255;not null"`
	MySQLNativeHash string     `gorm:"size:40"`
	ExpiresAt       time.Time  `gorm:"index:idx_connection_passwords_lookup,priority:4;not null"`
	UsedAt          *time.Time `gorm:"index:idx_connection_passwords_lookup,priority:5"`
	CreatedAt       time.Time
}

func (m *ConnectionPassword) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
