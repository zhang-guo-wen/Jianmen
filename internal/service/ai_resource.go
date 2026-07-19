package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/util"
)

var ErrAIResourceNotFound = errors.New("ai resource not found")

type AIResource struct {
	ID           string
	Type         string
	Name         string
	Group        string
	Remark       string
	Address      string
	Port         int
	Protocol     string
	Username     string
	ResourceID   string
	ResourceSeq  int
	Status       string
	ExpiresAt    string
	Capabilities []string
}

// AIHostAccountMetadata is deliberately credential-free.
type AIHostAccountMetadata struct {
	ID              string
	HostID          string
	Name            string
	Group           string
	Remark          string
	Address         string
	Port            int
	Protocol        string
	Username        string
	ResourceID      string
	ResourceSeq     int
	Status          string
	LifecycleStatus string
	ExpiresAt       string
	ParentStatus    string
}

// AIDatabaseAccountMetadata is deliberately credential-free and includes only
// the parent metadata needed to enforce resource lifecycle.
type AIDatabaseAccountMetadata struct {
	ID             string
	InstanceID     string
	Name           string
	Group          string
	Remark         string
	Username       string
	ResourceID     string
	ResourceSeq    int
	Status         string
	ExpiresAt      *time.Time
	ParentAddress  string
	ParentPort     int
	ParentProtocol string
	ParentStatus   string
}

type AIResourceRepository interface {
	ListHostAccounts(context.Context) ([]AIHostAccountMetadata, error)
	HostAccount(context.Context, string) (AIHostAccountMetadata, error)
	ListDatabaseAccounts(context.Context) ([]AIDatabaseAccountMetadata, error)
	DatabaseAccount(context.Context, string) (AIDatabaseAccountMetadata, error)
}

type AIResourceAuthorizationRequest struct {
	Actions      []string
	ResourceType string
	ResourceID   string
}

type AIResourceAuthorizationDecision struct {
	Allowed bool
}

type AIResourceAuthorizer interface {
	AuthorizeAIResources(context.Context, string, []AIResourceAuthorizationRequest) ([]AIResourceAuthorizationDecision, error)
}

type AIResourceSession struct {
	ID  string
	Seq int
}

type AIResourceSessionCreator interface {
	GetOrCreateAIResourceSession(context.Context, string) (AIResourceSession, error)
}

type AIResourceSessionResult struct {
	Resource        AIResource
	SessionID       string
	SessionSeq      int
	CompactUsername string
}

type AIResourceService struct {
	repository AIResourceRepository
	authorizer AIResourceAuthorizer
	sessions   AIResourceSessionCreator
	now        func() time.Time
}

func NewAIResourceService(
	repository AIResourceRepository,
	authorizer AIResourceAuthorizer,
	sessions AIResourceSessionCreator,
) (*AIResourceService, error) {
	switch {
	case isNilAIResourceDependency(repository):
		return nil, errors.New("ai resource repository is required")
	case isNilAIResourceDependency(authorizer):
		return nil, errors.New("ai resource authorizer is required")
	case isNilAIResourceDependency(sessions):
		return nil, errors.New("ai resource session creator is required")
	default:
		return &AIResourceService{
			repository: repository,
			authorizer: authorizer,
			sessions:   sessions,
			now:        time.Now,
		}, nil
	}
}

func (s *AIResourceService) List(ctx context.Context, actorID string) ([]AIResource, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list AI resources context: %w", err)
	}
	hostAccounts, err := s.repository.ListHostAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AI host accounts: %w", err)
	}
	databaseAccounts, err := s.repository.ListDatabaseAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AI database accounts: %w", err)
	}

	now := s.now().UTC()
	candidates := make([]AIResource, 0, len(hostAccounts)+len(databaseAccounts))
	requests := make([]AIResourceAuthorizationRequest, 0, cap(candidates))
	for _, account := range hostAccounts {
		if !hostAccountAvailable(account, now) {
			continue
		}
		candidates = append(candidates, hostResource(account))
		requests = append(requests, authorizationRequest(model.ResourceTypeHostAccount, account.ID))
	}
	for _, account := range databaseAccounts {
		if !databaseAccountAvailable(account, now) {
			continue
		}
		candidates = append(candidates, databaseResource(account))
		requests = append(requests, authorizationRequest(model.ResourceTypeDatabaseAccount, account.ID))
	}
	if len(candidates) == 0 {
		return []AIResource{}, nil
	}

	decisions, err := s.authorizer.AuthorizeAIResources(ctx, strings.TrimSpace(actorID), requests)
	if err != nil {
		return nil, fmt.Errorf("authorize AI resource list: %w", err)
	}
	if len(decisions) != len(candidates) {
		return nil, fmt.Errorf("authorize AI resource list: decision count %d does not match request count %d", len(decisions), len(candidates))
	}
	resources := make([]AIResource, 0, len(candidates))
	for index, decision := range decisions {
		if decision.Allowed {
			resources = append(resources, candidates[index])
		}
	}
	return resources, nil
}

func (s *AIResourceService) Get(ctx context.Context, actorID, resourceType, resourceID string) (AIResource, error) {
	if err := ctx.Err(); err != nil {
		return AIResource{}, fmt.Errorf("get AI resource context: %w", err)
	}
	return s.authorizedResource(ctx, actorID, resourceType, resourceID)
}

func (s *AIResourceService) CreateSession(
	ctx context.Context,
	actorID string,
	resourceType string,
	resourceID string,
) (AIResourceSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return AIResourceSessionResult{}, fmt.Errorf("create AI resource session context: %w", err)
	}
	resource, err := s.authorizedResource(ctx, actorID, resourceType, resourceID)
	if err != nil {
		return AIResourceSessionResult{}, err
	}
	session, err := s.sessions.GetOrCreateAIResourceSession(ctx, strings.TrimSpace(actorID))
	if err != nil {
		return AIResourceSessionResult{}, fmt.Errorf("create AI resource session: %w", err)
	}
	prefix := util.PrefixHost
	if resource.Type == model.ResourceTypeDatabaseAccount {
		prefix = util.PrefixDatabase
		if strings.EqualFold(resource.Protocol, "redis") {
			prefix = util.PrefixRedis
		}
	}
	return AIResourceSessionResult{
		Resource:        resource,
		SessionID:       session.ID,
		SessionSeq:      session.Seq,
		CompactUsername: prefix + resource.ResourceID + session.ID,
	}, nil
}

func (s *AIResourceService) authorizedResource(
	ctx context.Context,
	actorID string,
	resourceType string,
	resourceID string,
) (AIResource, error) {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if !supportedAIResourceType(resourceType) || resourceID == "" {
		return AIResource{}, ErrAIResourceNotFound
	}
	decisions, err := s.authorizer.AuthorizeAIResources(ctx, strings.TrimSpace(actorID), []AIResourceAuthorizationRequest{
		authorizationRequest(resourceType, resourceID),
	})
	if err != nil {
		return AIResource{}, fmt.Errorf("authorize AI resource: %w", err)
	}
	if len(decisions) != 1 {
		return AIResource{}, fmt.Errorf("authorize AI resource: decision count %d does not match request count 1", len(decisions))
	}
	if !decisions[0].Allowed {
		return AIResource{}, ErrAIResourceNotFound
	}

	now := s.now().UTC()
	switch resourceType {
	case model.ResourceTypeHostAccount:
		account, err := s.repository.HostAccount(ctx, resourceID)
		if err != nil {
			return AIResource{}, fmt.Errorf("get AI host account: %w", err)
		}
		if !hostAccountAvailable(account, now) {
			return AIResource{}, ErrAIResourceNotFound
		}
		return hostResource(account), nil
	case model.ResourceTypeDatabaseAccount:
		account, err := s.repository.DatabaseAccount(ctx, resourceID)
		if err != nil {
			return AIResource{}, fmt.Errorf("get AI database account: %w", err)
		}
		if !databaseAccountAvailable(account, now) {
			return AIResource{}, ErrAIResourceNotFound
		}
		return databaseResource(account), nil
	default:
		return AIResource{}, ErrAIResourceNotFound
	}
}

func authorizationRequest(resourceType, resourceID string) AIResourceAuthorizationRequest {
	actions := []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}
	if resourceType == model.ResourceTypeDatabaseAccount {
		actions = []string{rbac.ActionDBConnect}
	}
	return AIResourceAuthorizationRequest{
		Actions:      actions,
		ResourceType: resourceType,
		ResourceID:   resourceID,
	}
}

func supportedAIResourceType(resourceType string) bool {
	return resourceType == model.ResourceTypeHostAccount || resourceType == model.ResourceTypeDatabaseAccount
}

func hostAccountAvailable(account AIHostAccountMetadata, now time.Time) bool {
	return account.Protocol == "ssh" &&
		activeAIResourceStatus(account.LifecycleStatus) &&
		activeAIResourceStatus(account.ParentStatus) &&
		unexpiredAIResourceTime(account.ExpiresAt, now)
}

func databaseAccountAvailable(account AIDatabaseAccountMetadata, now time.Time) bool {
	return activeAIResourceStatus(account.Status) &&
		activeAIResourceStatus(account.ParentStatus) &&
		(account.ExpiresAt == nil || now.Before(account.ExpiresAt.UTC()))
}

func activeAIResourceStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "active" || status == "enabled"
}

func unexpiredAIResourceTime(value string, now time.Time) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && now.Before(expiresAt.UTC())
}

func hostResource(account AIHostAccountMetadata) AIResource {
	return AIResource{
		ID: account.ID, Type: model.ResourceTypeHostAccount,
		Name: account.Name, Group: account.Group, Remark: account.Remark,
		Address: account.Address, Port: account.Port, Username: account.Username,
		ResourceID: account.ResourceID, ResourceSeq: account.ResourceSeq,
		Status: account.Status, ExpiresAt: account.ExpiresAt,
		Capabilities: []string{"ssh", "sftp", "temporary_password"},
	}
}

func databaseResource(account AIDatabaseAccountMetadata) AIResource {
	expiresAt := ""
	if account.ExpiresAt != nil {
		expiresAt = account.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return AIResource{
		ID: account.ID, Type: model.ResourceTypeDatabaseAccount,
		Name: account.Name, Group: account.Group, Remark: account.Remark,
		Address: account.ParentAddress, Port: account.ParentPort, Protocol: account.ParentProtocol,
		Username: account.Username, ResourceID: account.ResourceID, ResourceSeq: account.ResourceSeq,
		Status: account.Status, ExpiresAt: expiresAt,
		Capabilities: []string{"database", "temporary_password"},
	}
}

func isNilAIResourceDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
