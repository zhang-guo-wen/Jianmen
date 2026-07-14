package admin

import (
	"errors"
	"strings"

	"jianmen/internal/rbac"
)

// authorizeConnection requires both an action permission and a resource grant.
// Super administrators bypass both checks, consistently with the other admin APIs.
func (s *Server) authorizeConnection(userID, action, resourceType, resourceID string) (bool, error) {
	return s.authorizeAnyConnection(userID, []string{action}, resourceType, resourceID)
}

func (s *Server) authorizeAnyConnection(userID string, actions []string, resourceType, resourceID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	if s.isSuperAdmin(userID) {
		return true, nil
	}

	checker := s.rbacChecker
	if checker == nil && s.db != nil {
		checker = rbac.NewChecker(s.db)
	}
	if checker == nil {
		return false, errors.New("rbac checker unavailable")
	}
	actionAllowed := false
	for _, action := range actions {
		allowed, err := checker.HasPermission(userID, action, "", "")
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

	if s.db == nil {
		return false, errors.New("resource grant checker unavailable")
	}
	return rbac.NewResourceGrantChecker(s.db).HasGrant(userID, resourceType, resourceID)
}
