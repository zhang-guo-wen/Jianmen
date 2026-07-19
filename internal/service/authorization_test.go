package service

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/storage"
)

type fakeAuthorizationIdentity struct {
	subject IdentitySubject
	found   bool
	err     error
	ctxErr  error
	calls   int
}

func (f *fakeAuthorizationIdentity) FindIdentitySubject(ctx context.Context, _ string) (IdentitySubject, bool, error) {
	f.calls++
	f.ctxErr = ctx.Err()
	if f.err != nil {
		return IdentitySubject{}, false, f.err
	}
	return f.subject, f.found, nil
}

type fakeActionAuthorizer struct {
	allowed map[string]bool
	denied  map[string]bool
	err     error
	denyErr error
	ctxErr  error
	calls   []string
}

func (f *fakeActionAuthorizer) HasPermissionContext(
	ctx context.Context,
	_ string,
	action string,
	_ string,
	_ string,
) (bool, error) {
	f.calls = append(f.calls, action)
	f.ctxErr = ctx.Err()
	if f.err != nil {
		return false, f.err
	}
	return f.allowed[action], nil
}

func (f *fakeActionAuthorizer) HasDenyContext(
	ctx context.Context,
	_ string,
	action string,
	_ string,
	_ string,
) (bool, error) {
	f.ctxErr = ctx.Err()
	if f.denyErr != nil {
		return false, f.denyErr
	}
	return f.denied[action], nil
}

type fakeResourceAuthorizer struct {
	allowed bool
	err     error
	ctxErr  error
	calls   int
}

type fakeBatchActionAuthorizer struct{ calls int }

func (*fakeBatchActionAuthorizer) HasPermissionContext(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}
func (*fakeBatchActionAuthorizer) HasDenyContext(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

func (f *fakeBatchActionAuthorizer) BatchActionDecisionsContext(_ context.Context, _ string, requests []rbac.BatchAuthorizationRequest) (map[string]rbac.BatchActionDecision, error) {
	f.calls++
	result := make(map[string]rbac.BatchActionDecision, len(requests))
	for _, request := range requests {
		result[rbac.BatchResourceKey(request.ResourceType, request.ResourceID)] = rbac.BatchActionDecision{Allowed: true}
	}
	return result, nil
}

type fakeBatchResourceAuthorizer struct{ calls int }

func (*fakeBatchResourceAuthorizer) HasGrantContext(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (f *fakeBatchResourceAuthorizer) BatchGrantsContext(_ context.Context, _ string, requests []rbac.BatchAuthorizationRequest) (map[string]bool, error) {
	f.calls++
	result := make(map[string]bool, len(requests))
	for _, request := range requests {
		result[rbac.BatchResourceKey(request.ResourceType, request.ResourceID)] = true
	}
	return result, nil
}

func (f *fakeResourceAuthorizer) HasGrantContext(
	ctx context.Context,
	_ string,
	_ string,
	_ string,
) (bool, error) {
	f.calls++
	f.ctxErr = ctx.Err()
	if f.err != nil {
		return false, f.err
	}
	return f.allowed, nil
}

func newAuthorizationTestService(
	t *testing.T,
	identity AuthorizationIdentity,
	actions ActionAuthorizer,
	resources ResourceAuthorizer,
) *AuthorizationService {
	t.Helper()
	authorizer, err := NewAuthorizationService(identity, actions, resources)
	if err != nil {
		t.Fatalf("new authorization service: %v", err)
	}
	return authorizer
}

func TestAuthorizationServiceDeniesEmptyUser(t *testing.T) {
	identity := &fakeAuthorizationIdentity{}
	authorizer := newAuthorizationTestService(
		t,
		identity,
		&fakeActionAuthorizer{},
		&fakeResourceAuthorizer{},
	)

	decision, err := authorizer.Authorize(context.Background(), AuthorizationRequest{
		Actions: []string{"session:connect"},
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if decision.Allowed || decision.Reason != AuthorizationReasonMissingUser {
		t.Fatalf("decision = %#v", decision)
	}
	if identity.calls != 0 {
		t.Fatalf("identity calls = %d, want 0", identity.calls)
	}
}

func TestAuthorizationServiceAllowsActiveSuperAdministrator(t *testing.T) {
	identity := &fakeAuthorizationIdentity{
		subject: IdentitySubject{ID: "u-admin", Status: "active", SuperAdmin: true},
		found:   true,
	}
	actions := &fakeActionAuthorizer{}
	resources := &fakeResourceAuthorizer{}
	authorizer := newAuthorizationTestService(t, identity, actions, resources)

	decision, err := authorizer.Authorize(context.Background(), AuthorizationRequest{
		UserID:       "u-admin",
		Actions:      []string{"session:connect"},
		ResourceType: "host_account",
		ResourceID:   "account-1",
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !decision.Allowed || decision.Reason != AuthorizationReasonSuperAdmin {
		t.Fatalf("decision = %#v", decision)
	}
	if len(actions.calls) != 0 || resources.calls != 0 {
		t.Fatalf("super administrator reached checkers: action=%v resource=%d", actions.calls, resources.calls)
	}
}

func TestAuthorizationServiceBatchSuperAdminSkipsRBAC(t *testing.T) {
	actions := &fakeBatchActionAuthorizer{}
	resources := &fakeBatchResourceAuthorizer{}
	authorizer, err := NewAuthorizationService(&fakeAuthorizationIdentity{subject: IdentitySubject{ID: "admin", SuperAdmin: true}, found: true}, actions, resources)
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := authorizer.AuthorizeBatch(context.Background(), "admin", []AuthorizationRequest{{Actions: []string{"host:view"}, ResourceType: "host", ResourceID: "h1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || !decisions[0].Allowed || actions.calls != 0 || resources.calls != 0 {
		t.Fatalf("unexpected batch superadmin result=%#v action=%d resource=%d", decisions, actions.calls, resources.calls)
	}
}

func TestAuthorizationServiceRequiresOneAllowedAction(t *testing.T) {
	identity := &fakeAuthorizationIdentity{
		subject: IdentitySubject{ID: "u1", Status: "active"},
		found:   true,
	}
	actions := &fakeActionAuthorizer{
		allowed: map[string]bool{"sftp:connect": true},
	}
	authorizer := newAuthorizationTestService(t, identity, actions, &fakeResourceAuthorizer{})

	decision, err := authorizer.Authorize(context.Background(), AuthorizationRequest{
		UserID:  "u1",
		Actions: []string{"session:connect", "sftp:connect"},
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !decision.Allowed || decision.Reason != AuthorizationReasonAllowed {
		t.Fatalf("decision = %#v", decision)
	}
	if len(actions.calls) != 2 {
		t.Fatalf("action calls = %v", actions.calls)
	}
}

func TestAuthorizationServiceRequiresConcreteResourceGrant(t *testing.T) {
	identity := &fakeAuthorizationIdentity{
		subject: IdentitySubject{ID: "u1", Status: "active"},
		found:   true,
	}
	actions := &fakeActionAuthorizer{allowed: map[string]bool{"session:connect": true}}
	resources := &fakeResourceAuthorizer{allowed: true}
	authorizer := newAuthorizationTestService(t, identity, actions, resources)

	decision, err := authorizer.Authorize(context.Background(), AuthorizationRequest{
		UserID:       "u1",
		Actions:      []string{"session:connect"},
		ResourceType: "host_account",
		ResourceID:   "account-1",
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !decision.Allowed || resources.calls != 1 {
		t.Fatalf("decision = %#v resource calls=%d", decision, resources.calls)
	}
}

func TestAuthorizationServicePreservesDenyDecision(t *testing.T) {
	identity := &fakeAuthorizationIdentity{
		subject: IdentitySubject{ID: "u1", Status: "active"},
		found:   true,
	}
	tests := []struct {
		name      string
		actions   *fakeActionAuthorizer
		resources *fakeResourceAuthorizer
		request   AuthorizationRequest
		reason    string
	}{
		{
			name:      "action denied",
			actions:   &fakeActionAuthorizer{allowed: map[string]bool{}},
			resources: &fakeResourceAuthorizer{allowed: true},
			request:   AuthorizationRequest{UserID: "u1", Actions: []string{"session:connect"}},
			reason:    AuthorizationReasonActionDenied,
		},
		{
			name:      "resource denied",
			actions:   &fakeActionAuthorizer{allowed: map[string]bool{"session:connect": true}},
			resources: &fakeResourceAuthorizer{},
			request: AuthorizationRequest{
				UserID:       "u1",
				Actions:      []string{"session:connect"},
				ResourceType: "host_account",
				ResourceID:   "account-1",
			},
			reason: AuthorizationReasonResourceDenied,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := newAuthorizationTestService(t, identity, test.actions, test.resources)
			decision, err := authorizer.Authorize(context.Background(), test.request)
			if err != nil {
				t.Fatalf("authorize: %v", err)
			}
			if decision.Allowed || decision.Reason != test.reason {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}

func TestAuthorizationServicePropagatesCheckerErrors(t *testing.T) {
	identityErr := errors.New("identity failed")
	actionErr := errors.New("action failed")
	resourceErr := errors.New("resource failed")
	tests := []struct {
		name      string
		identity  *fakeAuthorizationIdentity
		actions   *fakeActionAuthorizer
		resources *fakeResourceAuthorizer
		request   AuthorizationRequest
		want      error
	}{
		{
			name:      "identity",
			identity:  &fakeAuthorizationIdentity{err: identityErr},
			actions:   &fakeActionAuthorizer{},
			resources: &fakeResourceAuthorizer{},
			request:   AuthorizationRequest{UserID: "u1", Actions: []string{"session:connect"}},
			want:      identityErr,
		},
		{
			name: "action",
			identity: &fakeAuthorizationIdentity{
				subject: IdentitySubject{ID: "u1", Status: "active"},
				found:   true,
			},
			actions:   &fakeActionAuthorizer{err: actionErr},
			resources: &fakeResourceAuthorizer{},
			request:   AuthorizationRequest{UserID: "u1", Actions: []string{"session:connect"}},
			want:      actionErr,
		},
		{
			name: "resource",
			identity: &fakeAuthorizationIdentity{
				subject: IdentitySubject{ID: "u1", Status: "active"},
				found:   true,
			},
			actions:   &fakeActionAuthorizer{allowed: map[string]bool{"session:connect": true}},
			resources: &fakeResourceAuthorizer{err: resourceErr},
			request: AuthorizationRequest{
				UserID:       "u1",
				Actions:      []string{"session:connect"},
				ResourceType: "host_account",
				ResourceID:   "account-1",
			},
			want: resourceErr,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := newAuthorizationTestService(t, test.identity, test.actions, test.resources)
			_, err := authorizer.Authorize(context.Background(), test.request)
			if !errors.Is(err, test.want) {
				t.Fatalf("authorize error = %v, want wrapped %v", err, test.want)
			}
		})
	}
}

func TestAuthorizationServiceFailsClosedForCancelledContext(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	identity := &fakeAuthorizationIdentity{
		subject: IdentitySubject{ID: "u1", Status: "active"},
		found:   true,
	}
	actions := &fakeActionAuthorizer{allowed: map[string]bool{"session:connect": true}}
	resources := &fakeResourceAuthorizer{allowed: true}
	authorizer := newAuthorizationTestService(t, identity, actions, resources)

	decision, err := authorizer.Authorize(cancelled, AuthorizationRequest{
		UserID:       "u1",
		Actions:      []string{"session:connect"},
		ResourceType: "host_account",
		ResourceID:   "account-1",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("authorize error = %v, want context canceled", err)
	}
	if decision.Allowed {
		t.Fatalf("cancelled authorization decision = %#v", decision)
	}
}

func TestAuthorizationServiceResourcePermissionDenyOverridesResourceGrantAllow(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	user := model.User{ID: "u-denied", Username: "denied", Status: "active"}
	role := model.Role{ID: "r-denied", Name: "denied-role", Status: "active"}
	permissions := []model.Permission{
		{
			ID:     "p-global-allow",
			Action: rbac.ActionSessionConnect,
			Effect: model.PermissionEffectAllow,
		},
		{
			ID:           "p-resource-deny",
			Action:       rbac.ActionSessionConnect,
			ResourceType: model.ResourceTypeHostAccount,
			ResourceID:   "account-denied",
			Effect:       model.PermissionEffectDeny,
		},
	}
	for _, record := range []any{&user, &role} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("create %T: %v", record, err)
		}
	}
	if err := db.Create(&model.UserRole{ID: "ur-denied", UserID: user.ID, RoleID: role.ID}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}
	for i := range permissions {
		if err := db.Create(&permissions[i]).Error; err != nil {
			t.Fatalf("create permission: %v", err)
		}
		if err := db.Create(&model.RolePermission{
			ID:           "rp-" + permissions[i].ID,
			RoleID:       role.ID,
			PermissionID: permissions[i].ID,
		}).Error; err != nil {
			t.Fatalf("create role permission: %v", err)
		}
	}
	if err := db.Create(&model.ResourceGrant{
		ID:            "grant-allow",
		PrincipalType: "user",
		PrincipalID:   user.ID,
		ResourceType:  model.ResourceTypeHostAccount,
		ResourceID:    "account-denied",
		Effect:        model.PermissionEffectAllow,
	}).Error; err != nil {
		t.Fatalf("create resource grant: %v", err)
	}
	identity := &fakeAuthorizationIdentity{
		subject: IdentitySubject{ID: user.ID, Username: user.Username, Status: user.Status},
		found:   true,
	}
	authorizer := newAuthorizationTestService(
		t,
		identity,
		rbac.NewChecker(db),
		rbac.NewResourceGrantChecker(db),
	)

	decision, err := authorizer.Authorize(context.Background(), AuthorizationRequest{
		UserID:       user.ID,
		Actions:      []string{rbac.ActionSessionConnect},
		ResourceType: model.ResourceTypeHostAccount,
		ResourceID:   "account-denied",
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("resource permission deny was bypassed: %#v", decision)
	}
}

func TestNewAuthorizationServiceRejectsNilDependencies(t *testing.T) {
	identity := &fakeAuthorizationIdentity{}
	actions := &fakeActionAuthorizer{}
	resources := &fakeResourceAuthorizer{}
	tests := []struct {
		name      string
		identity  AuthorizationIdentity
		actions   ActionAuthorizer
		resources ResourceAuthorizer
	}{
		{name: "identity", actions: actions, resources: resources},
		{name: "actions", identity: identity, resources: resources},
		{name: "resources", identity: identity, actions: actions},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAuthorizationService(test.identity, test.actions, test.resources); err == nil {
				t.Fatal("expected nil dependency error")
			}
		})
	}
}

func TestAuthorizationServiceAuthorizeConnectionUsesUnifiedDecision(t *testing.T) {
	identity := &fakeAuthorizationIdentity{
		subject: IdentitySubject{ID: "u1", Status: "active"},
		found:   true,
	}
	actions := &fakeActionAuthorizer{allowed: map[string]bool{"db:connect": true}}
	resources := &fakeResourceAuthorizer{allowed: true}
	authorizer := newAuthorizationTestService(t, identity, actions, resources)

	allowed, err := authorizer.AuthorizeConnection(
		context.Background(),
		"u1",
		[]string{"db:connect"},
		"database_account",
		"account-1",
	)
	if err != nil {
		t.Fatalf("authorize connection: %v", err)
	}
	if !allowed {
		t.Fatal("unified connection decision was denied")
	}
}
