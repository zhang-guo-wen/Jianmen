package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
)

const maxTemporaryAccessDuration = 7 * 24 * time.Hour

var (
	ErrInvalidTemporaryAccess  = errors.New("invalid temporary access")
	ErrTemporaryAccessNotFound = errors.New("temporary access not found")
)

// TemporaryAccessRepository atomically persists a temporary-access aggregate.
type TemporaryAccessRepository interface {
	CreateTemporaryAccess(ctx context.Context, input CreateTemporaryAccessInput) (TemporaryAccessResult, error)
	ExtendTemporaryAccess(ctx context.Context, id string, expiresAt time.Time) error
	DisableTemporaryAccess(ctx context.Context, id string, now time.Time) error
}

// TemporaryAccountRepository preserves the existing AI temporary-account path.
type TemporaryAccountRepository interface {
	CreateTemporaryAccount(ctx context.Context, input CreateTemporaryAccountInput) (model.TemporaryAccount, error)
}

type CreateTemporaryAccessInput struct {
	AuthorizedUserID   string
	ResourceType       string
	ResourceID         string
	ExpiresAt          time.Time
	Remark             string
	CreatedBy          string
	Now                time.Time
	ConnectionPassword model.ConnectionPassword
}

type CreateTemporaryAccountInput struct {
	AccountType      string
	AuthorizedUserID string
	ExpiresAt        *time.Time
	Remark           string
	CreatedBy        string
	SessionID        string
	Now              time.Time
}

type TemporaryAccessResult struct {
	Account            model.TemporaryAccount
	Grant              model.TemporaryAccountGrant
	ConnectionPassword string
}

type TemporaryAccessService struct {
	repository     TemporaryAccessRepository
	accountCreator TemporaryAccountRepository
}

func NewTemporaryAccessService(repository TemporaryAccessRepository) (*TemporaryAccessService, error) {
	if repository == nil {
		return nil, errors.New("temporary access repository is required")
	}
	service := &TemporaryAccessService{repository: repository}
	if creator, ok := repository.(TemporaryAccountRepository); ok {
		service.accountCreator = creator
	}
	return service, nil
}

func (s *TemporaryAccessService) Create(ctx context.Context, input CreateTemporaryAccessInput) (TemporaryAccessResult, error) {
	input = normalizeTemporaryAccessInput(input)
	if err := validateTemporaryAccessInput(input); err != nil {
		return TemporaryAccessResult{}, err
	}
	issued, err := IssueConnectionPassword(input.Now, input.ExpiresAt.Sub(input.Now))
	if err != nil {
		return TemporaryAccessResult{}, fmt.Errorf("issue temporary connection password: %w", err)
	}
	input.ConnectionPassword = model.ConnectionPassword{
		UserID: input.AuthorizedUserID, ResourceType: input.ResourceType, ResourceID: input.ResourceID,
		SecretHash: issued.Hash, MySQLNativeHash: issued.MySQLNativeHash, ExpiresAt: issued.ExpiresAt,
	}
	result, err := s.repository.CreateTemporaryAccess(ctx, input)
	if err != nil {
		return TemporaryAccessResult{}, fmt.Errorf("create temporary access aggregate: %w", err)
	}
	result.ConnectionPassword = issued.Plaintext
	return result, nil
}

func (s *TemporaryAccessService) Extend(ctx context.Context, id string, expiresAt, now time.Time) error {
	id = strings.TrimSpace(id)
	expiresAt = expiresAt.UTC()
	now = now.UTC()
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidTemporaryAccess)
	}
	if err := validateTemporaryAccessExpiry(expiresAt, now); err != nil {
		return err
	}
	if err := s.repository.ExtendTemporaryAccess(ctx, id, expiresAt); err != nil {
		return fmt.Errorf("extend temporary access aggregate: %w", err)
	}
	return nil
}

func (s *TemporaryAccessService) Disable(ctx context.Context, id string, now time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidTemporaryAccess)
	}
	if err := s.repository.DisableTemporaryAccess(ctx, id, now.UTC()); err != nil {
		return fmt.Errorf("disable temporary access aggregate: %w", err)
	}
	return nil
}

func (s *TemporaryAccessService) CreateAccount(ctx context.Context, input CreateTemporaryAccountInput) (model.TemporaryAccount, error) {
	if s.accountCreator == nil {
		return model.TemporaryAccount{}, errors.New("temporary account creation is not supported")
	}
	input.AccountType = strings.TrimSpace(input.AccountType)
	input.AuthorizedUserID = strings.TrimSpace(input.AuthorizedUserID)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	input.Remark = strings.TrimSpace(input.Remark)
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	if input.AccountType == "" {
		return model.TemporaryAccount{}, fmt.Errorf("%w: account type is required", ErrInvalidTemporaryAccess)
	}
	account, err := s.accountCreator.CreateTemporaryAccount(ctx, input)
	if err != nil {
		return model.TemporaryAccount{}, fmt.Errorf("create temporary account: %w", err)
	}
	return account, nil
}

func normalizeTemporaryAccessInput(input CreateTemporaryAccessInput) CreateTemporaryAccessInput {
	input.AuthorizedUserID = strings.TrimSpace(input.AuthorizedUserID)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.Remark = strings.TrimSpace(input.Remark)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	input.ExpiresAt = input.ExpiresAt.UTC()
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	} else {
		input.Now = input.Now.UTC()
	}
	return input
}

func validateTemporaryAccessInput(input CreateTemporaryAccessInput) error {
	if input.AuthorizedUserID == "" || input.ResourceID == "" {
		return fmt.Errorf("%w: authorized_user_id and resource_id are required", ErrInvalidTemporaryAccess)
	}
	if input.ResourceType != model.ResourceTypeHostAccount && input.ResourceType != model.ResourceTypeDatabaseAccount {
		return fmt.Errorf("%w: resource_type must be host_account or database_account", ErrInvalidTemporaryAccess)
	}
	return validateTemporaryAccessExpiry(input.ExpiresAt, input.Now)
}

func validateTemporaryAccessExpiry(expiresAt, now time.Time) error {
	if !expiresAt.After(now) || expiresAt.After(now.Add(maxTemporaryAccessDuration)) {
		return fmt.Errorf("%w: expiry must be within seven days", ErrInvalidTemporaryAccess)
	}
	return nil
}
