package dbproxy

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

type connectionAuthorizationCall struct {
	ctx          context.Context
	userID       string
	actions      []string
	resourceType string
	resourceID   string
}

type captureConnectionAuthorizer struct {
	allowed bool
	err     error
	calls   []connectionAuthorizationCall
}

func (c *captureConnectionAuthorizer) Authorize(ctx context.Context, userID string, actions []string, resourceType, resourceID string) (bool, error) {
	c.calls = append(c.calls, connectionAuthorizationCall{
		ctx:          ctx,
		userID:       userID,
		actions:      actions,
		resourceType: resourceType,
		resourceID:   resourceID,
	})
	return c.allowed, c.err
}

func TestGatewayNewGatewayInjectsConnectionAuthorizer(t *testing.T) {
	authorizer := &captureConnectionAuthorizer{}
	gateway := NewGateway(config.DatabaseGatewayConfig{}, nil, "", nil, nil, authorizer, nil, nil)

	if gateway.authorizer != authorizer {
		t.Fatal("NewGateway did not retain the injected connection authorizer")
	}
}

func TestGatewayAuthorizeConnectUsesUnifiedDatabaseAuthorization(t *testing.T) {
	authorizer := &captureConnectionAuthorizer{allowed: true}
	gateway := &Gateway{authorizer: authorizer}
	ctx := context.Background()

	if err := gateway.authorizeConnect(ctx, "user-1", "dbacct-app"); err != nil {
		t.Fatalf("authorizeConnect returned error: %v", err)
	}
	if len(authorizer.calls) != 1 {
		t.Fatalf("authorization calls = %d, want 1", len(authorizer.calls))
	}
	call := authorizer.calls[0]
	if call.ctx != ctx {
		t.Fatal("authorizeConnect did not pass through the connection context")
	}
	if call.userID != "user-1" || len(call.actions) != 1 || call.actions[0] != rbac.ActionDBConnect || call.resourceType != model.ResourceTypeDatabaseAccount || call.resourceID != "dbacct-app" {
		t.Fatalf("unexpected authorization request: %#v", call)
	}
}

func TestGatewayAuthorizeConnectRejectsDeniedDecision(t *testing.T) {
	gateway := &Gateway{authorizer: &captureConnectionAuthorizer{allowed: false}}

	if err := gateway.authorizeConnect(context.Background(), "user-1", "dbacct-app"); err == nil {
		t.Fatal("expected authorization denial")
	}
}

func TestGatewayAuthorizeConnectDelegatesSuperAdminToUnifiedService(t *testing.T) {
	authorizer := &captureConnectionAuthorizer{allowed: true}
	gateway := &Gateway{authorizer: authorizer}

	if err := gateway.authorizeConnect(context.Background(), "super-admin-1", "dbacct-app"); err != nil {
		t.Fatalf("authorizeConnect returned error for super admin: %v", err)
	}
	if len(authorizer.calls) != 1 || authorizer.calls[0].userID != "super-admin-1" {
		t.Fatal("super administrator authorization must be delegated to the unified authorizer")
	}
}

func TestGatewayAuthorizeConnectPropagatesUnifiedAuthorizationError(t *testing.T) {
	authorizationErr := errors.New("authorization backend unavailable")
	gateway := &Gateway{authorizer: &captureConnectionAuthorizer{err: authorizationErr}}

	err := gateway.authorizeConnect(context.Background(), "user-1", "dbacct-app")
	if !errors.Is(err, authorizationErr) {
		t.Fatalf("authorizeConnect error = %v, want wrapped authorization error", err)
	}
}

func TestGatewayAuthorizeConnectFailsClosedWithoutAuthorizer(t *testing.T) {
	gateway := &Gateway{}

	if err := gateway.authorizeConnect(context.Background(), "user-1", "dbacct-app"); err == nil {
		t.Fatal("expected missing authorizer to deny the connection")
	}
}

func TestGatewayAuthorizeConnectPropagatesCancelledConnectionContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	authorizer := &captureConnectionAuthorizer{err: ctx.Err()}
	gateway := &Gateway{authorizer: authorizer}

	err := gateway.authorizeConnect(ctx, "user-1", "dbacct-app")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("authorizeConnect error = %v, want context cancellation", err)
	}
	if len(authorizer.calls) != 1 || authorizer.calls[0].ctx != ctx {
		t.Fatal("cancelled connection context was not passed to the unified authorizer")
	}
}

type captureDatabaseAccountResolver struct {
	ctx context.Context
	err error
}

func (c *captureDatabaseAccountResolver) AuthenticateConnectionPassword(ctx context.Context, _, _, _, _ string) error {
	c.ctx = ctx
	return c.err
}

func (c *captureDatabaseAccountResolver) AuthenticateMySQLConnectionPassword(context.Context, string, string, []byte, []byte) error {
	return nil
}

func TestGatewayValidateUserPasswordPropagatesCancelledConnectionContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := &captureDatabaseAccountResolver{err: ctx.Err()}
	gateway := &Gateway{store: resolver}

	err := gateway.validateUserPassword(ctx, &model.User{ID: "user-1"}, "dbacct-app", "password")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("validateUserPassword error = %v, want context cancellation", err)
	}
	if resolver.ctx != ctx {
		t.Fatal("validateUserPassword did not pass through the connection context")
	}
}
