package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/rbac"
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

func TestCreateSSHHostCaptureFailurePersistsDisabledAndUnavailable(t *testing.T) {
	repository := &hostManagementTestRepository{}
	collector := &hostManagementRecordingIdentityCollector{err: errors.New("unreachable")}
	svc := newHostManagementTestServiceWithCollector(t, repository, collector)

	_, err := svc.CreateHost(context.Background(), HostManagementActor{ID: "creator"}, HostManagementHostRecord{
		ID: "host-1", Address: "192.0.2.10", Port: 22, Protocol: "ssh", Status: "active",
	})
	if err != nil {
		t.Fatalf("CreateHost: %v", err)
	}
	if repository.managedRecord.Status != "disabled" ||
		repository.managedRecord.HostKeyFingerprint != "" ||
		repository.managedRecord.KnownHosts != "" {
		t.Fatalf("capture failure persisted unsafe host: %#v", repository.managedRecord)
	}
	if collector.calls != 1 {
		t.Fatalf("collector calls = %d, want 1", collector.calls)
	}
}

func TestReenableSSHHostRequiresSuccessfulIdentityRefresh(t *testing.T) {
	repository := &hostManagementTestRepository{host: HostManagementHostView{
		ID: "host-1", Address: "192.0.2.10", Port: 22, Protocol: "ssh", Status: "disabled",
		HostKeyFingerprint: "SHA256:old", KnownHosts: "old known hosts",
	}}
	collector := &hostManagementRecordingIdentityCollector{err: errors.New("unreachable")}
	svc := newHostManagementTestServiceWithCollector(t, repository, collector)
	input := HostManagementHostRecord{
		Name: "host-1", Address: "192.0.2.10", Port: 22, Protocol: "ssh", Status: "active",
	}

	_, err := svc.UpdateHost(context.Background(), HostManagementActor{ID: "operator"}, "host-1", input)
	if !errors.Is(err, ErrHostIdentityRefreshFailed) {
		t.Fatalf("UpdateHost error = %v, want identity refresh failure", err)
	}
	if repository.updateCalls != 0 {
		t.Fatalf("repository update calls = %d, want 0", repository.updateCalls)
	}

	collector.err = nil
	collector.identity = HostIdentity{Fingerprint: "SHA256:new", KnownHosts: "new known hosts"}
	if _, err := svc.UpdateHost(context.Background(), HostManagementActor{ID: "operator"}, "host-1", input); err != nil {
		t.Fatalf("UpdateHost after identity recovery: %v", err)
	}
	if repository.updateCalls != 1 ||
		repository.updatedRecord.Status != "active" ||
		repository.updatedRecord.HostKeyFingerprint != "SHA256:new" ||
		repository.updatedRecord.KnownHosts != "new known hosts" {
		t.Fatalf("unexpected refreshed update: calls=%d record=%#v", repository.updateCalls, repository.updatedRecord)
	}
}

func TestConfirmHostIdentityRequiresExplicitConfirmationAndUpdatePermission(t *testing.T) {
	repository := &hostManagementTestRepository{host: HostManagementHostView{
		ID: "host-1", Address: "192.0.2.10", Port: 22, Protocol: "ssh", Status: "disabled",
	}}
	collector := &hostManagementRecordingIdentityCollector{
		identity: HostIdentity{Fingerprint: "SHA256:new", KnownHosts: "new known hosts"},
	}
	svc := newHostManagementTestServiceWithCollector(t, repository, collector)

	if _, err := svc.ConfirmHostIdentity(
		context.Background(), HostManagementActor{ID: "operator"}, "host-1", false, "SHA256:new",
	); !errors.Is(err, ErrHostIdentityConfirmationRequired) {
		t.Fatalf("ConfirmHostIdentity error = %v, want explicit confirmation", err)
	}
	if repository.hostCalls != 0 || repository.refreshCalls != 0 || collector.calls != 0 {
		t.Fatalf("unconfirmed refresh performed work: host=%d refresh=%d collect=%d",
			repository.hostCalls, repository.refreshCalls, collector.calls)
	}
	if _, err := svc.ConfirmHostIdentity(
		context.Background(), HostManagementActor{ID: "operator"}, "host-1", true, "",
	); !errors.Is(err, ErrHostTargetInvalidInput) {
		t.Fatalf("empty fingerprint error = %v, want invalid input", err)
	}
	if _, err := svc.ConfirmHostIdentity(
		context.Background(), HostManagementActor{ID: "operator"}, "host-1", true, strings.Repeat("x", 257),
	); !errors.Is(err, ErrHostTargetInvalidInput) {
		t.Fatalf("oversized fingerprint error = %v, want invalid input", err)
	}

	deniedAuthorizer := &hostManagementSelectiveAuthorizer{allowedAction: rbac.ActionSessionConnect}
	deniedService, err := NewHostManagementService(repository, deniedAuthorizer, collector)
	if err != nil {
		t.Fatalf("NewHostManagementService: %v", err)
	}
	if _, err := deniedService.ConfirmHostIdentity(
		context.Background(), HostManagementActor{ID: "connect-only"}, "host-1", true, "SHA256:new",
	); !errors.Is(err, ErrHostAccessDenied) {
		t.Fatalf("connect-only confirmation error = %v, want access denied", err)
	}
	if repository.hostCalls != 0 || repository.refreshCalls != 0 || collector.calls != 0 {
		t.Fatalf("unauthorized refresh performed work: host=%d refresh=%d collect=%d",
			repository.hostCalls, repository.refreshCalls, collector.calls)
	}
}

func TestConfirmHostIdentityRejectsFingerprintThatChangedAfterWarning(t *testing.T) {
	revision := time.Now().UTC()
	repository := &hostManagementTestRepository{host: HostManagementHostView{
		ID: "host-1", Address: "192.0.2.10", Port: 22, Protocol: "ssh", Status: "disabled",
		IdentityStatus: "available", Revision: revision,
		PersistedStatus: "disabled", PersistedFingerprint: "SHA256:old",
		PersistedKnownHosts: "old known hosts\n",
	}}
	collector := &hostManagementRecordingIdentityCollector{
		identity: HostIdentity{Fingerprint: "SHA256:third", KnownHosts: "third known hosts"},
	}
	svc := newHostManagementTestServiceWithCollector(t, repository, collector)

	_, err := svc.ConfirmHostIdentity(
		context.Background(), HostManagementActor{ID: "operator"}, "host-1", true, "SHA256:second",
	)
	var mismatch *HostIdentityConfirmationMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("ConfirmHostIdentity error = %v, want confirmation mismatch", err)
	}
	if mismatch.OldFingerprint != "SHA256:old" ||
		mismatch.ExpectedFingerprint != "SHA256:second" ||
		mismatch.ActualFingerprint != "SHA256:third" {
		t.Fatalf("confirmation mismatch = %#v", mismatch)
	}
	if repository.refreshCalls != 0 {
		t.Fatalf("mismatched identity persisted %d times", repository.refreshCalls)
	}
}

func TestConfirmHostIdentityRefreshesVerificationMaterialAndActivatesHost(t *testing.T) {
	revision := time.Now().UTC()
	repository := &hostManagementTestRepository{host: HostManagementHostView{
		ID: "host-1", Address: "192.0.2.10", Port: 22, Protocol: "ssh", Status: "disabled",
		IdentityStatus: "unavailable", Revision: revision, PersistedStatus: "disabled",
	}}
	collector := &hostManagementRecordingIdentityCollector{
		identity: HostIdentity{Fingerprint: "SHA256:new", KnownHosts: "new known hosts"},
	}
	svc := newHostManagementTestServiceWithCollector(t, repository, collector)

	result, err := svc.ConfirmHostIdentity(
		context.Background(), HostManagementActor{ID: "operator"}, "host-1", true, "SHA256:new",
	)
	if err != nil {
		t.Fatalf("ConfirmHostIdentity: %v", err)
	}
	if collector.calls != 1 || repository.refreshCalls != 1 {
		t.Fatalf("refresh calls: collect=%d repository=%d, want 1/1", collector.calls, repository.refreshCalls)
	}
	if repository.refreshedIdentity != (HostManagementIdentityRefresh{
		Address: "192.0.2.10", Port: 22, Protocol: "ssh", Status: "disabled", UpdatedAt: revision,
		Fingerprint: "SHA256:new", KnownHosts: "new known hosts",
	}) {
		t.Fatalf("identity refresh = %#v", repository.refreshedIdentity)
	}
	if result.Host.Status != "active" || result.Host.IdentityStatus != "available" ||
		result.Host.HostKeyFingerprint != "SHA256:new" || result.Host.KnownHosts != "new known hosts" {
		t.Fatalf("refreshed host = %#v", result.Host)
	}
}

func TestResolveConnectionTestProbesActivePartialIdentityWithoutPersisting(t *testing.T) {
	repository := &hostManagementTestRepository{
		targetConfig: HostManagementTargetConfig{
			ID: "account-1", HostID: "host-1", Protocol: "ssh", Username: "root",
		},
		host: HostManagementHostView{
			ID: "host-1", Address: "192.0.2.10", Port: 22, Protocol: "ssh",
			Status: "active", IdentityStatus: "unavailable",
			HostKeyFingerprint: "SHA256:partial", PersistedFingerprint: "SHA256:partial",
		},
	}
	collector := &hostManagementRecordingIdentityCollector{
		identity: HostIdentity{Fingerprint: "SHA256:candidate", KnownHosts: "candidate known hosts"},
	}
	svc := newHostManagementTestServiceWithCollector(t, repository, collector)

	_, err := svc.ResolveConnectionTest(
		context.Background(), HostManagementActor{ID: "operator"}, config.Target{ID: "account-1"},
	)
	var unavailable *HostIdentityUnavailableError
	if !errors.As(err, &unavailable) || unavailable.NewFingerprint != "SHA256:candidate" {
		t.Fatalf("ResolveConnectionTest error = %#v, want candidate identity", err)
	}
	if collector.calls != 1 || repository.refreshCalls != 0 {
		t.Fatalf("probe calls: collect=%d persist=%d, want 1/0", collector.calls, repository.refreshCalls)
	}
}

func TestResolveConnectionTestDoesNotProbeDisabledUnavailableHost(t *testing.T) {
	repository := &hostManagementTestRepository{
		targetConfig: HostManagementTargetConfig{
			ID: "account-1", HostID: "host-1", Protocol: "ssh", Username: "root",
		},
		host: HostManagementHostView{
			ID: "host-1", Address: "192.0.2.10", Port: 22, Protocol: "ssh",
			Status: "disabled", IdentityStatus: "unavailable",
		},
	}
	collector := &hostManagementRecordingIdentityCollector{
		identity: HostIdentity{Fingerprint: "SHA256:candidate", KnownHosts: "candidate known hosts"},
	}
	svc := newHostManagementTestServiceWithCollector(t, repository, collector)

	_, err := svc.ResolveConnectionTest(
		context.Background(), HostManagementActor{ID: "operator"}, config.Target{ID: "account-1"},
	)
	if !errors.Is(err, ErrHostTargetUnavailable) {
		t.Fatalf("ResolveConnectionTest error = %v, want unavailable lifecycle", err)
	}
	if collector.calls != 0 {
		t.Fatalf("disabled host identity probed %d times", collector.calls)
	}
}

func TestConfirmHostIdentityCollectionFailureLeavesHostUnchanged(t *testing.T) {
	repository := &hostManagementTestRepository{host: HostManagementHostView{
		ID: "host-1", Address: "192.0.2.10", Port: 22, Protocol: "ssh", Status: "disabled",
		IdentityStatus: "unavailable", Revision: time.Now().UTC(), PersistedStatus: "disabled",
	}}
	collector := &hostManagementRecordingIdentityCollector{err: errors.New("unreachable")}
	svc := newHostManagementTestServiceWithCollector(t, repository, collector)

	_, err := svc.ConfirmHostIdentity(
		context.Background(), HostManagementActor{ID: "operator"}, "host-1", true, "SHA256:candidate",
	)
	if !errors.Is(err, ErrHostIdentityRefreshFailed) {
		t.Fatalf("ConfirmHostIdentity error = %v, want refresh failure", err)
	}
	if repository.refreshCalls != 0 {
		t.Fatalf("repository refresh calls = %d, want 0", repository.refreshCalls)
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

func TestResolveConnectionTestStoredAccountAllowsConnectOnlyAction(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		action   string
		port     int
	}{
		{name: "SSH session", protocol: "ssh", action: rbac.ActionSessionConnect, port: 22},
		{name: "SFTP", protocol: "ssh", action: rbac.ActionSFTPConnect, port: 22},
		{name: "RDP", protocol: "rdp", action: rbac.ActionRDPConnect, port: 3389},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &hostManagementTestRepository{
				target: HostManagementTargetView{ID: "account-1", HostID: "host-1", Protocol: tt.protocol},
				targetConfig: HostManagementTargetConfig{
					ID: "account-1", HostID: "host-1", Protocol: tt.protocol,
					Username: "root", Password: "stored-secret",
				},
				host: HostManagementHostView{
					ID: "host-1", Address: "192.0.2.10", Port: tt.port, Protocol: tt.protocol, Status: "active",
					IdentityStatus: "available", HostKeyFingerprint: "SHA256:test", KnownHosts: "known hosts",
				},
			}
			authorizer := &hostManagementSelectiveAuthorizer{allowedAction: tt.action}
			svc, err := NewHostManagementService(repository, authorizer, hostManagementTestIdentityCollector{})
			if err != nil {
				t.Fatalf("NewHostManagementService: %v", err)
			}

			resolved, err := svc.ResolveConnectionTest(
				context.Background(),
				HostManagementActor{ID: "connect-only"},
				config.Target{ID: "account-1"},
			)
			if err != nil {
				t.Fatalf("ResolveConnectionTest: %v", err)
			}
			if resolved.ID != "account-1" || resolved.Host != "192.0.2.10" || resolved.Port != tt.port {
				t.Fatalf("unexpected resolved target: %#v", resolved)
			}
			if len(authorizer.calls) != 2 ||
				!containsAction(authorizer.calls[0], tt.action) ||
				!containsAction(authorizer.calls[0], rbac.ActionTargetCreate) ||
				!containsAction(authorizer.calls[1], tt.action) {
				t.Fatalf("authorization calls = %#v, want %q or target:create", authorizer.calls, tt.action)
			}
		})
	}
}

func TestResolveConnectionTestAccountOverridesStillRequireTargetCreate(t *testing.T) {
	repository := &hostManagementTestRepository{}
	authorizer := &hostManagementSelectiveAuthorizer{allowedAction: rbac.ActionSessionConnect}
	svc, err := NewHostManagementService(repository, authorizer, hostManagementTestIdentityCollector{})
	if err != nil {
		t.Fatalf("NewHostManagementService: %v", err)
	}

	_, err = svc.ResolveConnectionTest(
		context.Background(),
		HostManagementActor{ID: "connect-only"},
		config.Target{ID: "account-1", Username: "override"},
	)
	if !errors.Is(err, ErrHostAccessDenied) {
		t.Fatalf("ResolveConnectionTest error = %v, want access denied", err)
	}
	if len(authorizer.calls) != 1 ||
		len(authorizer.calls[0]) != 1 ||
		authorizer.calls[0][0] != rbac.ActionTargetCreate {
		t.Fatalf("authorization calls = %#v, want target:create only", authorizer.calls)
	}
	if repository.targetCalls != 0 || repository.targetConfigCalls != 0 {
		t.Fatalf("unauthorized override loaded target data: view=%d config=%d", repository.targetCalls, repository.targetConfigCalls)
	}
}

func TestResolveConnectionTestRequiresGlobalConnectPermission(t *testing.T) {
	repository := &hostManagementTestRepository{
		target: HostManagementTargetView{ID: "account-1", HostID: "host-1", Protocol: "ssh"},
	}
	authorizer := hostManagementResourceOnlyAuthorizer{}
	svc, err := NewHostManagementService(repository, authorizer, hostManagementTestIdentityCollector{})
	if err != nil {
		t.Fatalf("NewHostManagementService: %v", err)
	}

	_, err = svc.ResolveConnectionTest(
		context.Background(),
		HostManagementActor{ID: "resource-only"},
		config.Target{ID: "account-1"},
	)
	if !errors.Is(err, ErrHostAccessDenied) {
		t.Fatalf("ResolveConnectionTest error = %v, want access denied", err)
	}
	if repository.targetConfigCalls != 0 || repository.hostCalls != 0 {
		t.Fatalf("missing global permission loaded secrets: config=%d host=%d", repository.targetConfigCalls, repository.hostCalls)
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
	return newHostManagementTestServiceWithCollector(t, repository, hostManagementTestIdentityCollector{})
}

func newHostManagementTestServiceWithCollector(t *testing.T, repository HostManagementRepository, collector HostIdentityCollector) *HostManagementService {
	t.Helper()
	svc, err := NewHostManagementService(repository, hostManagementAllowAuthorizer{}, collector)
	if err != nil {
		t.Fatalf("NewHostManagementService: %v", err)
	}
	return svc
}

type hostManagementTestIdentityCollector struct{}

func (hostManagementTestIdentityCollector) Collect(context.Context, string, int) (HostIdentity, error) {
	return HostIdentity{Fingerprint: "SHA256:test", KnownHosts: "host ssh-ed25519 dGVzdA=="}, nil
}

type hostManagementRecordingIdentityCollector struct {
	identity HostIdentity
	err      error
	calls    int
}

func (c *hostManagementRecordingIdentityCollector) Collect(context.Context, string, int) (HostIdentity, error) {
	c.calls++
	return c.identity, c.err
}

type hostManagementTestRepository struct {
	HostManagementRepository
	managedCreatorID  string
	managedRecord     HostManagementHostRecord
	updatedRecord     HostManagementHostRecord
	updateCalls       int
	refreshedIdentity HostManagementIdentityRefresh
	refreshCalls      int
	refreshErr        error
	targetConfig      HostManagementTargetConfig
	target            HostManagementTargetView
	host              HostManagementHostView
	targets           []HostManagementTargetView
	targetCalls       int
	targetConfigCalls int
	hostCalls         int
}

func (r *hostManagementTestRepository) CreateManagedHost(_ context.Context, record HostManagementHostRecord, creatorID string) (HostManagementHostView, error) {
	r.managedCreatorID = creatorID
	r.managedRecord = record
	return HostManagementHostView{ID: "created"}, nil
}

func (r *hostManagementTestRepository) UpdateHost(_ context.Context, _ string, record HostManagementHostRecord) (HostManagementHostView, error) {
	r.updateCalls++
	r.updatedRecord = record
	return HostManagementHostView{
		ID: record.ID, Name: record.Name, Address: record.Address, Port: record.Port,
		Protocol: record.Protocol, Status: record.Status,
		HostKeyFingerprint: record.HostKeyFingerprint, KnownHosts: record.KnownHosts,
	}, nil
}

func (r *hostManagementTestRepository) RefreshHostIdentity(_ context.Context, id string, refresh HostManagementIdentityRefresh) (HostManagementHostView, error) {
	r.refreshCalls++
	r.refreshedIdentity = refresh
	if r.refreshErr != nil {
		return HostManagementHostView{}, r.refreshErr
	}
	return HostManagementHostView{
		ID: id, Address: refresh.Address, Port: refresh.Port, Protocol: refresh.Protocol,
		Status: "active", IdentityStatus: "available",
		HostKeyFingerprint: refresh.Fingerprint, KnownHosts: refresh.KnownHosts,
	}, nil
}

func (r *hostManagementTestRepository) TargetConfig(context.Context, string) (HostManagementTargetConfig, error) {
	r.targetConfigCalls++
	return r.targetConfig, nil
}

func (r *hostManagementTestRepository) Target(context.Context, string) (HostManagementTargetView, error) {
	r.targetCalls++
	return r.target, nil
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

type hostManagementSelectiveAuthorizer struct {
	allowedAction string
	calls         [][]string
}

func (a *hostManagementSelectiveAuthorizer) AuthorizeConnection(_ context.Context, _ string, actions []string, _, _ string) (bool, error) {
	a.calls = append(a.calls, append([]string(nil), actions...))
	return containsAction(actions, a.allowedAction), nil
}

func (*hostManagementSelectiveAuthorizer) AuthorizeBatch(_ context.Context, _ string, requests []AuthorizationRequest) ([]AuthorizationDecision, error) {
	return make([]AuthorizationDecision, len(requests)), nil
}

func containsAction(actions []string, action string) bool {
	for _, candidate := range actions {
		if candidate == action {
			return true
		}
	}
	return false
}

type hostManagementResourceOnlyAuthorizer struct{}

func (hostManagementResourceOnlyAuthorizer) AuthorizeConnection(_ context.Context, _ string, _ []string, resourceType, resourceID string) (bool, error) {
	return resourceType != "" && resourceID != "", nil
}

func (hostManagementResourceOnlyAuthorizer) AuthorizeBatch(_ context.Context, _ string, requests []AuthorizationRequest) ([]AuthorizationDecision, error) {
	return make([]AuthorizationDecision, len(requests)), nil
}
