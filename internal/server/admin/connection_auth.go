package admin

import (
	"errors"
	"strings"

	"jianmen/internal/rbac"
)

// authorizeConnection requires both an action permission and a resource grant.
// Super administrators bypass both checks, consistently with the other admin APIs.
func (s *Server) authorizeConnection(userID, action, resourceType, resourceID string) (bool, error) {
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
	allowed, err := checker.HasPermission(userID, action, "", "")
	if err != nil || !allowed {
		return allowed, err
	}

	if s.db == nil {
		return false, errors.New("resource grant checker unavailable")
	}
	return rbac.NewResourceGrantChecker(s.db).HasGrant(userID, resourceType, resourceID)
}
