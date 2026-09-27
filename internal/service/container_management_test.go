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
	lastUpdate  ContainerEndpointRequest
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
func (r *containerManagementTestRepo) UpdateManagedContainerEndpoint(_ context.Context, _ string, request ContainerEndpointRequest) (ContainerEndpoint, error) {
	r.lastUpdate = request
	r.endpoint = containerEndpointFromRequest(request)
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
	allow          bool
	err            error
	globalCalls    [][]string
	batchCalls     [][]AuthorizationRequest
	batchDecisions [][]AuthorizationDecision
	batchErrors    []error
}

func (a *containerManagementTestAuthorizer) AuthorizeConnection(_ context.Context, _ string, actions []string, resourceType, resourceID string) (bool, error) {
	if a.err != nil {
		return false, a.err
	}
	if resourceType != "" || resourceID != "" {
		return false, errors.New("strict global authorizer received a resource")
	}
	a.globalCalls = append(a.globalCalls, append([]string(nil), actions...))
	return a.allow, nil
}

func (a *containerManagementTestAuthorizer) AuthorizeBatch(_ context.Context, _ string, requests []AuthorizationRequest) ([]AuthorizationDecision, error) {
	for _, request := range requests {
		if request.ResourceType == "" || request.ResourceID == "" {
			return nil, errors.New("strict batch authorizer rejected an empty resource")
		}
	}
	a.batchCalls = append(a.batchCalls, append([]AuthorizationRequest(nil), requests...))
	index := len(a.batchCalls) - 1
	if index < len(a.batchErrors) && a.batchErrors[index] != nil {
		return nil, a.batchErrors[index]
	}
	if index < len(a.batchDecisions) {
		return a.batchDecisions[index], nil
	}
	decisions := make([]AuthorizationDecision, len(requests))
	for i := range decisions {
		decisions[i].Allowed = a.allow
	}
	return decisions, nil
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

type containerManagementSuccessProtocol struct{}

func (containerManagementSuccessProtocol) Test(context.Context, ContainerEndpointConfig) (ContainerTestResult, error) {
	return ContainerTestResult{OK: true}, nil
}
func (containerManagementSuccessProtocol) List(context.Context, ContainerEndpointConfig) ([]ContainerRecord, error) {
	return nil, nil
}
func (containerManagementSuccessProtocol) Logs(context.Context, ContainerEndpointConfig, string, int) (string, error) {
	return "", nil
}

func TestContainerManagementDoesNotLoadHostCredentialsBeforeAuthorization(t *testing.T) {
	repo := &containerManagementTestRepo{account: ContainerHostAccount{ID: "account", HostID: "host"}}
	svc, err := NewContainerManagementService(repo, &containerManagementTestAuthorizer{}, containerManagementTestProtocol{})
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
	svc, err := NewContainerManagementService(repo, &containerManagementTestAuthorizer{allow: true}, containerManagementTestProtocol{})
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
	svc, err := NewContainerManagementService(repo, &containerManagementTestAuthorizer{allow: true}, containerManagementTestProtocol{})
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
	svc, err := NewContainerManagementService(repo, &containerManagementTestAuthorizer{allow: true}, containerManagementTestProtocol{})
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
	svc, err := NewContainerManagementService(repo, &containerManagementTestAuthorizer{allow: true}, containerManagementTestProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListRuntime(context.Background(), ContainerActor{UserID: "user"}, "endpoint")
	if err == nil || items != nil {
		t.Fatalf("repository error returned items=%#v err=%v", items, err)
	}
}

func TestContainerManagementUsesGlobalAuthorizationForCreateAndUnsavedTest(t *testing.T) {
	authorizer := &containerManagementTestAuthorizer{allow: true}
	repo := &containerManagementTestRepo{endpoint: ContainerEndpoint{ID: "created"}}
	svc, err := NewContainerManagementService(repo, authorizer, containerManagementSuccessProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	request := ContainerEndpointRequest{Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI, Address: "http://127.0.0.1:2375"}
	if _, err := svc.Create(context.Background(), ContainerActor{UserID: "user"}, request); err != nil {
		t.Fatalf("create with global permission: %v", err)
	}
	result, err := svc.Test(context.Background(), ContainerActor{UserID: "user"}, request)
	if err != nil || !result.OK {
		t.Fatalf("test unsaved config with global permission: result=%#v err=%v", result, err)
	}
	if len(authorizer.globalCalls) != 2 {
		t.Fatalf("global authorization calls = %d, want 2", len(authorizer.globalCalls))
	}
}

func TestContainerManagementGlobalAuthorizationDeniesCreateAndUnsavedTest(t *testing.T) {
	authorizer := &containerManagementTestAuthorizer{}
	svc, err := NewContainerManagementService(&containerManagementTestRepo{}, authorizer, containerManagementSuccessProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	request := ContainerEndpointRequest{Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI, Address: "http://127.0.0.1:2375"}
	if _, err := svc.Create(context.Background(), ContainerActor{UserID: "user"}, request); !errors.Is(err, ErrContainerForbidden) {
		t.Fatalf("denied create error = %v", err)
	}
	if _, err := svc.Test(context.Background(), ContainerActor{UserID: "user"}, request); !errors.Is(err, ErrContainerForbidden) {
		t.Fatalf("denied unsaved test error = %v", err)
	}
}

func TestContainerManagementExistingEndpointTestRequiresEndpointUpdate(t *testing.T) {
	authorizer := &containerManagementTestAuthorizer{allow: true}
	repo := &containerManagementTestRepo{endpoint: ContainerEndpoint{ID: "endpoint", Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI, Address: "http://127.0.0.1:2375", Status: "active"}}
	svc, err := NewContainerManagementService(repo, authorizer, containerManagementSuccessProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Test(context.Background(), ContainerActor{UserID: "user"}, ContainerEndpointRequest{ID: "endpoint"}); err != nil {
		t.Fatalf("test saved endpoint: %v", err)
	}
	if len(authorizer.globalCalls) != 0 || len(authorizer.batchCalls) != 1 {
		t.Fatalf("saved endpoint authorization global=%d batch=%d", len(authorizer.globalCalls), len(authorizer.batchCalls))
	}
	request := authorizer.batchCalls[0][0]
	if request.ResourceType != model.ResourceTypeContainerEndpoint || request.ResourceID != "endpoint" || len(request.Actions) != 1 || request.Actions[0] != "container:update" {
		t.Fatalf("saved endpoint authorization = %#v", request)
	}
}

func TestContainerManagementUpdateInheritsDisabledStatusAndConnection(t *testing.T) {
	previous := ContainerEndpoint{ID: "endpoint", Name: "old", Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionSSH, HostID: "host", HostAccountID: "account", Status: "disabled"}
	repo := &containerManagementTestRepo{endpoint: previous, account: ContainerHostAccount{ID: "account", HostID: "host"}}
	svc, err := NewContainerManagementService(repo, &containerManagementTestAuthorizer{allow: true}, containerManagementSuccessProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(context.Background(), ContainerActor{UserID: "user"}, "endpoint", ContainerEndpointRequest{Name: "renamed"}); err != nil {
		t.Fatalf("partial update: %v", err)
	}
	got := repo.lastUpdate
	if got.Status != "disabled" || got.Runtime != model.ContainerRuntimeDocker || got.ConnectionMode != model.ContainerConnectionSSH || got.HostID != "host" || got.HostAccountID != "account" {
		t.Fatalf("partial update lost previous fields: %#v", got)
	}
}

func TestContainerManagementGetDistinguishesManageDenyFromAuthorizationFailure(t *testing.T) {
	repo := &containerManagementTestRepo{endpoint: ContainerEndpoint{ID: "endpoint"}}
	t.Run("deny", func(t *testing.T) {
		authorizer := &containerManagementTestAuthorizer{batchDecisions: [][]AuthorizationDecision{{{Allowed: true}}, {{Allowed: false}}}}
		svc, err := NewContainerManagementService(repo, authorizer, containerManagementSuccessProtocol{})
		if err != nil {
			t.Fatal(err)
		}
		item, err := svc.Get(context.Background(), ContainerActor{UserID: "user"}, "endpoint")
		if err != nil || item.CanManage {
			t.Fatalf("manage deny returned item=%#v err=%v", item, err)
		}
	})
	tests := []struct {
		name       string
		decisions  [][]AuthorizationDecision
		batchError []error
		want       error
	}{
		{name: "storage error", decisions: [][]AuthorizationDecision{{{Allowed: true}}}, batchError: []error{nil, errors.New("authorization store unavailable")}},
		{name: "context canceled", decisions: [][]AuthorizationDecision{{{Allowed: true}}}, batchError: []error{nil, context.Canceled}, want: context.Canceled},
		{name: "decision mismatch", decisions: [][]AuthorizationDecision{{{Allowed: true}}, {}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &containerManagementTestAuthorizer{batchDecisions: test.decisions, batchErrors: test.batchError}
			svc, err := NewContainerManagementService(repo, authorizer, containerManagementSuccessProtocol{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = svc.Get(context.Background(), ContainerActor{UserID: "user"}, "endpoint")
			if err == nil {
				t.Fatal("authorization failure was folded into CanManage=false")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("authorization error = %v, want %v", err, test.want)
			}
		})
	}
}

func containerEndpointFromRequest(request ContainerEndpointRequest) ContainerEndpoint {
	return ContainerEndpoint{ID: request.ID, Name: request.Name, Group: request.Group, Runtime: request.Runtime, ConnectionMode: request.ConnectionMode, Address: request.Address, Port: request.Port, HostID: request.HostID, HostAccountID: request.HostAccountID, Remark: request.Remark, Status: request.Status}
}
