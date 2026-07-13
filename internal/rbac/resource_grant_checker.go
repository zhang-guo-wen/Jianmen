package rbac

import (
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
	if c == nil || c.db == nil {
		return false, nil
	}

	userID = strings.TrimSpace(userID)
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if userID == "" || resourceType == "" || resourceID == "" {
		return false, nil
	}

	// 收集用户的直接授权
	directGrants, err := c.directGrantsForUser(userID)
	if err != nil {
		return false, err
	}

	// 收集用户通过用户组获得的授权
	groupGrants, err := c.groupGrantsForUser(userID)
	if err != nil {
		return false, err
	}

	// 合并所有授权
	allGrants := append(directGrants, groupGrants...)

	// 检查是否有 deny 授权
	for _, grant := range allGrants {
		if c.matchesGrant(grant, resourceType, resourceID) && grant.Effect == model.PermissionEffectDeny {
			return false, nil
		}
	}

	// 检查是否有 allow 授权
	for _, grant := range allGrants {
		if c.matchesGrant(grant, resourceType, resourceID) && grant.Effect == model.PermissionEffectAllow {
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
		Where("principal_type = ? AND principal_id = ?", "user", userID).
		Where("expires_at IS NULL OR expires_at > ?", now).
		Find(&grants).Error
	return grants, err
}

// groupGrantsForUser 获取用户通过用户组获得的授权
func (c *ResourceGrantChecker) groupGrantsForUser(userID string) ([]model.ResourceGrant, error) {
	now := time.Now().UTC()
	var grants []model.ResourceGrant
	err := c.db.
		Table("resource_grants").
		Select("resource_grants.*").
		Joins("JOIN user_group_members ON user_group_members.group_id = resource_grants.principal_id").
		Where("resource_grants.principal_type = ?", "user_group").
		Where("user_group_members.user_id = ?", userID).
		Where("resource_grants.expires_at IS NULL OR resource_grants.expires_at > ?", now).
		Find(&grants).Error
	return grants, err
}

// matchesGrant 检查授权是否匹配资源
func (c *ResourceGrantChecker) matchesGrant(grant model.ResourceGrant, resourceType, resourceID string) bool {
	// 检查资源类型
	if !resourceTypeMatches(grant.ResourceType, resourceType) {
		return false
	}

	// 检查资源ID
	if !resourceIDMatches(grant.ResourceID, resourceID) {
		// 如果是资源组，检查资源是否在组内
		if grant.ResourceType == model.ResourceTypeGroup {
			return c.groupContainsResource(grant.ResourceID, resourceType, resourceID)
		}
		return false
	}

	return true
}

// groupContainsResource 检查资源组是否包含指定资源
// 通过 hosts.group_name / database_instances.group_name 查找（一对多关系）
func (c *ResourceGrantChecker) groupContainsResource(groupID, resourceType, resourceID string) bool {
	var group model.ResourceGroup
	if err := c.db.First(&group, "id = ?", groupID).Error; err != nil {
		return false
	}
	groupName := group.Name

	switch {
	case resourceType == model.ResourceTypeHostAccount:
		var count int64
		c.db.Model(&model.HostAccount{}).
			Joins("JOIN hosts ON hosts.id = host_accounts.host_id").
			Where("hosts.group_name = ? AND host_accounts.id = ?", groupName, resourceID).
			Count(&count)
		return count > 0
	case resourceType == model.ResourceTypeDatabaseAccount:
		var count int64
		c.db.Model(&model.DatabaseAccount{}).
			Joins("JOIN database_instances ON database_instances.id = database_accounts.instance_id").
			Where("database_instances.group_name = ? AND database_accounts.id = ?", groupName, resourceID).
			Count(&count)
		return count > 0
	case resourceType == model.ResourceTypeHost:
		var count int64
		c.db.Model(&model.Host{}).
			Where("group_name = ? AND id = ?", groupName, resourceID).
			Count(&count)
		return count > 0
	case resourceType == model.ResourceTypeDatabaseInstance:
		var count int64
		c.db.Model(&model.DatabaseInstance{}).
			Where("group_name = ? AND id = ?", groupName, resourceID).
			Count(&count)
		return count > 0
	default:
		return false
	}
}
