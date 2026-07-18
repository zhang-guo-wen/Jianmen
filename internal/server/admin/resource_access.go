package admin

import (
	"errors"
	"net/http"

	"jianmen/internal/model"
	"jianmen/internal/store"
)

func (s *Server) hasResourceGrant(r *http.Request, resourceType, resourceID string) (bool, error) {
	userID := userIDFromRequest(r)
	if userID == "" {
		return false, nil
	}
	if isSuperAdminRequest(r) {
		return true, nil
	}
	if s.resourceGrants == nil {
		return false, errors.New("resource grant service unavailable")
	}
	return s.resourceGrants.Check(r.Context(), userID, resourceType, resourceID)
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

func (s *Server) requireHostAccountManagement(w http.ResponseWriter, r *http.Request, accountID string) bool {
	if isSuperAdminRequest(r) {
		return true
	}
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return false
	}
	var account model.HostAccount
	if err := s.db.Select("id", "host_id").First(&account, "id = ?", accountID).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "host account not found")
		return false
	}
	return s.requireResourceGrant(w, r, model.ResourceTypeHost, account.HostID)
}

func (s *Server) requireDatabaseAccountManagement(w http.ResponseWriter, r *http.Request, accountID string) bool {
	if isSuperAdminRequest(r) {
		return true
	}
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return false
	}
	var account model.DatabaseAccount
	if err := s.db.Select("id", "instance_id").First(&account, "id = ?", accountID).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "database account not found")
		return false
	}
	return s.requireResourceGrant(w, r, model.ResourceTypeDatabaseInstance, account.InstanceID)
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
		if isSuperAdminRequest(r) {
			host.CanManage = true
			result = append(result, host)
			continue
		}
		var err error
		host.CanManage, err = s.hasResourceGrant(r, model.ResourceTypeHost, host.ID)
		if err != nil {
			return nil, err
		}
		if host.CanManage {
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
		result = append(result, host)
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
	result := make([]store.TargetView, 0, len(targets))
	for _, target := range targets {
		if isSuperAdminRequest(r) {
			target.CanManage = true
			result = append(result, target)
			continue
		}
		allowed, err := s.hasResourceGrant(r, model.ResourceTypeHostAccount, target.ID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		if target.HostID != "" {
			target.CanManage, err = s.hasResourceGrant(r, model.ResourceTypeHost, target.HostID)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, target)
	}
	return result, nil
}

func (s *Server) visibleDatabaseInstances(r *http.Request, instances []store.DatabaseInstanceView) ([]store.DatabaseInstanceView, error) {
	result := make([]store.DatabaseInstanceView, 0, len(instances))
	for _, instance := range instances {
		if isSuperAdminRequest(r) {
			instance.CanManage = true
			result = append(result, instance)
			continue
		}
		var err error
		instance.CanManage, err = s.hasResourceGrant(r, model.ResourceTypeDatabaseInstance, instance.ID)
		if err != nil {
			return nil, err
		}
		if instance.CanManage {
			result = append(result, instance)
			continue
		}
		accounts, err := s.store.InstanceAccounts(instance.ID)
		if err != nil {
			return nil, err
		}
		accounts, err = s.visibleDatabaseAccounts(r, accounts)
		if err != nil {
			return nil, err
		}
		if len(accounts) == 0 {
			continue
		}
		instance.AccountCount = len(accounts)
		result = append(result, instance)
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
	result := make([]store.DatabaseAccountView, 0, len(accounts))
	for _, account := range accounts {
		if isSuperAdminRequest(r) {
			account.CanManage = true
			result = append(result, account)
			continue
		}
		allowed, err := s.hasResourceGrant(r, model.ResourceTypeDatabaseAccount, account.ID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		account.CanManage, err = s.hasResourceGrant(r, model.ResourceTypeDatabaseInstance, account.InstanceID)
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
		if isSuperAdminRequest(r) {
			application.CanManage = true
			result = append(result, application)
			continue
		}
		allowed, err := s.hasResourceGrant(r, model.ResourceTypeApplication, application.ID)
		if err != nil {
			return nil, err
		}
		if allowed {
			application.CanManage = true
			result = append(result, application)
		}
	}
	return result, nil
}
func (s *Server) visiblePlatformAccounts(r *http.Request, accounts []store.PlatformAccountView) ([]store.PlatformAccountView, error) {
	result := make([]store.PlatformAccountView, 0, len(accounts))
	for _, account := range accounts {
		allowed, err := s.hasResourceGrant(r, model.ResourceTypePlatformAccount, account.ID)
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, account)
		}
	}
	return result, nil
}
