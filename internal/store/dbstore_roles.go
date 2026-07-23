package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// roleConflictError exposes a small structural marker consumed by service
// without making storage depend on it.
type roleConflictError struct{ err error }

func (e *roleConflictError) Error() string  { return "role repository conflict: " + e.err.Error() }
func (e *roleConflictError) Unwrap() error  { return e.err }
func (e *roleConflictError) Conflict() bool { return true }

func roleUniqueConstraint(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"unique constraint", "unique violation", "duplicate key", "duplicate entry", "sqlstate 23505"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (s *DBStore) SearchRoles(ctx context.Context, query string, page, pageSize int) ([]model.Role, int64, error) {
	build := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Model(&model.Role{}).Scopes(ActiveScope)
		if query != "" {
			like := "%" + query + "%"
			tx = tx.Where("name LIKE ? OR description LIKE ?", like, like)
		}
		return tx
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count roles: %w", err)
	}
	var roles []model.Role
	if err := build().Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&roles).Error; err != nil {
		return nil, 0, fmt.Errorf("list roles: %w", err)
	}
	return roles, total, nil
}

func (s *DBStore) FindRole(ctx context.Context, id string) (model.Role, bool, error) {
	var role model.Role
	err := s.db.WithContext(ctx).First(&role, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Role{}, false, nil
	}
	if err != nil {
		return model.Role{}, false, fmt.Errorf("find role: %w", err)
	}
	return role, true, nil
}
func (s *DBStore) RoleNameExists(ctx context.Context, name, excludeID string) (bool, error) {
	tx := s.db.WithContext(ctx).Model(&model.Role{}).Scopes(ActiveScope).Where("name = ?", strings.TrimSpace(name))
	if strings.TrimSpace(excludeID) != "" {
		tx = tx.Where("id <> ?", strings.TrimSpace(excludeID))
	}
	var count int64
	if err := tx.Count(&count).Error; err != nil {
		return false, fmt.Errorf("count roles: %w", err)
	}
	return count > 0, nil
}
func (s *DBStore) CreateRole(ctx context.Context, role model.Role) (model.Role, error) {
	if err := s.db.WithContext(ctx).Create(&role).Error; err != nil {
		if roleUniqueConstraint(err) {
			return model.Role{}, &roleConflictError{err}
		}
		return model.Role{}, fmt.Errorf("create role: %w", err)
	}
	return role, nil
}
func (s *DBStore) UpdateRole(ctx context.Context, role model.Role) (model.Role, error) {
	if err := s.db.WithContext(ctx).Save(&role).Error; err != nil {
		if roleUniqueConstraint(err) {
			return model.Role{}, &roleConflictError{err}
		}
		return model.Role{}, fmt.Errorf("update role: %w", err)
	}
	return role, nil
}
func (s *DBStore) DeleteRole(ctx context.Context, role model.Role) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", role.ID).Delete(&model.RolePermission{}).Error; err != nil {
			return fmt.Errorf("delete role permissions: %w", err)
		}
		if err := tx.Where("role_id = ?", role.ID).Delete(&model.UserRole{}).Error; err != nil {
			return fmt.Errorf("delete user roles: %w", err)
		}
		if err := SoftDelete(ctx, tx, "roles", role.ID); err != nil {
			return fmt.Errorf("delete role: %w", err)
		}
		return nil
	})
}

func (s *DBStore) SearchPermissions(ctx context.Context, query string, page, pageSize int) ([]model.Permission, int64, error) {
	build := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Model(&model.Permission{}).Scopes(ActiveScope)
		if query != "" {
			like := "%" + query + "%"
			tx = tx.Where("name LIKE ? OR action LIKE ? OR description LIKE ?", like, like, like)
		}
		return tx
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count permissions: %w", err)
	}
	var permissions []model.Permission
	if err := build().Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&permissions).Error; err != nil {
		return nil, 0, fmt.Errorf("list permissions: %w", err)
	}
	return permissions, total, nil
}
func (s *DBStore) FindPermission(ctx context.Context, id string) (model.Permission, bool, error) {
	var permission model.Permission
	err := s.db.WithContext(ctx).First(&permission, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Permission{}, false, nil
	}
	if err != nil {
		return model.Permission{}, false, fmt.Errorf("find permission: %w", err)
	}
	return permission, true, nil
}
func (s *DBStore) CreatePermission(ctx context.Context, permission model.Permission) (model.Permission, error) {
	if err := s.db.WithContext(ctx).Create(&permission).Error; err != nil {
		if roleUniqueConstraint(err) {
			return model.Permission{}, &roleConflictError{err}
		}
		return model.Permission{}, fmt.Errorf("create permission: %w", err)
	}
	return permission, nil
}
func (s *DBStore) UpdatePermission(ctx context.Context, permission model.Permission) (model.Permission, error) {
	if err := s.db.WithContext(ctx).Save(&permission).Error; err != nil {
		if roleUniqueConstraint(err) {
			return model.Permission{}, &roleConflictError{err}
		}
		return model.Permission{}, fmt.Errorf("update permission: %w", err)
	}
	return permission, nil
}
func (s *DBStore) DeletePermission(ctx context.Context, permission model.Permission) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("permission_id = ?", permission.ID).Delete(&model.RolePermission{}).Error; err != nil {
			return fmt.Errorf("delete permission bindings: %w", err)
		}
		if err := SoftDelete(ctx, tx, "permissions", permission.ID); err != nil {
			return fmt.Errorf("delete permission: %w", err)
		}
		return nil
	})
}

func (s *DBStore) SearchUserRoles(ctx context.Context, query string, page, pageSize int) ([]model.UserRole, int64, error) {
	build := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Model(&model.UserRole{})
		if query != "" {
			like := "%" + query + "%"
			tx = tx.Where("user_id LIKE ? OR role_id LIKE ?", like, like)
		}
		return tx
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count user roles: %w", err)
	}
	var items []model.UserRole
	if err := build().Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list user roles: %w", err)
	}
	return items, total, nil
}
func (s *DBStore) FindUserRole(ctx context.Context, id string) (model.UserRole, bool, error) {
	var binding model.UserRole
	err := s.db.WithContext(ctx).First(&binding, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UserRole{}, false, nil
	}
	if err != nil {
		return model.UserRole{}, false, fmt.Errorf("find user role: %w", err)
	}
	return binding, true, nil
}
func (s *DBStore) CreateUserRole(ctx context.Context, binding model.UserRole) (model.UserRole, error) {
	if err := s.db.WithContext(ctx).Create(&binding).Error; err != nil {
		if roleUniqueConstraint(err) {
			return model.UserRole{}, &roleConflictError{err}
		}
		return model.UserRole{}, fmt.Errorf("create user role: %w", err)
	}
	return binding, nil
}
func (s *DBStore) DeleteUserRole(ctx context.Context, binding model.UserRole) error {
	if err := s.db.WithContext(ctx).Delete(&binding).Error; err != nil {
		return fmt.Errorf("delete user role: %w", err)
	}
	return nil
}

func (s *DBStore) SearchRolePermissions(ctx context.Context, query string, page, pageSize int) ([]model.RolePermission, int64, error) {
	build := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Model(&model.RolePermission{})
		if query != "" {
			like := "%" + query + "%"
			tx = tx.Where("role_id LIKE ? OR permission_id LIKE ?", like, like)
		}
		return tx
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count role permissions: %w", err)
	}
	var items []model.RolePermission
	if err := build().Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list role permissions: %w", err)
	}
	return items, total, nil
}
func (s *DBStore) FindRolePermission(ctx context.Context, id string) (model.RolePermission, bool, error) {
	var binding model.RolePermission
	err := s.db.WithContext(ctx).First(&binding, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.RolePermission{}, false, nil
	}
	if err != nil {
		return model.RolePermission{}, false, fmt.Errorf("find role permission: %w", err)
	}
	return binding, true, nil
}
func (s *DBStore) CreateRolePermission(ctx context.Context, binding model.RolePermission) (model.RolePermission, error) {
	if err := s.db.WithContext(ctx).Create(&binding).Error; err != nil {
		if roleUniqueConstraint(err) {
			return model.RolePermission{}, &roleConflictError{err}
		}
		return model.RolePermission{}, fmt.Errorf("create role permission: %w", err)
	}
	return binding, nil
}
func (s *DBStore) DeleteRolePermission(ctx context.Context, binding model.RolePermission) error {
	if err := s.db.WithContext(ctx).Delete(&binding).Error; err != nil {
		return fmt.Errorf("delete role permission: %w", err)
	}
	return nil
}

func (s *DBStore) RoleActions(ctx context.Context, roleID string) ([]string, error) {
	var actions []string
	err := s.db.WithContext(ctx).Model(&model.Permission{}).Scopes(ActiveScope).Distinct("permissions.action").Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").Where("role_permissions.role_id = ?", strings.TrimSpace(roleID)).Where("permissions.action <> '' AND permissions.resource_type = '' AND permissions.resource_id = ''").Where("permissions.effect = '' OR permissions.effect = ?", model.PermissionEffectAllow).Order("permissions.action").Pluck("permissions.action", &actions).Error
	if err != nil {
		return nil, fmt.Errorf("list role actions: %w", err)
	}
	return actions, nil
}

func (s *DBStore) ReplaceRoleActions(ctx context.Context, roleID string, requested []model.Permission) error {
	for attempt := 0; ; attempt++ {
		err := s.replaceRoleActions(ctx, roleID, requested)
		if err == nil || attempt >= 4 || !isSQLiteLockError(err) {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *DBStore) replaceRoleActions(ctx context.Context, roleID string, requested []model.Permission) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var role model.Role
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").First(&role, "id = ?", strings.TrimSpace(roleID)).Error; err != nil {
			return fmt.Errorf("find role: %w", err)
		}
		actionPermissionIDs := tx.Model(&model.Permission{}).Scopes(ActiveScope).Select("id").Where("action <> '' AND resource_type = '' AND resource_id = ''").Where("effect = '' OR effect = ?", model.PermissionEffectAllow)
		if err := tx.Where("role_id = ? AND permission_id IN (?)", roleID, actionPermissionIDs).Delete(&model.RolePermission{}).Error; err != nil {
			return fmt.Errorf("remove action bindings: %w", err)
		}
		for _, request := range requested {
			var permission model.Permission
			err := tx.Where("action = ? AND resource_type = '' AND resource_id = ''", request.Action).Where("effect = '' OR effect = ?", model.PermissionEffectAllow).Order("created_at, id").First(&permission).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				permission = request
				result := tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{
						{Name: "action"},
						{Name: "resource_type"},
						{Name: "resource_id"},
						{Name: "effect"},
					},
					DoNothing: true,
				}).Create(&permission)
				if result.Error != nil {
					return fmt.Errorf("create action permission: %w", result.Error)
				}
				if result.RowsAffected == 0 {
					if err := tx.Where(
						"action = ? AND resource_type = ? AND resource_id = ? AND effect = ?",
						request.Action, request.ResourceType, request.ResourceID, request.Effect,
					).First(&permission).Error; err != nil {
						return fmt.Errorf("load concurrent action permission: %w", err)
					}
				}
			} else if err != nil {
				return fmt.Errorf("find action permission: %w", err)
			}
			binding := model.RolePermission{RoleID: roleID, PermissionID: permission.ID}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&binding).Error; err != nil {
				return fmt.Errorf("bind action permission: %w", err)
			}
		}
		return nil
	})
}

func isSQLiteLockError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked") ||
		strings.Contains(message, "sqlite_busy") ||
		strings.Contains(message, "sqlite_locked")
}

func (s *DBStore) EffectiveGlobalPermissions(ctx context.Context, userID string, now time.Time) ([]model.Permission, error) {
	var permissions []model.Permission
	comparisonTime := now.UTC()
	if s.db.Dialector.Name() == "sqlite" {
		comparisonTime = now.In(time.Local)
	}
	err := s.db.WithContext(ctx).Table("permissions").Select("permissions.*").Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").Joins("JOIN roles ON roles.id = user_roles.role_id").Where("user_roles.user_id = ?", strings.TrimSpace(userID)).Where("user_roles.expires_at IS NULL OR user_roles.expires_at > ?", comparisonTime).Where("roles.status = '' OR roles.status = ?", "active").Where("permissions.resource_type = '' AND permissions.resource_id = ''").Where("permissions.action <> ''").Find(&permissions).Error
	if err != nil {
		return nil, fmt.Errorf("load effective global permissions: %w", err)
	}
	return permissions, nil
}
