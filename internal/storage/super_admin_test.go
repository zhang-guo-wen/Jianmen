package storage

import (
	"errors"
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

func TestBootstrapMetadataPersistsOnlyExplicitConfigSuperAdministrator(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	cfg := &config.Config{
		Users: []config.User{
			{ID: "u-normal", Username: "normal"},
			{ID: "u-admin", Username: "admin", SuperAdmin: true},
		},
	}

	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("bootstrap metadata: %v", err)
	}

	var normal model.User
	if err := db.First(&normal, "id = ?", "u-normal").Error; err != nil {
		t.Fatalf("find normal user: %v", err)
	}
	if normal.IsSuperAdmin {
		t.Fatal("normal config user was implicitly promoted to super administrator")
	}

	var admin model.User
	if err := db.First(&admin, "id = ?", "u-admin").Error; err != nil {
		t.Fatalf("find super administrator: %v", err)
	}
	if !admin.IsSuperAdmin {
		t.Fatal("explicit config super administrator flag was not persisted")
	}
}

func TestMigrateAddsUserSuperAdministratorColumn(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !db.Migrator().HasColumn(&model.User{}, "is_super_admin") {
		t.Fatal("users.is_super_admin column is missing")
	}
	var applied SchemaMigration
	if err := db.First(&applied, "version = ?", "202607180001").Error; err != nil {
		t.Fatalf("find super administrator migration: %v", err)
	}
}

func TestBootstrapMetadataDoesNotOverwritePersistedSuperAdministratorState(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	cfg := &config.Config{
		Users: []config.User{{ID: "u-admin", Username: "admin", SuperAdmin: true}},
	}
	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("initial bootstrap: %v", err)
	}

	cfg.Users[0].SuperAdmin = false
	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("repeat bootstrap: %v", err)
	}
	var persisted model.User
	if err := db.First(&persisted, "id = ?", "u-admin").Error; err != nil {
		t.Fatalf("find persisted administrator: %v", err)
	}
	if !persisted.IsSuperAdmin {
		t.Fatal("repeat bootstrap overwrote database super administrator state")
	}
}

func TestBootstrapMetadataRejectsUsersWithoutActiveSuperAdministratorAndRollsBack(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	err = BootstrapMetadata(db, &config.Config{
		Users: []config.User{{ID: "u-normal", Username: "normal"}},
	})
	if !errors.Is(err, ErrNoActiveSuperAdmin) {
		t.Fatalf("BootstrapMetadata error = %v, want ErrNoActiveSuperAdmin", err)
	}
	var userCount int64
	if err := db.Model(&model.User{}).Count(&userCount).Error; err != nil {
		t.Fatalf("count rolled back users: %v", err)
	}
	if userCount != 0 {
		t.Fatalf("user count after rejected bootstrap = %d, want 0", userCount)
	}
}

func TestBootstrapMetadataUsesExplicitConfigSuperAdministratorForDatabaseRecovery(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.User{
		ID: "u-recovery", Username: "recovery", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create ordinary database user: %v", err)
	}

	if err := BootstrapMetadata(db, &config.Config{
		Users: []config.User{{ID: "u-recovery", Username: "recovery", SuperAdmin: true}},
	}); err != nil {
		t.Fatalf("recover super administrator: %v", err)
	}
	var recovered model.User
	if err := db.First(&recovered, "id = ?", "u-recovery").Error; err != nil {
		t.Fatalf("find recovered administrator: %v", err)
	}
	if !recovered.IsSuperAdmin {
		t.Fatal("explicit recovery seed was not persisted to the database")
	}
}

func TestBootstrapMetadataDoesNotLetConfigOverrideExistingDatabaseAuthority(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.User{
		ID: "u-authority", Username: "authority", Status: "active", IsSuperAdmin: true,
	}).Error; err != nil {
		t.Fatalf("create database administrator: %v", err)
	}

	if err := BootstrapMetadata(db, &config.Config{
		Users: []config.User{{ID: "u-config", Username: "config", SuperAdmin: true}},
	}); err != nil {
		t.Fatalf("bootstrap with existing database authority: %v", err)
	}
	var configured model.User
	if err := db.First(&configured, "id = ?", "u-config").Error; err != nil {
		t.Fatalf("find configured user: %v", err)
	}
	if configured.IsSuperAdmin {
		t.Fatal("config promoted a user while an active database super administrator existed")
	}
}

func TestBootstrapMetadataRejectsExpiredSuperAdministrator(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	if err := db.Create(&model.User{
		ID: "u-expired", Username: "expired", Status: "active", IsSuperAdmin: true, ExpiresAt: &expiredAt,
	}).Error; err != nil {
		t.Fatalf("create expired administrator: %v", err)
	}

	err = BootstrapMetadata(db, &config.Config{})
	if !errors.Is(err, ErrNoActiveSuperAdmin) {
		t.Fatalf("BootstrapMetadata error = %v, want ErrNoActiveSuperAdmin", err)
	}
}

func TestBootstrapMetadataDoesNotCountInactiveSuperAdministrator(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.User{
		ID: "u-inactive-admin", Username: "inactive-admin", Status: "active", IsSuperAdmin: true,
	}).Error; err != nil {
		t.Fatalf("create super administrator: %v", err)
	}
	if err := db.Model(&model.User{}).
		Where("id = ?", "u-inactive-admin").
		Update("active_marker", nil).Error; err != nil {
		t.Fatalf("mark super administrator inactive: %v", err)
	}

	err = BootstrapMetadata(db, &config.Config{})
	if !errors.Is(err, ErrNoActiveSuperAdmin) {
		t.Fatalf("BootstrapMetadata error = %v, want ErrNoActiveSuperAdmin", err)
	}
}

func TestBootstrapMetadataRestoresConfigUserAndReplacesInactivePermanentSession(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	cfg := &config.Config{
		Users: []config.User{{ID: "u-restored", Username: "restored", SuperAdmin: true}},
	}
	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("initial bootstrap: %v", err)
	}
	inactiveAt := time.Now().UTC().Add(-time.Minute)
	if err := db.Model(&model.User{}).
		Where("id = ?", "u-restored").
		UpdateColumns(map[string]any{
			"active_marker": nil,
			"status":        "disabled",
			"updated_at":    inactiveAt,
		}).Error; err != nil {
		t.Fatalf("mark config user inactive: %v", err)
	}
	if err := db.Model(&model.UserSession{}).
		Where("user_id = ? AND type = ?", "u-restored", "permanent").
		Updates(map[string]any{
			"active_marker": nil,
			"status":        "disabled",
		}).Error; err != nil {
		t.Fatalf("mark permanent session inactive: %v", err)
	}

	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("restore config user: %v", err)
	}

	var restored model.User
	if err := db.First(&restored, "id = ?", "u-restored").Error; err != nil {
		t.Fatalf("load restored config user: %v", err)
	}
	if restored.ActiveMarker == nil || *restored.ActiveMarker != model.ActiveMarkerValue {
		t.Fatalf("restored active marker = %v, want %d", restored.ActiveMarker, model.ActiveMarkerValue)
	}
	if restored.Status != "active" {
		t.Fatalf("restored status = %q, want active", restored.Status)
	}
	if !restored.UpdatedAt.After(inactiveAt) {
		t.Fatalf("restored updated_at = %v, want after inactive time %v", restored.UpdatedAt, inactiveAt)
	}
	if !restored.IsSuperAdmin {
		t.Fatal("restored config user lost super administrator authority")
	}

	var activePermanentSessions int64
	if err := db.Model(&model.UserSession{}).
		Where(
			"user_id = ? AND type = ? AND active_marker = ?",
			"u-restored",
			"permanent",
			model.ActiveMarkerValue,
		).
		Count(&activePermanentSessions).Error; err != nil {
		t.Fatalf("count active permanent sessions: %v", err)
	}
	if activePermanentSessions != 1 {
		t.Fatalf("active permanent sessions = %d, want 1", activePermanentSessions)
	}
	var allPermanentSessions int64
	if err := db.Model(&model.UserSession{}).
		Where("user_id = ? AND type = ?", "u-restored", "permanent").
		Count(&allPermanentSessions).Error; err != nil {
		t.Fatalf("count all permanent sessions: %v", err)
	}
	if allPermanentSessions != 2 {
		t.Fatalf("all permanent sessions = %d, want retained history plus replacement", allPermanentSessions)
	}
}

func TestRepairUserSessionsAndSequenceFloorIgnoreInactiveHistory(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.User{
		ID: "u-session-repair", Username: "session-repair", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create session parent user: %v", err)
	}

	historicalDuplicate := model.UserSession{
		ID:         "a-historical-duplicate",
		UserID:     "u-session-repair",
		SessionSeq: 7,
		SessionID:  util.EncodeBase62Padded(7, 5),
		Type:       "temporary",
		Status:     "active",
	}
	if err := db.Create(&historicalDuplicate).Error; err != nil {
		t.Fatalf("create historical duplicate session: %v", err)
	}
	if err := db.Model(&model.UserSession{}).
		Where("id = ?", historicalDuplicate.ID).
		Updates(map[string]any{
			"active_marker": nil,
			"status":        "disabled",
		}).Error; err != nil {
		t.Fatalf("mark duplicate session inactive: %v", err)
	}

	historicalHigh := model.UserSession{
		ID:         "b-historical-high",
		UserID:     "u-session-repair",
		SessionSeq: 900,
		SessionID:  util.EncodeBase62Padded(900, 5),
		Type:       "temporary",
		Status:     "active",
	}
	if err := db.Create(&historicalHigh).Error; err != nil {
		t.Fatalf("create high historical session: %v", err)
	}
	if err := db.Model(&model.UserSession{}).
		Where("id = ?", historicalHigh.ID).
		Updates(map[string]any{
			"active_marker": nil,
			"status":        "disabled",
		}).Error; err != nil {
		t.Fatalf("mark high session inactive: %v", err)
	}

	active := model.UserSession{
		ID:         "z-active-session",
		UserID:     "u-session-repair",
		SessionSeq: 7,
		SessionID:  util.EncodeBase62Padded(7, 5),
		Type:       "permanent",
		Status:     "active",
	}
	if err := db.Create(&active).Error; err != nil {
		t.Fatalf("create active session: %v", err)
	}

	if err := repairUserSessions(db); err != nil {
		t.Fatalf("repair user sessions: %v", err)
	}

	var repaired model.UserSession
	if err := db.First(&repaired, "id = ?", active.ID).Error; err != nil {
		t.Fatalf("load active session: %v", err)
	}
	if repaired.SessionSeq != active.SessionSeq || repaired.SessionID != active.SessionID {
		t.Fatalf(
			"active session changed from (%d, %q) to (%d, %q)",
			active.SessionSeq,
			active.SessionID,
			repaired.SessionSeq,
			repaired.SessionID,
		)
	}
	var sequence model.ResourceSequence
	if err := db.First(&sequence, "name = ?", SequenceUserSession).Error; err != nil {
		t.Fatalf("load user session sequence: %v", err)
	}
	if sequence.NextValue != active.SessionSeq+1 {
		t.Fatalf(
			"user session next value = %d, want %d",
			sequence.NextValue,
			active.SessionSeq+1,
		)
	}
}
