package model

import (
	"gorm.io/gorm"
)

// SystemSettingRevision is an immutable, non-secret configuration snapshot.
type SystemSettingRevision struct {
	ID                string    `gorm:"primaryKey;size:64"`
	Revision          int64     `gorm:"uniqueIndex:idx_ss_rev_revision_deleted,priority:1;not null"`
	SnapshotJSON      string    `gorm:"type:text;not null"`
	ChangedFieldsJSON string    `gorm:"type:text;not null"`
	FullAudit
}

func (m *SystemSettingRevision) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&m.ID)
}
