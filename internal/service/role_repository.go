package service

import (
	"context"
	"time"

	"jianmen/internal/model"
)

type RoleRepository interface {
	SearchRoles(context.Context, string, int, int) ([]model.Role, int64, error)
	FindRole(context.Context, string) (model.Role, bool, error)
	RoleNameExists(context.Context, string, string) (bool, error)
	CreateRole(context.Context, model.Role) (model.Role, error)
	UpdateRole(context.Context, model.Role) (model.Role, error)
	DeleteRole(context.Context, model.Role) error
	RoleActions(context.Context, string) ([]string, error)
	ReplaceRoleActions(context.Context, string, []model.Permission) error
}

type PermissionRepository interface {
	SearchPermissions(context.Context, string, int, int) ([]model.Permission, int64, error)
	FindPermission(context.Context, string) (model.Permission, bool, error)
	CreatePermission(context.Context, model.Permission) (model.Permission, error)
	UpdatePermission(context.Context, model.Permission) (model.Permission, error)
	DeletePermission(context.Context, model.Permission) error
}

type RoleBindingRepository interface {
	SearchUserRoles(context.Context, string, int, int) ([]model.UserRole, int64, error)
	FindUserRole(context.Context, string) (model.UserRole, bool, error)
	CreateUserRole(context.Context, model.UserRole) (model.UserRole, error)
	DeleteUserRole(context.Context, model.UserRole) error
	SearchRolePermissions(context.Context, string, int, int) ([]model.RolePermission, int64, error)
	FindRolePermission(context.Context, string) (model.RolePermission, bool, error)
	CreateRolePermission(context.Context, model.RolePermission) (model.RolePermission, error)
	DeleteRolePermission(context.Context, model.RolePermission) error
	FindUser(context.Context, string) (model.User, bool, error)
}

type EffectivePermissionRepository interface {
	EffectiveGlobalPermissions(context.Context, string, time.Time) ([]model.Permission, error)
}

type RoleManagementRepository interface {
	RoleRepository
	PermissionRepository
	RoleBindingRepository
	EffectivePermissionRepository
}
