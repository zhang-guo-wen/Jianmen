package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

type connectionPasswordContextKey struct{}

type connectionPasswordRepositoryStub struct {
	hostAccount      model.HostAccount
	hostAccountFound bool
	hostAccountErr   error
	host             model.Host
	hostFound        bool
	hostErr          error
	databaseAccount  model.DatabaseAccount
	databaseFound    bool
	databaseErr      error
	createErr        error
	created          model.ConnectionPassword
	createCalls      int
	contexts         []context.Context
}

func (s *connectionPasswordRepositoryStub) FindActiveHostAccount(
	ctx context.Context,
	_ string,
) (model.HostAccount, bool, error) {
	s.contexts = append(s.contexts, ctx)
	return s.hostAccount, s.hostAccountFound, s.hostAccountErr
}

func (s *connectionPasswordRepositoryStub) FindActiveHost(
	ctx context.Context,
	_ string,
) (model.Host, bool, error) {
	s.contexts = append(s.contexts, ctx)
	return s.host, s.hostFound, s.hostErr
}

func (s *connectionPasswordRepositoryStub) FindActiveDatabaseAccount(
	ctx context.Context,
	_ string,
) (model.DatabaseAccount, bool, error) {
	s.contexts = append(s.contexts, ctx)
	return s.databaseAccount, s.databaseFound, s.databaseErr
}

func (s *connectionPasswordRepositoryStub) CreateConnectionPassword(
	ctx context.Context,
	credential model.ConnectionPassword,
) error {
	s.contexts = append(s.contexts, ctx)
	s.createCalls++
	s.created = credential
	return s.createErr
}

type connectionPasswordAuthorizerStub struct {
	allowed      bool
	err          error
	ctx          context.Context
	userID       string
	actions      []string
	resourceType string
	resourceID   string
	calls        int
}

func (s *connectionPasswordAuthorizerStub) AuthorizeConnection(
	ctx context.Context,
	userID string,
	actions []string,
	resourceType string,
	resourceID string,
) (bool, error) {
	s.calls++
	s.ctx = ctx
	s.userID = userID
	s.actions = append([]string(nil), actions...)
	s.resourceType = resourceType
	s.resourceID = resourceID
	return s.allowed, s.err
}

func TestConnectionPasswordServiceIssuesResourceBoundCredential(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 19, 9, 30, 0, 0, time.UTC)
	tests := []struct {
		name             string
		repository       *connectionPasswordRepositoryStub
		wantResourceType string
		wantActions      []string
	}{
		{
			name: "host account",
			repository: &connectionPasswordRepositoryStub{
				hostAccount:      model.HostAccount{ID: "host-account", HostID: "host-1"},
				hostAccountFound: true,
				host:             model.Host{ID: "host-1", Status: "active"},
				hostFound:        true,
			},
			wantResourceType: model.ResourceTypeHostAccount,
			wantActions:      []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect},
		},
		{
			name: "database account",
			repository: &connectionPasswordRepositoryStub{
				databaseAccount: model.DatabaseAccount{
					ID: "database-account",
					Instance: model.DatabaseInstance{
						ID:     "database-1",
						Status: "active",
					},
				},
				databaseFound: true,
			},
			wantResourceType: model.ResourceTypeDatabaseAccount,
			wantActions:      []string{rbac.ActionDBConnect},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &connectionPasswordAuthorizerStub{allowed: true}
			service, err := NewConnectionPasswordService(test.repository, authorizer)
			if err != nil {
				t.Fatalf("new connection password service: %v", err)
			}
			service.now = func() time.Time { return fixedNow }
			ctx := context.WithValue(
				context.Background(),
				connectionPasswordContextKey{},
				"request-context",
			)

			result, err := service.Issue(ctx, ConnectionPasswordIssueRequest{
				UserID: " user-1 ", TargetID: " target-1 ",
			})
			if err != nil {
				t.Fatalf("issue connection password: %v", err)
			}

			if result.Password == "" ||
				result.ExpiresInSeconds != int(defaultConnectionPasswordTTL.Seconds()) ||
				!result.Reusable {
				t.Fatalf("unexpected issue result: %#v", result)
			}
			wantExpiry := fixedNow.Add(defaultConnectionPasswordTTL)
			if !result.ExpiresAt.Equal(wantExpiry) {
				t.Fatalf("expiry = %v, want %v", result.ExpiresAt, wantExpiry)
			}
			created := test.repository.created
			if created.UserID != "user-1" ||
				created.ResourceType != test.wantResourceType ||
				created.ResourceID != "target-1" {
				t.Fatalf("created credential binding = %#v", created)
			}
			if created.SecretHash == result.Password ||
				bcrypt.CompareHashAndPassword(
					[]byte(created.SecretHash),
					[]byte(result.Password),
				) != nil {
				t.Fatal("stored credential does not contain only the password hash")
			}
			if created.MySQLNativeHash == "" || !created.ExpiresAt.Equal(wantExpiry) {
				t.Fatalf("created credential metadata = %#v", created)
			}
			if authorizer.userID != "user-1" ||
				authorizer.resourceType != test.wantResourceType ||
				authorizer.resourceID != "target-1" ||
				!slices.Equal(authorizer.actions, test.wantActions) {
				t.Fatalf("authorization request = %#v", authorizer)
			}
			if authorizer.ctx.Value(connectionPasswordContextKey{}) != "request-context" {
				t.Fatal("authorizer did not receive the request context")
			}
			for _, repositoryContext := range test.repository.contexts {
				if repositoryContext.Value(connectionPasswordContextKey{}) != "request-context" {
					t.Fatal("repository did not receive the request context")
				}
			}
		})
	}
}

func TestConnectionPasswordServiceFailsClosedBeforePersistence(t *testing.T) {
	lookupFailure := errors.New("lookup unavailable")
	authorizationFailure := errors.New("authorization unavailable")
	tests := []struct {
		name                 string
		repository           *connectionPasswordRepositoryStub
		authorizer           *connectionPasswordAuthorizerStub
		expectedResourceType string
		want                 error
	}{
		{
			name: "host parent inactive",
			repository: &connectionPasswordRepositoryStub{
				hostAccount:      model.HostAccount{ID: "host-account", HostID: "host-1"},
				hostAccountFound: true,
			},
			authorizer: &connectionPasswordAuthorizerStub{allowed: true},
			want:       ErrConnectionPasswordTargetNotFound,
		},
		{
			name: "host account expired",
			repository: &connectionPasswordRepositoryStub{
				hostAccount: model.HostAccount{
					ID:        "host-account",
					HostID:    "host-1",
					ExpiresAt: timePointer(time.Now().UTC().Add(-time.Minute)),
				},
				hostAccountFound: true,
				host:             model.Host{ID: "host-1", Status: "active"},
				hostFound:        true,
			},
			authorizer: &connectionPasswordAuthorizerStub{allowed: true},
			want:       ErrConnectionPasswordTargetNotFound,
		},
		{
			name: "database parent inactive",
			repository: &connectionPasswordRepositoryStub{
				databaseAccount: model.DatabaseAccount{
					ID:       "database-account",
					Instance: model.DatabaseInstance{ID: "database-1", Status: "disabled"},
				},
				databaseFound: true,
			},
			authorizer: &connectionPasswordAuthorizerStub{allowed: true},
			want:       ErrConnectionPasswordTargetNotFound,
		},
		{
			name: "target lookup failure",
			repository: &connectionPasswordRepositoryStub{
				hostAccountErr: lookupFailure,
			},
			authorizer: &connectionPasswordAuthorizerStub{allowed: true},
			want:       ErrConnectionPasswordTargetLookup,
		},
		{
			name: "route resource type mismatch",
			repository: &connectionPasswordRepositoryStub{
				databaseAccount: model.DatabaseAccount{
					ID:       "database-account",
					Instance: model.DatabaseInstance{ID: "database-1", Status: "active"},
				},
				databaseFound: true,
			},
			authorizer:           &connectionPasswordAuthorizerStub{allowed: true},
			expectedResourceType: model.ResourceTypeHostAccount,
			want:                 ErrConnectionPasswordTargetNotFound,
		},
		{
			name: "authorization denied",
			repository: &connectionPasswordRepositoryStub{
				databaseAccount: model.DatabaseAccount{
					ID:       "database-account",
					Instance: model.DatabaseInstance{ID: "database-1", Status: "active"},
				},
				databaseFound: true,
			},
			authorizer: &connectionPasswordAuthorizerStub{},
			want:       ErrConnectionPasswordForbidden,
		},
		{
			name: "authorization failure",
			repository: &connectionPasswordRepositoryStub{
				databaseAccount: model.DatabaseAccount{
					ID:       "database-account",
					Instance: model.DatabaseInstance{ID: "database-1", Status: "active"},
				},
				databaseFound: true,
			},
			authorizer: &connectionPasswordAuthorizerStub{err: authorizationFailure},
			want:       ErrConnectionPasswordAuthorization,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewConnectionPasswordService(test.repository, test.authorizer)
			if err != nil {
				t.Fatalf("new connection password service: %v", err)
			}
			_, err = service.Issue(context.Background(), ConnectionPasswordIssueRequest{
				UserID:               "user-1",
				TargetID:             "target-1",
				ExpectedResourceType: test.expectedResourceType,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("issue error = %v, want %v", err, test.want)
			}
			if test.repository.createCalls != 0 {
				t.Fatalf("persistence calls = %d, want 0", test.repository.createCalls)
			}
		})
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func TestConnectionPasswordServicePropagatesCancellationAndPersistenceFailure(t *testing.T) {
	repository := &connectionPasswordRepositoryStub{
		databaseAccount: model.DatabaseAccount{
			ID:       "database-account",
			Instance: model.DatabaseInstance{ID: "database-1", Status: "active"},
		},
		databaseFound: true,
		createErr:     errors.New("storage unavailable"),
	}
	authorizer := &connectionPasswordAuthorizerStub{allowed: true}
	service, err := NewConnectionPasswordService(repository, authorizer)
	if err != nil {
		t.Fatalf("new connection password service: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Issue(ctx, ConnectionPasswordIssueRequest{
		UserID: "user-1", TargetID: "target-1",
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled issue error = %v, want context canceled", err)
	}
	if len(repository.contexts) != 0 || authorizer.calls != 0 {
		t.Fatal("canceled issue reached a dependency")
	}

	if _, err := service.Issue(context.Background(), ConnectionPasswordIssueRequest{
		UserID: "user-1", TargetID: "target-1",
	}); !errors.Is(err, ErrConnectionPasswordPersistence) {
		t.Fatalf("persistence error = %v, want %v", err, ErrConnectionPasswordPersistence)
	}
}

func TestNewConnectionPasswordServiceRejectsNilDependencies(t *testing.T) {
	repository := &connectionPasswordRepositoryStub{}
	authorizer := &connectionPasswordAuthorizerStub{}
	var nilRepository *connectionPasswordRepositoryStub
	var nilAuthorizer *connectionPasswordAuthorizerStub

	tests := []struct {
		name       string
		repository ConnectionPasswordRepository
		authorizer ConnectionPasswordAuthorizer
	}{
		{name: "nil repository", authorizer: authorizer},
		{name: "typed nil repository", repository: nilRepository, authorizer: authorizer},
		{name: "nil authorizer", repository: repository},
		{name: "typed nil authorizer", repository: repository, authorizer: nilAuthorizer},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewConnectionPasswordService(
				test.repository,
				test.authorizer,
			); err == nil {
				t.Fatal("constructor accepted a nil dependency")
			}
		})
	}
}
