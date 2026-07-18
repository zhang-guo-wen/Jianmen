package admin

import (
	"errors"
	"fmt"
	"net/http"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func (s *Server) requireAuthenticatedUser(w http.ResponseWriter, r *http.Request) bool {
	if userIDFromRequest(r) != "" {
		return true
	}
	s.forbidden(w, r)
	return false
}

// requireResourceAction evaluates an action and a concrete resource together.
// Splitting these checks would allow a resource-specific permission deny to be
// bypassed by an otherwise valid ResourceGrant.
func (s *Server) requireResourceAction(w http.ResponseWriter, r *http.Request, action, resourceType, resourceID string) bool {
	return s.requireResourceActions(w, r, []string{action}, resourceType, resourceID)
}

func (s *Server) requireResourceActions(w http.ResponseWriter, r *http.Request, actions []string, resourceType, resourceID string) bool {
	allowed, err := s.authorizeResourceActions(r, actions, resourceType, resourceID)
	if err != nil {
		s.logger.Warn("resource authorization failed", "user_id", userIDFromRequest(r), "actions", actions, "resource_type", resourceType, "resource_id", resourceID, "error", err)
		s.forbidden(w, r)
		return false
	}
	if !allowed {
		s.forbidden(w, r)
		return false
	}
	return true
}

func (s *Server) authorizeResourceActions(r *http.Request, actions []string, resourceType, resourceID string) (bool, error) {
	if userIDFromRequest(r) == "" {
		return false, nil
	}
	if s.authorization == nil {
		return false, errors.New("authorization service unavailable")
	}
	allowed, err := s.authorizeAnyConnection(r.Context(), userIDFromRequest(r), actions, resourceType, resourceID)
	if err != nil {
		return false, fmt.Errorf("authorize resource actions: %w", err)
	}
	return allowed, nil
}

func (s *Server) grantCreatedResource(r *http.Request, resourceType, resourceID string) error {
	if s.db == nil || isSuperAdminRequest(r) {
		return nil
	}
	grant := model.ResourceGrant{
		PrincipalType: "user",
		PrincipalID:   userIDFromRequest(r),
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		Effect:        model.PermissionEffectAllow,
	}
	return s.db.Where(grant).FirstOrCreate(&grant).Error
}

func (s *Server) visibleHosts(r *http.Request, hosts []store.HostView) ([]store.HostView, error) {
	result := make([]store.HostView, 0, len(hosts))
	for _, host := range hosts {
		visible, err := s.authorizeResourceActions(r, []string{rbac.ActionHostView}, model.ResourceTypeHost, host.ID)
		if err != nil {
			return nil, err
		}
		canManage, err := s.authorizeResourceActions(r, []string{rbac.ActionHostUpdate, rbac.ActionHostDelete}, model.ResourceTypeHost, host.ID)
		if err != nil {
			return nil, err
		}
		if visible {
			host.CanManage = canManage
			result = append(result, host)
			continue
		}

		accounts, err := s.store.HostAccounts(host.ID)
		if err != nil {
			return nil, err
		}
		accounts, err = s.visibleTargets(r, accounts)
		if err != nil {
			return nil, err
		}
		if len(accounts) == 0 {
			continue
		}
		host.AccountCount = len(accounts)
		host.CanManage = false
		result = append(result, host)
	}
	return result, nil
}

func (s *Server) hostVisible(r *http.Request, hostID string) (bool, error) {
	allowed, err := s.authorizeResourceActions(r, []string{rbac.ActionHostView}, model.ResourceTypeHost, hostID)
	if err != nil || allowed {
		return allowed, err
	}
	accounts, err := s.store.HostAccounts(hostID)
	if err != nil {
		return false, err
	}
	for _, account := range accounts {
		allowed, err = s.authorizeResourceActions(r, []string{rbac.ActionTargetView}, model.ResourceTypeHostAccount, account.ID)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

func (s *Server) visibleTargets(r *http.Request, targets []store.TargetView) ([]store.TargetView, error) {
	return s.visibleTargetsForActions(r, targets, []string{rbac.ActionTargetView})
}

func (s *Server) visibleTargetsForActions(r *http.Request, targets []store.TargetView, actions []string) ([]store.TargetView, error) {
	result := make([]store.TargetView, 0, len(targets))
	for _, target := range targets {
		allowed, err := s.authorizeResourceActions(r, actions, model.ResourceTypeHostAccount, target.ID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		target.CanManage, err = s.authorizeResourceActions(r, []string{rbac.ActionTargetUpdate, rbac.ActionTargetDelete}, model.ResourceTypeHostAccount, target.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, target)
	}
	return result, nil
}

func (s *Server) visibleDatabaseInstances(r *http.Request, instances []store.DatabaseInstanceView) ([]store.DatabaseInstanceView, error) {
	return s.visibleDatabaseInstancesForActions(r, instances, []string{rbac.ActionDBProxyView})
}

func (s *Server) visibleDatabaseInstancesForActions(
	r *http.Request,
	instances []store.DatabaseInstanceView,
	actions []string,
) ([]store.DatabaseInstanceView, error) {
	result := make([]store.DatabaseInstanceView, 0, len(instances))
	for _, instance := range instances {
		visible, err := s.authorizeResourceActions(r, actions, model.ResourceTypeDatabaseInstance, instance.ID)
		if err != nil {
			return nil, err
		}
		canManage, err := s.authorizeResourceActions(r, []string{rbac.ActionDBProxyUpdate, rbac.ActionDBProxyDelete}, model.ResourceTypeDatabaseInstance, instance.ID)
		if err != nil {
			return nil, err
		}
		if visible {
			instance.CanManage = canManage
			result = append(result, instance)
			continue
		}

		accounts, err := s.store.InstanceAccounts(instance.ID)
		if err != nil {
			return nil, err
		}
		accounts, err = s.visibleDatabaseAccountsForActions(r, accounts, actions)
		if err != nil {
			return nil, err
		}
		if len(accounts) == 0 {
			continue
		}
		instance.AccountCount = len(accounts)
		instance.CanManage = false
		result = append(result, instance)
	}
	return result, nil
}

func (s *Server) databaseInstanceVisible(r *http.Request, instanceID string) (bool, error) {
	allowed, err := s.authorizeResourceActions(r, []string{rbac.ActionDBProxyView}, model.ResourceTypeDatabaseInstance, instanceID)
	if err != nil || allowed {
		return allowed, err
	}
	accounts, err := s.store.InstanceAccounts(instanceID)
	if err != nil {
		return false, err
	}
	for _, account := range accounts {
		allowed, err = s.authorizeResourceActions(r, []string{rbac.ActionDBProxyView}, model.ResourceTypeDatabaseAccount, account.ID)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

func (s *Server) visibleDatabaseAccounts(r *http.Request, accounts []store.DatabaseAccountView) ([]store.DatabaseAccountView, error) {
	return s.visibleDatabaseAccountsForActions(r, accounts, []string{rbac.ActionDBProxyView})
}

func (s *Server) visibleDatabaseAccountsForActions(r *http.Request, accounts []store.DatabaseAccountView, actions []string) ([]store.DatabaseAccountView, error) {
	result := make([]store.DatabaseAccountView, 0, len(accounts))
	for _, account := range accounts {
		allowed, err := s.authorizeResourceActions(r, actions, model.ResourceTypeDatabaseAccount, account.ID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		account.CanManage, err = s.authorizeResourceActions(r, []string{rbac.ActionDBProxyUpdate, rbac.ActionDBProxyDelete}, model.ResourceTypeDatabaseAccount, account.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, account)
	}
	return result, nil
}

func (s *Server) visibleApplications(r *http.Request, applications []store.ApplicationView) ([]store.ApplicationView, error) {
	result := make([]store.ApplicationView, 0, len(applications))
	for _, application := range applications {
		allowed, err := s.authorizeResourceActions(r, []string{rbac.ActionAppView}, model.ResourceTypeApplication, application.ID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		application.CanManage, err = s.authorizeResourceActions(r, []string{rbac.ActionAppUpdate, rbac.ActionAppDelete}, model.ResourceTypeApplication, application.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, application)
	}
	return result, nil
}

func (s *Server) visiblePlatformAccounts(r *http.Request, accounts []store.PlatformAccountView) ([]store.PlatformAccountView, error) {
	result := make([]store.PlatformAccountView, 0, len(accounts))
	for _, account := range accounts {
		allowed, err := s.authorizeResourceActions(r, []string{rbac.ActionPlatformAccountView}, model.ResourceTypePlatformAccount, account.ID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		result = append(result, account)
	}
	return result, nil
}
