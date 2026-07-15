package model

import (
	"time"

	"gorm.io/gorm"
)

type DatabaseInstance struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	Name      string    `gorm:"uniqueIndex;size:255;not null" json:"name"`
	Protocol  string    `gorm:"index:idx_database_instances_endpoint,priority:1;size:32;not null;default:mysql" json:"protocol"`
	Address   string    `gorm:"index:idx_database_instances_endpoint,priority:2;size:255;not null" json:"address"`
	Port      int       `gorm:"index:idx_database_instances_endpoint,priority:3;not null;default:3306" json:"port"`
	GroupName string    `gorm:"size:128" json:"group"`
	Remark    string    `gorm:"type:text" json:"remark,omitempty"`
	Status    string    `gorm:"index;size:32;not null;default:active" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DatabaseAccount struct {
	ID          string           `gorm:"primaryKey;size:64" json:"id"`
	InstanceID  string           `gorm:"index;index:idx_database_accounts_instance_username,priority:1;index:idx_database_accounts_instance_status,priority:1;size:64;not null" json:"instance_id"`
	UniqueName  string           `gorm:"uniqueIndex;size:128;not null" json:"unique_name"`
	Username    string           `gorm:"index:idx_database_accounts_instance_username,priority:2;size:128;not null" json:"username"`
	Password    EncryptedField   `gorm:"type:text" json:"-"`
	GroupName   string           `gorm:"size:128" json:"group"`
	Remark      string           `gorm:"type:text" json:"remark,omitempty"`
	ExpiresAt   *time.Time       `gorm:"index;index:idx_database_accounts_status_expires,priority:2" json:"expires_at,omitempty"`
	Status      string           `gorm:"index;index:idx_database_accounts_instance_status,priority:2;index:idx_database_accounts_status_expires,priority:1;size:32;not null;default:active" json:"status"`
	ResourceSeq int              `gorm:"index;not null;default:0" json:"resource_seq"`
	ResourceID  string           `gorm:"uniqueIndex;size:4" json:"resource_id"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Instance    DatabaseInstance `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

func (m *DatabaseInstance) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
func (m *DatabaseAccount) BeforeCreate(_ *gorm.DB) error  { return ensureID(&m.ID) }
