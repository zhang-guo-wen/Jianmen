package store

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/service"
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

func TestCreateManagedContainerEndpointRollsBackWhenCreatorGrantCannotBeCreated(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	repository := NewDBStore(db)
	_, err = repository.CreateManagedContainerEndpoint(context.Background(), service.ContainerEndpointRequest{ID: "endpoint-no-creator", Name: "endpoint", Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI, Address: "http://127.0.0.1:2375", Status: "active"}, "missing-user")
	if err == nil {
		t.Fatal("create endpoint with missing creator succeeded")
	}
	var endpoints, resources int64
	if err := db.Model(&model.ContainerEndpoint{}).Where("id = ?", "endpoint-no-creator").Count(&endpoints).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Resource{}).Where("type = ? AND resource_id = ?", model.ResourceTypeContainerEndpoint, "endpoint-no-creator").Count(&resources).Error; err != nil {
		t.Fatal(err)
	}
	if endpoints != 0 || resources != 0 {
		t.Fatalf("managed create left orphan endpoint=%d resource=%d", endpoints, resources)
	}
}

func TestUpdateManagedContainerEndpointInheritsOmittedFieldsInsideTransaction(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	host := model.Host{ID: "host-update", Name: "host", Address: "127.0.0.1", Port: 22, Status: "active"}
	account := model.HostAccount{ID: "account-update", HostID: host.ID, Name: "root", Username: "root", Status: "active"}
	endpoint := model.ContainerEndpoint{
		ID: "endpoint-update", Name: "old", GroupName: "ops", Runtime: model.ContainerRuntimeDocker,
		ConnectionMode: model.ContainerConnectionSSH, HostID: host.ID, HostAccountID: account.ID,
		Remark: "keep", Status: "disabled",
	}
	if err := db.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}

	got, err := NewDBStore(db).UpdateManagedContainerEndpoint(context.Background(), endpoint.ID, service.ContainerEndpointRequest{Name: "renamed"})
	if err != nil {
		t.Fatalf("partial managed update: %v", err)
	}
	if got.Name != "renamed" || got.Status != "disabled" || got.Runtime != model.ContainerRuntimeDocker ||
		got.ConnectionMode != model.ContainerConnectionSSH || got.HostID != host.ID ||
		got.HostAccountID != account.ID || got.Group != "ops" || got.Remark != "keep" {
		t.Fatalf("partial managed update = %#v", got)
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

func TestContainerEndpointReadAndCreateHonorCancelledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	repository := NewDBStore(db)

	if err := db.Create(&model.Host{
		ID: "host-cancel", Name: "cancel-host", Address: "127.0.0.1", Port: 22, Status: "active",
	}).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&model.HostAccount{
		ID: "account-cancel", HostID: "host-cancel", Name: "cancel-account", Username: "root", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create host account: %v", err)
	}
	if err := db.Create(&model.ContainerEndpoint{
		ID: "endpoint-existing", Name: "existing", Runtime: model.ContainerRuntimeDocker,
		ConnectionMode: model.ContainerConnectionDockerAPI, Address: "http://127.0.0.1:2375", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create container endpoint: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repository.ContainerEndpoint(ctx, "endpoint-existing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("container endpoint read with canceled context = %v, want context canceled", err)
	}

	if _, err := repository.AddContainerEndpoint(ctx, ContainerEndpointInput{
		ID:             "endpoint-cancelled-add",
		Name:           "cancelled",
		Runtime:        model.ContainerRuntimeDocker,
		ConnectionMode: model.ContainerConnectionDockerAPI,
		Address:        "http://127.0.0.1:2375",
		Status:         "active",
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("container endpoint add with canceled context = %v, want context canceled", err)
	}

	var count int64
	if err := db.Model(&model.ContainerEndpoint{}).Where("id = ?", "endpoint-cancelled-add").Count(&count).Error; err != nil {
		t.Fatalf("count cancelled add endpoint: %v", err)
	}
	if count != 0 {
		t.Fatalf("canceled context add should not persist record, got count=%d", count)
	}
}
