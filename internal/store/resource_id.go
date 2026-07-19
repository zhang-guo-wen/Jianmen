package store

import (
	"fmt"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func (s *DBStore) nextHostResourceSeq(tx *gorm.DB) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("host resource sequence: nil database")
	}
	var maxSeq int
	if err := tx.Model(&model.HostAccount{}).
		Select("COALESCE(MAX(resource_seq), 0)").
		Scan(&maxSeq).Error; err != nil {
		return 0, fmt.Errorf("host resource sequence floor: %w", err)
	}
	if err := storage.EnsureSequenceNextValueInTransaction(tx, storage.SequenceHostAccount, maxSeq+1); err != nil {
		return 0, err
	}
	return storage.NextSequenceValueInTransaction(tx, storage.SequenceHostAccount, storage.MaxCompactResourceSeq)
}

func (s *DBStore) nextDBResourceSeq(db *gorm.DB) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database handle required")
	}
	var maxSeq int
	if err := db.Model(&model.DatabaseAccount{}).
		Select("COALESCE(MAX(resource_seq), 0)").
		Scan(&maxSeq).Error; err != nil {
		return 0, fmt.Errorf("database resource sequence floor: %w", err)
	}
	if err := storage.EnsureSequenceNextValue(db, storage.SequenceDatabaseAccount, maxSeq+1); err != nil {
		return 0, err
	}
	return storage.NextSequenceValue(db, storage.SequenceDatabaseAccount, storage.MaxCompactResourceSeq)
}
