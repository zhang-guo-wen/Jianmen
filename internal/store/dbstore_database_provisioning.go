package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

const provisioningStageActiveManaged = "active_managed"

func (s *DBStore) CreateDatabaseProvisioningOperation(
	ctx context.Context,
	input service.DatabaseProvisioningOperationInput,
) (service.DatabaseProvisioningOperation, service.DatabaseProvisioningLeaseWindow, error) {
	if ctx == nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{},
			errors.New("create database provisioning operation: nil context")
	}
	input.ID = strings.TrimSpace(input.ID)
	input.InstanceID = strings.TrimSpace(input.InstanceID)
	input.AdminAccountID = strings.TrimSpace(input.AdminAccountID)
	input.Username = strings.TrimSpace(input.Username)
	input.Host = strings.TrimSpace(input.Host)
	input.Lease.Owner = strings.TrimSpace(input.Lease.Owner)
	input.Lease.Token = strings.TrimSpace(input.Lease.Token)
	if !validProvisioningIdentity(input.ID, input.Username) ||
		input.InstanceID == "" || input.AdminAccountID == "" ||
		input.Password == "" || input.Host == "" ||
		strings.TrimSpace(input.GrantsJSON) == "" ||
		input.Lease.Owner == "" || input.Lease.Token == "" || input.Lease.Duration <= 0 {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{},
			errors.New("invalid database provisioning operation")
	}
	clock, err := newDatabaseProvisioningStatementClock(s.db)
	if err != nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{},
			errors.New("create database provisioning operation: database clock")
	}
	expiresAt, err := clock.leaseExpiryExpression(input.Lease.Duration)
	if err != nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{},
			errors.New("create database provisioning operation: invalid lease duration")
	}
	record := model.DatabaseProvisioningOperation{
		ID: input.ID, InstanceID: input.InstanceID,
		AdminAccountID: input.AdminAccountID, UpstreamUsername: input.Username,
		Password: model.NewEncryptedField(input.Password), Host: input.Host,
		GrantsJSON: input.GrantsJSON, GroupName: strings.TrimSpace(input.Group),
		Remark: strings.TrimSpace(input.Remark), ExpiresAt: input.ExpiresAt,
		Stage: service.ProvisioningStageReserved, CleanupStatus: service.ProvisioningCleanupNone,
		Revision: 1, LeaseOwner: input.Lease.Owner, LeaseToken: input.Lease.Token,
	}
	var window service.DatabaseProvisioningLeaseWindow
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var instance model.DatabaseInstance
		if err := tx.First(&instance, "id = ?", input.InstanceID).Error; err != nil {
			return err
		}
		var admin model.DatabaseAccount
		if err := tx.First(
			&admin, "id = ? AND instance_id = ?", input.AdminAccountID, input.InstanceID,
		).Error; err != nil {
			return err
		}
		var accountCount int64
		if err := tx.Model(&model.DatabaseAccount{}).
			Where("instance_id = ? AND username = ?", input.InstanceID, input.Username).
			Count(&accountCount).Error; err != nil {
			return err
		}
		if accountCount != 0 {
			return errors.New("database provisioning username is unavailable")
		}
		if err := tx.Omit("LeaseExpiresAt").Create(&record).Error; err != nil {
			return err
		}
		updated := tx.Model(&model.DatabaseProvisioningOperation{}).
			Where("id = ?", record.ID).
			Update("lease_expires_at", expiresAt)
		if updated.Error != nil || updated.RowsAffected != 1 {
			return errors.New("set database provisioning initial lease")
		}
		if err := tx.First(&record, "id = ?", record.ID).Error; err != nil {
			return err
		}
		var err error
		window, err = clock.leaseWindow(ctx, tx, record.ID, input.Lease.Duration)
		return err
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{},
			errors.New("database provisioning dependency is unavailable")
	}
	if err != nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{},
			fmt.Errorf("create database provisioning operation: %w", err)
	}
	return databaseProvisioningOperationFromModel(record), window, nil
}

func (s *DBStore) DatabaseProvisioningOperation(
	ctx context.Context,
	id string,
) (service.DatabaseProvisioningOperation, error) {
	if ctx == nil {
		return service.DatabaseProvisioningOperation{},
			errors.New("load database provisioning operation: nil context")
	}
	var record model.DatabaseProvisioningOperation
	err := s.db.WithContext(ctx).First(&record, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.DatabaseProvisioningOperation{},
			fmt.Errorf("%w: provisioning operation", ErrDatabaseProvisioningOperationNotFound)
	}
	if err != nil {
		return service.DatabaseProvisioningOperation{},
			errors.New("load database provisioning operation")
	}
	return databaseProvisioningOperationFromModel(record), nil
}

func (s *DBStore) ListExecutableDatabaseProvisioningOperations(
	ctx context.Context,
	staleAfter time.Duration,
	limit int,
) ([]service.DatabaseProvisioningOperation, error) {
	if ctx == nil {
		return nil, errors.New("list database provisioning operations: nil context")
	}
	if staleAfter <= 0 || limit <= 0 || limit > 500 {
		return nil, errors.New("invalid database provisioning operation query")
	}
	now, err := databaseNow(ctx, s.db)
	if err != nil {
		return nil, errors.New("list database provisioning operations: database clock")
	}
	staleBefore := now.Add(-staleAfter)
	var records []model.DatabaseProvisioningOperation
	err = s.db.WithContext(ctx).
		Where("(lease_expires_at IS NULL OR lease_expires_at <= ?)", now).
		Where(
			`(stage IN (?, ?) OR
			 (stage = ? AND created_at <= ?) OR
			 (stage IN (?, ?, ?, ?, ?, ?) AND
			  COALESCE(last_attempt_at, updated_at) <= ?))`,
			service.ProvisioningStageActivationPending,
			service.ProvisioningStageNotCreated,
			service.ProvisioningStageReserved,
			staleBefore,
			service.ProvisioningStageCreateStarted,
			service.ProvisioningStageCreateUncertain,
			service.ProvisioningStageUpstreamCreated,
			service.ProvisioningStageGrantStarted,
			service.ProvisioningStageCleanupRequired,
			service.ProvisioningStageCleanupInProgress,
			staleBefore,
		).
		Order("updated_at ASC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, errors.New("list database provisioning operations")
	}
	operations := make([]service.DatabaseProvisioningOperation, 0, len(records))
	for _, record := range records {
		operations = append(operations, databaseProvisioningOperationFromModel(record))
	}
	return operations, nil
}

func timePointer(value time.Time) *time.Time { return &value }
