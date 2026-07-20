package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func (s *HostManagementService) ResolveConnectionTest(ctx context.Context, actor HostManagementActor, input config.Target) (HostManagementTargetConfig, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.HostID = strings.TrimSpace(input.HostID)
	if input.ID == "" && input.HostID == "" && !actor.SuperAdmin {
		return HostManagementTargetConfig{}, ErrHostAccessDenied
	}

	var resolved HostManagementTargetConfig
	switch {
	case input.ID != "":
		actions := []string{rbac.ActionTargetCreate}
		if connectionTestUsesStoredAccountOnly(input) {
			target, err := s.repository.Target(ctx, input.ID)
			if err != nil {
				return HostManagementTargetConfig{}, fmt.Errorf("load host account for connection test authorization: %w", err)
			}
			actions = []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect, rbac.ActionTargetCreate}
			if strings.EqualFold(target.Protocol, "rdp") {
				actions = []string{rbac.ActionRDPConnect, rbac.ActionTargetCreate}
			}
		}
		if err := s.require(ctx, actor, actions, "", ""); err != nil {
			return HostManagementTargetConfig{}, err
		}
		if err := s.require(ctx, actor, actions, model.ResourceTypeHostAccount, input.ID); err != nil {
			return HostManagementTargetConfig{}, err
		}
		stored, err := s.repository.TargetConfig(ctx, input.ID)
		if err != nil {
			return HostManagementTargetConfig{}, fmt.Errorf("load host account credentials: %w", err)
		}
		if strings.TrimSpace(stored.HostID) == "" {
			return HostManagementTargetConfig{}, fmt.Errorf("%w: stored target has no parent host", ErrHostTargetUnavailable)
		}
		host, err := s.repository.Host(ctx, stored.HostID)
		if err != nil {
			return HostManagementTargetConfig{}, fmt.Errorf("load parent host for connection test: %w", err)
		}
		if err := validatePersistedEndpoint(input, host, stored.HostID); err != nil {
			return HostManagementTargetConfig{}, err
		}
		if strings.EqualFold(host.Protocol, "ssh") && strings.EqualFold(host.IdentityStatus, "unavailable") {
			return HostManagementTargetConfig{}, &HostIdentityUnavailableError{HostID: stored.HostID}
		}
		if stored.Disabled || stored.Expired(time.Now().UTC()) || strings.EqualFold(host.Status, "disabled") {
			return HostManagementTargetConfig{}, ErrHostTargetUnavailable
		}
		resolved = mergeStoredConnectionCredentials(stored, input)
		applyPersistedEndpoint(&resolved, host, stored.HostID)

	case input.HostID != "":
		if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, "", ""); err != nil {
			return HostManagementTargetConfig{}, err
		}
		if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, model.ResourceTypeHost, input.HostID); err != nil {
			return HostManagementTargetConfig{}, err
		}
		host, err := s.repository.Host(ctx, input.HostID)
		if err != nil {
			return HostManagementTargetConfig{}, fmt.Errorf("load host for connection test: %w", err)
		}
		if err := validatePersistedEndpoint(input, host, input.HostID); err != nil {
			return HostManagementTargetConfig{}, err
		}
		if strings.EqualFold(host.Protocol, "ssh") && strings.EqualFold(host.IdentityStatus, "unavailable") {
			return HostManagementTargetConfig{}, &HostIdentityUnavailableError{HostID: input.HostID}
		}
		if strings.EqualFold(host.Status, "disabled") {
			return HostManagementTargetConfig{}, ErrHostTargetUnavailable
		}
		resolved = targetConfigFromInput(input)
		resolved.Disabled = false
		resolved.ExpiresAt = ""
		applyPersistedEndpoint(&resolved, host, input.HostID)

	default:
		if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, "", ""); err != nil {
			return HostManagementTargetConfig{}, err
		}
		resolved = targetConfigFromInput(input)
	}

	if resolved.Addr() == "" || strings.TrimSpace(resolved.Username) == "" {
		return HostManagementTargetConfig{}, fmt.Errorf("%w: host, port, and username are required", ErrHostTargetInvalidInput)
	}
	return resolved, nil
}

func connectionTestUsesStoredAccountOnly(input config.Target) bool {
	if strings.TrimSpace(input.ID) == "" {
		return false
	}
	input.ID = ""
	return input == (config.Target{})
}

func validatePersistedEndpoint(input config.Target, host HostManagementHostView, expectedHostID string) error {
	if input.HostID != "" && input.HostID != expectedHostID {
		return fmt.Errorf("%w: target does not belong to requested host", ErrHostTargetInvalidInput)
	}
	if strings.TrimSpace(input.Host) != "" && strings.TrimSpace(input.Host) != strings.TrimSpace(host.Address) {
		return fmt.Errorf("%w: persisted host address cannot be overridden", ErrHostTargetInvalidInput)
	}
	if input.Port != 0 && input.Port != host.Port {
		return fmt.Errorf("%w: persisted host port cannot be overridden", ErrHostTargetInvalidInput)
	}
	if strings.TrimSpace(input.Protocol) != "" && !strings.EqualFold(input.Protocol, host.Protocol) {
		return fmt.Errorf("%w: persisted host protocol cannot be overridden", ErrHostTargetInvalidInput)
	}
	return nil
}

func applyPersistedEndpoint(target *HostManagementTargetConfig, host HostManagementHostView, hostID string) {
	target.HostID = hostID
	target.HostName = host.Name
	target.Host = host.Address
	target.Port = host.Port
	target.Protocol = host.Protocol
	target.InsecureIgnoreHostKey = false
	target.HostKeyFingerprint = host.HostKeyFingerprint
	target.KnownHosts = host.KnownHosts
	target.KnownHostsPath = ""
	target.HostKeyChangeHandler = host.HostKeyChangeHandler
}

func mergeStoredConnectionCredentials(stored HostManagementTargetConfig, input config.Target) HostManagementTargetConfig {
	if strings.TrimSpace(input.Username) != "" {
		stored.Username = input.Username
	}
	if strings.TrimSpace(input.Name) != "" {
		stored.Name = input.Name
	}
	if strings.TrimSpace(input.Domain) != "" {
		stored.Domain = input.Domain
	}
	switch {
	case input.Password != "":
		stored.Password = input.Password
		stored.PrivateKeyPath = ""
		stored.PrivateKeyPEM = ""
		stored.Passphrase = ""
	case input.PrivateKeyPEM != "" || input.PrivateKeyPath != "":
		stored.Password = ""
		stored.PrivateKeyPath = input.PrivateKeyPath
		stored.PrivateKeyPEM = input.PrivateKeyPEM
		stored.Passphrase = input.Passphrase
	case input.Passphrase != "":
		stored.Passphrase = input.Passphrase
	}
	if strings.TrimSpace(input.RDPSecurity) != "" {
		stored.RDPSecurity = input.RDPSecurity
	}
	if strings.TrimSpace(input.RDPCertFingerprints) != "" {
		stored.RDPCertFingerprints = input.RDPCertFingerprints
	}
	stored.RDPIgnoreCertificate = input.RDPIgnoreCertificate
	return stored
}

func (s *HostManagementService) ResolveWebTerminalTarget(ctx context.Context, actor HostManagementActor, targetID string) (HostManagementTargetConfig, error) {
	if strings.TrimSpace(targetID) != "" {
		if err := s.require(ctx, actor, []string{rbac.ActionSessionConnect}, model.ResourceTypeHostAccount, targetID); err != nil {
			return HostManagementTargetConfig{}, err
		}
		return s.loadConnectableTarget(ctx, targetID)
	}
	targets, err := s.repository.Targets(ctx)
	if err != nil {
		return HostManagementTargetConfig{}, fmt.Errorf("list default host accounts: %w", err)
	}
	for _, target := range targets {
		if !targetLifecycleConnectable(target, time.Now().UTC()) || strings.EqualFold(target.Protocol, "rdp") {
			continue
		}
		allowed, err := s.authorize(ctx, actor, []string{rbac.ActionSessionConnect}, model.ResourceTypeHostAccount, target.ID)
		if err != nil {
			return HostManagementTargetConfig{}, err
		}
		if !allowed {
			continue
		}
		return s.loadConnectableTarget(ctx, target.ID)
	}
	return HostManagementTargetConfig{}, ErrHostTargetUnavailable
}

func (s *HostManagementService) loadConnectableTarget(ctx context.Context, targetID string) (HostManagementTargetConfig, error) {
	target, err := s.repository.TargetConfig(ctx, targetID)
	if err != nil {
		return HostManagementTargetConfig{}, fmt.Errorf("load host account credentials: %w", err)
	}
	if target.Disabled || target.Expired(time.Now().UTC()) || !strings.EqualFold(target.Protocol, "ssh") {
		return HostManagementTargetConfig{}, ErrHostTargetUnavailable
	}
	return target, nil
}

func targetConfigFromInput(target config.Target) HostManagementTargetConfig {
	return HostManagementTargetConfig{ID: target.ID, Name: target.Name, Host: target.Host, Port: target.Port, Protocol: target.Protocol, Username: target.Username, Domain: target.Domain, Password: target.Password, PrivateKeyPath: target.PrivateKeyPath, PrivateKeyPEM: target.PrivateKeyPEM, Passphrase: target.Passphrase, InsecureIgnoreHostKey: target.InsecureIgnoreHostKey, HostKeyFingerprint: target.HostKeyFingerprint, KnownHostsPath: target.KnownHostsPath, RDPSecurity: target.RDPSecurity, RDPIgnoreCertificate: target.RDPIgnoreCertificate, RDPCertFingerprints: target.RDPCertFingerprints, Disabled: target.Disabled, ExpiresAt: target.ExpiresAt, HostID: target.HostID}
}
