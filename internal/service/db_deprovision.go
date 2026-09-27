package service

import (
	"context"
)

// Deprovision removes one managed account only after the exact upstream identity
// owned by its retained provisioning operation has been dropped.
func (s *DatabaseProvisioningService) Deprovision(ctx context.Context, accountID string) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrDatabaseDeprovisionFailed
	}
	token, err := s.randomString(20, provisioningOperationAlphabet)
	if err != nil {
		return ErrDatabaseDeprovisionFailed
	}
	op, managed, err := s.repository.BeginDatabaseDeprovision(ctx, accountID, DatabaseProvisioningLease{
		Owner: s.workerID, Token: token, Duration: s.leaseDuration,
	})
	if err != nil {
		return ErrDatabaseDeprovisionFailed
	}
	if !managed {
		return ErrDatabaseAccountNotManaged
	}
	account, found, err := s.repository.ProvisionedDatabaseAccountByOperation(ctx, op.ID)
	if err != nil || !found || !managedAccountMatchesOperation(account, op) {
		return ErrDatabaseDeprovisionFailed
	}
	if op.Stage != ProvisioningStageDeprovisionRequested {
		return ErrDatabaseDeprovisionInProgress
	}
	return s.runDeprovision(ctx, accountID, account, op)
}

func (s *DatabaseProvisioningService) runDeprovision(parent context.Context, accountID string, account ProvisionedDatabaseAccount, op DatabaseProvisioningOperation) error {
	if !managedAccountMatchesOperation(account, op) || validateMySQLAccountHost(op.Host) != nil {
		return ErrDatabaseDeprovisionFailed
	}
	instance, admin, err := s.repository.DatabaseProvisioningAdmin(parent, op.InstanceID, op.AdminAccountID)
	if err != nil || validateProvisioningAdministratorForRecovery(instance, admin) != nil {
		s.deferDropUncertain(parent, op)
		return ErrDatabaseDeprovisionFailed
	}
	if op.Stage != ProvisioningStageDropStarted {
		var ok bool
		op, ok, err = s.transitionDetached(parent, op, DatabaseProvisioningTransition{
			Stage: ProvisioningStageDropStarted, CleanupStatus: ProvisioningCleanupInProgress, IncrementAttempt: true,
		})
		if err != nil || !ok {
			return ErrDatabaseDeprovisionInProgress
		}
	} else if op.CleanupStatus != ProvisioningCleanupInProgress {
		return ErrDatabaseDeprovisionInProgress
	}
	renewed, dropCtx, cancel, ready := s.renewForSideEffect(parent, op)
	if !ready || dropCtx == nil || dropCtx.Err() != nil {
		if cancel != nil {
			cancel()
		}
		s.deferDropUncertain(parent, op)
		return ErrDatabaseDeprovisionFailed
	}
	op = renewed
	dropErr := s.provisioner.DropAccount(dropCtx, instance, admin, op.Username, op.Host)
	cancel()
	if dropErr != nil {
		s.deferDropUncertain(parent, op)
		return ErrDatabaseDeprovisionFailed
	}
	completed, err := s.completeDeprovisionDetached(parent, op, accountID)
	if err != nil || !completed {
		// The upstream DROP is final. Never recreate it on a local commit failure.
		s.deferDropUncertain(parent, op)
		return ErrDatabaseDeprovisionFailed
	}
	return nil
}

func (s *DatabaseProvisioningService) deferDropUncertain(parent context.Context, op DatabaseProvisioningOperation) {
	_, _, _ = s.transitionDetached(parent, op, DatabaseProvisioningTransition{
		Stage: ProvisioningStageDropUncertain, CleanupStatus: ProvisioningCleanupRequired,
		LastError: ProvisioningErrorCleanupFailed, ReleaseLease: true,
	})
}

func (s *DatabaseProvisioningService) completeDeprovisionDetached(parent context.Context, op DatabaseProvisioningOperation, accountID string) (bool, error) {
	ctx, cancel := s.detachedContext(parent)
	defer cancel()
	return s.repository.CompleteDatabaseDeprovision(ctx, op.Fence(), accountID)
}

func (s *DatabaseProvisioningService) reconcileDeprovision(parent context.Context, op DatabaseProvisioningOperation) bool {
	account, found, err := s.repository.ProvisionedDatabaseAccountByOperation(parent, op.ID)
	if err != nil || !found || !managedAccountMatchesOperation(account, op) {
		return false
	}
	return s.runDeprovision(parent, account.ID, account, op) == nil
}

func managedAccountMatchesOperation(account ProvisionedDatabaseAccount, operation DatabaseProvisioningOperation) bool {
	return account.Managed && account.ProvisioningOperationID == operation.ID &&
		account.Username == operation.Username && account.UpstreamHost == operation.Host &&
		operationOwnsUpstreamIdentity(operation)
}
