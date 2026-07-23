package store

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"
	"jianmen/internal/util"
)

var errDatabaseProvisioningFenceLost = errors.New("database provisioning fence was lost")

func (s *DBStore) ActivateDatabaseProvisioningOperation(
	ctx context.Context,
	expected service.DatabaseProvisioningFence,
) (service.ProvisionedDatabaseAccount, bool, error) {
	if ctx == nil {
		return service.ProvisionedDatabaseAccount{}, false,
			errors.New("activate database provisioning operation: nil context")
	}
	if expected.Stage != service.ProvisioningStageActivationPending ||
		expected.CleanupStatus != service.ProvisioningCleanupNone {
		return service.ProvisionedDatabaseAccount{}, false, nil
	}
	clock, err := newDatabaseProvisioningStatementClock(s.db)
	if err != nil {
		return service.ProvisionedDatabaseAccount{}, false,
			errors.New("activate database provisioning operation: database clock")
	}
	var account model.DatabaseAccount
	activated := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation model.DatabaseProvisioningOperation
		result := tx.Where(provisioningFenceCondition(), provisioningFenceArguments(expected)...).
			Where(clock.validLeaseCondition()).
			First(&operation)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if result.Error != nil {
			return result.Error
		}
		if !validProvisioningIdentity(operation.ID, operation.UpstreamUsername) {
			return errors.New("invalid provisioning operation identity")
		}
		err := tx.First(&account, "provisioning_operation_id = ?", expected.ID).Error
		if err == nil {
			if account.Status != "active" || !account.Managed ||
				account.ProvisioningOperationID == nil ||
				*account.ProvisioningOperationID != operation.ID {
				return errors.New("invalid activated database account")
			}
			if err := activateDatabaseProvisioningOperation(tx, expected, clock); err != nil {
				return err
			}
			activated = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		uniqueName, err := generateProvisioningUniqueName(tx)
		if err != nil {
			return err
		}
		sequence, err := nextProvisioningResourceSequence(tx)
		if err != nil {
			return err
		}
		operationID := operation.ID
		account = model.DatabaseAccount{
			InstanceID: operation.InstanceID, UniqueName: uniqueName,
			Username: operation.UpstreamUsername, Password: operation.Password,
			GroupName: operation.GroupName, Remark: operation.Remark,
			ExpiresAt: operation.ExpiresAt, Status: "active",
			Managed: true, UpstreamHost: operation.Host,
			ResourceSeq:             sequence,
			ResourceID:              util.ResourceIDFromSeq(util.PrefixDatabase, sequence),
			ProvisioningOperationID: &operationID,
		}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		if err := ensureAccountGroup(tx, account.GroupName); err != nil {
			return err
		}
		if err := s.syncResourceTx(
			tx, model.ResourceTypeDatabaseAccount, account.ID,
			databaseAccountResourceName(account), account.InstanceID,
		); err != nil {
			return err
		}
		if err := activateDatabaseProvisioningOperation(tx, expected, clock); err != nil {
			return err
		}
		activated = true
		return nil
	})
	if errors.Is(err, errDatabaseProvisioningFenceLost) {
		return service.ProvisionedDatabaseAccount{}, false, nil
	}
	if err != nil {
		return service.ProvisionedDatabaseAccount{}, false,
			errors.New("activate database provisioning operation")
	}
	if !activated {
		return service.ProvisionedDatabaseAccount{}, false, nil
	}
	return provisionedDatabaseAccountFromModel(s, account), true, nil
}

func activateDatabaseProvisioningOperation(
	tx *gorm.DB,
	expected service.DatabaseProvisioningFence,
	clock databaseProvisioningStatementClock,
) error {
	updated := tx.Model(&model.DatabaseProvisioningOperation{}).Scopes(ActiveScope).
		Where(provisioningFenceCondition(), provisioningFenceArguments(expected)...).
		Where(clock.validLeaseCondition()).
		Updates(map[string]any{
			"stage":              provisioningStageActiveManaged,
			"active_retained_at": clock.currentTimestampExpression(),
			"terminal_at":        nil,
			"last_error":         "",
			"lease_owner":        "",
			"lease_token":        "",
			"lease_expires_at":   nil,
			"revision":           gorm.Expr("revision + 1"),
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return errDatabaseProvisioningFenceLost
	}
	return nil
}

func generateProvisioningUniqueName(db *gorm.DB) (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		name := "db-" + model.NewID()[:12]
		var count int64
		if err := db.Model(&model.DatabaseAccount{}).Scopes(ActiveScope).
			Where("unique_name = ?", name).
			Count(&count).Error; err != nil {
			return "", errors.New("check database account name")
		}
		if count == 0 {
			return name, nil
		}
	}
	return "", errors.New("generate unique database account name")
}

func nextProvisioningResourceSequence(db *gorm.DB) (int, error) {
	var maxSequence int
	if err := db.Model(&model.DatabaseAccount{}).Scopes(ActiveScope).
		Select("COALESCE(MAX(resource_seq), 0)").
		Scan(&maxSequence).Error; err != nil {
		return 0, errors.New("database resource sequence floor")
	}
	if err := storage.EnsureSequenceNextValue(
		db, storage.SequenceDatabaseAccount, maxSequence+1,
	); err != nil {
		return 0, err
	}
	return storage.NextSequenceValue(
		db, storage.SequenceDatabaseAccount, storage.MaxCompactResourceSeq,
	)
}
