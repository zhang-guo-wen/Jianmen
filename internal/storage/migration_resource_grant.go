package storage

import (
	"fmt"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

type resourceGrantLogicalKey struct {
	principalType string
	principalID   string
	resourceType  string
	resourceID    string
	effect        string
}

func migrateResourceGrantLogicalUniqueness(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&model.ResourceGrant{}) {
		return tx.AutoMigrate(&model.ResourceGrant{})
	}

	var grants []model.ResourceGrant
	// 使用 Unscoped 绕过 GORM 的 deleted_at IS NULL 自动过滤，
	// 因为旧表可能还没有 deleted_at 列
	if err := tx.Unscoped().Order("created_at, id").Find(&grants).Error; err != nil {
		return fmt.Errorf("load resource grants for deduplication: %w", err)
	}
	winners := make(map[resourceGrantLogicalKey]model.ResourceGrant, len(grants))
	duplicates := make([]string, 0)
	for _, grant := range grants {
		key := resourceGrantLogicalKey{
			principalType: grant.PrincipalType,
			principalID:   grant.PrincipalID,
			resourceType:  grant.ResourceType,
			resourceID:    grant.ResourceID,
			effect:        grant.Effect,
		}
		winner, found := winners[key]
		if !found {
			winners[key] = grant
			continue
		}
		if winner.ExpiresAt != nil &&
			(grant.ExpiresAt == nil || grant.ExpiresAt.After(*winner.ExpiresAt)) {
			winner.ExpiresAt = grant.ExpiresAt
			winners[key] = winner
		}
		duplicates = append(duplicates, grant.ID)
	}
	for _, winner := range winners {
		if err := tx.Unscoped().Model(&model.ResourceGrant{}).
			Where("id = ?", winner.ID).
			Update("expires_at", winner.ExpiresAt).Error; err != nil {
			return fmt.Errorf("preserve resource grant expiry %s: %w", winner.ID, err)
		}
	}
	if len(duplicates) > 0 {
		if err := tx.Unscoped().Delete(&model.ResourceGrant{}, "id IN ?", duplicates).Error; err != nil {
			return fmt.Errorf("delete duplicate resource grants: %w", err)
		}
	}
	if err := tx.AutoMigrate(&model.ResourceGrant{}); err != nil {
		return fmt.Errorf("add resource grant logical uniqueness: %w", err)
	}
	return nil
}
