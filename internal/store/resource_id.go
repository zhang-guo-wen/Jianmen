package store

import (
	"fmt"

	"jianmen/internal/model"
	"jianmen/internal/storage"
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

func (s *DBStore) nextDBResourceSeq() (int, error) {
	var maxSeq int
	if err := s.db.Model(&model.DatabaseAccount{}).
		Select("COALESCE(MAX(resource_seq), 0)").
		Scan(&maxSeq).Error; err != nil {
		return 0, fmt.Errorf("database resource sequence floor: %w", err)
	}
	if err := storage.EnsureSequenceNextValue(s.db, storage.SequenceDatabaseAccount, maxSeq+1); err != nil {
		return 0, err
	}
	return storage.NextSequenceValue(s.db, storage.SequenceDatabaseAccount, storage.MaxCompactResourceSeq)
}
