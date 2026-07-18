package service

import (
	"context"
	"errors"
	"time"
)

type DatabaseProvisioningReconcileResult struct {
	Scanned         int
	Claimed         int
	Cleaned         int
	Activated       int
	DeletedReserved int
	ClaimSkipped    int
	Failed          int
}

func (s *DatabaseProvisioningService) Reconcile(
	ctx context.Context,
	limit int,
) (DatabaseProvisioningReconcileResult, error) {
	if ctx == nil {
		return DatabaseProvisioningReconcileResult{},
			errors.New("reconcile database provisioning: nil context")
	}
	operations, err := s.repository.ListExecutableDatabaseProvisioningOperations(
		ctx,
		s.reconcileStaleAfter,
		limit,
	)
	if err != nil {
		return DatabaseProvisioningReconcileResult{},
			errors.New("list executable database provisioning operations")
	}
	result := DatabaseProvisioningReconcileResult{Scanned: len(operations)}
	for _, candidate := range operations {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		claimed, ok, claimErr := s.claimForReconcile(ctx, candidate)
		if claimErr != nil {
			result.Failed++
			continue
		}
		if !ok {
			result.ClaimSkipped++
			continue
		}
		result.Claimed++
		switch claimed.Stage {
		case ProvisioningStageReserved:
			deleted, deleteErr := s.deleteClaimedOperation(ctx, claimed)
			if deleteErr != nil || !deleted {
				result.Failed++
			} else {
				result.DeletedReserved++
			}
		case ProvisioningStageActivationPending:
			account, activated := s.activateDetached(ctx, claimed)
			if !activated || account.Status != "active" {
				s.releaseActivationForReconcile(ctx, claimed)
				result.Failed++
			} else {
				result.Activated++
			}
		case ProvisioningStageDeprovisionRequested, ProvisioningStageDropStarted, ProvisioningStageDropUncertain:
			if s.reconcileDeprovision(ctx, claimed) {
				result.Cleaned++
			} else {
				result.Failed++
			}
		default:
			if !databaseProvisioningStageRequiresCleanup(claimed.Stage) {
				result.Failed++
				continue
			}
			if s.reconcileCleanup(ctx, claimed) {
				result.Cleaned++
			} else {
				result.Failed++
			}
		}
	}
	return result, nil
}

func (s *DatabaseProvisioningService) claimForReconcile(
	ctx context.Context,
	candidate DatabaseProvisioningOperation,
) (DatabaseProvisioningOperation, bool, error) {
	token, err := randomProvisioningString(
		s.random,
		20,
		provisioningOperationAlphabet,
	)
	if err != nil {
		return DatabaseProvisioningOperation{}, false,
			errors.New("generate database provisioning claim token")
	}
	claimed, _, ok, err := s.repository.ClaimDatabaseProvisioningOperation(
		ctx,
		candidate.Fence(),
		DatabaseProvisioningLease{
			Owner: s.workerID, Token: token, Duration: s.leaseDuration,
		},
	)
	if err != nil {
		return DatabaseProvisioningOperation{}, false,
			errors.New("claim database provisioning operation")
	}
	return claimed, ok, nil
}

func (s *DatabaseProvisioningService) deleteClaimedOperation(
	parent context.Context,
	operation DatabaseProvisioningOperation,
) (bool, error) {
	ctx, cancel := s.detachedContext(parent)
	defer cancel()
	return s.repository.DeleteDatabaseProvisioningOperation(
		ctx,
		operation.Fence(),
	)
}

func (s *DatabaseProvisioningService) reconcileCleanup(
	parent context.Context,
	operation DatabaseProvisioningOperation,
) bool {
	if !operationOwnsUpstreamIdentity(operation) {
		s.persistCleanupFailure(parent, operation)
		return false
	}
	instance, admin, err := s.repository.DatabaseProvisioningAdmin(
		parent,
		operation.InstanceID,
		operation.AdminAccountID,
	)
	if err != nil || validateProvisioningAdministrator(instance, admin, s.now().UTC()) != nil {
		s.persistCleanupFailure(parent, operation)
		return false
	}
	auditID, err := s.beginCredentialAudit(
		parent,
		DatabaseProvisioningActor{UserID: "system", Username: "reconciler"},
		operation.InstanceID,
		operation.AdminAccountID,
		"reconcile_cleanup",
	)
	if err != nil {
		s.persistCleanupFailure(parent, operation)
		return false
	}
	operation, ok, err := s.transitionDetached(
		parent,
		operation,
		DatabaseProvisioningTransition{
			Stage:            ProvisioningStageCleanupInProgress,
			CleanupStatus:    ProvisioningCleanupInProgress,
			LastError:        operation.LastError,
			IncrementAttempt: true,
		},
	)
	if err != nil || !ok {
		s.completeCredentialAudit(parent, auditID, "failure")
		return false
	}
	operation, ctx, cancel, ready := s.renewForSideEffect(parent, operation)
	if !ready {
		s.completeCredentialAudit(parent, auditID, "failure")
		return false
	}
	if err := ctx.Err(); err != nil {
		cancel()
		s.completeCredentialAudit(parent, auditID, "failure")
		return false
	}
	dropErr := s.provisioner.DropAccount(
		ctx,
		instance,
		admin,
		operation.Username,
		operation.Host,
	)
	cancel()
	if dropErr != nil {
		s.completeCredentialAudit(parent, auditID, "failure")
		s.persistCleanupFailure(parent, operation)
		return false
	}
	s.completeCredentialAudit(parent, auditID, "success")
	deleted, err := s.deleteClaimedOperation(parent, operation)
	if err != nil || !deleted {
		s.persistCleanupFailure(parent, operation)
		return false
	}
	return true
}

func databaseProvisioningStageRequiresCleanup(stage string) bool {
	switch stage {
	case ProvisioningStageCreateStarted,
		ProvisioningStageCreateUncertain,
		ProvisioningStageUpstreamCreated,
		ProvisioningStageGrantStarted,
		ProvisioningStageCleanupRequired,
		ProvisioningStageCleanupInProgress:
		return true
	default:
		return false
	}
}

func (s *DatabaseProvisioningService) RunReconciler(
	ctx context.Context,
	interval time.Duration,
	batchSize int,
) error {
	if ctx == nil {
		return errors.New("run database provisioning reconciler: nil context")
	}
	if interval <= 0 {
		return errors.New("database provisioning reconcile interval must be positive")
	}
	if batchSize <= 0 {
		return errors.New("database provisioning reconcile batch size must be positive")
	}
	runRound := func() {
		result, err := s.Reconcile(ctx, batchSize)
		if err != nil {
			s.logger.Warn(
				"database provisioning reconciliation round failed",
				"scanned", result.Scanned,
				"claimed", result.Claimed,
				"failed", result.Failed+1,
			)
			return
		}
		log := s.logger.Info
		if result.Failed > 0 {
			log = s.logger.Warn
		}
		log(
			"database provisioning reconciliation round completed",
			"scanned", result.Scanned,
			"claimed", result.Claimed,
			"cleaned", result.Cleaned,
			"activated", result.Activated,
			"deleted_reserved", result.DeletedReserved,
			"claim_skipped", result.ClaimSkipped,
			"failed", result.Failed,
		)
	}
	runRound()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runRound()
		}
	}
}
