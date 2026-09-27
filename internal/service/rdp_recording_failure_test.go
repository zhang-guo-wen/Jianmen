package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestRDPRecordingServiceStorageFailuresFailClosed(t *testing.T) {
	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	recording := []byte("guacamole recording")

	t.Run("audit begin", func(t *testing.T) {
		sentinel := errors.New("audit database unavailable")
		repository := &rdpAuditRepositoryFake{beginErr: sentinel}
		objects := &rdpObjectStoreFake{repository: repository}
		service, config := newRDPRecordingServiceForTest(t, repository, objects, now)

		handle, err := service.Begin(context.Background(), validBeginRDPAuditInput())
		if !errors.Is(err, sentinel) || handle != nil {
			t.Fatalf("Begin() = (%#v, %v), want nil and sentinel", handle, err)
		}
		if objects.putCalls != 0 {
			t.Fatalf("object upload calls = %d", objects.putCalls)
		}
		sessionDir := filepath.Join(config.SpoolRoot, "session-1")
		if _, statErr := os.Stat(sessionDir); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed audit left spool directory: %v", statErr)
		}
	})

	t.Run("artifact uploading state", func(t *testing.T) {
		sentinel := errors.New("artifact database unavailable")
		repository := &rdpAuditRepositoryFake{
			updateErrAt: map[int]error{1: sentinel},
		}
		objects := &rdpObjectStoreFake{repository: repository}
		service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
		handle := beginWithRecording(t, service, recording)

		err := service.Finish(
			context.Background(), handle, model.AuditOutcomeSucceeded, "", "",
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Finish() error = %v, want sentinel", err)
		}
		assertFailedRecordingFinish(t, repository)
		if objects.putCalls != 0 {
			t.Fatalf("object upload occurred without uploading index: %d", objects.putCalls)
		}
	})

	t.Run("object upload", func(t *testing.T) {
		sentinel := errors.New("object storage unavailable")
		repository := &rdpAuditRepositoryFake{}
		objects := &rdpObjectStoreFake{repository: repository, putErr: sentinel}
		service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
		handle := beginWithRecording(t, service, recording)

		err := service.Finish(
			context.Background(), handle, model.AuditOutcomeSucceeded, "", "",
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Finish() error = %v, want sentinel", err)
		}
		assertFailedRecordingFinish(t, repository)
		if handle.Artifact.Status != model.RecordingStatusFailed {
			t.Fatalf("artifact status = %q", handle.Artifact.Status)
		}
	})

	t.Run("artifact ready state", func(t *testing.T) {
		sentinel := errors.New("ready state database unavailable")
		repository := &rdpAuditRepositoryFake{
			updateErrAt: map[int]error{2: sentinel},
		}
		objects := &rdpObjectStoreFake{repository: repository}
		service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
		handle := beginWithRecording(t, service, recording)

		err := service.Finish(
			context.Background(), handle, model.AuditOutcomeSucceeded, "", "",
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Finish() error = %v, want sentinel", err)
		}
		assertFailedRecordingFinish(t, repository)
		if objects.putCalls != 1 {
			t.Fatalf("object upload calls = %d, want 1", objects.putCalls)
		}
		if _, statErr := os.Stat(handle.LocalPath); statErr != nil {
			t.Fatalf("recording needed for retry was removed: %v", statErr)
		}
	})

	t.Run("audit finish", func(t *testing.T) {
		sentinel := errors.New("finish database unavailable")
		repository := &rdpAuditRepositoryFake{finishErr: sentinel}
		objects := &rdpObjectStoreFake{repository: repository}
		service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
		handle := beginWithRecording(t, service, recording)

		err := service.Finish(
			context.Background(), handle, model.AuditOutcomeSucceeded, "", "",
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Finish() error = %v, want sentinel", err)
		}
		if repository.finish == nil ||
			repository.finish.recordingStatus != model.RecordingStatusReady {
			t.Fatalf("finish call = %#v", repository.finish)
		}
	})
}

func TestRDPRecordingServiceActivateFailurePreventsConnectionProgress(t *testing.T) {
	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	sentinel := errors.New("cannot activate audit")
	repository := &rdpAuditRepositoryFake{activateErr: sentinel}
	objects := &rdpObjectStoreFake{repository: repository}
	service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
	handle, err := service.Begin(context.Background(), validBeginRDPAuditInput())
	if err != nil {
		t.Fatal(err)
	}

	if err = service.Activate(context.Background(), handle); !errors.Is(err, sentinel) {
		t.Fatalf("Activate() error = %v, want sentinel", err)
	}
	if repository.activateCalls != 1 {
		t.Fatalf("activate calls = %d", repository.activateCalls)
	}
}

func TestRDPRecordingServiceRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	for _, sessionID := range []string{
		"", ".", "..", "../escape", `..\escape`, "nested/session", `nested\session`, "bad\x00id",
	} {
		t.Run(strings.ReplaceAll(sessionID, "\x00", "NUL"), func(t *testing.T) {
			if dir, err := safeSessionDir(root, sessionID); err == nil {
				t.Fatalf("safeSessionDir(%q) = %q, want error", sessionID, dir)
			}
		})
	}
	dir, err := safeSessionDir(root, "session-1")
	if err != nil {
		t.Fatalf("safeSessionDir(valid) error = %v", err)
	}
	relative, err := filepath.Rel(root, dir)
	if err != nil || relative != "session-1" {
		t.Fatalf("safe path relative = %q, error = %v", relative, err)
	}

	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
	input := validBeginRDPAuditInput()
	input.ID = "../escape"
	if handle, beginErr := service.Begin(context.Background(), input); beginErr == nil || handle != nil {
		t.Fatalf("Begin(unsafe id) = (%#v, %v)", handle, beginErr)
	}
	if repository.beginSession != nil {
		t.Fatalf("unsafe id reached audit repository: %#v", repository.beginSession)
	}
}

func TestRDPRecordingServiceDriveMappingRequiresBothRoots(t *testing.T) {
	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	for _, missing := range []string{"local", "guacd"} {
		t.Run(missing, func(t *testing.T) {
			repository := &rdpAuditRepositoryFake{}
			objects := &rdpObjectStoreFake{repository: repository}
			service, config := newRDPRecordingServiceForTest(t, repository, objects, now)
			if missing == "local" {
				service.config.LocalDriveRoot = ""
			} else {
				service.config.GuacdDriveRoot = ""
			}
			input := validBeginRDPAuditInput()
			input.Policy.DriveMapping = true

			handle, err := service.Begin(context.Background(), input)
			if err == nil || handle != nil {
				t.Fatalf("Begin() = (%#v, %v), want root error", handle, err)
			}
			if repository.beginSession != nil {
				t.Fatalf("invalid drive roots reached audit repository")
			}
			sessionDir := filepath.Join(config.SpoolRoot, input.ID)
			if _, statErr := os.Stat(sessionDir); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("invalid drive roots left spool directory: %v", statErr)
			}
		})
	}
}

func TestRDPRecordingServiceAllowUnrecordedDoesNotCreateArtifact(t *testing.T) {
	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	service, config := newRDPRecordingServiceForTest(t, repository, objects, now)
	service.config.AllowUnrecorded = true
	if err := os.MkdirAll(filepath.Dir(config.SpoolRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.SpoolRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	handle, err := service.Begin(context.Background(), validBeginRDPAuditInput())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if handle.Artifact != nil || handle.Session.RecordingStatus != model.RecordingStatusNone {
		t.Fatalf("unrecorded handle = %#v", handle)
	}
	if repository.beginArtifact != nil {
		t.Fatalf("unrecorded session created artifact: %#v", repository.beginArtifact)
	}
}

func TestRDPRecordingServiceFinishRemovesDriveSpool(t *testing.T) {
	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
	input := validBeginRDPAuditInput()
	input.Policy.DriveMapping = true
	handle, err := service.Begin(context.Background(), input)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err = os.WriteFile(handle.LocalPath, []byte("recording"), 0o600); err != nil {
		t.Fatal(err)
	}
	driveFile := filepath.Join(handle.LocalDrivePath, "sensitive.txt")
	if err = os.WriteFile(driveFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err = service.Finish(
		context.Background(), handle, model.AuditOutcomeSucceeded, "", "",
	); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if _, err = os.Stat(handle.LocalDrivePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("drive spool remains after Finish(): %v", err)
	}
}

func beginWithRecording(
	t *testing.T,
	service *RDPRecordingService,
	recording []byte,
) *RDPAuditHandle {
	t.Helper()
	handle, err := service.Begin(context.Background(), validBeginRDPAuditInput())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err = os.WriteFile(handle.LocalPath, recording, 0o600); err != nil {
		t.Fatalf("write recording: %v", err)
	}
	return handle
}

func assertFailedRecordingFinish(t *testing.T, repository *rdpAuditRepositoryFake) {
	t.Helper()
	if repository.finish == nil ||
		repository.finish.recordingStatus != model.RecordingStatusFailed {
		t.Fatalf("finish call = %#v, want failed recording", repository.finish)
	}
}
