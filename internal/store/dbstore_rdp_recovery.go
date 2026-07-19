package store

import (
	"context"
	"fmt"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func (s *DBStore) ListRecoverableRDPRecordings(
	ctx context.Context,
	includeInterrupted bool,
) ([]service.RDPRecordingRecoveryItem, error) {
	query := s.db.WithContext(ctx).
		Model(&model.AuditArtifact{}).
		Joins(
			"JOIN audit_sessions ON audit_sessions.id = audit_artifacts.audit_session_id",
		).
		Where("audit_sessions.protocol = ?", "rdp").
		Where("audit_artifacts.kind = ?", model.AuditArtifactKindRecording).
		Where(
			"audit_artifacts.status IN ?",
			[]string{
				model.RecordingStatusPending,
				model.RecordingStatusUploading,
				model.RecordingStatusFailed,
			},
		).
		Where(
			"NOT (audit_artifacts.status = ? AND audit_artifacts.error_message LIKE ?)",
			model.RecordingStatusFailed,
			"recording spool unavailable for recovery%",
		)
	if !includeInterrupted {
		query = query.Where("audit_sessions.state = ?", "ended")
	}

	var artifacts []model.AuditArtifact
	if err := query.
		Order("audit_artifacts.created_at ASC").
		Find(&artifacts).Error; err != nil {
		return nil, fmt.Errorf("list recoverable RDP artifacts: %w", err)
	}
	if len(artifacts) == 0 {
		return nil, nil
	}
	sessionIDs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		sessionIDs = append(sessionIDs, artifact.AuditSessionID)
	}
	var sessions []model.AuditSession
	if err := s.db.WithContext(ctx).
		Where("id IN ?", sessionIDs).
		Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("load recoverable RDP sessions: %w", err)
	}
	byID := make(map[string]model.AuditSession, len(sessions))
	for _, session := range sessions {
		byID[session.ID] = session
	}
	items := make([]service.RDPRecordingRecoveryItem, 0, len(artifacts))
	for _, artifact := range artifacts {
		session, found := byID[artifact.AuditSessionID]
		if !found {
			return nil, fmt.Errorf(
				"recoverable RDP artifact %q has no audit session",
				artifact.ID,
			)
		}
		items = append(items, service.RDPRecordingRecoveryItem{
			Session: session, Artifact: artifact,
		})
	}
	return items, nil
}
