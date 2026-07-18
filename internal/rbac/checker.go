package rbac

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

type Checker struct {
	db *gorm.DB
}

func NewChecker(db *gorm.DB) *Checker {
	return &Checker{db: db}
}

func (c *Checker) HasPermission(userID, action, resourceType, resourceID string) (bool, error) {
	return c.HasPermissionContext(context.Background(), userID, action, resourceType, resourceID)
}

func (c *Checker) HasPermissionContext(
	ctx context.Context,
	userID string,
	action string,
	resourceType string,
	resourceID string,
) (bool, error) {
	if c == nil || c.db == nil {
		return false, errors.New("rbac: nil database")
	}
	scoped := &Checker{db: c.db.WithContext(ctx)}

	userID = strings.TrimSpace(userID)
	action = strings.TrimSpace(action)
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if userID == "" || action == "" {
		return false, nil
	}

	permissions, err := scoped.permissionsForUser(userID)
	if err != nil {
		return false, err
	}

	hasAction := false
	hasResourceGrant := resourceType == "" && resourceID == ""
	for _, permission := range permissions {
		if isDeny(permission) && scoped.matches(permission, action, resourceType, resourceID) {
			return false, nil
		}
	}

	for _, permission := range permissions {
		if !isAllow(permission) {
			continue
		}
		if actionMatches(permission.Action, action) && isActionOnly(permission) {
			hasAction = true
			continue
		}
		if resourceType != "" && resourceID != "" && scoped.resourceMatches(permission, resourceType, resourceID) {
			if actionMatches(permission.Action, action) {
				return true, nil
			}
			if permission.Action == "" || permission.Action == "*" {
				hasResourceGrant = true
			}
		}
	}

	return hasAction && hasResourceGrant, nil
}

func (c *Checker) HasDenyContext(
	ctx context.Context,
	userID string,
	action string,
	resourceType string,
	resourceID string,
) (bool, error) {
	if c == nil || c.db == nil {
		return false, errors.New("rbac: nil database")
	}
	scoped := &Checker{db: c.db.WithContext(ctx)}
	userID = strings.TrimSpace(userID)
	action = strings.TrimSpace(action)
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if userID == "" || action == "" {
		return false, nil
	}
	permissions, err := scoped.permissionsForUser(userID)
	if err != nil {
		return false, err
	}
	for _, permission := range permissions {
		if isDeny(permission) && scoped.matches(permission, action, resourceType, resourceID) {
			return true, nil
		}
	}
	return false, nil
}

func (c *Checker) permissionsForUser(userID string) ([]model.Permission, error) {
	now := time.Now().UTC()
	var permissions []model.Permission
	err := c.db.
		Table("permissions").
		Select("permissions.*").
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("user_roles.user_id = ?", userID).
		Where("user_roles.expires_at IS NULL OR user_roles.expires_at > ?", now).
		Where("roles.status = '' OR roles.status = ?", "active").
		Find(&permissions).Error
	return permissions, err
}

func (c *Checker) matches(permission model.Permission, action, resourceType, resourceID string) bool {
	if !actionMatches(permission.Action, action) {
		return false
	}
	if resourceType == "" && resourceID == "" {
		return isActionOnly(permission)
	}
	return isActionOnly(permission) || c.resourceMatches(permission, resourceType, resourceID)
}

func (c *Checker) resourceMatches(permission model.Permission, resourceType, resourceID string) bool {
	switch {
	case resourceTypeMatches(permission.ResourceType, resourceType) && resourceIDMatches(permission.ResourceID, resourceID):
		return true
	case permission.ResourceType == model.ResourceTypeGroup && permission.ResourceID != "":
		return c.groupContainsResource(permission.ResourceID, resourceType, resourceID)
	default:
		return false
	}
}

func (c *Checker) groupContainsResource(groupID, resourceType, resourceID string) bool {
	// 查找资源组名称
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
	case resourceType == model.ResourceTypeApplication:
		var count int64
		c.db.Model(&model.Application{}).
			Where("app_group = ? AND id = ?", groupName, resourceID).
			Count(&count)
		return count > 0
	case resourceType == model.ResourceTypePlatformAccount:
		var count int64
		c.db.Model(&model.PlatformAccount{}).
			Where("group_name = ? AND id = ?", groupName, resourceID).
			Count(&count)
		return count > 0
	default:
		return false
	}
}

func actionMatches(granted, requested string) bool {
	return granted == requested || granted == "*"
}

func resourceTypeMatches(granted, requested string) bool {
	return granted == requested || granted == "*"
}

func resourceIDMatches(granted, requested string) bool {
	return granted == requested || granted == "*"
}

func isActionOnly(permission model.Permission) bool {
	return permission.ResourceType == "" && permission.ResourceID == ""
}

func isAllow(permission model.Permission) bool {
	return permission.Effect == "" || strings.EqualFold(permission.Effect, model.PermissionEffectAllow)
}

func isDeny(permission model.Permission) bool {
	return strings.EqualFold(permission.Effect, model.PermissionEffectDeny)
}
