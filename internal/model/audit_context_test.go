package model

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func TestAuditUserContextRoundTrip(t *testing.T) {
	ctx := WithAuditUserID(context.Background(), "user-1")
	if got := AuditUserIDFromContext(ctx); got != "user-1" {
		t.Fatalf("audit user ID = %q, want user-1", got)
	}
	if got := AuditUserIDFromContext(nil); got != "" {
		t.Fatalf("nil context audit user ID = %q, want empty", got)
	}
}

func TestFullAuditHooksPreserveExplicitActors(t *testing.T) {
	audit := FullAudit{
		CreatedBy: "creator",
		UpdatedBy: "updater",
	}
	tx := &gorm.DB{Statement: &gorm.Statement{
		Context: WithAuditUserID(context.Background(), "context-user"),
	}}
	if err := audit.BeforeCreate(tx); err != nil {
		t.Fatalf("before create: %v", err)
	}
	if audit.CreatedBy != "creator" || audit.UpdatedBy != "updater" {
		t.Fatalf(
			"explicit actors changed to created=%q updated=%q",
			audit.CreatedBy,
			audit.UpdatedBy,
		)
	}
	if audit.ActiveMarker == nil || *audit.ActiveMarker != ActiveMarkerValue {
		t.Fatalf("active marker = %v, want %d", audit.ActiveMarker, ActiveMarkerValue)
	}

	emptyContextTx := &gorm.DB{Statement: &gorm.Statement{Context: context.Background()}}
	if err := audit.BeforeUpdate(emptyContextTx); err != nil {
		t.Fatalf("before update: %v", err)
	}
	if audit.UpdatedBy != "updater" {
		t.Fatalf("empty context cleared updated actor to %q", audit.UpdatedBy)
	}
}
