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
}

// ActiveScope 返回 GORM Scope，过滤软删除行（仅保留 deleted_at = sentinel 的记录）。
// 仅用于嵌入了 FullAudit 的业务表。
func ActiveScope(db *gorm.DB) *gorm.DB {
	return db.Where("deleted_at = ?", model.SentinelDeletedAt)
}

// SoftDeleteRecord 对嵌入 FullAudit 的业务模型执行软删除。
func (s *DBStore) SoftDeleteRecord(ctx context.Context, dest interface{}, id string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(dest).Where("id = ?", id).Updates(map[string]interface{}{
		"deleted_at": now,
		"updated_at": now,
	}).Error
}

// SoftDelete 对指定的业务表行执行软删除（设 deleted_at = now）。
func SoftDelete(ctx context.Context, db *gorm.DB, table string, id string) error {
	return db.WithContext(ctx).
		Table(table).
		Where("id = ?", id).
		Where("deleted_at = ?", model.SentinelDeletedAt).
		Updates(map[string]interface{}{
			"deleted_at": time.Now().UTC(),
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
