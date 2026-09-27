package model

const SystemInitializationSetup = "setup"

// SystemInitialization is a database-backed singleton guard for one-time
// system initialization operations.
type SystemInitialization struct {
	Key       string `gorm:"primaryKey;size:64"`
	FullAudit
}
