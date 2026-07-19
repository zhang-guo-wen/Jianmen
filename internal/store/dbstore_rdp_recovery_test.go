package store

import (
	"context"
	"fmt"
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
		{
			ID: "cleanup-pending-rdp", Protocol: "rdp", State: "ended",
			Outcome: model.AuditOutcomeFailed, StartedAt: now.Add(-time.Hour),
			EndedAt: &endedAt, CleanupStatus: "pending", CleanupAt: &now,
		},
	}
	if err := db.Create(&sessions).Error; err != nil {
		t.Fatalf("create audit sessions: %v", err)
	}
	artifacts := []model.AuditArtifact{
		recoveryArtifact("ended-rdp", model.RecordingStatusFailed, "", now.Add(-time.Hour)),
		recoveryArtifact("active-rdp", model.RecordingStatusPending, "", now),
		recoveryArtifact(
			"unrecoverable-rdp",
			model.RecordingStatusFailed,
			"recording spool unavailable for recovery",
			now.Add(-time.Hour),
		),
		recoveryArtifact("ended-ssh", model.RecordingStatusFailed, "", now.Add(-time.Hour)),
		recoveryArtifact(
			"cleanup-pending-rdp",
			model.RecordingStatusFailed,
			"",
			now.Add(-time.Hour),
		),
	}
	if err := db.Create(&artifacts).Error; err != nil {
		t.Fatalf("create audit artifacts: %v", err)
	}
	repository := NewDBStore(db)

	ended, err := repository.ClaimRecoverableRDPRecordings(
		context.Background(),
		false,
		now,
		now.Add(-5*time.Minute),
	)
	if err != nil {
		t.Fatalf("list ended recovery items: %v", err)
	}
	if ids := recoverySessionIDs(ended); !slices.Equal(ids, []string{"ended-rdp"}) {
		t.Fatalf("ended recovery sessions = %#v, want ended-rdp", ids)
	}
	second, err := repository.ClaimRecoverableRDPRecordings(
		context.Background(),
		false,
		now,
		now.Add(-5*time.Minute),
	)
	if err != nil {
		t.Fatalf("second ended recovery claim: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("fresh uploading recovery was reclaimed: %#v", second)
	}

	all, err := repository.ClaimRecoverableRDPRecordings(
		context.Background(),
		true,
		now,
		now.Add(-5*time.Minute),
	)
	if err != nil {
		t.Fatalf("list startup recovery items: %v", err)
	}
	if ids := recoverySessionIDs(all); !slices.Equal(
		ids,
		[]string{"active-rdp"},
	) {
		t.Fatalf("startup recovery sessions = %#v", ids)
	}
	var claimedArtifact model.AuditArtifact
	if err := db.First(
		&claimedArtifact,
		"id = ?",
		"artifact-ended-rdp",
	).Error; err != nil {
		t.Fatalf("load claimed artifact: %v", err)
	}
	if claimedArtifact.Status != model.RecordingStatusUploading {
		t.Fatalf("claimed artifact status = %q", claimedArtifact.Status)
	}
}

func TestClaimRecoverableRDPRecordingsContinuesPastBatchLimit(t *testing.T) {
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
	now := time.Date(2026, 7, 19, 16, 0, 0, 0, time.UTC)
	for index := 0; index < 205; index++ {
		sessionID := fmt.Sprintf("interrupted-%03d", index)
		session := model.AuditSession{
			ID: sessionID, Protocol: "rdp", State: "active",
			Outcome: model.AuditOutcomeActive, StartedAt: now.Add(-time.Hour),
		}
		if err := db.Create(&session).Error; err != nil {
			t.Fatalf("create session %s: %v", sessionID, err)
		}
		artifact := recoveryArtifact(
			sessionID,
			model.RecordingStatusPending,
			"",
			now.Add(-time.Hour),
		)
		if err := db.Create(&artifact).Error; err != nil {
			t.Fatalf("create artifact %s: %v", sessionID, err)
		}
	}
	repository := NewDBStore(db)

	var batchSizes []int
	for {
		items, claimErr := repository.ClaimRecoverableRDPRecordings(
			context.Background(),
			true,
			now,
			now.Add(-5*time.Minute),
		)
		if claimErr != nil {
			t.Fatalf("claim recovery batch: %v", claimErr)
		}
		if len(items) == 0 {
			break
		}
		batchSizes = append(batchSizes, len(items))
	}
	if !slices.Equal(batchSizes, []int{100, 100, 5}) {
		t.Fatalf("recovery batch sizes = %#v, want 100, 100, 5", batchSizes)
	}
}

func recoveryArtifact(
	sessionID string,
	status string,
	errorMessage string,
	updatedAt time.Time,
) model.AuditArtifact {
	return model.AuditArtifact{
		ID: "artifact-" + sessionID, AuditSessionID: sessionID,
		Kind:      model.AuditArtifactKindRecording,
		Format:    model.AuditArtifactFormatGuac,
		ObjectKey: "rdp/" + sessionID + "/recording.guac",
		Status:    status, ErrorMessage: errorMessage,
		CreatedAt: updatedAt, UpdatedAt: updatedAt,
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
