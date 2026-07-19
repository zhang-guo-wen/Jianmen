package store

import (
	"sync"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const defaultAuditSessionLeaseDuration = 90 * time.Second

// DBStore implements Store backed by a GORM database.
type DBStore struct {
	db                 *gorm.DB
	auditLeaseOwner    string
	auditLeaseDuration time.Duration
	now                func() time.Time

	auditLeaseMu      sync.RWMutex
	activeAuditLeases map[string]struct{}
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
