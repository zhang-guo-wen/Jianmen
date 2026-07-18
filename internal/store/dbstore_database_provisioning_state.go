package store

import (
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func validProvisioningIdentity(id, username string) bool {
	token := strings.TrimPrefix(strings.TrimSpace(id), "jmo_")
	if token == strings.TrimSpace(id) || len(token) != 20 ||
		strings.TrimSpace(username) != "jm_"+token {
		return false
	}
	for _, character := range token {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func validProvisioningTransition(from, to, cleanup string) bool {
	switch to {
	case service.ProvisioningStageCreateStarted:
		return from == service.ProvisioningStageReserved &&
			cleanup == service.ProvisioningCleanupNone
	case service.ProvisioningStageUpstreamCreated:
		return from == service.ProvisioningStageCreateStarted &&
			cleanup == service.ProvisioningCleanupNone
	case service.ProvisioningStageCreateUncertain:
		return from == service.ProvisioningStageCreateStarted &&
			cleanup == service.ProvisioningCleanupRequired
	case service.ProvisioningStageGrantStarted:
		return from == service.ProvisioningStageUpstreamCreated &&
			cleanup == service.ProvisioningCleanupNone
	case service.ProvisioningStageActivationPending:
		return (from == service.ProvisioningStageGrantStarted ||
			from == service.ProvisioningStageActivationPending) &&
			cleanup == service.ProvisioningCleanupNone
	case service.ProvisioningStageNotCreated:
		return (from == service.ProvisioningStageReserved ||
			from == service.ProvisioningStageCreateStarted ||
			from == service.ProvisioningStageNotCreated) &&
			cleanup == service.ProvisioningCleanupNone
	case service.ProvisioningStageCleanupRequired:
		return databaseProvisioningCleanupSource(from) &&
			(cleanup == service.ProvisioningCleanupRequired ||
				cleanup == service.ProvisioningCleanupFailed)
	case service.ProvisioningStageCleanupInProgress:
		return databaseProvisioningCleanupSource(from) &&
			cleanup == service.ProvisioningCleanupInProgress
	default:
		return false
	}
}

func databaseProvisioningCleanupSource(stage string) bool {
	switch stage {
	case service.ProvisioningStageCreateStarted,
		service.ProvisioningStageCreateUncertain,
		service.ProvisioningStageUpstreamCreated,
		service.ProvisioningStageGrantStarted,
		service.ProvisioningStageCleanupRequired,
		service.ProvisioningStageCleanupInProgress:
		return true
	default:
		return false
	}
}

func safeProvisioningErrorCode(value string) bool {
	switch strings.TrimSpace(value) {
	case "", service.ProvisioningErrorCreateUncertain,
		service.ProvisioningErrorGrantFailed,
		service.ProvisioningErrorActivationFailed,
		service.ProvisioningErrorCleanupFailed:
		return true
	default:
		return false
	}
}

func databaseProvisioningOperationFromModel(
	record model.DatabaseProvisioningOperation,
) service.DatabaseProvisioningOperation {
	return service.DatabaseProvisioningOperation{
		ID: record.ID, InstanceID: record.InstanceID,
		AdminAccountID: record.AdminAccountID, Username: record.UpstreamUsername,
		Password: record.Password.GetPlaintext(), Host: record.Host,
		GrantsJSON: record.GrantsJSON, Group: record.GroupName, Remark: record.Remark,
		ExpiresAt: record.ExpiresAt, Stage: record.Stage,
		CleanupStatus: record.CleanupStatus, LastError: record.LastError,
		AttemptCount: record.AttemptCount, LastAttemptAt: record.LastAttemptAt,
		Revision: record.Revision, LeaseOwner: record.LeaseOwner,
		LeaseToken: record.LeaseToken, LeaseExpiresAt: record.LeaseExpiresAt,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func provisionedDatabaseAccountFromModel(
	s *DBStore,
	account model.DatabaseAccount,
) service.ProvisionedDatabaseAccount {
	view := s.databaseAccountView(account)
	return service.ProvisionedDatabaseAccount{
		ID: view.ID, InstanceID: view.InstanceID, UniqueName: view.UniqueName,
		Username: view.Username, Group: view.Group, Remark: view.Remark,
		ExpiresAt: view.ExpiresAt, Status: view.Status, ResourceID: view.ResourceID,
		ResourceSeq: view.ResourceSeq, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
	}
}
