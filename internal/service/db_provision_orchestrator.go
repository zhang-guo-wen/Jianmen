package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
)

func (s *DatabaseProvisioningService) Provision(
	ctx context.Context,
	request ProvisionDatabaseAccountRequest,
) (ProvisionDatabaseAccountResult, error) {
	if ctx == nil {
		return ProvisionDatabaseAccountResult{}, errors.New("provision database account: nil context")
	}
	request.InstanceID = strings.TrimSpace(request.InstanceID)
	request.AdminAccountID = strings.TrimSpace(request.AdminAccountID)
	request.Host = strings.TrimSpace(request.Host)
	instance, admin, err := s.repository.DatabaseProvisioningAdmin(
		ctx, request.InstanceID, request.AdminAccountID,
	)
	if err != nil || validateProvisioningAdministrator(instance, admin, s.now().UTC()) != nil {
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	auditID, err := s.beginCredentialAudit(
		ctx, request.Actor, request.InstanceID, request.AdminAccountID, "provision_account",
	)
	if err != nil {
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	auditResult := "failure"
	defer func() {
		s.completeCredentialAudit(ctx, auditID, auditResult)
	}()
	if err := ctx.Err(); err != nil {
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}

	token, err := randomProvisioningString(s.random, 20, provisioningOperationAlphabet)
	if err != nil {
		return ProvisionDatabaseAccountResult{}, fmt.Errorf("generate database operation identity: %w", err)
	}
	password, err := randomProvisioningString(s.random, 32, provisioningPasswordAlphabet)
	if err != nil {
		return ProvisionDatabaseAccountResult{}, fmt.Errorf("generate database password: %w", err)
	}
	leaseToken, err := randomProvisioningString(s.random, 20, provisioningOperationAlphabet)
	if err != nil {
		return ProvisionDatabaseAccountResult{}, fmt.Errorf("generate database operation lease: %w", err)
	}
	username := "jm_" + token
	if err := ValidateMySQLProvisioning(username, password, request.Host, request.Grants); err != nil {
		return ProvisionDatabaseAccountResult{}, fmt.Errorf("%w: invalid provisioning fields", ErrInvalidDatabaseProvisioningRequest)
	}
	grantsJSON, err := json.Marshal(request.Grants)
	if err != nil {
		return ProvisionDatabaseAccountResult{}, errors.New("encode database grants")
	}
	operation, _, err := s.repository.CreateDatabaseProvisioningOperation(
		ctx,
		DatabaseProvisioningOperationInput{
			ID: "jmo_" + token, InstanceID: request.InstanceID, AdminAccountID: request.AdminAccountID,
			Username: username, Password: password, Host: request.Host, GrantsJSON: string(grantsJSON),
			Group: request.Group, Remark: request.Remark, ExpiresAt: request.ExpiresAt,
			Lease: DatabaseProvisioningLease{Owner: s.workerID, Token: leaseToken, Duration: s.leaseDuration},
		},
	)
	if err != nil {
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	if ctx.Err() != nil {
		s.discardNotCreatedOperation(ctx, operation)
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	operation, ok, err := s.transitionDetached(ctx, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageCreateStarted, CleanupStatus: ProvisioningCleanupNone,
		IncrementAttempt: true,
	})
	if err != nil || !ok {
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}

	operation, createContext, cancelCreate, ready := s.renewForSideEffect(ctx, operation)
	if !ready {
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	if err := createContext.Err(); err != nil {
		cancelCreate()
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	createResult, createErr := s.provisioner.CreateAccount(
		createContext, instance, admin, operation.Username, operation.Password, operation.Host,
	)
	cancelCreate()
	switch createResult.Disposition {
	case DatabaseAccountCreateNotSent, DatabaseAccountCreateNotCreated:
		s.discardNotCreatedOperation(ctx, operation)
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	case DatabaseAccountCreateMayBeApplied:
		return ProvisionDatabaseAccountResult{}, s.cleanupUncertainCreate(ctx, operation, instance, admin)
	case DatabaseAccountCreateApplied:
		if createErr != nil {
			return ProvisionDatabaseAccountResult{}, s.cleanupUncertainCreate(ctx, operation, instance, admin)
		}
	default:
		return ProvisionDatabaseAccountResult{}, s.cleanupUncertainCreate(ctx, operation, instance, admin)
	}

	operation, ok, err = s.transitionDetached(ctx, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageUpstreamCreated, CleanupStatus: ProvisioningCleanupNone,
	})
	if err != nil || !ok {
		return ProvisionDatabaseAccountResult{}, s.cleanupOperation(ctx, operation, instance, admin, ProvisioningErrorGrantFailed)
	}
	operation, ok, err = s.transitionDetached(ctx, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageGrantStarted, CleanupStatus: ProvisioningCleanupNone,
	})
	if err != nil || !ok {
		return ProvisionDatabaseAccountResult{}, s.cleanupOperation(ctx, operation, instance, admin, ProvisioningErrorGrantFailed)
	}
	renewed, grantContext, cancelGrant, ready := s.renewForSideEffect(ctx, operation)
	if !ready {
		s.deferCleanupForReconcile(ctx, operation, ProvisioningErrorGrantFailed)
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	operation = renewed
	if err := grantContext.Err(); err != nil {
		cancelGrant()
		s.deferCleanupForReconcile(ctx, operation, ProvisioningErrorGrantFailed)
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	grantErr := s.provisioner.GrantAccount(
		grantContext, instance, admin, operation.Username, operation.Host, request.Grants,
	)
	cancelGrant()
	if grantErr != nil {
		return ProvisionDatabaseAccountResult{}, s.cleanupOperation(ctx, operation, instance, admin, ProvisioningErrorGrantFailed)
	}
	operation, ok, err = s.transitionDetached(ctx, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageActivationPending, CleanupStatus: ProvisioningCleanupNone,
	})
	if err != nil || !ok {
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	account, activated := s.activateDetached(ctx, operation)
	if !activated {
		s.releaseActivationForReconcile(ctx, operation)
		return ProvisionDatabaseAccountResult{}, ErrDatabaseProvisioningFailed
	}
	auditResult = "success"
	return ProvisionDatabaseAccountResult{Account: account}, nil
}

func (s *DatabaseProvisioningService) cleanupUncertainCreate(
	parent context.Context,
	operation DatabaseProvisioningOperation,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
) error {
	operation, ok, err := s.transitionDetached(parent, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageCreateUncertain, CleanupStatus: ProvisioningCleanupRequired,
		LastError: ProvisioningErrorCreateUncertain,
	})
	if err != nil || !ok {
		return ErrDatabaseProvisioningCleanupRequired
	}
	return s.cleanupOperation(parent, operation, instance, admin, ProvisioningErrorCreateUncertain)
}

func (s *DatabaseProvisioningService) discardNotCreatedOperation(
	parent context.Context,
	operation DatabaseProvisioningOperation,
) {
	updated, ok, err := s.transitionDetached(parent, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageNotCreated, CleanupStatus: ProvisioningCleanupNone,
	})
	if err != nil || !ok {
		return
	}
	ctx, cancel := s.detachedContext(parent)
	defer cancel()
	_, _ = s.repository.DeleteDatabaseProvisioningOperation(ctx, updated.Fence())
}

func (s *DatabaseProvisioningService) cleanupOperation(
	parent context.Context,
	operation DatabaseProvisioningOperation,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	reason string,
) error {
	if !operationOwnsUpstreamIdentity(operation) {
		return ErrDatabaseProvisioningCleanupRequired
	}
	updated, ok, err := s.transitionDetached(parent, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageCleanupRequired, CleanupStatus: ProvisioningCleanupRequired, LastError: reason,
	})
	if err != nil || !ok {
		return ErrDatabaseProvisioningCleanupRequired
	}
	updated, ok, err = s.transitionDetached(parent, updated, DatabaseProvisioningTransition{
		Stage: ProvisioningStageCleanupInProgress, CleanupStatus: ProvisioningCleanupInProgress,
		LastError: reason, IncrementAttempt: true,
	})
	if err != nil || !ok {
		return ErrDatabaseProvisioningCleanupRequired
	}
	updated, dropContext, cancelDrop, ready := s.renewForSideEffect(parent, updated)
	if !ready {
		return ErrDatabaseProvisioningCleanupRequired
	}
	if err := dropContext.Err(); err != nil {
		cancelDrop()
		return ErrDatabaseProvisioningCleanupRequired
	}
	dropErr := s.provisioner.DropAccount(dropContext, instance, admin, updated.Username, updated.Host)
	cancelDrop()
	if dropErr != nil {
		s.persistCleanupFailure(parent, updated)
		return ErrDatabaseProvisioningCleanupRequired
	}
	ctx, cancel := s.detachedContext(parent)
	defer cancel()
	deleted, err := s.repository.DeleteDatabaseProvisioningOperation(ctx, updated.Fence())
	if err != nil || !deleted {
		return ErrDatabaseProvisioningCleanupRequired
	}
	return ErrDatabaseProvisioningFailed
}

func (s *DatabaseProvisioningService) persistCleanupFailure(
	parent context.Context,
	operation DatabaseProvisioningOperation,
) {
	_, _, _ = s.transitionDetached(parent, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageCleanupRequired, CleanupStatus: ProvisioningCleanupFailed,
		LastError: ProvisioningErrorCleanupFailed, ReleaseLease: true,
	})
}

func (s *DatabaseProvisioningService) deferCleanupForReconcile(
	parent context.Context,
	operation DatabaseProvisioningOperation,
	reason string,
) {
	_, _, _ = s.transitionDetached(parent, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageCleanupRequired, CleanupStatus: ProvisioningCleanupRequired,
		LastError: reason, ReleaseLease: true,
	})
}

func (s *DatabaseProvisioningService) activateDetached(
	parent context.Context,
	operation DatabaseProvisioningOperation,
) (ProvisionedDatabaseAccount, bool) {
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := s.detachedContext(parent)
		account, activated, err := s.repository.ActivateDatabaseProvisioningOperation(ctx, operation.Fence())
		cancel()
		if err == nil && activated {
			return account, true
		}
	}
	return ProvisionedDatabaseAccount{}, false
}

func (s *DatabaseProvisioningService) releaseActivationForReconcile(
	parent context.Context,
	operation DatabaseProvisioningOperation,
) {
	_, _, _ = s.transitionDetached(parent, operation, DatabaseProvisioningTransition{
		Stage: ProvisioningStageActivationPending, CleanupStatus: ProvisioningCleanupNone,
		LastError: ProvisioningErrorActivationFailed, IncrementAttempt: true, ReleaseLease: true,
	})
}

func (s *DatabaseProvisioningService) transitionDetached(
	parent context.Context,
	operation DatabaseProvisioningOperation,
	transition DatabaseProvisioningTransition,
) (DatabaseProvisioningOperation, bool, error) {
	ctx, cancel := s.detachedContext(parent)
	defer cancel()
	return s.repository.TransitionDatabaseProvisioningOperation(ctx, operation.Fence(), transition)
}

func (s *DatabaseProvisioningService) renewForSideEffect(
	parent context.Context,
	operation DatabaseProvisioningOperation,
) (DatabaseProvisioningOperation, context.Context, context.CancelFunc, bool) {
	if err := parent.Err(); err != nil {
		return DatabaseProvisioningOperation{}, nil, nil, false
	}
	renewStartedAt := time.Now()
	renewed, window, ok, err := s.repository.RenewDatabaseProvisioningOperation(
		parent,
		operation.Fence(),
		DatabaseProvisioningLease{
			Owner: operation.LeaseOwner, Token: operation.LeaseToken, Duration: s.leaseDuration,
		},
	)
	if err != nil || !ok {
		return DatabaseProvisioningOperation{}, nil, nil, false
	}
	deadline, ok := s.sideEffectDeadline(renewStartedAt, window)
	if !ok {
		return DatabaseProvisioningOperation{}, nil, nil, false
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	if err := ctx.Err(); err != nil {
		cancel()
		return DatabaseProvisioningOperation{}, nil, nil, false
	}
	return renewed, ctx, cancel, true
}

func (s *DatabaseProvisioningService) sideEffectDeadline(
	renewStartedAt time.Time,
	window DatabaseProvisioningLeaseWindow,
) (time.Time, bool) {
	timeout, ok := s.sideEffectTimeout(window)
	if !ok {
		return time.Time{}, false
	}
	return renewStartedAt.Add(timeout), true
}

func (s *DatabaseProvisioningService) sideEffectTimeout(
	window DatabaseProvisioningLeaseWindow,
) (time.Duration, bool) {
	if window.Remaining <= time.Nanosecond || s.leaseDuration <= time.Nanosecond {
		return 0, false
	}
	margin := s.cleanupTimeout
	if margin <= 0 || margin >= window.Remaining {
		margin = window.Remaining / 4
	}
	timeout := window.Remaining - margin
	if timeout <= 0 || timeout >= window.Remaining {
		return 0, false
	}
	if timeout >= s.leaseDuration {
		timeout = s.leaseDuration - time.Nanosecond
	}
	return timeout, timeout > 0 && timeout < window.Remaining && timeout < s.leaseDuration
}

func (s *DatabaseProvisioningService) detachedContext(
	parent context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), s.cleanupTimeout)
}
