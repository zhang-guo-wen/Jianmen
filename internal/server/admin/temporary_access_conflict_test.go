package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jianmen/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestTemporaryAuthorizationExtensionCASMissReturnsConflict(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)
	if err := db.Create(&model.TemporaryAccount{
		ID: "temporary-cas", SessionID: "CAS01", Type: model.TemporaryAccountTypeUser,
		Username: "tmp_CAS01", Status: "active", StartsAt: now, ExpiresAt: &expiresAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	const callbackName = "test:temporary-access-cas-miss"
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "temporary_accounts" {
			return
		}
		tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Table: clause.CurrentTable, Name: "id"}, Value: "concurrently-disabled"},
		}})
	}); err != nil {
		t.Fatalf("register CAS miss callback: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(callbackName); err != nil {
			t.Errorf("remove CAS miss callback: %v", err)
		}
	})

	body, _ := json.Marshal(map[string]any{"expires_at": now.Add(2 * time.Hour)})
	req := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/temporary-accounts/temporary-cas/extend",
		bytes.NewReader(body),
	))
	rec := httptest.NewRecorder()
	server.extendTemporaryAccount(rec, req, "temporary-cas")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
}
