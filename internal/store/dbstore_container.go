package store

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func (s *DBStore) ContainerEndpoints() []ContainerEndpointView {
	var endpoints []model.ContainerEndpoint
	if err := s.db.Order("created_at DESC").Find(&endpoints).Error; err != nil {
		return nil
	}
	views := make([]ContainerEndpointView, 0, len(endpoints))
	for _, endpoint := range endpoints {
		views = append(views, s.containerEndpointView(endpoint))
	}
	return views
}

func (s *DBStore) ContainerEndpoint(id string) (ContainerEndpointView, error) {
	var endpoint model.ContainerEndpoint
	if err := s.db.First(&endpoint, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return ContainerEndpointView{}, fmt.Errorf("%w: %q", ErrContainerEndpointNotFound, id)
	}
	return s.containerEndpointView(endpoint), nil
}

func (s *DBStore) AddContainerEndpoint(input ContainerEndpointInput) (ContainerEndpointView, error) {
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
	if err := s.db.Transaction(func(tx *gorm.DB) error {
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
	return s.containerEndpointView(endpoint), nil
}

func (s *DBStore) UpdateContainerEndpoint(id string, input ContainerEndpointInput) (ContainerEndpointView, error) {
	var endpoint model.ContainerEndpoint
	if err := s.db.First(&endpoint, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return ContainerEndpointView{}, fmt.Errorf("%w: %q", ErrContainerEndpointNotFound, id)
	}
	normalized, err := normalizeContainerEndpointInput(input)
	if err != nil {
		return ContainerEndpointView{}, err
	}
	endpoint.Name, endpoint.GroupName = normalized.Name, normalized.Group
	endpoint.Runtime, endpoint.ConnectionMode = normalized.Runtime, normalized.ConnectionMode
	endpoint.Address, endpoint.Port = normalized.Address, normalized.Port
	endpoint.HostID, endpoint.HostAccountID = normalized.HostID, normalized.HostAccountID
	endpoint.Remark, endpoint.Status = normalized.Remark, normalized.Status
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&endpoint).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, normalized.Group); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeContainerEndpoint, endpoint.ID, endpoint.Name, "")
	}); err != nil {
		return ContainerEndpointView{}, fmt.Errorf("update container endpoint: %w", err)
	}
	return s.containerEndpointView(endpoint), nil
}

func (s *DBStore) DeleteContainerEndpoint(id string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var endpoint model.ContainerEndpoint
		if err := tx.First(&endpoint, "id = ?", strings.TrimSpace(id)).Error; err != nil {
			return fmt.Errorf("%w: %q", ErrContainerEndpointNotFound, id)
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeContainerEndpoint, endpoint.ID); err != nil {
			return err
		}
		return tx.Delete(&endpoint).Error
	})
}

func (s *DBStore) containerEndpointView(endpoint model.ContainerEndpoint) ContainerEndpointView {
	view := ContainerEndpointView{
		ID: endpoint.ID, Name: endpoint.Name, Group: endpoint.GroupName,
		Runtime: endpoint.Runtime, ConnectionMode: endpoint.ConnectionMode,
		Address: endpoint.Address, Port: endpoint.Port, HostID: endpoint.HostID,
		HostAccountID: endpoint.HostAccountID, Remark: endpoint.Remark,
		Status: endpoint.Status, CreatedAt: endpoint.CreatedAt.Format(time.RFC3339),
		UpdatedAt: endpoint.UpdatedAt.Format(time.RFC3339),
	}
	if endpoint.HostID != "" {
		var host model.Host
		if s.db.First(&host, "id = ?", endpoint.HostID).Error == nil {
			view.HostName = host.Name
		}
	}
	if endpoint.HostAccountID != "" {
		var account model.HostAccount
		if s.db.First(&account, "id = ?", endpoint.HostAccountID).Error == nil {
			view.HostAccountName = account.Name
			if view.HostAccountName == "" {
				view.HostAccountName = account.Username
			}
		}
	}
	return view
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
	if input.ConnectionMode == model.ContainerConnectionSSH && input.HostAccountID == "" {
		return ContainerEndpointInput{}, fmt.Errorf("ssh connection requires a host account")
	}
	if input.Address == "" {
		return ContainerEndpointInput{}, fmt.Errorf("container endpoint address is required")
	}
	if input.Status == "" {
		input.Status = "active"
	}
	return input, nil
}
