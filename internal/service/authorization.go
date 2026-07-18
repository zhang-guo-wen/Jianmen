package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	AuthorizationReasonAllowed         = "allowed"
	AuthorizationReasonSuperAdmin      = "super_admin"
	AuthorizationReasonMissingUser     = "missing_user"
	AuthorizationReasonInvalidIdentity = "invalid_identity"
	AuthorizationReasonActionDenied    = "action_denied"
	AuthorizationReasonInvalidResource = "invalid_resource"
	AuthorizationReasonResourceDenied  = "resource_denied"
)

type AuthorizationRequest struct {
	UserID       string
	Actions      []string
	ResourceType string
	ResourceID   string
}

type AuthorizationDecision struct {
	Allowed bool
	Reason  string
}

type AuthorizationIdentity interface {
	FindIdentitySubject(ctx context.Context, userID string) (IdentitySubject, bool, error)
}

type ActionAuthorizer interface {
	HasPermissionContext(
		ctx context.Context,
		userID string,
		action string,
		resourceType string,
		resourceID string,
	) (bool, error)
}

type ResourceAuthorizer interface {
	HasGrantContext(ctx context.Context, userID, resourceType, resourceID string) (bool, error)
}

type AuthorizationService struct {
	identity  AuthorizationIdentity
	actions   ActionAuthorizer
	resources ResourceAuthorizer
}

func NewAuthorizationService(
	identity AuthorizationIdentity,
	actions ActionAuthorizer,
	resources ResourceAuthorizer,
) (*AuthorizationService, error) {
	switch {
	case identity == nil:
		return nil, errors.New("authorization identity is required")
	case actions == nil:
		return nil, errors.New("action authorizer is required")
	case resources == nil:
		return nil, errors.New("resource authorizer is required")
	default:
		return &AuthorizationService{
			identity:  identity,
			actions:   actions,
			resources: resources,
		}, nil
	}
}

func (s *AuthorizationService) Authorize(
	ctx context.Context,
	request AuthorizationRequest,
) (AuthorizationDecision, error) {
	userID := strings.TrimSpace(request.UserID)
	if userID == "" {
		return AuthorizationDecision{Reason: AuthorizationReasonMissingUser}, nil
	}
	subject, found, err := s.identity.FindIdentitySubject(ctx, userID)
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("authorize identity: %w", err)
	}
	if !found {
		return AuthorizationDecision{Reason: AuthorizationReasonInvalidIdentity}, nil
	}
	if subject.SuperAdmin {
		return AuthorizationDecision{
			Allowed: true,
			Reason:  AuthorizationReasonSuperAdmin,
		}, nil
	}

	allowedAction := false
	for _, action := range normalizedActions(request.Actions) {
		allowed, err := s.actions.HasPermissionContext(ctx, subject.ID, action, "", "")
		if err != nil {
			return AuthorizationDecision{}, fmt.Errorf("authorize action %q: %w", action, err)
		}
		if allowed {
			allowedAction = true
			break
		}
	}
	if !allowedAction {
		return AuthorizationDecision{Reason: AuthorizationReasonActionDenied}, nil
	}

	resourceType := strings.TrimSpace(request.ResourceType)
	resourceID := strings.TrimSpace(request.ResourceID)
	if (resourceType == "") != (resourceID == "") {
		return AuthorizationDecision{Reason: AuthorizationReasonInvalidResource}, nil
	}
	if resourceType != "" {
		allowed, err := s.resources.HasGrantContext(ctx, subject.ID, resourceType, resourceID)
		if err != nil {
			return AuthorizationDecision{}, fmt.Errorf("authorize resource: %w", err)
		}
		if !allowed {
			return AuthorizationDecision{Reason: AuthorizationReasonResourceDenied}, nil
		}
	}
	return AuthorizationDecision{
		Allowed: true,
		Reason:  AuthorizationReasonAllowed,
	}, nil
}

func normalizedActions(actions []string) []string {
	normalized := make([]string, 0, len(actions))
	seen := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		action = strings.TrimSpace(action)
		if action == "" {
			continue
		}
		if _, exists := seen[action]; exists {
			continue
		}
		seen[action] = struct{}{}
		normalized = append(normalized, action)
	}
	return normalized
}
