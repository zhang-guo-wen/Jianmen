package model

import (
	"time"

	"gorm.io/gorm"
)

// UserGroup 用户组
type UserGroup struct {
	ID          string `gorm:"primaryKey;size:64" json:"id"`
	Name        string `gorm:"uniqueIndex:idx_user_groups_name_active,priority:1;size:128;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	FullAudit
}

// UserGroupMember 用户组成员
type UserGroupMember struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	GroupID   string    `gorm:"uniqueIndex:idx_user_group_members_pair;size:64;not null" json:"group_id"`
	UserID    string    `gorm:"uniqueIndex:idx_user_group_members_pair;index;size:64;not null" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	Group     UserGroup `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	User      User      `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

func (m *UserGroup) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&m.ID)
}
func (m *UserGroupMember) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
