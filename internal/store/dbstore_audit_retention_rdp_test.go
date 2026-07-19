package store

import (
	"context"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestDBStoreAuditRetentionReplayQuotaSkipsObjectBackedSessions(
	t *testing.T,
) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()
	now := time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC)
	rdpEndedAt := now.Add(-2 * time.Hour)
	sshEndedAt := now.Add(-time.Hour)
	sessions := []model.AuditSession{
		{
			ID: "rdp-object", Protocol: "rdp", State: "ended",
			StartedAt: rdpEndedAt, EndedAt: &rdpEndedAt,
			CleanupStatus: "ready",
		},
		{
			ID: "ssh-replay", Protocol: "ssh", State: "ended",
			StartedAt: sshEndedAt, EndedAt: &sshEndedAt,
			ReplayDir: "ssh/ssh-replay", CleanupStatus: "ready",
		},
	}
	if err := repository.db.Create(&sessions).Error; err != nil {
		t.Fatalf("create audit sessions: %v", err)
	}

	claimed, err := repository.ClaimAuditSessionsForCleanup(
		context.Background(),
		now,
		now,
		now.Add(-15*time.Minute),
		10,
		true,
	)
	if err != nil {
		t.Fatalf("claim replay quota sessions: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != "ssh-replay" {
		t.Fatalf("replay quota claims = %#v, want ssh-replay", claimed)
	}
}

func TestDBStoreAuditRetentionWaitsForRecoverableRDPArtifacts(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()
	now := time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC)
	endedAt := now.AddDate(0, 0, -60)
	statuses := []struct {
		sessionID string
		status    string
		error     string
	}{
		{
			sessionID: "failed-recoverable",
			status:    model.RecordingStatusFailed,
			error:     "temporary object storage failure",
		},
		{
			sessionID: "failed-unavailable",
			status:    model.RecordingStatusFailed,
			error:     "recording spool unavailable for recovery: file not found",
		},
		{
			sessionID: "pending",
			status:    model.RecordingStatusPending,
		},
		{
			sessionID: "ready",
			status:    model.RecordingStatusReady,
		},
		{
			sessionID: "uploading",
			status:    model.RecordingStatusUploading,
		},
	}
	for _, item := range statuses {
		session := model.AuditSession{
			ID: item.sessionID, Protocol: "rdp", State: "ended",
			StartedAt: endedAt, EndedAt: &endedAt, CleanupStatus: "ready",
		}
		if err := repository.db.Create(&session).Error; err != nil {
			t.Fatalf("create audit session %s: %v", item.sessionID, err)
		}
		artifact := model.AuditArtifact{
			ID: item.sessionID + "-artifact", AuditSessionID: item.sessionID,
			Kind: model.AuditArtifactKindRecording, Format: model.AuditArtifactFormatGuac,
			ObjectKey:    "rdp/" + item.sessionID + "/recording.guac",
			Status:       item.status,
			ErrorMessage: item.error,
		}
		if err := repository.db.Create(&artifact).Error; err != nil {
			t.Fatalf("create audit artifact %s: %v", item.sessionID, err)
		}
	}

	claimed, err := repository.ClaimAuditSessionsForCleanup(
		context.Background(),
		now.AddDate(0, 0, -30),
		now,
		now.Add(-15*time.Minute),
		10,
		false,
	)
	if err != nil {
		t.Fatalf("claim cleanup sessions: %v", err)
	}
	if len(claimed) != 2 ||
		claimed[0].ID != "failed-unavailable" ||
		claimed[1].ID != "ready" {
		t.Fatalf("claimed sessions = %#v, want failed-unavailable and ready", claimed)
	}
}
