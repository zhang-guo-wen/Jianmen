package store

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func (s *DBStore) TransitionDatabaseProvisioningOperation(
	ctx context.Context,
	expected service.DatabaseProvisioningFence,
	transition service.DatabaseProvisioningTransition,
) (service.DatabaseProvisioningOperation, bool, error) {
	if ctx == nil {
		return service.DatabaseProvisioningOperation{}, false,
			errors.New("transition database provisioning operation: nil context")
	}
	if !validProvisioningTransition(expected.Stage, transition.Stage, transition.CleanupStatus) ||
		!safeProvisioningErrorCode(transition.LastError) {
		return service.DatabaseProvisioningOperation{}, false,
			errors.New("invalid database provisioning transition")
	}
	clock, err := newDatabaseProvisioningStatementClock(s.db)
	if err != nil {
		return service.DatabaseProvisioningOperation{}, false,
			errors.New("transition database provisioning operation: database clock")
	}
	updates := map[string]any{
		"stage": transition.Stage, "cleanup_status": transition.CleanupStatus,
		"last_error": strings.TrimSpace(transition.LastError),
		"revision":   gorm.Expr("revision + 1"),
	}
	if transition.IncrementAttempt {
		updates["attempt_count"] = gorm.Expr("attempt_count + 1")
		updates["last_attempt_at"] = clock.currentTimestampExpression()
	}
	if transition.ReleaseLease {
		updates["lease_owner"] = ""
		updates["lease_token"] = ""
		updates["lease_expires_at"] = nil
	}
	if transition.Stage == service.ProvisioningStageNotCreated {
		updates["terminal_at"] = clock.currentTimestampExpression()
	}
	result := s.db.WithContext(ctx).Model(&model.DatabaseProvisioningOperation{}).Scopes(ActiveScope).
		Where(provisioningFenceCondition(), provisioningFenceArguments(expected)...).
		Where(clock.validLeaseCondition()).
		Updates(updates)
	if result.Error != nil {
		return service.DatabaseProvisioningOperation{}, false,
			errors.New("transition database provisioning operation")
	}
	if result.RowsAffected == 0 {
		return service.DatabaseProvisioningOperation{}, false, nil
	}
	operation, err := s.DatabaseProvisioningOperation(ctx, expected.ID)
	return operation, err == nil, err
}

func (s *DBStore) ClaimDatabaseProvisioningOperation(
	ctx context.Context,
	expected service.DatabaseProvisioningFence,
	lease service.DatabaseProvisioningLease,
) (service.DatabaseProvisioningOperation, service.DatabaseProvisioningLeaseWindow, bool, error) {
	if ctx == nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			errors.New("claim database provisioning operation: nil context")
	}
	lease.Owner = strings.TrimSpace(lease.Owner)
	lease.Token = strings.TrimSpace(lease.Token)
	if lease.Owner == "" || lease.Token == "" || lease.Duration <= 0 {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			errors.New("invalid database provisioning lease")
	}
	clock, err := newDatabaseProvisioningStatementClock(s.db)
	if err != nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			errors.New("claim database provisioning operation: database clock")
	}
	expiresAt, err := clock.leaseExpiryExpression(lease.Duration)
	if err != nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			errors.New("claim database provisioning operation: invalid lease duration")
	}
	return s.updateProvisioningLease(ctx, expected, clock, expiresAt, lease, true)
}

func (s *DBStore) RenewDatabaseProvisioningOperation(
	ctx context.Context,
	expected service.DatabaseProvisioningFence,
	lease service.DatabaseProvisioningLease,
) (service.DatabaseProvisioningOperation, service.DatabaseProvisioningLeaseWindow, bool, error) {
	if ctx == nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			errors.New("renew database provisioning operation: nil context")
	}
	lease.Owner = strings.TrimSpace(lease.Owner)
	lease.Token = strings.TrimSpace(lease.Token)
	if lease.Owner == "" || lease.Token == "" || lease.Duration <= 0 ||
		expected.LeaseOwner != lease.Owner || expected.LeaseToken != lease.Token {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			errors.New("invalid database provisioning lease renewal")
	}
	clock, err := newDatabaseProvisioningStatementClock(s.db)
	if err != nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			errors.New("renew database provisioning operation: database clock")
	}
	expiresAt, err := clock.leaseExpiryExpression(lease.Duration)
	if err != nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			errors.New("renew database provisioning operation: invalid lease duration")
	}
	return s.updateProvisioningLease(ctx, expected, clock, expiresAt, lease, false)
}

func (s *DBStore) updateProvisioningLease(
	ctx context.Context,
	expected service.DatabaseProvisioningFence,
	clock databaseProvisioningStatementClock,
	expiresAt any,
	lease service.DatabaseProvisioningLease,
	claim bool,
) (service.DatabaseProvisioningOperation, service.DatabaseProvisioningLeaseWindow, bool, error) {
	var operation model.DatabaseProvisioningOperation
	var window service.DatabaseProvisioningLeaseWindow
	updated := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&model.DatabaseProvisioningOperation{}).Scopes(ActiveScope).
			Where(provisioningFenceCondition(), provisioningFenceArguments(expected)...)
		if claim {
			query = query.Where(clock.expiredOrUnsetLeaseCondition())
		} else {
			query = query.Where(clock.validLeaseCondition())
		}
		result := query.Updates(map[string]any{
			"lease_owner": lease.Owner, "lease_token": lease.Token,
			"lease_expires_at": expiresAt,
			"revision":         gorm.Expr("revision + 1"),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		updated = true
		if err := tx.Scopes(ActiveScope).First(&operation, "id = ?", expected.ID).Error; err != nil {
			return err
		}
		var err error
		window, err = clock.leaseWindow(ctx, tx, expected.ID, lease.Duration)
		return err
	})
	if err != nil {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false,
			errors.New("update database provisioning lease")
	}
	if !updated {
		return service.DatabaseProvisioningOperation{}, service.DatabaseProvisioningLeaseWindow{}, false, nil
	}
	return databaseProvisioningOperationFromModel(operation), window, true, nil
}

func (s *DBStore) DeleteDatabaseProvisioningOperation(
	ctx context.Context,
	expected service.DatabaseProvisioningFence,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("delete database provisioning operation: nil context")
	}
	switch expected.Stage {
	case service.ProvisioningStageReserved,
		service.ProvisioningStageNotCreated,
		service.ProvisioningStageCleanupInProgress:
	default:
		return false, nil
	}
	clock, err := newDatabaseProvisioningStatementClock(s.db)
	if err != nil {
		return false, errors.New("delete database provisioning operation: database clock")
	}
	result := softDeleteWhere(
		ctx,
		s.db,
		"database_provisioning_operations",
		"("+provisioningFenceCondition()+") AND ("+clock.validLeaseCondition()+")",
		provisioningFenceArguments(expected)...,
	)
	if result.Error != nil {
		return false, errors.New("delete database provisioning operation")
	}
	return result.RowsAffected == 1, nil
}

func provisioningFenceCondition() string {
	return "id = ? AND stage = ? AND cleanup_status = ? AND revision = ? AND lease_owner = ? AND lease_token = ?"
}

func provisioningFenceArguments(expected service.DatabaseProvisioningFence) []any {
	return []any{
		expected.ID, expected.Stage, expected.CleanupStatus, expected.Revision,
		expected.LeaseOwner, expected.LeaseToken,
	}
}
