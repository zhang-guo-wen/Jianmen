package storage

import (
	"time"

	"gorm.io/gorm"
)

const sshHostIdentityMigrationVersion = "202607200001"

// sshHostIdentityMigrationHost freezes the hosts schema known when this
// migration was authored instead of coupling historical migrations to the
// evolving runtime model.
type sshHostIdentityMigrationHost struct {
	ID                 string `gorm:"primaryKey;size:64"`
	Name               string `gorm:"size:255;not null"`
	Address            string `gorm:"index;index:idx_hosts_address_port,priority:1;size:255;not null"`
	Port               int    `gorm:"index:idx_hosts_address_port,priority:2;not null;default:22"`
	Protocol           string `gorm:"index;size:16;not null;default:ssh"`
	GroupName          string `gorm:"size:128"`
	Remark             string `gorm:"type:text"`
	Status             string `gorm:"index;size:32;not null;default:active"`
	HostKeyFingerprint string `gorm:"size:128"`
	KnownHosts         string `gorm:"type:text"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (sshHostIdentityMigrationHost) TableName() string { return "hosts" }

func migrateSSHHostIdentity(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&sshHostIdentityMigrationHost{}); err != nil {
		return err
	}
	return tx.Model(&sshHostIdentityMigrationHost{}).
		Where("LOWER(COALESCE(protocol, 'ssh')) = ?", "ssh").
		Where("COALESCE(host_key_fingerprint, '') = '' OR COALESCE(known_hosts, '') = ''").
		Update("status", "disabled").Error
}
