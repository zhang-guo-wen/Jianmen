package store

import (
	"context"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestNormalizeContainerEndpointInputAllowsSSHWithoutAddress(t *testing.T) {
	input, err := normalizeContainerEndpointInput(ContainerEndpointInput{
		Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionSSH,
		HostID: "host-1", HostAccountID: "account-1",
	})
	if err != nil {
		t.Fatalf("normalize SSH endpoint: %v", err)
	}
	if input.Address != "" {
		t.Fatalf("SSH address = %q, want empty", input.Address)
	}
}

func TestNormalizeContainerEndpointInputRequiresSSHHostAndAccount(t *testing.T) {
	_, err := normalizeContainerEndpointInput(ContainerEndpointInput{
		Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionSSH,
	})
	if err == nil {
		t.Fatal("SSH endpoint without host and account was accepted")
	}
	_, err = normalizeContainerEndpointInput(ContainerEndpointInput{
		Runtime: model.ContainerRuntimeContainerd, ConnectionMode: model.ContainerConnectionContainerd,
		HostID: "host-1",
	})
	if err == nil {
		t.Fatal("containerd endpoint without host account was accepted")
	}
}

func TestNormalizeContainerEndpointInputRequiresDockerAPIAddress(t *testing.T) {
	_, err := normalizeContainerEndpointInput(ContainerEndpointInput{
		Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI,
	})
	if err == nil {
		t.Fatal("Docker API endpoint without address was accepted")
	}
}

func TestListContainerEndpointsPaginatesAndIncludesHostMetadata(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	host := model.Host{
		ID: "host-1", Name: "prod-node", Address: "10.0.0.8", Port: 22,
		GroupName: "production", Remark: "payments cluster", Status: "active",
	}
	account := model.HostAccount{
		ID: "account-1", HostID: host.ID, Name: "ops", Username: "root", Status: "active",
	}
	endpoints := []model.ContainerEndpoint{
		{ID: "endpoint-active", Name: "docker-prod", Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionSSH, HostID: host.ID, HostAccountID: account.ID, Status: "active"},
		{ID: "endpoint-disabled", Name: "docker-disabled", Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionSSH, HostID: host.ID, HostAccountID: account.ID, Status: "disabled"},
	}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := db.Create(&endpoints).Error; err != nil {
		t.Fatalf("create endpoints: %v", err)
	}

	items, total, err := NewDBStore(db).ListContainerEndpoints(context.Background(), ContainerEndpointListParams{
		Page: 1, Size: 20, Query: "10.0.0.8", Status: "active",
	})
	if err != nil {
		t.Fatalf("list container endpoints: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("container endpoints = total:%d items:%#v", total, items)
	}
	got := items[0]
	if got.ID != "endpoint-active" || got.HostName != host.Name || got.HostAddress != host.Address || got.HostGroup != host.GroupName || got.HostRemark != host.Remark || got.HostAccountName != account.Name {
		t.Fatalf("container endpoint metadata = %#v", got)
	}
}
