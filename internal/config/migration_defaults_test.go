package config

import "testing"

func TestDefaultDatabaseUsesVersionedMigrations(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Database.AutoMigrate {
		t.Fatal("default database configuration must not use AutoMigrate")
	}
}
