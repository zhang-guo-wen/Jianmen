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
	sessionFound     bool
	created          model.UserSession
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
func (s *userSessionCreationRepositoryStub) FindActivePermanentUserSession(ctx context.Context, _ string) (model.UserSession, bool, error) {
	s.remember(ctx)
	return s.session, s.sessionFound, s.err
}
func (s *userSessionCreationRepositoryStub) CreateUserSessionWithContext(ctx context.Context, session model.UserSession) (*model.UserSession, error) {
	s.remember(ctx)
	s.created = session
	session.ID, session.SessionID, session.SessionSeq = "session-1", "00001", 1
	return &session, s.err
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
	if repository.created.UserID != "user-1" || repository.created.Type != "permanent" || repository.created.Status != "active" {
		t.Fatalf("created = %#v", repository.created)
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
	repository := &userSessionCreationRepositoryStub{database: model.DatabaseAccount{ID: "db-1", ResourceID: "D001", Instance: model.DatabaseInstance{ID: "instance-1", Protocol: "redis", Status: "active"}}, databaseFound: true, session: model.UserSession{ID: "existing", SessionID: "00007", SessionSeq: 7}, sessionFound: true}
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
