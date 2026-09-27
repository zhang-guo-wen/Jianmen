package storage

import (
	"fmt"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const loginCaptchaEnabledMigrationVersion = "202608070001"

// migrateLoginCaptchaEnabled 为存量部署的 system_settings 表补充登录验证码开关列。
// 新部署由初始迁移直接建出该列，存量库已应用过初始迁移，必须由版本化迁移补齐。
func migrateLoginCaptchaEnabled(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&model.SystemSetting{}); err != nil {
		return fmt.Errorf("add login captcha enabled system setting: %w", err)
	}
	return nil
}
