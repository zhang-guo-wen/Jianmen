package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func TestWebRDPServiceApprovalIsAdditionalAndRechecked(t *testing.T) {
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	startsAt := now.Add(-time.Minute)
	expiresAt := now.Add(time.Hour)
	actionsJSON, err := json.Marshal([]string{
		rbac.ActionRDPConnect,
		rbac.ActionRDPClipboardRead,
	})
	if err != nil {
		t.Fatal(err)
	}
	approvalRepository := &webRDPApprovalRepositoryStub{
		found: true,
		request: model.AccessRequest{
			ID: "approval-1", RequesterID: "user-1",
			ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-1",
			Protocol: "rdp", Status: model.AccessRequestApproved,
			ActionsJSON:    string(actionsJSON),
			AccessStartsAt: &startsAt, AccessExpiresAt: &expiresAt,
		},
	}
	target := baseWebRDPTarget()
	target.ApprovalRequired = true
	target.ClipboardRead = true
	authorizer := &webRDPAuthorizerStub{
		allowed: map[string]bool{
			rbac.ActionRDPConnect:       true,
			rbac.ActionRDPClipboardRead: true,
		},
		errs: map[string]error{},
	}
	service := newWebRDPServiceForTest(t, target, authorizer, approvalRepository)
	service.now = func() time.Time { return now }
	service.approvals.now = func() time.Time { return now }

	first, err := service.Authorize(context.Background(), "user-1", target.ID)
	if err != nil {
		t.Fatalf("first Authorize() error = %v", err)
	}
	if first.Plan.AccessRequestID != "approval-1" {
		t.Fatalf("AccessRequestID = %q", first.Plan.AccessRequestID)
	}
	if first.Plan.AccessExpiresAt == nil ||
		!first.Plan.AccessExpiresAt.Equal(expiresAt) {
		t.Fatalf(
			"AccessExpiresAt = %v, want %v",
			first.Plan.AccessExpiresAt,
			expiresAt,
		)
	}

	// This is the same check used after a single-use ticket is consumed. A
	// revoked/expired approval must not remain valid just because ticket
	// issuance succeeded.
	approvalRepository.found = false
	_, err = service.Authorize(context.Background(), "user-1", target.ID)
	if !errors.Is(err, ErrWebRDPApprovalRequired) {
		t.Fatalf("second Authorize() error = %v, want approval required", err)
	}
	if approvalRepository.calls != 2 {
		t.Fatalf("approval checks = %d, want 2", approvalRepository.calls)
	}
	if authorizer.count(rbac.ActionRDPConnect) != 2 ||
		authorizer.count(rbac.ActionRDPClipboardRead) != 2 {
		t.Fatalf("RBAC was not rechecked: %#v", authorizer.calls)
	}
}

func TestWebRDPServiceApprovalNeverReplacesRBAC(t *testing.T) {
	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)
	approvalRepository := &webRDPApprovalRepositoryStub{
		found: true,
		request: model.AccessRequest{
			ID: "approval-1", RequesterID: "user-1",
			ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-1",
			Protocol: "rdp", Status: model.AccessRequestApproved,
			ActionsJSON: `["rdp:connect"]`, AccessExpiresAt: &expiresAt,
		},
	}
	target := baseWebRDPTarget()
	target.ApprovalRequired = true
	authorizer := &webRDPAuthorizerStub{
		allowed: map[string]bool{rbac.ActionRDPConnect: false},
		errs:    map[string]error{},
	}
	service := newWebRDPServiceForTest(t, target, authorizer, approvalRepository)

	_, err := service.Authorize(context.Background(), "user-1", target.ID)
	if !errors.Is(err, ErrWebRDPNotAuthorized) {
		t.Fatalf("Authorize() error = %v, want ErrWebRDPNotAuthorized", err)
	}
	if approvalRepository.calls != 0 {
		t.Fatalf("approval was consulted before RBAC: calls = %d", approvalRepository.calls)
	}
}

func TestWebRDPServiceApprovalLookupErrorFailsClosed(t *testing.T) {
	target := baseWebRDPTarget()
	target.ApprovalRequired = true
	sentinel := errors.New("approval database unavailable")
	approvalRepository := &webRDPApprovalRepositoryStub{found: true, err: sentinel}
	authorizer := &webRDPAuthorizerStub{
		allowed: map[string]bool{rbac.ActionRDPConnect: true},
		errs:    map[string]error{},
	}
	service := newWebRDPServiceForTest(t, target, authorizer, approvalRepository)

	_, err := service.Authorize(context.Background(), "user-1", target.ID)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Authorize() error = %v, want wrapped sentinel", err)
	}
}
