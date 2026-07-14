package admin

import (
	"fmt"
	"net/http"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func (s *Server) hasResourceGrant(r *http.Request, resourceType, resourceID string) (bool, error) {
	userID := userIDFromRequest(r)
	if userID == "" {
		return false, nil
	}
	if s.isSuperAdmin(userID) {
		return true, nil
	}
	if s.db == nil {
		return false, fmt.Errorf("database not available")
	}
	return rbac.NewResourceGrantChecker(s.db).HasGrant(userID, resourceType, resourceID)
}

func (s *Server) requireResourceGrant(w http.ResponseWriter, r *http.Request, resourceType, resourceID string) bool {
	allowed, err := s.hasResourceGrant(r, resourceType, resourceID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return false
	}
	if !allowed {
		s.forbidden(w, r)
		return false
	}
	return true
}

func (s *Server) grantCreatedResource(r *http.Request, resourceType, resourceID string) error {
	if s.db == nil || s.isSuperAdmin(userIDFromRequest(r)) {
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
	if s.isSuperAdmin(userIDFromRequest(r)) {
		return hosts, nil
	}
	result := make([]store.HostView, 0, len(hosts))
	for _, host := range hosts {
		visible, err := s.hostVisible(r, host.ID)
		if err != nil {
			return nil, err
		}
		if visible {
			result = append(result, host)
		}
	}
	return result, nil
}

func (s *Server) hostVisible(r *http.Request, hostID string) (bool, error) {
	allowed, err := s.hasResourceGrant(r, model.ResourceTypeHost, hostID)
	if err != nil || allowed {
		return allowed, err
	}
	accounts, err := s.store.HostAccounts(hostID)
	if err != nil {
		return false, err
	}
	for _, account := range accounts {
		allowed, err = s.hasResourceGrant(r, model.ResourceTypeHostAccount, account.ID)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

func (s *Server) visibleTargets(r *http.Request, targets []store.TargetView) ([]store.TargetView, error) {
	if s.isSuperAdmin(userIDFromRequest(r)) {
		return targets, nil
	}
	result := make([]store.TargetView, 0, len(targets))
	for _, target := range targets {
		allowed, err := s.hasResourceGrant(r, model.ResourceTypeHostAccount, target.ID)
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, target)
		}
	}
	return result, nil
}

func (s *Server) visibleDatabaseInstances(r *http.Request, instances []store.DatabaseInstanceView) ([]store.DatabaseInstanceView, error) {
	if s.isSuperAdmin(userIDFromRequest(r)) {
		return instances, nil
	}
	result := make([]store.DatabaseInstanceView, 0, len(instances))
	for _, instance := range instances {
		visible, err := s.databaseInstanceVisible(r, instance.ID)
		if err != nil {
			return nil, err
		}
		if visible {
			result = append(result, instance)
		}
	}
	return result, nil
}

func (s *Server) databaseInstanceVisible(r *http.Request, instanceID string) (bool, error) {
	allowed, err := s.hasResourceGrant(r, model.ResourceTypeDatabaseInstance, instanceID)
	if err != nil || allowed {
		return allowed, err
	}
	accounts, err := s.store.InstanceAccounts(instanceID)
	if err != nil {
		return false, err
	}
	for _, account := range accounts {
		allowed, err = s.hasResourceGrant(r, model.ResourceTypeDatabaseAccount, account.ID)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

func (s *Server) visibleDatabaseAccounts(r *http.Request, accounts []store.DatabaseAccountView) ([]store.DatabaseAccountView, error) {
	if s.isSuperAdmin(userIDFromRequest(r)) {
		return accounts, nil
	}
	result := make([]store.DatabaseAccountView, 0, len(accounts))
	for _, account := range accounts {
		allowed, err := s.hasResourceGrant(r, model.ResourceTypeDatabaseAccount, account.ID)
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, account)
		}
	}
	return result, nil
}
