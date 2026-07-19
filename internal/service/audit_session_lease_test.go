package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type auditSessionLeaseRepositoryStub struct {
	heartbeatAt  time.Time
	recoverAt    time.Time
	recovered    int64
	heartbeatErr error
	recoverErr   error
}

func (s *auditSessionLeaseRepositoryStub) HeartbeatActiveAuditSessions(
	_ context.Context,
	heartbeatAt time.Time,
) error {
	s.heartbeatAt = heartbeatAt
	return s.heartbeatErr
}

func (s *auditSessionLeaseRepositoryStub) RecoverExpiredAuditSessions(
	_ context.Context,
	now time.Time,
) (int64, error) {
	s.recoverAt = now
	return s.recovered, s.recoverErr
}

func TestAuditSessionLeaseServiceDelegatesUTCHeartbeatAndRecovery(t *testing.T) {
	repository := &auditSessionLeaseRepositoryStub{recovered: 3}
	service, err := NewAuditSessionLeaseService(repository)
	if err != nil {
		t.Fatalf("NewAuditSessionLeaseService: %v", err)
	}
	local := time.Date(
		2026,
		7,
		19,
		23,
		0,
		0,
		0,
		time.FixedZone("UTC+8", 8*60*60),
	)
	if err := service.Heartbeat(context.Background(), local); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	recovered, err := service.RecoverExpired(context.Background(), local)
	if err != nil {
		t.Fatalf("RecoverExpired: %v", err)
	}
	if recovered != 3 ||
		repository.heartbeatAt.Location() != time.UTC ||
		repository.recoverAt.Location() != time.UTC ||
		!repository.heartbeatAt.Equal(local) ||
		!repository.recoverAt.Equal(local) {
		t.Fatalf(
			"delegated heartbeat=%v recovery=%v recovered=%d",
			repository.heartbeatAt,
			repository.recoverAt,
			recovered,
		)
	}
}

func TestAuditSessionLeaseServicePropagatesMaintenanceFailures(t *testing.T) {
	repository := &auditSessionLeaseRepositoryStub{
		heartbeatErr: errors.New("heartbeat unavailable"),
		recoverErr:   errors.New("recovery unavailable"),
	}
	service, err := NewAuditSessionLeaseService(repository)
	if err != nil {
		t.Fatalf("NewAuditSessionLeaseService: %v", err)
	}
	if err := service.Heartbeat(context.Background(), time.Now()); err == nil {
		t.Fatal("Heartbeat unexpectedly succeeded")
	}
	if _, err := service.RecoverExpired(context.Background(), time.Now()); err == nil {
		t.Fatal("RecoverExpired unexpectedly succeeded")
	}
}
