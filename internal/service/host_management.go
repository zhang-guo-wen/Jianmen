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
)

// HostManagementRepository is the persistence contract required by host and
// host-account operations. It deliberately excludes unrelated Admin resources.
type HostManagementRepository interface {
	Hosts(context.Context) ([]HostManagementHostView, error)
	Host(context.Context, string) (HostManagementHostView, error)
	AddHost(context.Context, HostManagementHostRecord) (HostManagementHostView, error)
	CreateManagedHost(context.Context, HostManagementHostRecord, string) (HostManagementHostView, error)
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

type HostManagementActor struct {
	ID         string
	SuperAdmin bool
}

type HostManagementService struct {
	repository HostManagementRepository
	authorizer HostManagementAuthorizer
}

func NewHostManagementService(
	repository HostManagementRepository,
	authorizer HostManagementAuthorizer,
) (*HostManagementService, error) {
	if repository == nil {
		return nil, errors.New("host management repository is required")
	}
	if authorizer == nil {
		return nil, errors.New("host management authorizer is required")
	}
	return &HostManagementService{repository: repository, authorizer: authorizer}, nil
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

func (s *HostManagementService) filterTargets(ctx context.Context, actor HostManagementActor, targets []HostManagementTargetView, connectable bool) ([]HostManagementTargetView, error) {
	if connectable {
		now := time.Now().UTC()
		candidates := make([]HostManagementTargetView, 0, len(targets))
		for _, target := range targets {
			if targetLifecycleConnectable(target, now) {
				candidates = append(candidates, target)
			}
		}
		targets = candidates
	}
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

func targetIDs(targets []HostManagementTargetView) []string {
	ids := make([]string, len(targets))
	for index := range targets {
		ids[index] = targets[index].ID
	}
	return ids
}

func targetLifecycleConnectable(target HostManagementTargetView, now time.Time) bool {
	if strings.EqualFold(target.Status, "disabled") || strings.EqualFold(target.HostStatus, "disabled") {
		return false
	}
	if strings.TrimSpace(target.ExpiresAt) == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, target.ExpiresAt)
	return err == nil && now.Before(expiresAt)
}
