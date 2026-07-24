package rbac

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

// ResourceGrantChecker 资源授权检查器
type ResourceGrantChecker struct {
	db *gorm.DB
}

func NewResourceGrantChecker(db *gorm.DB) *ResourceGrantChecker {
	return &ResourceGrantChecker{db: db}
}

// HasGrant 检查用户对资源的 SSH 连接授权
// 资源授权只管 SSH 连接权限，SFTP 不管
// 新增删除主机等操作属于菜单权限，由角色控制
func (c *ResourceGrantChecker) HasGrant(userID, resourceType, resourceID string) (bool, error) {
	return c.HasGrantContext(context.Background(), userID, resourceType, resourceID)
}

func (c *ResourceGrantChecker) HasGrantContext(
	ctx context.Context,
	userID string,
	resourceType string,
	resourceID string,
) (bool, error) {
	if c == nil || c.db == nil {
		return false, nil
	}
	scoped := &ResourceGrantChecker{db: c.db.WithContext(ctx)}

	userID = strings.TrimSpace(userID)
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if userID == "" || resourceType == "" || resourceID == "" {
		return false, nil
	}
	active, err := activeResourceExists(scoped.db, resourceType, resourceID)
	if err != nil {
		return false, err
	}
	if !active {
		return false, nil
	}

	// 收集用户的直接授权
	directGrants, err := scoped.directGrantsForUser(userID)
	if err != nil {
		return false, err
	}

	// 收集用户通过用户组获得的授权
	groupGrants, err := scoped.groupGrantsForUser(userID)
	if err != nil {
		return false, err
	}
	temporaryGrants, err := scoped.temporaryGrantsForUser(userID)
	if err != nil {
		return false, err
	}

	// 合并所有授权
	allGrants := append(directGrants, groupGrants...)
	allGrants = append(allGrants, temporaryGrants...)

	// 检查是否有 deny 授权
	for _, grant := range allGrants {
		if scoped.matchesGrant(grant, resourceType, resourceID) && grant.Effect == model.PermissionEffectDeny {
			return false, nil
		}
	}

	// 检查是否有 allow 授权
	for _, grant := range allGrants {
		if scoped.matchesGrant(grant, resourceType, resourceID) && grant.Effect == model.PermissionEffectAllow {
			return true, nil
		}
	}

	return false, nil
}

// directGrantsForUser 获取用户的直接授权
func (c *ResourceGrantChecker) directGrantsForUser(userID string) ([]model.ResourceGrant, error) {
	now := time.Now().UTC()
	var grants []model.ResourceGrant
	err := c.db.
		Table("resource_grants").
		Select("resource_grants.*").
		Joins("JOIN users ON users.id = resource_grants.principal_id").
		Where("resource_grants.principal_type = ? AND resource_grants.principal_id = ?", "user", userID).
		Where("resource_grants.active_marker = ?", model.ActiveMarkerValue).
		Where("users.active_marker = ?", model.ActiveMarkerValue).
		Where("resource_grants.expires_at IS NULL OR resource_grants.expires_at > ?", now).
		Find(&grants).Error
	return grants, err
}

// groupGrantsForUser 获取用户通过用户组获得的授权
func (c *ResourceGrantChecker) temporaryGrantsForUser(userID string) ([]model.ResourceGrant, error) {
	now := time.Now().UTC()
	var grants []model.TemporaryAccountGrant
	err := c.db.
		Joins("JOIN temporary_accounts ON temporary_accounts.id = temporary_account_grants.temporary_account_id").
		Joins("JOIN users ON users.id = temporary_account_grants.user_id").
		Where("temporary_account_grants.user_id = ?", userID).
		Where("temporary_account_grants.active_marker = ?", model.ActiveMarkerValue).
		Where("temporary_accounts.active_marker = ?", model.ActiveMarkerValue).
		Where("users.active_marker = ?", model.ActiveMarkerValue).
		Where("temporary_account_grants.revoked_at IS NULL").
		Where("temporary_accounts.status = ?", "active").
		Where("temporary_account_grants.starts_at IS NULL OR temporary_account_grants.starts_at <= ?", now).
		Where("temporary_account_grants.expires_at IS NULL OR temporary_account_grants.expires_at > ?", now).
		Where("temporary_accounts.expires_at IS NULL OR temporary_accounts.expires_at > ?", now).
		Find(&grants).Error
	if err != nil {
		return nil, err
	}
	out := make([]model.ResourceGrant, 0, len(grants))
	for _, grant := range grants {
		out = append(out, model.ResourceGrant{PrincipalType: "user", PrincipalID: userID, ResourceType: grant.ResourceType, ResourceID: grant.ResourceID, Effect: model.PermissionEffectAllow, ExpiresAt: grant.ExpiresAt})
	}
	return out, nil
}

func (c *ResourceGrantChecker) groupGrantsForUser(userID string) ([]model.ResourceGrant, error) {
	now := time.Now().UTC()
	var grants []model.ResourceGrant
	err := c.db.
		Table("resource_grants").
		Select("resource_grants.*").
		Joins("JOIN user_group_members ON user_group_members.group_id = resource_grants.principal_id").
		Joins("JOIN user_groups ON user_groups.id = user_group_members.group_id").
		Joins("JOIN users ON users.id = user_group_members.user_id").
		Where("resource_grants.principal_type = ?", "user_group").
		Where("user_group_members.user_id = ?", userID).
		Where("resource_grants.active_marker = ?", model.ActiveMarkerValue).
		Where("user_groups.active_marker = ?", model.ActiveMarkerValue).
		Where("users.active_marker = ?", model.ActiveMarkerValue).
		Where("resource_grants.expires_at IS NULL OR resource_grants.expires_at > ?", now).
		Find(&grants).Error
	return grants, err
}

// matchesGrant 检查授权是否匹配资源
func (c *ResourceGrantChecker) matchesGrant(grant model.ResourceGrant, resourceType, resourceID string) bool {
	if resourceTypeMatches(grant.ResourceType, resourceType) && resourceIDMatches(grant.ResourceID, resourceID) {
		return true
	}
	switch grant.ResourceType {
	case model.ResourceTypeHost:
		return resourceType == model.ResourceTypeHostAccount && c.hostContainsAccount(grant.ResourceID, resourceID)
	case model.ResourceTypeDatabaseInstance:
		return resourceType == model.ResourceTypeDatabaseAccount && c.databaseContainsAccount(grant.ResourceID, resourceID)
	case model.ResourceTypeGroup:
		return c.groupContainsResource(grant.ResourceID, resourceType, resourceID, model.ResourceGroupTypeResource)
	case model.ResourceTypeAccountGroup:
		return c.groupContainsResource(grant.ResourceID, resourceType, resourceID, model.ResourceGroupTypeAccount)
	default:
		return false
	}
}

func (c *ResourceGrantChecker) hostContainsAccount(hostID, accountID string) bool {
	var count int64
	c.db.Model(&model.HostAccount{}).
		Joins("JOIN hosts ON hosts.id = host_accounts.host_id").
		Where("host_accounts.host_id = ? AND host_accounts.id = ?", hostID, accountID).
		Where("host_accounts.active_marker = ? AND hosts.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue).
		Count(&count)
	return count > 0
}

func (c *ResourceGrantChecker) databaseContainsAccount(instanceID, accountID string) bool {
	var count int64
	c.db.Model(&model.DatabaseAccount{}).
		Joins("JOIN database_instances ON database_instances.id = database_accounts.instance_id").
		Where("database_accounts.instance_id = ? AND database_accounts.id = ?", instanceID, accountID).
		Where("database_accounts.active_marker = ? AND database_instances.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue).
		Count(&count)
	return count > 0
}

// groupContainsResource 检查资源组是否包含指定资源
// 通过 hosts.group_name / database_instances.group_name 查找（一对多关系）
func (c *ResourceGrantChecker) groupContainsResource(groupID, resourceType, resourceID, groupType string) bool {
	var group model.ResourceGroup
	if err := c.db.
		Where("id = ? AND group_type = ? AND active_marker = ?", groupID, groupType, model.ActiveMarkerValue).
		First(&group).Error; err != nil {
		return false
	}
	groupName := group.Name

	if groupType == model.ResourceGroupTypeAccount {
		switch resourceType {
		case model.ResourceTypeHostAccount:
			var count int64
			c.db.Model(&model.HostAccount{}).
				Joins("JOIN hosts ON hosts.id = host_accounts.host_id").
				Where("host_accounts.group_name = ? AND host_accounts.id = ?", groupName, resourceID).
				Where("host_accounts.active_marker = ? AND hosts.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue).
				Count(&count)
			return count > 0
		case model.ResourceTypeDatabaseAccount:
			var count int64
			c.db.Model(&model.DatabaseAccount{}).
				Joins("JOIN database_instances ON database_instances.id = database_accounts.instance_id").
				Where("database_accounts.group_name = ? AND database_accounts.id = ?", groupName, resourceID).
				Where("database_accounts.active_marker = ? AND database_instances.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue).
				Count(&count)
			return count > 0
		case model.ResourceTypePlatformAccount:
			var count int64
			c.db.Model(&model.PlatformAccount{}).
				Where("group_name = ? AND id = ? AND active_marker = ?", groupName, resourceID, model.ActiveMarkerValue).
				Count(&count)
			return count > 0
		default:
			return false
		}
	}

	switch {
	case resourceType == model.ResourceTypeHostAccount:
		var count int64
		c.db.Model(&model.HostAccount{}).
			Joins("JOIN hosts ON hosts.id = host_accounts.host_id").
			Where("hosts.group_name = ? AND host_accounts.id = ?", groupName, resourceID).
			Where("host_accounts.active_marker = ? AND hosts.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue).
			Count(&count)
		return count > 0
	case resourceType == model.ResourceTypeDatabaseAccount:
		var count int64
		c.db.Model(&model.DatabaseAccount{}).
			Joins("JOIN database_instances ON database_instances.id = database_accounts.instance_id").
			Where("database_instances.group_name = ? AND database_accounts.id = ?", groupName, resourceID).
			Where("database_accounts.active_marker = ? AND database_instances.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue).
			Count(&count)
		return count > 0
	case resourceType == model.ResourceTypeHost:
		var count int64
		c.db.Model(&model.Host{}).
			Where("group_name = ? AND id = ? AND active_marker = ?", groupName, resourceID, model.ActiveMarkerValue).
			Count(&count)
		return count > 0
	case resourceType == model.ResourceTypeDatabaseInstance:
		var count int64
		c.db.Model(&model.DatabaseInstance{}).
			Where("group_name = ? AND id = ? AND active_marker = ?", groupName, resourceID, model.ActiveMarkerValue).
			Count(&count)
		return count > 0
	case resourceType == model.ResourceTypeApplication:
		var count int64
		c.db.Model(&model.Application{}).
			Where("app_group = ? AND id = ? AND active_marker = ?", groupName, resourceID, model.ActiveMarkerValue).
			Count(&count)
		return count > 0
	case resourceType == model.ResourceTypeContainerEndpoint:
		var count int64
		c.db.Model(&model.ContainerEndpoint{}).
			Where("group_name = ? AND id = ? AND active_marker = ?", groupName, resourceID, model.ActiveMarkerValue).
			Count(&count)
		return count > 0
	default:
		return false
	}
}

// BatchGrantsContext evaluates direct, user-group and temporary grants using a
// bounded set of set-loading queries. It never calls HasGrantContext per item.
func (c *ResourceGrantChecker) BatchGrantsContext(ctx context.Context, userID string, requests []BatchAuthorizationRequest) ([]bool, error) {
	result := make([]bool, len(requests))
	if c == nil || c.db == nil || len(requests) == 0 {
		return result, nil
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return result, nil
	}
	scoped := &ResourceGrantChecker{db: c.db.WithContext(ctx)}
	direct, err := scoped.directGrantsForUser(userID)
	if err != nil {
		return nil, err
	}
	groups, err := scoped.groupGrantsForUser(userID)
	if err != nil {
		return nil, err
	}
	temporary, err := scoped.temporaryGrantsForUser(userID)
	if err != nil {
		return nil, err
	}
	grants := append(append(direct, groups...), temporary...)
	facts, err := (&Checker{db: c.db}).loadBatchFacts(ctx, requests, groupIDsFromGrants(grants))
	if err != nil {
		return nil, err
	}
	for index, request := range requests {
		denied := false
		allowed := false
		for _, grant := range grants {
			if !batchGrantMatches(grant, request, facts) {
				continue
			}
			if grant.Effect == model.PermissionEffectDeny {
				denied = true
				break
			}
			if grant.Effect == model.PermissionEffectAllow {
				allowed = true
			}
		}
		result[index] = allowed && !denied
	}
	return result, nil
}

func batchGrantMatches(grant model.ResourceGrant, request BatchAuthorizationRequest, facts batchFacts) bool {
	if !facts.resourceIsActive(request.ResourceType, request.ResourceID) {
		return false
	}
	if resourceTypeMatches(grant.ResourceType, request.ResourceType) && resourceIDMatches(grant.ResourceID, request.ResourceID) {
		return true
	}
	switch grant.ResourceType {
	case model.ResourceTypeHost:
		return request.ResourceType == model.ResourceTypeHostAccount && facts.hostOf[request.ResourceID] == grant.ResourceID
	case model.ResourceTypeDatabaseInstance:
		return request.ResourceType == model.ResourceTypeDatabaseAccount && facts.instanceOf[request.ResourceID] == grant.ResourceID
	case model.ResourceTypeGroup:
		if request.ResourceType == model.ResourceTypePlatformAccount {
			return false
		}
		return facts.groupMatches(grant.ResourceID, request.ResourceType, request.ResourceID, false)
	case model.ResourceTypeAccountGroup:
		return facts.groupMatches(grant.ResourceID, request.ResourceType, request.ResourceID, true)
	default:
		return false
	}
}
