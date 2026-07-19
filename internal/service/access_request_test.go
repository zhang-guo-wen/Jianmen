package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

type accessRequestMemoryRepository struct {
	requests  map[string]model.AccessRequest
	nextID    int
	createErr error
}

func newAccessRequestMemoryRepository() *accessRequestMemoryRepository {
	return &accessRequestMemoryRepository{requests: make(map[string]model.AccessRequest)}
}

func (r *accessRequestMemoryRepository) CreateAccessRequest(
	_ context.Context,
	request *model.AccessRequest,
) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.nextID++
	if request.ID == "" {
		request.ID = fmt.Sprintf("request-%d", r.nextID)
	}
	r.requests[request.ID] = *request
	return nil
}

func (r *accessRequestMemoryRepository) AccessRequest(
	_ context.Context,
	id string,
) (model.AccessRequest, error) {
	request, found := r.requests[id]
	if !found {
		return model.AccessRequest{}, ErrAccessRequestNotFound
	}
	return request, nil
}

func (r *accessRequestMemoryRepository) ListAccessRequests(
	_ context.Context,
	params AccessRequestListParams,
) ([]model.AccessRequest, int64, error) {
	requests := make([]model.AccessRequest, 0, len(r.requests))
	for _, request := range r.requests {
		if params.RequesterID != "" && request.RequesterID != params.RequesterID {
			continue
		}
		if params.ResourceType != "" && request.ResourceType != params.ResourceType {
			continue
		}
		if params.ResourceID != "" && request.ResourceID != params.ResourceID {
			continue
		}
		if params.Protocol != "" && request.Protocol != params.Protocol {
			continue
		}
		if params.Status != "" && request.Status != params.Status {
			continue
		}
		requests = append(requests, request)
	}
	return requests, int64(len(requests)), nil
}

func (r *accessRequestMemoryRepository) DecideAccessRequest(
	_ context.Context,
	id string,
	status string,
	decidedBy string,
	remark string,
	decidedAt time.Time,
) (model.AccessRequest, error) {
	request, found := r.requests[id]
	if !found {
		return model.AccessRequest{}, ErrAccessRequestNotFound
	}
	if request.Status != model.AccessRequestPending {
		return model.AccessRequest{}, ErrAccessRequestConflict
	}
	request.Status = status
	request.DecidedBy = decidedBy
	request.DecisionRemark = remark
	request.DecidedAt = &decidedAt
	r.requests[id] = request
	return request, nil
}

func (r *accessRequestMemoryRepository) CancelAccessRequest(
	_ context.Context,
	id string,
	requesterID string,
	cancelledAt time.Time,
) (model.AccessRequest, error) {
	request, found := r.requests[id]
	if !found || request.RequesterID != requesterID {
		return model.AccessRequest{}, ErrAccessRequestNotFound
	}
	if request.Status != model.AccessRequestPending {
		return model.AccessRequest{}, ErrAccessRequestConflict
	}
	request.Status = model.AccessRequestCancelled
	request.CancelledAt = &cancelledAt
	r.requests[id] = request
	return request, nil
}

func (r *accessRequestMemoryRepository) FindActiveAccessRequest(
	_ context.Context,
	requesterID string,
	resourceType string,
	resourceID string,
	protocol string,
	now time.Time,
	requiredActions []string,
) (model.AccessRequest, bool, error) {
	for _, request := range r.requests {
		if request.RequesterID != requesterID ||
			request.ResourceType != resourceType ||
			request.ResourceID != resourceID ||
			request.Protocol != protocol ||
			request.Status != model.AccessRequestApproved {
			continue
		}
		if request.AccessStartsAt != nil && request.AccessStartsAt.After(now) {
			continue
		}
		if request.AccessExpiresAt == nil || !request.AccessExpiresAt.After(now) {
			continue
		}
		view, err := accessRequestView(request)
		if err != nil {
			return model.AccessRequest{}, false, err
		}
		matches := true
		for _, action := range requiredActions {
			if !slices.Contains(view.Actions, strings.TrimSpace(action)) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		return request, true, nil
	}
	return model.AccessRequest{}, false, nil
}

func newAccessRequestServiceForTest(
	t *testing.T,
	repository AccessRequestRepository,
	now time.Time,
) *AccessRequestService {
	t.Helper()
	service, err := NewAccessRequestService(repository)
	if err != nil {
		t.Fatalf("NewAccessRequestService() error = %v", err)
	}
	service.now = func() time.Time { return now }
	return service
}

func validAccessRequestInput() CreateAccessRequestInput {
	return CreateAccessRequestInput{
		RequesterID: "user-1", ResourceType: model.ResourceTypeHostAccount,
		ResourceID: "account-1", Protocol: "rdp",
		Actions: []string{rbac.ActionRDPConnect}, Reason: "maintenance",
	}
}

func TestAccessRequestServiceCreateTimeRange(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	t.Run("default eight hours", func(t *testing.T) {
		repository := newAccessRequestMemoryRepository()
		service := newAccessRequestServiceForTest(t, repository, now)

		view, err := service.Create(context.Background(), validAccessRequestInput())
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if view.AccessStartsAt == nil || !view.AccessStartsAt.Equal(now) {
			t.Fatalf("AccessStartsAt = %v, want %v", view.AccessStartsAt, now)
		}
		wantExpiry := now.Add(8 * time.Hour)
		if view.AccessExpiresAt == nil || !view.AccessExpiresAt.Equal(wantExpiry) {
			t.Fatalf("AccessExpiresAt = %v, want %v", view.AccessExpiresAt, wantExpiry)
		}
	})

	t.Run("maximum duration accepted", func(t *testing.T) {
		repository := newAccessRequestMemoryRepository()
		service := newAccessRequestServiceForTest(t, repository, now)
		input := validAccessRequestInput()
		startsAt := now.Add(time.Hour)
		expiresAt := startsAt.Add(30 * 24 * time.Hour)
		input.AccessStartsAt = &startsAt
		input.AccessExpiresAt = &expiresAt

		view, err := service.Create(context.Background(), input)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if view.AccessStartsAt == nil || !view.AccessStartsAt.Equal(startsAt) ||
			view.AccessExpiresAt == nil || !view.AccessExpiresAt.Equal(expiresAt) {
			t.Fatalf("created window = %v - %v", view.AccessStartsAt, view.AccessExpiresAt)
		}
	})

	tests := []struct {
		name      string
		startsAt  time.Time
		expiresAt time.Time
	}{
		{"expiry equals start", now.Add(time.Hour), now.Add(time.Hour)},
		{"expiry before start", now.Add(2 * time.Hour), now.Add(time.Hour)},
		{"expiry already elapsed", now.Add(-2 * time.Hour), now.Add(-time.Hour)},
		{
			"duration exceeds maximum",
			now.Add(time.Hour),
			now.Add(time.Hour).Add(30*24*time.Hour + time.Second),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newAccessRequestMemoryRepository()
			service := newAccessRequestServiceForTest(t, repository, now)
			input := validAccessRequestInput()
			input.AccessStartsAt = &test.startsAt
			input.AccessExpiresAt = &test.expiresAt

			if _, err := service.Create(context.Background(), input); err == nil {
				t.Fatal("Create() error = nil")
			}
			if len(repository.requests) != 0 {
				t.Fatalf("invalid request was persisted: %#v", repository.requests)
			}
		})
	}
}

func TestAccessRequestServiceActionWhitelist(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	repository := newAccessRequestMemoryRepository()
	service := newAccessRequestServiceForTest(t, repository, now)
	input := validAccessRequestInput()
	input.Actions = []string{
		" " + rbac.ActionRDPFileUpload + " ",
		rbac.ActionRDPConnect,
		rbac.ActionRDPFileUpload,
		"",
		rbac.ActionRDPClipboardRead,
	}

	view, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	want := []string{
		rbac.ActionRDPConnect,
		rbac.ActionRDPFileUpload,
		rbac.ActionRDPClipboardRead,
	}
	if !reflect.DeepEqual(view.Actions, want) {
		t.Fatalf("Actions = %#v, want %#v", view.Actions, want)
	}

	invalid := validAccessRequestInput()
	invalid.Actions = []string{"session:connect"}
	if _, err = service.Create(context.Background(), invalid); err == nil ||
		!strings.Contains(err.Error(), "not supported") {
		t.Fatalf("unsupported action error = %v", err)
	}
	if len(repository.requests) != 1 {
		t.Fatalf("unsupported action was persisted: %#v", repository.requests)
	}
}

func TestAccessRequestServiceRepeatedStateTransitionsConflict(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	repository := newAccessRequestMemoryRepository()
	service := newAccessRequestServiceForTest(t, repository, now)

	approved, err := service.Create(context.Background(), validAccessRequestInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Decide(
		context.Background(), approved.ID, true, "reviewer-1", "approved",
	); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if _, err = service.Decide(
		context.Background(), approved.ID, false, "reviewer-2", "changed mind",
	); !errors.Is(err, ErrAccessRequestConflict) {
		t.Fatalf("second Decide() error = %v, want conflict", err)
	}
	if _, err = service.Cancel(
		context.Background(), approved.ID, "user-1",
	); !errors.Is(err, ErrAccessRequestConflict) {
		t.Fatalf("Cancel(approved) error = %v, want conflict", err)
	}

	cancelled, err := service.Create(context.Background(), validAccessRequestInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Cancel(
		context.Background(), cancelled.ID, "user-1",
	); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if _, err = service.Cancel(
		context.Background(), cancelled.ID, "user-1",
	); !errors.Is(err, ErrAccessRequestConflict) {
		t.Fatalf("second Cancel() error = %v, want conflict", err)
	}
	if _, err = service.Decide(
		context.Background(), cancelled.ID, true, "reviewer-1", "",
	); !errors.Is(err, ErrAccessRequestConflict) {
		t.Fatalf("Decide(cancelled) error = %v, want conflict", err)
	}
}

func TestAccessRequestServiceRejectsSelfDecision(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	repository := newAccessRequestMemoryRepository()
	service := newAccessRequestServiceForTest(t, repository, now)
	request, err := service.Create(context.Background(), validAccessRequestInput())
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Decide(
		context.Background(),
		request.ID,
		true,
		"user-1",
		"self approval",
	)

	if !errors.Is(err, ErrAccessRequestSelfDecision) {
		t.Fatalf("Decide() error = %v, want self-decision error", err)
	}
	persisted, err := repository.AccessRequest(context.Background(), request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.AccessRequestPending ||
		persisted.DecidedAt != nil ||
		persisted.DecidedBy != "" {
		t.Fatalf("self decision changed request: %#v", persisted)
	}
}

func TestAccessRequestServiceActiveApprovalRequiresActiveIdentityAndActions(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	startsAt := now.Add(-time.Minute)
	expiresAt := now.Add(time.Hour)
	base := model.AccessRequest{
		ID: "approval-1", RequesterID: "user-1",
		ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-1",
		Protocol: "rdp", Status: model.AccessRequestApproved,
		ActionsJSON:    `["rdp:connect","rdp:file:upload"]`,
		AccessStartsAt: &startsAt, AccessExpiresAt: &expiresAt,
	}
	tests := []struct {
		name           string
		mutate         func(*model.AccessRequest)
		required       []string
		wantFound      bool
		repositoryFind bool
	}{
		{
			name: "active with required actions", required: []string{
				rbac.ActionRDPConnect, rbac.ActionRDPFileUpload,
			},
			wantFound: true, repositoryFind: true,
		},
		{
			name: "missing requested action",
			required: []string{
				rbac.ActionRDPConnect, rbac.ActionRDPFileDownload,
			},
			repositoryFind: true,
		},
		{
			name: "pending is not approval",
			mutate: func(request *model.AccessRequest) {
				request.Status = model.AccessRequestPending
			},
			required: []string{rbac.ActionRDPConnect}, repositoryFind: true,
		},
		{
			name: "future approval is inactive",
			mutate: func(request *model.AccessRequest) {
				future := now.Add(time.Minute)
				request.AccessStartsAt = &future
			},
			required: []string{rbac.ActionRDPConnect}, repositoryFind: true,
		},
		{
			name: "expired approval is inactive",
			mutate: func(request *model.AccessRequest) {
				expired := now
				request.AccessExpiresAt = &expired
			},
			required: []string{rbac.ActionRDPConnect}, repositoryFind: true,
		},
		{
			name: "approval without expiry is inactive",
			mutate: func(request *model.AccessRequest) {
				request.AccessExpiresAt = nil
			},
			required: []string{rbac.ActionRDPConnect}, repositoryFind: true,
		},
		{
			name: "substituted requester is inactive",
			mutate: func(request *model.AccessRequest) {
				request.RequesterID = "user-2"
			},
			required: []string{rbac.ActionRDPConnect}, repositoryFind: true,
		},
		{
			name:     "repository did not find approval",
			required: []string{rbac.ActionRDPConnect},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			if test.mutate != nil {
				test.mutate(&request)
			}
			repository := &webRDPApprovalRepositoryStub{
				request: request,
				found:   test.repositoryFind,
			}
			service := newAccessRequestServiceForTest(t, repository, now)

			_, found, err := service.ActiveApproval(
				context.Background(),
				"user-1",
				model.ResourceTypeHostAccount,
				"account-1",
				"rdp",
				test.required,
			)
			if err != nil {
				t.Fatalf("ActiveApproval() error = %v", err)
			}
			if found != test.wantFound {
				t.Fatalf("ActiveApproval() found = %t, want %t", found, test.wantFound)
			}
		})
	}
}

func TestAccessRequestServiceActiveApprovalRepositoryErrorFailsClosed(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	sentinel := errors.New("database unavailable")
	repository := &webRDPApprovalRepositoryStub{found: true, err: sentinel}
	service := newAccessRequestServiceForTest(t, repository, now)

	view, found, err := service.ActiveApproval(
		context.Background(),
		"user-1",
		model.ResourceTypeHostAccount,
		"account-1",
		"rdp",
		[]string{rbac.ActionRDPConnect},
	)
	if !errors.Is(err, sentinel) || found || view.ID != "" {
		t.Fatalf("ActiveApproval() = (%#v, %t, %v)", view, found, err)
	}
}
