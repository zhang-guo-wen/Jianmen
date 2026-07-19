package admin

import (
	"context"
	"errors"

	"jianmen/internal/service"
	"jianmen/internal/store"
)

type aiResourceRepositoryAdapter struct {
	hostTargets adminHostTargetRepository
	databases   adminDatabaseRepository
}

func (a aiResourceRepositoryAdapter) ListHostAccounts(ctx context.Context) ([]service.AIHostAccountMetadata, error) {
	targets, err := a.hostTargets.Targets(ctx)
	if err != nil {
		return nil, err
	}
	hosts, err := a.hostTargets.Hosts(ctx)
	if err != nil {
		return nil, err
	}
	hostStatuses := make(map[string]string, len(hosts))
	for _, host := range hosts {
		hostStatuses[host.ID] = host.Status
	}
	accounts := make([]service.AIHostAccountMetadata, 0, len(targets))
	for _, target := range targets {
		accounts = append(accounts, hostAccountMetadata(target, hostStatuses[target.HostID]))
	}
	return accounts, nil
}

func (a aiResourceRepositoryAdapter) HostAccount(ctx context.Context, id string) (service.AIHostAccountMetadata, error) {
	target, err := a.hostTargets.Target(ctx, id)
	if err != nil {
		return service.AIHostAccountMetadata{}, aiResourceRepositoryError(err)
	}
	host, err := a.hostTargets.Host(ctx, target.HostID)
	if err != nil {
		return service.AIHostAccountMetadata{}, aiResourceRepositoryError(err)
	}
	return hostAccountMetadata(target, host.Status), nil
}

func (a aiResourceRepositoryAdapter) ListDatabaseAccounts(ctx context.Context) ([]service.AIDatabaseAccountMetadata, error) {
	accounts, err := a.databases.DatabaseAccounts(ctx)
	if err != nil {
		return nil, err
	}
	instances, err := a.databases.ListDatabaseInstances(ctx)
	if err != nil {
		return nil, err
	}
	instanceByID := make(map[string]store.DatabaseInstanceView, len(instances))
	for _, instance := range instances {
		instanceByID[instance.ID] = instance
	}
	metadata := make([]service.AIDatabaseAccountMetadata, 0, len(accounts))
	for _, account := range accounts {
		metadata = append(metadata, databaseAccountMetadata(account, instanceByID[account.InstanceID]))
	}
	return metadata, nil
}

func (a aiResourceRepositoryAdapter) DatabaseAccount(ctx context.Context, id string) (service.AIDatabaseAccountMetadata, error) {
	account, err := a.databases.DatabaseAccount(ctx, id)
	if err != nil {
		return service.AIDatabaseAccountMetadata{}, aiResourceRepositoryError(err)
	}
	instance, err := a.databases.DatabaseInstance(ctx, account.InstanceID)
	if err != nil {
		return service.AIDatabaseAccountMetadata{}, aiResourceRepositoryError(err)
	}
	return databaseAccountMetadata(account, instance), nil
}

func hostAccountMetadata(target store.TargetView, parentStatus string) service.AIHostAccountMetadata {
	return service.AIHostAccountMetadata{
		ID: target.ID, HostID: target.HostID,
		Name: target.Name, Group: target.Group, Remark: target.Remark,
		Address: target.Host, Port: target.Port, Username: target.Username,
		ResourceID: target.ResourceID, ResourceSeq: target.ResourceSeq,
		Status: target.Status, ExpiresAt: target.ExpiresAt, ParentStatus: parentStatus,
	}
}

func databaseAccountMetadata(
	account store.DatabaseAccountView,
	instance store.DatabaseInstanceView,
) service.AIDatabaseAccountMetadata {
	return service.AIDatabaseAccountMetadata{
		ID: account.ID, InstanceID: account.InstanceID,
		Name: account.UniqueName, Group: account.Group, Remark: account.Remark,
		Username: account.Username, ResourceID: account.ResourceID, ResourceSeq: account.ResourceSeq,
		Status: account.Status, ExpiresAt: account.ExpiresAt,
		ParentAddress: instance.Address, ParentPort: instance.Port,
		ParentProtocol: instance.Protocol, ParentStatus: instance.Status,
	}
}

func aiResourceRepositoryError(err error) error {
	if errors.Is(err, store.ErrTargetNotFound) ||
		errors.Is(err, store.ErrHostNotFound) ||
		errors.Is(err, store.ErrDBAccountNotFound) ||
		errors.Is(err, store.ErrDBInstanceNotFound) {
		return service.ErrAIResourceNotFound
	}
	return err
}

type aiResourceAuthorizerAdapter struct {
	authorization authorizationService
}

func (a aiResourceAuthorizerAdapter) AuthorizeAIResources(
	ctx context.Context,
	actorID string,
	requests []service.AIResourceAuthorizationRequest,
) ([]service.AIResourceAuthorizationDecision, error) {
	authorizationRequests := make([]service.AuthorizationRequest, len(requests))
	for index, request := range requests {
		authorizationRequests[index] = service.AuthorizationRequest{
			UserID: actorID, Actions: request.Actions,
			ResourceType: request.ResourceType, ResourceID: request.ResourceID,
		}
	}
	decisions, err := a.authorization.AuthorizeBatch(ctx, actorID, authorizationRequests)
	if err != nil {
		return nil, err
	}
	result := make([]service.AIResourceAuthorizationDecision, len(decisions))
	for index, decision := range decisions {
		result[index] = service.AIResourceAuthorizationDecision{Allowed: decision.Allowed}
	}
	return result, nil
}

type aiResourceSessionCreatorAdapter struct {
	sessions *service.UserSessionCreationService
}

func (a aiResourceSessionCreatorAdapter) GetOrCreateAIResourceSession(
	ctx context.Context,
	actorID string,
) (service.AIResourceSession, error) {
	session, err := a.sessions.GetOrCreateActivePermanentUserSession(ctx, actorID)
	if err != nil {
		return service.AIResourceSession{}, err
	}
	return service.AIResourceSession{ID: session.SessionID, Seq: session.SessionSeq}, nil
}
