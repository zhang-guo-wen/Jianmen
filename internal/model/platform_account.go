package model

import "time"

// PlatformAccount represents a credential for an external web platform
// (Jenkins, GitLab, Jira, etc.).
type PlatformAccount struct {
	ID           string         `gorm:"primaryKey;size:64" json:"id"`
	Name         string         `gorm:"size:255" json:"name"`
	PlatformName string         `gorm:"index;size:128;not null" json:"platform_name"`
	URL          string         `gorm:"size:512" json:"url,omitempty"`
	GroupName    string         `gorm:"size:128" json:"group,omitempty"`
	Username     string         `gorm:"size:255;not null" json:"username"`
	Password     EncryptedField `gorm:"type:text" json:"-"`
	// HasPassword is populated by credential-free metadata queries. It is not
	// persisted and allows list/detail paths to avoid loading the password.
	HasPassword bool       `gorm:"->;-:migration" json:"-"`
	Remark      string     `gorm:"type:text" json:"remark,omitempty"`
	OwnerID     string     `gorm:"index;size:64;not null" json:"owner_id"`
	Status      string     `gorm:"index;size:32;not null;default:active" json:"status"`
	ExpiresAt   *time.Time `gorm:"index" json:"expires_at,omitempty"`
	FullAudit
	Owner       User       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}
