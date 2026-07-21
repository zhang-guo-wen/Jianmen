package webrdp

import (
	"context"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func TestWebRDPSessionContextUsesAccountExpiry(t *testing.T) {
	accountExpiry := time.Now().UTC().Add(time.Hour)
	ctx, cancel, reason := webRDPSessionContext(
		context.Background(),
		service.WebRDPConnection{
			Target: service.WebRDPTarget{
				ExpiresAt: &accountExpiry,
			},
		},
	)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(accountExpiry) {
		t.Fatalf("session deadline = %v, %v; want %v", deadline, ok, accountExpiry)
	}
	if reason != "account_expired" {
		t.Fatalf("deadline reason = %q, want account_expired", reason)
	}
}

func TestRelayOutcomeRecordsExpiredAccount(t *testing.T) {
	expired := time.Now().UTC().Add(-time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), expired)
	defer cancel()
	<-ctx.Done()

	outcome, code, message := relayOutcomeWithDeadline(
		ctx.Err(),
		ctx,
		"account_expired",
	)
	if outcome != model.AuditOutcomeTerminated ||
		code != "account_expired" ||
		message != "" {
		t.Fatalf(
			"relay outcome = (%q, %q, %q), want account expiry",
			outcome,
			code,
			message,
		)
	}
}
