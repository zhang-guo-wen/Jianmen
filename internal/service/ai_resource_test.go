package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/util"
)

type aiResourceRepositoryStub struct {
	hosts            []AIHostAccountMetadata
	databases        []AIDatabaseAccountMetadata
	listHostErr      error
	listDatabaseErr  error
	hostErr          error
	databaseErr      error
	listHostCalls    int
	listDBCalls      int
	hostCalls        int
	databaseCalls    int
	receivedContexts []context.Context
}

func (r *aiResourceRepositoryStub) ListHostAccounts(ctx context.Context) ([]AIHostAccountMetadata, error) {
	r.listHostCalls++
	r.receivedContexts = append(r.receivedContexts, ctx)
	return append([]AIHostAccountMetadata(nil), r.hosts...), r.listHostErr
}

func (r *aiResourceRepositoryStub) HostAccount(ctx context.Context, id string) (AIHostAccountMetadata, error) {
	r.hostCalls++
	r.receivedContexts = append(r.receivedContexts, ctx)
	if r.hostErr != nil {
		return AIHostAccountMetadata{}, r.hostErr
	}
	for _, account := range r.hosts {
		if account.ID == id {
			return account, nil
		}
	}
	return AIHostAccountMetadata{}, ErrAIResourceNotFound
}

func (r *aiResourceRepositoryStub) ListDatabaseAccounts(ctx context.Context) ([]AIDatabaseAccountMetadata, error) {
	r.listDBCalls++
	r.receivedContexts = append(r.receivedContexts, ctx)
	return append([]AIDatabaseAccountMetadata(nil), r.databases...), r.listDatabaseErr
}

func (r *aiResourceRepositoryStub) DatabaseAccount(ctx context.Context, id string) (AIDatabaseAccountMetadata, error) {
	r.databaseCalls++
	r.receivedContexts = append(r.receivedContexts, ctx)
	if r.databaseErr != nil {
		return AIDatabaseAccountMetadata{}, r.databaseErr
	}
	for _, account := range r.databases {
		if account.ID == id {
			return account, nil
		}
	}
	return AIDatabaseAccountMetadata{}, ErrAIResourceNotFound
}

type aiResourceAuthorizerStub struct {
	allowed          map[string]bool
	err              error
	decisions        []AIResourceAuthorizationDecision
	calls            int
	actorID          string
	requests         []AIResourceAuthorizationRequest
	receivedContexts []context.Context
}

func (a *aiResourceAuthorizerStub) AuthorizeAIResources(
	ctx context.Context,
	actorID string,
	requests []AIResourceAuthorizationRequest,
) ([]AIResourceAuthorizationDecision, error) {
	a.calls++
	a.actorID = actorID
	a.receivedContexts = append(a.receivedContexts, ctx)
	a.requests = append([]AIResourceAuthorizationRequest(nil), requests...)
	if a.err != nil {
		return nil, a.err
	}
	if a.decisions != nil {
		return append([]AIResourceAuthorizationDecision(nil), a.decisions...), nil
	}
	decisions := make([]AIResourceAuthorizationDecision, len(requests))
	for index, request := range requests {
		decisions[index].Allowed = a.allowed[request.ResourceType+"/"+request.ResourceID]
	}
	return decisions, nil
}

type aiResourceSessionCreatorStub struct {
	session          AIResourceSession
	err              error
	calls            int
	actorID          string
	receivedContexts []context.Context
}

func (s *aiResourceSessionCreatorStub) GetOrCreateAIResourceSession(
	ctx context.Context,
	actorID string,
) (AIResourceSession, error) {
	s.calls++
	s.actorID = actorID
	s.receivedContexts = append(s.receivedContexts, ctx)
	return s.session, s.err
}

func TestAIResourceListFiltersUnauthorizedAndUnavailableResources(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	repository := &aiResourceRepositoryStub{
		hosts: []AIHostAccountMetadata{
			{ID: "host-visible", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "active", ResourceID: "H001"},
			{ID: "host-other-user", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "active", ResourceID: "H002"},
			{ID: "host-disabled", Protocol: "ssh", Status: "disabled", LifecycleStatus: "disabled", ParentStatus: "active"},
			{ID: "host-expired", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "active", ExpiresAt: expired.Format(time.RFC3339Nano)},
			{ID: "host-parent-disabled", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "disabled"},
		},
		databases: []AIDatabaseAccountMetadata{
			{ID: "db-visible", Status: "active", ParentStatus: "active", ResourceID: "D001", ExpiresAt: &future},
			{ID: "db-other-user", Status: "active", ParentStatus: "active", ResourceID: "D002"},
			{ID: "db-disabled", Status: "disabled", ParentStatus: "active"},
			{ID: "db-expired", Status: "active", ParentStatus: "active", ExpiresAt: &expired},
			{ID: "db-parent-disabled", Status: "active", ParentStatus: "disabled"},
		},
	}
	authorizer := &aiResourceAuthorizerStub{allowed: map[string]bool{
		model.ResourceTypeHostAccount + "/host-visible":           true,
		model.ResourceTypeDatabaseAccount + "/db-visible":         true,
		model.ResourceTypeHostAccount + "/host-disabled":          true,
		model.ResourceTypeDatabaseAccount + "/db-parent-disabled": true,
	}}
	sessions := &aiResourceSessionCreatorStub{}
	service := newAIResourceTestService(t, repository, authorizer, sessions, now)

	resources, err := service.List(context.Background(), "actor-a")
	if err != nil {
		t.Fatalf("list AI resources: %v", err)
	}
	if got := resourceKeys(resources); !reflect.DeepEqual(got, []string{
		model.ResourceTypeHostAccount + "/host-visible",
		model.ResourceTypeDatabaseAccount + "/db-visible",
	}) {
		t.Fatalf("visible resources = %#v", got)
	}
	if repository.listHostCalls != 1 || repository.listDBCalls != 1 {
		t.Fatalf("repository list calls = host %d, database %d", repository.listHostCalls, repository.listDBCalls)
	}
	if authorizer.calls != 1 || authorizer.actorID != "actor-a" {
		t.Fatalf("authorization calls = %d, actor = %q", authorizer.calls, authorizer.actorID)
	}
	if got := authorizationRequestKeys(authorizer.requests); !reflect.DeepEqual(got, []string{
		model.ResourceTypeHostAccount + "/host-visible",
		model.ResourceTypeHostAccount + "/host-other-user",
		model.ResourceTypeDatabaseAccount + "/db-visible",
		model.ResourceTypeDatabaseAccount + "/db-other-user",
	}) {
		t.Fatalf("batched authorization requests = %#v", got)
	}
}

func TestAIResourceGetHidesUnauthorizedAndUnsupportedResources(t *testing.T) {
	repository := &aiResourceRepositoryStub{
		hosts: []AIHostAccountMetadata{{ID: "host-a", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "active"}},
	}
	authorizer := &aiResourceAuthorizerStub{allowed: map[string]bool{}}
	service := newAIResourceTestService(t, repository, authorizer, &aiResourceSessionCreatorStub{}, time.Now())

	_, unauthorizedErr := service.Get(context.Background(), "actor-b", model.ResourceTypeHostAccount, "host-a")
	_, unsupportedErr := service.Get(context.Background(), "actor-b", "unsupported", "host-a")
	if !errors.Is(unauthorizedErr, ErrAIResourceNotFound) || !errors.Is(unsupportedErr, ErrAIResourceNotFound) {
		t.Fatalf("hidden errors = unauthorized %v, unsupported %v", unauthorizedErr, unsupportedErr)
	}
	if repository.hostCalls != 0 {
		t.Fatalf("unauthorized resource reached repository %d times", repository.hostCalls)
	}
}

func TestAIResourceUnavailableAccountsCannotGetOrCreateSession(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Second)
	tests := []struct {
		name         string
		resourceType string
		resourceID   string
		host         AIHostAccountMetadata
		database     AIDatabaseAccountMetadata
	}{
		{name: "disabled host account", resourceType: model.ResourceTypeHostAccount, resourceID: "host-disabled", host: AIHostAccountMetadata{ID: "host-disabled", Protocol: "ssh", Status: "disabled", LifecycleStatus: "disabled", ParentStatus: "active"}},
		{name: "expired host account", resourceType: model.ResourceTypeHostAccount, resourceID: "host-expired", host: AIHostAccountMetadata{ID: "host-expired", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "active", ExpiresAt: expired.Format(time.RFC3339Nano)}},
		{name: "disabled host", resourceType: model.ResourceTypeHostAccount, resourceID: "host-parent", host: AIHostAccountMetadata{ID: "host-parent", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "disabled"}},
		{name: "disabled database account", resourceType: model.ResourceTypeDatabaseAccount, resourceID: "db-disabled", database: AIDatabaseAccountMetadata{ID: "db-disabled", Status: "disabled", ParentStatus: "active"}},
		{name: "expired database account", resourceType: model.ResourceTypeDatabaseAccount, resourceID: "db-expired", database: AIDatabaseAccountMetadata{ID: "db-expired", Status: "active", ParentStatus: "active", ExpiresAt: &expired}},
		{name: "disabled database instance", resourceType: model.ResourceTypeDatabaseAccount, resourceID: "db-parent", database: AIDatabaseAccountMetadata{ID: "db-parent", Status: "active", ParentStatus: "disabled"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &aiResourceRepositoryStub{}
			if test.host.ID != "" {
				repository.hosts = []AIHostAccountMetadata{test.host}
			}
			if test.database.ID != "" {
				repository.databases = []AIDatabaseAccountMetadata{test.database}
			}
			authorizer := &aiResourceAuthorizerStub{allowed: map[string]bool{
				test.resourceType + "/" + test.resourceID: true,
			}}
			sessions := &aiResourceSessionCreatorStub{session: AIResourceSession{ID: "abc12", Seq: 1}}
			service := newAIResourceTestService(t, repository, authorizer, sessions, now)

			if _, err := service.Get(context.Background(), "actor", test.resourceType, test.resourceID); !errors.Is(err, ErrAIResourceNotFound) {
				t.Fatalf("get error = %v, want not found", err)
			}
			if _, err := service.CreateSession(context.Background(), "actor", test.resourceType, test.resourceID); !errors.Is(err, ErrAIResourceNotFound) {
				t.Fatalf("create session error = %v, want not found", err)
			}
			if sessions.calls != 0 {
				t.Fatalf("session creator called %d times for unavailable resource", sessions.calls)
			}
		})
	}
}

func TestAIResourceSessionPreservesHostDatabaseAndRedisSemantics(t *testing.T) {
	repository := &aiResourceRepositoryStub{
		hosts: []AIHostAccountMetadata{{
			ID: "host", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "active", ResourceID: "H001",
		}},
		databases: []AIDatabaseAccountMetadata{
			{ID: "database", Status: "active", ParentStatus: "active", ResourceID: "D001", ParentProtocol: "mysql"},
			{ID: "redis", Status: "active", ParentStatus: "active", ResourceID: "D002", ParentProtocol: "redis"},
		},
	}
	authorizer := &aiResourceAuthorizerStub{allowed: map[string]bool{
		model.ResourceTypeHostAccount + "/host":         true,
		model.ResourceTypeDatabaseAccount + "/database": true,
		model.ResourceTypeDatabaseAccount + "/redis":    true,
	}}
	sessions := &aiResourceSessionCreatorStub{session: AIResourceSession{ID: "abc12", Seq: 9}}
	service := newAIResourceTestService(t, repository, authorizer, sessions, time.Now())
	tests := []struct {
		resourceType string
		resourceID   string
		wantUsername string
	}{
		{model.ResourceTypeHostAccount, "host", util.PrefixHost + "H001abc12"},
		{model.ResourceTypeDatabaseAccount, "database", util.PrefixDatabase + "D001abc12"},
		{model.ResourceTypeDatabaseAccount, "redis", util.PrefixRedis + "D002abc12"},
	}
	for _, test := range tests {
		result, err := service.CreateSession(context.Background(), "actor", test.resourceType, test.resourceID)
		if err != nil {
			t.Fatalf("create %s session: %v", test.resourceID, err)
		}
		if result.CompactUsername != test.wantUsername || result.SessionID != "abc12" || result.SessionSeq != 9 {
			t.Fatalf("%s session = %#v, want username %q", test.resourceID, result, test.wantUsername)
		}
	}
}

func TestAIResourceMetadataTypesExcludeCredentialFields(t *testing.T) {
	for _, metadataType := range []reflect.Type{
		reflect.TypeOf(AIHostAccountMetadata{}),
		reflect.TypeOf(AIDatabaseAccountMetadata{}),
	} {
		for index := 0; index < metadataType.NumField(); index++ {
			name := strings.ToLower(metadataType.Field(index).Name)
			for _, forbidden := range []string{"password", "privatekey", "passphrase", "secret"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("%s exposes forbidden field %q", metadataType.Name(), metadataType.Field(index).Name)
				}
			}
		}
	}
}

func newAIResourceTestService(
	t *testing.T,
	repository AIResourceRepository,
	authorizer AIResourceAuthorizer,
	sessions AIResourceSessionCreator,
	now time.Time,
) *AIResourceService {
	t.Helper()
	service, err := NewAIResourceService(repository, authorizer, sessions)
	if err != nil {
		t.Fatalf("new AI resource service: %v", err)
	}
	service.now = func() time.Time { return now }
	return service
}

func resourceKeys(resources []AIResource) []string {
	keys := make([]string, len(resources))
	for index, resource := range resources {
		keys[index] = resource.Type + "/" + resource.ID
	}
	return keys
}

func authorizationRequestKeys(requests []AIResourceAuthorizationRequest) []string {
	keys := make([]string, len(requests))
	for index, request := range requests {
		keys[index] = request.ResourceType + "/" + request.ResourceID
	}
	return keys
}
