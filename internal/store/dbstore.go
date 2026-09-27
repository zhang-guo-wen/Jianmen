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

// ActiveScope returns a GORM scope that only includes live business rows.
func ActiveScope(db *gorm.DB) *gorm.DB {
	return db.Where("active_marker = ?", model.ActiveMarkerValue)
}

// activeHostAccountScope keeps host-account reads fail-closed when either the
// account itself or its parent host has been soft-deleted.
func activeHostAccountScope(db *gorm.DB) *gorm.DB {
	return db.
		Where("host_accounts.active_marker = ?", model.ActiveMarkerValue).
		Where(
			`EXISTS (
				SELECT 1
				FROM hosts
				WHERE hosts.id = host_accounts.host_id
				  AND hosts.active_marker = ?
			)`,
			model.ActiveMarkerValue,
		)
}

// activeDatabaseAccountScope fails closed when either a database account or
// its parent instance has been soft-deleted.
func activeDatabaseAccountScope(db *gorm.DB) *gorm.DB {
	return db.
		Where("database_accounts.active_marker = ?", model.ActiveMarkerValue).
		Where(
			`EXISTS (
				SELECT 1
				FROM database_instances
				WHERE database_instances.id = database_accounts.instance_id
				  AND database_instances.active_marker = ?
			)`,
			model.ActiveMarkerValue,
		)
}

// SoftDeleteRecord soft-deletes a business model and records the actor even
// when the update is expressed as a map.
func (s *DBStore) SoftDeleteRecord(ctx context.Context, dest interface{}, id string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(dest).Where("id = ?", id).Updates(map[string]interface{}{
		"active_marker": nil,
		"updated_at":    now,
		"updated_by":    model.AuditUserIDFromContext(ctx),
	}).Error
}

var softDeleteStatusTables = map[string]struct{}{
	"applications":        {},
	"container_endpoints": {},
	"database_accounts":   {},
	"database_instances":  {},
	"host_accounts":       {},
	"hosts":               {},
	"platform_accounts":   {},
	"roles":               {},
	"temporary_accounts":  {},
	"user_sessions":       {},
	"users":               {},
}

// SoftDelete soft-deletes a business row without relying on model hooks.
// Table-based updates bypass embedded-model hooks, so all audit columns are
// populated explicitly here.
func SoftDelete(ctx context.Context, db *gorm.DB, table string, id string) error {
	return softDeleteWhere(ctx, db, table, "id = ?", id).Error
}

func softDeleteWhere(ctx context.Context, db *gorm.DB, table, condition string, args ...interface{}) *gorm.DB {
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"active_marker": nil,
		"updated_at":    now,
		"updated_by":    model.AuditUserIDFromContext(ctx),
	}
	if _, ok := softDeleteStatusTables[table]; ok {
		updates["status"] = "disabled"
	}
	return db.WithContext(ctx).
		Table(table).
		Where(condition, args...).
		Where("active_marker = ?", model.ActiveMarkerValue).
		Updates(updates)
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
