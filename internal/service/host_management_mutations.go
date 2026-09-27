package service

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func (s *HostManagementService) CreateHost(ctx context.Context, actor HostManagementActor, host HostManagementHostRecord) (HostManagementHostView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionHostCreate}, "", ""); err != nil {
		return HostManagementHostView{}, err
	}
	host = normalizedManagementHostRecord(host)
	if host.Protocol == "ssh" {
		identity, err := s.identityCollector.Collect(ctx, host.Address, host.Port)
		if err == nil {
			err = validateHostIdentity(identity)
		}
		if err != nil {
			clearHostIdentity(&host)
			host.Status = "disabled"
		} else {
			applyHostIdentity(&host, identity)
		}
	} else {
		clearHostIdentity(&host)
	}
	creatorID := actor.ID
	if actor.SuperAdmin {
		creatorID = ""
	}
	view, err := s.repository.CreateManagedHost(ctx, host, creatorID)
	if err != nil {
		return HostManagementHostView{}, fmt.Errorf("create host: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) UpdateHost(ctx context.Context, actor HostManagementActor, hostID string, host HostManagementHostRecord) (HostManagementHostView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionHostUpdate}, model.ResourceTypeHost, hostID); err != nil {
		return HostManagementHostView{}, err
	}
	current, err := s.repository.Host(ctx, hostID)
	if err != nil {
		return HostManagementHostView{}, fmt.Errorf("load host before update: %w", err)
	}
	if strings.TrimSpace(host.Status) == "" {
		host.Status = current.Status
	}
	host = normalizedManagementHostRecord(host)
	endpointChanged := hostEndpointChanged(current, host)
	if host.Protocol != "ssh" {
		clearHostIdentity(&host)
	} else if host.Status == "active" &&
		(strings.EqualFold(strings.TrimSpace(current.Status), "disabled") || endpointChanged) {
		identity, collectErr := s.identityCollector.Collect(ctx, host.Address, host.Port)
		if collectErr == nil {
			collectErr = validateHostIdentity(identity)
		}
		if collectErr != nil {
			return HostManagementHostView{}, &HostIdentityRefreshError{
				HostID: hostID, HostStatus: current.Status,
				IdentityStatus: current.IdentityStatus, Cause: collectErr,
			}
		}
		applyHostIdentity(&host, identity)
	} else if endpointChanged {
		clearHostIdentity(&host)
	} else {
		host.HostKeyFingerprint = current.HostKeyFingerprint
		host.KnownHosts = current.KnownHosts
	}
	view, err := s.repository.UpdateHost(ctx, hostID, host)
	if err != nil {
		return HostManagementHostView{}, fmt.Errorf("update host: %w", err)
	}
	return view, nil
}

// ConfirmHostIdentity refreshes a host's SSH verification material after an
// operator has explicitly accepted the identity warning. Refreshing and
// enabling are persisted by one repository operation bound to the endpoint
// that was scanned.
func (s *HostManagementService) ConfirmHostIdentity(
	ctx context.Context,
	actor HostManagementActor,
	hostID string,
	confirmed bool,
	expectedFingerprint string,
) (HostIdentityRefreshResult, error) {
	hostID = strings.TrimSpace(hostID)
	if err := s.require(ctx, actor, []string{rbac.ActionHostUpdate}, model.ResourceTypeHost, hostID); err != nil {
		return HostIdentityRefreshResult{}, err
	}
	if !confirmed {
		return HostIdentityRefreshResult{}, ErrHostIdentityConfirmationRequired
	}
	if expectedFingerprint == "" || expectedFingerprint != strings.TrimSpace(expectedFingerprint) ||
		len(expectedFingerprint) > 256 ||
		strings.IndexFunc(expectedFingerprint, func(r rune) bool {
			return unicode.IsControl(r) || unicode.IsSpace(r)
		}) >= 0 {
		return HostIdentityRefreshResult{}, fmt.Errorf("%w: expected_fingerprint is invalid", ErrHostTargetInvalidInput)
	}
	current, err := s.repository.Host(ctx, hostID)
	if err != nil {
		return HostIdentityRefreshResult{}, fmt.Errorf("load host before identity refresh: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(current.Protocol), "ssh") ||
		strings.TrimSpace(current.Address) == "" || current.Port <= 0 {
		return HostIdentityRefreshResult{}, fmt.Errorf("%w: ssh host endpoint is required", ErrHostTargetInvalidInput)
	}
	persistedStatus := current.PersistedStatus
	if persistedStatus == "" {
		persistedStatus = current.Status
	}
	persistedFingerprint := current.PersistedFingerprint
	if persistedFingerprint == "" {
		persistedFingerprint = current.HostKeyFingerprint
	}
	persistedKnownHosts := current.PersistedKnownHosts
	if persistedKnownHosts == "" {
		persistedKnownHosts = current.KnownHosts
	}
	oldFingerprint := strings.TrimSpace(persistedFingerprint)
	identity, err := s.identityCollector.Collect(ctx, current.Address, current.Port)
	if err == nil {
		err = validateHostIdentity(identity)
	}
	if err != nil {
		return HostIdentityRefreshResult{}, &HostIdentityRefreshError{
			HostID: hostID, HostStatus: current.Status,
			IdentityStatus: current.IdentityStatus,
			OldFingerprint: oldFingerprint, ExpectedFingerprint: expectedFingerprint,
			Cause: err,
		}
	}
	actualFingerprint := strings.TrimSpace(identity.Fingerprint)
	if actualFingerprint != expectedFingerprint {
		return HostIdentityRefreshResult{}, &HostIdentityConfirmationMismatchError{
			HostID: hostID, OldFingerprint: oldFingerprint,
			ExpectedFingerprint: expectedFingerprint, ActualFingerprint: actualFingerprint,
			HostDisabled: strings.EqualFold(strings.TrimSpace(current.Status), "disabled"),
		}
	}
	view, err := s.repository.RefreshHostIdentity(ctx, hostID, HostManagementIdentityRefresh{
		Address: current.Address, Port: current.Port, Protocol: current.Protocol, Status: persistedStatus,
		PreviousFingerprint: persistedFingerprint, PreviousKnownHosts: persistedKnownHosts,
		UpdatedAt:   current.Revision,
		Fingerprint: identity.Fingerprint, KnownHosts: identity.KnownHosts,
	})
	if err != nil {
		return HostIdentityRefreshResult{}, &HostIdentityRefreshError{
			HostID: hostID, HostStatus: current.Status,
			IdentityStatus: current.IdentityStatus,
			OldFingerprint: oldFingerprint, ExpectedFingerprint: expectedFingerprint,
			ActualFingerprint: actualFingerprint, Cause: err,
		}
	}
	return HostIdentityRefreshResult{
		Host: view, OldFingerprint: oldFingerprint,
		ExpectedFingerprint: expectedFingerprint, ActualFingerprint: actualFingerprint,
	}, nil
}

func (s *HostManagementService) DeleteHost(ctx context.Context, actor HostManagementActor, hostID string) error {
	if err := s.require(ctx, actor, []string{rbac.ActionHostDelete}, model.ResourceTypeHost, hostID); err != nil {
		return err
	}
	if err := s.repository.DeleteHost(ctx, hostID); err != nil {
		return fmt.Errorf("delete host: %w", err)
	}
	return nil
}

func (s *HostManagementService) Target(ctx context.Context, actor HostManagementActor, targetID string) (HostManagementTargetView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionTargetView}, model.ResourceTypeHostAccount, targetID); err != nil {
		return HostManagementTargetView{}, err
	}
	view, err := s.repository.Target(ctx, targetID)
	if err != nil {
		return HostManagementTargetView{}, fmt.Errorf("get host account: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) CreateTarget(ctx context.Context, actor HostManagementActor, target config.Target) (HostManagementTargetView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, "", ""); err != nil {
		return HostManagementTargetView{}, err
	}
	if strings.TrimSpace(target.HostID) == "" {
		if !actor.SuperAdmin {
			return HostManagementTargetView{}, fmt.Errorf("%w: host_id is required", ErrHostTargetInvalidInput)
		}
	} else if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, model.ResourceTypeHost, target.HostID); err != nil {
		return HostManagementTargetView{}, err
	}
	view, err := s.repository.AddTarget(ctx, target)
	if err != nil {
		return HostManagementTargetView{}, fmt.Errorf("create host account: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) UpdateTarget(ctx context.Context, actor HostManagementActor, targetID string, target config.Target) (HostManagementTargetView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionTargetUpdate}, model.ResourceTypeHostAccount, targetID); err != nil {
		return HostManagementTargetView{}, err
	}
	view, err := s.repository.UpdateTarget(ctx, targetID, target)
	if err != nil {
		return HostManagementTargetView{}, fmt.Errorf("update host account: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) DeleteTarget(ctx context.Context, actor HostManagementActor, targetID string) error {
	if err := s.require(ctx, actor, []string{rbac.ActionTargetDelete}, model.ResourceTypeHostAccount, targetID); err != nil {
		return err
	}
	if err := s.repository.DeleteTarget(ctx, targetID); err != nil {
		return fmt.Errorf("delete host account: %w", err)
	}
	return nil
}
