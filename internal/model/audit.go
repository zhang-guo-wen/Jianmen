package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// ActiveMarkerValue 表示业务记录当前可用。
// active_marker 为 1 时记录活跃，为 NULL 时记录已停用/移除。使用 NULL
// 作为非活跃值，可让 (business_key, active_marker) 复合唯一索引在
// MySQL、SQLite 和 PostgreSQL 中同时允许保留多条历史记录。
const ActiveMarkerValue = 1

// contextKey 用于在 context 中存储/提取审计相关值。
type contextKey string

const auditUserIDKey contextKey = "admin_user_id"

// WithAuditUserID 返回携带审计操作者 ID 的 context。
func WithAuditUserID(ctx context.Context, userID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, auditUserIDKey, userID)
}

// AuditUserIDFromContext 从 context 中提取审计操作者 ID。
func AuditUserIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(auditUserIDKey).(string); ok {
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
	if a.CreatedBy == "" {
		a.CreatedBy = AuditUserIDFromContext(tx.Statement.Context)
	}
	return nil
}

// FullAudit 业务表嵌入：完整的五个审计字段。
type FullAudit struct {
	CreatedBy    string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
	UpdatedBy    string    `gorm:"index;size:64;not null;default:''" json:"updated_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ActiveMarker *int      `gorm:"column:active_marker;index;default:1" json:"-"`
}

// BeforeCreate 自动从 context 获取当前用户 ID 填充 CreatedBy 和 UpdatedBy，
// 并将 ActiveMarker 初始化为 ActiveMarkerValue。
func (a *FullAudit) BeforeCreate(tx *gorm.DB) error {
	if a.ActiveMarker == nil {
		v := ActiveMarkerValue
		a.ActiveMarker = &v
	}
	userID := AuditUserIDFromContext(tx.Statement.Context)
	if a.CreatedBy == "" {
		a.CreatedBy = userID
	}
	if a.UpdatedBy == "" {
		a.UpdatedBy = userID
	}
	return nil
}

// BeforeUpdate 自动从 context 获取当前用户 ID 填充 UpdatedBy。
func (a *FullAudit) BeforeUpdate(tx *gorm.DB) error {
	if userID := AuditUserIDFromContext(tx.Statement.Context); userID != "" {
		a.UpdatedBy = userID
	}
	return nil
}
