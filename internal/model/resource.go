package model

import (
	"gorm.io/gorm"
)

type Resource struct {
	ID         string    `gorm:"primaryKey;size:64" json:"id"`
	Type       string `gorm:"index;uniqueIndex:idx_resources_type_resource_id_deleted,priority:1;size:64;not null" json:"type"`
	ResourceID string `gorm:"uniqueIndex:idx_resources_type_resource_id_deleted,priority:2;size:64" json:"resource_id"`
	Name       string `gorm:"size:255" json:"name,omitempty"`
	ParentID   string `gorm:"index;size:64" json:"parent_id,omitempty"`
	FullAudit
}

type ResourceGroup struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	Name        string `gorm:"uniqueIndex:idx_resource_groups_name_type_deleted,priority:1;size:128;not null" json:"name"`
	GroupType   string `gorm:"uniqueIndex:idx_resource_groups_name_type_deleted,priority:2;index;size:32;not null;default:resource" json:"group_type"` // "resource" 或 "account"
	Description string `gorm:"type:text" json:"description,omitempty"`
	FullAudit
}

const (
	ResourceGroupTypeResource = "resource" // 资源分组（主机、数据库实例）
	ResourceGroupTypeAccount  = "account"  // 账号分组（主机账号、数据库账号）
)

func (m *Resource) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&m.ID)
}
func (m *ResourceGroup) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&m.ID)
}
