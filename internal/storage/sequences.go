package storage

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
)

const (
	SequenceHostAccount     = "host_account"
	SequenceDatabaseAccount = "database_account"
	SequenceUserSession     = "user_session"

	MaxCompactResourceSeq = 62*62*62*62 - 1
	MaxCompactSessionSeq  = 62*62*62*62*62 - 1
)

func UserSessionSequenceName(userID string) string {
	_ = userID
	return SequenceUserSession
}

func NextSequenceValue(db *gorm.DB, name string, maxValue int) (int, error) {
	if db == nil {
		return 0, errors.New("sequence: nil database")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("sequence: empty name")
	}

	var value int
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		err := db.Transaction(func(tx *gorm.DB) error {
			var allocateErr error
			value, allocateErr = NextSequenceValueInTransaction(tx, name, maxValue)
			return allocateErr
		})
		if err == nil {
			return value, nil
		}
		lastErr = err
	}
	return 0, fmt.Errorf("next sequence %q: %w", name, lastErr)
}

// NextSequenceValueInTransaction allocates a sequence value without opening a
// nested transaction. The caller owns the transaction boundary, so allocation
// can be committed atomically with the record that consumes the value.
func NextSequenceValueInTransaction(tx *gorm.DB, name string, maxValue int) (int, error) {
	if tx == nil {
		return 0, errors.New("sequence: nil database")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("sequence: empty name")
	}
	var seq model.ResourceSequence
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("name = ?", name).Find(&seq)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		seq = model.ResourceSequence{Name: name, NextValue: 1}
		if err := tx.Create(&seq).Error; err != nil {
			return 0, err
		}
	}
	if seq.NextValue < 1 {
		seq.NextValue = 1
	}
	if maxValue > 0 && seq.NextValue > maxValue {
		return 0, fmt.Errorf("sequence %q exhausted at %d", name, maxValue)
	}
	value := seq.NextValue
	if err := tx.Model(&model.ResourceSequence{}).Where("name = ?", name).Update("next_value", seq.NextValue+1).Error; err != nil {
		return 0, err
	}
	return value, nil
}

func EnsureSequenceNextValue(db *gorm.DB, name string, nextValue int) error {
	if db == nil {
		return errors.New("sequence: nil database")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("sequence: empty name")
	}
	if nextValue < 1 {
		nextValue = 1
	}

	return db.Transaction(func(tx *gorm.DB) error {
		return EnsureSequenceNextValueInTransaction(tx, name, nextValue)
	})
}

func EnsureSequenceNextValueInTransaction(tx *gorm.DB, name string, nextValue int) error {
	if tx == nil {
		return errors.New("sequence: nil database")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("sequence: empty name")
	}
	if nextValue < 1 {
		nextValue = 1
	}
	var seq model.ResourceSequence
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("name = ?", name).Find(&seq)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tx.Create(&model.ResourceSequence{Name: name, NextValue: nextValue}).Error
	}
	if seq.NextValue >= nextValue {
		return nil
	}
	return tx.Model(&model.ResourceSequence{}).Where("name = ?", name).Update("next_value", nextValue).Error
}
