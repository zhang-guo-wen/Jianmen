package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jianmen/internal/model"
)

const recoveryUnavailableMessage = "recording spool unavailable for recovery"

type RDPRecordingRecoveryItem struct {
	Session  model.AuditSession
	Artifact model.AuditArtifact
}

type rdpRecordingRecoveryRepository interface {
	ListRecoverableRDPRecordings(
		ctx context.Context,
		includeInterrupted bool,
	) ([]RDPRecordingRecoveryItem, error)
}

// Recover retries recordings left in the local spool after an upload failure
// or process interruption. The initial startup pass may include interrupted
// sessions; periodic passes must only return sessions already marked ended.
func (s *RDPRecordingService) Recover(
	ctx context.Context,
	includeInterrupted bool,
) error {
	repository, ok := s.repository.(rdpRecordingRecoveryRepository)
	if !ok {
		return errors.New("RDP audit repository does not support recording recovery")
	}
	items, err := repository.ListRecoverableRDPRecordings(
		ctx,
		includeInterrupted,
	)
	if err != nil {
		return fmt.Errorf("list recoverable RDP recordings: %w", err)
	}
	var recoveryErrors []error
	for index := range items {
		if err := s.recoverRecording(ctx, &items[index]); err != nil {
			recoveryErrors = append(
				recoveryErrors,
				fmt.Errorf(
					"recover RDP recording %q: %w",
					items[index].Session.ID,
					err,
				),
			)
		}
	}
	return errors.Join(recoveryErrors...)
}

func (s *RDPRecordingService) recoverRecording(
	ctx context.Context,
	item *RDPRecordingRecoveryItem,
) error {
	if item == nil ||
		strings.TrimSpace(item.Session.ID) == "" ||
		item.Artifact.AuditSessionID != item.Session.ID ||
		item.Artifact.Kind != model.AuditArtifactKindRecording ||
		strings.TrimSpace(item.Artifact.ObjectKey) == "" {
		return errors.New("recoverable RDP recording metadata is invalid")
	}
	localDir, err := safeSessionDir(s.config.SpoolRoot, item.Session.ID)
	if err != nil {
		return err
	}
	handle := &RDPAuditHandle{
		Session: item.Session, Artifact: &item.Artifact,
		LocalPath:      filepath.Join(localDir, rdpRecordingFilename),
		RecordingName:  rdpRecordingFilename,
		LocalDrivePath: recoveryDrivePath(s.config.LocalDriveRoot, item.Session.ID),
	}

	info, statErr := os.Lstat(handle.LocalPath)
	if statErr != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		missingErr := errors.New(recoveryUnavailableMessage)
		status, artifactErr := s.failArtifact(
			ctx,
			handle.Artifact,
			missingErr,
		)
		finishErr := s.finishRecoveredRecording(
			ctx,
			handle,
			status,
			missingErr,
		)
		cleanupEmptySpool(localDir)
		driveErr := s.cleanupDriveSpool(handle)
		return errors.Join(missingErr, artifactErr, finishErr, driveErr)
	}

	status, uploadErr := s.uploadRecording(ctx, handle)
	finishErr := s.finishRecoveredRecording(
		ctx,
		handle,
		status,
		uploadErr,
	)
	driveErr := s.cleanupDriveSpool(handle)
	return errors.Join(uploadErr, finishErr, driveErr)
}

func (s *RDPRecordingService) finishRecoveredRecording(
	ctx context.Context,
	handle *RDPAuditHandle,
	recordingStatus string,
	recordingErr error,
) error {
	session := handle.Session
	outcome := strings.TrimSpace(session.Outcome)
	failureCode := strings.TrimSpace(session.FailureCode)
	failureMessage := strings.TrimSpace(session.FailureMessage)
	endedAt := s.now().UTC()
	if session.State == "ended" && session.EndedAt != nil {
		endedAt = session.EndedAt.UTC()
		if outcome == "" {
			outcome = model.AuditOutcomeFailed
		}
	} else {
		outcome = model.AuditOutcomeTerminated
		failureCode = "service_restarted"
		failureMessage = "RDP session was interrupted by a service restart"
	}
	if recordingErr != nil && failureMessage == "" {
		failureMessage = truncateAuditFailure(recordingErr.Error())
	}
	return s.repository.FinishAuditSession(
		ctx,
		session.ID,
		outcome,
		failureCode,
		failureMessage,
		recordingStatus,
		endedAt,
	)
}

func recoveryDrivePath(root, sessionID string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	dir, err := safeSessionDir(root, sessionID)
	if err != nil {
		return ""
	}
	return dir
}
