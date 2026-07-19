package store

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestSystemSettingStoreLifecycle(t *testing.T) {
	db := openSystemSettingStoreDatabase(t)
	repository := NewDBStore(db)
	ctx := context.Background()
	createdAt := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	initial := systemSettingStoreFixture()
	initial.CreatedAt = createdAt
	initial.UpdatedAt = createdAt

	persisted, created, err := repository.InitializeSystemSetting(
		ctx,
		initial,
		systemSettingRevisionFixture(1, createdAt),
	)
	if err != nil {
		t.Fatalf("InitializeSystemSetting() error = %v", err)
	}
	if !created || persisted.ID != model.SystemSettingSingletonID || persisted.Revision != 1 {
		t.Fatalf("initialized setting = %#v, created = %v", persisted, created)
	}

	replacement := initial
	replacement.WebRDPConnectTimeoutSeconds = 99
	persisted, created, err = repository.InitializeSystemSetting(
		ctx,
		replacement,
		systemSettingRevisionFixture(1, createdAt.Add(time.Minute)),
	)
	if err != nil {
		t.Fatalf("second InitializeSystemSetting() error = %v", err)
	}
	if created || persisted.WebRDPConnectTimeoutSeconds != initial.WebRDPConnectTimeoutSeconds {
		t.Fatalf("second initialize replaced setting: %#v, created = %v", persisted, created)
	}

	changedAt := createdAt.Add(2 * time.Minute)
	replacement.WebRDPEnabled = true
	replacement.WebRDPConnectTimeoutSeconds = 30
	replacement.UpdatedByID = "user-1"
	replacement.UpdatedByUsername = "admin"
	persisted, updated, err := repository.UpdateSystemSetting(
		ctx,
		1,
		replacement,
		systemSettingRevisionFixture(2, changedAt),
	)
	if err != nil {
		t.Fatalf("UpdateSystemSetting() error = %v", err)
	}
	if !updated || persisted.Revision != 2 || !persisted.WebRDPEnabled {
		t.Fatalf("updated setting = %#v, updated = %v", persisted, updated)
	}
	if persisted.UpdatedByID != "user-1" || persisted.UpdatedByUsername != "admin" {
		t.Fatalf("updated actor = %q/%q", persisted.UpdatedByID, persisted.UpdatedByUsername)
	}

	stale := replacement
	stale.WebRDPEnabled = false
	_, updated, err = repository.UpdateSystemSetting(
		ctx,
		1,
		stale,
		systemSettingRevisionFixture(2, changedAt.Add(time.Minute)),
	)
	if err != nil {
		t.Fatalf("stale UpdateSystemSetting() error = %v", err)
	}
	if updated {
		t.Fatal("stale UpdateSystemSetting() unexpectedly succeeded")
	}

	appliedAt := changedAt.Add(2 * time.Minute)
	applied, marked, err := repository.MarkSystemSettingApplied(ctx, 2, appliedAt)
	if err != nil {
		t.Fatalf("MarkSystemSettingApplied() error = %v", err)
	}
	if !marked || applied.AppliedRevision != 2 ||
		applied.AppliedAt == nil || !applied.AppliedAt.Equal(appliedAt) {
		t.Fatalf("applied setting = %#v, marked = %v", applied, marked)
	}
	if !applied.UpdatedAt.Equal(changedAt) {
		t.Fatalf("mark applied changed UpdatedAt to %v, want %v", applied.UpdatedAt, changedAt)
	}

	revisions, err := repository.ListSystemSettingRevisions(ctx, 20)
	if err != nil {
		t.Fatalf("ListSystemSettingRevisions() error = %v", err)
	}
	if len(revisions) != 2 || revisions[0].Revision != 2 || revisions[1].Revision != 1 {
		t.Fatalf("revisions = %#v", revisions)
	}
	if revisions[0].SnapshotJSON != `{"database_gateway_mode":"unified","web_rdp_enabled":true}` ||
		revisions[0].ChangedFieldsJSON != `["web_rdp_enabled"]` {
		t.Fatalf("revision payload = %#v", revisions[0])
	}
}

func TestSystemSettingStoreRollsBackWhenRevisionInsertFails(t *testing.T) {
	db := openSystemSettingStoreDatabase(t)
	repository := NewDBStore(db)
	ctx := context.Background()
	initial := systemSettingStoreFixture()
	if _, _, err := repository.InitializeSystemSetting(
		ctx,
		initial,
		systemSettingRevisionFixture(1, time.Now().UTC()),
	); err != nil {
		t.Fatalf("InitializeSystemSetting() error = %v", err)
	}
	if err := db.Create(&model.SystemSettingRevision{
		Revision:          2,
		SnapshotJSON:      `{}`,
		ChangedFieldsJSON: `[]`,
		CreatedAt:         time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed conflicting revision: %v", err)
	}

	changed := initial
	changed.WebRDPEnabled = true
	_, updated, err := repository.UpdateSystemSetting(
		ctx,
		1,
		changed,
		systemSettingRevisionFixture(2, time.Now().UTC()),
	)
	if err == nil {
		t.Fatal("UpdateSystemSetting() error = nil, want revision insert failure")
	}
	if updated {
		t.Fatal("failed UpdateSystemSetting() reported success")
	}
	persisted, found, loadErr := repository.LoadSystemSetting(ctx)
	if loadErr != nil {
		t.Fatalf("LoadSystemSetting() error = %v", loadErr)
	}
	if !found || persisted.Revision != 1 || persisted.WebRDPEnabled {
		t.Fatalf("failed transaction persisted partial update: %#v", persisted)
	}
}

func openSystemSettingStoreDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.SystemSetting{},
		&model.SystemSettingRevision{},
	); err != nil {
		t.Fatalf("migrate system setting store schema: %v", err)
	}
	return db
}

func systemSettingStoreFixture() model.SystemSetting {
	return model.SystemSetting{
		ID:                          model.SystemSettingSingletonID,
		DatabaseGatewayMode:         "unified",
		WebRDPConnectTimeoutSeconds: 15,
		RecordingEnabled:            true,
		RecordingRecordCommands:     true,
		RecordingRetentionDays:      30,
		RecordingMaxReplayBytes:     1024,
		RecordingCleanupBatchSize:   100,
	}
}

func systemSettingRevisionFixture(
	revision int64,
	createdAt time.Time,
) model.SystemSettingRevision {
	snapshot := `{"database_gateway_mode":"unified","web_rdp_enabled":false}`
	changedFields := `["database_gateway_mode","web_rdp_enabled","web_rdp_connect_timeout_seconds","web_rdp_allow_unrecorded","recording_enabled","recording_record_input","recording_record_commands","recording_retention_days","recording_max_replay_bytes","recording_cleanup_batch_size"]`
	if revision == 2 {
		snapshot = `{"database_gateway_mode":"unified","web_rdp_enabled":true}`
		changedFields = `["web_rdp_enabled"]`
	}
	return model.SystemSettingRevision{
		SnapshotJSON:      snapshot,
		ChangedFieldsJSON: changedFields,
		CreatedAt:         createdAt,
	}
}
