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
	ErrTemporaryAccessInactive = errors.New("temporary access is not active")
	ErrTemporaryAccessExpiry   = fmt.Errorf("%w: expiry must be within seven days", ErrInvalidTemporaryAccess)
)

// TemporaryAccessRepository atomically persists a temporary-access aggregate.
type TemporaryAccessRepository interface {
	CreateTemporaryAccess(ctx context.Context, input CreateTemporaryAccessInput) (TemporaryAccessResult, error)
	CreateTemporaryAIAccess(ctx context.Context, input CreateTemporaryAIAccessInput) (TemporaryAIAccessResult, error)
	ExtendTemporaryAccess(ctx context.Context, id string, expiresAt time.Time) error
	DisableTemporaryAccess(ctx context.Context, id string, now time.Time) error
	ListTemporaryAccess(ctx context.Context, params TemporaryAccessListParams) (TemporaryAccessPage, error)
	GetTemporaryAccess(ctx context.Context, id string) (TemporaryAccessDetails, error)
	TemporaryConnectionTarget(ctx context.Context, resourceType, resourceID string) (TemporaryConnectionTarget, error)
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

type CreateTemporaryAIAccessInput struct {
	UserID     string
	Name       string
	Remark     string
	CreatedBy  string
	ExpiresAt  *time.Time
	Now        time.Time
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Token      model.AIAccessToken
}

type TemporaryAccessResult struct {
	Account            model.TemporaryAccount
	Grant              model.TemporaryAccountGrant
	ConnectionPassword string
	Details            TemporaryAccessDetails
	ConnectionTarget   TemporaryConnectionTarget
}

type TemporaryAccessListParams struct {
	Query    string
	Page     int
	PageSize int
	Now      time.Time
}

type TemporaryAccessPage struct {
	Items    []TemporaryAccessDetails
	Total    int
	Page     int
	PageSize int
}

type TemporaryAccessDetails struct {
	ID               string
	SessionID        string
	Type             string
	AuthorizedUserID string
	AuthorizedUser   string
	Status           string
	StartsAt         time.Time
	ExpiresAt        *time.Time
	ResourceType     string
	ResourceID       string
	ResourceName     string
	AccountName      string
	Remark           string
	CreatedBy        string
	CreatedAt        time.Time
}

const (
	TemporaryGatewaySSH      = "ssh"
	TemporaryGatewayDatabase = "database"
)

type TemporaryConnectionTarget struct {
	ResourceType      string
	ResourceID        string
	UsernamePrefix    string
	CompactResourceID string
	Protocol          string
	Gateway           string
}

type TemporaryAIAccessResult struct {
	Account      model.TemporaryAccount
	Token        model.AIAccessToken
	AccessToken  string
	RefreshToken string
}

type TemporaryAccessService struct {
	repository TemporaryAccessRepository
}

func NewTemporaryAccessService(repository TemporaryAccessRepository) (*TemporaryAccessService, error) {
	if repository == nil {
		return nil, errors.New("temporary access repository is required")
	}
	return &TemporaryAccessService{repository: repository}, nil
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
	result.Details, err = s.repository.GetTemporaryAccess(ctx, result.Account.ID)
	if err != nil {
		return TemporaryAccessResult{}, fmt.Errorf("load created temporary access: %w", err)
	}
	result.ConnectionTarget, err = s.repository.TemporaryConnectionTarget(ctx, result.Grant.ResourceType, result.Grant.ResourceID)
	if err != nil {
		return TemporaryAccessResult{}, fmt.Errorf("load temporary connection target: %w", err)
	}
	result.ConnectionPassword = issued.Plaintext
	return result, nil
}

func (s *TemporaryAccessService) CreateAI(ctx context.Context, input CreateTemporaryAIAccessInput) (TemporaryAIAccessResult, error) {
	input.UserID = strings.TrimSpace(input.UserID)
	input.Name = strings.TrimSpace(input.Name)
	input.Remark = strings.TrimSpace(input.Remark)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	} else {
		input.Now = input.Now.UTC()
	}
	if input.UserID == "" {
		return TemporaryAIAccessResult{}, fmt.Errorf("%w: user_id is required", ErrInvalidTemporaryAccess)
	}
	if input.Name == "" {
		input.Name = "AI client"
	}
	if input.ExpiresAt != nil {
		expiresAt := input.ExpiresAt.UTC()
		if !expiresAt.After(input.Now) {
			return TemporaryAIAccessResult{}, fmt.Errorf("%w: AI authorization expiry must be in the future", ErrInvalidTemporaryAccess)
		}
		input.ExpiresAt = &expiresAt
	}
	issued, err := IssueAIAccessToken(input.Now, input.AccessTTL, input.RefreshTTL)
	if err != nil {
		return TemporaryAIAccessResult{}, fmt.Errorf("issue AI access token: %w", err)
	}
	input.Token = model.AIAccessToken{
		ID: model.NewID(), UserID: input.UserID, Name: input.Name,
		AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash,
		AccessToken: model.NewEncryptedField(issued.AccessToken), RefreshToken: model.NewEncryptedField(issued.RefreshToken),
		AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt,
	}
	result, err := s.repository.CreateTemporaryAIAccess(ctx, input)
	if err != nil {
		return TemporaryAIAccessResult{}, fmt.Errorf("create temporary AI access aggregate: %w", err)
	}
	result.AccessToken = issued.AccessToken
	result.RefreshToken = issued.RefreshToken
	return result, nil
}

func (s *TemporaryAccessService) Extend(ctx context.Context, id string, expiresAt, now time.Time) (TemporaryAccessDetails, error) {
	id = strings.TrimSpace(id)
	expiresAt = expiresAt.UTC()
	now = now.UTC()
	if id == "" {
		return TemporaryAccessDetails{}, fmt.Errorf("%w: id is required", ErrInvalidTemporaryAccess)
	}
	if err := validateTemporaryAccessExpiry(expiresAt, now); err != nil {
		return TemporaryAccessDetails{}, err
	}
	if err := s.repository.ExtendTemporaryAccess(ctx, id, expiresAt); err != nil {
		return TemporaryAccessDetails{}, fmt.Errorf("extend temporary access aggregate: %w", err)
	}
	details, err := s.repository.GetTemporaryAccess(ctx, id)
	if err != nil {
		return TemporaryAccessDetails{}, fmt.Errorf("load extended temporary access: %w", err)
	}
	return details, nil
}

func (s *TemporaryAccessService) Disable(ctx context.Context, id string, now time.Time) (TemporaryAccessDetails, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TemporaryAccessDetails{}, fmt.Errorf("%w: id is required", ErrInvalidTemporaryAccess)
	}
	if err := s.repository.DisableTemporaryAccess(ctx, id, now.UTC()); err != nil {
		return TemporaryAccessDetails{}, fmt.Errorf("disable temporary access aggregate: %w", err)
	}
	details, err := s.repository.GetTemporaryAccess(ctx, id)
	if err != nil {
		return TemporaryAccessDetails{}, fmt.Errorf("load disabled temporary access: %w", err)
	}
	return details, nil
}

func (s *TemporaryAccessService) List(ctx context.Context, query string, page, pageSize int, now time.Time) (TemporaryAccessPage, error) {
	query = strings.TrimSpace(query)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	result, err := s.repository.ListTemporaryAccess(ctx, TemporaryAccessListParams{
		Query: query, Page: page, PageSize: pageSize, Now: now,
	})
	if err != nil {
		return TemporaryAccessPage{}, fmt.Errorf("list temporary access: %w", err)
	}
	result.Page = page
	result.PageSize = pageSize
	return result, nil
}

func (s *TemporaryAccessService) Get(ctx context.Context, id string) (TemporaryAccessDetails, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TemporaryAccessDetails{}, fmt.Errorf("%w: id is required", ErrInvalidTemporaryAccess)
	}
	result, err := s.repository.GetTemporaryAccess(ctx, id)
	if err != nil {
		return TemporaryAccessDetails{}, fmt.Errorf("get temporary access: %w", err)
	}
	return result, nil
}

func (s *TemporaryAccessService) ConnectionTarget(ctx context.Context, resourceType, resourceID string) (TemporaryConnectionTarget, error) {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" || resourceID == "" {
		return TemporaryConnectionTarget{}, fmt.Errorf("%w: resource_type and resource_id are required", ErrInvalidTemporaryAccess)
	}
	result, err := s.repository.TemporaryConnectionTarget(ctx, resourceType, resourceID)
	if err != nil {
		return TemporaryConnectionTarget{}, fmt.Errorf("get temporary connection target: %w", err)
	}
	return result, nil
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
		return ErrTemporaryAccessExpiry
	}
	return nil
}
