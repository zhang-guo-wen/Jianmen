package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func (s *DBStore) SearchResourceGrants(ctx context.Context, query string) ([]model.ResourceGrant, error) {
	tx := s.db.WithContext(ctx).Model(&model.ResourceGrant{})
	query = strings.ToLower(strings.TrimSpace(query))
	if query != "" {
		like := "%" + query + "%"
		principalIDs, err := s.searchResourceGrantPrincipalIDs(ctx, like)
		if err != nil {
			return nil, err
		}
		resourceIDs, err := s.searchResourceGrantResourceIDs(ctx, like)
		if err != nil {
			return nil, err
		}
		conditions := []string{
			"LOWER(principal_type) LIKE ?",
			"LOWER(principal_id) LIKE ?",
			"LOWER(resource_type) LIKE ?",
			"LOWER(resource_id) LIKE ?",
			"LOWER(effect) LIKE ?",
		}
		args := []any{like, like, like, like, like}
		if len(principalIDs) > 0 {
			conditions = append(conditions, "principal_id IN ?")
			args = append(args, principalIDs)
		}
		if len(resourceIDs) > 0 {
			conditions = append(conditions, "resource_id IN ?")
			args = append(args, resourceIDs)
		}
		tx = tx.Where("("+strings.Join(conditions, " OR ")+")", args...)
	}

	var grants []model.ResourceGrant
	if err := tx.Order("created_at DESC").Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("list resource grants: %w", err)
	}
	return grants, nil
}

func (s *DBStore) FindResourceGrant(ctx context.Context, id string) (model.ResourceGrant, bool, error) {
	var grant model.ResourceGrant
	err := s.db.WithContext(ctx).First(&grant, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ResourceGrant{}, false, nil
	}
	if err != nil {
		return model.ResourceGrant{}, false, fmt.Errorf("find resource grant: %w", err)
	}
	return grant, true, nil
}

func (s *DBStore) CreateResourceGrant(ctx context.Context, grant model.ResourceGrant) (model.ResourceGrant, error) {
	if err := s.db.WithContext(ctx).Create(&grant).Error; err != nil {
		return model.ResourceGrant{}, fmt.Errorf("create resource grant: %w", err)
	}
	return grant, nil
}

func (s *DBStore) EnsureResourceGrant(ctx context.Context, grant model.ResourceGrant) error {
	// 使用互斥锁确保并发安全的检查-创建操作，避免重复创建。
	// 在 PostgreSQL 中，唯一索引包含 deleted_at 列（NULLS NOT DISTINCT）可防止重复；
	// 在 SQLite 中 NULL 视为互异，无法依赖索引，故使用应用层锁。
	s.ensureGrantMu.Lock()
	defer s.ensureGrantMu.Unlock()

	var existing model.ResourceGrant
	result := s.db.WithContext(ctx).
		Where("principal_type = ? AND principal_id = ? AND resource_type = ? AND resource_id = ? AND effect = ?",
			grant.PrincipalType, grant.PrincipalID, grant.ResourceType, grant.ResourceID, grant.Effect).
		Limit(1).Find(&existing)
	if result.Error != nil {
		return fmt.Errorf("ensure resource grant: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Create(&grant).Error; err != nil {
		return fmt.Errorf("ensure resource grant: %w", err)
	}
	return nil
}

func (s *DBStore) DeleteResourceGrant(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Delete(&model.ResourceGrant{}, "id = ?", strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete resource grant: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.New("resource grant not found")
	}
	return nil
}

func (s *DBStore) ResourceGrantPrincipalExists(ctx context.Context, principalType, principalID string) (bool, error) {
	var count int64
	var tx *gorm.DB
	switch principalType {
	case "user":
		tx = s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", principalID)
	case "user_group":
		tx = s.db.WithContext(ctx).Model(&model.UserGroup{}).Where("id = ?", principalID)
	default:
		return false, nil
	}
	if err := tx.Count(&count).Error; err != nil {
		return false, fmt.Errorf("count resource grant principal: %w", err)
	}
	return count > 0, nil
}

func (s *DBStore) ResourceGrantResourceExists(ctx context.Context, resourceType, resourceID string) (bool, error) {
	var count int64
	db := s.db.WithContext(ctx)
	var tx *gorm.DB
	switch resourceType {
	case model.ResourceTypeHost:
		tx = db.Model(&model.Host{}).Where("id = ?", resourceID)
	case model.ResourceTypeHostAccount:
		tx = db.Model(&model.HostAccount{}).Where("id = ?", resourceID)
	case model.ResourceTypeDatabaseInstance:
		tx = db.Model(&model.DatabaseInstance{}).Where("id = ?", resourceID)
	case model.ResourceTypeDatabaseAccount:
		tx = db.Model(&model.DatabaseAccount{}).Where("id = ?", resourceID)
	case model.ResourceTypeApplication:
		tx = db.Model(&model.Application{}).Where("id = ?", resourceID)
	case model.ResourceTypeContainerEndpoint:
		tx = db.Model(&model.ContainerEndpoint{}).Where("id = ?", resourceID)
	case model.ResourceTypePlatformAccount:
		tx = db.Model(&model.PlatformAccount{}).Where("id = ?", resourceID)
	case model.ResourceTypeGroup:
		tx = db.Model(&model.ResourceGroup{}).Where("id = ? AND group_type = ?", resourceID, model.ResourceGroupTypeResource)
	case model.ResourceTypeAccountGroup:
		tx = db.Model(&model.ResourceGroup{}).Where("id = ? AND group_type = ?", resourceID, model.ResourceGroupTypeAccount)
	default:
		return false, nil
	}
	if err := tx.Count(&count).Error; err != nil {
		return false, fmt.Errorf("count resource grant resource: %w", err)
	}
	return count > 0, nil
}

func (s *DBStore) searchResourceGrantPrincipalIDs(ctx context.Context, like string) ([]string, error) {
	var userIDs []string
	if err := s.db.WithContext(ctx).Model(&model.User{}).
		Where("LOWER(username) LIKE ?", like).
		Pluck("id", &userIDs).Error; err != nil {
		return nil, fmt.Errorf("search resource grant users: %w", err)
	}
	var groupIDs []string
	if err := s.db.WithContext(ctx).Model(&model.UserGroup{}).
		Where("LOWER(name) LIKE ?", like).
		Pluck("id", &groupIDs).Error; err != nil {
		return nil, fmt.Errorf("search resource grant user groups: %w", err)
	}
	return uniqueStrings(append(userIDs, groupIDs...)), nil
}

func (s *DBStore) searchResourceGrantResourceIDs(ctx context.Context, like string) ([]string, error) {
	queries := []struct {
		name   string
		query  *gorm.DB
		column string
	}{
		{name: "hosts", query: s.db.WithContext(ctx).Model(&model.Host{}).Where("LOWER(name) LIKE ? OR LOWER(address) LIKE ?", like, like), column: "id"},
		{name: "host accounts", query: s.db.WithContext(ctx).Model(&model.HostAccount{}).Joins("JOIN hosts ON hosts.id = host_accounts.host_id").Where("LOWER(host_accounts.name) LIKE ? OR LOWER(host_accounts.username) LIKE ? OR LOWER(hosts.name) LIKE ? OR LOWER(hosts.address) LIKE ?", like, like, like, like), column: "host_accounts.id"},
		{name: "database instances", query: s.db.WithContext(ctx).Model(&model.DatabaseInstance{}).Where("LOWER(name) LIKE ? OR LOWER(address) LIKE ?", like, like), column: "id"},
		{name: "database accounts", query: s.db.WithContext(ctx).Model(&model.DatabaseAccount{}).Joins("JOIN database_instances ON database_instances.id = database_accounts.instance_id").Where("LOWER(database_accounts.unique_name) LIKE ? OR LOWER(database_accounts.username) LIKE ? OR LOWER(database_instances.name) LIKE ? OR LOWER(database_instances.address) LIKE ?", like, like, like, like), column: "database_accounts.id"},
		{name: "applications", query: s.db.WithContext(ctx).Model(&model.Application{}).Where("LOWER(name) LIKE ? OR LOWER(address) LIKE ? OR LOWER(internal_host) LIKE ?", like, like, like), column: "id"},
		{name: "container endpoints", query: s.db.WithContext(ctx).Model(&model.ContainerEndpoint{}).Where("LOWER(name) LIKE ? OR LOWER(address) LIKE ? OR LOWER(group_name) LIKE ?", like, like, like), column: "id"},
		{name: "platform accounts", query: s.db.WithContext(ctx).Model(&model.PlatformAccount{}).Where("LOWER(name) LIKE ? OR LOWER(platform_name) LIKE ? OR LOWER(username) LIKE ? OR LOWER(url) LIKE ?", like, like, like, like), column: "id"},
		{name: "resource groups", query: s.db.WithContext(ctx).Model(&model.ResourceGroup{}).Where("LOWER(name) LIKE ?", like), column: "id"},
	}

	var resourceIDs []string
	for _, item := range queries {
		var ids []string
		if err := item.query.Pluck(item.column, &ids).Error; err != nil {
			return nil, fmt.Errorf("search resource grant %s: %w", item.name, err)
		}
		resourceIDs = append(resourceIDs, ids...)
	}
	return uniqueStrings(resourceIDs), nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// FindGrantsByPrincipal 查询某主体所有未删除的授权
func (s *DBStore) FindGrantsByPrincipal(ctx context.Context, principalType, principalID string) ([]model.ResourceGrant, error) {
	principalType = strings.ToLower(strings.TrimSpace(principalType))
	principalID = strings.TrimSpace(principalID)
	if principalType == "" || principalID == "" {
		return nil, fmt.Errorf("principal_type and principal_id are required")
	}
	var grants []model.ResourceGrant
	if err := s.db.WithContext(ctx).
		Where("principal_type = ? AND principal_id = ?", principalType, principalID).
		Order("created_at DESC").
		Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("find grants by principal: %w", err)
	}
	return grants, nil
}

// BatchUpsertGrants 批量处理授权：存在则软删旧记录+插入新记录，不存在则直接插入
func (s *DBStore) BatchUpsertGrants(ctx context.Context, grants []model.ResourceGrant, actorID string) (created int, refreshed int, err error) {
	if len(grants) == 0 {
		return 0, 0, nil
	}
	// 在事务中逐条处理
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, grant := range grants {
			// 查找是否已有未删除的相同授权
			var existing model.ResourceGrant
			result := tx.Where(
				"principal_type = ? AND principal_id = ? AND resource_type = ? AND resource_id = ? AND effect = ?",
				grant.PrincipalType, grant.PrincipalID, grant.ResourceType, grant.ResourceID, grant.Effect,
			).First(&existing)

			if result.Error == nil {
				// 已存在：软删除旧记录
				if err := tx.Model(&existing).Updates(map[string]interface{}{
					"deleted_at": time.Now(),
					"updated_by": actorID,
				}).Error; err != nil {
					return fmt.Errorf("soft delete existing grant %s: %w", existing.ID, err)
				}
				refreshed++
			} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return fmt.Errorf("find existing grant: %w", result.Error)
			} else {
				created++
			}

			// 插入新记录
			grant.ID = "" // 使用 BeforeCreate 生成的 ID
			grant.DeletedAt = gorm.DeletedAt{} // 零值 = NULL
			grant.CreatedBy = actorID
			grant.UpdatedBy = actorID
			grant.CreatedAt = time.Now()
			grant.UpdatedAt = time.Now()
			if err := tx.Create(&grant).Error; err != nil {
				return fmt.Errorf("create resource grant: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return created, refreshed, nil
}
