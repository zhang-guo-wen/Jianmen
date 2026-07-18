package service

import (
	"context"
	"errors"
	"strings"
)

type DatabaseProvisioningActor struct {
	UserID   string
	Username string
	ClientIP string
}

type DatabaseProvisioningAudit struct {
	Actor      DatabaseProvisioningActor
	InstanceID string
	AccountID  string
	Operation  string
	Result     string
}

type ListProvisioningDatabasesRequest struct {
	InstanceID     string
	AdminAccountID string
	Actor          DatabaseProvisioningActor
}

func (s *DatabaseProvisioningService) ListDatabases(
	ctx context.Context,
	request ListProvisioningDatabasesRequest,
) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("list provisioning databases: nil context")
	}
	request.InstanceID = strings.TrimSpace(request.InstanceID)
	request.AdminAccountID = strings.TrimSpace(request.AdminAccountID)
	instance, admin, err := s.repository.DatabaseProvisioningAdmin(
		ctx,
		request.InstanceID,
		request.AdminAccountID,
	)
	if err != nil || validateProvisioningAdministrator(instance, admin, s.now().UTC()) != nil {
		return nil, ErrDatabaseProvisioningFailed
	}
	auditID, err := s.beginCredentialAudit(
		ctx,
		request.Actor,
		request.InstanceID,
		request.AdminAccountID,
		"list_databases",
	)
	if err != nil {
		return nil, ErrDatabaseProvisioningFailed
	}
	databases, err := s.provisioner.ListDatabases(ctx, instance, admin)
	result := "success"
	if err != nil {
		result = "failure"
	}
	s.completeCredentialAudit(ctx, auditID, result)
	if err != nil {
		return nil, ErrDatabaseProvisioningFailed
	}
	return databases, nil
}

func (s *DatabaseProvisioningService) beginCredentialAudit(
	ctx context.Context,
	actor DatabaseProvisioningActor,
	instanceID, accountID, operation string,
) (string, error) {
	return s.repository.BeginDatabaseProvisioningAudit(
		ctx,
		DatabaseProvisioningAudit{
			Actor: actor, InstanceID: instanceID, AccountID: accountID,
			Operation: operation, Result: "started",
		},
	)
}

func (s *DatabaseProvisioningService) completeCredentialAudit(
	parent context.Context,
	auditID, result string,
) {
	ctx, cancel := s.detachedContext(parent)
	defer cancel()
	if err := s.repository.CompleteDatabaseProvisioningAudit(ctx, auditID, result); err != nil {
		s.logger.Warn(
			"database provisioning audit completion failed",
			"audit_id", auditID,
			"result", result,
		)
	}
}
