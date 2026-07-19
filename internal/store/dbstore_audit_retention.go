package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const (
	auditCleanupReady    = "ready"
	auditCleanupPending  = "pending"
	auditCleanupFailed   = "failed"
	maxAuditCleanupBatch = 1000
	maxAuditCleanupError = 2048
)

// ClaimAuditSessionsForCleanup records cleanup intent before any replay file is
// touched. A stale pending/failed claim can be retried after staleBefore.
func (s *DBStore) ClaimAuditSessionsForCleanup(
	ctx context.Context,
	cutoff time.Time,
	claimedAt time.Time,
	staleBefore time.Time,
	limit int,
) ([]model.AuditSession, error) {
	if ctx == nil {
		return nil, errors.New("claim audit cleanup sessions: nil context")
	}
	if limit <= 0 {
		return []model.AuditSession{}, nil
	}
	if limit > maxAuditCleanupBatch {
		limit = maxAuditCleanupBatch
	}

	claimed := make([]model.AuditSession, 0, limit)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidates []model.AuditSession
		query := auditCleanupEligible(tx.Model(&model.AuditSession{}), staleBefore).
			Select("id", "replay_dir", "ended_at", "cleanup_status", "cleanup_at").
			Where("state = ? AND ended_at IS NOT NULL AND ended_at <= ?", "ended", cutoff).
			Order("ended_at ASC, id ASC").
			Limit(limit)
		if err := query.Find(&candidates).Error; err != nil {
			return fmt.Errorf("list audit cleanup candidates: %w", err)
		}

		for _, candidate := range candidates {
			update := auditCleanupEligible(
				tx.Model(&model.AuditSession{}).
					Where("id = ? AND state = ? AND ended_at IS NOT NULL AND ended_at <= ?",
						candidate.ID, "ended", cutoff),
				staleBefore,
			).Updates(map[string]any{
				"cleanup_status": auditCleanupPending,
				"cleanup_at":     claimedAt,
				"cleanup_error":  "",
			})
			if update.Error != nil {
				return fmt.Errorf("claim audit session %q: %w", candidate.ID, update.Error)
			}
			if update.RowsAffected != 1 {
				continue
			}
			candidate.CleanupStatus = auditCleanupPending
			candidate.CleanupAt = &claimedAt
			candidate.CleanupError = ""
			claimed = append(claimed, candidate)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func auditCleanupEligible(query *gorm.DB, staleBefore time.Time) *gorm.DB {
	return query.Where(
		`(
			cleanup_status = ? OR cleanup_status = '' OR cleanup_status IS NULL
			OR (
				cleanup_status IN ?
				AND (cleanup_at IS NULL OR cleanup_at <= ?)
			)
		)`,
		auditCleanupReady,
		[]string{auditCleanupPending, auditCleanupFailed},
		staleBefore,
	)
}

// MarkAuditSessionCleanupFailed keeps the database row and makes the claim
// retryable after the stale-claim delay.
func (s *DBStore) MarkAuditSessionCleanupFailed(
	ctx context.Context,
	id string,
	failedAt time.Time,
	message string,
) error {
	if ctx == nil {
		return errors.New("mark audit cleanup failed: nil context")
	}
	message = strings.TrimSpace(message)
	if len(message) > maxAuditCleanupError {
		message = message[:maxAuditCleanupError]
	}
	result := s.db.WithContext(ctx).
		Model(&model.AuditSession{}).
		Where("id = ? AND state = ? AND cleanup_status = ?", id, "ended", auditCleanupPending).
		Updates(map[string]any{
			"cleanup_status": auditCleanupFailed,
			"cleanup_at":     failedAt,
			"cleanup_error":  message,
		})
	if result.Error != nil {
		return fmt.Errorf("mark audit session %q cleanup failed: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("mark audit session %q cleanup failed: claim not found", id)
	}
	return nil
}

// DeleteClaimedAuditSession removes all relational audit data in one
// transaction. It may only delete an ended session with a persisted claim.
func (s *DBStore) DeleteClaimedAuditSession(ctx context.Context, id string) error {
	if ctx == nil {
		return errors.New("delete claimed audit session: nil context")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.AuditSession{}).
			Where("id = ? AND state = ? AND cleanup_status = ?", id, "ended", auditCleanupPending).
			Count(&count).Error; err != nil {
			return fmt.Errorf("verify audit session %q cleanup claim: %w", id, err)
		}
		if count != 1 {
			return fmt.Errorf("delete audit session %q: cleanup claim not found", id)
		}
		for _, child := range []any{
			&model.AuditSSHCommand{},
			&model.AuditDBQuery{},
			&model.AuditSFTPEvent{},
		} {
			if err := tx.Where("audit_session_id = ?", id).Delete(child).Error; err != nil {
				return fmt.Errorf("delete audit session %q child records: %w", id, err)
			}
		}
		result := tx.Where("id = ? AND state = ? AND cleanup_status = ?",
			id, "ended", auditCleanupPending).
			Delete(&model.AuditSession{})
		if result.Error != nil {
			return fmt.Errorf("delete audit session %q: %w", id, result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("delete audit session %q: cleanup claim changed", id)
		}
		return nil
	})
}
