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
	resourceGroups map[string]string
	accountGroups  map[string]string
	resourceOf     map[string]string
	accountOf      map[string]string
	hostOf         map[string]string
	instanceOf     map[string]string
}

func (f batchFacts) groupMatches(groupID string, resourceType, resourceID string, account bool) bool {
	key := BatchResourceKey(resourceType, resourceID)
	if account {
		return f.accountGroups[groupID] != "" && f.accountGroups[groupID] == f.accountOf[key]
	}
	return f.resourceGroups[groupID] != "" && f.resourceGroups[groupID] == f.resourceOf[key]
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
	facts := batchFacts{resourceGroups: map[string]string{}, accountGroups: map[string]string{}, resourceOf: map[string]string{}, accountOf: map[string]string{}, hostOf: map[string]string{}, instanceOf: map[string]string{}}
	db := c.db.WithContext(ctx)
	if len(groupIDs) > 0 {
		var groups []model.ResourceGroup
		if err := db.Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
			return facts, err
		}
		for _, group := range groups {
			if group.GroupType == model.ResourceGroupTypeAccount {
				facts.accountGroups[group.ID] = group.Name
			} else {
				facts.resourceGroups[group.ID] = group.Name
			}
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeHost); len(ids) > 0 {
		var rows []model.Host
		if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			facts.resourceOf[BatchResourceKey(model.ResourceTypeHost, x.ID)] = x.GroupName
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeDatabaseInstance); len(ids) > 0 {
		var rows []model.DatabaseInstance
		if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			facts.resourceOf[BatchResourceKey(model.ResourceTypeDatabaseInstance, x.ID)] = x.GroupName
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeApplication); len(ids) > 0 {
		var rows []model.Application
		if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			facts.resourceOf[BatchResourceKey(model.ResourceTypeApplication, x.ID)] = x.AppGroup
		}
	}
	if ids := batchIDs(requests, model.ResourceTypePlatformAccount); len(ids) > 0 {
		var rows []model.PlatformAccount
		if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			key := BatchResourceKey(model.ResourceTypePlatformAccount, x.ID)
			facts.resourceOf[key] = x.GroupName
			facts.accountOf[key] = x.GroupName
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeContainerEndpoint); len(ids) > 0 {
		var rows []model.ContainerEndpoint
		if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			facts.resourceOf[BatchResourceKey(model.ResourceTypeContainerEndpoint, x.ID)] = x.GroupName
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeHostAccount); len(ids) > 0 {
		var rows []struct{ ID, HostID, GroupName, HostGroup string }
		if err := db.Table("host_accounts").Select("host_accounts.id, host_accounts.host_id, host_accounts.group_name, hosts.group_name AS host_group").Joins("JOIN hosts ON hosts.id = host_accounts.host_id").Where("host_accounts.id IN ?", ids).Scan(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			key := BatchResourceKey(model.ResourceTypeHostAccount, x.ID)
			facts.resourceOf[key] = x.HostGroup
			facts.accountOf[key] = x.GroupName
			facts.hostOf[x.ID] = x.HostID
		}
	}
	if ids := batchIDs(requests, model.ResourceTypeDatabaseAccount); len(ids) > 0 {
		var rows []struct{ ID, InstanceID, GroupName, InstanceGroup string }
		if err := db.Table("database_accounts").Select("database_accounts.id, database_accounts.instance_id, database_accounts.group_name, database_instances.group_name AS instance_group").Joins("JOIN database_instances ON database_instances.id = database_accounts.instance_id").Where("database_accounts.id IN ?", ids).Scan(&rows).Error; err != nil {
			return facts, err
		}
		for _, x := range rows {
			key := BatchResourceKey(model.ResourceTypeDatabaseAccount, x.ID)
			facts.resourceOf[key] = x.InstanceGroup
			facts.accountOf[key] = x.GroupName
			facts.instanceOf[x.ID] = x.InstanceID
		}
	}
	return facts, nil
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
	if resourceTypeMatches(permission.ResourceType, request.ResourceType) && resourceIDMatches(permission.ResourceID, request.ResourceID) {
		return true
	}
	return permission.ResourceType == model.ResourceTypeGroup && facts.groupMatches(permission.ResourceID, request.ResourceType, request.ResourceID, false)
}
