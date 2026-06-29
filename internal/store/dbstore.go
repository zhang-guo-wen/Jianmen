package store

import "gorm.io/gorm"

// DBStore implements Store backed by a GORM database.
// Database proxies still use config-based management (not yet migrated to DB).
type DBStore struct {
	db *gorm.DB
}

func NewDBStore(db *gorm.DB) *DBStore {
	return &DBStore{db: db}
}
