package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/objectstore"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

func bootstrapSystemSettings(
	ctx context.Context,
	cfg *config.Config,
	repository *store.DBStore,
) (*service.SystemSettingsService, error) {
	if cfg == nil || repository == nil {
		return nil, fmt.Errorf("system settings runtime dependencies are required")
	}
	settings, err := service.NewSystemSettingsService(repository)
	if err != nil {
		return nil, fmt.Errorf("initialize system settings service: %w", err)
	}
	prepared, err := settings.PrepareBootstrap(ctx, systemSettingsFromConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("prepare system settings: %w", err)
	}
	projected := *cfg
	applySystemSettings(&projected, prepared.Settings)
	if err := projected.Validate(); err != nil {
		return nil, fmt.Errorf("validate effective system settings: %w", err)
	}
	state, err := settings.ActivateBootstrap(ctx, prepared)
	if err != nil {
		return nil, fmt.Errorf("activate system settings: %w", err)
	}
	applySystemSettings(cfg, state.Effective)
	return settings, nil
}

func systemSettingsFromConfig(cfg *config.Config) service.SystemSettings {
	return service.SystemSettings{
		WebRDPEnabled:               cfg.WebRDP.Enabled,
		WebRDPConnectTimeoutSeconds: cfg.WebRDP.ConnectTimeoutSecs,
		WebRDPAllowUnrecorded:       cfg.WebRDP.AllowUnrecorded,
		RecordingEnabled:            cfg.Recording.Enabled,
		RecordingRecordInput:        cfg.Recording.RecordInput,
		RecordingRecordCommands:     cfg.Recording.RecordCommands,
		RecordingRetentionDays:      cfg.Recording.RetentionDays,
		RecordingMaxReplayBytes:     cfg.Recording.MaxReplayBytes,
		RecordingCleanupBatchSize:   cfg.Recording.CleanupBatchSize,
	}
}

func applySystemSettings(cfg *config.Config, settings service.SystemSettings) {
	cfg.WebRDP.Enabled = settings.WebRDPEnabled
	cfg.WebRDP.ConnectTimeoutSecs = settings.WebRDPConnectTimeoutSeconds
	cfg.WebRDP.AllowUnrecorded = settings.WebRDPAllowUnrecorded
	cfg.Recording.Enabled = settings.RecordingEnabled
	cfg.Recording.RecordInput = settings.RecordingRecordInput
	cfg.Recording.RecordCommands = settings.RecordingRecordCommands
	cfg.Recording.RetentionDays = settings.RecordingRetentionDays
	cfg.Recording.MaxReplayBytes = settings.RecordingMaxReplayBytes
	cfg.Recording.CleanupBatchSize = settings.RecordingCleanupBatchSize
}

func newSystemSettingsDiagnostics(
	cfg *config.Config,
	objects objectstore.Store,
) (*service.SystemSettingsDiagnosticService, error) {
	infrastructure := service.SystemSettingsRuntimeInfrastructure{
		GuacdAddress:       cfg.WebRDP.GuacdAddress,
		SpoolDir:           cfg.WebRDP.SpoolDir,
		GuacdRecordingRoot: cfg.WebRDP.GuacdRecordingRoot,
		LocalDriveRoot:     cfg.WebRDP.LocalDriveRoot,
		GuacdDriveRoot:     cfg.WebRDP.GuacdDriveRoot,
		ReplayDir:          cfg.ReplayDir,
		ObjectStorage: service.SystemSettingsObjectStorageInfrastructure{
			Provider: cfg.ObjectStorage.Provider, LocalDir: cfg.ObjectStorage.LocalDir,
			Endpoint: cfg.ObjectStorage.Endpoint, Bucket: cfg.ObjectStorage.Bucket,
			Region: cfg.ObjectStorage.Region, Prefix: cfg.ObjectStorage.Prefix,
			Secure: cfg.ObjectStorage.Secure, PathStyle: cfg.ObjectStorage.PathStyle,
			AutoCreateBucket:          cfg.ObjectStorage.AutoCreateBucket,
			AccessKeyIDConfigured:     strings.TrimSpace(cfg.ObjectStorage.AccessKeyID) != "",
			SecretAccessKeyConfigured: strings.TrimSpace(cfg.ObjectStorage.SecretAccessKey) != "",
			SessionTokenConfigured:    strings.TrimSpace(cfg.ObjectStorage.SessionToken) != "",
		},
	}
	timeout := time.Duration(cfg.WebRDP.ConnectTimeoutSecs) * time.Second
	diagnostics, err := service.NewSystemSettingsDiagnosticService(
		infrastructure,
		objects,
		timeout,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize system settings diagnostics: %w", err)
	}
	return diagnostics, nil
}
