package store

import "gorm.io/gorm"

// DBStore implements Store backed by a GORM database.
type DBStore struct {
	db *gorm.DB
}

func NewDBStore(db *gorm.DB) *DBStore {
	return &DBStore{db: db}
}
