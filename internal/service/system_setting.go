package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

const (
	defaultSystemSettingRevisionLimit = 20
	maxSystemSettingRevisionLimit     = 100
)

var (
	ErrInvalidSystemSettings                  = errors.New("invalid system settings")
	ErrSystemSettingsRiskConfirmationRequired = errors.New("system settings risk confirmation required")
	ErrSystemSettingsRevisionConflict         = errors.New("system settings revision conflict")
	ErrSystemSettingsNotBootstrapped          = errors.New("system settings not bootstrapped")
)

var systemSettingFieldNames = []string{
	"database_gateway_mode",
	"web_rdp_enabled",
	"web_rdp_connect_timeout_seconds",
	"web_rdp_allow_unrecorded",
	"recording_enabled",
	"recording_record_input",
	"recording_record_commands",
	"recording_retention_days",
	"recording_max_replay_bytes",
	"recording_cleanup_batch_size",
}

type SystemSettings struct {
	DatabaseGatewayMode         string
	WebRDPEnabled               bool
	WebRDPConnectTimeoutSeconds int
	WebRDPAllowUnrecorded       bool
	RecordingEnabled            bool
	RecordingRecordInput        bool
	RecordingRecordCommands     bool
	RecordingRetentionDays      int
	RecordingMaxReplayBytes     int64
	RecordingCleanupBatchSize   int
}

type SystemSettingsActor struct {
	ID       string
	Username string
}

type SystemSettingsUpdate struct {
	Settings         SystemSettings
	ExpectedRevision int64
	ConfirmRisk      bool
	Actor            SystemSettingsActor
}

type SystemSettingsState struct {
	Desired           SystemSettings
	Effective         SystemSettings
	Revision          int64
	EffectiveRevision int64
	PendingRestart    bool
	UpdatedByID       string
	UpdatedByUsername string
	UpdatedAt         time.Time
	AppliedAt         *time.Time
}

type SystemSettingsRevision struct {
	ID                string
	Revision          int64
	Snapshot          SystemSettings
	ChangedFields     []string
	UpdatedByID       string
	UpdatedByUsername string
	CreatedAt         time.Time
}

type SystemSettingsRepository interface {
	LoadSystemSetting(ctx context.Context) (model.SystemSetting, bool, error)
	InitializeSystemSetting(ctx context.Context, setting model.SystemSetting, revision model.SystemSettingRevision) (model.SystemSetting, bool, error)
	UpdateSystemSetting(ctx context.Context, expectedRevision int64, setting model.SystemSetting, revision model.SystemSettingRevision) (model.SystemSetting, bool, error)
	MarkSystemSettingApplied(ctx context.Context, revision int64, appliedAt time.Time) (model.SystemSetting, bool, error)
	ListSystemSettingRevisions(ctx context.Context, limit int) ([]model.SystemSettingRevision, error)
}

type SystemSettingsService struct {
	repository                    SystemSettingsRepository
	availableDatabaseGatewayModes map[string]struct{}
	now                           func() time.Time

	mu                sync.RWMutex
	effective         SystemSettings
	effectiveRevision int64
	bootstrapped      bool
}

func NewSystemSettingsService(
	repository SystemSettingsRepository,
	availableDatabaseGatewayModes []string,
) (*SystemSettingsService, error) {
	if repository == nil {
		return nil, errors.New("system settings repository is required")
	}
	availableModes := make(map[string]struct{}, len(availableDatabaseGatewayModes))
	for _, mode := range availableDatabaseGatewayModes {
		switch mode {
		case config.DatabaseGatewayModeUnified, config.DatabaseGatewayModeIndependent:
			availableModes[mode] = struct{}{}
		default:
			return nil, fmt.Errorf("unsupported available database gateway mode %q", mode)
		}
	}
	if len(availableModes) == 0 {
		return nil, errors.New("at least one database gateway mode must be available")
	}
	return &SystemSettingsService{
		repository:                    repository,
		availableDatabaseGatewayModes: availableModes,
		now:                           time.Now,
	}, nil
}

func (s *SystemSettingsService) GetState(ctx context.Context) (SystemSettingsState, error) {
	if ctx == nil {
		return SystemSettingsState{}, errors.New("get system settings: nil context")
	}
	persisted, found, err := s.repository.LoadSystemSetting(ctx)
	if err != nil {
		return SystemSettingsState{}, fmt.Errorf("get system settings: %w", err)
	}
	if !found {
		return SystemSettingsState{}, ErrSystemSettingsNotBootstrapped
	}
	return s.stateFromModel(persisted)
}

func (s *SystemSettingsService) Update(ctx context.Context, update SystemSettingsUpdate) (SystemSettingsState, error) {
	if ctx == nil {
		return SystemSettingsState{}, errors.New("update system settings: nil context")
	}
	if err := s.requireBootstrapped(); err != nil {
		return SystemSettingsState{}, err
	}
	if err := s.validateSystemSettings(update.Settings); err != nil {
		return SystemSettingsState{}, err
	}
	if update.ExpectedRevision < 1 {
		return SystemSettingsState{}, fmt.Errorf("%w: expected revision must be positive", ErrInvalidSystemSettings)
	}

	current, found, err := s.repository.LoadSystemSetting(ctx)
	if err != nil {
		return SystemSettingsState{}, fmt.Errorf("load current system settings: %w", err)
	}
	if !found {
		return SystemSettingsState{}, ErrSystemSettingsNotBootstrapped
	}
	if current.Revision != update.ExpectedRevision {
		return SystemSettingsState{}, fmt.Errorf("%w: expected %d, current %d",
			ErrSystemSettingsRevisionConflict, update.ExpectedRevision, current.Revision)
	}

	currentSettings := systemSettingsFromModel(current)
	changedFields := changedSystemSettingFields(currentSettings, update.Settings)
	if len(changedFields) == 0 {
		return s.stateFromModel(current)
	}
	riskFields := riskySystemSettingFields(currentSettings, update.Settings)
	if len(riskFields) > 0 && !update.ConfirmRisk {
		return SystemSettingsState{}, fmt.Errorf("%w: %s",
			ErrSystemSettingsRiskConfirmationRequired, strings.Join(riskFields, ", "))
	}

	snapshotJSON, err := marshalSystemSettings(update.Settings)
	if err != nil {
		return SystemSettingsState{}, err
	}
	changedFieldsJSON, err := json.Marshal(changedFields)
	if err != nil {
		return SystemSettingsState{}, fmt.Errorf("marshal changed system setting fields: %w", err)
	}
	now := s.now().UTC()
	next := systemSettingModel(update.Settings, update.Actor)
	next.UpdatedAt = now
	persisted, updated, err := s.repository.UpdateSystemSetting(
		ctx,
		update.ExpectedRevision,
		next,
		model.SystemSettingRevision{
			SnapshotJSON:      snapshotJSON,
			ChangedFieldsJSON: string(changedFieldsJSON),
			UpdatedByID:       strings.TrimSpace(update.Actor.ID),
			UpdatedByUsername: strings.TrimSpace(update.Actor.Username),
			CreatedAt:         now,
		},
	)
	if err != nil {
		return SystemSettingsState{}, fmt.Errorf("persist system settings: %w", err)
	}
	if !updated {
		return SystemSettingsState{}, ErrSystemSettingsRevisionConflict
	}
	return s.stateFromModel(persisted)
}

func (s *SystemSettingsService) ListRevisions(ctx context.Context, limit int) ([]SystemSettingsRevision, error) {
	if ctx == nil {
		return nil, errors.New("list system setting revisions: nil context")
	}
	if limit <= 0 {
		limit = defaultSystemSettingRevisionLimit
	}
	if limit > maxSystemSettingRevisionLimit {
		limit = maxSystemSettingRevisionLimit
	}
	rows, err := s.repository.ListSystemSettingRevisions(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list system setting revisions: %w", err)
	}
	revisions := make([]SystemSettingsRevision, 0, len(rows))
	for _, row := range rows {
		snapshot, err := unmarshalSystemSettings(row.SnapshotJSON)
		if err != nil {
			return nil, fmt.Errorf("decode system setting revision %d: %w", row.Revision, err)
		}
		var changedFields []string
		if err := json.Unmarshal([]byte(row.ChangedFieldsJSON), &changedFields); err != nil {
			return nil, fmt.Errorf("decode system setting revision %d changed fields: %w",
				row.Revision, err)
		}
		revisions = append(revisions, SystemSettingsRevision{
			ID:                row.ID,
			Revision:          row.Revision,
			Snapshot:          snapshot,
			ChangedFields:     changedFields,
			UpdatedByID:       row.UpdatedByID,
			UpdatedByUsername: row.UpdatedByUsername,
			CreatedAt:         row.CreatedAt,
		})
	}
	return revisions, nil
}

func (s *SystemSettingsService) requireBootstrapped() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.bootstrapped {
		return ErrSystemSettingsNotBootstrapped
	}
	return nil
}

func (s *SystemSettingsService) stateFromModel(persisted model.SystemSetting) (SystemSettingsState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.bootstrapped {
		return SystemSettingsState{}, ErrSystemSettingsNotBootstrapped
	}
	return SystemSettingsState{
		Desired:           systemSettingsFromModel(persisted),
		Effective:         s.effective,
		Revision:          persisted.Revision,
		EffectiveRevision: s.effectiveRevision,
		PendingRestart:    persisted.Revision != s.effectiveRevision,
		UpdatedByID:       persisted.UpdatedByID,
		UpdatedByUsername: persisted.UpdatedByUsername,
		UpdatedAt:         persisted.UpdatedAt,
		AppliedAt:         persisted.AppliedAt,
	}, nil
}

func validateSystemSettings(settings SystemSettings) error {
	switch {
	case settings.DatabaseGatewayMode != config.DatabaseGatewayModeUnified &&
		settings.DatabaseGatewayMode != config.DatabaseGatewayModeIndependent:
		return fmt.Errorf(
			"%w: database gateway mode must be %q or %q",
			ErrInvalidSystemSettings,
			config.DatabaseGatewayModeUnified,
			config.DatabaseGatewayModeIndependent,
		)
	case settings.WebRDPConnectTimeoutSeconds < 1 ||
		settings.WebRDPConnectTimeoutSeconds > 300:
		return fmt.Errorf("%w: web RDP connect timeout must be between 1 and 300 seconds",
			ErrInvalidSystemSettings)
	case settings.RecordingRetentionDays < 1 ||
		settings.RecordingRetentionDays > 3650:
		return fmt.Errorf("%w: recording retention must be between 1 and 3650 days",
			ErrInvalidSystemSettings)
	case settings.RecordingMaxReplayBytes < 0:
		return fmt.Errorf("%w: recording replay byte quota must not be negative", ErrInvalidSystemSettings)
	case settings.RecordingCleanupBatchSize < 1 ||
		settings.RecordingCleanupBatchSize > 1000:
		return fmt.Errorf("%w: recording cleanup batch size must be between 1 and 1000",
			ErrInvalidSystemSettings)
	default:
		return nil
	}
}

func (s *SystemSettingsService) validateSystemSettings(settings SystemSettings) error {
	if err := validateSystemSettings(settings); err != nil {
		return err
	}
	if _, available := s.availableDatabaseGatewayModes[settings.DatabaseGatewayMode]; !available {
		return fmt.Errorf(
			"%w: database gateway mode %q is not configured and cannot become effective after restart",
			ErrInvalidSystemSettings,
			settings.DatabaseGatewayMode,
		)
	}
	return nil
}

func changedSystemSettingFields(before, after SystemSettings) []string {
	changed := make([]string, 0, len(systemSettingFieldNames))
	if before.DatabaseGatewayMode != after.DatabaseGatewayMode {
		changed = append(changed, "database_gateway_mode")
	}
	if before.WebRDPEnabled != after.WebRDPEnabled {
		changed = append(changed, "web_rdp_enabled")
	}
	if before.WebRDPConnectTimeoutSeconds != after.WebRDPConnectTimeoutSeconds {
		changed = append(changed, "web_rdp_connect_timeout_seconds")
	}
	if before.WebRDPAllowUnrecorded != after.WebRDPAllowUnrecorded {
		changed = append(changed, "web_rdp_allow_unrecorded")
	}
	if before.RecordingEnabled != after.RecordingEnabled {
		changed = append(changed, "recording_enabled")
	}
	if before.RecordingRecordInput != after.RecordingRecordInput {
		changed = append(changed, "recording_record_input")
	}
	if before.RecordingRecordCommands != after.RecordingRecordCommands {
		changed = append(changed, "recording_record_commands")
	}
	if before.RecordingRetentionDays != after.RecordingRetentionDays {
		changed = append(changed, "recording_retention_days")
	}
	if before.RecordingMaxReplayBytes != after.RecordingMaxReplayBytes {
		changed = append(changed, "recording_max_replay_bytes")
	}
	if before.RecordingCleanupBatchSize != after.RecordingCleanupBatchSize {
		changed = append(changed, "recording_cleanup_batch_size")
	}
	return changed
}

func riskySystemSettingFields(before, after SystemSettings) []string {
	risky := make([]string, 0, 6)
	if !before.WebRDPAllowUnrecorded && after.WebRDPAllowUnrecorded {
		risky = append(risky, "web_rdp_allow_unrecorded")
	}
	if before.RecordingEnabled && !after.RecordingEnabled {
		risky = append(risky, "recording_enabled")
	}
	if !before.RecordingRecordInput && after.RecordingRecordInput {
		risky = append(risky, "recording_record_input")
	}
	if before.RecordingRecordCommands && !after.RecordingRecordCommands {
		risky = append(risky, "recording_record_commands")
	}
	if after.RecordingRetentionDays < before.RecordingRetentionDays {
		risky = append(risky, "recording_retention_days")
	}
	if replayQuotaTightened(before.RecordingMaxReplayBytes, after.RecordingMaxReplayBytes) {
		risky = append(risky, "recording_max_replay_bytes")
	}
	return risky
}

func replayQuotaTightened(before, after int64) bool {
	return before == 0 && after > 0 || before > 0 && after > 0 && after < before
}

func systemSettingModel(settings SystemSettings, actor SystemSettingsActor) model.SystemSetting {
	return model.SystemSetting{
		ID:                          model.SystemSettingSingletonID,
		DatabaseGatewayMode:         settings.DatabaseGatewayMode,
		WebRDPEnabled:               settings.WebRDPEnabled,
		WebRDPConnectTimeoutSeconds: settings.WebRDPConnectTimeoutSeconds,
		WebRDPAllowUnrecorded:       settings.WebRDPAllowUnrecorded,
		RecordingEnabled:            settings.RecordingEnabled,
		RecordingRecordInput:        settings.RecordingRecordInput,
		RecordingRecordCommands:     settings.RecordingRecordCommands,
		RecordingRetentionDays:      settings.RecordingRetentionDays,
		RecordingMaxReplayBytes:     settings.RecordingMaxReplayBytes,
		RecordingCleanupBatchSize:   settings.RecordingCleanupBatchSize,
		UpdatedByID:                 strings.TrimSpace(actor.ID),
		UpdatedByUsername:           strings.TrimSpace(actor.Username),
	}
}

func systemSettingsFromModel(setting model.SystemSetting) SystemSettings {
	mode := strings.TrimSpace(setting.DatabaseGatewayMode)
	if mode == "" {
		mode = config.DatabaseGatewayModeUnified
	}
	return SystemSettings{
		DatabaseGatewayMode:         mode,
		WebRDPEnabled:               setting.WebRDPEnabled,
		WebRDPConnectTimeoutSeconds: setting.WebRDPConnectTimeoutSeconds,
		WebRDPAllowUnrecorded:       setting.WebRDPAllowUnrecorded,
		RecordingEnabled:            setting.RecordingEnabled,
		RecordingRecordInput:        setting.RecordingRecordInput,
		RecordingRecordCommands:     setting.RecordingRecordCommands,
		RecordingRetentionDays:      setting.RecordingRetentionDays,
		RecordingMaxReplayBytes:     setting.RecordingMaxReplayBytes,
		RecordingCleanupBatchSize:   setting.RecordingCleanupBatchSize,
	}
}

type systemSettingsSnapshot struct {
	DatabaseGatewayMode         string `json:"database_gateway_mode"`
	WebRDPEnabled               bool   `json:"web_rdp_enabled"`
	WebRDPConnectTimeoutSeconds int    `json:"web_rdp_connect_timeout_seconds"`
	WebRDPAllowUnrecorded       bool   `json:"web_rdp_allow_unrecorded"`
	RecordingEnabled            bool   `json:"recording_enabled"`
	RecordingRecordInput        bool   `json:"recording_record_input"`
	RecordingRecordCommands     bool   `json:"recording_record_commands"`
	RecordingRetentionDays      int    `json:"recording_retention_days"`
	RecordingMaxReplayBytes     int64  `json:"recording_max_replay_bytes"`
	RecordingCleanupBatchSize   int    `json:"recording_cleanup_batch_size"`
}

func marshalSystemSettings(settings SystemSettings) (string, error) {
	encoded, err := json.Marshal(snapshotFromSystemSettings(settings))
	if err != nil {
		return "", fmt.Errorf("marshal system settings snapshot: %w", err)
	}
	return string(encoded), nil
}

func unmarshalSystemSettings(encoded string) (SystemSettings, error) {
	var snapshot systemSettingsSnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return SystemSettings{}, err
	}
	if snapshot.DatabaseGatewayMode == "" {
		snapshot.DatabaseGatewayMode = config.DatabaseGatewayModeUnified
	}
	return snapshot.systemSettings(), nil
}

func snapshotFromSystemSettings(settings SystemSettings) systemSettingsSnapshot {
	return systemSettingsSnapshot{
		DatabaseGatewayMode:         settings.DatabaseGatewayMode,
		WebRDPEnabled:               settings.WebRDPEnabled,
		WebRDPConnectTimeoutSeconds: settings.WebRDPConnectTimeoutSeconds,
		WebRDPAllowUnrecorded:       settings.WebRDPAllowUnrecorded,
		RecordingEnabled:            settings.RecordingEnabled,
		RecordingRecordInput:        settings.RecordingRecordInput,
		RecordingRecordCommands:     settings.RecordingRecordCommands,
		RecordingRetentionDays:      settings.RecordingRetentionDays,
		RecordingMaxReplayBytes:     settings.RecordingMaxReplayBytes,
		RecordingCleanupBatchSize:   settings.RecordingCleanupBatchSize,
	}
}

func (snapshot systemSettingsSnapshot) systemSettings() SystemSettings {
	return SystemSettings{
		DatabaseGatewayMode:         snapshot.DatabaseGatewayMode,
		WebRDPEnabled:               snapshot.WebRDPEnabled,
		WebRDPConnectTimeoutSeconds: snapshot.WebRDPConnectTimeoutSeconds,
		WebRDPAllowUnrecorded:       snapshot.WebRDPAllowUnrecorded,
		RecordingEnabled:            snapshot.RecordingEnabled,
		RecordingRecordInput:        snapshot.RecordingRecordInput,
		RecordingRecordCommands:     snapshot.RecordingRecordCommands,
		RecordingRetentionDays:      snapshot.RecordingRetentionDays,
		RecordingMaxReplayBytes:     snapshot.RecordingMaxReplayBytes,
		RecordingCleanupBatchSize:   snapshot.RecordingCleanupBatchSize,
	}
}
