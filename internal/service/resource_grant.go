package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jianmen/internal/model"
)

var (
	ErrInvalidResourceGrant   = errors.New("invalid resource grant")
	ErrResourceGrantNotFound  = errors.New("resource grant not found")
	ErrResourceGrantForbidden = errors.New("resource grant access forbidden")
)

type ResourceGrantRepository interface {
	SearchResourceGrants(ctx context.Context, query string) ([]model.ResourceGrant, error)
	FindResourceGrant(ctx context.Context, id string) (model.ResourceGrant, bool, error)
	CreateResourceGrant(ctx context.Context, grant model.ResourceGrant) (model.ResourceGrant, error)
	EnsureResourceGrant(ctx context.Context, grant model.ResourceGrant) error
	DeleteResourceGrant(ctx context.Context, id string) error
	ResourceGrantPrincipalExists(ctx context.Context, principalType, principalID string) (bool, error)
	ResourceGrantResourceExists(ctx context.Context, resourceType, resourceID string) (bool, error)
}

type ResourceGrantChecker interface {
	HasGrant(userID, resourceType, resourceID string) (bool, error)
}

type ResourceGrantPage struct {
	Items    []model.ResourceGrant
	Total    int
	Page     int
	PageSize int
}

type ResourceGrantService struct {
	repository ResourceGrantRepository
	checker    ResourceGrantChecker
}

func NewResourceGrantService(repository ResourceGrantRepository, checker ResourceGrantChecker) (*ResourceGrantService, error) {
	if repository == nil {
		return nil, errors.New("resource grant repository is required")
	}
	if checker == nil {
		return nil, errors.New("resource grant checker is required")
	}
	return &ResourceGrantService{repository: repository, checker: checker}, nil
}

func (s *ResourceGrantService) List(ctx context.Context, actorID string, bypass bool, query string, page, pageSize int) (ResourceGrantPage, error) {
	if !bypass && strings.TrimSpace(actorID) == "" {
		return ResourceGrantPage{}, ErrResourceGrantForbidden
	}
	page, pageSize = normalizeResourceGrantPage(page, pageSize)
	grants, err := s.repository.SearchResourceGrants(ctx, strings.TrimSpace(query))
	if err != nil {
		return ResourceGrantPage{}, fmt.Errorf("search resource grants: %w", err)
	}

	visible := grants
	if !bypass {
		visible = make([]model.ResourceGrant, 0, len(grants))
		decisions := make(map[string]bool)
		for _, grant := range grants {
			key := grant.ResourceType + "\x00" + grant.ResourceID
			allowed, checked := decisions[key]
			if !checked {
				allowed, err = s.checker.HasGrant(actorID, grant.ResourceType, grant.ResourceID)
				if err != nil {
					return ResourceGrantPage{}, fmt.Errorf("authorize resource grant list: %w", err)
				}
				decisions[key] = allowed
			}
			if allowed {
				visible = append(visible, grant)
			}
		}
	}

	total := len(visible)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return ResourceGrantPage{
		Items:    visible[start:end],
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *ResourceGrantService) Get(ctx context.Context, actorID string, bypass bool, id string) (model.ResourceGrant, error) {
	grant, found, err := s.repository.FindResourceGrant(ctx, strings.TrimSpace(id))
	if err != nil {
		return model.ResourceGrant{}, fmt.Errorf("find resource grant: %w", err)
	}
	if !found {
		return model.ResourceGrant{}, ErrResourceGrantNotFound
	}
	if err := s.authorize(actorID, bypass, grant.ResourceType, grant.ResourceID); err != nil {
		return model.ResourceGrant{}, err
	}
	return grant, nil
}

func (s *ResourceGrantService) Create(ctx context.Context, actorID string, bypass bool, grant model.ResourceGrant) (model.ResourceGrant, error) {
	grant = normalizeResourceGrant(grant)
	if err := validateResourceGrant(grant); err != nil {
		return model.ResourceGrant{}, err
	}
	principalExists, err := s.repository.ResourceGrantPrincipalExists(ctx, grant.PrincipalType, grant.PrincipalID)
	if err != nil {
		return model.ResourceGrant{}, fmt.Errorf("check resource grant principal: %w", err)
	}
	if !principalExists {
		return model.ResourceGrant{}, fmt.Errorf("%w: principal not found", ErrInvalidResourceGrant)
	}
	resourceExists, err := s.repository.ResourceGrantResourceExists(ctx, grant.ResourceType, grant.ResourceID)
	if err != nil {
		return model.ResourceGrant{}, fmt.Errorf("check resource grant resource: %w", err)
	}
	if !resourceExists {
		return model.ResourceGrant{}, fmt.Errorf("%w: resource not found or resource_type mismatch", ErrInvalidResourceGrant)
	}
	if err := s.authorize(actorID, bypass, grant.ResourceType, grant.ResourceID); err != nil {
		return model.ResourceGrant{}, err
	}
	grant.FullAudit.CreatedBy = actorID // 记录创建人
	created, err := s.repository.CreateResourceGrant(ctx, grant)
	if err != nil {
		return model.ResourceGrant{}, fmt.Errorf("create resource grant: %w", err)
	}
	return created, nil
}

// GrantCreatedResource ensures that a non-super-administrator can manage a
// resource they have just created. It is separate from Create because the
// creator cannot already hold a grant on a resource that did not previously
// exist.
func (s *ResourceGrantService) GrantCreatedResource(
	ctx context.Context,
	actorID string,
	bypass bool,
	resourceType string,
	resourceID string,
) error {
	if bypass {
		return nil
	}
	grant := normalizeResourceGrant(model.ResourceGrant{
		PrincipalType: "user",
		PrincipalID:   actorID,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		Effect:        model.PermissionEffectAllow,
		FullAudit: model.FullAudit{CreatedBy: actorID}, // 记录创建人
	})
	if err := validateResourceGrant(grant); err != nil {
		return err
	}
	principalExists, err := s.repository.ResourceGrantPrincipalExists(ctx, grant.PrincipalType, grant.PrincipalID)
	if err != nil {
		return fmt.Errorf("check created resource principal: %w", err)
	}
	if !principalExists {
		return fmt.Errorf("%w: creator not found", ErrInvalidResourceGrant)
	}
	resourceExists, err := s.repository.ResourceGrantResourceExists(ctx, grant.ResourceType, grant.ResourceID)
	if err != nil {
		return fmt.Errorf("check created resource: %w", err)
	}
	if !resourceExists {
		return fmt.Errorf("%w: created resource not found or resource_type mismatch", ErrInvalidResourceGrant)
	}
	if err := s.repository.EnsureResourceGrant(ctx, grant); err != nil {
		return fmt.Errorf("ensure creator resource grant: %w", err)
	}
	return nil
}

func (s *ResourceGrantService) Delete(ctx context.Context, actorID string, bypass bool, id string) error {
	grant, err := s.Get(ctx, actorID, bypass, id)
	if err != nil {
		return err
	}
	if err := s.repository.DeleteResourceGrant(ctx, grant.ID); err != nil {
		return fmt.Errorf("delete resource grant: %w", err)
	}
	return nil
}

func (s *ResourceGrantService) Check(ctx context.Context, userID, resourceType, resourceID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if userID == "" || resourceType == "" || resourceID == "" {
		return false, fmt.Errorf("%w: user_id, resource_type, and resource_id are required", ErrInvalidResourceGrant)
	}
	if !supportedResourceGrantType(resourceType) {
		return false, fmt.Errorf("%w: unsupported resource_type", ErrInvalidResourceGrant)
	}
	allowed, err := s.checker.HasGrant(userID, resourceType, resourceID)
	if err != nil {
		return false, fmt.Errorf("check resource grant: %w", err)
	}
	return allowed, nil
}

func (s *ResourceGrantService) authorize(actorID string, bypass bool, resourceType, resourceID string) error {
	if bypass {
		return nil
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return ErrResourceGrantForbidden
	}
	allowed, err := s.checker.HasGrant(actorID, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("authorize resource grant: %w", err)
	}
	if !allowed {
		return ErrResourceGrantForbidden
	}
	return nil
}

func normalizeResourceGrant(grant model.ResourceGrant) model.ResourceGrant {
	grant.ID = strings.TrimSpace(grant.ID)
	grant.PrincipalType = strings.ToLower(strings.TrimSpace(grant.PrincipalType))
	grant.PrincipalID = strings.TrimSpace(grant.PrincipalID)
	grant.ResourceType = strings.ToLower(strings.TrimSpace(grant.ResourceType))
	grant.ResourceID = strings.TrimSpace(grant.ResourceID)
	grant.FullAudit.CreatedBy = strings.TrimSpace(grant.FullAudit.CreatedBy)
	grant.Effect = strings.ToLower(strings.TrimSpace(grant.Effect))
	if grant.Effect == "" {
		grant.Effect = model.PermissionEffectAllow
	}
	return grant
}

func validateResourceGrant(grant model.ResourceGrant) error {
	if grant.PrincipalType == "" || grant.PrincipalID == "" {
		return fmt.Errorf("%w: principal_type and principal_id are required", ErrInvalidResourceGrant)
	}
	if grant.PrincipalType != "user" && grant.PrincipalType != "user_group" {
		return fmt.Errorf("%w: principal_type must be 'user' or 'user_group'", ErrInvalidResourceGrant)
	}
	if grant.ResourceType == "" || grant.ResourceID == "" {
		return fmt.Errorf("%w: resource_type and resource_id are required", ErrInvalidResourceGrant)
	}
	if !supportedResourceGrantType(grant.ResourceType) {
		return fmt.Errorf("%w: unsupported resource_type", ErrInvalidResourceGrant)
	}
	if grant.Effect != model.PermissionEffectAllow && grant.Effect != model.PermissionEffectDeny {
		return fmt.Errorf("%w: effect must be 'allow' or 'deny'", ErrInvalidResourceGrant)
	}
	return nil
}

func supportedResourceGrantType(resourceType string) bool {
	switch resourceType {
	case model.ResourceTypeHost,
		model.ResourceTypeHostAccount,
		model.ResourceTypeDatabaseInstance,
		model.ResourceTypeDatabaseAccount,
		model.ResourceTypeApplication,
		model.ResourceTypeContainerEndpoint,
		model.ResourceTypePlatformAccount,
		model.ResourceTypeGroup,
		model.ResourceTypeAccountGroup:
		return true
	default:
		return false
	}
}

func normalizeResourceGrantPage(page, pageSize int) (int, int) {
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
