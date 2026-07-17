package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func (s *DBStore) SearchResourceGroups(
	ctx context.Context,
	groupType string,
	query string,
	page int,
	pageSize int,
) ([]model.ResourceGroup, int64, error) {
	buildQuery := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Model(&model.ResourceGroup{})
		if groupType != "" {
			tx = tx.Where("group_type = ?", groupType)
		}
		if query != "" {
			like := "%" + strings.ToLower(query) + "%"
			tx = tx.Where("LOWER(name) LIKE ? OR LOWER(description) LIKE ?", like, like)
		}
		return tx
	}

	var total int64
	if err := buildQuery().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count resource groups: %w", err)
	}
	var groups []model.ResourceGroup
	if err := buildQuery().
		Order("group_type, name").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&groups).Error; err != nil {
		return nil, 0, fmt.Errorf("list resource groups: %w", err)
	}
	return groups, total, nil
}

func (s *DBStore) FindResourceGroup(ctx context.Context, id string) (model.ResourceGroup, bool, error) {
	var group model.ResourceGroup
	err := s.db.WithContext(ctx).First(&group, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ResourceGroup{}, false, nil
	}
	if err != nil {
		return model.ResourceGroup{}, false, fmt.Errorf("find resource group: %w", err)
	}
	return group, true, nil
}

func (s *DBStore) ResourceGroupNameExists(ctx context.Context, name, groupType, excludeID string) (bool, error) {
	tx := s.db.WithContext(ctx).Model(&model.ResourceGroup{}).
		Where("name = ? AND group_type = ?", strings.TrimSpace(name), groupType)
	if excludeID != "" {
		tx = tx.Where("id <> ?", excludeID)
	}
	var count int64
	if err := tx.Count(&count).Error; err != nil {
		return false, fmt.Errorf("count resource group names: %w", err)
	}
	return count > 0, nil
}

func (s *DBStore) ResourceGroupUsage(ctx context.Context, groupType, name string) (map[string]int64, error) {
	usage := map[string]int64{
		"host":        0,
		"database":    0,
		"application": 0,
		"container":   0,
		"platform":    0,
		"account":     0,
	}
	db := s.db.WithContext(ctx)
	if groupType == model.ResourceGroupTypeResource {
		counts := []struct {
			key    string
			model  any
			column string
		}{
			{key: "host", model: &model.Host{}, column: "group_name"},
			{key: "database", model: &model.DatabaseInstance{}, column: "group_name"},
			{key: "application", model: &model.Application{}, column: "app_group"},
			{key: "container", model: &model.ContainerEndpoint{}, column: "group_name"},
		}
		for _, item := range counts {
			var count int64
			if err := db.Model(item.model).Where(item.column+" = ?", name).Count(&count).Error; err != nil {
				return nil, fmt.Errorf("count %s resources in group: %w", item.key, err)
			}
			usage[item.key] = count
		}
		return usage, nil
	}

	counts := []struct {
		key   string
		model any
	}{
		{key: "host_account", model: &model.HostAccount{}},
		{key: "database_account", model: &model.DatabaseAccount{}},
		{key: "platform", model: &model.PlatformAccount{}},
	}
	for _, item := range counts {
		var count int64
		if err := db.Model(item.model).Where("group_name = ?", name).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("count %s resources in group: %w", item.key, err)
		}
		if item.key == "platform" {
			usage["platform"] = count
		}
		usage["account"] += count
	}
	return usage, nil
}

func (s *DBStore) CreateResourceGroup(ctx context.Context, group model.ResourceGroup) (model.ResourceGroup, error) {
	if err := s.db.WithContext(ctx).Create(&group).Error; err != nil {
		return model.ResourceGroup{}, fmt.Errorf("create resource group: %w", err)
	}
	return group, nil
}

func (s *DBStore) UpdateResourceGroup(ctx context.Context, group model.ResourceGroup, oldName string) (model.ResourceGroup, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if oldName != group.Name {
			if err := updateGroupedResourcesTx(tx, group.GroupType, oldName, group.Name); err != nil {
				return err
			}
		}
		if err := tx.Save(&group).Error; err != nil {
			return fmt.Errorf("save resource group: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.ResourceGroup{}, fmt.Errorf("update resource group transaction: %w", err)
	}
	return group, nil
}

func (s *DBStore) DeleteResourceGroup(ctx context.Context, group model.ResourceGroup) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := updateGroupedResourcesTx(tx, group.GroupType, group.Name, ""); err != nil {
			return err
		}
		grantType := model.ResourceTypeGroup
		if group.GroupType == model.ResourceGroupTypeAccount {
			grantType = model.ResourceTypeAccountGroup
		}
		if err := tx.Where("resource_type = ? AND resource_id = ?", grantType, group.ID).
			Delete(&model.ResourceGrant{}).Error; err != nil {
			return fmt.Errorf("delete resource group grants: %w", err)
		}
		if err := tx.Delete(&group).Error; err != nil {
			return fmt.Errorf("delete resource group: %w", err)
		}
		return nil
	})
}

func updateGroupedResourcesTx(tx *gorm.DB, groupType, oldName, newName string) error {
	type groupedModel struct {
		model  any
		column string
	}
	var items []groupedModel
	if groupType == model.ResourceGroupTypeResource {
		items = []groupedModel{
			{model: &model.Host{}, column: "group_name"},
			{model: &model.DatabaseInstance{}, column: "group_name"},
			{model: &model.Application{}, column: "app_group"},
			{model: &model.ContainerEndpoint{}, column: "group_name"},
		}
	} else {
		items = []groupedModel{
			{model: &model.HostAccount{}, column: "group_name"},
			{model: &model.DatabaseAccount{}, column: "group_name"},
			{model: &model.PlatformAccount{}, column: "group_name"},
		}
	}
	for _, item := range items {
		if err := tx.Model(item.model).
			Where(item.column+" = ?", oldName).
			Update(item.column, newName).Error; err != nil {
			return fmt.Errorf("update grouped resources: %w", err)
		}
	}
	return nil
}
