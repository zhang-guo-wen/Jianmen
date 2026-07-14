package sshserver

import (
	"testing"
	"time"

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
