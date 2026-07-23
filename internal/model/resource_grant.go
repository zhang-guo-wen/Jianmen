package model

import (
	"time"

	"gorm.io/gorm"
)

// ResourceGrant 资源授权
// 一个授权规则：主体（用户 或 用户组）→ 客体（资源 或 资源组）→ SSH 连接权限
type ResourceGrant struct {
	ID            string     `gorm:"primaryKey;size:64" json:"id"`
	PrincipalType string     `gorm:"index;index:idx_resource_grants_principal,priority:1;uniqueIndex:idx_rg_logic_deleted,priority:1;size:32;not null" json:"principal_type"` // "user" 或 "user_group"
	PrincipalID   string     `gorm:"index;index:idx_resource_grants_principal,priority:2;uniqueIndex:idx_rg_logic_deleted,priority:2;size:64;not null" json:"principal_id"`   // user_id 或 user_group_id
	ResourceType  string     `gorm:"index;index:idx_resource_grants_resource,priority:1;uniqueIndex:idx_rg_logic_deleted,priority:3;size:64;not null" json:"resource_type"`   // "host", "host_account", "database_account", "resource_group"
	ResourceID    string     `gorm:"index;index:idx_resource_grants_resource,priority:2;uniqueIndex:idx_rg_logic_deleted,priority:4;size:64;not null" json:"resource_id"`     // 资源ID 或 资源组ID
	Effect        string     `gorm:"index;uniqueIndex:idx_rg_logic_deleted,priority:5;size:16;not null;default:allow" json:"effect"`                                          // "allow" 或 "deny"
	ExpiresAt     *time.Time `gorm:"index" json:"expires_at,omitempty"`
	FullAudit
	// 覆盖 FullAudit.DeletedAt，将其纳入唯一索引，使软删后可以重建相同业务键的记录
	DeletedAt *int `gorm:"index;default:1;uniqueIndex:idx_rg_logic_deleted,priority:6" json:"-"`
}

func (m *ResourceGrant) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	// 同步 FullAudit 的 DeletedAt 到外层字段（GORM 以外层字段的 tag 为准映射数据库列）
	if m.DeletedAt == nil {
		m.DeletedAt = m.FullAudit.DeletedAt
	}
	return ensureID(&m.ID)
}
