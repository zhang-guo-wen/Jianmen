package store

import (
	"errors"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

// ensureResourceGroup 在给定事务中 upsert 一个资源组记录
// 如果 groupName 为空则跳过
func ensureResourceGroup(tx *gorm.DB, groupName string) error {
	if groupName == "" {
		return nil
	}
	var existing model.ResourceGroup
	err := tx.Where("name = ?", groupName).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&model.ResourceGroup{Name: groupName}).Error
	}
	return err
}
