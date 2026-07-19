package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"jianmen/internal/service"
)

type denyTicketAuthorization struct{}

func (denyTicketAuthorization) AuthorizeConnection(context.Context, string, []string, string, string) (bool, error) {
	return false, nil
}

func (denyTicketAuthorization) AuthorizeBatch(context.Context, string, []service.AuthorizationRequest) ([]service.AuthorizationDecision, error) {
	return nil, nil
}

func TestWebTerminalTicketDenialDoesNotRevealTargetExistence(t *testing.T) {
	server, _ := newAdminDBTestServer(t)
	server.authorization = denyTicketAuthorization{}
	for _, targetID := range []string{"existing-target", "nonexistent-target"} {
		t.Run(targetID, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/web-terminal/tickets", bytes.NewBufferString(`{"target_id":"`+targetID+`"}`))
			ctx := context.WithValue(req.Context(), ctxKeyBrowserSession, service.BrowserSessionSubject{SessionID: "session-1", UserID: "user-1"})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			server.handleWebTerminalTicket(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
		})
	}
}
