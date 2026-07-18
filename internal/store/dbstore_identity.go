package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func (s *DBStore) FindIdentitySubject(
	ctx context.Context,
	userID string,
) (service.IdentitySubject, bool, error) {
	var user model.User
	err := s.db.WithContext(ctx).First(&user, "id = ?", strings.TrimSpace(userID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.IdentitySubject{}, false, nil
	}
	if err != nil {
		return service.IdentitySubject{}, false, fmt.Errorf("find identity subject: %w", err)
	}
	return identitySubject(user), true, nil
}

func (s *DBStore) FindIdentitySubjectByTokenHash(
	ctx context.Context,
	tokenHash string,
) (service.IdentitySubject, bool, error) {
	var user model.User
	err := s.db.WithContext(ctx).
		First(&user, "token_hash = ?", strings.TrimSpace(tokenHash)).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.IdentitySubject{}, false, nil
	}
	if err != nil {
		return service.IdentitySubject{}, false, fmt.Errorf("find identity subject by token hash: %w", err)
	}
	return identitySubject(user), true, nil
}

func identitySubject(user model.User) service.IdentitySubject {
	return service.IdentitySubject{
		ID:         user.ID,
		Username:   user.Username,
		SuperAdmin: user.IsSuperAdmin,
		Status:     user.Status,
		ExpiresAt:  user.ExpiresAt,
	}
}
