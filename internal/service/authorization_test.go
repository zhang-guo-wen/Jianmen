package service

import (
	"context"
	"errors"
	"testing"
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
	err     error
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

type fakeResourceAuthorizer struct {
	allowed bool
	err     error
	ctxErr  error
	calls   int
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

func TestAuthorizationServicePropagatesCancelledContextToEveryChecker(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	identity := &fakeAuthorizationIdentity{
		subject: IdentitySubject{ID: "u1", Status: "active"},
		found:   true,
	}
	actions := &fakeActionAuthorizer{allowed: map[string]bool{"session:connect": true}}
	resources := &fakeResourceAuthorizer{allowed: true}
	authorizer := newAuthorizationTestService(t, identity, actions, resources)

	_, _ = authorizer.Authorize(cancelled, AuthorizationRequest{
		UserID:       "u1",
		Actions:      []string{"session:connect"},
		ResourceType: "host_account",
		ResourceID:   "account-1",
	})
	if !errors.Is(identity.ctxErr, context.Canceled) {
		t.Fatalf("identity context error = %v", identity.ctxErr)
	}
	if !errors.Is(actions.ctxErr, context.Canceled) {
		t.Fatalf("action context error = %v", actions.ctxErr)
	}
	if !errors.Is(resources.ctxErr, context.Canceled) {
		t.Fatalf("resource context error = %v", resources.ctxErr)
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
