package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

var (
	ErrContainerForbidden   = errors.New("container endpoint access forbidden")
	ErrContainerUnavailable = errors.New("container endpoint is unavailable")
	ErrInvalidContainer     = errors.New("invalid container endpoint")
	ErrContainerRuntime     = errors.New("container runtime operation failed")
)

type ContainerActor struct {
	UserID     string
	SuperAdmin bool
}

type ContainerEndpoint struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Group           string `json:"group"`
	Runtime         string `json:"runtime"`
	ConnectionMode  string `json:"connection_mode"`
	Address         string `json:"address"`
	Port            int    `json:"port"`
	HostID          string `json:"host_id"`
	HostName        string `json:"host_name"`
	HostAddress     string `json:"host_address"`
	HostGroup       string `json:"host_group"`
	HostRemark      string `json:"host_remark"`
	HostAccountID   string `json:"host_account_id"`
	HostAccountName string `json:"host_account_name"`
	Remark          string `json:"remark"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	CanManage       bool   `json:"can_manage"`
}

type ContainerEndpointRequest struct {
	ID, Name, Group, Runtime, ConnectionMode, Address, HostID, HostAccountID, Remark, Status string
	Port                                                                                     int
}

type ContainerListRequest struct {
	Page, PageSize int
	Query, Status  string
}
type ContainerPage struct {
	Items    []ContainerEndpoint `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}
type ContainerHostAccount struct {
	ID, HostID  string
	Unavailable bool
}

// ContainerManagementRepository is owned by the service consumer. It keeps
// credential-free relation checks separate from the sensitive SSH config load.
type ContainerManagementRepository interface {
	ListManagedContainerEndpoints(context.Context, string, string) ([]ContainerEndpoint, error)
	ManagedContainerEndpoint(context.Context, string) (ContainerEndpoint, error)
	CreateManagedContainerEndpoint(context.Context, ContainerEndpointRequest, string) (ContainerEndpoint, error)
	UpdateManagedContainerEndpoint(context.Context, string, ContainerEndpointRequest) (ContainerEndpoint, error)
	DeleteManagedContainerEndpoint(context.Context, string) error
	ContainerHostAccount(context.Context, string) (ContainerHostAccount, error)
	ContainerHostAccountConfig(context.Context, string) (ContainerEndpointConfig, error)
}

type ContainerAuthorizer interface {
	AuthorizeConnection(context.Context, string, []string, string, string) (bool, error)
	AuthorizeBatch(context.Context, string, []AuthorizationRequest) ([]AuthorizationDecision, error)
}

type ContainerProtocol interface {
	Test(context.Context, ContainerEndpointConfig) (ContainerTestResult, error)
	List(context.Context, ContainerEndpointConfig) ([]ContainerRecord, error)
	Logs(context.Context, ContainerEndpointConfig, string, int) (string, error)
}

type ContainerManagementService struct {
	repository                  ContainerManagementRepository
	authorizer                  ContainerAuthorizer
	protocol                    ContainerProtocol
	testTimeout, runtimeTimeout time.Duration
}

func NewContainerManagementService(repository ContainerManagementRepository, authorizer ContainerAuthorizer, protocol ContainerProtocol) (*ContainerManagementService, error) {
	if repository == nil {
		return nil, errors.New("container management repository is required")
	}
	if authorizer == nil {
		return nil, errors.New("container management authorizer is required")
	}
	if protocol == nil {
		return nil, errors.New("container protocol service is required")
	}
	return &ContainerManagementService{repository: repository, authorizer: authorizer, protocol: protocol, testTimeout: 15 * time.Second, runtimeTimeout: 20 * time.Second}, nil
}

func (s *ContainerManagementService) List(ctx context.Context, actor ContainerActor, request ContainerListRequest) (ContainerPage, error) {
	if strings.TrimSpace(actor.UserID) == "" {
		return ContainerPage{}, ErrContainerForbidden
	}
	items, err := s.repository.ListManagedContainerEndpoints(ctx, request.Query, request.Status)
	if err != nil {
		return ContainerPage{}, fmt.Errorf("list container endpoints: %w", err)
	}
	if !actor.SuperAdmin {
		reqs := make([]AuthorizationRequest, 0, len(items)*2)
		for _, item := range items {
			reqs = append(reqs, AuthorizationRequest{Actions: []string{rbac.ActionContainerView, rbac.ActionContainerConnect}, ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: item.ID}, AuthorizationRequest{Actions: []string{rbac.ActionContainerUpdate, rbac.ActionContainerDelete}, ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: item.ID})
		}
		decisions, authErr := s.authorizer.AuthorizeBatch(ctx, actor.UserID, reqs)
		if authErr != nil {
			return ContainerPage{}, fmt.Errorf("authorize container endpoint list: %w", authErr)
		}
		if len(decisions) != len(reqs) {
			return ContainerPage{}, errors.New("authorize container endpoint list: decision count mismatch")
		}
		visible := make([]ContainerEndpoint, 0, len(items))
		for i := range items {
			if decisions[i*2].Allowed {
				items[i].CanManage = decisions[i*2+1].Allowed
				visible = append(visible, items[i])
			}
		}
		items = visible
	} else {
		for i := range items {
			items[i].CanManage = true
		}
	}
	return paginateContainerEndpoints(items, request), nil
}

func (s *ContainerManagementService) Get(ctx context.Context, actor ContainerActor, id string) (ContainerEndpoint, error) {
	if err := s.authorize(ctx, actor, []string{rbac.ActionContainerView}, model.ResourceTypeContainerEndpoint, id); err != nil {
		return ContainerEndpoint{}, err
	}
	item, err := s.repository.ManagedContainerEndpoint(ctx, strings.TrimSpace(id))
	if err != nil {
		return ContainerEndpoint{}, fmt.Errorf("get container endpoint: %w", err)
	}
	if actor.SuperAdmin {
		item.CanManage = true
		return item, nil
	}
	canManage, err := s.authorizationAllowed(ctx, actor, []string{rbac.ActionContainerUpdate, rbac.ActionContainerDelete}, model.ResourceTypeContainerEndpoint, id)
	if err != nil {
		return ContainerEndpoint{}, err
	}
	item.CanManage = canManage
	return item, nil
}

func (s *ContainerManagementService) Create(ctx context.Context, actor ContainerActor, request ContainerEndpointRequest) (ContainerEndpoint, error) {
	if err := s.authorizeGlobal(ctx, actor, []string{rbac.ActionContainerCreate}); err != nil {
		return ContainerEndpoint{}, err
	}
	if err := validateContainerRequest(request); err != nil {
		return ContainerEndpoint{}, err
	}
	if err := s.validateHostAccount(ctx, actor, request); err != nil {
		return ContainerEndpoint{}, err
	}
	creatorID := actor.UserID
	if actor.SuperAdmin {
		creatorID = ""
	}
	item, err := s.repository.CreateManagedContainerEndpoint(ctx, request, creatorID)
	if err != nil {
		return ContainerEndpoint{}, fmt.Errorf("create container endpoint: %w", err)
	}
	item.CanManage = true
	return item, nil
}

func (s *ContainerManagementService) Update(ctx context.Context, actor ContainerActor, id string, request ContainerEndpointRequest) (ContainerEndpoint, error) {
	if err := s.authorize(ctx, actor, []string{rbac.ActionContainerUpdate}, model.ResourceTypeContainerEndpoint, id); err != nil {
		return ContainerEndpoint{}, err
	}
	previous, err := s.repository.ManagedContainerEndpoint(ctx, strings.TrimSpace(id))
	if err != nil {
		return ContainerEndpoint{}, fmt.Errorf("get container endpoint before update: %w", err)
	}
	effective := mergeContainerEndpointRequest(containerRequest(previous), request)
	if err := validateContainerRequest(effective); err != nil {
		return ContainerEndpoint{}, err
	}
	if err := s.validateHostAccount(ctx, actor, effective); err != nil {
		return ContainerEndpoint{}, err
	}
	item, err := s.repository.UpdateManagedContainerEndpoint(ctx, strings.TrimSpace(id), effective)
	if err != nil {
		return ContainerEndpoint{}, fmt.Errorf("update container endpoint: %w", err)
	}
	item.CanManage = true
	return item, nil
}

func (s *ContainerManagementService) Delete(ctx context.Context, actor ContainerActor, id string) error {
	if err := s.authorize(ctx, actor, []string{rbac.ActionContainerDelete}, model.ResourceTypeContainerEndpoint, id); err != nil {
		return err
	}
	if err := s.repository.DeleteManagedContainerEndpoint(ctx, strings.TrimSpace(id)); err != nil {
		return fmt.Errorf("delete container endpoint: %w", err)
	}
	return nil
}

func (s *ContainerManagementService) Test(ctx context.Context, actor ContainerActor, request ContainerEndpointRequest) (ContainerTestResult, error) {
	if endpointID := strings.TrimSpace(request.ID); endpointID != "" {
		if err := s.authorize(ctx, actor, []string{rbac.ActionContainerUpdate}, model.ResourceTypeContainerEndpoint, endpointID); err != nil {
			return ContainerTestResult{}, err
		}
		previous, err := s.repository.ManagedContainerEndpoint(ctx, endpointID)
		if err != nil {
			return ContainerTestResult{}, fmt.Errorf("get container endpoint before test: %w", err)
		}
		request = mergeContainerEndpointRequest(containerRequest(previous), request)
	} else if err := s.authorizeGlobal(ctx, actor, []string{rbac.ActionContainerCreate, rbac.ActionContainerUpdate}); err != nil {
		return ContainerTestResult{}, err
	}
	config, err := s.endpointConfig(ctx, actor, request, false)
	if err != nil {
		return ContainerTestResult{}, err
	}
	timed, cancel := context.WithTimeout(ctx, s.testTimeout)
	defer cancel()
	result, err := s.protocol.Test(timed, config)
	if err != nil {
		return ContainerTestResult{}, fmt.Errorf("test container endpoint: %w", err)
	}
	return result, nil
}

func (s *ContainerManagementService) ListRuntime(ctx context.Context, actor ContainerActor, id string) ([]ContainerRecord, error) {
	config, err := s.runtimeConfig(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	timed, cancel := context.WithTimeout(ctx, s.runtimeTimeout)
	defer cancel()
	items, err := s.protocol.List(timed, config)
	if err != nil {
		return nil, fmt.Errorf("%w: list runtime containers: %w", ErrContainerRuntime, err)
	}
	return items, nil
}

func (s *ContainerManagementService) Logs(ctx context.Context, actor ContainerActor, endpointID, containerID string, tail int) (string, error) {
	config, err := s.runtimeConfig(ctx, actor, endpointID)
	if err != nil {
		return "", err
	}
	timed, cancel := context.WithTimeout(ctx, s.runtimeTimeout)
	defer cancel()
	logs, err := s.protocol.Logs(timed, config, containerID, tail)
	if err != nil {
		return "", fmt.Errorf("%w: read container logs: %w", ErrContainerRuntime, err)
	}
	return logs, nil
}

func (s *ContainerManagementService) runtimeConfig(ctx context.Context, actor ContainerActor, id string) (ContainerEndpointConfig, error) {
	if err := s.authorize(ctx, actor, []string{rbac.ActionContainerConnect}, model.ResourceTypeContainerEndpoint, id); err != nil {
		return ContainerEndpointConfig{}, err
	}
	endpoint, err := s.repository.ManagedContainerEndpoint(ctx, strings.TrimSpace(id))
	if err != nil {
		return ContainerEndpointConfig{}, fmt.Errorf("get container endpoint for runtime: %w", err)
	}
	if endpoint.Status != "active" {
		return ContainerEndpointConfig{}, ErrContainerUnavailable
	}
	return s.endpointConfig(ctx, actor, containerRequest(endpoint), true)
}

func (s *ContainerManagementService) endpointConfig(ctx context.Context, actor ContainerActor, request ContainerEndpointRequest, requireActive bool) (ContainerEndpointConfig, error) {
	if err := validateContainerRequest(request); err != nil {
		return ContainerEndpointConfig{}, err
	}
	config := ContainerEndpointConfig{Runtime: request.Runtime, ConnectionMode: request.ConnectionMode, Address: request.Address, Port: request.Port}
	if request.ConnectionMode != model.ContainerConnectionSSH && request.ConnectionMode != model.ContainerConnectionContainerd {
		return config, nil
	}
	if err := s.validateHostAccount(ctx, actor, request); err != nil {
		return ContainerEndpointConfig{}, err
	}
	accountConfig, err := s.repository.ContainerHostAccountConfig(ctx, request.HostAccountID)
	if err != nil {
		return ContainerEndpointConfig{}, fmt.Errorf("load container host account config: %w", err)
	}
	if accountConfig.Unavailable && requireActive {
		return ContainerEndpointConfig{}, ErrContainerUnavailable
	}
	accountConfig.Runtime, accountConfig.ConnectionMode, accountConfig.Address, accountConfig.Port = request.Runtime, request.ConnectionMode, request.Address, request.Port
	return accountConfig, nil
}

func (s *ContainerManagementService) validateHostAccount(ctx context.Context, actor ContainerActor, request ContainerEndpointRequest) error {
	hostID, accountID := strings.TrimSpace(request.HostID), strings.TrimSpace(request.HostAccountID)
	if hostID == "" && accountID == "" {
		if request.ConnectionMode == model.ContainerConnectionSSH || request.ConnectionMode == model.ContainerConnectionContainerd {
			return fmt.Errorf("%w: ssh connection requires a host account", ErrInvalidContainer)
		}
		return nil
	}
	if hostID == "" || accountID == "" {
		return fmt.Errorf("%w: host_id and host_account_id must be provided together", ErrInvalidContainer)
	}
	if err := s.authorize(ctx, actor, []string{rbac.ActionSessionConnect}, model.ResourceTypeHostAccount, accountID); err != nil {
		return err
	}
	account, err := s.repository.ContainerHostAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("load container host account: %w", err)
	}
	if strings.TrimSpace(account.HostID) != hostID {
		return fmt.Errorf("%w: host account %q does not belong to host %q", ErrInvalidContainer, accountID, hostID)
	}
	if account.Unavailable {
		return ErrContainerUnavailable
	}
	return nil
}

func (s *ContainerManagementService) authorize(ctx context.Context, actor ContainerActor, actions []string, resourceType, resourceID string) error {
	allowed, err := s.authorizationAllowed(ctx, actor, actions, resourceType, resourceID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrContainerForbidden
	}
	return nil
}

func (s *ContainerManagementService) authorizeGlobal(ctx context.Context, actor ContainerActor, actions []string) error {
	if strings.TrimSpace(actor.UserID) == "" {
		return ErrContainerForbidden
	}
	if actor.SuperAdmin {
		return nil
	}
	allowed, err := s.authorizer.AuthorizeConnection(ctx, strings.TrimSpace(actor.UserID), actions, "", "")
	if err != nil {
		return fmt.Errorf("authorize global container action: %w", err)
	}
	if !allowed {
		return ErrContainerForbidden
	}
	return nil
}

func (s *ContainerManagementService) authorizationAllowed(ctx context.Context, actor ContainerActor, actions []string, resourceType, resourceID string) (bool, error) {
	if strings.TrimSpace(actor.UserID) == "" {
		return false, nil
	}
	if actor.SuperAdmin {
		return true, nil
	}
	decisions, err := s.authorizer.AuthorizeBatch(ctx, actor.UserID, []AuthorizationRequest{{Actions: actions, ResourceType: resourceType, ResourceID: resourceID}})
	if err != nil {
		return false, fmt.Errorf("authorize container endpoint: %w", err)
	}
	if len(decisions) != 1 {
		return false, errors.New("authorize container endpoint: decision count mismatch")
	}
	return decisions[0].Allowed, nil
}

func validateContainerRequest(request ContainerEndpointRequest) error {
	request.Runtime, request.ConnectionMode, request.Address = strings.TrimSpace(request.Runtime), strings.TrimSpace(request.ConnectionMode), strings.TrimSpace(request.Address)
	if request.Runtime != model.ContainerRuntimeDocker && request.Runtime != model.ContainerRuntimeContainerd {
		return fmt.Errorf("%w: runtime must be docker or containerd", ErrInvalidContainer)
	}
	if request.ConnectionMode != model.ContainerConnectionSSH && request.ConnectionMode != model.ContainerConnectionDockerAPI && request.ConnectionMode != model.ContainerConnectionContainerd {
		return fmt.Errorf("%w: unsupported container connection mode", ErrInvalidContainer)
	}
	if request.Runtime == model.ContainerRuntimeDocker && request.ConnectionMode == model.ContainerConnectionContainerd {
		return fmt.Errorf("%w: docker runtime cannot use containerd connection", ErrInvalidContainer)
	}
	if request.Runtime == model.ContainerRuntimeContainerd && request.ConnectionMode == model.ContainerConnectionDockerAPI {
		return fmt.Errorf("%w: containerd runtime cannot use docker api connection", ErrInvalidContainer)
	}
	if request.ConnectionMode == model.ContainerConnectionDockerAPI && request.Address == "" {
		return fmt.Errorf("%w: docker api address is required", ErrInvalidContainer)
	}
	return nil
}

func containerRequest(endpoint ContainerEndpoint) ContainerEndpointRequest {
	return ContainerEndpointRequest{ID: endpoint.ID, Name: endpoint.Name, Group: endpoint.Group, Runtime: endpoint.Runtime, ConnectionMode: endpoint.ConnectionMode, Address: endpoint.Address, Port: endpoint.Port, HostID: endpoint.HostID, HostAccountID: endpoint.HostAccountID, Remark: endpoint.Remark, Status: endpoint.Status}
}

func mergeContainerEndpointRequest(previous, update ContainerEndpointRequest) ContainerEndpointRequest {
	update.ID = previous.ID
	if strings.TrimSpace(update.Name) == "" {
		update.Name = previous.Name
	}
	if strings.TrimSpace(update.Group) == "" {
		update.Group = previous.Group
	}
	if strings.TrimSpace(update.Runtime) == "" {
		update.Runtime = previous.Runtime
	}
	if strings.TrimSpace(update.ConnectionMode) == "" {
		update.ConnectionMode = previous.ConnectionMode
	}
	if strings.TrimSpace(update.Address) == "" {
		update.Address = previous.Address
	}
	if update.Port == 0 {
		update.Port = previous.Port
	}
	if strings.TrimSpace(update.HostID) == "" {
		update.HostID = previous.HostID
	}
	if strings.TrimSpace(update.HostAccountID) == "" {
		update.HostAccountID = previous.HostAccountID
	}
	if strings.TrimSpace(update.Remark) == "" {
		update.Remark = previous.Remark
	}
	if strings.TrimSpace(update.Status) == "" {
		update.Status = previous.Status
	}
	return update
}
func paginateContainerEndpoints(items []ContainerEndpoint, request ContainerListRequest) ContainerPage {
	page, size := request.Page, request.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	start := (page - 1) * size
	if start > len(items) {
		start = len(items)
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return ContainerPage{Items: items[start:end], Total: len(items), Page: page, PageSize: size}
}
