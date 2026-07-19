package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/util"
)

const defaultConnectionPasswordTTL = 30 * time.Minute

var (
	ErrInvalidConnectionPasswordRequest = errors.New("invalid connection password request")
	ErrConnectionPasswordTargetNotFound = errors.New("connection password target account not found or disabled")
	ErrConnectionPasswordTargetLookup   = errors.New("connection password target lookup failed")
	ErrConnectionPasswordForbidden      = errors.New("connection password forbidden")
	ErrConnectionPasswordAuthorization  = errors.New("connection password authorization failed")
	ErrConnectionPasswordGeneration     = errors.New("connection password generation failed")
	ErrConnectionPasswordPersistence    = errors.New("connection password persistence failed")
)

type IssuedConnectionPassword struct {
	Plaintext       string
	Hash            string
	ExpiresAt       time.Time
	MySQLNativeHash string
}

type ConnectionPasswordRepository interface {
	FindActiveHostAccount(context.Context, string) (model.HostAccount, bool, error)
	FindActiveHost(context.Context, string) (model.Host, bool, error)
	FindActiveDatabaseAccount(context.Context, string) (model.DatabaseAccount, bool, error)
	CreateConnectionPassword(context.Context, model.ConnectionPassword) error
}

type ConnectionPasswordAuthorizer interface {
	AuthorizeConnection(context.Context, string, []string, string, string) (bool, error)
}

type ConnectionPasswordService struct {
	repository ConnectionPasswordRepository
	authorizer ConnectionPasswordAuthorizer
	now        func() time.Time
	ttl        time.Duration
}

type ConnectionPasswordIssueRequest struct {
	UserID               string
	TargetID             string
	ExpectedResourceType string
}

type ConnectionPasswordIssueResult struct {
	Password         string
	ExpiresAt        time.Time
	ExpiresInSeconds int
	Reusable         bool
}

func NewConnectionPasswordService(
	repository ConnectionPasswordRepository,
	authorizer ConnectionPasswordAuthorizer,
) (*ConnectionPasswordService, error) {
	if isNilConnectionPasswordDependency(repository) {
		return nil, errors.New("connection password repository is required")
	}
	if isNilConnectionPasswordDependency(authorizer) {
		return nil, errors.New("connection password authorizer is required")
	}
	return &ConnectionPasswordService{
		repository: repository,
		authorizer: authorizer,
		now:        time.Now,
		ttl:        defaultConnectionPasswordTTL,
	}, nil
}

func (s *ConnectionPasswordService) Issue(
	ctx context.Context,
	request ConnectionPasswordIssueRequest,
) (ConnectionPasswordIssueResult, error) {
	if ctx == nil {
		return ConnectionPasswordIssueResult{},
			fmt.Errorf("%w: nil context", ErrInvalidConnectionPasswordRequest)
	}
	if err := ctx.Err(); err != nil {
		return ConnectionPasswordIssueResult{},
			fmt.Errorf("issue connection password context: %w", err)
	}
	userID := strings.TrimSpace(request.UserID)
	targetID := strings.TrimSpace(request.TargetID)
	if userID == "" || targetID == "" {
		return ConnectionPasswordIssueResult{}, ErrInvalidConnectionPasswordRequest
	}

	now := s.now().UTC()
	resourceType, actions, err := s.resolveTarget(ctx, targetID, now)
	if err != nil {
		return ConnectionPasswordIssueResult{}, err
	}
	expectedResourceType := strings.TrimSpace(request.ExpectedResourceType)
	if expectedResourceType != "" && expectedResourceType != resourceType {
		return ConnectionPasswordIssueResult{}, ErrConnectionPasswordTargetNotFound
	}
	allowed, err := s.authorizer.AuthorizeConnection(
		ctx,
		userID,
		actions,
		resourceType,
		targetID,
	)
	if err != nil {
		return ConnectionPasswordIssueResult{},
			fmt.Errorf("%w: %w", ErrConnectionPasswordAuthorization, err)
	}
	if !allowed {
		return ConnectionPasswordIssueResult{}, ErrConnectionPasswordForbidden
	}
	if err := ctx.Err(); err != nil {
		return ConnectionPasswordIssueResult{},
			fmt.Errorf("issue connection password context: %w", err)
	}

	issued, err := IssueConnectionPassword(now, s.ttl)
	if err != nil {
		return ConnectionPasswordIssueResult{},
			fmt.Errorf("%w: %w", ErrConnectionPasswordGeneration, err)
	}
	credential := model.ConnectionPassword{
		UserID:          userID,
		ResourceType:    resourceType,
		ResourceID:      targetID,
		SecretHash:      issued.Hash,
		MySQLNativeHash: issued.MySQLNativeHash,
		ExpiresAt:       issued.ExpiresAt,
	}
	if err := s.repository.CreateConnectionPassword(ctx, credential); err != nil {
		return ConnectionPasswordIssueResult{},
			fmt.Errorf("%w: %w", ErrConnectionPasswordPersistence, err)
	}
	return ConnectionPasswordIssueResult{
		Password:         issued.Plaintext,
		ExpiresAt:        issued.ExpiresAt,
		ExpiresInSeconds: int(s.ttl.Seconds()),
		Reusable:         true,
	}, nil
}

func (s *ConnectionPasswordService) resolveTarget(
	ctx context.Context,
	targetID string,
	now time.Time,
) (string, []string, error) {
	hostAccount, found, err := s.repository.FindActiveHostAccount(ctx, targetID)
	if err != nil {
		return "", nil,
			fmt.Errorf("%w: host account: %w", ErrConnectionPasswordTargetLookup, err)
	}
	if found {
		_, hostFound, err := s.repository.FindActiveHost(ctx, hostAccount.HostID)
		if err != nil {
			return "", nil,
				fmt.Errorf("%w: host: %w", ErrConnectionPasswordTargetLookup, err)
		}
		if !hostFound ||
			(hostAccount.ExpiresAt != nil && !now.Before(hostAccount.ExpiresAt.UTC())) {
			return "", nil, ErrConnectionPasswordTargetNotFound
		}
		return model.ResourceTypeHostAccount,
			[]string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect},
			nil
	}

	databaseAccount, found, err := s.repository.FindActiveDatabaseAccount(ctx, targetID)
	if err != nil {
		return "", nil,
			fmt.Errorf("%w: database account: %w", ErrConnectionPasswordTargetLookup, err)
	}
	if !found ||
		databaseAccount.Instance.ID == "" ||
		databaseAccount.Instance.Status == "disabled" ||
		(databaseAccount.ExpiresAt != nil && !now.Before(databaseAccount.ExpiresAt.UTC())) {
		return "", nil, ErrConnectionPasswordTargetNotFound
	}
	return model.ResourceTypeDatabaseAccount, []string{rbac.ActionDBConnect}, nil
}

func isNilConnectionPasswordDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func IssueConnectionPassword(now time.Time, ttl time.Duration) (IssuedConnectionPassword, error) {
	if ttl <= 0 {
		return IssuedConnectionPassword{}, fmt.Errorf("connection password ttl must be positive")
	}
	secretBytes := make([]byte, 24)
	if _, err := rand.Read(secretBytes); err != nil {
		return IssuedConnectionPassword{}, fmt.Errorf("generate connection password: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(secretBytes)
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return IssuedConnectionPassword{}, fmt.Errorf("hash connection password: %w", err)
	}
	return IssuedConnectionPassword{
		Plaintext:       plaintext,
		Hash:            string(hash),
		ExpiresAt:       now.UTC().Add(ttl),
		MySQLNativeHash: util.MySQLNativePasswordHash(plaintext),
	}, nil
}
