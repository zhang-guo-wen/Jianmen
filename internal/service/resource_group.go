package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jianmen/internal/model"
)

var (
	ErrInvalidResourceGroup  = errors.New("invalid resource group")
	ErrResourceGroupNotFound = errors.New("resource group not found")
	ErrResourceGroupConflict = errors.New("resource group already exists")
)

type ResourceGroupRepository interface {
	SearchResourceGroups(ctx context.Context, groupType, query string, page, pageSize int) ([]model.ResourceGroup, int64, error)
	FindResourceGroup(ctx context.Context, id string) (model.ResourceGroup, bool, error)
	ResourceGroupNameExists(ctx context.Context, name, groupType, excludeID string) (bool, error)
	ResourceGroupUsage(ctx context.Context, groupType, name string) (map[string]int64, error)
	CreateResourceGroup(ctx context.Context, group model.ResourceGroup) (model.ResourceGroup, error)
	UpdateResourceGroup(ctx context.Context, group model.ResourceGroup, oldName string) (model.ResourceGroup, error)
	DeleteResourceGroup(ctx context.Context, group model.ResourceGroup) error
}

type ResourceGroupListParams struct {
	GroupType string
	Query     string
	Page      int
	PageSize  int
}

type ResourceGroupSummary struct {
	Group            model.ResourceGroup
	HostCount        int64
	DatabaseCount    int64
	ApplicationCount int64
	ContainerCount   int64
	PlatformCount    int64
	AccountCount     int64
}

type CreateResourceGroupInput struct {
	Name        string
	GroupType   string
	Description string
}

type UpdateResourceGroupInput struct {
	Name        *string
	Description *string
}

type ResourceGroupService struct {
	repository ResourceGroupRepository
}

func NewResourceGroupService(repository ResourceGroupRepository) (*ResourceGroupService, error) {
	if repository == nil {
		return nil, errors.New("resource group repository is required")
	}
	return &ResourceGroupService{repository: repository}, nil
}

func (s *ResourceGroupService) List(ctx context.Context, params ResourceGroupListParams) ([]ResourceGroupSummary, int64, ResourceGroupListParams, error) {
	params.GroupType = strings.ToLower(strings.TrimSpace(params.GroupType))
	params.Query = strings.TrimSpace(params.Query)
	if params.GroupType != "" && !validResourceGroupType(params.GroupType) {
		return nil, 0, params, fmt.Errorf("%w: group_type must be resource or account", ErrInvalidResourceGroup)
	}
	params.Page, params.PageSize = normalizeResourceGroupPage(params.Page, params.PageSize)

	groups, total, err := s.repository.SearchResourceGroups(
		ctx, params.GroupType, params.Query, params.Page, params.PageSize,
	)
	if err != nil {
		return nil, 0, params, fmt.Errorf("search resource groups: %w", err)
	}
	summaries := make([]ResourceGroupSummary, 0, len(groups))
	for _, group := range groups {
		usage, err := s.repository.ResourceGroupUsage(ctx, group.GroupType, group.Name)
		if err != nil {
			return nil, 0, params, fmt.Errorf("load resource group usage: %w", err)
		}
		summaries = append(summaries, ResourceGroupSummary{
			Group:            group,
			HostCount:        usage["host"],
			DatabaseCount:    usage["database"],
			ApplicationCount: usage["application"],
			ContainerCount:   usage["container"],
			PlatformCount:    usage["platform"],
			AccountCount:     usage["account"],
		})
	}
	return summaries, total, params, nil
}

func (s *ResourceGroupService) Get(ctx context.Context, id string) (model.ResourceGroup, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.ResourceGroup{}, ErrResourceGroupNotFound
	}
	group, found, err := s.repository.FindResourceGroup(ctx, id)
	if err != nil {
		return model.ResourceGroup{}, fmt.Errorf("find resource group: %w", err)
	}
	if !found {
		return model.ResourceGroup{}, ErrResourceGroupNotFound
	}
	return group, nil
}

func (s *ResourceGroupService) Create(ctx context.Context, input CreateResourceGroupInput) (model.ResourceGroup, error) {
	group := model.ResourceGroup{
		Name:        strings.TrimSpace(input.Name),
		GroupType:   strings.ToLower(strings.TrimSpace(input.GroupType)),
		Description: strings.TrimSpace(input.Description),
	}
	if group.GroupType == "" {
		group.GroupType = model.ResourceGroupTypeResource
	}
	if err := validateResourceGroup(group); err != nil {
		return model.ResourceGroup{}, err
	}
	exists, err := s.repository.ResourceGroupNameExists(ctx, group.Name, group.GroupType, "")
	if err != nil {
		return model.ResourceGroup{}, fmt.Errorf("check resource group name: %w", err)
	}
	if exists {
		return model.ResourceGroup{}, ErrResourceGroupConflict
	}
	created, err := s.repository.CreateResourceGroup(ctx, group)
	if err != nil {
		return model.ResourceGroup{}, fmt.Errorf("create resource group: %w", err)
	}
	return created, nil
}

func (s *ResourceGroupService) Update(ctx context.Context, id string, input UpdateResourceGroupInput) (model.ResourceGroup, error) {
	group, err := s.Get(ctx, id)
	if err != nil {
		return model.ResourceGroup{}, err
	}
	oldName := group.Name
	if input.Name != nil {
		group.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		group.Description = strings.TrimSpace(*input.Description)
	}
	if err := validateResourceGroup(group); err != nil {
		return model.ResourceGroup{}, err
	}
	if group.Name != oldName {
		exists, err := s.repository.ResourceGroupNameExists(ctx, group.Name, group.GroupType, group.ID)
		if err != nil {
			return model.ResourceGroup{}, fmt.Errorf("check resource group name: %w", err)
		}
		if exists {
			return model.ResourceGroup{}, ErrResourceGroupConflict
		}
	}
	updated, err := s.repository.UpdateResourceGroup(ctx, group, oldName)
	if err != nil {
		return model.ResourceGroup{}, fmt.Errorf("update resource group: %w", err)
	}
	return updated, nil
}

func (s *ResourceGroupService) Delete(ctx context.Context, id string) error {
	group, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.DeleteResourceGroup(ctx, group); err != nil {
		return fmt.Errorf("delete resource group: %w", err)
	}
	return nil
}

func validateResourceGroup(group model.ResourceGroup) error {
	if strings.TrimSpace(group.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidResourceGroup)
	}
	if !validResourceGroupType(group.GroupType) {
		return fmt.Errorf("%w: group_type must be resource or account", ErrInvalidResourceGroup)
	}
	return nil
}

func validResourceGroupType(groupType string) bool {
	return groupType == model.ResourceGroupTypeResource || groupType == model.ResourceGroupTypeAccount
}

func normalizeResourceGroupPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}
