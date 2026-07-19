package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"jianmen/internal/config"
)

func TestHostManagementCreateHostUsesAtomicRepositoryBoundary(t *testing.T) {
	repository := &hostManagementTestRepository{}
	svc := newHostManagementTestService(t, repository)

	if _, err := svc.CreateHost(context.Background(), HostManagementActor{ID: "creator"}, HostManagementHostRecord{Name: "host"}); err != nil {
		t.Fatalf("CreateHost: %v", err)
	}
	if repository.managedCreatorID != "creator" {
		t.Fatalf("managed creator ID = %q, want creator", repository.managedCreatorID)
	}

	if _, err := svc.CreateHost(context.Background(), HostManagementActor{ID: "root", SuperAdmin: true}, HostManagementHostRecord{Name: "host-2"}); err != nil {
		t.Fatalf("CreateHost super admin: %v", err)
	}
	if repository.managedCreatorID != "" {
		t.Fatalf("super-admin managed creator ID = %q, want empty", repository.managedCreatorID)
	}
}

func TestResolveConnectionTestRejectsPersistedCredentialEndpointOverride(t *testing.T) {
	repository := &hostManagementTestRepository{
		targetConfig: HostManagementTargetConfig{
			ID: "account-1", HostID: "host-1", Host: "10.0.0.10", Port: 22,
			Protocol: "ssh", Username: "root", Password: "stored-secret",
		},
		host: HostManagementHostView{ID: "host-1", Address: "10.0.0.10", Port: 22, Protocol: "ssh", Status: "active"},
	}
	svc := newHostManagementTestService(t, repository)

	resolved, err := svc.ResolveConnectionTest(context.Background(), HostManagementActor{ID: "operator"}, config.Target{
		ID: "account-1", Host: "203.0.113.77", Port: 65022,
	})
	if !errors.Is(err, ErrHostTargetInvalidInput) {
		t.Fatalf("ResolveConnectionTest error = %v, want invalid endpoint", err)
	}
	if resolved.Password != "" || resolved.Host != "" || resolved.Port != 0 {
		t.Fatalf("rejected resolution leaked credentials or endpoint: %#v", resolved)
	}
	if repository.targetConfigCalls != 1 || repository.hostCalls != 1 {
		t.Fatalf("repository calls: target=%d host=%d, want 1/1", repository.targetConfigCalls, repository.hostCalls)
	}
}

func TestResolveConnectionTestExplicitCredentialsCannotBypassPersistedLifecycle(t *testing.T) {
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	tests := []struct {
		name   string
		target HostManagementTargetConfig
		host   HostManagementHostView
	}{
		{
			name:   "target disabled",
			target: HostManagementTargetConfig{ID: "account-1", HostID: "host-1", Username: "root", Disabled: true},
			host:   HostManagementHostView{ID: "host-1", Address: "10.0.0.10", Port: 22, Protocol: "ssh", Status: "active"},
		},
		{
			name:   "target expired",
			target: HostManagementTargetConfig{ID: "account-1", HostID: "host-1", Username: "root", ExpiresAt: expired},
			host:   HostManagementHostView{ID: "host-1", Address: "10.0.0.10", Port: 22, Protocol: "ssh", Status: "active"},
		},
		{
			name:   "parent host disabled",
			target: HostManagementTargetConfig{ID: "account-1", HostID: "host-1", Username: "root"},
			host:   HostManagementHostView{ID: "host-1", Address: "10.0.0.10", Port: 22, Protocol: "ssh", Status: "disabled"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &hostManagementTestRepository{targetConfig: tt.target, host: tt.host}
			svc := newHostManagementTestService(t, repository)
			_, err := svc.ResolveConnectionTest(context.Background(), HostManagementActor{ID: "operator"}, config.Target{
				ID: "account-1", Username: "edited", Password: "explicit-secret",
			})
			if !errors.Is(err, ErrHostTargetUnavailable) {
				t.Fatalf("ResolveConnectionTest error = %v, want unavailable", err)
			}
		})
	}
}

func TestResolveConnectionTestArbitraryEndpointRequiresSuperAdmin(t *testing.T) {
	repository := &hostManagementTestRepository{}
	svc := newHostManagementTestService(t, repository)

	_, err := svc.ResolveConnectionTest(context.Background(), HostManagementActor{ID: "operator"}, config.Target{
		Host: "203.0.113.10", Port: 22, Protocol: "ssh", Username: "root", Password: "secret",
	})
	if !errors.Is(err, ErrHostAccessDenied) {
		t.Fatalf("ResolveConnectionTest error = %v, want access denied", err)
	}
	if repository.targetConfigCalls != 0 || repository.hostCalls != 0 {
		t.Fatalf("arbitrary endpoint touched repository: target=%d host=%d", repository.targetConfigCalls, repository.hostCalls)
	}
}

func TestListConnectableTargetsFiltersLifecycleWithoutLoadingCredentials(t *testing.T) {
	now := time.Now().UTC()
	repository := &hostManagementTestRepository{targets: []HostManagementTargetView{
		{ID: "active", Status: "enabled", HostStatus: "active", Protocol: "ssh"},
		{ID: "target-disabled", Status: "disabled", HostStatus: "active", Protocol: "ssh"},
		{ID: "expired", Status: "enabled", HostStatus: "active", Protocol: "ssh", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339Nano)},
		{ID: "host-disabled", Status: "enabled", HostStatus: "disabled", Protocol: "ssh"},
	}}
	svc := newHostManagementTestService(t, repository)

	targets, err := svc.ListTargets(context.Background(), HostManagementActor{ID: "operator"}, true)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != "active" {
		t.Fatalf("connectable targets = %#v, want only active", targets)
	}
	if repository.targetConfigCalls != 0 {
		t.Fatalf("connectable list loaded %d secret configs, want 0", repository.targetConfigCalls)
	}
}

func newHostManagementTestService(t *testing.T, repository HostManagementRepository) *HostManagementService {
	t.Helper()
	svc, err := NewHostManagementService(repository, hostManagementAllowAuthorizer{})
	if err != nil {
		t.Fatalf("NewHostManagementService: %v", err)
	}
	return svc
}

type hostManagementTestRepository struct {
	HostManagementRepository
	managedCreatorID  string
	targetConfig      HostManagementTargetConfig
	host              HostManagementHostView
	targets           []HostManagementTargetView
	targetConfigCalls int
	hostCalls         int
}

func (r *hostManagementTestRepository) CreateManagedHost(_ context.Context, _ HostManagementHostRecord, creatorID string) (HostManagementHostView, error) {
	r.managedCreatorID = creatorID
	return HostManagementHostView{ID: "created"}, nil
}

func (r *hostManagementTestRepository) TargetConfig(context.Context, string) (HostManagementTargetConfig, error) {
	r.targetConfigCalls++
	return r.targetConfig, nil
}

func (r *hostManagementTestRepository) Host(context.Context, string) (HostManagementHostView, error) {
	r.hostCalls++
	return r.host, nil
}

func (r *hostManagementTestRepository) Targets(context.Context) ([]HostManagementTargetView, error) {
	return append([]HostManagementTargetView(nil), r.targets...), nil
}

type hostManagementAllowAuthorizer struct{}

func (hostManagementAllowAuthorizer) AuthorizeConnection(context.Context, string, []string, string, string) (bool, error) {
	return true, nil
}

func (hostManagementAllowAuthorizer) AuthorizeBatch(_ context.Context, _ string, requests []AuthorizationRequest) ([]AuthorizationDecision, error) {
	decisions := make([]AuthorizationDecision, len(requests))
	for index := range decisions {
		decisions[index] = AuthorizationDecision{Allowed: true}
	}
	return decisions, nil
}
