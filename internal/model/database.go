package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

type DatabaseInstance struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	Name          string    `gorm:"uniqueIndex;size:255;not null" json:"name"`
	Protocol      string    `gorm:"index:idx_database_instances_endpoint,priority:1;size:32;not null;default:mysql" json:"protocol"`
	Address       string    `gorm:"index:idx_database_instances_endpoint,priority:2;size:255;not null" json:"address"`
	Port          int       `gorm:"index:idx_database_instances_endpoint,priority:3;not null;default:3306" json:"port"`
	TLSMode       string    `gorm:"size:16;not null;default:verify-full" json:"tls_mode"`
	TLSServerName string    `gorm:"size:255" json:"tls_server_name,omitempty"`
	TLSCAPEM      string    `gorm:"column:tls_ca_pem;type:text" json:"-"`
	GroupName     string    `gorm:"size:128" json:"group"`
	Remark        string    `gorm:"type:text" json:"remark,omitempty"`
	Status        string    `gorm:"index;size:32;not null;default:active" json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type DatabaseAccount struct {
	ID                      string                         `gorm:"primaryKey;size:64" json:"id"`
	InstanceID              string                         `gorm:"index;index:idx_database_accounts_instance_username,priority:1;uniqueIndex:uidx_database_accounts_instance_username,priority:1;index:idx_database_accounts_instance_status,priority:1;size:64;not null" json:"instance_id"`
	UniqueName              string                         `gorm:"uniqueIndex;size:128;not null" json:"unique_name"`
	Username                string                         `gorm:"index:idx_database_accounts_instance_username,priority:2;uniqueIndex:uidx_database_accounts_instance_username,priority:2;size:128;not null" json:"username"`
	Password                EncryptedField                 `gorm:"type:text" json:"-"`
	Managed                 bool                           `gorm:"index:idx_database_accounts_managed_status,priority:1;not null;default:false;check:chk_database_accounts_managed_consistency,((managed = false AND upstream_host = '' AND provisioning_operation_id IS NULL) OR (managed = true AND upstream_host <> '' AND provisioning_operation_id IS NOT NULL))" json:"-"`
	UpstreamHost            string                         `gorm:"size:255;not null;default:''" json:"-"`
	GroupName               string                         `gorm:"size:128" json:"group"`
	Remark                  string                         `gorm:"type:text" json:"remark,omitempty"`
	ExpiresAt               *time.Time                     `gorm:"index;index:idx_database_accounts_status_expires,priority:2" json:"expires_at,omitempty"`
	Status                  string                         `gorm:"index;index:idx_database_accounts_instance_status,priority:2;index:idx_database_accounts_managed_status,priority:2;index:idx_database_accounts_status_expires,priority:1;size:32;not null;default:active" json:"status"`
	ResourceSeq             int                            `gorm:"index;not null;default:0" json:"resource_seq"`
	ResourceID              string                         `gorm:"uniqueIndex;size:4" json:"resource_id"`
	ProvisioningOperationID *string                        `gorm:"uniqueIndex:uidx_database_accounts_provisioning_operation;size:64" json:"-"`
	CreatedAt               time.Time                      `json:"created_at"`
	UpdatedAt               time.Time                      `json:"updated_at"`
	Instance                DatabaseInstance               `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	ProvisioningOperation   *DatabaseProvisioningOperation `gorm:"foreignKey:ProvisioningOperationID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;" json:"-"`
}

func (m *DatabaseInstance) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
func (m *DatabaseAccount) BeforeCreate(_ *gorm.DB) error  { return ensureID(&m.ID) }

func (m *DatabaseInstance) BeforeDelete(tx *gorm.DB) error {
	if tx == nil || !tx.Migrator().HasTable(&DatabaseProvisioningOperation{}) {
		return nil
	}
	var count int64
	if err := tx.Model(&DatabaseProvisioningOperation{}).
		Where("instance_id = ?", m.ID).
		Count(&count).Error; err != nil {
		return errors.New("check pending database provisioning operations")
	}
	if count != 0 {
		return errors.New("database instance has pending provisioning operations")
	}
	return nil
}
