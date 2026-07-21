package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

type webRDPTargetRepositoryStub struct {
	target WebRDPTarget
	err    error
	calls  int
}

func (r *webRDPTargetRepositoryStub) WebRDPTarget(
	_ context.Context,
	_ string,
) (WebRDPTarget, error) {
	r.calls++
	return r.target, r.err
}

type webRDPAuthorizationCall struct {
	userID       string
	action       string
	resourceType string
	resourceID   string
}

type webRDPAuthorizerStub struct {
	allowed map[string]bool
	errs    map[string]error
	calls   []webRDPAuthorizationCall
}

func (a *webRDPAuthorizerStub) AuthorizeConnection(
	_ context.Context,
	userID string,
	actions []string,
	resourceType string,
	resourceID string,
) (bool, error) {
	action := ""
	if len(actions) == 1 {
		action = actions[0]
	}
	a.calls = append(a.calls, webRDPAuthorizationCall{
		userID: userID, action: action,
		resourceType: resourceType, resourceID: resourceID,
	})
	if err := a.errs[action]; err != nil {
		return false, err
	}
	return a.allowed[action], nil
}

func (a *webRDPAuthorizerStub) count(action string) int {
	count := 0
	for _, call := range a.calls {
		if call.action == action {
			count++
		}
	}
	return count
}

func baseWebRDPTarget() WebRDPTarget {
	return WebRDPTarget{
		ID: "account-1", HostID: "host-1", HostName: "windows-prod",
		Protocol: "rdp", Address: "10.0.0.8", Port: 3389,
		Username: "Administrator", Password: "secret",
	}
}

func newWebRDPServiceForTest(
	t *testing.T,
	target WebRDPTarget,
	authorizer *webRDPAuthorizerStub,
) *WebRDPService {
	t.Helper()
	service, err := NewWebRDPService(
		&webRDPTargetRepositoryStub{target: target},
		authorizer,
	)
	if err != nil {
		t.Fatalf("NewWebRDPService() error = %v", err)
	}
	return service
}

func TestWebRDPServiceRejectsInvalidTargetState(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	expired := now
	tests := []struct {
		name   string
		mutate func(*WebRDPTarget)
	}{
		{"empty protocol", func(target *WebRDPTarget) { target.Protocol = "" }},
		{"wrong protocol", func(target *WebRDPTarget) { target.Protocol = "ssh" }},
		{"disabled account or host", func(target *WebRDPTarget) { target.Disabled = true }},
		{"expired", func(target *WebRDPTarget) { target.ExpiresAt = &expired }},
		{"missing id", func(target *WebRDPTarget) { target.ID = "" }},
		{"substituted id", func(target *WebRDPTarget) { target.ID = "account-2" }},
		{"missing address", func(target *WebRDPTarget) { target.Address = " " }},
		{"invalid port", func(target *WebRDPTarget) { target.Port = 0 }},
		{"missing username", func(target *WebRDPTarget) { target.Username = " " }},
		{"missing password", func(target *WebRDPTarget) { target.Password = "" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := baseWebRDPTarget()
			test.mutate(&target)
			authorizer := &webRDPAuthorizerStub{
				allowed: map[string]bool{rbac.ActionRDPConnect: true},
				errs:    map[string]error{},
			}
			service := newWebRDPServiceForTest(t, target, authorizer)
			service.now = func() time.Time { return now }

			_, err := service.Plan(context.Background(), "user-1", "account-1")
			if !errors.Is(err, ErrWebRDPUnavailable) {
				t.Fatalf("Plan() error = %v, want ErrWebRDPUnavailable", err)
			}
			if len(authorizer.calls) != 0 {
				t.Fatalf("invalid target reached RBAC: calls = %#v", authorizer.calls)
			}
		})
	}
}

func TestWebRDPServiceRequiresConnectPermission(t *testing.T) {
	target := baseWebRDPTarget()
	target.ClipboardRead = true
	authorizer := &webRDPAuthorizerStub{
		allowed: map[string]bool{
			rbac.ActionRDPConnect:       false,
			rbac.ActionRDPClipboardRead: true,
		},
		errs: map[string]error{},
	}
	service := newWebRDPServiceForTest(t, target, authorizer)

	_, err := service.Plan(context.Background(), "user-1", target.ID)
	if !errors.Is(err, ErrWebRDPNotAuthorized) {
		t.Fatalf("Plan() error = %v, want ErrWebRDPNotAuthorized", err)
	}
	if len(authorizer.calls) != 1 ||
		authorizer.calls[0].action != rbac.ActionRDPConnect {
		t.Fatalf("authorization calls = %#v, want connect only", authorizer.calls)
	}
	call := authorizer.calls[0]
	if call.resourceType != model.ResourceTypeHostAccount ||
		call.resourceID != target.ID || call.userID != "user-1" {
		t.Fatalf("connect authorization scope = %#v", call)
	}
}

func TestWebRDPServiceChannelPolicyIsAccountSwitchIntersectedWithRBAC(t *testing.T) {
	tests := []struct {
		name          string
		action        string
		configure     func(*WebRDPTarget, bool)
		enabled       func(WebRDPChannelPolicy) bool
		requiresDrive bool
	}{
		{
			"clipboard read", rbac.ActionRDPClipboardRead,
			func(target *WebRDPTarget, value bool) { target.ClipboardRead = value },
			func(policy WebRDPChannelPolicy) bool { return policy.ClipboardRead },
			false,
		},
		{
			"clipboard write", rbac.ActionRDPClipboardWrite,
			func(target *WebRDPTarget, value bool) { target.ClipboardWrite = value },
			func(policy WebRDPChannelPolicy) bool { return policy.ClipboardWrite },
			false,
		},
		{
			"file upload", rbac.ActionRDPFileUpload,
			func(target *WebRDPTarget, value bool) { target.FileUpload = value },
			func(policy WebRDPChannelPolicy) bool { return policy.FileUpload },
			true,
		},
		{
			"file download", rbac.ActionRDPFileDownload,
			func(target *WebRDPTarget, value bool) { target.FileDownload = value },
			func(policy WebRDPChannelPolicy) bool { return policy.FileDownload },
			true,
		},
		{
			"drive mapping", rbac.ActionRDPDriveMap,
			func(target *WebRDPTarget, value bool) { target.DriveMapping = value },
			func(policy WebRDPChannelPolicy) bool { return policy.DriveMapping },
			false,
		},
	}

	for _, test := range tests {
		for _, accountEnabled := range []bool{false, true} {
			for _, rbacAllowed := range []bool{false, true} {
				name := strings.Join([]string{
					test.name,
					"account=" + boolName(accountEnabled),
					"rbac=" + boolName(rbacAllowed),
				}, "/")
				t.Run(name, func(t *testing.T) {
					target := baseWebRDPTarget()
					test.configure(&target, accountEnabled)
					if test.requiresDrive {
						target.DriveMapping = true
					}
					authorizer := &webRDPAuthorizerStub{
						allowed: map[string]bool{
							rbac.ActionRDPConnect: true,
							test.action:           rbacAllowed,
						},
						errs: map[string]error{},
					}
					if test.requiresDrive {
						authorizer.allowed[rbac.ActionRDPDriveMap] = true
					}
					service := newWebRDPServiceForTest(t, target, authorizer)

					plan, err := service.Plan(context.Background(), "user-1", target.ID)
					if err != nil {
						t.Fatalf("Plan() error = %v", err)
					}
					want := accountEnabled && rbacAllowed
					if got := test.enabled(plan.EffectivePolicy); got != want {
						t.Fatalf("effective channel = %t, want %t", got, want)
					}
					wantCalls := 0
					if accountEnabled {
						wantCalls = 1
					}
					if got := authorizer.count(test.action); got != wantCalls {
						t.Fatalf("%s authorization calls = %d, want %d", test.action, got, wantCalls)
					}
					if got := slices.Contains(plan.RequiredActions, test.action); got != want {
						t.Fatalf("RequiredActions contains %s = %t, want %t", test.action, got, want)
					}
				})
			}
		}
	}
}

func TestWebRDPServiceFileTransferAlsoRequiresDriveAuthorization(t *testing.T) {
	target := baseWebRDPTarget()
	target.DriveMapping = true
	target.FileUpload = true
	target.FileDownload = true
	authorizer := &webRDPAuthorizerStub{
		allowed: map[string]bool{
			rbac.ActionRDPConnect:      true,
			rbac.ActionRDPDriveMap:     false,
			rbac.ActionRDPFileUpload:   true,
			rbac.ActionRDPFileDownload: true,
		},
		errs: map[string]error{},
	}
	service := newWebRDPServiceForTest(t, target, authorizer)

	plan, err := service.Plan(context.Background(), "user-1", target.ID)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.EffectivePolicy.DriveMapping ||
		plan.EffectivePolicy.FileUpload ||
		plan.EffectivePolicy.FileDownload {
		t.Fatalf("file policy bypassed drive authorization: %#v", plan.EffectivePolicy)
	}
	if authorizer.count(rbac.ActionRDPFileUpload) != 0 ||
		authorizer.count(rbac.ActionRDPFileDownload) != 0 {
		t.Fatalf("file actions were checked despite denied drive: %#v", authorizer.calls)
	}
}

func TestWebRDPServiceAuthorizationErrorsFailClosed(t *testing.T) {
	channelConfig := map[string]func(*WebRDPTarget){
		rbac.ActionRDPClipboardRead:  func(target *WebRDPTarget) { target.ClipboardRead = true },
		rbac.ActionRDPClipboardWrite: func(target *WebRDPTarget) { target.ClipboardWrite = true },
		rbac.ActionRDPFileUpload:     func(target *WebRDPTarget) { target.FileUpload = true },
		rbac.ActionRDPFileDownload:   func(target *WebRDPTarget) { target.FileDownload = true },
		rbac.ActionRDPDriveMap:       func(target *WebRDPTarget) { target.DriveMapping = true },
	}
	actions := []string{
		rbac.ActionRDPConnect,
		rbac.ActionRDPClipboardRead,
		rbac.ActionRDPClipboardWrite,
		rbac.ActionRDPFileUpload,
		rbac.ActionRDPFileDownload,
		rbac.ActionRDPDriveMap,
	}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			target := baseWebRDPTarget()
			if configure := channelConfig[action]; configure != nil {
				configure(&target)
			}
			if action == rbac.ActionRDPFileUpload || action == rbac.ActionRDPFileDownload {
				target.DriveMapping = true
			}
			sentinel := errors.New("authorizer unavailable")
			authorizer := &webRDPAuthorizerStub{
				allowed: map[string]bool{rbac.ActionRDPConnect: true},
				errs:    map[string]error{action: sentinel},
			}
			if action == rbac.ActionRDPFileUpload || action == rbac.ActionRDPFileDownload {
				authorizer.allowed[rbac.ActionRDPDriveMap] = true
			}
			service := newWebRDPServiceForTest(t, target, authorizer)

			plan, err := service.Plan(context.Background(), "user-1", target.ID)
			if !errors.Is(err, sentinel) {
				t.Fatalf("Plan() error = %v, want wrapped sentinel", err)
			}
			if plan.TargetID != "" {
				t.Fatalf("Plan() returned partial authorization: %#v", plan)
			}
		})
	}
}

func boolName(value bool) string {
	if value {
		return "on"
	}
	return "off"
}
