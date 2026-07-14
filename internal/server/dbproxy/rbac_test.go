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

type capturedResourceCheck struct {
	userID       string
	resourceType string
	resourceID   string
}

type captureResourceGrantChecker struct {
	allowed bool
	err     error
	calls   []capturedResourceCheck
}

func (c *captureResourceGrantChecker) HasGrant(userID, resourceType, resourceID string) (bool, error) {
	c.calls = append(c.calls, capturedResourceCheck{userID: userID, resourceType: resourceType, resourceID: resourceID})
	return c.allowed, c.err
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
	grants := &captureResourceGrantChecker{allowed: true}
	gateway := &Gateway{
		permissionChecker: checker,
		resourceChecker:   grants,
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
		call.resourceType != "" ||
		call.resourceID != "" {
		t.Fatalf("unexpected permission check: %#v", call)
	}
	if len(grants.calls) != 1 || grants.calls[0].resourceType != model.ResourceTypeDatabaseAccount || grants.calls[0].resourceID != "dbacct-app" {
		t.Fatalf("unexpected resource grant check: %#v", grants.calls)
	}
}

func TestGatewayAuthorizeConnectRejectsDeniedPermission(t *testing.T) {
	checker := &capturePermissionChecker{allowed: false}
	gateway := &Gateway{
		permissionChecker: checker,
		resourceChecker:   &captureResourceGrantChecker{allowed: true},
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
		resourceChecker:   &captureResourceGrantChecker{allowed: true},
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
		resourceChecker:   &captureResourceGrantChecker{allowed: false},
		superAdminIDs:     map[string]bool{"admin-1": true},
	}

	if err := gateway.authorizeConnect("admin-1", "D000100001", "dbacct-app"); err != nil {
		t.Fatalf("authorizeConnect returned error for super admin: %v", err)
	}
	if len(checker.calls) != 0 {
		t.Fatalf("permission checks = %d, want 0 for super admin", len(checker.calls))
	}
}

func TestGatewayAuthorizeConnectRejectsMissingResourceGrant(t *testing.T) {
	gateway := &Gateway{
		permissionChecker: &capturePermissionChecker{allowed: true},
		resourceChecker:   &captureResourceGrantChecker{allowed: false},
		superAdminIDs:     map[string]bool{},
	}
	if err := gateway.authorizeConnect("user-1", "D000100001", "dbacct-app"); err == nil {
		t.Fatal("expected resource grant denial")
	}
}
