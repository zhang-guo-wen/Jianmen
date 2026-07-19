package store

import (
	"fmt"

	"jianmen/internal/model"
	"jianmen/internal/storage"
	"gorm.io/gorm"
)

func (s *DBStore) nextHostResourceSeq() (int, error) {
	var maxSeq int
	if err := s.db.Model(&model.HostAccount{}).
		Select("COALESCE(MAX(resource_seq), 0)").
		Scan(&maxSeq).Error; err != nil {
		return 0, fmt.Errorf("host resource sequence floor: %w", err)
	}
	if err := storage.EnsureSequenceNextValue(s.db, storage.SequenceHostAccount, maxSeq+1); err != nil {
		return 0, err
	}
	return storage.NextSequenceValue(s.db, storage.SequenceHostAccount, storage.MaxCompactResourceSeq)
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
