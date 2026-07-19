package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

var (
	ErrWebRDPUnavailable      = errors.New("web RDP is unavailable")
	ErrWebRDPNotAuthorized    = errors.New("web RDP is not authorized")
	ErrWebRDPApprovalRequired = errors.New("web RDP approval is required")
)

// WebRDPTarget contains the server-owned RDP connection settings for one host
// account. Password must never be serialized or logged.
type WebRDPTarget struct {
	ID                     string
	HostID                 string
	HostName               string
	Protocol               string
	Address                string
	Port                   int
	Username               string
	Domain                 string
	Password               string
	Security               string
	IgnoreCertificate      bool
	CertificateFingerprint string
	ApprovalRequired       bool
	ClipboardRead          bool
	ClipboardWrite         bool
	FileUpload             bool
	FileDownload           bool
	DriveMapping           bool
	Disabled               bool
	ExpiresAt              *time.Time
}

type WebRDPTargetRepository interface {
	WebRDPTarget(ctx context.Context, targetID string) (WebRDPTarget, error)
}

type WebRDPAuthorizer interface {
	AuthorizeConnection(
		ctx context.Context,
		userID string,
		actions []string,
		resourceType string,
		resourceID string,
	) (bool, error)
}

type WebRDPChannelPolicy struct {
	ClipboardRead  bool `json:"clipboard_read"`
	ClipboardWrite bool `json:"clipboard_write"`
	FileUpload     bool `json:"file_upload"`
	FileDownload   bool `json:"file_download"`
	DriveMapping   bool `json:"drive_mapping"`
}

type WebRDPPlan struct {
	TargetID        string              `json:"target_id"`
	TargetName      string              `json:"target_name"`
	EffectivePolicy WebRDPChannelPolicy `json:"effective_policy"`
	RequiredActions []string            `json:"required_actions"`
	AccessRequestID string              `json:"access_request_id,omitempty"`
	AccessExpiresAt *time.Time          `json:"access_expires_at,omitempty"`
}

type WebRDPConnection struct {
	Plan   WebRDPPlan
	Target WebRDPTarget
}

type WebRDPService struct {
	targets    WebRDPTargetRepository
	authorizer WebRDPAuthorizer
	approvals  *AccessRequestService
	now        func() time.Time
}

func NewWebRDPService(
	targets WebRDPTargetRepository,
	authorizer WebRDPAuthorizer,
	approvals *AccessRequestService,
) (*WebRDPService, error) {
	if targets == nil {
		return nil, errors.New("web RDP target repository is required")
	}
	if authorizer == nil {
		return nil, errors.New("web RDP authorizer is required")
	}
	if approvals == nil {
		return nil, errors.New("web RDP approval service is required")
	}
	return &WebRDPService{
		targets: targets, authorizer: authorizer, approvals: approvals,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

// Plan re-evaluates account state, RBAC and every optional channel. It does
// not grant access and deliberately leaves the approval gate for Authorize.
func (s *WebRDPService) Plan(ctx context.Context, userID, targetID string) (WebRDPPlan, error) {
	target, plan, err := s.evaluate(ctx, userID, targetID)
	if err != nil {
		return WebRDPPlan{}, err
	}
	_ = target
	return plan, nil
}

// Authorize is called both before ticket issuance and after single-use ticket
// consumption. This prevents a ticket from preserving stale RBAC or policy.
func (s *WebRDPService) Authorize(
	ctx context.Context,
	userID string,
	targetID string,
) (WebRDPConnection, error) {
	target, plan, err := s.evaluate(ctx, userID, targetID)
	if err != nil {
		return WebRDPConnection{}, err
	}
	if target.ApprovalRequired {
		approval, found, approvalErr := s.approvals.ActiveApproval(
			ctx,
			strings.TrimSpace(userID),
			model.ResourceTypeHostAccount,
			target.ID,
			"rdp",
			plan.RequiredActions,
		)
		if approvalErr != nil {
			return WebRDPConnection{}, fmt.Errorf("check RDP approval: %w", approvalErr)
		}
		if !found {
			return WebRDPConnection{}, ErrWebRDPApprovalRequired
		}
		plan.AccessRequestID = approval.ID
		if approval.AccessExpiresAt != nil {
			expiresAt := approval.AccessExpiresAt.UTC()
			plan.AccessExpiresAt = &expiresAt
		}
	}
	return WebRDPConnection{Plan: plan, Target: target}, nil
}

func (s *WebRDPService) evaluate(
	ctx context.Context,
	userID string,
	targetID string,
) (WebRDPTarget, WebRDPPlan, error) {
	if err := ctx.Err(); err != nil {
		return WebRDPTarget{}, WebRDPPlan{}, err
	}
	userID = strings.TrimSpace(userID)
	targetID = strings.TrimSpace(targetID)
	if userID == "" || targetID == "" {
		return WebRDPTarget{}, WebRDPPlan{}, ErrWebRDPNotAuthorized
	}
	target, err := s.targets.WebRDPTarget(ctx, targetID)
	if err != nil {
		return WebRDPTarget{}, WebRDPPlan{}, fmt.Errorf("%w: %v", ErrWebRDPUnavailable, err)
	}
	if strings.TrimSpace(target.ID) != targetID ||
		!strings.EqualFold(strings.TrimSpace(target.Protocol), "rdp") ||
		target.Disabled ||
		(target.ExpiresAt != nil && !s.now().UTC().Before(target.ExpiresAt.UTC())) {
		return WebRDPTarget{}, WebRDPPlan{}, ErrWebRDPUnavailable
	}
	if strings.TrimSpace(target.Address) == "" || target.Port < 1 ||
		strings.TrimSpace(target.Username) == "" || target.Password == "" {
		return WebRDPTarget{}, WebRDPPlan{}, ErrWebRDPUnavailable
	}
	allowed, err := s.allowed(ctx, userID, target.ID, rbac.ActionRDPConnect)
	if err != nil {
		return WebRDPTarget{}, WebRDPPlan{}, err
	}
	if !allowed {
		return WebRDPTarget{}, WebRDPPlan{}, ErrWebRDPNotAuthorized
	}

	policy := WebRDPChannelPolicy{}
	required := []string{rbac.ActionRDPConnect}
	checkChannel := func(configured bool, action string, enable func()) error {
		if !configured {
			return nil
		}
		channelAllowed, channelErr := s.allowed(ctx, userID, target.ID, action)
		if channelErr != nil {
			return channelErr
		}
		if channelAllowed {
			enable()
			required = append(required, action)
		}
		return nil
	}
	checks := []struct {
		configured bool
		action     string
		enable     func()
	}{
		{target.ClipboardRead, rbac.ActionRDPClipboardRead, func() { policy.ClipboardRead = true }},
		{target.ClipboardWrite, rbac.ActionRDPClipboardWrite, func() { policy.ClipboardWrite = true }},
		{target.DriveMapping, rbac.ActionRDPDriveMap, func() { policy.DriveMapping = true }},
	}
	for _, check := range checks {
		if err := checkChannel(check.configured, check.action, check.enable); err != nil {
			return WebRDPTarget{}, WebRDPPlan{}, err
		}
	}
	// Guacamole implements RDP file transfer through drive redirection.
	// Upload/download therefore remain independently authorized, but neither
	// can become effective unless the drive channel is also effective.
	fileChecks := []struct {
		configured bool
		action     string
		enable     func()
	}{
		{target.FileUpload, rbac.ActionRDPFileUpload, func() { policy.FileUpload = true }},
		{target.FileDownload, rbac.ActionRDPFileDownload, func() { policy.FileDownload = true }},
	}
	if policy.DriveMapping {
		for _, check := range fileChecks {
			if err := checkChannel(check.configured, check.action, check.enable); err != nil {
				return WebRDPTarget{}, WebRDPPlan{}, err
			}
		}
	}
	name := strings.TrimSpace(target.HostName)
	if name == "" {
		name = target.Address
	}
	return target, WebRDPPlan{
		TargetID: target.ID, TargetName: name,
		EffectivePolicy: policy, RequiredActions: required,
	}, nil
}

func (s *WebRDPService) allowed(
	ctx context.Context,
	userID string,
	targetID string,
	action string,
) (bool, error) {
	allowed, err := s.authorizer.AuthorizeConnection(
		ctx,
		userID,
		[]string{action},
		model.ResourceTypeHostAccount,
		targetID,
	)
	if err != nil {
		return false, fmt.Errorf("authorize RDP action %q: %w", action, err)
	}
	return allowed, nil
}
