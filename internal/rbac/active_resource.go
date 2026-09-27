package rbac

import (
	"gorm.io/gorm"

	"jianmen/internal/model"
)

// activeResourceExists is the authorization boundary for concrete business
// resources. Unknown resource types retain their existing permission semantics.
// Known account resources require both the account and its parent to be active.
func activeResourceExists(db *gorm.DB, resourceType, resourceID string) (bool, error) {
	if db == nil {
		return false, nil
	}

	var query *gorm.DB
	switch resourceType {
	case model.ResourceTypeHost:
		query = db.Table("hosts").
			Where("hosts.id = ? AND hosts.active_marker = ?", resourceID, model.ActiveMarkerValue)
	case model.ResourceTypeHostAccount:
		query = db.Table("host_accounts").
			Joins("JOIN hosts ON hosts.id = host_accounts.host_id").
			Where("host_accounts.id = ?", resourceID).
			Where("host_accounts.active_marker = ? AND hosts.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue)
	case model.ResourceTypeDatabaseInstance:
		query = db.Table("database_instances").
			Where("database_instances.id = ? AND database_instances.active_marker = ?", resourceID, model.ActiveMarkerValue)
	case model.ResourceTypeDatabaseAccount:
		query = db.Table("database_accounts").
			Joins("JOIN database_instances ON database_instances.id = database_accounts.instance_id").
			Where("database_accounts.id = ?", resourceID).
			Where("database_accounts.active_marker = ? AND database_instances.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue)
	case model.ResourceTypeApplication:
		query = db.Table("applications").
			Where("applications.id = ? AND applications.active_marker = ?", resourceID, model.ActiveMarkerValue)
	case model.ResourceTypeContainerEndpoint:
		query = db.Table("container_endpoints").
			Where("container_endpoints.id = ? AND container_endpoints.active_marker = ?", resourceID, model.ActiveMarkerValue)
	case model.ResourceTypePlatformAccount:
		query = db.Table("platform_accounts").
			Where("platform_accounts.id = ? AND platform_accounts.active_marker = ?", resourceID, model.ActiveMarkerValue)
	case model.ResourceTypeGroup, model.ResourceTypeAccountGroup:
		query = db.Table("resource_groups").
			Where("resource_groups.id = ? AND resource_groups.active_marker = ?", resourceID, model.ActiveMarkerValue)
	default:
		return true, nil
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func auditedResourceType(resourceType string) bool {
	switch resourceType {
	case model.ResourceTypeHost,
		model.ResourceTypeHostAccount,
		model.ResourceTypeDatabaseInstance,
		model.ResourceTypeDatabaseAccount,
		model.ResourceTypeApplication,
		model.ResourceTypeContainerEndpoint,
		model.ResourceTypePlatformAccount,
		model.ResourceTypeGroup,
		model.ResourceTypeAccountGroup:
		return true
	default:
		return false
	}
}
