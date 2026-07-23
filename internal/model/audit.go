package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// SentinelDeletedAt 未删除（活跃）行的标记时间。
// 使用非 NULL 值以便与业务键组合成有效的复合唯一索引（MySQL 唯一索引中 NULL 值不参与等值比较）。
var SentinelDeletedAt = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)

// SentinelDeletedAtPtr 返回 SentinelDeletedAt 的指针。
func SentinelDeletedAtPtr() *time.Time {
	t := SentinelDeletedAt
	return &t
}

// contextKey 用于在 context 中存储/提取审计相关值。
type contextKey string

// CtxKeyUserID 与 admin server 的 ctxKeyUserID 保持一致的值。
const CtxKeyUserID contextKey = "admin_user_id"

// userIDFromContext 从 context 中提取当前用户 ID。
func userIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(CtxKeyUserID).(string); ok {
		return id
	}
	return ""
}

// CreationAudit 审计日志嵌入：仅创建信息。
// 适用于只追加不修改不删除的审计日志表。
type CreationAudit struct {
	CreatedBy string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// BeforeCreate 自动从 context 获取当前用户 ID 填充 CreatedBy。
func (a *CreationAudit) BeforeCreate(tx *gorm.DB) error {
	a.CreatedBy = userIDFromContext(tx.Statement.Context)
	return nil
}

// FullAudit 业务表嵌入：完整的五个审计字段。
type FullAudit struct {
	CreatedBy string     `gorm:"index;size:64;not null;default:''" json:"created_by"`
	UpdatedBy string     `gorm:"index;size:64;not null;default:''" json:"updated_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index;not null;default:'0001-01-01 00:00:00'" json:"-"`
}

// BeforeCreate 自动从 context 获取当前用户 ID 填充 CreatedBy 和 UpdatedBy，
// 并将 DeletedAt 初始化为 SentinelDeletedAt。
func (a *FullAudit) BeforeCreate(tx *gorm.DB) error {
	if a.DeletedAt == nil {
		a.DeletedAt = SentinelDeletedAtPtr()
	}
	userID := userIDFromContext(tx.Statement.Context)
	a.CreatedBy = userID
	a.UpdatedBy = userID
	return nil
}

// BeforeUpdate 自动从 context 获取当前用户 ID 填充 UpdatedBy。
func (a *FullAudit) BeforeUpdate(tx *gorm.DB) error {
	a.UpdatedBy = userIDFromContext(tx.Statement.Context)
	return nil
}
