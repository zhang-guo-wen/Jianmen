package service

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/util"
)

type userSessionCreationRepositoryStub struct {
	hostAccount      model.HostAccount
	hostAccountFound bool
	hostFound        bool
	host             model.Host
	database         model.DatabaseAccount
	databaseFound    bool
	session          model.UserSession
	createdUserID    string
	allocationCalls  int
	contexts         []context.Context
	err              error
}

func (s *userSessionCreationRepositoryStub) remember(ctx context.Context) {
	s.contexts = append(s.contexts, ctx)
}
func (s *userSessionCreationRepositoryStub) FindActiveHostAccount(ctx context.Context, _ string) (model.HostAccount, bool, error) {
	s.remember(ctx)
	return s.hostAccount, s.hostAccountFound, s.err
}
func (s *userSessionCreationRepositoryStub) FindActiveHost(ctx context.Context, _ string) (model.Host, bool, error) {
	s.remember(ctx)
	return s.host, s.hostFound, s.err
}
func (s *userSessionCreationRepositoryStub) FindActiveDatabaseAccount(ctx context.Context, _ string) (model.DatabaseAccount, bool, error) {
	s.remember(ctx)
	return s.database, s.databaseFound, s.err
}
func (s *userSessionCreationRepositoryStub) GetOrCreateActivePermanentUserSession(ctx context.Context, userID string) (model.UserSession, error) {
	s.remember(ctx)
	s.createdUserID = userID
	s.allocationCalls++
	if s.session.ID == "" {
		s.session = model.UserSession{ID: "session-1", UserID: userID, SessionID: "00001", SessionSeq: 1, Type: "permanent", Status: "active"}
	}
	return s.session, s.err
}

type userSessionCreationAuthorizerStub struct {
	allowed      bool
	err          error
	actions      []string
	resourceType string
	resourceID   string
	ctx          context.Context
}

func (s *userSessionCreationAuthorizerStub) AuthorizeConnection(ctx context.Context, _ string, actions []string, resourceType, resourceID string) (bool, error) {
	s.ctx, s.actions, s.resourceType, s.resourceID = ctx, actions, resourceType, resourceID
	return s.allowed, s.err
}

func TestUserSessionCreationServiceExposesSharedAtomicAllocationPath(t *testing.T) {
	repository := &userSessionCreationRepositoryStub{
		session: model.UserSession{
			ID: "shared-session", UserID: "user-1", SessionID: "00009",
			SessionSeq: 9, Type: "permanent", Status: "active",
		},
	}
	creation, err := NewUserSessionCreationService(repository, &userSessionCreationAuthorizerStub{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.WithValue(context.Background(), userSessionCreationContextKey{}, "ai-request")
	session, err := creation.GetOrCreateActivePermanentUserSession(ctx, " user-1 ")
	if err != nil {
		t.Fatalf("get or create permanent session: %v", err)
	}
	if session.ID != "shared-session" || session.Type != "permanent" || session.Status != "active" {
		t.Fatalf("session = %#v", session)
	}
	if repository.createdUserID != "user-1" || repository.allocationCalls != 1 {
		t.Fatalf("atomic allocation user=%q calls=%d", repository.createdUserID, repository.allocationCalls)
	}
	if len(repository.contexts) != 1 || repository.contexts[0] != ctx {
		t.Fatal("shared allocation path did not preserve request context")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := creation.GetOrCreateActivePermanentUserSession(canceled, "user-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled allocation error = %v, want context canceled", err)
	}
	if repository.allocationCalls != 1 {
		t.Fatal("canceled allocation reached repository")
	}
}

func TestUserSessionCreationServiceCreatesHostSessionAndPropagatesContext(t *testing.T) {
	repository := &userSessionCreationRepositoryStub{
		hostAccount: model.HostAccount{ID: "target-1", HostID: "host-1", ResourceID: "H001"}, hostAccountFound: true, hostFound: true,
		host: model.Host{ID: "host-1", Status: "active"},
	}
	authorizer := &userSessionCreationAuthorizerStub{allowed: true}
	service, err := NewUserSessionCreationService(repository, authorizer)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.WithValue(context.Background(), userSessionCreationContextKey{}, "trace")
	result, err := service.Create(ctx, CreateUserSessionRequest{UserID: "user-1", TargetID: "target-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.ResourceType != model.ResourceTypeHostAccount || result.ResourceID != "H001" || result.CompactUsername != util.PrefixHost+"H00100001" {
		t.Fatalf("result = %#v", result)
	}
	if want := []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}; len(authorizer.actions) != len(want) || authorizer.actions[0] != want[0] || authorizer.actions[1] != want[1] {
		t.Fatalf("actions = %#v, want %#v", authorizer.actions, want)
	}
	if authorizer.resourceID != "target-1" || authorizer.resourceType != model.ResourceTypeHostAccount {
		t.Fatalf("authorization resource = %s/%s", authorizer.resourceType, authorizer.resourceID)
	}
	if repository.createdUserID != "user-1" {
		t.Fatalf("repository user id = %q, want user-1", repository.createdUserID)
	}
	if authorizer.ctx != ctx {
		t.Fatal("authorizer did not receive request context")
	}
	for _, got := range repository.contexts {
		if got != ctx {
			t.Fatal("repository did not receive request context")
		}
	}
}

func TestUserSessionCreationServiceDatabaseRedisAndFailClosed(t *testing.T) {
	repository := &userSessionCreationRepositoryStub{database: model.DatabaseAccount{ID: "db-1", ResourceID: "D001", Instance: model.DatabaseInstance{ID: "instance-1", Protocol: "redis", Status: "active"}}, databaseFound: true, session: model.UserSession{ID: "existing", SessionID: "00007", SessionSeq: 7}}
	authorizer := &userSessionCreationAuthorizerStub{allowed: false}
	service, err := NewUserSessionCreationService(repository, authorizer)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.Create(context.Background(), CreateUserSessionRequest{UserID: "user", TargetID: "db-1"}); !errors.Is(err, ErrUserSessionForbidden) {
		t.Fatalf("denied error = %v", err)
	}
	if len(repository.contexts) != 2 {
		t.Fatalf("repository calls = %d, want target lookups only", len(repository.contexts))
	}
	authorizer.allowed = true
	result, err := service.Create(context.Background(), CreateUserSessionRequest{UserID: "user", TargetID: "db-1"})
	if err != nil {
		t.Fatalf("create database session: %v", err)
	}
	if result.ResourceType != model.ResourceTypeDatabaseAccount || result.CompactUsername != util.PrefixRedis+"D00100007" {
		t.Fatalf("database result = %#v", result)
	}
	if len(authorizer.actions) != 1 || authorizer.actions[0] != rbac.ActionDBConnect {
		t.Fatalf("database actions = %#v", authorizer.actions)
	}
}

func TestNewUserSessionCreationServiceRejectsTypedNilDependencies(t *testing.T) {
	var repository *userSessionCreationRepositoryStub
	if _, err := NewUserSessionCreationService(repository, &userSessionCreationAuthorizerStub{}); err == nil {
		t.Fatal("typed-nil repository was accepted")
	}
	var authorizer *userSessionCreationAuthorizerStub
	if _, err := NewUserSessionCreationService(&userSessionCreationRepositoryStub{}, authorizer); err == nil {
		t.Fatal("typed-nil authorizer was accepted")
	}
}

func TestUserSessionCreationServiceRejectsInactiveParentsAndCanceledContext(t *testing.T) {
	tests := []struct {
		name       string
		repository *userSessionCreationRepositoryStub
		want       error
	}{
		{"inactive-host", &userSessionCreationRepositoryStub{hostAccount: model.HostAccount{HostID: "host"}, hostAccountFound: true}, ErrUserSessionHostInactive},
		{"inactive-database", &userSessionCreationRepositoryStub{database: model.DatabaseAccount{Instance: model.DatabaseInstance{ID: "instance", Status: "disabled"}}, databaseFound: true}, ErrUserSessionDatabaseInactive},
		{"missing-target", &userSessionCreationRepositoryStub{}, ErrUserSessionTargetNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewUserSessionCreationService(test.repository, &userSessionCreationAuthorizerStub{allowed: true})
			if err != nil {
				t.Fatalf("new service: %v", err)
			}
			if _, err := service.Create(context.Background(), CreateUserSessionRequest{UserID: "user", TargetID: "target"}); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service, err := NewUserSessionCreationService(&userSessionCreationRepositoryStub{}, &userSessionCreationAuthorizerStub{allowed: true})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.Create(ctx, CreateUserSessionRequest{UserID: "user", TargetID: "target"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}

type userSessionCreationContextKey struct{}
