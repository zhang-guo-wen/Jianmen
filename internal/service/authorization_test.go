package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/storage"
)

var (
	_ AuthorizationActionAuthorizer   = (*rbac.Checker)(nil)
	_ AuthorizationResourceAuthorizer = (*rbac.ResourceGrantChecker)(nil)
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

func (f *fakeActionAuthorizer) BatchActionDecisionsContext(
	_ context.Context,
	_ string,
	requests []rbac.BatchAuthorizationRequest,
) ([]rbac.BatchActionDecision, error) {
	result := make([]rbac.BatchActionDecision, len(requests))
	if f.err != nil {
		return nil, f.err
	}
	for index, request := range requests {
		for _, action := range request.Actions {
			if !f.allowed[action] {
				continue
			}
			result[index].ActionAllowed = true
			if f.denied[action] {
				result[index].Denied = true
				continue
			}
			result[index].Allowed = true
		}
		if result[index].Allowed {
			result[index].Denied = false
		}
	}
	return result, nil
}

type fakeResourceAuthorizer struct {
	allowed bool
	err     error
	ctxErr  error
	calls   int
}

type fakeBatchActionAuthorizer struct {
	calls     int
	decisions []rbac.BatchActionDecision
	err       error
}

func (*fakeBatchActionAuthorizer) HasPermissionContext(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}
func (*fakeBatchActionAuthorizer) HasDenyContext(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

func (f *fakeBatchActionAuthorizer) BatchActionDecisionsContext(_ context.Context, _ string, requests []rbac.BatchAuthorizationRequest) ([]rbac.BatchActionDecision, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.decisions != nil {
		return f.decisions, nil
	}
	result := make([]rbac.BatchActionDecision, len(requests))
	for index := range requests {
		result[index] = rbac.BatchActionDecision{ActionAllowed: true, Allowed: true}
	}
	return result, nil
}

type fakeBatchResourceAuthorizer struct {
	calls     int
	decisions []bool
	err       error
}

func (*fakeBatchResourceAuthorizer) HasGrantContext(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (f *fakeBatchResourceAuthorizer) BatchGrantsContext(_ context.Context, _ string, requests []rbac.BatchAuthorizationRequest) ([]bool, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.decisions != nil {
		return f.decisions, nil
	}
	result := make([]bool, len(requests))
	for index := range requests {
		result[index] = true
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

func (f *fakeResourceAuthorizer) BatchGrantsContext(
	_ context.Context,
	_ string,
	requests []rbac.BatchAuthorizationRequest,
) ([]bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	result := make([]bool, len(requests))
	for index := range result {
		result[index] = f.allowed
	}
	return result, nil
}

func newAuthorizationTestService(
	t *testing.T,
	identity AuthorizationIdentity,
	actions AuthorizationActionAuthorizer,
	resources AuthorizationResourceAuthorizer,
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
	if err := db.Create(&model.Host{
		ID:      "host-authorization",
		Name:    "authorization",
		Address: "authorization.example",
		Port:    22,
		Status:  "active",
	}).Error; err != nil {
		t.Fatalf("create authorization host: %v", err)
	}
	for _, account := range []model.HostAccount{
		{
			ID:         "account-denied",
			HostID:     "host-authorization",
			Username:   "denied",
			ResourceID: "d001",
			Status:     "active",
		},
		{
			ID:         "account-visible",
			HostID:     "host-authorization",
			Username:   "visible",
			ResourceID: "v001",
			Status:     "active",
		},
	} {
		if err := db.Create(&account).Error; err != nil {
			t.Fatalf("create authorization account %s: %v", account.ID, err)
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
	if err := db.Create(&model.ResourceGrant{ID: "grant-visible", PrincipalType: "user", PrincipalID: user.ID, ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-visible", Effect: model.PermissionEffectAllow}).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		request AuthorizationRequest
		reason  string
		allowed bool
	}{
		{name: "allowed", request: AuthorizationRequest{UserID: user.ID, Actions: []string{rbac.ActionSessionConnect}, ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-visible"}, reason: AuthorizationReasonAllowed, allowed: true},
		{name: "action denied", request: AuthorizationRequest{UserID: user.ID, Actions: []string{"missing:action"}, ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-visible"}, reason: AuthorizationReasonActionDenied},
		{name: "resource denied", request: AuthorizationRequest{UserID: user.ID, Actions: []string{rbac.ActionSessionConnect}, ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-denied"}, reason: AuthorizationReasonResourceDenied},
		{name: "missing grant", request: AuthorizationRequest{UserID: user.ID, Actions: []string{rbac.ActionSessionConnect}, ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-missing"}, reason: AuthorizationReasonResourceDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			single, err := authorizer.Authorize(context.Background(), test.request)
			if err != nil {
				t.Fatal(err)
			}
			batch, err := authorizer.AuthorizeBatch(context.Background(), user.ID, []AuthorizationRequest{test.request})
			if err != nil {
				t.Fatal(err)
			}
			if len(batch) != 1 || batch[0] != single || batch[0].Allowed != test.allowed || batch[0].Reason != test.reason {
				t.Fatalf("single=%#v batch=%#v", single, batch)
			}
		})
	}
}

func TestNewAuthorizationServiceRejectsNilDependencies(t *testing.T) {
	identity := &fakeAuthorizationIdentity{}
	actions := &fakeActionAuthorizer{}
	resources := &fakeResourceAuthorizer{}
	tests := []struct {
		name      string
		identity  AuthorizationIdentity
		actions   AuthorizationActionAuthorizer
		resources AuthorizationResourceAuthorizer
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

func TestNewAuthorizationServiceRejectsTypedNilDependencies(t *testing.T) {
	var identity *fakeAuthorizationIdentity
	var actions *fakeActionAuthorizer
	var resources *fakeResourceAuthorizer
	if _, err := NewAuthorizationService(identity, &fakeActionAuthorizer{}, &fakeResourceAuthorizer{}); err == nil {
		t.Fatal("typed-nil identity was accepted")
	}
	if _, err := NewAuthorizationService(&fakeAuthorizationIdentity{}, actions, &fakeResourceAuthorizer{}); err == nil {
		t.Fatal("typed-nil action authorizer was accepted")
	}
	if _, err := NewAuthorizationService(&fakeAuthorizationIdentity{}, &fakeActionAuthorizer{}, resources); err == nil {
		t.Fatal("typed-nil resource authorizer was accepted")
	}
}

func TestNewAuthorizationServiceRequiresBatchCapabilitiesInStaticContract(t *testing.T) {
	constructor := reflect.TypeOf(NewAuthorizationService)
	for _, test := range []struct {
		name     string
		argument int
		methods  []string
	}{
		{
			name:     "action authorizer",
			argument: 1,
			methods:  []string{"HasPermissionContext", "HasDenyContext", "BatchActionDecisionsContext"},
		},
		{
			name:     "resource authorizer",
			argument: 2,
			methods:  []string{"HasGrantContext", "BatchGrantsContext"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			contract := constructor.In(test.argument)
			for _, method := range test.methods {
				if _, ok := contract.MethodByName(method); !ok {
					t.Fatalf("constructor argument %d does not statically require %s", test.argument, method)
				}
			}
		})
	}
}

func TestAuthorizationServiceBatchPreservesDecisionClassificationAndRejectsMismatchedResults(t *testing.T) {
	identity := &fakeAuthorizationIdentity{subject: IdentitySubject{ID: "u1"}, found: true}
	actions := &fakeBatchActionAuthorizer{decisions: []rbac.BatchActionDecision{{ActionAllowed: false}, {ActionAllowed: true, Allowed: false, Denied: true}, {ActionAllowed: true, Allowed: true}}}
	resources := &fakeBatchResourceAuthorizer{decisions: []bool{true, true, false}}
	authorizer, err := NewAuthorizationService(identity, actions, resources)
	if err != nil {
		t.Fatal(err)
	}
	requests := []AuthorizationRequest{{Actions: []string{"view"}, ResourceType: "host", ResourceID: "a"}, {Actions: []string{"view"}, ResourceType: "host", ResourceID: "b"}, {Actions: []string{"view"}, ResourceType: "host", ResourceID: "c"}}
	got, err := authorizer.AuthorizeBatch(context.Background(), "u1", requests)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{AuthorizationReasonActionDenied, AuthorizationReasonResourceDenied, AuthorizationReasonResourceDenied}
	for index := range want {
		if got[index].Reason != want[index] || got[index].Allowed {
			t.Fatalf("decision[%d]=%#v want %s", index, got[index], want[index])
		}
	}
	actions.decisions = actions.decisions[:2]
	if _, err := authorizer.AuthorizeBatch(context.Background(), "u1", requests); err == nil {
		t.Fatal("mismatched batch decision length was accepted")
	}
}

func TestAuthorizationServiceBatchChecksContextAfterIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	identity := &fakeAuthorizationIdentity{subject: IdentitySubject{ID: "admin", SuperAdmin: true}, found: true}
	identity.err = nil
	identityCancel := AuthorizationIdentity(identityAfterContext{identity: identity, cancel: cancel})
	authorizer, err := NewAuthorizationService(identityCancel, &fakeBatchActionAuthorizer{}, &fakeBatchResourceAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizer.AuthorizeBatch(ctx, "admin", []AuthorizationRequest{{Actions: []string{"view"}, ResourceType: "host", ResourceID: "h1"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context canceled", err)
	}
}

func TestAuthorizationServiceBatchActionOnlyDenyMatchesSingleForAlternativeActions(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	user := model.User{ID: "u-batch-equivalent", Username: "batch-equivalent", Status: "active"}
	role := model.Role{ID: "r-batch-equivalent", Name: "batch-equivalent", Status: "active"}
	for _, record := range []any{&user, &role, &model.UserRole{ID: "ur-batch-equivalent", UserID: user.ID, RoleID: role.ID}} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("create %T: %v", record, err)
		}
	}
	for _, host := range []model.Host{
		{
			ID:      "h1",
			Name:    "h1",
			Address: "h1.example",
			Port:    22,
			Status:  "active",
		},
		{
			ID:      "h2",
			Name:    "h2",
			Address: "h2.example",
			Port:    22,
			Status:  "active",
		},
	} {
		if err := db.Create(&host).Error; err != nil {
			t.Fatalf("create authorization host %s: %v", host.ID, err)
		}
	}
	permissions := []model.Permission{
		{ID: "allow-view-equivalent", Action: "resource:view", Effect: model.PermissionEffectAllow},
		{ID: "deny-view-equivalent", Action: "resource:view", Effect: model.PermissionEffectDeny},
		{ID: "allow-connect-equivalent", Action: "resource:connect", Effect: model.PermissionEffectAllow},
		{ID: "deny-connect-h1-equivalent", Action: "resource:connect", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectDeny},
	}
	for index := range permissions {
		if err := db.Create(&permissions[index]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.RolePermission{ID: "rp-" + permissions[index].ID, RoleID: role.ID, PermissionID: permissions[index].ID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"h1", "h2"} {
		if err := db.Create(&model.ResourceGrant{ID: "grant-" + id, PrincipalType: "user", PrincipalID: user.ID, ResourceType: model.ResourceTypeHost, ResourceID: id, Effect: model.PermissionEffectAllow}).Error; err != nil {
			t.Fatal(err)
		}
	}
	authorizer := newAuthorizationTestService(
		t,
		&fakeAuthorizationIdentity{subject: IdentitySubject{ID: user.ID, Username: user.Username, Status: user.Status}, found: true},
		rbac.NewChecker(db),
		rbac.NewResourceGrantChecker(db),
	)
	requests := []AuthorizationRequest{
		{UserID: user.ID, Actions: []string{"resource:view"}, ResourceType: model.ResourceTypeHost, ResourceID: "h1"},
		{UserID: user.ID, Actions: []string{"resource:view", "resource:connect"}, ResourceType: model.ResourceTypeHost, ResourceID: "h1"},
		{UserID: user.ID, Actions: []string{"resource:view", "resource:connect"}, ResourceType: model.ResourceTypeHost, ResourceID: "h2"},
		{UserID: user.ID, Actions: []string{"resource:view", "missing"}, ResourceType: model.ResourceTypeHost, ResourceID: "h2"},
	}
	batch, err := authorizer.AuthorizeBatch(context.Background(), user.ID, requests)
	if err != nil {
		t.Fatal(err)
	}
	wantReasons := []string{
		AuthorizationReasonActionDenied,
		AuthorizationReasonResourceDenied,
		AuthorizationReasonAllowed,
		AuthorizationReasonActionDenied,
	}
	for index, request := range requests {
		single, err := authorizer.Authorize(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if batch[index] != single {
			t.Fatalf("request[%d] single=%#v batch=%#v", index, single, batch[index])
		}
		if batch[index].Reason != wantReasons[index] {
			t.Fatalf("request[%d] reason=%s want=%s", index, batch[index].Reason, wantReasons[index])
		}
	}
}

type identityAfterContext struct {
	identity *fakeAuthorizationIdentity
	cancel   context.CancelFunc
}

func (i identityAfterContext) FindIdentitySubject(ctx context.Context, userID string) (IdentitySubject, bool, error) {
	subject, found, err := i.identity.FindIdentitySubject(ctx, userID)
	i.cancel()
	return subject, found, err
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
