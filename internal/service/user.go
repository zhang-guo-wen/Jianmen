package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/util"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidUser   = errors.New("invalid user")
	ErrUserNotFound  = errors.New("user not found")
	ErrUserConflict  = errors.New("user already exists")
	ErrUserForbidden = errors.New("user operation forbidden")
)

type conflictMarker interface{ Conflict() bool }

func mapRepositoryConflict(err, sentinel error) error {
	var marker conflictMarker
	if errors.As(err, &marker) && marker.Conflict() {
		return fmt.Errorf("%w: %w", sentinel, err)
	}
	return err
}

type UserRepository interface {
	SearchUsers(ctx context.Context, query string, page, pageSize int) ([]model.User, int64, error)
	FindUser(ctx context.Context, id string) (model.User, bool, error)
	UsernameExists(ctx context.Context, username, excludeID string) (bool, error)
	CreateUser(ctx context.Context, user model.User) (model.User, error)
	UpdateUser(ctx context.Context, user model.User) (model.User, error)
	DeleteUser(ctx context.Context, user model.User) error
}

type UserListParams struct {
	Query    string
	Page     int
	PageSize int
}

type UserCreateInput struct {
	Username    string
	Password    string
	DisplayName string
	Email       string
	ExpiresAt   *time.Time
	Permanent   bool
}

type UserUpdateInput struct {
	DisplayName *string
	Email       *string
	Status      *string
	ExpiresAt   *time.Time
	Permanent   *bool
}

type UserView struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"display_name,omitempty"`
	Email        string     `json:"email,omitempty"`
	Status       string     `json:"status"`
	IsSuperAdmin bool       `json:"is_super_admin"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type CreatedUser struct {
	User  UserView
	Token string
}

type UserService struct {
	repository UserRepository
	now        func() time.Time
}

func NewUserService(repository UserRepository) (*UserService, error) {
	if repository == nil {
		return nil, errors.New("user repository is required")
	}
	return &UserService{repository: repository, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *UserService) List(ctx context.Context, params UserListParams) ([]UserView, int64, UserListParams, error) {
	params.Query = strings.TrimSpace(params.Query)
	params.Page, params.PageSize = normalizeUserPage(params.Page, params.PageSize)
	users, total, err := s.repository.SearchUsers(ctx, params.Query, params.Page, params.PageSize)
	if err != nil {
		return nil, 0, params, fmt.Errorf("search users: %w", err)
	}
	now := s.now()
	views := make([]UserView, 0, len(users))
	for _, user := range users {
		user, err = s.disableExpiredUser(ctx, user, now)
		if err != nil {
			return nil, 0, params, err
		}
		views = append(views, userView(user))
	}
	return views, total, params, nil
}

func (s *UserService) Create(ctx context.Context, input UserCreateInput) (CreatedUser, error) {
	username := strings.TrimSpace(input.Username)
	password := input.Password
	if username == "" || password == "" {
		return CreatedUser{}, fmt.Errorf("%w: username and password are required", ErrInvalidUser)
	}
	exists, err := s.repository.UsernameExists(ctx, username, "")
	if err != nil {
		return CreatedUser{}, fmt.Errorf("check username: %w", err)
	}
	if exists {
		return CreatedUser{}, ErrUserConflict
	}

	now := s.now()
	expiresAt, err := userExpiry(input.Permanent, input.ExpiresAt, now)
	if err != nil {
		return CreatedUser{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return CreatedUser{}, fmt.Errorf("hash password: %w", err)
	}
	token, tokenHash, err := newUserToken()
	if err != nil {
		return CreatedUser{}, err
	}
	user, err := s.repository.CreateUser(ctx, model.User{
		ID:              model.NewID(),
		Username:        username,
		PasswordHash:    string(passwordHash),
		MySQLNativeHash: util.MySQLNativePasswordHash(password),
		TokenHash:       tokenHash,
		DisplayName:     strings.TrimSpace(input.DisplayName),
		Email:           strings.TrimSpace(input.Email),
		Status:          "active",
		ExpiresAt:       expiresAt,
	})
	if err != nil {
		return CreatedUser{}, fmt.Errorf("create user: %w", mapRepositoryConflict(err, ErrUserConflict))
	}
	return CreatedUser{User: userView(user), Token: token}, nil
}

func (s *UserService) Get(ctx context.Context, id string) (UserView, error) {
	user, err := s.find(ctx, id)
	if err != nil {
		return UserView{}, err
	}
	user, err = s.disableExpiredUser(ctx, user, s.now())
	if err != nil {
		return UserView{}, err
	}
	return userView(user), nil
}

func (s *UserService) disableExpiredUser(ctx context.Context, user model.User, now time.Time) (model.User, error) {
	if !user.IsSuperAdmin && user.Status == "active" && user.IsExpired(now) {
		user.Status = "disabled"
		updated, err := s.repository.UpdateUser(ctx, user)
		if err != nil {
			return model.User{}, fmt.Errorf("disable expired user: %w", err)
		}
		return updated, nil
	}
	return user, nil
}

func (s *UserService) Update(ctx context.Context, id string, input UserUpdateInput) (UserView, error) {
	user, err := s.find(ctx, id)
	if err != nil {
		return UserView{}, err
	}
	if input.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*input.DisplayName)
	}
	if input.Email != nil {
		user.Email = strings.TrimSpace(*input.Email)
	}
	now := s.now()
	if input.Permanent != nil {
		if *input.Permanent {
			user.ExpiresAt = nil
		} else {
			expiresAt, err := userExpiry(false, input.ExpiresAt, now)
			if err != nil {
				return UserView{}, err
			}
			user.ExpiresAt = expiresAt
		}
	} else if input.ExpiresAt != nil {
		value := input.ExpiresAt.UTC()
		user.ExpiresAt = &value
	}
	if input.Status != nil {
		status := strings.TrimSpace(*input.Status)
		if status != "active" && status != "disabled" {
			return UserView{}, fmt.Errorf("%w: status must be active or disabled", ErrInvalidUser)
		}
		if status == "disabled" && user.IsSuperAdmin {
			return UserView{}, ErrUserForbidden
		}
		if status == "active" && !user.IsSuperAdmin && (user.ExpiresAt == nil || user.IsExpired(now)) {
			value := now.AddDate(1, 0, 0)
			user.ExpiresAt = &value
		}
		user.Status = status
	}
	if user.ExpiresAt != nil && !user.ExpiresAt.After(now) && (input.Status == nil || strings.TrimSpace(*input.Status) == "active") {
		return UserView{}, fmt.Errorf("%w: expires_at must be in the future when user is active", ErrInvalidUser)
	}
	updated, err := s.repository.UpdateUser(ctx, user)
	if err != nil {
		return UserView{}, fmt.Errorf("update user: %w", mapRepositoryConflict(err, ErrUserConflict))
	}
	return userView(updated), nil
}

func (s *UserService) Delete(ctx context.Context, id, currentUserID string) error {
	if strings.TrimSpace(id) != "" && strings.TrimSpace(id) == strings.TrimSpace(currentUserID) {
		return fmt.Errorf("%w: cannot delete yourself", ErrInvalidUser)
	}
	user, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if user.IsSuperAdmin {
		return ErrUserForbidden
	}
	if err := s.repository.DeleteUser(ctx, user); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

func (s *UserService) find(ctx context.Context, id string) (model.User, error) {
	if strings.TrimSpace(id) == "" {
		return model.User{}, ErrUserNotFound
	}
	user, found, err := s.repository.FindUser(ctx, strings.TrimSpace(id))
	if err != nil {
		return model.User{}, fmt.Errorf("find user: %w", err)
	}
	if !found {
		return model.User{}, ErrUserNotFound
	}
	return user, nil
}

func userExpiry(permanent bool, requested *time.Time, now time.Time) (*time.Time, error) {
	if permanent {
		return nil, nil
	}
	value := now.AddDate(1, 0, 0)
	if requested != nil {
		value = requested.UTC()
	}
	if !value.After(now) {
		return nil, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidUser)
	}
	return &value, nil
}

func normalizeUserPage(page, pageSize int) (int, int) {
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

func userView(user model.User) UserView {
	return UserView{
		ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Email: user.Email,
		Status: user.Status, IsSuperAdmin: user.IsSuperAdmin, ExpiresAt: user.ExpiresAt,
		LastLoginAt: user.LastLoginAt, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

func newUserToken() (string, string, error) {
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(bytes)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}
