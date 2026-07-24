package admin

import (
	"net/http"
	"testing"

	"jianmen/internal/model"
)

func TestAuditQueryEventRecoversStructuredFieldsFromLegacyDetail(t *testing.T) {
	item := auditQueryEvent(model.AuditEvent{
		ID:     "legacy-result",
		Detail: `{"phase":"result","result":"failure","intent_id":"legacy-intent","request_id":"request-1","status":409}`,
	})
	if item.Phase != "result" ||
		item.Result != "failure" ||
		item.IntentID != "legacy-intent" ||
		item.RequestID != "request-1" ||
		item.StatusCode != http.StatusConflict {
		t.Fatalf("legacy operation audit = %#v", item)
	}
}

func TestAuditQueryLoginRecoversLegacyLinkWithoutExposingItAsReason(t *testing.T) {
	item := auditQueryLogin(model.LoginAuditLog{
		ID:       "legacy-login-result",
		Username: "alice",
		Outcome:  "failure",
		Reason:   "intent_id=legacy-login-intent;invalid_credentials",
	})
	if item.Phase != "result" ||
		item.Result != "failure" ||
		item.IntentID != "legacy-login-intent" ||
		item.Reason != "invalid_credentials" ||
		item.StatusCode != http.StatusUnauthorized {
		t.Fatalf("legacy login audit = %#v", item)
	}
}

func TestAuditQueryLoginKeepsOrphanIntentPending(t *testing.T) {
	item := auditQueryLogin(model.LoginAuditLog{
		ID:       "orphan-login-intent",
		Username: "alice",
		Outcome:  loginAuditOutcomePending,
		Reason:   loginAuditReasonIntent,
	})
	if item.Phase != "intent" || item.Result != "pending" || item.StatusCode != 0 {
		t.Fatalf("orphan login intent = %#v", item)
	}
}
