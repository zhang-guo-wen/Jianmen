package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func (s *DBStore) UpdateDatabaseInstance(ctx context.Context, id string, input DatabaseInstanceInput) (DatabaseInstanceView, error) {
	id = strings.TrimSpace(id)
	var (
		inst         model.DatabaseInstance
		accountCount int
	)
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := lockProvisioningInstance(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
			}
			return err
		}
		if strings.TrimSpace(input.TLSMode) == "" {
			input.TLSMode = locked.TLSMode
			input.TLSServerName = locked.TLSServerName
		}
		if strings.TrimSpace(input.Status) == "" {
			input.Status = locked.Status
		}
		updated, err := normalizeDatabaseInstanceInput(input, locked.TLSCAPEM)
		if err != nil {
			return err
		}
		if hasCriticalDatabaseInstanceChange(locked, updated) {
			if err := protectReferencedInstance(tx, locked.ID); err != nil {
				return err
			}
		}
		inst = updated
		inst.ID = locked.ID
		if inst.Name == "" {
			inst.Name = inst.Address
		}
		if err := tx.Save(&inst).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, inst.GroupName); err != nil {
			return err
		}
		if err := s.syncResourceTx(tx, model.ResourceTypeDatabaseInstance, inst.ID, databaseInstanceResourceName(inst), ""); err != nil {
			return err
		}
		count, err := s.databaseAccountCount(tx, inst.ID)
		if err != nil {
			return err
		}
		accountCount = count
		return nil
	}); err != nil {
		return DatabaseInstanceView{}, err
	}
	return s.databaseInstanceView(inst, accountCount), nil
}

func (s *DBStore) DeleteDatabaseInstance(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		inst, err := lockProvisioningInstance(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
			}
			return err
		}
		if err := protectReferencedInstance(tx, inst.ID); err != nil {
			return err
		}
		var accounts []model.DatabaseAccount
		if err := tx.Where("instance_id = ?", id).Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			if err := s.deleteResourceTx(tx, model.ResourceTypeDatabaseAccount, account.ID); err != nil {
				return err
			}
		}
		if err := tx.Where("instance_id = ?", id).Delete(&model.DatabaseAccount{}).Error; err != nil {
			return err
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeDatabaseInstance, inst.ID); err != nil {
			return err
		}
		return tx.Delete(&inst).Error
	})
}
