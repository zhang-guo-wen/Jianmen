package storage

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type resourceGrantLogicalKey struct {
	principalType string
	principalID   string
	resourceType  string
	resourceID    string
	effect        string
}

// resourceGrantLogicalUniquenessSchema freezes the schema owned by migration
// 202607190001. The audit columns and active_marker-aware index are installed by
// later migrations.
type resourceGrantLogicalUniquenessSchema struct {
	ID            string     `gorm:"primaryKey;size:64"`
	PrincipalType string     `gorm:"index;index:idx_resource_grants_principal,priority:1;uniqueIndex:uidx_resource_grants_logic,priority:1;size:32;not null"`
	PrincipalID   string     `gorm:"index;index:idx_resource_grants_principal,priority:2;uniqueIndex:uidx_resource_grants_logic,priority:2;size:64;not null"`
	ResourceType  string     `gorm:"index;index:idx_resource_grants_resource,priority:1;uniqueIndex:uidx_resource_grants_logic,priority:3;size:64;not null"`
	ResourceID    string     `gorm:"index;index:idx_resource_grants_resource,priority:2;uniqueIndex:uidx_resource_grants_logic,priority:4;size:64;not null"`
	Effect        string     `gorm:"index;uniqueIndex:uidx_resource_grants_logic,priority:5;size:16;not null;default:allow"`
	ExpiresAt     *time.Time `gorm:"index"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (resourceGrantLogicalUniquenessSchema) TableName() string {
	return "resource_grants"
}

func migrateResourceGrantLogicalUniqueness(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&resourceGrantLogicalUniquenessSchema{}) {
		return tx.AutoMigrate(&resourceGrantLogicalUniquenessSchema{})
	}

	var grants []resourceGrantLogicalUniquenessSchema
	// 历史表此时还没有最终的 active_marker 列，因此使用历史模型迁移。
	if err := tx.Order("created_at, id").Find(&grants).Error; err != nil {
		return fmt.Errorf("load resource grants for deduplication: %w", err)
	}
	winners := make(map[resourceGrantLogicalKey]resourceGrantLogicalUniquenessSchema, len(grants))
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
		if err := tx.Model(&resourceGrantLogicalUniquenessSchema{}).
			Where("id = ?", winner.ID).
			Update("expires_at", winner.ExpiresAt).Error; err != nil {
			return fmt.Errorf("preserve resource grant expiry %s: %w", winner.ID, err)
		}
	}
	if len(duplicates) > 0 {
		if err := tx.Delete(&resourceGrantLogicalUniquenessSchema{}, "id IN ?", duplicates).Error; err != nil {
			return fmt.Errorf("delete duplicate resource grants: %w", err)
		}
	}
	if err := tx.AutoMigrate(&resourceGrantLogicalUniquenessSchema{}); err != nil {
		return fmt.Errorf("add resource grant logical uniqueness: %w", err)
	}
	return nil
}
