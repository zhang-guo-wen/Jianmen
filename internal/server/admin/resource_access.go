package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

type resourceAccessRepository interface {
	ListHostAccounts(ctx context.Context, hostID string) ([]store.TargetView, error)
	ListDatabaseAccountsByInstance(ctx context.Context, instanceID string) ([]store.DatabaseAccountView, error)
}

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
	if s.resourceGrants == nil {
		return errors.New("resource grant service unavailable")
	}
	if err := s.resourceGrants.GrantCreatedResource(
		r.Context(),
		userIDFromRequest(r),
		isSuperAdminRequest(r),
		resourceType,
		resourceID,
	); err != nil {
		return fmt.Errorf("grant created resource: %w", err)
	}
	return nil
}

func (s *Server) visibleHosts(r *http.Request, hosts []store.HostView) ([]store.HostView, error) {
	hostIDs := make([]string, len(hosts))
	for index := range hosts {
		hostIDs[index] = hosts[index].ID
	}
	hostVisible, err := s.authorizeResourceActionsBatch(r, []string{rbac.ActionHostView}, model.ResourceTypeHost, hostIDs)
	if err != nil {
		return nil, err
	}
	hostManage, err := s.authorizeResourceActionsBatch(r, []string{rbac.ActionHostUpdate, rbac.ActionHostDelete}, model.ResourceTypeHost, hostIDs)
	if err != nil {
		return nil, err
	}
	allTargets, err := s.hostTargets.Targets(r.Context())
	if err != nil {
		return nil, err
	}
	visibleTargets, err := s.visibleTargets(r, allTargets)
	if err != nil {
		return nil, err
	}
	targetCount := make(map[string]int, len(hosts))
	for _, target := range visibleTargets {
		targetCount[target.HostID]++
	}
	result := make([]store.HostView, 0, len(hosts))
	for index, host := range hosts {
		if hostVisible[index] {
			host.CanManage = hostManage[index]
			result = append(result, host)
			continue
		}
		if targetCount[host.ID] == 0 {
			continue
		}
		host.AccountCount = targetCount[host.ID]
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
	accounts, err := s.resourceAccess.ListHostAccounts(r.Context(), hostID)
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
	ids := make([]string, len(targets))
	for index := range targets {
		ids[index] = targets[index].ID
	}
	visible, err := s.authorizeResourceActionsBatch(r, actions, model.ResourceTypeHostAccount, ids)
	if err != nil {
		return nil, err
	}
	manageable, err := s.authorizeResourceActionsBatch(r, []string{rbac.ActionTargetUpdate, rbac.ActionTargetDelete}, model.ResourceTypeHostAccount, ids)
	if err != nil {
		return nil, err
	}
	result := make([]store.TargetView, 0, len(targets))
	for index, target := range targets {
		if !visible[index] {
			continue
		}
		target.CanManage = manageable[index]
		result = append(result, target)
	}
	return result, nil
}

func (s *Server) visibleConnectableTargets(r *http.Request, targets []store.TargetView) ([]store.TargetView, error) {
	requests := make([]service.AuthorizationRequest, len(targets))
	for index, target := range targets {
		actions := []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}
		if strings.EqualFold(target.Protocol, "rdp") {
			actions = []string{rbac.ActionRDPConnect}
		}
		requests[index] = service.AuthorizationRequest{Actions: actions, ResourceType: model.ResourceTypeHostAccount, ResourceID: target.ID}
	}
	visible, err := s.authorizeResourceRequestsBatch(r, requests)
	if err != nil {
		return nil, err
	}
	manageRequests := make([]service.AuthorizationRequest, len(targets))
	for index, target := range targets {
		manageRequests[index] = service.AuthorizationRequest{Actions: []string{rbac.ActionTargetUpdate, rbac.ActionTargetDelete}, ResourceType: model.ResourceTypeHostAccount, ResourceID: target.ID}
	}
	manageable, err := s.authorizeResourceRequestsBatch(r, manageRequests)
	if err != nil {
		return nil, err
	}
	result := make([]store.TargetView, 0, len(targets))
	for index, target := range targets {
		if !visible[index] {
			continue
		}
		target.CanManage = manageable[index]
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
	instanceIDs := make([]string, len(instances))
	for index := range instances {
		instanceIDs[index] = instances[index].ID
	}
	instanceVisible, err := s.authorizeResourceActionsBatch(r, actions, model.ResourceTypeDatabaseInstance, instanceIDs)
	if err != nil {
		return nil, err
	}
	instanceManage, err := s.authorizeResourceActionsBatch(r, []string{rbac.ActionDBProxyUpdate, rbac.ActionDBProxyDelete}, model.ResourceTypeDatabaseInstance, instanceIDs)
	if err != nil {
		return nil, err
	}
	allAccounts, err := s.databases.DatabaseAccounts(r.Context())
	if err != nil {
		return nil, err
	}
	visibleAccounts, err := s.visibleDatabaseAccountsForActions(r, allAccounts, actions)
	if err != nil {
		return nil, err
	}
	accountCount := make(map[string]int, len(instances))
	for _, account := range visibleAccounts {
		accountCount[account.InstanceID]++
	}
	result := make([]store.DatabaseInstanceView, 0, len(instances))
	for index, instance := range instances {
		if instanceVisible[index] {
			instance.CanManage = instanceManage[index]
			result = append(result, instance)
			continue
		}
		if accountCount[instance.ID] == 0 {
			continue
		}
		instance.AccountCount = accountCount[instance.ID]
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
	accounts, err := s.resourceAccess.ListDatabaseAccountsByInstance(r.Context(), instanceID)
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
	ids := make([]string, len(accounts))
	for index := range accounts {
		ids[index] = accounts[index].ID
	}
	visible, err := s.authorizeResourceActionsBatch(r, actions, model.ResourceTypeDatabaseAccount, ids)
	if err != nil {
		return nil, err
	}
	manageable, err := s.authorizeResourceActionsBatch(r, []string{rbac.ActionDBProxyUpdate, rbac.ActionDBProxyDelete}, model.ResourceTypeDatabaseAccount, ids)
	if err != nil {
		return nil, err
	}
	result := make([]store.DatabaseAccountView, 0, len(accounts))
	for index, account := range accounts {
		if !visible[index] {
			continue
		}
		account.CanManage = manageable[index]
		result = append(result, account)
	}
	return result, nil
}

func (s *Server) visiblePlatformAccounts(r *http.Request, accounts []store.PlatformAccountView) ([]store.PlatformAccountView, error) {
	ids := make([]string, len(accounts))
	for index := range accounts {
		ids[index] = accounts[index].ID
	}
	visible, err := s.authorizeResourceActionsBatch(r, []string{rbac.ActionPlatformAccountView}, model.ResourceTypePlatformAccount, ids)
	if err != nil {
		return nil, err
	}
	result := make([]store.PlatformAccountView, 0, len(accounts))
	for index, account := range accounts {
		if !visible[index] {
			continue
		}
		result = append(result, account)
	}
	return result, nil
}

func (s *Server) visibleContainerEndpoints(r *http.Request, endpoints []store.ContainerEndpointView) ([]store.ContainerEndpointView, error) {
	ids := make([]string, len(endpoints))
	for index := range endpoints {
		ids[index] = endpoints[index].ID
	}
	visible, err := s.authorizeResourceActionsBatch(r, []string{rbac.ActionContainerView, rbac.ActionContainerConnect}, model.ResourceTypeContainerEndpoint, ids)
	if err != nil {
		return nil, err
	}
	manageable, err := s.authorizeResourceActionsBatch(r, []string{rbac.ActionContainerUpdate, rbac.ActionContainerDelete}, model.ResourceTypeContainerEndpoint, ids)
	if err != nil {
		return nil, err
	}
	result := make([]store.ContainerEndpointView, 0, len(endpoints))
	for index, endpoint := range endpoints {
		if !visible[index] {
			continue
		}
		endpoint.CanManage = manageable[index]
		result = append(result, endpoint)
	}
	return result, nil
}

func (s *Server) authorizeResourceActionsBatch(r *http.Request, actions []string, resourceType string, ids []string) ([]bool, error) {
	requests := make([]service.AuthorizationRequest, len(ids))
	for index, id := range ids {
		requests[index] = service.AuthorizationRequest{Actions: actions, ResourceType: resourceType, ResourceID: id}
	}
	return s.authorizeResourceRequestsBatch(r, requests)
}

func (s *Server) authorizeResourceRequestsBatch(r *http.Request, requests []service.AuthorizationRequest) ([]bool, error) {
	allowed := make([]bool, len(requests))
	if len(requests) == 0 {
		return allowed, nil
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		return allowed, nil
	}
	if s.authorization == nil {
		return nil, errors.New("batch authorization service unavailable")
	}
	decisions, err := s.authorization.AuthorizeBatch(r.Context(), userID, requests)
	if err != nil {
		return nil, fmt.Errorf("batch authorize resource actions: %w", err)
	}
	if len(decisions) != len(requests) {
		return nil, errors.New("batch authorization decision count mismatch")
	}
	for index, decision := range decisions {
		allowed[index] = decision.Allowed
	}
	return allowed, nil
}
