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
		input.Lease.Owner == "" || input.Lease.Token == "" || input.Lease.Duration <= 0 ||
		strings.TrimSpace(input.AdministratorUsername) == "" || input.AdministratorPassword == "" ||
		len(strings.TrimSpace(input.InstanceProof)) != 64 {
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
		ActorID:        strings.TrimSpace(input.ActorID),
		AdminAccountID: input.AdminAccountID, UpstreamUsername: input.Username,
		Password: model.NewEncryptedField(input.Password), Host: input.Host,
		GrantsJSON: input.GrantsJSON, GroupName: strings.TrimSpace(input.Group),
		Remark: strings.TrimSpace(input.Remark), ExpiresAt: input.ExpiresAt,
		Stage: service.ProvisioningStageReserved, CleanupStatus: service.ProvisioningCleanupNone,
		Revision: 1, LeaseOwner: input.Lease.Owner, LeaseToken: input.Lease.Token,
	}
	if key := strings.TrimSpace(input.IdempotencyKey); key != "" {
		record.Kind = "create"
		record.IdempotencyKey = &key
		record.CanonicalRequestHash = strings.TrimSpace(input.RequestHash)
	}
	var window service.DatabaseProvisioningLeaseWindow
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		instance, err := lockProvisioningInstance(tx, input.InstanceID)
		if err != nil {
			return err
		}
		admin, err := lockProvisioningAdministrator(tx, input.InstanceID, input.AdminAccountID)
		if err != nil {
			return err
		}
		if instance.Status != "active" || instance.Protocol != "mysql" || admin.Status != "active" ||
			admin.Password.GetPlaintext() == "" {
			return errors.New("database provisioning administrator changed")
		}
		if admin.Username != input.AdministratorUsername || admin.Password.GetPlaintext() != input.AdministratorPassword ||
			databaseProvisioningInstanceProof(instance) != strings.TrimSpace(input.InstanceProof) {
			return errors.New("database provisioning administrator changed")
		}
		now, err := databaseNow(ctx, tx)
		if err != nil || (admin.ExpiresAt != nil && !now.Before(*admin.ExpiresAt)) {
			return errors.New("database provisioning administrator unavailable")
		}
		var accountCount int64
		if err := tx.Model(&model.DatabaseAccount{}).Scopes(ActiveScope).
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
		updated := tx.Model(&model.DatabaseProvisioningOperation{}).Scopes(ActiveScope).
			Where("id = ?", record.ID).
			Update("lease_expires_at", expiresAt)
		if updated.Error != nil || updated.RowsAffected != 1 {
			return errors.New("set database provisioning initial lease")
		}
		if err := tx.First(&record, "id = ?", record.ID).Error; err != nil {
			return err
		}
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

func (s *DBStore) CreateOrGetDatabaseProvisioningOperation(
	ctx context.Context,
	input service.DatabaseProvisioningOperationInput,
) (service.DatabaseProvisioningOperation, service.DatabaseProvisioningLeaseWindow, bool, error) {
	if strings.TrimSpace(input.ActorID) == "" || strings.TrimSpace(input.IdempotencyKey) == "" ||
		len(strings.TrimSpace(input.RequestHash)) != 64 {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			service.ErrInvalidDatabaseProvisioningRequest
	}
	if operation, found, err := s.DatabaseProvisioningOperationByIdempotency(
		ctx, input.ActorID, input.IdempotencyKey,
	); err != nil || found {
		if err != nil {
			return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false, err
		}
		if operation.RequestHash != input.RequestHash {
			return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
				service.ErrDatabaseProvisioningIdempotencyConflict
		}
		return operation, service.DatabaseProvisioningLeaseWindow{}, false, nil
	}
	operation, window, err := s.CreateDatabaseProvisioningOperation(ctx, input)
	if err == nil {
		return operation, window, true, nil
	}
	// A concurrent insert can win the unique (actor, kind, key) index. Re-read only
	// that identity and never create a second upstream operation.
	operation, found, lookupErr := s.DatabaseProvisioningOperationByIdempotency(
		ctx, input.ActorID, input.IdempotencyKey,
	)
	if lookupErr == nil && found {
		if operation.RequestHash != input.RequestHash {
			return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
				service.ErrDatabaseProvisioningIdempotencyConflict
		}
		return operation, service.DatabaseProvisioningLeaseWindow{}, false, nil
	}
	return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false, err
}

func (s *DBStore) DatabaseProvisioningOperationByIdempotency(
	ctx context.Context,
	actorID, key string,
) (service.DatabaseProvisioningOperation, bool, error) {
	if ctx == nil {
		return service.DatabaseProvisioningOperation{}, false, errors.New("load database provisioning idempotency operation: nil context")
	}
	var record model.DatabaseProvisioningOperation
	err := s.db.WithContext(ctx).Where(
		"actor_id = ? AND kind = ? AND idempotency_key = ?",
		strings.TrimSpace(actorID), "create", strings.TrimSpace(key),
	).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.DatabaseProvisioningOperation{}, false, nil
	}
	if err != nil {
		return service.DatabaseProvisioningOperation{}, false, errors.New("load database provisioning idempotency operation")
	}
	return databaseProvisioningOperationFromModel(record), true, nil
}

func (s *DBStore) ProvisionedDatabaseAccountByOperation(
	ctx context.Context,
	operationID string,
) (service.ProvisionedDatabaseAccount, bool, error) {
	if ctx == nil {
		return service.ProvisionedDatabaseAccount{}, false, errors.New("load provisioned database account: nil context")
	}
	var account model.DatabaseAccount
	err := s.db.WithContext(ctx).Where("provisioning_operation_id = ?", strings.TrimSpace(operationID)).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.ProvisionedDatabaseAccount{}, false, nil
	}
	if err != nil {
		return service.ProvisionedDatabaseAccount{}, false, errors.New("load provisioned database account")
	}
	return provisionedDatabaseAccountFromModel(s, account), true, nil
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
			`(stage = ? OR
			 (stage = ? AND created_at <= ?) OR
			 (stage IN (?, ?, ?, ?, ?, ?, ?, ?, ?) AND
			  COALESCE(last_attempt_at, updated_at) <= ?))`,
			service.ProvisioningStageActivationPending,
			service.ProvisioningStageReserved,
			staleBefore,
			service.ProvisioningStageCreateStarted,
			service.ProvisioningStageCreateUncertain,
			service.ProvisioningStageUpstreamCreated,
			service.ProvisioningStageGrantStarted,
			service.ProvisioningStageCleanupRequired,
			service.ProvisioningStageCleanupInProgress,
			service.ProvisioningStageDeprovisionRequested,
			service.ProvisioningStageDropStarted,
			service.ProvisioningStageDropUncertain,
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
