package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func (s *DBStore) FindActiveUserByTokenHash(ctx context.Context, tokenHash string) (model.User, bool, error) {
	var user model.User
	err := s.db.WithContext(ctx).
		Where("token_hash = ? AND status = ?", strings.TrimSpace(tokenHash), "active").
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, false, nil
	}
	if err != nil {
		return model.User{}, false, fmt.Errorf("find active user by token hash: %w", err)
	}
	return user, true, nil
}

func (s *DBStore) DisableUser(ctx context.Context, userID string) error {
	result := s.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", strings.TrimSpace(userID)).
		Update("status", "disabled")
	if result.Error != nil {
		return fmt.Errorf("disable user: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.New("user not found")
	}
	return nil
}
