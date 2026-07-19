package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

var (
	ErrHostAccessDenied       = errors.New("host resource access denied")
	ErrHostTargetUnavailable  = errors.New("host target is unavailable")
	ErrHostTargetInvalidInput = errors.New("invalid host target input")
	ErrHostGrantFailed        = errors.New("grant created host failed")
)

// HostManagementRepository is the persistence contract required by host and
// host-account operations. It deliberately excludes unrelated Admin resources.
type HostManagementRepository interface {
	Hosts(context.Context) ([]HostManagementHostView, error)
	Host(context.Context, string) (HostManagementHostView, error)
	AddHost(context.Context, HostManagementHostRecord) (HostManagementHostView, error)
	UpdateHost(context.Context, string, HostManagementHostRecord) (HostManagementHostView, error)
	DeleteHost(context.Context, string) error
	Targets(context.Context) ([]HostManagementTargetView, error)
	ListHostAccounts(context.Context, string) ([]HostManagementTargetView, error)
	Target(context.Context, string) (HostManagementTargetView, error)
	TargetConfig(context.Context, string) (HostManagementTargetConfig, error)
	AddTarget(context.Context, config.Target) (HostManagementTargetView, error)
	UpdateTarget(context.Context, string, config.Target) (HostManagementTargetView, error)
	DeleteTarget(context.Context, string) error
}

type HostManagementAuthorizer interface {
	AuthorizeConnection(context.Context, string, []string, string, string) (bool, error)
	AuthorizeBatch(context.Context, string, []AuthorizationRequest) ([]AuthorizationDecision, error)
}

type HostManagementGrants interface {
	GrantCreatedResource(context.Context, string, bool, string, string) error
}

type HostManagementActor struct {
	ID         string
	SuperAdmin bool
}

type HostManagementService struct {
	repository     HostManagementRepository
	authorizer     HostManagementAuthorizer
	resourceGrants HostManagementGrants
}

func NewHostManagementService(
	repository HostManagementRepository,
	authorizer HostManagementAuthorizer,
	resourceGrants HostManagementGrants,
) (*HostManagementService, error) {
	if repository == nil {
		return nil, errors.New("host management repository is required")
	}
	if authorizer == nil {
		return nil, errors.New("host management authorizer is required")
	}
	if resourceGrants == nil {
		return nil, errors.New("host management resource grant service is required")
	}
	return &HostManagementService{repository: repository, authorizer: authorizer, resourceGrants: resourceGrants}, nil
}

func (s *HostManagementService) ListHosts(ctx context.Context, actor HostManagementActor) ([]HostManagementHostView, error) {
	if strings.TrimSpace(actor.ID) == "" {
		return nil, ErrHostAccessDenied
	}
	hosts, err := s.repository.Hosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	hostIDs := make([]string, len(hosts))
	for index := range hosts {
		hostIDs[index] = hosts[index].ID
	}
	visible, err := s.authorizeBatch(ctx, actor, []string{rbac.ActionHostView}, model.ResourceTypeHost, hostIDs)
	if err != nil {
		return nil, err
	}
	manageable, err := s.authorizeBatch(ctx, actor, []string{rbac.ActionHostUpdate, rbac.ActionHostDelete}, model.ResourceTypeHost, hostIDs)
	if err != nil {
		return nil, err
	}
	targets, err := s.repository.Targets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list host accounts for host visibility: %w", err)
	}
	visibleTargets, err := s.filterTargets(ctx, actor, targets, false)
	if err != nil {
		return nil, err
	}
	targetCounts := make(map[string]int, len(hosts))
	for _, target := range visibleTargets {
		targetCounts[target.HostID]++
	}
	result := make([]HostManagementHostView, 0, len(hosts))
	for index, host := range hosts {
		if visible[index] {
			host.CanManage = manageable[index]
			result = append(result, host)
			continue
		}
		if targetCounts[host.ID] == 0 {
			continue
		}
		host.AccountCount = targetCounts[host.ID]
		host.CanManage = false
		result = append(result, host)
	}
	return result, nil
}

func (s *HostManagementService) Host(ctx context.Context, actor HostManagementActor, hostID string) (HostManagementHostView, error) {
	allowed, err := s.authorize(ctx, actor, []string{rbac.ActionHostView}, model.ResourceTypeHost, hostID)
	if err != nil {
		return HostManagementHostView{}, err
	}
	if !allowed {
		accounts, err := s.repository.ListHostAccounts(ctx, hostID)
		if err != nil {
			return HostManagementHostView{}, fmt.Errorf("list host accounts for visibility: %w", err)
		}
		for _, account := range accounts {
			allowed, err = s.authorize(ctx, actor, []string{rbac.ActionTargetView}, model.ResourceTypeHostAccount, account.ID)
			if err != nil {
				return HostManagementHostView{}, err
			}
			if allowed {
				break
			}
		}
		if !allowed {
			return HostManagementHostView{}, ErrHostAccessDenied
		}
	}
	view, err := s.repository.Host(ctx, hostID)
	if err != nil {
		return HostManagementHostView{}, fmt.Errorf("get host: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) CreateHost(ctx context.Context, actor HostManagementActor, host HostManagementHostRecord) (HostManagementHostView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionHostCreate}, "", ""); err != nil {
		return HostManagementHostView{}, err
	}
	view, err := s.repository.AddHost(ctx, host)
	if err != nil {
		return HostManagementHostView{}, fmt.Errorf("create host: %w", err)
	}
	if err := s.resourceGrants.GrantCreatedResource(ctx, actor.ID, actor.SuperAdmin, model.ResourceTypeHost, view.ID); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if cleanupErr := s.repository.DeleteHost(cleanupCtx, view.ID); cleanupErr != nil {
			return HostManagementHostView{}, errors.Join(fmt.Errorf("%w: %v", ErrHostGrantFailed, err), fmt.Errorf("delete ungranted host: %w", cleanupErr))
		}
		return HostManagementHostView{}, fmt.Errorf("%w: %v", ErrHostGrantFailed, err)
	}
	return view, nil
}

func (s *HostManagementService) UpdateHost(ctx context.Context, actor HostManagementActor, hostID string, host HostManagementHostRecord) (HostManagementHostView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionHostUpdate}, model.ResourceTypeHost, hostID); err != nil {
		return HostManagementHostView{}, err
	}
	view, err := s.repository.UpdateHost(ctx, hostID, host)
	if err != nil {
		return HostManagementHostView{}, fmt.Errorf("update host: %w", err)
	}
	return view, nil
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

func (s *HostManagementService) ListTargets(ctx context.Context, actor HostManagementActor, connectable bool) ([]HostManagementTargetView, error) {
	if connectable {
		if err := s.require(ctx, actor, []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect, rbac.ActionRDPConnect}, "", ""); err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(actor.ID) == "" {
		return nil, ErrHostAccessDenied
	}
	targets, err := s.repository.Targets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list host accounts: %w", err)
	}
	return s.filterTargets(ctx, actor, targets, connectable)
}

func (s *HostManagementService) ListHostAccounts(ctx context.Context, actor HostManagementActor, hostID string, connectable bool) ([]HostManagementTargetView, error) {
	if connectable {
		if err := s.require(ctx, actor, []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect, rbac.ActionRDPConnect}, "", ""); err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(actor.ID) == "" {
		return nil, ErrHostAccessDenied
	}
	targets, err := s.repository.ListHostAccounts(ctx, hostID)
	if err != nil {
		return nil, fmt.Errorf("list host accounts: %w", err)
	}
	return s.filterTargets(ctx, actor, targets, connectable)
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

func (s *HostManagementService) ResolveConnectionTest(ctx context.Context, actor HostManagementActor, input config.Target) (HostManagementTargetConfig, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, "", ""); err != nil {
		return HostManagementTargetConfig{}, err
	}
	config := targetConfigFromInput(input)
	if config.ID != "" {
		if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, model.ResourceTypeHostAccount, config.ID); err != nil {
			return HostManagementTargetConfig{}, err
		}
		if config.Password == "" && config.PrivateKeyPath == "" && config.PrivateKeyPEM == "" {
			stored, err := s.repository.TargetConfig(ctx, config.ID)
			if err != nil {
				return HostManagementTargetConfig{}, fmt.Errorf("load host account credentials: %w", err)
			}
			if config.HostID != "" && config.HostID != stored.HostID {
				return HostManagementTargetConfig{}, fmt.Errorf("%w: target does not belong to host", ErrHostTargetInvalidInput)
			}
			config = mergeTargetConfig(config, stored, inputHostKeyConfigProvided(input))
		}
	} else if config.HostID != "" {
		if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, model.ResourceTypeHost, config.HostID); err != nil {
			return HostManagementTargetConfig{}, err
		}
	}
	if config.HostID != "" && (config.Host == "" || config.Port == 0) {
		host, err := s.repository.Host(ctx, config.HostID)
		if err != nil {
			return HostManagementTargetConfig{}, fmt.Errorf("load host for connection test: %w", err)
		}
		if strings.EqualFold(host.Status, "disabled") {
			return HostManagementTargetConfig{}, ErrHostTargetUnavailable
		}
		if config.Host == "" {
			config.Host = host.Address
		}
		if config.Protocol == "" {
			config.Protocol = host.Protocol
		}
		if config.Port == 0 {
			config.Port = host.Port
		}
	}
	if config.Disabled || config.Expired(time.Now().UTC()) {
		return HostManagementTargetConfig{}, ErrHostTargetUnavailable
	}
	if config.Addr() == "" || strings.TrimSpace(config.Username) == "" {
		return HostManagementTargetConfig{}, fmt.Errorf("%w: host, port, and username are required", ErrHostTargetInvalidInput)
	}
	return config, nil
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
		if strings.EqualFold(target.Protocol, "rdp") || strings.EqualFold(target.Status, "disabled") || targetExpired(target) {
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

func (s *HostManagementService) filterTargets(ctx context.Context, actor HostManagementActor, targets []HostManagementTargetView, connectable bool) ([]HostManagementTargetView, error) {
	requests := make([]AuthorizationRequest, len(targets))
	for index, target := range targets {
		actions := []string{rbac.ActionTargetView}
		if connectable {
			actions = []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}
			if strings.EqualFold(target.Protocol, "rdp") {
				actions = []string{rbac.ActionRDPConnect}
			}
		}
		requests[index] = AuthorizationRequest{Actions: actions, ResourceType: model.ResourceTypeHostAccount, ResourceID: target.ID}
	}
	visible, err := s.authorizeRequests(ctx, actor, requests)
	if err != nil {
		return nil, err
	}
	manageable, err := s.authorizeBatch(ctx, actor, []string{rbac.ActionTargetUpdate, rbac.ActionTargetDelete}, model.ResourceTypeHostAccount, targetIDs(targets))
	if err != nil {
		return nil, err
	}
	result := make([]HostManagementTargetView, 0, len(targets))
	for index, target := range targets {
		if !visible[index] {
			continue
		}
		target.CanManage = manageable[index]
		result = append(result, target)
	}
	return result, nil
}

func (s *HostManagementService) require(ctx context.Context, actor HostManagementActor, actions []string, resourceType, resourceID string) error {
	allowed, err := s.authorize(ctx, actor, actions, resourceType, resourceID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrHostAccessDenied
	}
	return nil
}

func (s *HostManagementService) authorize(ctx context.Context, actor HostManagementActor, actions []string, resourceType, resourceID string) (bool, error) {
	if strings.TrimSpace(actor.ID) == "" {
		return false, nil
	}
	allowed, err := s.authorizer.AuthorizeConnection(ctx, actor.ID, actions, resourceType, resourceID)
	if err != nil {
		return false, fmt.Errorf("authorize host resource: %w", err)
	}
	return allowed, nil
}

func (s *HostManagementService) authorizeBatch(ctx context.Context, actor HostManagementActor, actions []string, resourceType string, ids []string) ([]bool, error) {
	requests := make([]AuthorizationRequest, len(ids))
	for index, id := range ids {
		requests[index] = AuthorizationRequest{Actions: actions, ResourceType: resourceType, ResourceID: id}
	}
	return s.authorizeRequests(ctx, actor, requests)
}

func (s *HostManagementService) authorizeRequests(ctx context.Context, actor HostManagementActor, requests []AuthorizationRequest) ([]bool, error) {
	allowed := make([]bool, len(requests))
	if len(requests) == 0 || strings.TrimSpace(actor.ID) == "" {
		return allowed, nil
	}
	decisions, err := s.authorizer.AuthorizeBatch(ctx, actor.ID, requests)
	if err != nil {
		return nil, fmt.Errorf("batch authorize host resources: %w", err)
	}
	if len(decisions) != len(requests) {
		return nil, errors.New("host authorization decision count mismatch")
	}
	for index, decision := range decisions {
		allowed[index] = decision.Allowed
	}
	return allowed, nil
}

func targetConfigFromInput(target config.Target) HostManagementTargetConfig {
	return HostManagementTargetConfig{ID: target.ID, Name: target.Name, Host: target.Host, Port: target.Port, Protocol: target.Protocol, Username: target.Username, Domain: target.Domain, Password: target.Password, PrivateKeyPath: target.PrivateKeyPath, PrivateKeyPEM: target.PrivateKeyPEM, Passphrase: target.Passphrase, InsecureIgnoreHostKey: target.InsecureIgnoreHostKey, HostKeyFingerprint: target.HostKeyFingerprint, KnownHostsPath: target.KnownHostsPath, RDPSecurity: target.RDPSecurity, RDPIgnoreCertificate: target.RDPIgnoreCertificate, RDPCertFingerprints: target.RDPCertFingerprints, Disabled: target.Disabled, ExpiresAt: target.ExpiresAt, HostID: target.HostID}
}

func mergeTargetConfig(input, stored HostManagementTargetConfig, hostKeyConfigProvided bool) HostManagementTargetConfig {
	stored.Host = firstNonEmpty(input.Host, stored.Host)
	if input.Port != 0 {
		stored.Port = input.Port
	}
	stored.Username = firstNonEmpty(input.Username, stored.Username)
	stored.Name = firstNonEmpty(input.Name, stored.Name)
	stored.HostID = firstNonEmpty(input.HostID, stored.HostID)
	stored.Protocol = firstNonEmpty(input.Protocol, stored.Protocol)
	stored.Domain = firstNonEmpty(input.Domain, stored.Domain)
	stored.RDPSecurity = firstNonEmpty(input.RDPSecurity, stored.RDPSecurity)
	stored.RDPIgnoreCertificate = input.RDPIgnoreCertificate
	stored.RDPCertFingerprints = firstNonEmpty(input.RDPCertFingerprints, stored.RDPCertFingerprints)
	if hostKeyConfigProvided {
		stored.InsecureIgnoreHostKey = input.InsecureIgnoreHostKey
		stored.HostKeyFingerprint = input.HostKeyFingerprint
		stored.KnownHostsPath = input.KnownHostsPath
	}
	stored.Disabled = input.Disabled
	stored.ExpiresAt = input.ExpiresAt
	return stored
}

func inputHostKeyConfigProvided(target config.Target) bool {
	return target.InsecureIgnoreHostKey || target.HostKeyFingerprint != "" || target.KnownHostsPath != ""
}

func targetIDs(targets []HostManagementTargetView) []string {
	ids := make([]string, len(targets))
	for index := range targets {
		ids[index] = targets[index].ID
	}
	return ids
}

func targetExpired(target HostManagementTargetView) bool {
	if strings.TrimSpace(target.ExpiresAt) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, target.ExpiresAt)
	return err == nil && !time.Now().UTC().Before(expiresAt)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
