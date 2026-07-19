package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/util"
)

var (
	ErrUserSessionTargetNotFound   = errors.New("user session target account not found or disabled")
	ErrUserSessionHostInactive     = errors.New("user session host is disabled or not found")
	ErrUserSessionDatabaseInactive = errors.New("user session database instance is disabled")
	ErrUserSessionForbidden        = errors.New("user session connection forbidden")
	ErrUserSessionTargetLookup     = errors.New("user session target lookup failed")
	ErrUserSessionAuthorization    = errors.New("user session authorization failed")
)

// UserSessionCreationRepository is owned by the session-creation use case.
// Its methods deliberately receive context first so cancellation and deadlines
// reach the storage boundary.
type UserSessionCreationRepository interface {
	FindActiveHostAccount(context.Context, string) (model.HostAccount, bool, error)
	FindActiveHost(context.Context, string) (model.Host, bool, error)
	FindActiveDatabaseAccount(context.Context, string) (model.DatabaseAccount, bool, error)
	GetOrCreateActivePermanentUserSession(context.Context, string) (model.UserSession, error)
}

// UserSessionConnectionAuthorizer is the narrow authorization dependency for
// creating connection configuration sessions.
type UserSessionConnectionAuthorizer interface {
	AuthorizeConnection(context.Context, string, []string, string, string) (bool, error)
}

type UserSessionCreationService struct {
	repository UserSessionCreationRepository
	authorizer UserSessionConnectionAuthorizer
}

type CreateUserSessionRequest struct {
	UserID   string
	TargetID string
}

type CreateUserSessionResult struct {
	Session         model.UserSession
	ResourceID      string
	ResourceType    string
	CompactUsername string
}

func NewUserSessionCreationService(repository UserSessionCreationRepository, authorizer UserSessionConnectionAuthorizer) (*UserSessionCreationService, error) {
	if isNilUserSessionCreationDependency(repository) {
		return nil, errors.New("user session creation repository is required")
	}
	if isNilUserSessionCreationDependency(authorizer) {
		return nil, errors.New("user session creation authorizer is required")
	}
	return &UserSessionCreationService{repository: repository, authorizer: authorizer}, nil
}

func (s *UserSessionCreationService) Create(ctx context.Context, request CreateUserSessionRequest) (CreateUserSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return CreateUserSessionResult{}, fmt.Errorf("create user session context: %w", err)
	}
	userID := strings.TrimSpace(request.UserID)
	targetID := strings.TrimSpace(request.TargetID)
	if userID == "" {
		return CreateUserSessionResult{}, fmt.Errorf("user_id is required")
	}
	if targetID == "" {
		return CreateUserSessionResult{}, fmt.Errorf("target_id is required")
	}

	resourceID, resourceType, prefix, actions, err := s.resolveTarget(ctx, targetID)
	if err != nil {
		return CreateUserSessionResult{}, err
	}
	allowed, err := s.authorizer.AuthorizeConnection(ctx, userID, actions, resourceType, targetID)
	if err != nil {
		return CreateUserSessionResult{}, fmt.Errorf("%w: %w", ErrUserSessionAuthorization, err)
	}
	if !allowed {
		return CreateUserSessionResult{}, ErrUserSessionForbidden
	}

	session, err := s.GetOrCreateActivePermanentUserSession(ctx, userID)
	if err != nil {
		return CreateUserSessionResult{}, err
	}
	return CreateUserSessionResult{
		Session:         session,
		ResourceID:      resourceID,
		ResourceType:    resourceType,
		CompactUsername: prefix + resourceID + session.SessionID,
	}, nil
}

// GetOrCreateActivePermanentUserSession is the shared allocation boundary for
// every connection-configuration entry point, including the AI resource API.
func (s *UserSessionCreationService) GetOrCreateActivePermanentUserSession(ctx context.Context, userID string) (model.UserSession, error) {
	if err := ctx.Err(); err != nil {
		return model.UserSession{}, fmt.Errorf("get or create permanent user session context: %w", err)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return model.UserSession{}, fmt.Errorf("user_id is required")
	}
	session, err := s.repository.GetOrCreateActivePermanentUserSession(ctx, userID)
	if err != nil {
		return model.UserSession{}, fmt.Errorf("get or create permanent user session: %w", err)
	}
	return session, nil
}

func isNilUserSessionCreationDependency(dependency any) bool {
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

func (s *UserSessionCreationService) resolveTarget(ctx context.Context, targetID string) (resourceID, resourceType, prefix string, actions []string, err error) {
	hostAccount, found, err := s.repository.FindActiveHostAccount(ctx, targetID)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("%w: host account: %w", ErrUserSessionTargetLookup, err)
	}
	if found {
		if _, hostFound, err := s.repository.FindActiveHost(ctx, hostAccount.HostID); err != nil {
			return "", "", "", nil, fmt.Errorf("%w: host: %w", ErrUserSessionTargetLookup, err)
		} else if !hostFound {
			return "", "", "", nil, ErrUserSessionHostInactive
		}
		return hostAccount.ResourceID, model.ResourceTypeHostAccount, util.PrefixHost,
			[]string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}, nil
	}

	databaseAccount, found, err := s.repository.FindActiveDatabaseAccount(ctx, targetID)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("%w: database account: %w", ErrUserSessionTargetLookup, err)
	}
	if !found {
		return "", "", "", nil, ErrUserSessionTargetNotFound
	}
	if databaseAccount.Instance.Status == "disabled" || databaseAccount.Instance.ID == "" {
		return "", "", "", nil, ErrUserSessionDatabaseInactive
	}
	prefix = util.PrefixDatabase
	if strings.EqualFold(databaseAccount.Instance.Protocol, "redis") {
		prefix = util.PrefixRedis
	}
	return databaseAccount.ResourceID, model.ResourceTypeDatabaseAccount, prefix,
		[]string{rbac.ActionDBConnect}, nil
}
