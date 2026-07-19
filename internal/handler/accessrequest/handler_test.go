package accessrequest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

type plannerStub struct {
	plan     service.WebRDPPlan
	err      error
	userID   string
	targetID string
	calls    int
}

func (s *plannerStub) Plan(
	_ context.Context,
	userID string,
	targetID string,
) (service.WebRDPPlan, error) {
	s.calls++
	s.userID = userID
	s.targetID = targetID
	return s.plan, s.err
}

type authorizationCall struct {
	userID       string
	actions      []string
	resourceType string
	resourceID   string
}

type authorizerStub struct {
	allowed map[string]bool
	err     error
	calls   []authorizationCall
}

func (s *authorizerStub) AuthorizeConnection(
	_ context.Context,
	userID string,
	actions []string,
	resourceType string,
	resourceID string,
) (bool, error) {
	s.calls = append(s.calls, authorizationCall{
		userID:       userID,
		actions:      append([]string(nil), actions...),
		resourceType: resourceType,
		resourceID:   resourceID,
	})
	if s.err != nil {
		return false, s.err
	}
	for _, action := range actions {
		if !s.allowed[resourceID+"|"+action] {
			return false, nil
		}
	}
	return true, nil
}

type accessRepositoryStub struct {
	requests    map[string]model.AccessRequest
	created     *model.AccessRequest
	listParams  []service.AccessRequestListParams
	decideCalls int
	cancelCalls int
	cancelUsers []string
}

func (s *accessRepositoryStub) CreateAccessRequest(
	_ context.Context,
	request *model.AccessRequest,
) error {
	copy := *request
	if copy.ID == "" {
		copy.ID = "created-request"
		request.ID = copy.ID
	}
	if s.requests == nil {
		s.requests = make(map[string]model.AccessRequest)
	}
	s.requests[copy.ID] = copy
	s.created = &copy
	return nil
}

func (s *accessRepositoryStub) AccessRequest(
	_ context.Context,
	id string,
) (model.AccessRequest, error) {
	request, found := s.requests[id]
	if !found {
		return model.AccessRequest{}, service.ErrAccessRequestNotFound
	}
	return request, nil
}

func (s *accessRepositoryStub) ListAccessRequests(
	_ context.Context,
	params service.AccessRequestListParams,
) ([]model.AccessRequest, int64, error) {
	s.listParams = append(s.listParams, params)
	items := make([]model.AccessRequest, 0, len(s.requests))
	for _, request := range s.requests {
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
		items = append(items, request)
	}
	return items, int64(len(items)), nil
}

func (s *accessRepositoryStub) DecideAccessRequest(
	_ context.Context,
	id string,
	status string,
	decidedBy string,
	remark string,
	decidedAt time.Time,
) (model.AccessRequest, error) {
	s.decideCalls++
	request, found := s.requests[id]
	if !found {
		return model.AccessRequest{}, service.ErrAccessRequestNotFound
	}
	if request.Status != model.AccessRequestPending {
		return model.AccessRequest{}, service.ErrAccessRequestConflict
	}
	request.Status = status
	request.DecidedBy = decidedBy
	request.DecisionRemark = remark
	request.DecidedAt = &decidedAt
	s.requests[id] = request
	return request, nil
}

func (s *accessRepositoryStub) CancelAccessRequest(
	_ context.Context,
	id string,
	requesterID string,
	cancelledAt time.Time,
) (model.AccessRequest, error) {
	s.cancelCalls++
	s.cancelUsers = append(s.cancelUsers, requesterID)
	request, found := s.requests[id]
	if !found || request.RequesterID != requesterID {
		return model.AccessRequest{}, service.ErrAccessRequestNotFound
	}
	if request.Status != model.AccessRequestPending {
		return model.AccessRequest{}, service.ErrAccessRequestConflict
	}
	request.Status = model.AccessRequestCancelled
	request.CancelledAt = &cancelledAt
	s.requests[id] = request
	return request, nil
}

func (s *accessRepositoryStub) FindActiveAccessRequest(
	context.Context,
	string,
	string,
	string,
	string,
	time.Time,
	[]string,
) (model.AccessRequest, bool, error) {
	return model.AccessRequest{}, false, nil
}

func TestCreateAccessRequestAlwaysUsesAuthenticatedRequester(t *testing.T) {
	repository := &accessRepositoryStub{}
	planner := &plannerStub{plan: service.WebRDPPlan{
		TargetID: "account-1",
		RequiredActions: []string{
			rbac.ActionRDPConnect,
			rbac.ActionRDPClipboardRead,
		},
	}}
	handler := newTestHandler(t, repository, planner, &authorizerStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/access-requests",
		strings.NewReader(`{
			"target_id":" account-1 ",
			"requester_id":"another-user",
			"reason":"temporary maintenance"
		}`),
	)

	handler.Collection(
		recorder,
		request,
		Subject{UserID: "authenticated-user", Username: "alice"},
	)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repository.created == nil {
		t.Fatal("access request was not persisted")
	}
	if repository.created.RequesterID != "authenticated-user" {
		t.Fatalf(
			"requester = %q, want authenticated subject",
			repository.created.RequesterID,
		)
	}
	if planner.calls != 1 ||
		planner.userID != "authenticated-user" ||
		planner.targetID != "account-1" {
		t.Fatalf("planner call = %#v", planner)
	}
	var response service.AccessRequestView
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.RequesterID != "authenticated-user" {
		t.Fatalf("response requester = %q", response.RequesterID)
	}
}

func TestReviewListFiltersRequestsByEachResourcePermission(t *testing.T) {
	repository := &accessRepositoryStub{requests: map[string]model.AccessRequest{
		"request-a": pendingRequest("request-a", "requester-a", "account-a"),
		"request-b": pendingRequest("request-b", "requester-b", "account-b"),
		"request-c": pendingRequest("request-c", "requester-c", "account-c"),
	}}
	authorizer := allowManage("account-a", "account-c")
	authorizer.allowed["|"+rbac.ActionRDPApprovalManage] = true
	handler := newTestHandler(t, repository, &plannerStub{}, authorizer)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/access-requests?review=true&status=pending",
		nil,
	)

	handler.Collection(recorder, request, Subject{UserID: "reviewer-1"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Items []service.AccessRequestView `json:"items"`
		Total int64                       `json:"total"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 2 || len(response.Items) != 2 {
		t.Fatalf("review response = %#v", response)
	}
	visible := map[string]bool{}
	for _, item := range response.Items {
		visible[item.ResourceID] = true
	}
	if !visible["account-a"] || !visible["account-c"] || visible["account-b"] {
		t.Fatalf("visible resources = %#v", visible)
	}
	if len(authorizer.calls) != 4 {
		t.Fatalf("authorization calls = %d, want global check plus 3 resources", len(authorizer.calls))
	}
	global := authorizer.calls[0]
	if global.userID != "reviewer-1" ||
		global.resourceType != "" ||
		global.resourceID != "" ||
		len(global.actions) != 1 ||
		global.actions[0] != rbac.ActionRDPApprovalManage {
		t.Fatalf("global review authorization call = %#v", global)
	}
	for _, call := range authorizer.calls[1:] {
		if call.userID != "reviewer-1" ||
			call.resourceType != model.ResourceTypeHostAccount ||
			len(call.actions) != 1 ||
			call.actions[0] != rbac.ActionRDPApprovalManage {
			t.Fatalf("authorization call = %#v", call)
		}
	}
	if len(repository.listParams) != 1 {
		t.Fatalf("list calls = %d, want 1", len(repository.listParams))
	}
	params := repository.listParams[0]
	if params.RequesterID != "" ||
		params.ResourceType != model.ResourceTypeHostAccount ||
		params.Protocol != "rdp" ||
		params.Status != model.AccessRequestPending {
		t.Fatalf("review list params = %#v", params)
	}
}

func TestApprovalRequiresManagePermissionOnRequestedResource(t *testing.T) {
	tests := []struct {
		name       string
		authorizer *authorizerStub
		wantStatus int
		wantCalls  int
	}{
		{
			name:       "denied",
			authorizer: &authorizerStub{allowed: map[string]bool{}},
			wantStatus: http.StatusForbidden,
			wantCalls:  0,
		},
		{
			name:       "allowed",
			authorizer: allowManage("account-1"),
			wantStatus: http.StatusOK,
			wantCalls:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &accessRepositoryStub{requests: map[string]model.AccessRequest{
				"request-1": pendingRequest("request-1", "requester-1", "account-1"),
			}}
			handler := newTestHandler(t, repository, &plannerStub{}, test.authorizer)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/access-requests/request-1/approve",
				strings.NewReader(`{"remark":"approved for change window"}`),
			)

			handler.Item(recorder, request, Subject{UserID: "reviewer-1"})

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if repository.decideCalls != test.wantCalls {
				t.Fatalf("decision calls = %d, want %d", repository.decideCalls, test.wantCalls)
			}
			if test.wantStatus == http.StatusOK {
				updated := repository.requests["request-1"]
				if updated.Status != model.AccessRequestApproved ||
					updated.DecidedBy != "reviewer-1" ||
					updated.DecisionRemark != "approved for change window" {
					t.Fatalf("approved request = %#v", updated)
				}
			} else if repository.requests["request-1"].Status != model.AccessRequestPending {
				t.Fatal("unauthorized approval changed request state")
			}
		})
	}
}

func TestRequesterCannotApproveOwnAccessRequest(t *testing.T) {
	repository := &accessRepositoryStub{requests: map[string]model.AccessRequest{
		"request-1": pendingRequest("request-1", "requester-1", "account-1"),
	}}
	authorizer := allowManage("account-1")
	handler := newTestHandler(t, repository, &plannerStub{}, authorizer)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/access-requests/request-1/approve",
		strings.NewReader(`{"remark":"self approved"}`),
	)

	handler.Item(recorder, request, Subject{UserID: "requester-1"})

	if recorder.Code != http.StatusForbidden {
		t.Fatalf(
			"status = %d, want %d: %s",
			recorder.Code,
			http.StatusForbidden,
			recorder.Body.String(),
		)
	}
	if repository.decideCalls != 0 {
		t.Fatalf("decision calls = %d, want 0", repository.decideCalls)
	}
	if len(authorizer.calls) != 1 {
		t.Fatalf("authorization calls = %d, want 1", len(authorizer.calls))
	}
	if repository.requests["request-1"].Status != model.AccessRequestPending {
		t.Fatal("self approval changed request state")
	}
}

func TestCancelAccessRequestIsRestrictedToOwner(t *testing.T) {
	tests := []struct {
		name       string
		subjectID  string
		wantStatus int
		wantState  string
	}{
		{
			name: "owner", subjectID: "requester-1",
			wantStatus: http.StatusOK, wantState: model.AccessRequestCancelled,
		},
		{
			name: "different user", subjectID: "another-user",
			wantStatus: http.StatusNotFound, wantState: model.AccessRequestPending,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &accessRepositoryStub{requests: map[string]model.AccessRequest{
				"request-1": pendingRequest("request-1", "requester-1", "account-1"),
			}}
			handler := newTestHandler(t, repository, &plannerStub{}, &authorizerStub{})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/access-requests/request-1/cancel",
				nil,
			)

			handler.Item(recorder, request, Subject{UserID: test.subjectID})

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if repository.requests["request-1"].Status != test.wantState {
				t.Fatalf(
					"request state = %q, want %q",
					repository.requests["request-1"].Status,
					test.wantState,
				)
			}
			if repository.cancelCalls != 1 ||
				len(repository.cancelUsers) != 1 ||
				repository.cancelUsers[0] != test.subjectID {
				t.Fatalf("cancel calls = %#v", repository.cancelUsers)
			}
		})
	}
}

func newTestHandler(
	t *testing.T,
	repository *accessRepositoryStub,
	planner RDPPlanner,
	authorizer Authorizer,
) *Handler {
	t.Helper()
	access, err := service.NewAccessRequestService(repository)
	if err != nil {
		t.Fatalf("new access request service: %v", err)
	}
	handler, err := New(access, planner, authorizer)
	if err != nil {
		t.Fatalf("new access request handler: %v", err)
	}
	return handler
}

func pendingRequest(id, requesterID, resourceID string) model.AccessRequest {
	start := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	expires := start.Add(8 * time.Hour)
	return model.AccessRequest{
		ID: id, RequesterID: requesterID,
		ResourceType:    model.ResourceTypeHostAccount,
		ResourceID:      resourceID,
		Protocol:        "rdp",
		ActionsJSON:     `["rdp:connect"]`,
		Reason:          "maintenance",
		Status:          model.AccessRequestPending,
		RequestedAt:     start,
		AccessStartsAt:  &start,
		AccessExpiresAt: &expires,
	}
}

func allowManage(resourceIDs ...string) *authorizerStub {
	allowed := make(map[string]bool, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		allowed[resourceID+"|"+rbac.ActionRDPApprovalManage] = true
	}
	return &authorizerStub{allowed: allowed}
}
