package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestSystemSettingsBootstrapUpdateAndRestart(t *testing.T) {
	ctx := context.Background()
	repository := &systemSettingsMemoryRepository{}
	startedAt := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	svc := newTestSystemSettingsService(t, repository, startedAt)
	baseline := validSystemSettings()

	state, err := svc.Bootstrap(ctx, baseline)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if state.Revision != 1 || state.EffectiveRevision != 1 || state.PendingRestart {
		t.Fatalf("initial state = %#v", state)
	}
	if !reflect.DeepEqual(state.Desired, baseline) || !reflect.DeepEqual(state.Effective, baseline) {
		t.Fatalf("initial settings = %#v", state)
	}
	if state.AppliedAt == nil || !state.AppliedAt.Equal(startedAt) {
		t.Fatalf("initial applied at = %v, want %v", state.AppliedAt, startedAt)
	}

	desired := baseline
	desired.WebRDPEnabled = true
	desired.WebRDPConnectTimeoutSeconds = 30
	updatedAt := startedAt.Add(time.Minute)
	svc.now = func() time.Time { return updatedAt }
	state, err = svc.Update(ctx, SystemSettingsUpdate{
		Settings:         desired,
		ExpectedRevision: 1,
		Actor:            SystemSettingsActor{ID: "user-1", Username: "admin"},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if state.Revision != 2 || state.EffectiveRevision != 1 || !state.PendingRestart {
		t.Fatalf("updated state = %#v", state)
	}
	if !reflect.DeepEqual(state.Desired, desired) || !reflect.DeepEqual(state.Effective, baseline) {
		t.Fatalf("updated settings = %#v", state)
	}
	if state.UpdatedByID != "user-1" || state.UpdatedByUsername != "admin" {
		t.Fatalf("updated actor = %q/%q", state.UpdatedByID, state.UpdatedByUsername)
	}

	revisions, err := svc.ListRevisions(ctx, 20)
	if err != nil {
		t.Fatalf("ListRevisions() error = %v", err)
	}
	if len(revisions) != 2 || revisions[0].Revision != 2 {
		t.Fatalf("revisions = %#v", revisions)
	}
	wantChanged := []string{"web_rdp_enabled", "web_rdp_connect_timeout_seconds"}
	if !reflect.DeepEqual(revisions[0].ChangedFields, wantChanged) {
		t.Fatalf("changed fields = %v, want %v", revisions[0].ChangedFields, wantChanged)
	}
	if !reflect.DeepEqual(revisions[0].Snapshot, desired) {
		t.Fatalf("revision snapshot = %#v, want %#v", revisions[0].Snapshot, desired)
	}

	restartedAt := startedAt.Add(2 * time.Minute)
	restarted := newTestSystemSettingsService(t, repository, restartedAt)
	differentBaseline := baseline
	differentBaseline.RecordingRetentionDays = 365
	state, err = restarted.Bootstrap(ctx, differentBaseline)
	if err != nil {
		t.Fatalf("restarted Bootstrap() error = %v", err)
	}
	if state.Revision != 2 || state.EffectiveRevision != 2 || state.PendingRestart {
		t.Fatalf("restarted state = %#v", state)
	}
	if !reflect.DeepEqual(state.Desired, desired) || !reflect.DeepEqual(state.Effective, desired) {
		t.Fatalf("database settings did not win on restart: %#v", state)
	}
	if state.AppliedAt == nil || !state.AppliedAt.Equal(restartedAt) {
		t.Fatalf("restart applied at = %v, want %v", state.AppliedAt, restartedAt)
	}
}

func TestSystemSettingsUpdateRequiresRiskConfirmation(t *testing.T) {
	tests := []struct {
		name  string
		field string
		apply func(*SystemSettings)
	}{
		{
			name: "allow unrecorded RDP", field: "web_rdp_allow_unrecorded",
			apply: func(settings *SystemSettings) { settings.WebRDPAllowUnrecorded = true },
		},
		{
			name: "disable recording", field: "recording_enabled",
			apply: func(settings *SystemSettings) { settings.RecordingEnabled = false },
		},
		{
			name: "record raw input", field: "recording_record_input",
			apply: func(settings *SystemSettings) { settings.RecordingRecordInput = true },
		},
		{
			name: "disable command recording", field: "recording_record_commands",
			apply: func(settings *SystemSettings) { settings.RecordingRecordCommands = false },
		},
		{
			name: "lower retention", field: "recording_retention_days",
			apply: func(settings *SystemSettings) { settings.RecordingRetentionDays-- },
		},
		{
			name: "tighten replay quota", field: "recording_max_replay_bytes",
			apply: func(settings *SystemSettings) { settings.RecordingMaxReplayBytes /= 2 },
		},
		{
			name:  "change database client message size",
			field: "database_max_client_message_bytes",
			apply: func(settings *SystemSettings) {
				settings.DatabaseMaxClientMessageBytes = 12 * 1024 * 1024
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repository := &systemSettingsMemoryRepository{}
			svc := newTestSystemSettingsService(t, repository, time.Now())
			baseline := validSystemSettings()
			if _, err := svc.Bootstrap(ctx, baseline); err != nil {
				t.Fatalf("Bootstrap() error = %v", err)
			}
			desired := baseline
			tt.apply(&desired)
			_, err := svc.Update(ctx, SystemSettingsUpdate{
				Settings:         desired,
				ExpectedRevision: 1,
			})
			if !errors.Is(err, ErrSystemSettingsRiskConfirmationRequired) {
				t.Fatalf("Update() error = %v, want risk confirmation", err)
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("Update() error = %v, want field %q", err, tt.field)
			}
			state, err := svc.GetState(ctx)
			if err != nil {
				t.Fatalf("GetState() error = %v", err)
			}
			if state.Revision != 1 {
				t.Fatalf("rejected update advanced revision to %d", state.Revision)
			}

			state, err = svc.Update(ctx, SystemSettingsUpdate{
				Settings:         desired,
				ExpectedRevision: 1,
				ConfirmRisk:      true,
			})
			if err != nil {
				t.Fatalf("confirmed Update() error = %v", err)
			}
			if state.Revision != 2 || !state.PendingRestart {
				t.Fatalf("confirmed state = %#v", state)
			}
		})
	}
}

func TestSystemSettingsValidation(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*SystemSettings)
	}{
		{name: "timeout below minimum", apply: func(s *SystemSettings) { s.WebRDPConnectTimeoutSeconds = 0 }},
		{name: "timeout above maximum", apply: func(s *SystemSettings) { s.WebRDPConnectTimeoutSeconds = 301 }},
		{name: "retention below minimum", apply: func(s *SystemSettings) { s.RecordingRetentionDays = 0 }},
		{name: "retention above maximum", apply: func(s *SystemSettings) { s.RecordingRetentionDays = 3651 }},
		{name: "negative replay quota", apply: func(s *SystemSettings) { s.RecordingMaxReplayBytes = -1 }},
		{name: "batch below minimum", apply: func(s *SystemSettings) { s.RecordingCleanupBatchSize = 0 }},
		{name: "batch above maximum", apply: func(s *SystemSettings) { s.RecordingCleanupBatchSize = 1001 }},
		{name: "database message below minimum", apply: func(s *SystemSettings) {
			s.DatabaseMaxClientMessageBytes = minDatabaseMaxClientMessageBytes - 1
		}},
		{name: "database message above maximum", apply: func(s *SystemSettings) {
			s.DatabaseMaxClientMessageBytes = maxDatabaseMaxClientMessageBytes + 1
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := validSystemSettings()
			tt.apply(&settings)
			svc := newTestSystemSettingsService(
				t,
				&systemSettingsMemoryRepository{},
				time.Now(),
			)
			_, err := svc.Bootstrap(context.Background(), settings)
			if !errors.Is(err, ErrInvalidSystemSettings) {
				t.Fatalf("Bootstrap() error = %v, want invalid settings", err)
			}
		})
	}
}

func TestSystemSettingsUpdateUsesOptimisticRevision(t *testing.T) {
	ctx := context.Background()
	repository := &systemSettingsMemoryRepository{}
	svc := newTestSystemSettingsService(t, repository, time.Now())
	baseline := validSystemSettings()
	if _, err := svc.Bootstrap(ctx, baseline); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	desired := baseline
	desired.WebRDPEnabled = true

	_, err := svc.Update(ctx, SystemSettingsUpdate{
		Settings:         desired,
		ExpectedRevision: 2,
	})
	if !errors.Is(err, ErrSystemSettingsRevisionConflict) {
		t.Fatalf("stale Update() error = %v, want revision conflict", err)
	}

	repository.conflictOnUpdate = true
	_, err = svc.Update(ctx, SystemSettingsUpdate{
		Settings:         desired,
		ExpectedRevision: 1,
	})
	if !errors.Is(err, ErrSystemSettingsRevisionConflict) {
		t.Fatalf("racing Update() error = %v, want revision conflict", err)
	}
}

func TestSystemSettingsNoopDoesNotCreateRevision(t *testing.T) {
	ctx := context.Background()
	repository := &systemSettingsMemoryRepository{}
	svc := newTestSystemSettingsService(t, repository, time.Now())
	baseline := validSystemSettings()
	if _, err := svc.Bootstrap(ctx, baseline); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	state, err := svc.Update(ctx, SystemSettingsUpdate{
		Settings:         baseline,
		ExpectedRevision: 1,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if state.Revision != 1 || len(repository.revisions) != 1 {
		t.Fatalf("no-op state/revisions = %#v/%d", state, len(repository.revisions))
	}
}

func TestLegacySystemSettingRevisionUsesTenMiBDatabaseLimit(t *testing.T) {
	settings, err := unmarshalSystemSettings(`{
		"web_rdp_enabled":false,
		"web_rdp_connect_timeout_seconds":15,
		"web_rdp_allow_unrecorded":false,
		"recording_enabled":true,
		"recording_record_input":false,
		"recording_record_commands":true,
		"recording_retention_days":30,
		"recording_max_replay_bytes":10485760,
		"recording_cleanup_batch_size":100
	}`)
	if err != nil {
		t.Fatalf("unmarshalSystemSettings() error = %v", err)
	}
	if settings.DatabaseMaxClientMessageBytes !=
		defaultDatabaseMaxClientMessageBytes {
		t.Fatalf(
			"legacy database client message limit = %d, want %d",
			settings.DatabaseMaxClientMessageBytes,
			defaultDatabaseMaxClientMessageBytes,
		)
	}
}

func TestSystemSettingsRequiresBootstrap(t *testing.T) {
	svc := newTestSystemSettingsService(
		t,
		&systemSettingsMemoryRepository{},
		time.Now(),
	)
	if _, err := svc.GetState(context.Background()); !errors.Is(err, ErrSystemSettingsNotBootstrapped) {
		t.Fatalf("GetState() error = %v", err)
	}
	if _, err := svc.Update(context.Background(), SystemSettingsUpdate{
		Settings:         validSystemSettings(),
		ExpectedRevision: 1,
	}); !errors.Is(err, ErrSystemSettingsNotBootstrapped) {
		t.Fatalf("Update() error = %v", err)
	}
}

func TestSystemSettingsPrepareDoesNotMarkRevisionApplied(t *testing.T) {
	ctx := context.Background()
	repository := &systemSettingsMemoryRepository{}
	svc := newTestSystemSettingsService(t, repository, time.Now())
	prepared, err := svc.PrepareBootstrap(ctx, validSystemSettings())
	if err != nil {
		t.Fatalf("PrepareBootstrap() error = %v", err)
	}
	if repository.setting.AppliedRevision != 0 || repository.setting.AppliedAt != nil {
		t.Fatalf("prepared setting was marked applied: %#v", repository.setting)
	}
	if _, err := svc.GetState(ctx); !errors.Is(err, ErrSystemSettingsNotBootstrapped) {
		t.Fatalf("GetState() before activation error = %v", err)
	}
	state, err := svc.ActivateBootstrap(ctx, prepared)
	if err != nil {
		t.Fatalf("ActivateBootstrap() error = %v", err)
	}
	if state.EffectiveRevision != prepared.Revision || state.PendingRestart {
		t.Fatalf("activated state = %#v", state)
	}
}

func TestSystemSettingsActivationRejectsMismatchedAppliedSnapshot(t *testing.T) {
	ctx := context.Background()
	repository := &systemSettingsMemoryRepository{mismatchOnMark: true}
	svc := newTestSystemSettingsService(t, repository, time.Now())
	prepared, err := svc.PrepareBootstrap(ctx, validSystemSettings())
	if err != nil {
		t.Fatalf("PrepareBootstrap() error = %v", err)
	}
	if _, err := svc.ActivateBootstrap(ctx, prepared); !errors.Is(
		err,
		ErrSystemSettingsRevisionConflict,
	) {
		t.Fatalf("ActivateBootstrap() error = %v, want revision conflict", err)
	}
}

func newTestSystemSettingsService(
	t *testing.T,
	repository SystemSettingsRepository,
	now time.Time,
) *SystemSettingsService {
	t.Helper()
	svc, err := NewSystemSettingsService(repository)
	if err != nil {
		t.Fatalf("NewSystemSettingsService() error = %v", err)
	}
	svc.now = func() time.Time { return now }
	return svc
}

func validSystemSettings() SystemSettings {
	return SystemSettings{
		WebRDPConnectTimeoutSeconds:   15,
		RecordingEnabled:              true,
		RecordingRecordCommands:       true,
		RecordingRetentionDays:        30,
		RecordingMaxReplayBytes:       10 * 1024 * 1024,
		RecordingCleanupBatchSize:     100,
		DatabaseMaxClientMessageBytes: defaultDatabaseMaxClientMessageBytes,
	}
}

type systemSettingsMemoryRepository struct {
	setting          model.SystemSetting
	found            bool
	revisions        []model.SystemSettingRevision
	conflictOnUpdate bool
	mismatchOnMark   bool
}

func (r *systemSettingsMemoryRepository) LoadSystemSetting(
	_ context.Context,
) (model.SystemSetting, bool, error) {
	return r.setting, r.found, nil
}

func (r *systemSettingsMemoryRepository) InitializeSystemSetting(
	_ context.Context,
	setting model.SystemSetting,
	revision model.SystemSettingRevision,
) (model.SystemSetting, bool, error) {
	if r.found {
		return r.setting, false, nil
	}
	setting.ID = model.SystemSettingSingletonID
	setting.Revision = 1
	setting.AppliedRevision = 0
	revision.ID = "revision-1"
	revision.Revision = 1
	r.setting = setting
	r.found = true
	r.revisions = append([]model.SystemSettingRevision{revision}, r.revisions...)
	return setting, true, nil
}

func (r *systemSettingsMemoryRepository) UpdateSystemSetting(
	_ context.Context,
	expectedRevision int64,
	setting model.SystemSetting,
	revision model.SystemSettingRevision,
) (model.SystemSetting, bool, error) {
	if r.conflictOnUpdate || !r.found || r.setting.Revision != expectedRevision {
		return model.SystemSetting{}, false, nil
	}
	setting.ID = model.SystemSettingSingletonID
	setting.Revision = expectedRevision + 1
	setting.AppliedRevision = r.setting.AppliedRevision
	setting.AppliedAt = r.setting.AppliedAt
	setting.CreatedAt = r.setting.CreatedAt
	revision.ID = "revision-" + string(rune('0'+setting.Revision))
	revision.Revision = setting.Revision
	r.setting = setting
	r.revisions = append([]model.SystemSettingRevision{revision}, r.revisions...)
	return setting, true, nil
}

func (r *systemSettingsMemoryRepository) MarkSystemSettingApplied(
	_ context.Context,
	revision int64,
	appliedAt time.Time,
) (model.SystemSetting, bool, error) {
	if !r.found || r.setting.Revision != revision {
		return model.SystemSetting{}, false, nil
	}
	r.setting.AppliedRevision = revision
	r.setting.AppliedAt = &appliedAt
	if r.mismatchOnMark {
		mismatched := r.setting
		mismatched.Revision++
		return mismatched, true, nil
	}
	return r.setting, true, nil
}

func (r *systemSettingsMemoryRepository) ListSystemSettingRevisions(
	_ context.Context,
	limit int,
) ([]model.SystemSettingRevision, error) {
	if limit > len(r.revisions) {
		limit = len(r.revisions)
	}
	return append([]model.SystemSettingRevision(nil), r.revisions[:limit]...), nil
}
