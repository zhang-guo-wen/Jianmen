package rbac

import (
	"context"
	"strings"

	"jianmen/internal/model"
)

// BatchAuthorizationRequest identifies one resource and alternative actions.
type BatchAuthorizationRequest struct {
	ResourceType string
	ResourceID   string
	Actions      []string
}

type BatchActionDecision struct {
	ActionAllowed bool
	Allowed       bool
	Denied        bool
}

func BatchResourceKey(resourceType, resourceID string) string {
	return resourceType + "\x00" + resourceID
}

type batchFacts struct {
	permissionGroups map[string]string
	permissionOf     map[string]string
	resourceGroups   map[string]string
	accountGroups    map[string]string
	resourceOf       map[string]string
	accountOf        map[string]string
	hostOf           map[string]string
	instanceOf       map[string]string
	activeResources  map[string]bool
}

func (f batchFacts) groupMatches(groupID string, resourceType, resourceID string, account bool) bool {
	key := BatchResourceKey(resourceType, resourceID)
	if account {
		return f.accountGroups[groupID] != "" && f.accountGroups[groupID] == f.accountOf[key]
	}
	return f.resourceGroups[groupID] != "" && f.resourceGroups[groupID] == f.resourceOf[key]
}

func (f batchFacts) permissionGroupMatches(groupID, resourceType, resourceID string) bool {
	groupName := f.permissionGroups[groupID]
	if groupName == "" {
		return false
	}
	return groupName == f.permissionOf[BatchResourceKey(resourceType, resourceID)]
}

func (f batchFacts) resourceIsActive(resourceType, resourceID string) bool {
	if !auditedResourceType(resourceType) {
		return true
	}
	return f.activeResources[BatchResourceKey(resourceType, resourceID)]
}

func batchIDs(requests []BatchAuthorizationRequest, resourceType string) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0)
	for _, request := range requests {
		if request.ResourceType == resourceType && strings.TrimSpace(request.ResourceID) != "" {
			if _, ok := seen[request.ResourceID]; !ok {
				seen[request.ResourceID] = struct{}{}
				ids = append(ids, request.ResourceID)
			}
		}
	}
	return ids
}

func (c *Checker) loadBatchFacts(ctx context.Context, requests []BatchAuthorizationRequest, groupIDs []string) (batchFacts, error) {
	facts := batchFacts{
		permissionGroups: map[string]string{},
		permissionOf:     map[string]string{},
		resourceGroups:   map[string]string{},
		accountGroups:    map[string]string{},
		resourceOf:       map[string]string{},
		accountOf:        map[string]string{},
		hostOf:           map[string]string{},
		instanceOf:       map[string]string{},
		activeResources:  map[string]bool{},
	}
	db := c.db.WithContext(ctx)
	allGroupIDs := append([]string(nil), groupIDs...)
	allGroupIDs = appendUniqueBatchIDs(allGroupIDs, batchIDs(requests, model.ResourceTypeGroup)...)
	allGroupIDs = appendUniqueBatchIDs(allGroupIDs, batchIDs(requests, model.ResourceTypeAccountGroup)...)
	if len(allGroupIDs) > 0 {
		var groups []model.ResourceGroup
		if err := db.Where("id IN ? AND active_marker = ?", allGroupIDs, model.ActiveMarkerValue).Find(&groups).Error; err != nil {
			return facts, err
		}
		for _, group := range groups {
			facts.activeResources[BatchResourceKey(model.ResourceTypeGroup, group.ID)] = true
			facts.activeResources[BatchResourceKey(model.ResourceTypeAccountGroup, group.ID)] = true
			// Permission keeps the legacy Checker.groupContainsResource
			// semantics: the referenced group ID supplies a name regardless of
			// group_type. ResourceGrant uses the two typed maps below instead.
			facts.permissionGroups[group.ID] = group.Name
			if group.GroupType == model.ResourceGroupTypeAccount {
				facts.accountGroups[group.ID] = group.Name
			} else {
				facts.resourceGroups[group.ID] = group.Name
			}
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeHost); len(ids) > 0 {
		var rows []model.Host
		if err := db.Where("id IN ? AND active_marker = ?", ids, model.ActiveMarkerValue).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			key := BatchResourceKey(model.ResourceTypeHost, x.ID)
			facts.activeResources[key] = true
			facts.permissionOf[key] = x.GroupName
			facts.resourceOf[key] = x.GroupName
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeDatabaseInstance); len(ids) > 0 {
		var rows []model.DatabaseInstance
		if err := db.Where("id IN ? AND active_marker = ?", ids, model.ActiveMarkerValue).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			key := BatchResourceKey(model.ResourceTypeDatabaseInstance, x.ID)
			facts.activeResources[key] = true
			facts.permissionOf[key] = x.GroupName
			facts.resourceOf[key] = x.GroupName
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeApplication); len(ids) > 0 {
		var rows []model.Application
		if err := db.Where("id IN ? AND active_marker = ?", ids, model.ActiveMarkerValue).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			key := BatchResourceKey(model.ResourceTypeApplication, x.ID)
			facts.activeResources[key] = true
			facts.permissionOf[key] = x.AppGroup
			facts.resourceOf[key] = x.AppGroup
		}
	}
	if ids := batchIDs(requests, model.ResourceTypePlatformAccount); len(ids) > 0 {
		var rows []model.PlatformAccount
		if err := db.Where("id IN ? AND active_marker = ?", ids, model.ActiveMarkerValue).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			key := BatchResourceKey(model.ResourceTypePlatformAccount, x.ID)
			facts.activeResources[key] = true
			facts.permissionOf[key] = x.GroupName
			facts.accountOf[key] = x.GroupName
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeContainerEndpoint); len(ids) > 0 {
		var rows []model.ContainerEndpoint
		if err := db.Where("id IN ? AND active_marker = ?", ids, model.ActiveMarkerValue).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			// Legacy Permission group matching intentionally excludes container
			// endpoints. ResourceGrant resource groups continue to include them.
			key := BatchResourceKey(model.ResourceTypeContainerEndpoint, x.ID)
			facts.activeResources[key] = true
			facts.resourceOf[key] = x.GroupName
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeHostAccount); len(ids) > 0 {
		var rows []struct{ ID, HostID, GroupName, HostGroup string }
		if err := db.Table("host_accounts").
			Select("host_accounts.id, host_accounts.host_id, host_accounts.group_name, hosts.group_name AS host_group").
			Joins("JOIN hosts ON hosts.id = host_accounts.host_id").
			Where("host_accounts.id IN ?", ids).
			Where("host_accounts.active_marker = ? AND hosts.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue).
			Scan(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			key := BatchResourceKey(model.ResourceTypeHostAccount, x.ID)
			facts.activeResources[key] = true
			facts.permissionOf[key] = x.HostGroup
			facts.resourceOf[key] = x.HostGroup
			facts.accountOf[key] = x.GroupName
			facts.hostOf[x.ID] = x.HostID
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeDatabaseAccount); len(ids) > 0 {
		var rows []struct{ ID, InstanceID, GroupName, InstanceGroup string }
		if err := db.Table("database_accounts").
			Select("database_accounts.id, database_accounts.instance_id, database_accounts.group_name, database_instances.group_name AS instance_group").
			Joins("JOIN database_instances ON database_instances.id = database_accounts.instance_id").
			Where("database_accounts.id IN ?", ids).
			Where("database_accounts.active_marker = ? AND database_instances.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue).
			Scan(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			key := BatchResourceKey(model.ResourceTypeDatabaseAccount, x.ID)
			facts.activeResources[key] = true
			facts.permissionOf[key] = x.InstanceGroup
			facts.resourceOf[key] = x.InstanceGroup
			facts.accountOf[key] = x.GroupName
			facts.instanceOf[x.ID] = x.InstanceID
		}
	}
	return facts, nil
}

func appendUniqueBatchIDs(ids []string, additional ...string) []string {
	seen := make(map[string]struct{}, len(ids)+len(additional))
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	for _, id := range additional {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func groupIDsFromPermissions(permissions []model.Permission) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, p := range permissions {
		if p.ResourceType == model.ResourceTypeGroup && p.ResourceID != "" {
			if _, ok := seen[p.ResourceID]; !ok {
				seen[p.ResourceID] = struct{}{}
				out = append(out, p.ResourceID)
			}
		}
	}
	return out
}
func groupIDsFromGrants(grants []model.ResourceGrant) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, g := range grants {
		if (g.ResourceType == model.ResourceTypeGroup || g.ResourceType == model.ResourceTypeAccountGroup) && g.ResourceID != "" {
			if _, ok := seen[g.ResourceID]; !ok {
				seen[g.ResourceID] = struct{}{}
				out = append(out, g.ResourceID)
			}
		}
	}
	return out
}

func batchPermissionResourceMatches(permission model.Permission, request BatchAuthorizationRequest, facts batchFacts) bool {
	if !facts.resourceIsActive(request.ResourceType, request.ResourceID) {
		return false
	}
	if resourceTypeMatches(permission.ResourceType, request.ResourceType) && resourceIDMatches(permission.ResourceID, request.ResourceID) {
		return true
	}
	return permission.ResourceType == model.ResourceTypeGroup && facts.permissionGroupMatches(permission.ResourceID, request.ResourceType, request.ResourceID)
}
