package model

import (
	"time"

	"gorm.io/gorm"
)

type Host struct {
	ID                 string `gorm:"primaryKey;size:64" json:"id"`
	Name               string `gorm:"size:255;not null" json:"name"`
	Address            string `gorm:"index;index:idx_hosts_address_port,priority:1;size:255;not null" json:"address"`
	Port               int    `gorm:"index:idx_hosts_address_port,priority:2;not null;default:22" json:"port"`
	Protocol           string `gorm:"index;size:16;not null;default:ssh" json:"protocol"`
	GroupName          string `gorm:"size:128" json:"group"`
	Remark             string `gorm:"type:text" json:"remark,omitempty"`
	Status             string `gorm:"index;size:32;not null;default:active" json:"status"`
	HostKeyFingerprint string `gorm:"size:128"`
	KnownHosts         string `gorm:"type:text"`
	FullAudit
}

type HostAccount struct {
	ID                    string         `gorm:"primaryKey;size:64" json:"id"`
	HostID                string         `gorm:"index;index:idx_host_accounts_host_username,priority:1;index:idx_host_accounts_host_status,priority:1;size:64;not null" json:"host_id"`
	Name                  string         `gorm:"size:128;not null;default:''" json:"name"`
	Username              string         `gorm:"index:idx_host_accounts_host_username,priority:2;size:128;not null" json:"username"`
	Domain                string         `gorm:"size:255" json:"domain,omitempty"`
	AuthType              string         `gorm:"size:32" json:"auth_type,omitempty"`
	Password              EncryptedField `gorm:"type:text" json:"-"`
	PrivateKeyPEM         EncryptedField `gorm:"type:text" json:"-"`
	Passphrase            EncryptedField `gorm:"type:text" json:"-"`
	InsecureIgnoreHostKey bool           `gorm:"not null;default:false" json:"insecure_ignore_host_key"`
	HostKeyFingerprint    string         `gorm:"size:128" json:"host_key_fingerprint,omitempty"`
	KnownHostsPath        string         `gorm:"size:255" json:"known_hosts_path,omitempty"`
	RDPSecurity           string         `gorm:"size:32;not null;default:any" json:"rdp_security,omitempty"`
	RDPIgnoreCertificate  bool           `gorm:"not null;default:false" json:"rdp_ignore_certificate"`
	RDPCertFingerprints   string         `gorm:"type:text" json:"rdp_cert_fingerprints,omitempty"`
	RDPClipboardRead      bool           `gorm:"not null;default:false" json:"rdp_clipboard_read"`
	RDPClipboardWrite     bool           `gorm:"not null;default:false" json:"rdp_clipboard_write"`
	RDPFileUpload         bool           `gorm:"not null;default:false" json:"rdp_file_upload"`
	RDPFileDownload       bool           `gorm:"not null;default:false" json:"rdp_file_download"`
	RDPDriveMapping       bool           `gorm:"not null;default:false" json:"rdp_drive_mapping"`
	Status                string         `gorm:"index;index:idx_host_accounts_host_status,priority:2;index:idx_host_accounts_status_expires,priority:1;size:32;not null;default:active" json:"status"`
	ResourceSeq           int            `gorm:"index;not null;default:0" json:"resource_seq"`
	ResourceID            string         `gorm:"uniqueIndex:idx_host_accounts_resource_id_active,priority:1;size:4" json:"resource_id"`
	GroupName             string         `gorm:"size:128" json:"group"`
	Remark                string         `gorm:"type:text" json:"remark,omitempty"`
	ExpiresAt             *time.Time     `gorm:"index;index:idx_host_accounts_status_expires,priority:2" json:"expires_at"`
	FullAudit
	Host Host `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}

func (m *Host) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&m.ID)
}
func (m *HostAccount) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&m.ID)
}
