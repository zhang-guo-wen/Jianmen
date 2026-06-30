package dbproxy

import (
	"errors"
	"testing"

	"jianmen/internal/model"
	rbaccheck "jianmen/internal/rbac"
)

type capturedPermissionCheck struct {
	userID       string
	action       string
	resourceType string
	resourceID   string
}

type capturePermissionChecker struct {
	allowed bool
	err     error
	calls   []capturedPermissionCheck
}

func (c *capturePermissionChecker) HasPermission(userID, action, resourceType, resourceID string) (bool, error) {
	c.calls = append(c.calls, capturedPermissionCheck{
		userID:       userID,
		action:       action,
		resourceType: resourceType,
		resourceID:   resourceID,
	})
	return c.allowed, c.err
}

func TestGatewayAuthorizeConnectChecksDatabaseAccountPermission(t *testing.T) {
	checker := &capturePermissionChecker{allowed: true}
	gateway := &Gateway{
		permissionChecker: checker,
		superAdminIDs:     map[string]bool{},
	}

	if err := gateway.authorizeConnect("user-1", "D000100001", "dbacct-app"); err != nil {
		t.Fatalf("authorizeConnect returned error: %v", err)
	}

	if len(checker.calls) != 1 {
		t.Fatalf("permission checks = %d, want 1", len(checker.calls))
	}
	call := checker.calls[0]
	if call.userID != "user-1" ||
		call.action != rbaccheck.ActionDBConnect ||
		call.resourceType != model.ResourceTypeDatabaseAccount ||
		call.resourceID != "dbacct-app" {
		t.Fatalf("unexpected permission check: %#v", call)
	}
}

func TestGatewayAuthorizeConnectRejectsDeniedPermission(t *testing.T) {
	checker := &capturePermissionChecker{allowed: false}
	gateway := &Gateway{
		permissionChecker: checker,
		superAdminIDs:     map[string]bool{},
	}

	if err := gateway.authorizeConnect("user-1", "D000100001", "dbacct-app"); err == nil {
		t.Fatal("expected authorization denial")
	}
}

func TestGatewayAuthorizeConnectPropagatesCheckerError(t *testing.T) {
	checkerErr := errors.New("checker unavailable")
	checker := &capturePermissionChecker{err: checkerErr}
	gateway := &Gateway{
		permissionChecker: checker,
		superAdminIDs:     map[string]bool{},
	}

	err := gateway.authorizeConnect("user-1", "D000100001", "dbacct-app")
	if err == nil || !errors.Is(err, checkerErr) {
		t.Fatalf("authorizeConnect error = %v, want checker error", err)
	}
}

func TestGatewayAuthorizeConnectSkipsSuperAdmin(t *testing.T) {
	checker := &capturePermissionChecker{allowed: false}
	gateway := &Gateway{
		permissionChecker: checker,
		superAdminIDs:     map[string]bool{"admin-1": true},
	}

	if err := gateway.authorizeConnect("admin-1", "D000100001", "dbacct-app"); err != nil {
		t.Fatalf("authorizeConnect returned error for super admin: %v", err)
	}
	if len(checker.calls) != 0 {
		t.Fatalf("permission checks = %d, want 0 for super admin", len(checker.calls))
	}
}
