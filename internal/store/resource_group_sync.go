package store

import (
	"errors"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

// ensureResourceGroup 在给定事务中 upsert 一个资源分组记录（groupType: "resource"）
func ensureResourceGroup(tx *gorm.DB, groupName string) error {
	return ensureGroupByType(tx, groupName, model.ResourceGroupTypeResource)
}

// ensureAccountGroup 在给定事务中 upsert 一个账号分组记录（groupType: "account"）
func ensureAccountGroup(tx *gorm.DB, groupName string) error {
	return ensureGroupByType(tx, groupName, model.ResourceGroupTypeAccount)
}

func ensureGroupByType(tx *gorm.DB, groupName, groupType string) error {
	if groupName == "" {
		return nil
	}
	var existing model.ResourceGroup
	err := tx.Scopes(ActiveScope).Where("name = ? AND group_type = ?", groupName, groupType).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&model.ResourceGroup{Name: groupName, GroupType: groupType}).Error
	}
	return err
}
