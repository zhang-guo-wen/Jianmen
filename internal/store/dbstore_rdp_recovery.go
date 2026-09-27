package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

const maxRDPRecordingRecoveryBatch = 100

func (s *DBStore) ClaimRecoverableRDPRecordings(
	ctx context.Context,
	includeInterrupted bool,
	claimedAt time.Time,
	staleBefore time.Time,
) ([]service.RDPRecordingRecoveryItem, error) {
	if ctx == nil {
		return nil, errors.New("claim recoverable RDP recordings: nil context")
	}
	claimedAt = claimedAt.UTC()
	staleBefore = staleBefore.UTC()
	var items []service.RDPRecordingRecoveryItem
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := recoverableRDPArtifactQuery(tx, staleBefore).
			Joins(
				"JOIN audit_sessions ON audit_sessions.id = audit_artifacts.audit_session_id",
			).
			Where("audit_sessions.protocol = ?", "rdp").
			Where(
				`(
					audit_sessions.cleanup_status IS NULL
					OR audit_sessions.cleanup_status = ''
					OR audit_sessions.cleanup_status = ?
				)`,
				auditCleanupReady,
			)
		query = scopeRecoverableRDPSessions(query, includeInterrupted, claimedAt)

		var candidates []model.AuditArtifact
		if err := query.
			Order("audit_artifacts.created_at ASC, audit_artifacts.id ASC").
			Limit(maxRDPRecordingRecoveryBatch).
			Find(&candidates).Error; err != nil {
			return fmt.Errorf("list recoverable RDP artifacts: %w", err)
		}
		artifacts := make([]model.AuditArtifact, 0, len(candidates))
		for _, artifact := range candidates {
			updateQuery := recoverableRDPArtifactQuery(
				tx.Model(&model.AuditArtifact{}).Where("id = ?", artifact.ID),
				staleBefore,
			)
			sessionScope, sessionArgs := recoverableRDPSessionExists(
				includeInterrupted,
				claimedAt,
			)
			existsArgs := append([]any{auditCleanupReady}, sessionArgs...)
			update := updateQuery.
				Where(
					`EXISTS (
						SELECT 1 FROM audit_sessions
						WHERE audit_sessions.id = audit_artifacts.audit_session_id
						AND (
							audit_sessions.cleanup_status IS NULL
							OR audit_sessions.cleanup_status = ''
							OR audit_sessions.cleanup_status = ?
						)
					`+sessionScope+`)`,
					existsArgs...,
				).
				Updates(map[string]any{
					"status":     model.RecordingStatusUploading,
					"updated_at": claimedAt,
				})
			if update.Error != nil {
				return fmt.Errorf(
					"claim recoverable RDP artifact %q: %w",
					artifact.ID,
					update.Error,
				)
			}
			if update.RowsAffected != 1 {
				continue
			}
			artifact.Status = model.RecordingStatusUploading
			artifact.UpdatedAt = claimedAt
			artifacts = append(artifacts, artifact)
		}
		if len(artifacts) == 0 {
			return nil
		}

		sessionIDs := make([]string, 0, len(artifacts))
		for _, artifact := range artifacts {
			sessionIDs = append(sessionIDs, artifact.AuditSessionID)
		}
		var sessions []model.AuditSession
		if err := tx.Where("id IN ?", sessionIDs).Find(&sessions).Error; err != nil {
			return fmt.Errorf("load recoverable RDP sessions: %w", err)
		}
		byID := make(map[string]model.AuditSession, len(sessions))
		for _, session := range sessions {
			byID[session.ID] = session
		}
		items = make([]service.RDPRecordingRecoveryItem, 0, len(artifacts))
		for _, artifact := range artifacts {
			session, found := byID[artifact.AuditSessionID]
			if !found {
				return fmt.Errorf(
					"recoverable RDP artifact %q has no audit session",
					artifact.ID,
				)
			}
			items = append(items, service.RDPRecordingRecoveryItem{
				Session: session, Artifact: artifact,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func scopeRecoverableRDPSessions(
	query *gorm.DB,
	includeInterrupted bool,
	now time.Time,
) *gorm.DB {
	if !includeInterrupted {
		return query.Where("audit_sessions.state = ?", "ended")
	}
	return query.Where(
		`(
			audit_sessions.state = ?
			OR (
				audit_sessions.state = ?
				AND audit_sessions.lease_owner IS NOT NULL
				AND audit_sessions.lease_owner <> ''
				AND audit_sessions.heartbeat_at IS NOT NULL
				AND audit_sessions.lease_expires_at IS NOT NULL
				AND audit_sessions.heartbeat_at <= audit_sessions.lease_expires_at
				AND audit_sessions.lease_expires_at <= ?
			)
		)`,
		"ended",
		"started",
		now,
	)
}

func recoverableRDPSessionExists(
	includeInterrupted bool,
	now time.Time,
) (string, []any) {
	if !includeInterrupted {
		return "\nAND audit_sessions.state = ?", []any{"ended"}
	}
	return `
	AND (
		audit_sessions.state = ?
		OR (
			audit_sessions.state = ?
			AND audit_sessions.lease_owner IS NOT NULL
			AND audit_sessions.lease_owner <> ''
			AND audit_sessions.heartbeat_at IS NOT NULL
			AND audit_sessions.lease_expires_at IS NOT NULL
			AND audit_sessions.heartbeat_at <= audit_sessions.lease_expires_at
			AND audit_sessions.lease_expires_at <= ?
		)
	)`, []any{"ended", "started", now}
}

func recoverableRDPArtifactQuery(
	query *gorm.DB,
	staleBefore time.Time,
) *gorm.DB {
	return query.
		Where("audit_artifacts.kind = ?", model.AuditArtifactKindRecording).
		Where(
			`(
				audit_artifacts.status = ?
				OR (
					audit_artifacts.status IN ?
					AND audit_artifacts.updated_at <= ?
				)
			)`,
			model.RecordingStatusPending,
			[]string{
				model.RecordingStatusFailed,
				model.RecordingStatusUploading,
			},
			staleBefore,
		).
		Where(
			"NOT (audit_artifacts.status = ? AND audit_artifacts.error_message LIKE ?)",
			model.RecordingStatusFailed,
			recoveryUnavailableMessagePrefix,
		)
}

const recoveryUnavailableMessagePrefix = "recording spool unavailable for recovery%"
