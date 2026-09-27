package dbproxy

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func TestProtocolAuthenticationPropagatesConnectionAuthorization(t *testing.T) {
	type protocolCase struct {
		name string
		run  func(*Gateway, context.Context, *resolvedDBAccount) error
		auth func(*captureDatabaseAccountResolver) []context.Context
	}
	protocols := []protocolCase{
		{
			name: "mysql",
			run: func(gateway *Gateway, ctx context.Context, resolved *resolvedDBAccount) error {
				return gateway.authenticateMySQLConnection(ctx, resolved, []byte("salt"), []byte("response"))
			},
			auth: func(resolver *captureDatabaseAccountResolver) []context.Context {
				return resolver.mysqlContexts
			},
		},
		{
			name: "postgres",
			run: func(gateway *Gateway, ctx context.Context, resolved *resolvedDBAccount) error {
				return gateway.authenticatePostgresConnection(ctx, resolved, "password")
			},
			auth: func(resolver *captureDatabaseAccountResolver) []context.Context {
				return resolver.passwordContexts
			},
		},
		{
			name: "redis",
			run: func(gateway *Gateway, ctx context.Context, resolved *resolvedDBAccount) error {
				return gateway.authenticateRedisConnection(ctx, resolved, "password")
			},
			auth: func(resolver *captureDatabaseAccountResolver) []context.Context {
				return resolver.passwordContexts
			},
		},
	}
	authorizationErr := errors.New("authorization unavailable")
	outcomes := []struct {
		name      string
		allowed   bool
		err       error
		wantError bool
	}{
		{name: "allow", allowed: true},
		{name: "deny", wantError: true},
		{name: "error", err: authorizationErr, wantError: true},
	}

	for _, protocol := range protocols {
		for _, outcome := range outcomes {
			t.Run(protocol.name+"/"+outcome.name, func(t *testing.T) {
				ctx := context.WithValue(context.Background(), resolveContextKey{}, protocol.name)
				resolver := &captureDatabaseAccountResolver{}
				authorizer := &captureConnectionAuthorizer{allowed: outcome.allowed, err: outcome.err}
				gateway := &Gateway{store: resolver, authorizer: authorizer}
				resolved := &resolvedDBAccount{
					account: &model.DatabaseAccount{ID: "db-account-1"},
					user:    &model.User{ID: "user-1"},
				}

				err := protocol.run(gateway, ctx, resolved)
				if (err != nil) != outcome.wantError {
					t.Fatalf("protocol authentication error = %v, wantError %v", err, outcome.wantError)
				}
				if outcome.err != nil && !errors.Is(err, outcome.err) {
					t.Fatalf("protocol authentication error = %v, want wrapped %v", err, outcome.err)
				}

				authContexts := protocol.auth(resolver)
				if len(authContexts) != 1 || authContexts[0] != ctx {
					t.Fatalf("authentication contexts = %#v, want original connection context", authContexts)
				}
				if len(authorizer.calls) != 1 {
					t.Fatalf("authorization calls = %d, want 1", len(authorizer.calls))
				}
				call := authorizer.calls[0]
				if call.ctx != ctx ||
					call.userID != "user-1" ||
					len(call.actions) != 1 ||
					call.actions[0] != rbac.ActionDBConnect ||
					call.resourceType != model.ResourceTypeDatabaseAccount ||
					call.resourceID != "db-account-1" {
					t.Fatalf("unexpected authorization call: %#v", call)
				}
			})
		}
	}
}

func TestAuthenticateAndAuthorizeConnectionFailsClosedAfterAuthenticationError(t *testing.T) {
	ctx := context.WithValue(context.Background(), resolveContextKey{}, "helper")
	authenticationErr := errors.New("password backend unavailable")
	authorizer := &captureConnectionAuthorizer{allowed: true}
	gateway := &Gateway{authorizer: authorizer}
	var authenticationCtx context.Context

	err := gateway.authenticateAndAuthorizeConnection(ctx, "user-1", "db-account-1", func(ctx context.Context) error {
		authenticationCtx = ctx
		return authenticationErr
	})
	if !errors.Is(err, authenticationErr) {
		t.Fatalf("helper error = %v, want wrapped authentication error", err)
	}
	if authenticationCtx != ctx {
		t.Fatal("helper did not pass the connection context to authentication")
	}
	if len(authorizer.calls) != 0 {
		t.Fatalf("authorization calls = %d, want 0 after authentication failure", len(authorizer.calls))
	}
}
