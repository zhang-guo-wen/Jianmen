package appproxy

import "testing"

type stubPermissionChecker struct {
	allowed bool
}

func (s stubPermissionChecker) HasPermission(_, _, _, _ string) (bool, error) {
	return s.allowed, nil
}

type stubResourceGrantChecker struct {
	allowed bool
}

func (s stubResourceGrantChecker) HasGrant(_, _, _ string) (bool, error) {
	return s.allowed, nil
}

func TestAuthorizeAppRequiresActionAndResourceGrant(t *testing.T) {
	tests := []struct {
		name          string
		actionAllowed bool
		grantAllowed  bool
		wantAllowed   bool
	}{
		{name: "both allowed", actionAllowed: true, grantAllowed: true, wantAllowed: true},
		{name: "action denied", actionAllowed: false, grantAllowed: true},
		{name: "resource denied", actionAllowed: true, grantAllowed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{
				checker:       stubPermissionChecker{allowed: tt.actionAllowed},
				resourceGrant: stubResourceGrantChecker{allowed: tt.grantAllowed},
				superAdminIDs: map[string]bool{},
			}
			err := s.authorizeApp("u1", "app1")
			if (err == nil) != tt.wantAllowed {
				t.Fatalf("authorizeApp error = %v, wantAllowed = %v", err, tt.wantAllowed)
			}
		})
	}
}

func TestAuthorizeAppAllowsSuperAdmin(t *testing.T) {
	s := &Server{superAdminIDs: map[string]bool{"admin": true}}
	if err := s.authorizeApp("admin", "app1"); err != nil {
		t.Fatalf("authorizeApp super admin: %v", err)
	}
}
