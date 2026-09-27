package storage

import (
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

type hostBeforeSSHIdentity struct {
	ID       string `gorm:"primaryKey;size:64"`
	Name     string `gorm:"size:255;not null"`
	Address  string `gorm:"size:255;not null"`
	Port     int    `gorm:"not null;default:22"`
	Protocol string `gorm:"size:16;not null;default:ssh"`
	Status   string `gorm:"size:32;not null;default:active"`
}

func (hostBeforeSSHIdentity) TableName() string { return "hosts" }

func TestMigrateSSHHostIdentityAddsHostLevelIdentityColumns(t *testing.T) {
	db := openMigrationSQLite(t)
	if err := db.AutoMigrate(&hostBeforeSSHIdentity{}); err != nil {
		t.Fatalf("create old hosts table: %v", err)
	}
	if err := db.Create(&hostBeforeSSHIdentity{
		ID: "legacy-ssh", Name: "legacy-ssh", Address: "192.0.2.10", Port: 22, Protocol: "ssh", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create legacy SSH host: %v", err)
	}
	if err := db.Create(&hostBeforeSSHIdentity{
		ID: "legacy-rdp", Name: "legacy-rdp", Address: "192.0.2.20", Port: 3389, Protocol: "rdp", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create legacy RDP host: %v", err)
	}
	if err := migrateSSHHostIdentity(db); err != nil {
		t.Fatalf("migrate SSH host identity: %v", err)
	}
	for _, field := range []string{"HostKeyFingerprint", "KnownHosts"} {
		if !db.Migrator().HasColumn(&model.Host{}, field) {
			t.Fatalf("hosts column for %s was not created", field)
		}
	}
	var sshHost, rdpHost model.Host
	if err := db.First(&sshHost, "id = ?", "legacy-ssh").Error; err != nil {
		t.Fatalf("load migrated SSH host: %v", err)
	}
	if err := db.First(&rdpHost, "id = ?", "legacy-rdp").Error; err != nil {
		t.Fatalf("load migrated RDP host: %v", err)
	}
	if sshHost.Status != "disabled" {
		t.Fatalf("legacy SSH host status = %q, want disabled", sshHost.Status)
	}
	if rdpHost.Status != "active" {
		t.Fatalf("legacy RDP host status = %q, want active", rdpHost.Status)
	}
}

func openMigrationSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}
