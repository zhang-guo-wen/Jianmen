package store

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const defaultAuditSessionLeaseDuration = 90 * time.Second

// DBStore provides GORM-backed repository methods for consumer-owned interfaces.
type DBStore struct {
	db                 *gorm.DB
	auditLeaseOwner    string
	auditLeaseDuration time.Duration
	now                func() time.Time

	auditLeaseMu      sync.RWMutex
	activeAuditLeases map[string]struct{}
	ensureGrantMu     sync.Mutex
}

// ActiveScope 返回 GORM Scope，过滤软删除行（仅保留 deleted_at = DeletedMarkerActive 的记录）。
// 仅用于嵌入了 FullAudit 的业务表。
// deleted_at = 1 表示活跃（未删除），NULL 表示已删除。
// 使用整型值 1 作为哨兵，不依赖 time.Time 的序列化行为，所有数据库格式一致。
func ActiveScope(db *gorm.DB) *gorm.DB {
	return db.Where("deleted_at = ?", model.DeletedMarkerActive)
}

// SoftDeleteRecord 对嵌入 FullAudit 的业务模型执行软删除（设 deleted_at = NULL）。
func (s *DBStore) SoftDeleteRecord(ctx context.Context, dest interface{}, id string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(dest).Where("id = ?", id).Updates(map[string]interface{}{
		"deleted_at": nil,
		"updated_at": now,
	}).Error
}

// SoftDelete 对指定的业务表行执行软删除（设 deleted_at = NULL）。
func SoftDelete(ctx context.Context, db *gorm.DB, table string, id string) error {
	return db.WithContext(ctx).
		Table(table).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"deleted_at": nil,
			"updated_at": time.Now().UTC(),
		}).Error
}

func NewDBStore(db *gorm.DB) *DBStore {
	owner := model.NewID()
	if len(owner) > 64 {
		owner = owner[:64]
	}
	return &DBStore{
		db:                 db,
		auditLeaseOwner:    owner,
		auditLeaseDuration: defaultAuditSessionLeaseDuration,
		now:                time.Now,
		activeAuditLeases:  make(map[string]struct{}),
	}
}
