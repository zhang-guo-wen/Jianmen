package webrdp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

type ticketStub struct {
	createTicket       string
	createErr          error
	createSubject      service.BrowserSessionSubject
	createPurpose      string
	createTargetID     string
	createConnectionID string
	createCalls        int

	consumeSubject  service.WebSocketTicketSubject
	consumeFound    bool
	consumeErr      error
	consumeSecret   string
	consumePurpose  string
	consumeTargetID string
	consumeCalls    int
}

func (s *ticketStub) CreateScopedWebSocketTicket(
	_ context.Context,
	subject service.BrowserSessionSubject,
	purpose string,
	targetID string,
	connectionID string,
) (string, error) {
	s.createCalls++
	s.createSubject = subject
	s.createPurpose = purpose
	s.createTargetID = targetID
	s.createConnectionID = connectionID
	return s.createTicket, s.createErr
}

func (s *ticketStub) ConsumeScopedWebSocketTicket(
	_ context.Context,
	secret string,
	purpose string,
	targetID string,
) (service.WebSocketTicketSubject, bool, error) {
	s.consumeCalls++
	s.consumeSecret = secret
	s.consumePurpose = purpose
	s.consumeTargetID = targetID
	return s.consumeSubject, s.consumeFound, s.consumeErr
}

type rdpTargetStub struct {
	target service.WebRDPTarget
	err    error
}

func (s rdpTargetStub) WebRDPTarget(context.Context, string) (service.WebRDPTarget, error) {
	return s.target, s.err
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

type identityStub struct {
	subject service.IdentitySubject
	found   bool
	err     error
	calls   int
}

func (s *identityStub) FindIdentitySubject(
	context.Context,
	string,
) (service.IdentitySubject, bool, error) {
	s.calls++
	return s.subject, s.found, s.err
}

func TestCreateTicketRejectsWhenWebRDPDisabled(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		TicketPath,
		strings.NewReader(`{"target_id":"account-1","width":1280,"height":720,"dpi":96}`),
	)

	(&Handler{config: Config{Enabled: false}}).CreateTicket(
		recorder,
		request,
		AuthenticatedSubject{UserID: "user-1"},
		service.BrowserSessionSubject{UserID: "user-1"},
	)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestCreateTicketRejectsInvalidDisplayDimensions(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		dpi    int
	}{
		{name: "zero width", width: 0, height: 720, dpi: 96},
		{name: "oversized width", width: maxRDPDimension + 1, height: 720, dpi: 96},
		{name: "zero height", width: 1280, height: 0, dpi: 96},
		{name: "oversized height", width: 1280, height: maxRDPDimension + 1, dpi: 96},
		{name: "low dpi", width: 1280, height: 720, dpi: 47},
		{name: "high dpi", width: 1280, height: 720, dpi: 481},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			body, err := json.Marshal(map[string]any{
				"target_id": "account-1",
				"width":     test.width,
				"height":    test.height,
				"dpi":       test.dpi,
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			request := httptest.NewRequest(http.MethodPost, TicketPath, strings.NewReader(string(body)))
			(&Handler{config: Config{Enabled: true}}).CreateTicket(
				recorder,
				request,
				AuthenticatedSubject{UserID: "user-1"},
				service.BrowserSessionSubject{UserID: "user-1"},
			)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestCreateTicketBindsPurposeTargetConnectionAndReturnsEffectivePolicy(t *testing.T) {
	target := validTestTarget()
	target.ClipboardRead = true
	target.ClipboardWrite = true
	target.FileUpload = true
	target.FileDownload = true
	target.DriveMapping = true
	authorizer := allowRDP(
		target.ID,
		rbac.ActionRDPConnect,
		rbac.ActionRDPClipboardRead,
		rbac.ActionRDPFileUpload,
		rbac.ActionRDPDriveMap,
	)
	control := newTestWebRDPService(t, target, authorizer)
	tickets := &ticketStub{createTicket: "single-use-ticket"}
	handler := ticketHandler(control, tickets)
	browser := service.BrowserSessionSubject{
		SessionID: "browser-session-1", UserID: "user-1",
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		TicketPath,
		strings.NewReader(`{"target_id":"account-1","width":1920,"height":1080,"dpi":120}`),
	)
	handler.CreateTicket(
		recorder,
		request,
		AuthenticatedSubject{UserID: "user-1", Username: "alice"},
		browser,
	)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if tickets.createCalls != 1 {
		t.Fatalf("ticket create calls = %d, want 1", tickets.createCalls)
	}
	if tickets.createPurpose != service.WebSocketPurposeRDP {
		t.Fatalf("ticket purpose = %q, want %q", tickets.createPurpose, service.WebSocketPurposeRDP)
	}
	if tickets.createTargetID != target.ID {
		t.Fatalf("ticket target = %q, want %q", tickets.createTargetID, target.ID)
	}
	if tickets.createConnectionID == "" {
		t.Fatal("ticket connection ID is empty")
	}
	if tickets.createSubject != browser {
		t.Fatalf("ticket browser subject = %#v, want %#v", tickets.createSubject, browser)
	}

	var response struct {
		Ticket          string                      `json:"ticket"`
		TargetID        string                      `json:"target_id"`
		EffectivePolicy service.WebRDPChannelPolicy `json:"effective_policy"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Ticket != "single-use-ticket" || response.TargetID != target.ID {
		t.Fatalf("ticket response = %#v", response)
	}
	wantPolicy := service.WebRDPChannelPolicy{
		ClipboardRead: true,
		FileUpload:    true,
		DriveMapping:  true,
	}
	if response.EffectivePolicy != wantPolicy {
		t.Fatalf("effective policy = %#v, want %#v", response.EffectivePolicy, wantPolicy)
	}
}

func TestConnectConsumesOnlyRDPScopedTargetTicketAndRequiresConnectionBinding(t *testing.T) {
	tickets := &ticketStub{
		consumeFound: true,
		consumeSubject: service.WebSocketTicketSubject{
			BrowserSessionSubject: service.BrowserSessionSubject{
				SessionID: "browser-session-1", UserID: "user-1",
			},
		},
	}
	identity := &identityStub{found: true}
	handler := &Handler{
		config:   Config{Enabled: true},
		tickets:  tickets,
		identity: identity,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		Path+"?target_id=account-9&ticket=one-time&width=1280&height=720&dpi=96",
		nil,
	)

	handler.Connect(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if tickets.consumeCalls != 1 ||
		tickets.consumeSecret != "one-time" ||
		tickets.consumePurpose != service.WebSocketPurposeRDP ||
		tickets.consumeTargetID != "account-9" {
		t.Fatalf("consume binding = %#v", tickets)
	}
	if identity.calls != 0 {
		t.Fatalf("identity calls = %d, want 0 before connection binding is present", identity.calls)
	}
}

func validTestTarget() service.WebRDPTarget {
	return service.WebRDPTarget{
		ID:       "account-1",
		HostID:   "host-1",
		HostName: "windows-1",
		Protocol: "rdp",
		Address:  "10.0.0.10",
		Port:     3389,
		Username: "administrator",
		Password: "target-password",
	}
}

func allowRDP(targetID string, actions ...string) *authorizerStub {
	allowed := make(map[string]bool, len(actions))
	for _, action := range actions {
		allowed[targetID+"|"+action] = true
	}
	return &authorizerStub{allowed: allowed}
}

func newTestWebRDPService(
	t *testing.T,
	target service.WebRDPTarget,
	authorizer *authorizerStub,
) *service.WebRDPService {
	t.Helper()
	control, err := service.NewWebRDPService(rdpTargetStub{target: target}, authorizer)
	if err != nil {
		t.Fatalf("new web RDP service: %v", err)
	}
	return control
}

func ticketHandler(control *service.WebRDPService, tickets *ticketStub) *Handler {
	return &Handler{
		config:  Config{Enabled: true},
		control: control,
		tickets: tickets,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}
