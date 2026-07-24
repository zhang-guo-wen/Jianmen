package storage

import (
	"time"

	"gorm.io/gorm"
)

// permissionLogicalUniquenessSchema freezes the schema owned by migration
// 202607180007. Audit fields and deleted_at uniqueness are installed by later
// migrations.
type permissionLogicalUniquenessSchema struct {
	ID           string `gorm:"primaryKey;size:64"`
	Name         string `gorm:"index;size:128"`
	Action       string `gorm:"index;index:idx_permissions_action_resource,priority:1;uniqueIndex:idx_permissions_logic,priority:1;size:128"`
	ResourceType string `gorm:"index;index:idx_permissions_action_resource,priority:2;uniqueIndex:idx_permissions_logic,priority:2;size:64"`
	ResourceID   string `gorm:"index;index:idx_permissions_action_resource,priority:3;uniqueIndex:idx_permissions_logic,priority:3;size:64"`
	Effect       string `gorm:"index:idx_permissions_action_resource,priority:4;uniqueIndex:idx_permissions_logic,priority:4;size:16;not null;default:allow"`
	Description  string `gorm:"type:text"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (permissionLogicalUniquenessSchema) TableName() string {
	return "permissions"
}

func migratePermissionLogicalUniqueness(tx *gorm.DB) error {
	return tx.AutoMigrate(&permissionLogicalUniquenessSchema{})
}
