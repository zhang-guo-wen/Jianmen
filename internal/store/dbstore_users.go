package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jianmen/internal/model"

	"gorm.io/gorm"
)

type repositoryConflictError struct {
	err error
}

func (e *repositoryConflictError) Error() string  { return "repository conflict: " + e.err.Error() }
func (e *repositoryConflictError) Unwrap() error  { return e.err }
func (e *repositoryConflictError) Conflict() bool { return true }

func (s *DBStore) SearchUsers(ctx context.Context, query string, page, pageSize int) ([]model.User, int64, error) {
	buildQuery := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Model(&model.User{})
		if query != "" {
			like := "%" + query + "%"
			tx = tx.Where("username LIKE ? OR display_name LIKE ? OR email LIKE ?", like, like, like)
		}
		return tx
	}
	var total int64
	if err := buildQuery().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	var users []model.User
	if err := buildQuery().Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	return users, total, nil
}

func (s *DBStore) FindUser(ctx context.Context, id string) (model.User, bool, error) {
	var user model.User
	err := s.db.WithContext(ctx).First(&user, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, false, nil
	}
	if err != nil {
		return model.User{}, false, fmt.Errorf("find user: %w", err)
	}
	return user, true, nil
}

func (s *DBStore) UsernameExists(ctx context.Context, username, excludeID string) (bool, error) {
	tx := s.db.WithContext(ctx).Model(&model.User{}).Where("username = ?", strings.TrimSpace(username))
	if strings.TrimSpace(excludeID) != "" {
		tx = tx.Where("id <> ?", strings.TrimSpace(excludeID))
	}
	var count int64
	if err := tx.Count(&count).Error; err != nil {
		return false, fmt.Errorf("count user names: %w", err)
	}
	return count > 0, nil
}

func (s *DBStore) CreateUser(ctx context.Context, user model.User) (model.User, error) {
	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		if isUniqueConstraintError(err) {
			return model.User{}, fmt.Errorf("create user: %w", &repositoryConflictError{err: err})
		}
		return model.User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (s *DBStore) UpdateUser(ctx context.Context, user model.User) (model.User, error) {
	if err := s.db.WithContext(ctx).Save(&user).Error; err != nil {
		if isUniqueConstraintError(err) {
			return model.User{}, fmt.Errorf("update user: %w", &repositoryConflictError{err: err})
		}
		return model.User{}, fmt.Errorf("update user: %w", err)
	}
	return user, nil
}

// isUniqueConstraintError recognizes duplicate-key failures without coupling
// this package to one database driver's concrete error type.
func isUniqueConstraintError(err error) bool {
	type sqliteCodeError interface {
		Code() int
	}
	var sqliteErr sqliteCodeError
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case 1555, 2067: // SQLITE_CONSTRAINT_PRIMARYKEY, SQLITE_CONSTRAINT_UNIQUE
			return true
		}
	}

	type sqlStateError interface {
		SQLState() string
	}
	var stateErr sqlStateError
	if errors.As(err, &stateErr) && stateErr.SQLState() == "23505" {
		return true
	}

	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"unique constraint",
		"unique violation",
		"duplicate key",
		"duplicate entry",
		"sqlstate 23505",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (s *DBStore) DeleteUser(ctx context.Context, user model.User) error {
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", user.ID).Delete(&model.UserRole{}).Error; err != nil {
			return fmt.Errorf("delete user roles: %w", err)
		}
		if err := SoftDelete(ctx, tx, "users", user.ID).Error; err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("delete user transaction: %w", err)
	}
	return nil
}
