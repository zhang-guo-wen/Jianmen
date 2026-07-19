package model

import (
	"time"

	"gorm.io/gorm"
)

// SystemSettingRevision is an immutable, non-secret configuration snapshot.
type SystemSettingRevision struct {
	ID                string    `gorm:"primaryKey;size:64"`
	Revision          int64     `gorm:"uniqueIndex;not null"`
	SnapshotJSON      string    `gorm:"type:text;not null"`
	ChangedFieldsJSON string    `gorm:"type:text;not null"`
	UpdatedByID       string    `gorm:"size:64"`
	UpdatedByUsername string    `gorm:"size:128"`
	CreatedAt         time.Time `gorm:"index;not null"`
}

func (m *SystemSettingRevision) BeforeCreate(_ *gorm.DB) error {
	return ensureID(&m.ID)
}
