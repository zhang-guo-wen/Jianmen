package service

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
)

type containerManagementTestRepo struct {
	endpoint    ContainerEndpoint
	account     ContainerHostAccount
	configCalls int
	endpointErr error
}

func (r *containerManagementTestRepo) ListManagedContainerEndpoints(context.Context, string, string) ([]ContainerEndpoint, error) {
	return []ContainerEndpoint{r.endpoint}, nil
}
func (r *containerManagementTestRepo) ManagedContainerEndpoint(context.Context, string) (ContainerEndpoint, error) {
	return r.endpoint, r.endpointErr
}
func (r *containerManagementTestRepo) CreateManagedContainerEndpoint(context.Context, ContainerEndpointRequest, string) (ContainerEndpoint, error) {
	return r.endpoint, nil
}
func (r *containerManagementTestRepo) UpdateManagedContainerEndpoint(context.Context, string, ContainerEndpointRequest) (ContainerEndpoint, error) {
	return r.endpoint, nil
}
func (r *containerManagementTestRepo) DeleteManagedContainerEndpoint(context.Context, string) error {
	return nil
}
func (r *containerManagementTestRepo) ContainerHostAccount(context.Context, string) (ContainerHostAccount, error) {
	return r.account, nil
}
func (r *containerManagementTestRepo) ContainerHostAccountConfig(context.Context, string) (ContainerEndpointConfig, error) {
	r.configCalls++
	return ContainerEndpointConfig{}, nil
}

type containerManagementTestAuthorizer struct {
	allow bool
	err   error
}

func (a containerManagementTestAuthorizer) AuthorizeBatch(context.Context, string, []AuthorizationRequest) ([]AuthorizationDecision, error) {
	if a.err != nil {
		return nil, a.err
	}
	return []AuthorizationDecision{{Allowed: a.allow}}, nil
}

type containerManagementTestProtocol struct{}

func (containerManagementTestProtocol) Test(ctx context.Context, _ ContainerEndpointConfig) (ContainerTestResult, error) {
	<-ctx.Done()
	return ContainerTestResult{}, ctx.Err()
}
func (containerManagementTestProtocol) List(ctx context.Context, _ ContainerEndpointConfig) ([]ContainerRecord, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (containerManagementTestProtocol) Logs(ctx context.Context, _ ContainerEndpointConfig, _ string, _ int) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestContainerManagementDoesNotLoadHostCredentialsBeforeAuthorization(t *testing.T) {
	repo := &containerManagementTestRepo{account: ContainerHostAccount{ID: "account", HostID: "host"}}
	svc, err := NewContainerManagementService(repo, containerManagementTestAuthorizer{}, containerManagementTestProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Test(context.Background(), ContainerActor{UserID: "user"}, ContainerEndpointRequest{Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionSSH, HostID: "host", HostAccountID: "account"})
	if !errors.Is(err, ErrContainerForbidden) {
		t.Fatalf("test error = %v, want forbidden", err)
	}
	if repo.configCalls != 0 {
		t.Fatalf("credential config loaded %d times before authorization", repo.configCalls)
	}
}

func TestContainerManagementRuntimePropagatesCancellation(t *testing.T) {
	repo := &containerManagementTestRepo{endpoint: ContainerEndpoint{ID: "endpoint", Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI, Address: "http://127.0.0.1:2375", Status: "active"}}
	svc, err := NewContainerManagementService(repo, containerManagementTestAuthorizer{allow: true}, containerManagementTestProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.ListRuntime(ctx, ContainerActor{UserID: "user"}, "endpoint")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runtime cancellation error = %v, want context canceled", err)
	}
}

func TestContainerManagementRejectsUnavailableHostAccount(t *testing.T) {
	repo := &containerManagementTestRepo{account: ContainerHostAccount{ID: "account", HostID: "host", Unavailable: true}}
	svc, err := NewContainerManagementService(repo, containerManagementTestAuthorizer{allow: true}, containerManagementTestProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Test(context.Background(), ContainerActor{UserID: "user"}, ContainerEndpointRequest{Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionSSH, HostID: "host", HostAccountID: "account"})
	if !errors.Is(err, ErrContainerUnavailable) {
		t.Fatalf("unavailable host account error = %v", err)
	}
	if repo.configCalls != 0 {
		t.Fatalf("credential config loaded for unavailable account")
	}
}

func TestContainerManagementRejectsDisabledEndpointBeforeRuntimeProtocol(t *testing.T) {
	repo := &containerManagementTestRepo{endpoint: ContainerEndpoint{ID: "endpoint", Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI, Address: "http://127.0.0.1:2375", Status: "disabled"}}
	svc, err := NewContainerManagementService(repo, containerManagementTestAuthorizer{allow: true}, containerManagementTestProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ListRuntime(context.Background(), ContainerActor{UserID: "user"}, "endpoint")
	if !errors.Is(err, ErrContainerUnavailable) {
		t.Fatalf("disabled endpoint error = %v", err)
	}
}

func TestContainerManagementRepositoryErrorDoesNotReturnRuntimeSuccess(t *testing.T) {
	repo := &containerManagementTestRepo{endpointErr: errors.New("database unavailable")}
	svc, err := NewContainerManagementService(repo, containerManagementTestAuthorizer{allow: true}, containerManagementTestProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListRuntime(context.Background(), ContainerActor{UserID: "user"}, "endpoint")
	if err == nil || items != nil {
		t.Fatalf("repository error returned items=%#v err=%v", items, err)
	}
}
