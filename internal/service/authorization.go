package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jianmen/internal/rbac"
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
	HasDenyContext(
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
	if err := ctx.Err(); err != nil {
		return AuthorizationDecision{}, fmt.Errorf("authorization context: %w", err)
	}
	userID := strings.TrimSpace(request.UserID)
	if userID == "" {
		return AuthorizationDecision{Reason: AuthorizationReasonMissingUser}, nil
	}
	subject, found, err := s.identity.FindIdentitySubject(ctx, userID)
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("authorize identity: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return AuthorizationDecision{}, fmt.Errorf("authorization context: %w", err)
	}
	if !found {
		return AuthorizationDecision{Reason: AuthorizationReasonInvalidIdentity}, nil
	}
	resourceType := strings.TrimSpace(request.ResourceType)
	resourceID := strings.TrimSpace(request.ResourceID)
	if (resourceType == "") != (resourceID == "") {
		return AuthorizationDecision{Reason: AuthorizationReasonInvalidResource}, nil
	}
	if subject.SuperAdmin {
		return AuthorizationDecision{
			Allowed: true,
			Reason:  AuthorizationReasonSuperAdmin,
		}, nil
	}

	allowedAction := false
	resourcePermissionDenied := false
	for _, action := range normalizedActions(request.Actions) {
		allowed, err := s.actions.HasPermissionContext(ctx, subject.ID, action, "", "")
		if err != nil {
			return AuthorizationDecision{}, fmt.Errorf("authorize action %q: %w", action, err)
		}
		if err := ctx.Err(); err != nil {
			return AuthorizationDecision{}, fmt.Errorf("authorization context: %w", err)
		}
		if !allowed {
			continue
		}
		if resourceType != "" {
			denied, err := s.actions.HasDenyContext(ctx, subject.ID, action, resourceType, resourceID)
			if err != nil {
				return AuthorizationDecision{}, fmt.Errorf("authorize resource deny %q: %w", action, err)
			}
			if err := ctx.Err(); err != nil {
				return AuthorizationDecision{}, fmt.Errorf("authorization context: %w", err)
			}
			if denied {
				resourcePermissionDenied = true
				continue
			}
		}
		allowedAction = true
		break
	}
	if !allowedAction {
		if resourcePermissionDenied {
			return AuthorizationDecision{Reason: AuthorizationReasonResourceDenied}, nil
		}
		return AuthorizationDecision{Reason: AuthorizationReasonActionDenied}, nil
	}

	if resourceType != "" {
		allowed, err := s.resources.HasGrantContext(ctx, subject.ID, resourceType, resourceID)
		if err != nil {
			return AuthorizationDecision{}, fmt.Errorf("authorize resource: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return AuthorizationDecision{}, fmt.Errorf("authorization context: %w", err)
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

// AuthorizeConnection is the protocol-adapter boundary. Its primitive signature
// lets protocol packages depend on the authorization contract without importing
// service-owned DTOs (some service code also reuses protocol helpers).
func (s *AuthorizationService) AuthorizeConnection(
	ctx context.Context,
	userID string,
	actions []string,
	resourceType string,
	resourceID string,
) (bool, error) {
	decision, err := s.Authorize(ctx, AuthorizationRequest{
		UserID:       userID,
		Actions:      actions,
		ResourceType: resourceType,
		ResourceID:   resourceID,
	})
	if err != nil {
		return false, err
	}
	return decision.Allowed, nil
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

type BatchActionAuthorizer interface {
	BatchActionDecisionsContext(context.Context, string, []rbac.BatchAuthorizationRequest) (map[string]rbac.BatchActionDecision, error)
}

type BatchResourceAuthorizer interface {
	BatchGrantsContext(context.Context, string, []rbac.BatchAuthorizationRequest) (map[string]bool, error)
}

// AuthorizeBatch resolves the identity once and authorizes every concrete
// resource from bounded authorization datasets. Empty input never reaches RBAC.
func (s *AuthorizationService) AuthorizeBatch(ctx context.Context, userID string, requests []AuthorizationRequest) ([]AuthorizationDecision, error) {
	decisions := make([]AuthorizationDecision, len(requests))
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("authorization context: %w", err)
	}
	if len(requests) == 0 {
		return decisions, nil
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		for i := range decisions {
			decisions[i] = AuthorizationDecision{Reason: AuthorizationReasonMissingUser}
		}
		return decisions, nil
	}
	subject, found, err := s.identity.FindIdentitySubject(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("authorize identity: %w", err)
	}
	if !found {
		for i := range decisions {
			decisions[i] = AuthorizationDecision{Reason: AuthorizationReasonInvalidIdentity}
		}
		return decisions, nil
	}
	batch := make([]rbac.BatchAuthorizationRequest, 0, len(requests))
	valid := make([]bool, len(requests))
	for i, request := range requests {
		resourceType := strings.TrimSpace(request.ResourceType)
		resourceID := strings.TrimSpace(request.ResourceID)
		if resourceType == "" || resourceID == "" {
			decisions[i] = AuthorizationDecision{Reason: AuthorizationReasonInvalidResource}
			continue
		}
		actions := normalizedActions(request.Actions)
		if len(actions) == 0 {
			decisions[i] = AuthorizationDecision{Reason: AuthorizationReasonActionDenied}
			continue
		}
		valid[i] = true
		batch = append(batch, rbac.BatchAuthorizationRequest{ResourceType: resourceType, ResourceID: resourceID, Actions: actions})
	}
	if subject.SuperAdmin {
		for i := range decisions {
			if valid[i] {
				decisions[i] = AuthorizationDecision{Allowed: true, Reason: AuthorizationReasonSuperAdmin}
			}
		}
		return decisions, nil
	}
	actions, ok := s.actions.(BatchActionAuthorizer)
	if !ok {
		return nil, errors.New("batch action authorizer is required")
	}
	resources, ok := s.resources.(BatchResourceAuthorizer)
	if !ok {
		return nil, errors.New("batch resource authorizer is required")
	}
	actionDecisions, err := actions.BatchActionDecisionsContext(ctx, subject.ID, batch)
	if err != nil {
		return nil, fmt.Errorf("batch authorize actions: %w", err)
	}
	grantDecisions, err := resources.BatchGrantsContext(ctx, subject.ID, batch)
	if err != nil {
		return nil, fmt.Errorf("batch authorize resources: %w", err)
	}
	for i, request := range requests {
		if !valid[i] {
			continue
		}
		key := rbac.BatchResourceKey(strings.TrimSpace(request.ResourceType), strings.TrimSpace(request.ResourceID))
		action := actionDecisions[key]
		if !action.Allowed || !grantDecisions[key] {
			decisions[i] = AuthorizationDecision{Reason: AuthorizationReasonResourceDenied}
			continue
		}
		decisions[i] = AuthorizationDecision{Allowed: true, Reason: AuthorizationReasonAllowed}
	}
	return decisions, nil
}
