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
	auditLeaseExpiredFailureCode    = "audit_lease_expired"
	auditLeaseExpiredFailureMessage = "audit session lease expired without a heartbeat"
	maxAuditLeaseHeartbeatBatch     = 500
)

func (s *DBStore) prepareAuditSessionLease(session *model.AuditSession) {
	if session == nil || session.State != "started" {
		return
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.auditLeaseDuration)
	session.LeaseOwner = s.auditLeaseOwner
	session.HeartbeatAt = &now
	session.LeaseExpiresAt = &expiresAt
}

func (s *DBStore) trackAuditSessionLease(session *model.AuditSession) {
	if session == nil || session.State != "started" || strings.TrimSpace(session.ID) == "" {
		return
	}
	s.auditLeaseMu.Lock()
	s.activeAuditLeases[session.ID] = struct{}{}
	s.auditLeaseMu.Unlock()
}

func (s *DBStore) untrackAuditSessionLease(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	s.auditLeaseMu.Lock()
	delete(s.activeAuditLeases, id)
	s.auditLeaseMu.Unlock()
}

func (s *DBStore) activeAuditSessionIDs() []string {
	s.auditLeaseMu.RLock()
	ids := make([]string, 0, len(s.activeAuditLeases))
	for id := range s.activeAuditLeases {
		ids = append(ids, id)
	}
	s.auditLeaseMu.RUnlock()
	return ids
}

// HeartbeatActiveAuditSessions renews only sessions that this process still
// considers active. Sessions are untracked before an end write is attempted,
// so a failed end write cannot be renewed forever.
func (s *DBStore) HeartbeatActiveAuditSessions(
	ctx context.Context,
	heartbeatAt time.Time,
) error {
	if ctx == nil {
		return errors.New("heartbeat audit sessions: nil context")
	}
	heartbeatAt = heartbeatAt.UTC()
	if heartbeatAt.IsZero() {
		return errors.New("heartbeat audit sessions: heartbeat time is required")
	}
	s.auditLeaseMu.RLock()
	defer s.auditLeaseMu.RUnlock()
	ids := make([]string, 0, len(s.activeAuditLeases))
	for id := range s.activeAuditLeases {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	expiresAt := heartbeatAt.Add(s.auditLeaseDuration)
	var renewed int64
	for start := 0; start < len(ids); start += maxAuditLeaseHeartbeatBatch {
		end := min(start+maxAuditLeaseHeartbeatBatch, len(ids))
		batch := ids[start:end]
		result := s.db.WithContext(ctx).
			Model(&model.AuditSession{}).
			Where(
				"id IN ? AND state = ? AND lease_owner = ?",
				batch,
				"started",
				s.auditLeaseOwner,
			).
			Updates(map[string]any{
				"heartbeat_at":     heartbeatAt,
				"lease_expires_at": expiresAt,
			})
		if result.Error != nil {
			return fmt.Errorf("heartbeat active audit sessions: %w", result.Error)
		}
		renewed += result.RowsAffected
	}
	if renewed != int64(len(ids)) {
		return fmt.Errorf(
			"heartbeat active audit sessions: renewed %d of %d active sessions",
			renewed,
			len(ids),
		)
	}
	return nil
}

// RecoverExpiredAuditSessions atomically interrupts only started sessions with
// complete, internally consistent lease evidence that has crossed its explicit
// expiry. Rows without a lease are deliberately not guessed from StartedAt.
func (s *DBStore) RecoverExpiredAuditSessions(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	if ctx == nil {
		return 0, errors.New("recover expired audit sessions: nil context")
	}
	now = now.UTC()
	if now.IsZero() {
		return 0, errors.New("recover expired audit sessions: recovery time is required")
	}
	result := s.db.WithContext(ctx).
		Model(&model.AuditSession{}).
		Where("state = ?", "started").
		Where("lease_owner IS NOT NULL AND lease_owner <> ''").
		Where("heartbeat_at IS NOT NULL AND lease_expires_at IS NOT NULL").
		Where("heartbeat_at <= lease_expires_at AND lease_expires_at <= ?", now).
		Updates(map[string]any{
			"state":           "ended",
			"ended_at":        gorm.Expr("lease_expires_at"),
			"outcome":         model.AuditOutcomeTerminated,
			"failure_code":    auditLeaseExpiredFailureCode,
			"failure_message": auditLeaseExpiredFailureMessage,
			"cleanup_status":  auditCleanupReady,
			"cleanup_at":      nil,
			"cleanup_error":   "",
		})
	if result.Error != nil {
		return 0, fmt.Errorf("recover expired audit sessions: %w", result.Error)
	}
	return result.RowsAffected, nil
}
