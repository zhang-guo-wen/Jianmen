package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type AuditSessionLeaseRepository interface {
	HeartbeatActiveAuditSessions(ctx context.Context, heartbeatAt time.Time) error
	RecoverExpiredAuditSessions(ctx context.Context, now time.Time) (int64, error)
}

type AuditSessionLeaseService struct {
	repository AuditSessionLeaseRepository
}

func NewAuditSessionLeaseService(
	repository AuditSessionLeaseRepository,
) (*AuditSessionLeaseService, error) {
	if repository == nil {
		return nil, errors.New("audit session lease repository is required")
	}
	return &AuditSessionLeaseService{repository: repository}, nil
}

func (s *AuditSessionLeaseService) Heartbeat(
	ctx context.Context,
	now time.Time,
) error {
	if ctx == nil {
		return errors.New("heartbeat audit session leases: nil context")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if err := s.repository.HeartbeatActiveAuditSessions(ctx, now); err != nil {
		return fmt.Errorf("heartbeat audit session leases: %w", err)
	}
	return nil
}

func (s *AuditSessionLeaseService) RecoverExpired(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	if ctx == nil {
		return 0, errors.New("recover expired audit session leases: nil context")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	recovered, err := s.repository.RecoverExpiredAuditSessions(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("recover expired audit session leases: %w", err)
	}
	return recovered, nil
}
