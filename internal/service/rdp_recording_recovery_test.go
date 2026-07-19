package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestRDPRecordingRecoveryUploadsRetainedSpool(t *testing.T) {
	now := time.Date(2026, 7, 19, 13, 0, 0, 0, time.UTC)
	endedAt := now.Add(-time.Minute)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	recording, config := newRDPRecordingServiceForTest(
		t,
		repository,
		objects,
		now,
	)
	content := []byte("recovered-guacamole-recording")
	localDir := filepath.Join(config.SpoolRoot, "session-recovery")
	if err := os.MkdirAll(localDir, 0o700); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(localDir, rdpRecordingFilename)
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	repository.recoveryItems = []RDPRecordingRecoveryItem{{
		Session: model.AuditSession{
			ID: "session-recovery", Protocol: "rdp",
			State: "ended", Outcome: model.AuditOutcomeSucceeded,
			EndedAt: &endedAt,
		},
		Artifact: model.AuditArtifact{
			ID: "artifact-recovery", AuditSessionID: "session-recovery",
			Kind:        model.AuditArtifactKindRecording,
			Format:      model.AuditArtifactFormatGuac,
			ObjectKey:   "rdp/session-recovery/recording.guac",
			ContentType: rdpRecordingContentType,
			Status:      model.RecordingStatusFailed,
		},
	}}

	if err := recording.Recover(context.Background(), false); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	if len(repository.recoveryCalls) != 2 ||
		repository.recoveryCalls[0] ||
		repository.recoveryCalls[1] {
		t.Fatalf("recovery calls = %#v, want two ended-only batches", repository.recoveryCalls)
	}
	if objects.putCalls != 1 || !bytes.Equal(objects.putBody, content) {
		t.Fatalf("recovered object upload = %#v", objects)
	}
	if repository.finish == nil ||
		repository.finish.outcome != model.AuditOutcomeSucceeded ||
		repository.finish.recordingStatus != model.RecordingStatusReady ||
		!repository.finish.endedAt.Equal(endedAt) {
		t.Fatalf("recovered audit finish = %#v", repository.finish)
	}
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatalf("recovered local recording remains: %v", err)
	}
}

func TestRDPRecordingRecoveryMarksMissingSpoolUnrecoverable(t *testing.T) {
	now := time.Date(2026, 7, 19, 13, 0, 0, 0, time.UTC)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	recording, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
	repository.recoveryItems = []RDPRecordingRecoveryItem{{
		Session: model.AuditSession{
			ID: "interrupted-session", Protocol: "rdp", State: "active",
		},
		Artifact: model.AuditArtifact{
			ID: "artifact-missing", AuditSessionID: "interrupted-session",
			Kind:      model.AuditArtifactKindRecording,
			ObjectKey: "rdp/interrupted-session/recording.guac",
			Status:    model.RecordingStatusPending,
		},
	}}

	err := recording.Recover(context.Background(), true)
	if err == nil {
		t.Fatal("Recover() succeeded with missing recording spool")
	}
	if len(repository.artifactStates) != 1 ||
		repository.artifactStates[0].Status != model.RecordingStatusFailed ||
		repository.artifactStates[0].ErrorMessage != recoveryUnavailableMessage {
		t.Fatalf("missing artifact state = %#v", repository.artifactStates)
	}
	if repository.finish == nil ||
		repository.finish.outcome != model.AuditOutcomeTerminated ||
		repository.finish.failureCode != "service_restarted" ||
		repository.finish.recordingStatus != model.RecordingStatusFailed {
		t.Fatalf("missing recovery audit finish = %#v", repository.finish)
	}
	if objects.putCalls != 0 {
		t.Fatalf("missing recording upload calls = %d, want 0", objects.putCalls)
	}
}

func TestRDPRecordingRecoveryDrainsEveryClaimBatch(t *testing.T) {
	now := time.Date(2026, 7, 19, 13, 0, 0, 0, time.UTC)
	endedAt := now.Add(-time.Minute)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	recording, config := newRDPRecordingServiceForTest(
		t,
		repository,
		objects,
		now,
	)
	items := make([]RDPRecordingRecoveryItem, 0, 2)
	for _, sessionID := range []string{"session-batch-1", "session-batch-2"} {
		localDir := filepath.Join(config.SpoolRoot, sessionID)
		if err := os.MkdirAll(localDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(localDir, rdpRecordingFilename),
			[]byte("recording-"+sessionID),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		items = append(items, RDPRecordingRecoveryItem{
			Session: model.AuditSession{
				ID: sessionID, Protocol: "rdp", State: "ended",
				Outcome: model.AuditOutcomeSucceeded, EndedAt: &endedAt,
			},
			Artifact: model.AuditArtifact{
				ID: "artifact-" + sessionID, AuditSessionID: sessionID,
				Kind:      model.AuditArtifactKindRecording,
				ObjectKey: "rdp/" + sessionID + "/recording.guac",
				Status:    model.RecordingStatusFailed,
			},
		})
	}
	repository.recoveryBatches = [][]RDPRecordingRecoveryItem{
		{items[0]},
		{items[1]},
	}

	if err := recording.Recover(context.Background(), true); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if len(repository.recoveryCalls) != 3 {
		t.Fatalf("recovery calls = %d, want two batches plus empty probe", len(repository.recoveryCalls))
	}
	for _, includeInterrupted := range repository.recoveryCalls {
		if !includeInterrupted {
			t.Fatalf("startup recovery lost interrupted scope: %#v", repository.recoveryCalls)
		}
	}
	if objects.putCalls != 2 || repository.finishCalls != 2 {
		t.Fatalf(
			"recovered batches: puts=%d finishes=%d, want 2 each",
			objects.putCalls,
			repository.finishCalls,
		)
	}
}
