package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func (s *DBStore) ListContainerEndpoints(ctx context.Context, params ContainerEndpointListParams) ([]ContainerEndpointView, int64, error) {
	page, size := params.Page, params.Size
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}

	buildQuery := func() *gorm.DB {
		query := s.db.WithContext(ctx).Model(&model.ContainerEndpoint{}).
			Where("container_endpoints.deleted_at = ?", model.DeletedMarkerActive).
			Joins("LEFT JOIN hosts ON hosts.id = container_endpoints.host_id")
		if status := strings.TrimSpace(params.Status); status != "" {
			query = query.Where("container_endpoints.status = ?", status)
		}
		if keyword := strings.ToLower(strings.TrimSpace(params.Query)); keyword != "" {
			like := "%" + keyword + "%"
			query = query.Where(`(
				LOWER(container_endpoints.name) LIKE ? OR
				LOWER(container_endpoints.runtime) LIKE ? OR
				LOWER(container_endpoints.address) LIKE ? OR
				LOWER(container_endpoints.group_name) LIKE ? OR
				LOWER(container_endpoints.remark) LIKE ? OR
				LOWER(hosts.name) LIKE ? OR
				LOWER(hosts.address) LIKE ? OR
				LOWER(hosts.group_name) LIKE ? OR
				LOWER(hosts.remark) LIKE ?)`,
				like, like, like, like, like, like, like, like, like,
			)
		}
		return query
	}

	var total int64
	if err := buildQuery().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count container endpoints: %w", err)
	}
	var endpoints []model.ContainerEndpoint
	if err := buildQuery().Select("container_endpoints.*").
		Order("container_endpoints.created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&endpoints).Error; err != nil {
		return nil, 0, fmt.Errorf("list container endpoints: %w", err)
	}
	views, err := s.containerEndpointViews(ctx, endpoints)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

// The managed variants are the narrow persistence boundary used by
// ContainerManagementService. Creation keeps the endpoint, resource and
// creator grant in one metadata-database transaction.
func (s *DBStore) ListManagedContainerEndpoints(ctx context.Context, query, status string) ([]service.ContainerEndpoint, error) {
	params := ContainerEndpointListParams{Page: 1, Size: 200, Query: query, Status: status}
	items := make([]service.ContainerEndpoint, 0)
	for {
		page, total, err := s.ListContainerEndpoints(ctx, params)
		if err != nil {
			return nil, err
		}
		for _, item := range page {
			items = append(items, managedContainerEndpoint(item))
		}
		if int64(len(items)) >= total || len(page) == 0 {
			return items, nil
		}
		params.Page++
	}
}

func (s *DBStore) ManagedContainerEndpoint(ctx context.Context, id string) (service.ContainerEndpoint, error) {
	view, err := s.ContainerEndpoint(ctx, id)
	if err != nil {
		return service.ContainerEndpoint{}, err
	}
	return managedContainerEndpoint(view), nil
}

func (s *DBStore) CreateManagedContainerEndpoint(ctx context.Context, input service.ContainerEndpointRequest, creatorID string) (service.ContainerEndpoint, error) {
	storeInput := containerInputFromManaged(input)
	// 当用户未指定名称且非 Docker API 模式时，根据主机信息生成默认名称
	if strings.TrimSpace(storeInput.Name) == "" && strings.TrimSpace(storeInput.Address) == "" && storeInput.HostID != "" {
		var host model.Host
		if err := s.db.WithContext(ctx).Scopes(ActiveScope).First(&host, "id = ?", storeInput.HostID).Error; err == nil {
			if host.Name != "" {
				storeInput.Name = host.Name
			} else if host.Address != "" {
				storeInput.Name = fmt.Sprintf("%s:%d", host.Address, host.Port)
			}
		}
	}
	normalized, err := normalizeContainerEndpointInput(storeInput)
	if err != nil {
		return service.ContainerEndpoint{}, err
	}
	endpoint := model.ContainerEndpoint{ID: normalized.ID, Name: normalized.Name, GroupName: normalized.Group, Runtime: normalized.Runtime, ConnectionMode: normalized.ConnectionMode, Address: normalized.Address, Port: normalized.Port, HostID: normalized.HostID, HostAccountID: normalized.HostAccountID, Remark: normalized.Remark, Status: normalized.Status}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&endpoint).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, normalized.Group); err != nil {
			return err
		}
		if err := s.syncResourceTx(tx, model.ResourceTypeContainerEndpoint, endpoint.ID, endpoint.Name, ""); err != nil {
			return err
		}
		return s.createContainerCreatorGrant(tx, creatorID, endpoint.ID)
	}); err != nil {
		return service.ContainerEndpoint{}, fmt.Errorf("create managed container endpoint: %w", err)
	}
	return s.ManagedContainerEndpoint(ctx, endpoint.ID)
}

func (s *DBStore) UpdateManagedContainerEndpoint(ctx context.Context, id string, input service.ContainerEndpointRequest) (service.ContainerEndpoint, error) {
	view, err := s.UpdateContainerEndpoint(ctx, id, containerInputFromManaged(input))
	if err != nil {
		return service.ContainerEndpoint{}, err
	}
	return managedContainerEndpoint(view), nil
}

func (s *DBStore) DeleteManagedContainerEndpoint(ctx context.Context, id string) error {
	return s.DeleteContainerEndpoint(ctx, id)
}

func (s *DBStore) ContainerHostAccount(ctx context.Context, id string) (service.ContainerHostAccount, error) {
	var account model.HostAccount
	if err := s.db.WithContext(ctx).Scopes(activeHostAccountScope).Preload("Host").
		First(&account, "host_accounts.id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return service.ContainerHostAccount{}, err
		}
		return service.ContainerHostAccount{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	unavailable := account.Status == "disabled" || account.Host.Status == "disabled" || (account.ExpiresAt != nil && !account.ExpiresAt.After(time.Now().UTC()))
	return service.ContainerHostAccount{ID: account.ID, HostID: account.HostID, Unavailable: unavailable}, nil
}

func (s *DBStore) ContainerHostAccountConfig(ctx context.Context, id string) (service.ContainerEndpointConfig, error) {
	target, err := s.TargetConfig(ctx, id)
	if err != nil {
		return service.ContainerEndpointConfig{}, err
	}
	sshConfig, err := ClientConfigForTarget(target)
	if err != nil {
		return service.ContainerEndpointConfig{}, err
	}
	return service.ContainerEndpointConfig{SSHAddress: target.Addr(), SSHConfig: sshConfig, SSHCacheKey: targetSSHCacheKey(target), Unavailable: target.Disabled || target.Expired(time.Now().UTC())}, nil
}

func (s *DBStore) createContainerCreatorGrant(tx *gorm.DB, creatorID, endpointID string) error {
	creatorID = strings.TrimSpace(creatorID)
	if creatorID == "" {
		return nil
	}
	var count int64
	if err := tx.Model(&model.User{}).Scopes(ActiveScope).Where("id = ?", creatorID).Count(&count).Error; err != nil {
		return fmt.Errorf("check container endpoint creator: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("container endpoint creator not found: %q", creatorID)
	}
	grant := model.ResourceGrant{PrincipalType: "user", PrincipalID: creatorID, ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: endpointID, Effect: model.PermissionEffectAllow}
	if err := tx.Where(&model.ResourceGrant{
		PrincipalType: grant.PrincipalType,
		PrincipalID:   grant.PrincipalID,
		ResourceType:  grant.ResourceType,
		ResourceID:    grant.ResourceID,
		Effect:        grant.Effect,
	}).FirstOrCreate(&grant).Error; err != nil {
		return fmt.Errorf("create container endpoint creator grant: %w", err)
	}
	return nil
}

func managedContainerEndpoint(view ContainerEndpointView) service.ContainerEndpoint {
	return service.ContainerEndpoint{ID: view.ID, Name: view.Name, Group: view.Group, Runtime: view.Runtime, ConnectionMode: view.ConnectionMode, Address: view.Address, Port: view.Port, HostID: view.HostID, HostName: view.HostName, HostAddress: view.HostAddress, HostGroup: view.HostGroup, HostRemark: view.HostRemark, HostAccountID: view.HostAccountID, HostAccountName: view.HostAccountName, Remark: view.Remark, Status: view.Status, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt, CanManage: view.CanManage}
}

func containerInputFromManaged(input service.ContainerEndpointRequest) ContainerEndpointInput {
	return ContainerEndpointInput{ID: input.ID, Name: input.Name, Group: input.Group, Runtime: input.Runtime, ConnectionMode: input.ConnectionMode, Address: input.Address, Port: input.Port, HostID: input.HostID, HostAccountID: input.HostAccountID, Remark: input.Remark, Status: input.Status}
}

func (s *DBStore) ContainerEndpoint(ctx context.Context, id string) (ContainerEndpointView, error) {
	var endpoint model.ContainerEndpoint
	if err := s.db.WithContext(ctx).First(&endpoint, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ContainerEndpointView{}, err
		}
		return ContainerEndpointView{}, fmt.Errorf("%w: %q", ErrContainerEndpointNotFound, id)
	}
	views, err := s.containerEndpointViews(ctx, []model.ContainerEndpoint{endpoint})
	if err != nil {
		return ContainerEndpointView{}, err
	}
	return views[0], nil
}

func (s *DBStore) AddContainerEndpoint(ctx context.Context, input ContainerEndpointInput) (ContainerEndpointView, error) {
	normalized, err := normalizeContainerEndpointInput(input)
	if err != nil {
		return ContainerEndpointView{}, err
	}
	endpoint := model.ContainerEndpoint{
		ID: normalized.ID, Name: normalized.Name, GroupName: normalized.Group,
		Runtime: normalized.Runtime, ConnectionMode: normalized.ConnectionMode,
		Address: normalized.Address, Port: normalized.Port, HostID: normalized.HostID,
		HostAccountID: normalized.HostAccountID, Remark: normalized.Remark, Status: normalized.Status,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&endpoint).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, normalized.Group); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeContainerEndpoint, endpoint.ID, endpoint.Name, "")
	}); err != nil {
		return ContainerEndpointView{}, fmt.Errorf("create container endpoint: %w", err)
	}
	views, err := s.containerEndpointViews(ctx, []model.ContainerEndpoint{endpoint})
	if err != nil {
		return ContainerEndpointView{}, err
	}
	return views[0], nil
}

func (s *DBStore) UpdateContainerEndpoint(ctx context.Context, id string, input ContainerEndpointInput) (ContainerEndpointView, error) {
	var endpoint model.ContainerEndpoint
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, "id = ?", strings.TrimSpace(id)).Error; err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("find container endpoint for update: %w", err)
			}
			return fmt.Errorf("%w: %q", ErrContainerEndpointNotFound, id)
		}
		normalized, err := normalizeContainerEndpointInput(mergeContainerEndpointUpdate(endpoint, input))
		if err != nil {
			return err
		}
		endpoint.Name, endpoint.GroupName = normalized.Name, normalized.Group
		endpoint.Runtime, endpoint.ConnectionMode = normalized.Runtime, normalized.ConnectionMode
		endpoint.Address, endpoint.Port = normalized.Address, normalized.Port
		endpoint.HostID, endpoint.HostAccountID = normalized.HostID, normalized.HostAccountID
		endpoint.Remark, endpoint.Status = normalized.Remark, normalized.Status
		if err := tx.Save(&endpoint).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, endpoint.GroupName); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeContainerEndpoint, endpoint.ID, endpoint.Name, "")
	}); err != nil {
		return ContainerEndpointView{}, fmt.Errorf("update container endpoint: %w", err)
	}
	views, err := s.containerEndpointViews(ctx, []model.ContainerEndpoint{endpoint})
	if err != nil {
		return ContainerEndpointView{}, err
	}
	return views[0], nil
}

// The current JSON contract cannot distinguish an omitted string from an
// explicitly cleared string. Updates therefore preserve blank fields until the
// API gains presence-aware inputs.
func mergeContainerEndpointUpdate(previous model.ContainerEndpoint, update ContainerEndpointInput) ContainerEndpointInput {
	if strings.TrimSpace(update.Name) == "" {
		update.Name = previous.Name
	}
	if strings.TrimSpace(update.Group) == "" {
		update.Group = previous.GroupName
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

func (s *DBStore) DeleteContainerEndpoint(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var endpoint model.ContainerEndpoint
		if err := tx.First(&endpoint, "id = ?", strings.TrimSpace(id)).Error; err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("find container endpoint for delete: %w", err)
			}
			return fmt.Errorf("%w: %q", ErrContainerEndpointNotFound, id)
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeContainerEndpoint, endpoint.ID); err != nil {
			return err
		}
		return SoftDelete(ctx, tx, "container_endpoints", id)
	})
}

func (s *DBStore) containerEndpointViews(ctx context.Context, endpoints []model.ContainerEndpoint) ([]ContainerEndpointView, error) {
	hostIDs := make([]string, 0, len(endpoints))
	accountIDs := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.HostID != "" {
			hostIDs = append(hostIDs, endpoint.HostID)
		}
		if endpoint.HostAccountID != "" {
			accountIDs = append(accountIDs, endpoint.HostAccountID)
		}
	}

	hosts := make(map[string]model.Host, len(hostIDs))
	if len(hostIDs) > 0 {
		var records []model.Host
		if err := s.db.WithContext(ctx).Where("id IN ?", hostIDs).Find(&records).Error; err != nil {
			return nil, fmt.Errorf("load container endpoint hosts: %w", err)
		}
		for _, host := range records {
			hosts[host.ID] = host
		}
	}

	accounts := make(map[string]model.HostAccount, len(accountIDs))
	if len(accountIDs) > 0 {
		var records []model.HostAccount
		if err := s.db.WithContext(ctx).Scopes(activeHostAccountScope).
			Where("host_accounts.id IN ?", accountIDs).
			Find(&records).Error; err != nil {
			return nil, fmt.Errorf("load container endpoint accounts: %w", err)
		}
		for _, account := range records {
			accounts[account.ID] = account
		}
	}

	views := make([]ContainerEndpointView, 0, len(endpoints))
	for _, endpoint := range endpoints {
		view := ContainerEndpointView{
			ID: endpoint.ID, Name: endpoint.Name, Group: endpoint.GroupName,
			Runtime: endpoint.Runtime, ConnectionMode: endpoint.ConnectionMode,
			Address: endpoint.Address, Port: endpoint.Port, HostID: endpoint.HostID,
			HostAccountID: endpoint.HostAccountID, Remark: endpoint.Remark,
			Status: endpoint.Status, CreatedAt: endpoint.CreatedAt.Format(time.RFC3339),
			UpdatedAt: endpoint.UpdatedAt.Format(time.RFC3339),
		}
		if host, ok := hosts[endpoint.HostID]; ok {
			view.HostName = host.Name
			view.HostAddress = host.Address
			view.HostGroup = host.GroupName
			view.HostRemark = host.Remark
		}
		if account, ok := accounts[endpoint.HostAccountID]; ok {
			view.HostAccountName = account.Name
			if view.HostAccountName == "" {
				view.HostAccountName = account.Username
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func normalizeContainerEndpointInput(input ContainerEndpointInput) (ContainerEndpointInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Group = strings.TrimSpace(input.Group)
	input.Runtime = strings.TrimSpace(input.Runtime)
	input.ConnectionMode = strings.TrimSpace(input.ConnectionMode)
	input.Address = strings.TrimSpace(input.Address)
	input.HostID = strings.TrimSpace(input.HostID)
	input.HostAccountID = strings.TrimSpace(input.HostAccountID)
	input.Remark = strings.TrimSpace(input.Remark)
	if input.Name == "" {
		input.Name = input.Address
		if input.Name == "" {
			// SSH/containerd 模式且未关联主机时的兜底名称
			input.Name = "容器连接"
		}
	}
	if input.ID == "" {
		input.ID = model.NewID()
	}
	if input.Runtime != model.ContainerRuntimeDocker && input.Runtime != model.ContainerRuntimeContainerd {
		return ContainerEndpointInput{}, fmt.Errorf("runtime must be docker or containerd")
	}
	if input.ConnectionMode != model.ContainerConnectionSSH && input.ConnectionMode != model.ContainerConnectionDockerAPI && input.ConnectionMode != model.ContainerConnectionContainerd {
		return ContainerEndpointInput{}, fmt.Errorf("unsupported container connection mode")
	}
	if input.Runtime == model.ContainerRuntimeDocker && input.ConnectionMode == model.ContainerConnectionContainerd {
		return ContainerEndpointInput{}, fmt.Errorf("docker runtime cannot use containerd connection")
	}
	if input.Runtime == model.ContainerRuntimeContainerd && input.ConnectionMode == model.ContainerConnectionDockerAPI {
		return ContainerEndpointInput{}, fmt.Errorf("containerd runtime cannot use docker api connection")
	}
	if input.ConnectionMode == model.ContainerConnectionSSH || input.ConnectionMode == model.ContainerConnectionContainerd {
		if input.HostID == "" {
			return ContainerEndpointInput{}, fmt.Errorf("ssh connection requires a host")
		}
		if input.HostAccountID == "" {
			return ContainerEndpointInput{}, fmt.Errorf("ssh connection requires a host account")
		}
	}
	if input.ConnectionMode == model.ContainerConnectionDockerAPI && input.Address == "" {
		return ContainerEndpointInput{}, fmt.Errorf("docker api address is required")
	}
	if input.Status == "" {
		input.Status = "active"
	}
	return input, nil
}
