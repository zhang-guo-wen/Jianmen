package model

import "time"

const SystemInitializationSetup = "setup"

// SystemInitialization is a database-backed singleton guard for one-time
// system initialization operations.
type SystemInitialization struct {
	Key       string    `gorm:"primaryKey;size:64"`
	CreatedAt time.Time `gorm:"not null"`
}
