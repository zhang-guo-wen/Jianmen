package storage

import (
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestMigrateCreatesAtomicSystemInitializationGuard(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !db.Migrator().HasTable(&model.SystemInitialization{}) {
		t.Fatal("system initialization guard table is missing")
	}

	var migration SchemaMigration
	if err := db.First(&migration, "version = ?", "202607180003").Error; err != nil {
		t.Fatalf("find system initialization migration: %v", err)
	}
}

func TestSystemInitializationMigrationUpgradesExistingUsersWithoutPromotion(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SchemaMigration{}, &model.User{}); err != nil {
		t.Fatalf("create previous schema: %v", err)
	}
	existing := model.User{ID: "existing", Username: "existing", Status: "active"}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("create existing user: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == "202607180003" {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version:   migration.Version,
			Name:      migration.Name,
			AppliedAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", migration.Version, err)
		}
	}
	if db.Migrator().HasTable(&model.SystemInitialization{}) {
		t.Fatal("system initialization guard unexpectedly existed before migration")
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate existing database: %v", err)
	}
	if !db.Migrator().HasTable(&model.SystemInitialization{}) {
		t.Fatal("system initialization guard table is missing after upgrade")
	}
	var stored model.User
	if err := db.First(&stored, "id = ?", existing.ID).Error; err != nil {
		t.Fatalf("load existing user: %v", err)
	}
	if stored.IsSuperAdmin {
		t.Fatal("existing user was implicitly promoted during migration")
	}
	var migrationCount int64
	if err := db.Model(&SchemaMigration{}).Count(&migrationCount).Error; err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != int64(len(migrations)) {
		t.Fatalf("migration count = %d, want %d", migrationCount, len(migrations))
	}
}
