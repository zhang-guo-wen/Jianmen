package rbac

import (
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
	if c == nil || c.db == nil {
		return false, errors.New("rbac: nil database")
	}

	userID = strings.TrimSpace(userID)
	action = strings.TrimSpace(action)
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if userID == "" || action == "" {
		return false, nil
	}

	permissions, err := c.permissionsForUser(userID)
	if err != nil {
		return false, err
	}

	hasAction := false
	hasResourceGrant := resourceType == "" && resourceID == ""
	for _, permission := range permissions {
		if isDeny(permission) && c.matches(permission, action, resourceType, resourceID) {
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
		if resourceType != "" && resourceID != "" && c.resourceMatches(permission, resourceType, resourceID) {
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
	var count int64
	err := c.db.Model(&model.ResourceGroupMember{}).
		Where("group_id = ?", groupID).
		Where("resource_type = ? OR resource_type = ?", resourceType, "*").
		Where("resource_id = ? OR resource_id = ?", resourceID, "*").
		Count(&count).Error
	return err == nil && count > 0
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
