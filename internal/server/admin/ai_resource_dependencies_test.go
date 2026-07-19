package admin

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/service"
	"jianmen/internal/store"
)

type aiResourceHostSourceStub struct {
	adminHostTargetRepository
	targets      []store.TargetView
	hosts        []store.HostView
	targetsErr   error
	hostsErr     error
	targetsCalls int
	hostsCalls   int
}

func (s *aiResourceHostSourceStub) Targets(context.Context) ([]store.TargetView, error) {
	s.targetsCalls++
	return append([]store.TargetView(nil), s.targets...), s.targetsErr
}

func (s *aiResourceHostSourceStub) Hosts(context.Context) ([]store.HostView, error) {
	s.hostsCalls++
	return append([]store.HostView(nil), s.hosts...), s.hostsErr
}

type aiResourceDatabaseSourceStub struct {
	adminDatabaseRepository
	accounts       []store.DatabaseAccountView
	instances      []store.DatabaseInstanceView
	accountsErr    error
	instancesErr   error
	accountsCalls  int
	instancesCalls int
}

func (s *aiResourceDatabaseSourceStub) DatabaseAccounts(context.Context) ([]store.DatabaseAccountView, error) {
	s.accountsCalls++
	return append([]store.DatabaseAccountView(nil), s.accounts...), s.accountsErr
}

func (s *aiResourceDatabaseSourceStub) ListDatabaseInstances(context.Context) ([]store.DatabaseInstanceView, error) {
	s.instancesCalls++
	return append([]store.DatabaseInstanceView(nil), s.instances...), s.instancesErr
}

func TestAIResourceRepositoryAdapterBatchesParentMetadata(t *testing.T) {
	hosts := &aiResourceHostSourceStub{
		targets: []store.TargetView{
			{ID: "ha-1", HostID: "h-1"},
			{ID: "ha-2", HostID: "h-2"},
		},
		hosts: []store.HostView{
			{ID: "h-1", Status: "active"},
			{ID: "h-2", Status: "disabled"},
		},
	}
	databases := &aiResourceDatabaseSourceStub{
		accounts: []store.DatabaseAccountView{
			{ID: "da-1", InstanceID: "db-1"},
			{ID: "da-2", InstanceID: "db-2"},
		},
		instances: []store.DatabaseInstanceView{
			{ID: "db-1", Status: "active"},
			{ID: "db-2", Status: "disabled"},
		},
	}
	adapter := aiResourceRepositoryAdapter{hostTargets: hosts, databases: databases}

	hostMetadata, err := adapter.ListHostAccounts(context.Background())
	if err != nil {
		t.Fatalf("list host metadata: %v", err)
	}
	databaseMetadata, err := adapter.ListDatabaseAccounts(context.Background())
	if err != nil {
		t.Fatalf("list database metadata: %v", err)
	}
	if hosts.targetsCalls != 1 || hosts.hostsCalls != 1 {
		t.Fatalf("host source calls = targets %d, hosts %d", hosts.targetsCalls, hosts.hostsCalls)
	}
	if databases.accountsCalls != 1 || databases.instancesCalls != 1 {
		t.Fatalf("database source calls = accounts %d, instances %d", databases.accountsCalls, databases.instancesCalls)
	}
	if len(hostMetadata) != 2 || hostMetadata[0].ParentStatus != "active" || hostMetadata[1].ParentStatus != "disabled" {
		t.Fatalf("host parent metadata = %#v", hostMetadata)
	}
	if len(databaseMetadata) != 2 || databaseMetadata[0].ParentStatus != "active" || databaseMetadata[1].ParentStatus != "disabled" {
		t.Fatalf("database parent metadata = %#v", databaseMetadata)
	}
}

func TestAIResourceRepositoryAdapterPreservesParentLookupErrors(t *testing.T) {
	parentErr := errors.New("parent lookup unavailable")
	hosts := &aiResourceHostSourceStub{
		targets:  []store.TargetView{{ID: "ha-1", HostID: "h-1"}},
		hostsErr: parentErr,
	}
	databases := &aiResourceDatabaseSourceStub{
		accounts:     []store.DatabaseAccountView{{ID: "da-1", InstanceID: "db-1"}},
		instancesErr: parentErr,
	}
	adapter := aiResourceRepositoryAdapter{hostTargets: hosts, databases: databases}

	if metadata, err := adapter.ListHostAccounts(context.Background()); metadata != nil || !errors.Is(err, parentErr) {
		t.Fatalf("host metadata = %#v, error = %v", metadata, err)
	}
	if metadata, err := adapter.ListDatabaseAccounts(context.Background()); metadata != nil || !errors.Is(err, parentErr) {
		t.Fatalf("database metadata = %#v, error = %v", metadata, err)
	}
}

var (
	_ service.AIResourceRepository     = aiResourceRepositoryAdapter{}
	_ service.AIResourceAuthorizer     = aiResourceAuthorizerAdapter{}
	_ service.AIResourceSessionCreator = aiResourceSessionCreatorAdapter{}
)
