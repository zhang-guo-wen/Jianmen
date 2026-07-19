package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"jianmen/internal/model"
)

const (
	defaultAccessDuration = 8 * time.Hour
	maxAccessDuration     = 30 * 24 * time.Hour
)

var (
	ErrAccessRequestNotFound     = errors.New("access request not found")
	ErrAccessRequestConflict     = errors.New("access request state conflict")
	ErrAccessRequestSelfDecision = errors.New(
		"requesters cannot decide their own access requests",
	)
)

// AccessRequestListParams is the repository-level query contract.
type AccessRequestListParams struct {
	RequesterID  string
	ResourceType string
	ResourceID   string
	Protocol     string
	Status       string
	Page         int
	Size         int
}

// AccessRequestRepository is defined at the service boundary. State changes
// must be atomic and may only transition pending requests.
type AccessRequestRepository interface {
	CreateAccessRequest(ctx context.Context, request *model.AccessRequest) error
	AccessRequest(ctx context.Context, id string) (model.AccessRequest, error)
	ListAccessRequests(ctx context.Context, params AccessRequestListParams) ([]model.AccessRequest, int64, error)
	DecideAccessRequest(
		ctx context.Context,
		id string,
		status string,
		decidedBy string,
		remark string,
		decidedAt time.Time,
	) (model.AccessRequest, error)
	CancelAccessRequest(ctx context.Context, id, requesterID string, cancelledAt time.Time) (model.AccessRequest, error)
	FindActiveAccessRequest(
		ctx context.Context,
		requesterID string,
		resourceType string,
		resourceID string,
		protocol string,
		now time.Time,
		requiredActions []string,
	) (model.AccessRequest, bool, error)
}

type AccessRequestService struct {
	repository AccessRequestRepository
	now        func() time.Time
}

type CreateAccessRequestInput struct {
	RequesterID     string
	ResourceType    string
	ResourceID      string
	Protocol        string
	Actions         []string
	Reason          string
	AccessStartsAt  *time.Time
	AccessExpiresAt *time.Time
}

type AccessRequestView struct {
	ID              string     `json:"id"`
	RequesterID     string     `json:"requester_id"`
	ResourceType    string     `json:"resource_type"`
	ResourceID      string     `json:"resource_id"`
	Protocol        string     `json:"protocol"`
	Actions         []string   `json:"actions"`
	Reason          string     `json:"reason"`
	Status          string     `json:"status"`
	RequestedAt     time.Time  `json:"requested_at"`
	AccessStartsAt  *time.Time `json:"access_starts_at,omitempty"`
	AccessExpiresAt *time.Time `json:"access_expires_at,omitempty"`
	DecidedBy       string     `json:"decided_by,omitempty"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	DecisionRemark  string     `json:"decision_remark,omitempty"`
	CancelledAt     *time.Time `json:"cancelled_at,omitempty"`
}

func NewAccessRequestService(repository AccessRequestRepository) (*AccessRequestService, error) {
	if repository == nil {
		return nil, errors.New("access request repository is required")
	}
	return &AccessRequestService{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *AccessRequestService) Create(ctx context.Context, input CreateAccessRequestInput) (AccessRequestView, error) {
	if err := ctx.Err(); err != nil {
		return AccessRequestView{}, err
	}
	input.RequesterID = strings.TrimSpace(input.RequesterID)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.Protocol = strings.ToLower(strings.TrimSpace(input.Protocol))
	input.Reason = strings.TrimSpace(input.Reason)
	actions, err := normalizeAccessActions(input.Actions)
	if err != nil {
		return AccessRequestView{}, err
	}
	if input.RequesterID == "" || input.ResourceType == "" || input.ResourceID == "" {
		return AccessRequestView{}, errors.New("requester and resource are required")
	}
	if input.Protocol != "rdp" || input.ResourceType != model.ResourceTypeHostAccount {
		return AccessRequestView{}, errors.New("only host account RDP access requests are supported")
	}
	if input.Reason == "" {
		return AccessRequestView{}, errors.New("access request reason is required")
	}

	now := s.now().UTC()
	startsAt := now
	if input.AccessStartsAt != nil {
		startsAt = input.AccessStartsAt.UTC()
	}
	expiresAt := startsAt.Add(defaultAccessDuration)
	if input.AccessExpiresAt != nil {
		expiresAt = input.AccessExpiresAt.UTC()
	}
	if expiresAt.Before(now) || !expiresAt.After(startsAt) {
		return AccessRequestView{}, errors.New("access request expiry must be after its start")
	}
	if expiresAt.Sub(startsAt) > maxAccessDuration {
		return AccessRequestView{}, errors.New("access request duration cannot exceed 30 days")
	}

	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		return AccessRequestView{}, fmt.Errorf("encode access request actions: %w", err)
	}
	request := &model.AccessRequest{
		RequesterID:     input.RequesterID,
		ResourceType:    input.ResourceType,
		ResourceID:      input.ResourceID,
		Protocol:        input.Protocol,
		ActionsJSON:     string(actionsJSON),
		Reason:          input.Reason,
		Status:          model.AccessRequestPending,
		RequestedAt:     now,
		AccessStartsAt:  &startsAt,
		AccessExpiresAt: &expiresAt,
	}
	if err := s.repository.CreateAccessRequest(ctx, request); err != nil {
		return AccessRequestView{}, fmt.Errorf("create access request: %w", err)
	}
	return accessRequestView(*request)
}

func (s *AccessRequestService) Get(ctx context.Context, id string) (AccessRequestView, error) {
	request, err := s.repository.AccessRequest(ctx, strings.TrimSpace(id))
	if err != nil {
		return AccessRequestView{}, err
	}
	return accessRequestView(request)
}

func (s *AccessRequestService) List(
	ctx context.Context,
	params AccessRequestListParams,
) ([]AccessRequestView, int64, error) {
	requests, total, err := s.repository.ListAccessRequests(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	views := make([]AccessRequestView, 0, len(requests))
	for _, request := range requests {
		view, viewErr := accessRequestView(request)
		if viewErr != nil {
			return nil, 0, viewErr
		}
		views = append(views, view)
	}
	return views, total, nil
}

func (s *AccessRequestService) Decide(
	ctx context.Context,
	id string,
	approved bool,
	decidedBy string,
	remark string,
) (AccessRequestView, error) {
	id = strings.TrimSpace(id)
	decidedBy = strings.TrimSpace(decidedBy)
	if decidedBy == "" {
		return AccessRequestView{}, errors.New("access request decider is required")
	}
	request, err := s.repository.AccessRequest(ctx, id)
	if err != nil {
		return AccessRequestView{}, err
	}
	if request.RequesterID == decidedBy {
		return AccessRequestView{}, ErrAccessRequestSelfDecision
	}
	status := model.AccessRequestRejected
	if approved {
		status = model.AccessRequestApproved
	}
	request, err = s.repository.DecideAccessRequest(
		ctx,
		id,
		status,
		decidedBy,
		strings.TrimSpace(remark),
		s.now().UTC(),
	)
	if err != nil {
		return AccessRequestView{}, err
	}
	return accessRequestView(request)
}

func (s *AccessRequestService) Cancel(
	ctx context.Context,
	id string,
	requesterID string,
) (AccessRequestView, error) {
	request, err := s.repository.CancelAccessRequest(
		ctx,
		strings.TrimSpace(id),
		strings.TrimSpace(requesterID),
		s.now().UTC(),
	)
	if err != nil {
		return AccessRequestView{}, err
	}
	return accessRequestView(request)
}

// ActiveApproval returns an approved, currently valid request that includes
// every requested action. It never replaces normal RBAC authorization.
func (s *AccessRequestService) ActiveApproval(
	ctx context.Context,
	requesterID string,
	resourceType string,
	resourceID string,
	protocol string,
	requiredActions []string,
) (AccessRequestView, bool, error) {
	request, found, err := s.repository.FindActiveAccessRequest(
		ctx,
		strings.TrimSpace(requesterID),
		strings.TrimSpace(resourceType),
		strings.TrimSpace(resourceID),
		strings.ToLower(strings.TrimSpace(protocol)),
		s.now().UTC(),
		requiredActions,
	)
	if err != nil {
		return AccessRequestView{}, false, err
	}
	if !found {
		return AccessRequestView{}, false, nil
	}
	view, err := accessRequestView(request)
	if err != nil {
		return AccessRequestView{}, false, err
	}
	now := s.now().UTC()
	if view.Status != model.AccessRequestApproved ||
		view.RequesterID != strings.TrimSpace(requesterID) ||
		view.ResourceType != strings.TrimSpace(resourceType) ||
		view.ResourceID != strings.TrimSpace(resourceID) ||
		!strings.EqualFold(view.Protocol, strings.TrimSpace(protocol)) ||
		(view.AccessStartsAt != nil && view.AccessStartsAt.UTC().After(now)) ||
		view.AccessExpiresAt == nil ||
		!view.AccessExpiresAt.UTC().After(now) {
		return AccessRequestView{}, false, nil
	}
	for _, action := range requiredActions {
		if !slices.Contains(view.Actions, strings.TrimSpace(action)) {
			return AccessRequestView{}, false, nil
		}
	}
	return view, true, nil
}

func accessRequestView(request model.AccessRequest) (AccessRequestView, error) {
	var actions []string
	if err := json.Unmarshal([]byte(request.ActionsJSON), &actions); err != nil {
		return AccessRequestView{}, fmt.Errorf("decode access request actions: %w", err)
	}
	return AccessRequestView{
		ID: request.ID, RequesterID: request.RequesterID,
		ResourceType: request.ResourceType, ResourceID: request.ResourceID,
		Protocol: request.Protocol, Actions: actions, Reason: request.Reason,
		Status: request.Status, RequestedAt: request.RequestedAt,
		AccessStartsAt: request.AccessStartsAt, AccessExpiresAt: request.AccessExpiresAt,
		DecidedBy: request.DecidedBy, DecidedAt: request.DecidedAt,
		DecisionRemark: request.DecisionRemark, CancelledAt: request.CancelledAt,
	}, nil
}

func normalizeAccessActions(actions []string) ([]string, error) {
	allowed := map[string]bool{
		"rdp:connect":         true,
		"rdp:clipboard:read":  true,
		"rdp:clipboard:write": true,
		"rdp:file:upload":     true,
		"rdp:file:download":   true,
		"rdp:drive:map":       true,
	}
	normalized := make([]string, 0, len(actions)+1)
	seen := make(map[string]bool, len(actions)+1)
	actions = append([]string{"rdp:connect"}, actions...)
	for _, action := range actions {
		action = strings.TrimSpace(action)
		if action == "" || seen[action] {
			continue
		}
		if !allowed[action] {
			return nil, fmt.Errorf("access request action %q is not supported", action)
		}
		seen[action] = true
		normalized = append(normalized, action)
	}
	return normalized, nil
}
