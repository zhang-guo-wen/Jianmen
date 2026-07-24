package sshserver

import (
	"path/filepath"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/store"
)

func TestTargetUnavailableReasonRejectsExpiredAccount(t *testing.T) {
	now := time.Now().UTC()
	target := store.TargetConfig{ID: "expired", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339Nano)}
	if got := targetUnavailableReason(target, now); got != "expired" {
		t.Fatalf("reason = %q, want expired", got)
	}
	target.ExpiresAt = now.Add(time.Minute).Format(time.RFC3339Nano)
	if got := targetUnavailableReason(target, now); got != "" {
		t.Fatalf("valid target reason = %q, want empty", got)
	}
}

func TestNewSSHAuditSessionKeepsTargetAndAccountLabelsSeparate(t *testing.T) {
	user := model.User{ID: "user-1", Username: "operator"}
	target := store.TargetConfig{
		ID: "account-1", Name: "operations", HostName: "application-host",
		Host: "47.113.206.31", Port: 22, Username: "arcuchi", HostID: "host-1",
	}
	session := model.NewSession(user, target.ID, target.Addr(), "127.0.0.1")
	audit := newSSHAuditSession(user, target, session, "replays")

	if audit.TargetAddress != "47.113.206.31:22" || audit.TargetName != "application-host" {
		t.Fatalf("audit target = address:%q name:%q", audit.TargetAddress, audit.TargetName)
	}
	if audit.AccountUsername != "arcuchi" || audit.AccountName != "operations" {
		t.Fatalf("audit account = username:%q name:%q", audit.AccountUsername, audit.AccountName)
	}
	if audit.ResourceType != model.ResourceTypeHostAccount ||
		audit.ResourceID != "account-1" ||
		audit.HostID != "host-1" ||
		audit.AccountID != "account-1" {
		t.Fatalf(
			"audit resource = type:%q resource:%q host:%q account:%q",
			audit.ResourceType,
			audit.ResourceID,
			audit.HostID,
			audit.AccountID,
		)
	}
	if audit.Outcome != model.AuditOutcomeActive {
		t.Fatalf("audit outcome = %q, want active", audit.Outcome)
	}
	wantReplayDir := filepath.Join("replays", "ssh", session.ID)
	if audit.ReplayDir != wantReplayDir {
		t.Fatalf("replay dir = %q, want %q", audit.ReplayDir, wantReplayDir)
	}
}
