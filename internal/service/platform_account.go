package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

var (
	ErrPlatformAccountForbidden   = errors.New("platform account access forbidden")
	ErrPlatformAccountUnavailable = errors.New("platform account is unavailable")
	ErrInvalidPlatformAccount     = errors.New("invalid platform account")
)

// PlatformAccountRepository owns only platform-account persistence. Metadata
// methods must never populate Password; password retrieval is deliberately a
// separate operation that is called only after use authorization succeeds.
type PlatformAccountRepository interface {
	ListPlatformAccountMetadata(context.Context, string, string) ([]model.PlatformAccount, error)
	GetPlatformAccountMetadata(context.Context, string) (model.PlatformAccount, error)
	CreateManagedPlatformAccount(context.Context, model.PlatformAccount, string) (model.PlatformAccount, error)
	UpdateManagedPlatformAccount(context.Context, string, model.PlatformAccount) (model.PlatformAccount, error)
	DeleteManagedPlatformAccount(context.Context, string) error
	GetPlatformAccountPassword(context.Context, string) (string, error)
}

type PlatformAccountAuthorizer interface {
	AuthorizeConnection(context.Context, string, []string, string, string) (bool, error)
	AuthorizeBatch(context.Context, string, []AuthorizationRequest) ([]AuthorizationDecision, error)
}

type PlatformAccountActor struct {
	UserID     string
	SuperAdmin bool
}

type PlatformAccountRequest struct {
	Name, PlatformName, URL, Group, Username, Password, Remark, Status string
	ExpiresAt                                                          *string
}

type PlatformAccount struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	PlatformName string     `json:"platform_name"`
	URL          string     `json:"url,omitempty"`
	Group        string     `json:"group,omitempty"`
	Username     string     `json:"username"`
	HasPassword  bool       `json:"has_password"`
	Remark       string     `json:"remark,omitempty"`
	OwnerID      string     `json:"owner_id"`
	OwnerName    string     `json:"owner_name,omitempty"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

type PlatformAccountService struct {
	repository PlatformAccountRepository
	authorizer PlatformAccountAuthorizer
	now        func() time.Time
}

func NewPlatformAccountService(repository PlatformAccountRepository, authorizer PlatformAccountAuthorizer) (*PlatformAccountService, error) {
	if repository == nil {
		return nil, errors.New("platform account repository is required")
	}
	if authorizer == nil {
		return nil, errors.New("platform account authorizer is required")
	}
	return &PlatformAccountService{repository: repository, authorizer: authorizer, now: time.Now}, nil
}

func (s *PlatformAccountService) List(ctx context.Context, actor PlatformAccountActor, search, platform string) ([]PlatformAccount, error) {
	if strings.TrimSpace(actor.UserID) == "" {
		return nil, ErrPlatformAccountForbidden
	}
	records, err := s.repository.ListPlatformAccountMetadata(ctx, strings.TrimSpace(search), strings.TrimSpace(platform))
	if err != nil {
		return nil, fmt.Errorf("list platform accounts: %w", err)
	}
	if actor.SuperAdmin {
		return platformAccountViews(records), nil
	}
	requests := make([]AuthorizationRequest, len(records))
	for i := range records {
		requests[i] = platformAccountAuthorizationRequest(rbac.ActionPlatformAccountView, records[i].ID)
	}
	decisions, err := s.authorizer.AuthorizeBatch(ctx, actor.UserID, requests)
	if err != nil {
		return nil, fmt.Errorf("authorize platform account list: %w", err)
	}
	if len(decisions) != len(records) {
		return nil, errors.New("authorize platform account list: decision count mismatch")
	}
	visible := make([]PlatformAccount, 0, len(records))
	for i := range records {
		if decisions[i].Allowed {
			visible = append(visible, platformAccountView(records[i]))
		}
	}
	return visible, nil
}

func (s *PlatformAccountService) Get(ctx context.Context, actor PlatformAccountActor, id string) (PlatformAccount, error) {
	if err := s.authorize(ctx, actor, rbac.ActionPlatformAccountView, id); err != nil {
		return PlatformAccount{}, err
	}
	record, err := s.repository.GetPlatformAccountMetadata(ctx, strings.TrimSpace(id))
	if err != nil {
		return PlatformAccount{}, fmt.Errorf("get platform account: %w", err)
	}
	return platformAccountView(record), nil
}

func (s *PlatformAccountService) Create(ctx context.Context, actor PlatformAccountActor, request PlatformAccountRequest) (PlatformAccount, error) {
	if err := s.authorize(ctx, actor, rbac.ActionPlatformAccountCreate, ""); err != nil {
		return PlatformAccount{}, err
	}
	input, err := platformAccountInput(request, actor.UserID, true)
	if err != nil {
		return PlatformAccount{}, err
	}
	creatorID := actor.UserID
	if actor.SuperAdmin {
		creatorID = ""
	}
	record, err := s.repository.CreateManagedPlatformAccount(ctx, input, creatorID)
	if err != nil {
		return PlatformAccount{}, fmt.Errorf("create platform account: %w", err)
	}
	return platformAccountView(record), nil
}

func (s *PlatformAccountService) Update(ctx context.Context, actor PlatformAccountActor, id string, request PlatformAccountRequest) (PlatformAccount, error) {
	if err := s.authorize(ctx, actor, rbac.ActionPlatformAccountUpdate, id); err != nil {
		return PlatformAccount{}, err
	}
	input, err := platformAccountInput(request, "", false)
	if err != nil {
		return PlatformAccount{}, err
	}
	record, err := s.repository.UpdateManagedPlatformAccount(ctx, strings.TrimSpace(id), input)
	if err != nil {
		return PlatformAccount{}, fmt.Errorf("update platform account: %w", err)
	}
	return platformAccountView(record), nil
}

func (s *PlatformAccountService) Delete(ctx context.Context, actor PlatformAccountActor, id string) error {
	if err := s.authorize(ctx, actor, rbac.ActionPlatformAccountDelete, id); err != nil {
		return err
	}
	if err := s.repository.DeleteManagedPlatformAccount(ctx, strings.TrimSpace(id)); err != nil {
		return fmt.Errorf("delete platform account: %w", err)
	}
	return nil
}

func (s *PlatformAccountService) Password(ctx context.Context, actor PlatformAccountActor, id string) (string, error) {
	if err := s.authorize(ctx, actor, rbac.ActionPlatformAccountUse, id); err != nil {
		return "", err
	}
	record, err := s.repository.GetPlatformAccountMetadata(ctx, strings.TrimSpace(id))
	if err != nil {
		return "", fmt.Errorf("get platform account for password: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(record.Status)) != "active" || (record.ExpiresAt != nil && !record.ExpiresAt.After(s.now().UTC())) {
		return "", ErrPlatformAccountUnavailable
	}
	password, err := s.repository.GetPlatformAccountPassword(ctx, record.ID)
	if err != nil {
		return "", fmt.Errorf("get platform account password: %w", err)
	}
	return password, nil
}

func (s *PlatformAccountService) authorize(ctx context.Context, actor PlatformAccountActor, action, id string) error {
	actor.UserID = strings.TrimSpace(actor.UserID)
	id = strings.TrimSpace(id)
	if actor.UserID == "" {
		return ErrPlatformAccountForbidden
	}
	if actor.SuperAdmin {
		return nil
	}
	resourceType := ""
	if id != "" {
		resourceType = model.ResourceTypePlatformAccount
	}
	allowed, err := s.authorizer.AuthorizeConnection(ctx, actor.UserID, []string{action}, resourceType, id)
	if err != nil {
		return fmt.Errorf("authorize platform account: %w", err)
	}
	if !allowed {
		return ErrPlatformAccountForbidden
	}
	return nil
}

func platformAccountAuthorizationRequest(action, id string) AuthorizationRequest {
	return AuthorizationRequest{Actions: []string{action}, ResourceType: model.ResourceTypePlatformAccount, ResourceID: strings.TrimSpace(id)}
}

func platformAccountInput(request PlatformAccountRequest, ownerID string, creating bool) (model.PlatformAccount, error) {
	expiresAt, err := parsePlatformAccountExpiry(request.ExpiresAt)
	if err != nil {
		return model.PlatformAccount{}, err
	}
	username := strings.TrimSpace(request.Username)
	if creating && username == "" {
		return model.PlatformAccount{}, fmt.Errorf("%w: username is required", ErrInvalidPlatformAccount)
	}
	platformName := strings.TrimSpace(request.PlatformName)
	url := strings.TrimSpace(request.URL)
	if platformName == "" {
		platformName = url
	}
	return model.PlatformAccount{
		Name: strings.TrimSpace(request.Name), PlatformName: platformName, URL: url,
		GroupName: strings.TrimSpace(request.Group), Username: username,
		Password: model.NewEncryptedField(request.Password), Remark: strings.TrimSpace(request.Remark),
		OwnerID: strings.TrimSpace(ownerID), Status: strings.TrimSpace(request.Status), ExpiresAt: expiresAt,
	}, nil
}

func parsePlatformAccountExpiry(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*value))
	if err != nil {
		return nil, fmt.Errorf("%w: expires_at must be RFC3339", ErrInvalidPlatformAccount)
	}
	return &expiresAt, nil
}

func platformAccountViews(records []model.PlatformAccount) []PlatformAccount {
	views := make([]PlatformAccount, len(records))
	for i := range records {
		views[i] = platformAccountView(records[i])
	}
	return views
}

func platformAccountView(record model.PlatformAccount) PlatformAccount {
	return PlatformAccount{
		ID: record.ID, Name: record.Name, PlatformName: record.PlatformName, URL: record.URL,
		Group: record.GroupName, Username: record.Username, HasPassword: record.HasPassword,
		Remark: record.Remark, OwnerID: record.OwnerID, OwnerName: record.Owner.Username,
		Status: record.Status, ExpiresAt: record.ExpiresAt,
		CreatedAt: record.CreatedAt.Format(time.RFC3339), UpdatedAt: record.UpdatedAt.Format(time.RFC3339),
	}
}
