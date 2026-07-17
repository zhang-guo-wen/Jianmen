package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
)

var (
	ErrInvalidAdminToken = errors.New("invalid admin token")
	ErrAdminUserExpired  = errors.New("admin user expired")
)

type AdminAuthRepository interface {
	FindActiveUserByTokenHash(ctx context.Context, tokenHash string) (model.User, bool, error)
	DisableUser(ctx context.Context, userID string) error
}

type AdminAuthService struct {
	repository AdminAuthRepository
}

func NewAdminAuthService(repository AdminAuthRepository) (*AdminAuthService, error) {
	if repository == nil {
		return nil, errors.New("admin auth repository is required")
	}
	return &AdminAuthService{repository: repository}, nil
}

func (s *AdminAuthService) Authenticate(
	ctx context.Context,
	tokenHash string,
	now time.Time,
	isExpiryExempt func(userID string) bool,
) (model.User, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return model.User{}, ErrInvalidAdminToken
	}
	user, found, err := s.repository.FindActiveUserByTokenHash(ctx, tokenHash)
	if err != nil {
		return model.User{}, fmt.Errorf("find admin user by token: %w", err)
	}
	if !found {
		return model.User{}, ErrInvalidAdminToken
	}
	if user.IsExpired(now.UTC()) && (isExpiryExempt == nil || !isExpiryExempt(user.ID)) {
		if err := s.repository.DisableUser(ctx, user.ID); err != nil {
			return model.User{}, fmt.Errorf("disable expired admin user: %w", err)
		}
		return model.User{}, ErrAdminUserExpired
	}
	return user, nil
}
