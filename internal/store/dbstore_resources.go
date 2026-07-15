package store

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
)

func (s *DBStore) syncResource(resourceType, resourceID, name, parentID string) error {
	return s.syncResourceTx(s.db, resourceType, resourceID, name, parentID)
}

func (s *DBStore) syncResourceTx(tx *gorm.DB, resourceType, resourceID, name, parentID string) error {
	if tx == nil {
		return errors.New("sync resource: nil database")
	}
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" || resourceID == "" {
		return errors.New("sync resource: resource type and id are required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = resourceID
	}

	resource := model.Resource{
		Type:       resourceType,
		ResourceID: resourceID,
		Name:       name,
		ParentID:   strings.TrimSpace(parentID),
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "type"}, {Name: "resource_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"name":      resource.Name,
			"parent_id": resource.ParentID,
		}),
	}).Create(&resource).Error
}

func (s *DBStore) deleteResource(resourceType, resourceID string) error {
	return s.deleteResourceTx(s.db, resourceType, resourceID)
}

func (s *DBStore) deleteResourceTx(tx *gorm.DB, resourceType, resourceID string) error {
	if tx == nil {
		return errors.New("delete resource: nil database")
	}
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" || resourceID == "" {
		return nil
	}
	if err := tx.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).
		Delete(&model.ResourceGrant{}).Error; err != nil {
		return err
	}
	if err := tx.Where("type = ? AND resource_id = ?", resourceType, resourceID).
		Delete(&model.Resource{}).Error; err != nil {
		return err
	}
	return nil
}

func hostResourceName(host model.Host) string {
	if name := strings.TrimSpace(host.Name); name != "" {
		return name
	}
	return formatHostAddress(host.Address, host.Port)
}

func hostAccountResourceName(account model.HostAccount) string {
	name := strings.TrimSpace(account.Name)
	if name == "" {
		name = strings.TrimSpace(account.Username)
	}
	host, port := account.Host.Address, account.Host.Port
	if host == "" && account.HostID != "" {
		host = account.HostID
	}
	if name == "" {
		return account.ID
	}
	if host == "" {
		return name
	}
	return name + "@" + formatHostAddress(host, port)
}

func databaseInstanceResourceName(inst model.DatabaseInstance) string {
	if name := strings.TrimSpace(inst.Name); name != "" {
		return name
	}
	return formatHostAddress(inst.Address, inst.Port)
}

func databaseAccountResourceName(account model.DatabaseAccount) string {
	if name := strings.TrimSpace(account.Username); name != "" {
		return name
	}
	return account.ID
}
