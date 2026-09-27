package admin

import (
	"context"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func TestNewRoleManagementServiceRequiresRoleManagementRepository(t *testing.T) {
	roleManagement, err := newRoleManagementService(&fakeRoleManagementRepository{})
	if err != nil {
		t.Fatalf("newRoleManagementService: %v", err)
	}
	if roleManagement == nil {
		t.Fatal("role service was nil")
	}
}

func TestNewRoleManagementServiceRejectsNilRepository(t *testing.T) {
	_, err := newRoleManagementService(nil)
	if err == nil {
		t.Fatal("expect error when repository is nil")
	}
	if !strings.Contains(err.Error(), "role repository is required") {
		t.Fatalf("error = %q, want contains role repository is required", err)
	}
}

type fakeRoleManagementRepository struct{}

func (fakeRoleManagementRepository) SearchRoles(context.Context, string, int, int) ([]model.Role, int64, error) {
	return nil, 0, nil
}
func (fakeRoleManagementRepository) FindRole(context.Context, string) (model.Role, bool, error) {
	return model.Role{}, false, nil
}
func (fakeRoleManagementRepository) RoleNameExists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (fakeRoleManagementRepository) RoleActions(context.Context, string) ([]string, error) {
	return nil, nil
}
func (fakeRoleManagementRepository) ReplaceRoleActions(context.Context, string, []model.Permission) error {
	return nil
}
func (fakeRoleManagementRepository) CreateRole(context.Context, model.Role) (model.Role, error) {
	return model.Role{}, nil
}
func (fakeRoleManagementRepository) UpdateRole(context.Context, model.Role) (model.Role, error) {
	return model.Role{}, nil
}
func (fakeRoleManagementRepository) DeleteRole(context.Context, model.Role) error {
	return nil
}
func (fakeRoleManagementRepository) SearchPermissions(context.Context, string, int, int) ([]model.Permission, int64, error) {
	return nil, 0, nil
}
func (fakeRoleManagementRepository) FindPermission(context.Context, string) (model.Permission, bool, error) {
	return model.Permission{}, false, nil
}
func (fakeRoleManagementRepository) CreatePermission(context.Context, model.Permission) (model.Permission, error) {
	return model.Permission{}, nil
}
func (fakeRoleManagementRepository) UpdatePermission(context.Context, model.Permission) (model.Permission, error) {
	return model.Permission{}, nil
}
func (fakeRoleManagementRepository) DeletePermission(context.Context, model.Permission) error {
	return nil
}
func (fakeRoleManagementRepository) SearchUserRoles(context.Context, string, int, int) ([]model.UserRole, int64, error) {
	return nil, 0, nil
}
func (fakeRoleManagementRepository) FindUserRole(context.Context, string) (model.UserRole, bool, error) {
	return model.UserRole{}, false, nil
}
func (fakeRoleManagementRepository) CreateUserRole(context.Context, model.UserRole) (model.UserRole, error) {
	return model.UserRole{}, nil
}
func (fakeRoleManagementRepository) DeleteUserRole(context.Context, model.UserRole) error {
	return nil
}
func (fakeRoleManagementRepository) SearchRolePermissions(context.Context, string, int, int) ([]model.RolePermission, int64, error) {
	return nil, 0, nil
}
func (fakeRoleManagementRepository) FindRolePermission(context.Context, string) (model.RolePermission, bool, error) {
	return model.RolePermission{}, false, nil
}
func (fakeRoleManagementRepository) CreateRolePermission(context.Context, model.RolePermission) (model.RolePermission, error) {
	return model.RolePermission{}, nil
}
func (fakeRoleManagementRepository) DeleteRolePermission(context.Context, model.RolePermission) error {
	return nil
}
func (fakeRoleManagementRepository) FindUser(context.Context, string) (model.User, bool, error) {
	return model.User{}, false, nil
}
func (fakeRoleManagementRepository) EffectiveGlobalPermissions(context.Context, string, time.Time) ([]model.Permission, error) {
	return nil, nil
}

var _ service.RoleManagementRepository = fakeRoleManagementRepository{}
