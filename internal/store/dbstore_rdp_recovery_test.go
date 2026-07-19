package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"
)

func TestListRecoverableRDPRecordingsScopesEndedAndInterrupted(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.AuditSession{},
		&model.AuditArtifact{},
	); err != nil {
		t.Fatalf("migrate RDP audit: %v", err)
	}
	now := time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC)
	endedAt := now.Add(-time.Minute)
	sessions := []model.AuditSession{
		{
			ID: "ended-rdp", Protocol: "rdp", State: "ended",
			Outcome: model.AuditOutcomeSucceeded, StartedAt: now.Add(-time.Hour),
			EndedAt: &endedAt,
		},
		{
			ID: "active-rdp", Protocol: "rdp", State: "active",
			Outcome: model.AuditOutcomeActive, StartedAt: now.Add(-time.Hour),
		},
		{
			ID: "unrecoverable-rdp", Protocol: "rdp", State: "ended",
			Outcome: model.AuditOutcomeFailed, StartedAt: now.Add(-time.Hour),
			EndedAt: &endedAt,
		},
		{
			ID: "ended-ssh", Protocol: "ssh", State: "ended",
			Outcome: model.AuditOutcomeSucceeded, StartedAt: now.Add(-time.Hour),
			EndedAt: &endedAt,
		},
	}
	if err := db.Create(&sessions).Error; err != nil {
		t.Fatalf("create audit sessions: %v", err)
	}
	artifacts := []model.AuditArtifact{
		recoveryArtifact("ended-rdp", model.RecordingStatusFailed, ""),
		recoveryArtifact("active-rdp", model.RecordingStatusPending, ""),
		recoveryArtifact(
			"unrecoverable-rdp",
			model.RecordingStatusFailed,
			"recording spool unavailable for recovery",
		),
		recoveryArtifact("ended-ssh", model.RecordingStatusFailed, ""),
	}
	if err := db.Create(&artifacts).Error; err != nil {
		t.Fatalf("create audit artifacts: %v", err)
	}
	repository := NewDBStore(db)

	ended, err := repository.ListRecoverableRDPRecordings(
		context.Background(),
		false,
	)
	if err != nil {
		t.Fatalf("list ended recovery items: %v", err)
	}
	if ids := recoverySessionIDs(ended); !slices.Equal(ids, []string{"ended-rdp"}) {
		t.Fatalf("ended recovery sessions = %#v, want ended-rdp", ids)
	}

	all, err := repository.ListRecoverableRDPRecordings(
		context.Background(),
		true,
	)
	if err != nil {
		t.Fatalf("list startup recovery items: %v", err)
	}
	if ids := recoverySessionIDs(all); !slices.Equal(
		ids,
		[]string{"active-rdp", "ended-rdp"},
	) {
		t.Fatalf("startup recovery sessions = %#v", ids)
	}
}

func recoveryArtifact(
	sessionID string,
	status string,
	errorMessage string,
) model.AuditArtifact {
	return model.AuditArtifact{
		ID: "artifact-" + sessionID, AuditSessionID: sessionID,
		Kind:      model.AuditArtifactKindRecording,
		Format:    model.AuditArtifactFormatGuac,
		ObjectKey: "rdp/" + sessionID + "/recording.guac",
		Status:    status, ErrorMessage: errorMessage,
	}
}

func recoverySessionIDs(items []service.RDPRecordingRecoveryItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.Session.ID)
	}
	slices.Sort(ids)
	return ids
}
