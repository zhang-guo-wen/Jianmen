package model

import "time"

// AdminSession is the server-side identity for a browser session.  Raw cookie
// values never reach persistence; only their SHA-256 hashes are stored.
type AdminSession struct {
	ID         string     `gorm:"primaryKey;size:64"`
	UserID     string     `gorm:"index;size:64;not null"`
	SecretHash string     `gorm:"uniqueIndex:idx_admin_sessions_secret_hash_active,priority:1;size:64;not null"`
	CSRFHash   string     `gorm:"size:64;not null"`
	ExpiresAt  time.Time  `gorm:"index;not null"`
	RevokedAt  *time.Time `gorm:"index"`
	FullAudit
}

func (AdminSession) TableName() string { return "admin_sessions" }
