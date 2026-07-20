package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/service"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

func TestSystemSettingsRequiredTLSPreparesManagedIdentityBeforeValidation(t *testing.T) {
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
	dataDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	prepare := func(effective *config.Config) error {
		return prepareDatabaseGatewayTLS(effective, dataDir, logger)
	}

	initial := validManagedSettingsConfig()
	initial.DatabaseGateway.Unified.CertFile = ""
	initial.DatabaseGateway.Unified.KeyFile = ""
	initial.DatabaseGateway.Unified.ServerName = ""
	settings, err := bootstrapSystemSettings(
		context.Background(),
		initial,
		repository,
		prepare,
	)
	if err != nil {
		t.Fatalf("initial bootstrap: %v", err)
	}
	desired := systemSettingsFromConfig(initial)
	desired.DatabaseGatewayClientTLSMode = config.DatabaseGatewayClientTLSModeRequired
	if _, err := settings.Update(context.Background(), service.SystemSettingsUpdate{
		Settings: desired, ExpectedRevision: 1,
	}); err != nil {
		t.Fatalf("store required TLS policy: %v", err)
	}

	restarted := validManagedSettingsConfig()
	restarted.DatabaseGateway.Unified.CertFile = ""
	restarted.DatabaseGateway.Unified.KeyFile = ""
	restarted.DatabaseGateway.Unified.ServerName = ""
	if _, err := bootstrapSystemSettings(
		context.Background(),
		restarted,
		repository,
		prepare,
	); err != nil {
		t.Fatalf("restart bootstrap with required TLS: %v", err)
	}
	if restarted.DatabaseGateway.EffectiveClientTLSMode() !=
		config.DatabaseGatewayClientTLSModeRequired {
		t.Fatalf(
			"effective client TLS mode = %q, want required",
			restarted.DatabaseGateway.EffectiveClientTLSMode(),
		)
	}
	if _, err := os.Stat(restarted.DatabaseGateway.Unified.CertFile); err != nil {
		t.Fatalf("managed certificate was not prepared: %v", err)
	}
	if _, err := os.Stat(restarted.DatabaseGateway.Unified.KeyFile); err != nil {
		t.Fatalf("managed private key was not prepared: %v", err)
	}
}

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
	desired.DatabaseGatewayMode = config.DatabaseGatewayModeIndependent
	desired.DatabaseGatewayClientTLSMode = config.DatabaseGatewayClientTLSModeRequired
	desired.WebRDPEnabled = true
	desired.WebRDPConnectTimeoutSeconds = 42
	desired.WebRDPAllowUnrecorded = true
	desired.RecordingEnabled = false
	desired.RecordingRecordInput = true
	desired.RecordingRecordCommands = false
	desired.RecordingRetentionDays = 7
	desired.RecordingMaxReplayBytes = 2 * 1024 * 1024
	desired.RecordingCleanupBatchSize = 17
	desired.DatabaseMaxClientMessageBytes = 12 * 1024 * 1024
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
	if cfg.DatabaseGateway.EffectiveMode() != config.DatabaseGatewayModeUnified ||
		cfg.DatabaseGateway.EffectiveClientTLSMode() != config.DatabaseGatewayClientTLSModeOptional ||
		cfg.WebRDP.Enabled ||
		cfg.WebRDP.ConnectTimeoutSecs != 15 {
		t.Fatalf(
			"running config changed before restart: database mode=%q web_rdp=%#v",
			cfg.DatabaseGateway.EffectiveMode(),
			cfg.WebRDP,
		)
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
		DatabaseGateway: config.DatabaseGatewayConfig{
			Enabled:       true,
			Mode:          config.DatabaseGatewayModeUnified,
			ClientTLSMode: config.DatabaseGatewayClientTLSModeOptional,
			Unified: config.DatabaseUnifiedListener{
				Enabled: true, Address: "127.0.0.1:33060",
				CertFile: "test.crt", KeyFile: "test.key", ServerName: "localhost",
				DetectionTimeoutMS: 200,
			},
			MySQL: config.DatabaseProtocolListener{
				Enabled: true, Address: "127.0.0.1:33061",
				CertFile: "test.crt", KeyFile: "test.key", ServerName: "localhost",
			},
			MaxClientMessageBytes: config.DefaultDatabaseGatewayMaxClientMessageBytes,
		},
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
