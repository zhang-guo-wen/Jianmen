package model

import "time"

// UserPreference stores per-user UI and local client preferences.
type UserPreference struct {
	UserID             string `gorm:"primaryKey;size:64"`
	Theme              string `gorm:"size:32;not null;default:system"`
	SSHClient          string `gorm:"size:32"`
	SSHClientPath      string `gorm:"size:512"`
	TerminalFontFamily string `gorm:"size:128"`
	TerminalFontSize   int    `gorm:"not null;default:14"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
