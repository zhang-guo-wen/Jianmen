package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func (s *DBStore) BeginDatabaseDeprovision(
	ctx context.Context,
	accountID string,
	lease service.DatabaseProvisioningLease,
) (service.DatabaseProvisioningOperation, bool, error) {
	if ctx == nil {
		return service.DatabaseProvisioningOperation{}, false, errors.New("begin database deprovision: nil context")
	}
	accountID, lease.Owner, lease.Token = strings.TrimSpace(accountID), strings.TrimSpace(lease.Owner), strings.TrimSpace(lease.Token)
	if accountID == "" || lease.Owner == "" || lease.Token == "" || lease.Duration <= 0 {
		return service.DatabaseProvisioningOperation{}, false, errors.New("begin database deprovision: invalid input")
	}
	clock, err := newDatabaseProvisioningStatementClock(s.db)
	if err != nil {
		return service.DatabaseProvisioningOperation{}, false, errors.New("begin database deprovision: database clock")
	}
	expiresAt, err := clock.leaseExpiryExpression(lease.Duration)
	if err != nil {
		return service.DatabaseProvisioningOperation{}, false, errors.New("begin database deprovision: invalid lease")
	}
	var operation model.DatabaseProvisioningOperation
	managed, claimed := false, false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account model.DatabaseAccount
		if err := tx.First(&account, "id = ?", accountID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if !account.Managed || account.ProvisioningOperationID == nil || strings.TrimSpace(*account.ProvisioningOperationID) == "" {
			return nil
		}
		managed = true
		var current model.DatabaseProvisioningOperation
		if err := tx.First(&current, "id = ? AND instance_id = ?", *account.ProvisioningOperationID, account.InstanceID).Error; err != nil {
			return err
		}
		if !validProvisioningIdentity(current.ID, current.UpstreamUsername) || current.Stage != service.ProvisioningStageActiveManaged || current.CleanupStatus != service.ProvisioningCleanupNone {
			return errors.New("begin database deprovision: invalid managed lifecycle")
		}
		if !managedAccountMatchesProvisioningOperation(account, current) {
			return errors.New("begin database deprovision: managed account identity mismatch")
		}
		result := tx.Model(&model.DatabaseProvisioningOperation{}).Scopes(ActiveScope).
			Where(provisioningFenceCondition(), provisioningFenceArguments(databaseProvisioningOperationFromModel(current).Fence())...).
			Where(clock.expiredOrUnsetLeaseCondition()).
			Updates(map[string]any{
				"stage": service.ProvisioningStageDeprovisionRequested, "cleanup_status": service.ProvisioningCleanupNone,
				"lease_owner": lease.Owner, "lease_token": lease.Token, "lease_expires_at": expiresAt,
				"revision": gorm.Expr("revision + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		claimed = true
		return tx.First(&operation, "id = ?", current.ID).Error
	})
	if err != nil {
		return service.DatabaseProvisioningOperation{}, managed, errors.New("begin database deprovision")
	}
	if !claimed {
		return service.DatabaseProvisioningOperation{}, managed, nil
	}
	return databaseProvisioningOperationFromModel(operation), true, nil
}

func (s *DBStore) CompleteDatabaseDeprovision(
	ctx context.Context,
	expected service.DatabaseProvisioningFence,
	accountID string,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("complete database deprovision: nil context")
	}
	if expected.Stage != service.ProvisioningStageDropStarted || expected.CleanupStatus != service.ProvisioningCleanupInProgress {
		return false, nil
	}
	clock, err := newDatabaseProvisioningStatementClock(s.db)
	if err != nil {
		return false, errors.New("complete database deprovision: database clock")
	}
	completed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation model.DatabaseProvisioningOperation
		if err := tx.Where(provisioningFenceCondition(), provisioningFenceArguments(expected)...).
			Where(clock.validLeaseCondition()).First(&operation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if !validProvisioningIdentity(operation.ID, operation.UpstreamUsername) {
			return errors.New("complete database deprovision: invalid operation identity")
		}
		var account model.DatabaseAccount
		if err := tx.First(&account, "id = ? AND instance_id = ?", strings.TrimSpace(accountID), operation.InstanceID).Error; err != nil {
			return err
		}
		if !managedAccountMatchesProvisioningOperation(account, operation) {
			return errors.New("complete database deprovision: account lifecycle mismatch")
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeDatabaseAccount, account.ID); err != nil {
			return err
		}
		if err := SoftDelete(ctx, tx, "database_accounts", account.ID); err != nil {
			return err
		}
		result := tx.Where(provisioningFenceCondition(), provisioningFenceArguments(expected)...).
			Where(clock.validLeaseCondition()).Where("deleted_at = ?", model.SentinelDeletedAt).Updates(map[string]interface{}{"deleted_at": time.Now().UTC(), "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errDatabaseProvisioningFenceLost
		}
		completed = true
		return nil
	})
	if errors.Is(err, errDatabaseProvisioningFenceLost) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("complete database deprovision")
	}
	return completed, nil
}

func managedAccountMatchesProvisioningOperation(account model.DatabaseAccount, operation model.DatabaseProvisioningOperation) bool {
	return account.Managed && account.ProvisioningOperationID != nil &&
		*account.ProvisioningOperationID == operation.ID &&
		account.Username == operation.UpstreamUsername &&
		account.UpstreamHost == operation.Host &&
		validProvisioningIdentity(operation.ID, operation.UpstreamUsername)
}
