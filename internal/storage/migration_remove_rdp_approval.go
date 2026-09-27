package storage

import (
	"fmt"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const removeRDPApprovalMigrationVersion = "202607210002"

func migrateRemoveRDPApproval(tx *gorm.DB) error {
	if tx.Migrator().HasTable(&model.HostAccount{}) &&
		tx.Migrator().HasColumn(&model.HostAccount{}, "rdp_approval_required") {
		if err := tx.Migrator().DropColumn(&model.HostAccount{}, "rdp_approval_required"); err != nil {
			return fmt.Errorf("drop RDP approval account flag: %w", err)
		}
	}
	if tx.Migrator().HasTable(&model.AuditSession{}) &&
		tx.Migrator().HasColumn(&model.AuditSession{}, "access_request_id") {
		if err := tx.Migrator().DropColumn(&model.AuditSession{}, "access_request_id"); err != nil {
			return fmt.Errorf("drop RDP approval audit link: %w", err)
		}
	}
	if tx.Migrator().HasTable(&accessRequestWebRDPSchema{}) {
		if err := tx.Migrator().DropTable(&accessRequestWebRDPSchema{}); err != nil {
			return fmt.Errorf("drop RDP access requests: %w", err)
		}
	}
	if err := tx.AutoMigrate(&model.HostAccount{}, &model.AuditSession{}); err != nil {
		return fmt.Errorf("restore RDP account and audit indexes: %w", err)
	}
	return nil
}
