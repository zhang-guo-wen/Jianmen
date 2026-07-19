package webrdp

import (
	"context"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func TestWebRDPSessionContextUsesEarliestAccessDeadline(t *testing.T) {
	now := time.Now().UTC()
	approvalExpiry := now.Add(2 * time.Hour)
	accountExpiry := now.Add(time.Hour)
	ctx, cancel, reason := webRDPSessionContext(
		context.Background(),
		service.WebRDPConnection{
			Plan: service.WebRDPPlan{AccessExpiresAt: &approvalExpiry},
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

func TestWebRDPSessionContextUsesApprovalExpiry(t *testing.T) {
	approvalExpiry := time.Now().UTC().Add(time.Hour)
	ctx, cancel, reason := webRDPSessionContext(
		context.Background(),
		service.WebRDPConnection{
			Plan: service.WebRDPPlan{AccessExpiresAt: &approvalExpiry},
		},
	)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(approvalExpiry) {
		t.Fatalf("session deadline = %v, %v; want %v", deadline, ok, approvalExpiry)
	}
	if reason != "approval_expired" {
		t.Fatalf("deadline reason = %q, want approval_expired", reason)
	}
}

func TestRelayOutcomeRecordsExpiredAccessWindow(t *testing.T) {
	expired := time.Now().UTC().Add(-time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), expired)
	defer cancel()
	<-ctx.Done()

	outcome, code, message := relayOutcomeWithDeadline(
		ctx.Err(),
		ctx,
		"approval_expired",
	)
	if outcome != model.AuditOutcomeTerminated ||
		code != "approval_expired" ||
		message != "" {
		t.Fatalf(
			"relay outcome = (%q, %q, %q), want approval expiry",
			outcome,
			code,
			message,
		)
	}
}
