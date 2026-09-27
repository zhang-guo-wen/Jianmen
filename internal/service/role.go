package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

var (
	ErrInvalidRole         = errors.New("invalid role")
	ErrRoleNotFound        = errors.New("role not found")
	ErrRoleConflict        = errors.New("role already exists")
	ErrBuiltinRole         = errors.New("builtin role is protected")
	ErrInvalidPermission   = errors.New("invalid permission")
	ErrPermissionNotFound  = errors.New("permission not found")
	ErrPermissionConflict  = errors.New("permission already exists")
	ErrRoleBindingNotFound = errors.New("role binding not found")
	ErrRoleBindingConflict = errors.New("role binding already exists")
)

type RoleListParams struct {
	Query    string
	Page     int
	PageSize int
}
type RoleInput struct {
	ID, Name, Description, Status string
	Builtin                       *bool
}
type PermissionInput struct{ ID, Name, Action, ResourceType, ResourceID, Effect, Description string }
type UserRoleInput struct {
	ID, UserID, RoleID string
	ExpiresAt          *time.Time
}
type RolePermissionInput struct{ ID, RoleID, PermissionID string }

type RoleService struct {
	repository RoleManagementRepository
	now        func() time.Time
}

func NewRoleService(repository RoleManagementRepository) (*RoleService, error) {
	if repository == nil {
		return nil, errors.New("role repository is required")
	}
	return &RoleService{repository: repository, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *RoleService) List(ctx context.Context, params RoleListParams) ([]model.Role, int64, RoleListParams, error) {
	params = normalizeRoleList(params)
	items, total, err := s.repository.SearchRoles(ctx, params.Query, params.Page, params.PageSize)
	if err != nil {
		return nil, 0, params, fmt.Errorf("search roles: %w", err)
	}
	return items, total, params, nil
}

func (s *RoleService) Create(ctx context.Context, input RoleInput) (model.Role, error) {
	if input.Builtin != nil {
		return model.Role{}, fmt.Errorf("%w: builtin is managed by the system", ErrInvalidRole)
	}
	role, err := roleFromInput(input)
	if err != nil {
		return model.Role{}, err
	}
	exists, err := s.repository.RoleNameExists(ctx, role.Name, "")
	if err != nil {
		return model.Role{}, fmt.Errorf("check role name: %w", err)
	}
	if exists {
		return model.Role{}, ErrRoleConflict
	}
	created, err := s.repository.CreateRole(ctx, role)
	if err != nil {
		return model.Role{}, fmt.Errorf("create role: %w", mapRoleRepositoryConflict(err, ErrRoleConflict))
	}
	return created, nil
}

func (s *RoleService) Get(ctx context.Context, id string) (model.Role, error) {
	return s.findRole(ctx, id)
}

func (s *RoleService) Update(ctx context.Context, id string, input RoleInput) (model.Role, error) {
	role, err := s.findRole(ctx, id)
	if err != nil {
		return model.Role{}, err
	}
	if role.Builtin {
		return model.Role{}, ErrBuiltinRole
	}
	if input.Builtin != nil {
		return model.Role{}, fmt.Errorf("%w: builtin is immutable", ErrInvalidRole)
	}
	updated, err := roleFromInput(input)
	if err != nil {
		return model.Role{}, err
	}
	if updated.Name != role.Name {
		exists, err := s.repository.RoleNameExists(ctx, updated.Name, role.ID)
		if err != nil {
			return model.Role{}, fmt.Errorf("check role name: %w", err)
		}
		if exists {
			return model.Role{}, ErrRoleConflict
		}
	}
	updated.ID = role.ID
	updated.Builtin = role.Builtin
	stored, err := s.repository.UpdateRole(ctx, updated)
	if err != nil {
		return model.Role{}, fmt.Errorf("update role: %w", mapRoleRepositoryConflict(err, ErrRoleConflict))
	}
	return stored, nil
}

func (s *RoleService) Delete(ctx context.Context, id string) error {
	role, err := s.findRole(ctx, id)
	if err != nil {
		return err
	}
	if role.Builtin {
		return ErrBuiltinRole
	}
	if err := s.repository.DeleteRole(ctx, role); err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	return nil
}

func (s *RoleService) ListPermissions(ctx context.Context, params RoleListParams) ([]model.Permission, int64, RoleListParams, error) {
	params = normalizeRoleList(params)
	items, total, err := s.repository.SearchPermissions(ctx, params.Query, params.Page, params.PageSize)
	if err != nil {
		return nil, 0, params, fmt.Errorf("search permissions: %w", err)
	}
	return items, total, params, nil
}

func (s *RoleService) CreatePermission(ctx context.Context, input PermissionInput) (model.Permission, error) {
	permission, err := permissionFromInput(input)
	if err != nil {
		return model.Permission{}, err
	}
	stored, err := s.repository.CreatePermission(ctx, permission)
	if err != nil {
		return model.Permission{}, fmt.Errorf("create permission: %w", mapRoleRepositoryConflict(err, ErrPermissionConflict))
	}
	return stored, nil
}

func (s *RoleService) GetPermission(ctx context.Context, id string) (model.Permission, error) {
	return s.findPermission(ctx, id)
}

func (s *RoleService) UpdatePermission(ctx context.Context, id string, input PermissionInput) (model.Permission, error) {
	if _, err := s.findPermission(ctx, id); err != nil {
		return model.Permission{}, err
	}
	permission, err := permissionFromInput(input)
	if err != nil {
		return model.Permission{}, err
	}
	permission.ID = strings.TrimSpace(id)
	stored, err := s.repository.UpdatePermission(ctx, permission)
	if err != nil {
		return model.Permission{}, fmt.Errorf("update permission: %w", mapRoleRepositoryConflict(err, ErrPermissionConflict))
	}
	return stored, nil
}

func (s *RoleService) DeletePermission(ctx context.Context, id string) error {
	permission, err := s.findPermission(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.DeletePermission(ctx, permission); err != nil {
		return fmt.Errorf("delete permission: %w", err)
	}
	return nil
}

func (s *RoleService) ListUserRoles(ctx context.Context, params RoleListParams) ([]model.UserRole, int64, RoleListParams, error) {
	params = normalizeRoleList(params)
	items, total, err := s.repository.SearchUserRoles(ctx, params.Query, params.Page, params.PageSize)
	if err != nil {
		return nil, 0, params, fmt.Errorf("search user roles: %w", err)
	}
	return items, total, params, nil
}

func (s *RoleService) CreateUserRole(ctx context.Context, input UserRoleInput) (model.UserRole, error) {
	binding := model.UserRole{ID: strings.TrimSpace(input.ID), UserID: strings.TrimSpace(input.UserID), RoleID: strings.TrimSpace(input.RoleID), ExpiresAt: input.ExpiresAt}
	if binding.UserID == "" || binding.RoleID == "" {
		return model.UserRole{}, fmt.Errorf("%w: user_id and role_id are required", ErrInvalidRole)
	}
	if binding.ExpiresAt != nil && !binding.ExpiresAt.After(s.now()) {
		return model.UserRole{}, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidRole)
	}
	if _, found, err := s.repository.FindUser(ctx, binding.UserID); err != nil {
		return model.UserRole{}, fmt.Errorf("find user: %w", err)
	} else if !found {
		return model.UserRole{}, ErrUserNotFound
	}
	if _, err := s.findRole(ctx, binding.RoleID); err != nil {
		return model.UserRole{}, err
	}
	stored, err := s.repository.CreateUserRole(ctx, binding)
	if err != nil {
		return model.UserRole{}, fmt.Errorf("create user role: %w", mapRoleRepositoryConflict(err, ErrRoleBindingConflict))
	}
	return stored, nil
}

func (s *RoleService) DeleteUserRole(ctx context.Context, id string) error {
	binding, found, err := s.repository.FindUserRole(ctx, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("find user role: %w", err)
	}
	if !found {
		return ErrRoleBindingNotFound
	}
	if err := s.repository.DeleteUserRole(ctx, binding); err != nil {
		return fmt.Errorf("delete user role: %w", err)
	}
	return nil
}

func (s *RoleService) ListRolePermissions(ctx context.Context, params RoleListParams) ([]model.RolePermission, int64, RoleListParams, error) {
	params = normalizeRoleList(params)
	items, total, err := s.repository.SearchRolePermissions(ctx, params.Query, params.Page, params.PageSize)
	if err != nil {
		return nil, 0, params, fmt.Errorf("search role permissions: %w", err)
	}
	return items, total, params, nil
}

func (s *RoleService) CreateRolePermission(ctx context.Context, input RolePermissionInput) (model.RolePermission, error) {
	binding := model.RolePermission{ID: strings.TrimSpace(input.ID), RoleID: strings.TrimSpace(input.RoleID), PermissionID: strings.TrimSpace(input.PermissionID)}
	if binding.RoleID == "" || binding.PermissionID == "" {
		return model.RolePermission{}, fmt.Errorf("%w: role_id and permission_id are required", ErrInvalidRole)
	}
	if _, err := s.findRole(ctx, binding.RoleID); err != nil {
		return model.RolePermission{}, err
	}
	if _, err := s.findPermission(ctx, binding.PermissionID); err != nil {
		return model.RolePermission{}, err
	}
	stored, err := s.repository.CreateRolePermission(ctx, binding)
	if err != nil {
		return model.RolePermission{}, fmt.Errorf("create role permission: %w", mapRoleRepositoryConflict(err, ErrRoleBindingConflict))
	}
	return stored, nil
}

func (s *RoleService) DeleteRolePermission(ctx context.Context, id string) error {
	binding, found, err := s.repository.FindRolePermission(ctx, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("find role permission: %w", err)
	}
	if !found {
		return ErrRoleBindingNotFound
	}
	if err := s.repository.DeleteRolePermission(ctx, binding); err != nil {
		return fmt.Errorf("delete role permission: %w", err)
	}
	return nil
}

func (s *RoleService) Actions(ctx context.Context, roleID string) ([]string, error) {
	if _, err := s.findRole(ctx, roleID); err != nil {
		return nil, err
	}
	actions, err := s.repository.RoleActions(ctx, strings.TrimSpace(roleID))
	if err != nil {
		return nil, fmt.Errorf("load role actions: %w", err)
	}
	return actions, nil
}

func (s *RoleService) ReplaceActions(ctx context.Context, roleID string, requested []string) ([]string, error) {
	role, err := s.findRole(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if role.Builtin {
		return nil, ErrBuiltinRole
	}
	actions, err := rbac.ValidateAssignableActions(requested)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRole, err)
	}
	permissions := make([]model.Permission, 0, len(actions))
	for _, action := range actions {
		definition, ok := rbac.FindPermissionDefinition(action)
		if !ok || !definition.Assignable {
			return nil, fmt.Errorf("%w: action is not assignable", ErrInvalidRole)
		}
		permissions = append(permissions, model.Permission{Name: definition.Label, Action: action, Effect: model.PermissionEffectAllow, Description: definition.Description})
	}
	if err := s.repository.ReplaceRoleActions(ctx, strings.TrimSpace(roleID), permissions); err != nil {
		return nil, fmt.Errorf("replace role actions: %w", err)
	}
	return actions, nil
}

func (s *RoleService) EffectiveGlobalActions(ctx context.Context, userID string) ([]string, error) {
	permissions, err := s.repository.EffectiveGlobalPermissions(ctx, strings.TrimSpace(userID), s.now())
	if err != nil {
		return nil, fmt.Errorf("load effective permissions: %w", err)
	}
	return effectiveActions(permissions), nil
}

func (s *RoleService) findRole(ctx context.Context, id string) (model.Role, error) {
	role, found, err := s.repository.FindRole(ctx, strings.TrimSpace(id))
	if err != nil {
		return model.Role{}, fmt.Errorf("find role: %w", err)
	}
	if !found {
		return model.Role{}, ErrRoleNotFound
	}
	return role, nil
}

func (s *RoleService) findPermission(ctx context.Context, id string) (model.Permission, error) {
	permission, found, err := s.repository.FindPermission(ctx, strings.TrimSpace(id))
	if err != nil {
		return model.Permission{}, fmt.Errorf("find permission: %w", err)
	}
	if !found {
		return model.Permission{}, ErrPermissionNotFound
	}
	return permission, nil
}

func roleFromInput(input RoleInput) (model.Role, error) {
	role := model.Role{ID: strings.TrimSpace(input.ID), Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Status: strings.TrimSpace(input.Status)}
	if input.Builtin != nil {
		role.Builtin = *input.Builtin
	}
	if role.Name == "" {
		return model.Role{}, fmt.Errorf("%w: name is required", ErrInvalidRole)
	}
	if role.Status == "" {
		role.Status = "active"
	}
	if role.Status != "active" && role.Status != "disabled" {
		return model.Role{}, fmt.Errorf("%w: status must be active or disabled", ErrInvalidRole)
	}
	return role, nil
}

func permissionFromInput(input PermissionInput) (model.Permission, error) {
	permission := model.Permission{ID: strings.TrimSpace(input.ID), Name: strings.TrimSpace(input.Name), Action: strings.TrimSpace(input.Action), ResourceType: strings.TrimSpace(input.ResourceType), ResourceID: strings.TrimSpace(input.ResourceID), Effect: strings.ToLower(strings.TrimSpace(input.Effect)), Description: strings.TrimSpace(input.Description)}
	if permission.Effect == "" {
		permission.Effect = model.PermissionEffectAllow
	}
	if permission.Effect != model.PermissionEffectAllow && permission.Effect != model.PermissionEffectDeny {
		return model.Permission{}, fmt.Errorf("%w: effect must be allow or deny", ErrInvalidPermission)
	}
	if (permission.ResourceType == "") != (permission.ResourceID == "") {
		return model.Permission{}, fmt.Errorf("%w: resource_type and resource_id must be provided together", ErrInvalidPermission)
	}
	if permission.Action == "" {
		if permission.ResourceType != "" {
			return permission, nil
		}
		return model.Permission{}, fmt.Errorf("%w: action or resource is required", ErrInvalidPermission)
	}
	definition, ok := rbac.FindPermissionDefinition(permission.Action)
	if !ok || !definition.Assignable || permission.Action == "*" {
		return model.Permission{}, fmt.Errorf("%w: action %q is not assignable", ErrInvalidPermission, permission.Action)
	}
	if permission.ResourceType == "" {
		return permission, nil
	}
	if permission.ResourceType == model.ResourceTypeGroup {
		if len(definition.ResourceTypes) == 0 {
			return model.Permission{}, fmt.Errorf("%w: action %q does not support resource scope", ErrInvalidPermission, permission.Action)
		}
		return permission, nil
	}
	for _, resourceType := range definition.ResourceTypes {
		if permission.ResourceType == resourceType {
			return permission, nil
		}
	}
	return model.Permission{}, fmt.Errorf("%w: action %q does not support resource type %q", ErrInvalidPermission, permission.Action, permission.ResourceType)
}

func normalizeRoleList(params RoleListParams) RoleListParams {
	params.Query = strings.TrimSpace(params.Query)
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 50
	}
	if params.PageSize > 200 {
		params.PageSize = 200
	}
	return params
}

func effectiveActions(permissions []model.Permission) []string {
	allows, denies := map[string]struct{}{}, map[string]struct{}{}
	for _, permission := range permissions {
		action := strings.TrimSpace(permission.Action)
		if strings.EqualFold(permission.Effect, model.PermissionEffectDeny) {
			denies[action] = struct{}{}
		} else {
			allows[action] = struct{}{}
		}
	}
	if _, denied := denies["*"]; denied {
		return []string{}
	}
	if _, wildcard := allows["*"]; wildcard && len(denies) == 0 {
		return []string{"*"}
	}
	effective := map[string]struct{}{}
	if _, wildcard := allows["*"]; wildcard {
		for _, definition := range rbac.PermissionCatalog() {
			if _, denied := denies[definition.Action]; !denied {
				effective[definition.Action] = struct{}{}
			}
		}
	}
	for action := range allows {
		if action != "*" {
			if _, denied := denies[action]; !denied {
				effective[action] = struct{}{}
			}
		}
	}
	actions := make([]string, 0, len(effective))
	for action := range effective {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return actions
}

func mapRoleRepositoryConflict(err, sentinel error) error {
	var marker interface{ Conflict() bool }
	if errors.As(err, &marker) && marker.Conflict() {
		return fmt.Errorf("%w: %w", sentinel, err)
	}
	return err
}
