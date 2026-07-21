package service

import (
	"context"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestRDPRecordingServiceRecordsDeniedAttemptWithoutArtifact(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)

	err := service.RecordDenied(context.Background(), DeniedRDPAuditInput{
		ID: "connection-1", UserSessionID: "browser-session-1",
		UserID: "user-1", Username: "alice", TargetID: "account-1",
		ClientIP: "192.0.2.10", FailureCode: "rbac_denied",
		FailureMessage: "RDP access is not authorized",
	})
	if err != nil {
		t.Fatalf("RecordDenied() error = %v", err)
	}
	session := repository.beginSession
	if session == nil {
		t.Fatal("denied audit session was not persisted")
	}
	if session.ID != "connection-1" ||
		session.UserSessionID != "browser-session-1" ||
		session.UserID != "user-1" ||
		session.Username != "alice" ||
		session.ResourceType != model.ResourceTypeHostAccount ||
		session.ResourceID != "account-1" ||
		session.AccountID != "account-1" ||
		session.Outcome != model.AuditOutcomeDenied ||
		session.State != "ended" ||
		session.EndedAt == nil ||
		session.FailureCode != "rbac_denied" ||
		session.RecordingStatus != model.RecordingStatusNone {
		t.Fatalf("denied audit session = %#v", session)
	}
	if repository.beginArtifact != nil || objects.putCalls != 0 {
		t.Fatalf(
			"denied attempt allocated recording: artifact=%#v puts=%d",
			repository.beginArtifact,
			objects.putCalls,
		)
	}
}
