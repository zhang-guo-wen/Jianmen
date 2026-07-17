package admin

import (
	"context"
	"errors"
	"strings"
)

// authorizeConnection requires both an action permission and a resource grant.
// Super administrators bypass both checks, consistently with the other admin APIs.
func (s *Server) authorizeConnection(ctx context.Context, userID, action, resourceType, resourceID string) (bool, error) {
	return s.authorizeAnyConnection(ctx, userID, []string{action}, resourceType, resourceID)
}

func (s *Server) authorizeAnyConnection(ctx context.Context, userID string, actions []string, resourceType, resourceID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	if s.isSuperAdmin(userID) {
		return true, nil
	}

	if s.rbacChecker == nil {
		return false, errors.New("rbac checker unavailable")
	}
	actionAllowed := false
	for _, action := range actions {
		allowed, err := s.rbacChecker.HasPermission(userID, action, "", "")
		if err != nil {
			return false, err
		}
		if allowed {
			actionAllowed = true
			break
		}
	}
	if !actionAllowed {
		return false, nil
	}

	if s.resourceGrants == nil {
		return false, errors.New("resource grant service unavailable")
	}
	return s.resourceGrants.Check(ctx, userID, resourceType, resourceID)
}
