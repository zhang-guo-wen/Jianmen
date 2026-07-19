package main

import (
	"context"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/service"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

func TestSystemSettingsBecomeEffectiveAfterRestartBootstrap(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open metadata database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate metadata database: %v", err)
	}
	repository := store.NewDBStore(db)
	cfg := validManagedSettingsConfig()
	settings, err := bootstrapSystemSettings(context.Background(), cfg, repository)
	if err != nil {
		t.Fatalf("bootstrapSystemSettings() error = %v", err)
	}

	desired := systemSettingsFromConfig(cfg)
	desired.WebRDPEnabled = true
	desired.WebRDPConnectTimeoutSeconds = 42
	desired.WebRDPAllowUnrecorded = true
	desired.RecordingEnabled = false
	desired.RecordingRecordInput = true
	desired.RecordingRecordCommands = false
	desired.RecordingRetentionDays = 7
	desired.RecordingMaxReplayBytes = 2 * 1024 * 1024
	desired.RecordingCleanupBatchSize = 17
	state, err := settings.Update(context.Background(), service.SystemSettingsUpdate{
		Settings: desired, ExpectedRevision: 1,
		ConfirmRisk: true,
		Actor:       service.SystemSettingsActor{ID: "admin-1", Username: "admin"},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !state.PendingRestart || state.EffectiveRevision != 1 || state.Revision != 2 {
		t.Fatalf("updated state = %#v, want revision 2 pending restart", state)
	}
	if cfg.WebRDP.Enabled || cfg.WebRDP.ConnectTimeoutSecs != 15 {
		t.Fatalf("running config changed before restart: %#v", cfg.WebRDP)
	}

	invalidRestartConfig := validManagedSettingsConfig()
	invalidRestartConfig.WebRDP.GuacdAddress = "invalid-address"
	if _, err := bootstrapSystemSettings(
		context.Background(),
		invalidRestartConfig,
		repository,
	); err == nil {
		t.Fatal("bootstrap with invalid effective infrastructure error = nil")
	}
	persisted, found, err := repository.LoadSystemSetting(context.Background())
	if err != nil || !found {
		t.Fatalf("load setting after failed restart: found=%v error=%v", found, err)
	}
	if persisted.Revision != 2 || persisted.AppliedRevision != 1 {
		t.Fatalf("failed restart marked revision applied: %#v", persisted)
	}

	restartedConfig := validManagedSettingsConfig()
	restartedConfig.WebRDP.ConnectTimeoutSecs = 99
	restartedSettings, err := bootstrapSystemSettings(
		context.Background(),
		restartedConfig,
		repository,
	)
	if err != nil {
		t.Fatalf("restart bootstrap error = %v", err)
	}
	restartedState, err := restartedSettings.GetState(context.Background())
	if err != nil {
		t.Fatalf("GetState() after restart error = %v", err)
	}
	if restartedState.PendingRestart || restartedState.EffectiveRevision != 2 {
		t.Fatalf("restarted state = %#v, want revision 2 effective", restartedState)
	}
	if applied := systemSettingsFromConfig(restartedConfig); applied != desired {
		t.Fatalf("persisted settings were not applied: %#v, want %#v", applied, desired)
	}
}

func validManagedSettingsConfig() *config.Config {
	return &config.Config{
		ListenAddr: "127.0.0.1:47102",
		WebRDP: config.WebRDPConfig{
			GuacdAddress: "127.0.0.1:4822", ConnectTimeoutSecs: 15,
			SpoolDir: "data/rdp-spool", GuacdRecordingRoot: "data/rdp-spool",
			LocalDriveRoot: "data/rdp-drive", GuacdDriveRoot: "data/rdp-drive",
		},
		ObjectStorage: config.ObjectStorageConfig{
			Provider: "filesystem", LocalDir: "data/objects",
		},
		Recording: config.RecordingConfig{
			Enabled: true, RecordCommands: true, RetentionDays: 30,
			MaxReplayBytes: 10 * 1024 * 1024 * 1024, CleanupBatchSize: 100,
		},
	}
}
