package store

import (
	"context"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestAuditSessionListKeepsDeletedAuthorizationSessionID(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	user := model.User{ID: "audit-user", Username: "audit-user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	userSession := model.UserSession{
		ID: "authorization-session", UserID: user.ID, SessionSeq: 1,
		SessionID: "00001", Type: "permanent", Status: "disabled",
	}
	if err := db.Create(&userSession).Error; err != nil {
		t.Fatalf("create user session: %v", err)
	}
	if err := db.Model(&model.UserSession{}).
		Where("id = ?", userSession.ID).
		UpdateColumn("active_marker", nil).Error; err != nil {
		t.Fatalf("logically delete user session: %v", err)
	}

	repository := NewDBStore(db)
	if err := repository.CreateAuditSession(context.Background(), &model.AuditSession{
		ID: "audit-session", UserSessionID: userSession.ID,
		UserID: user.ID, Username: user.Username, Protocol: "ssh",
		TargetName: "host", ClientIP: "127.0.0.1",
		StartedAt: time.Now().UTC(), State: "ended", Outcome: model.AuditOutcomeSucceeded,
	}); err != nil {
		t.Fatalf("create audit session: %v", err)
	}

	items, total, err := repository.ListAuditSessions(
		context.Background(),
		AuditListParams{Protocol: "ssh", Page: 1, Size: 10},
	)
	if err != nil {
		t.Fatalf("list audit sessions: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("audit sessions = total:%d items:%+v", total, items)
	}
	if items[0].SessionID != "00001" {
		t.Fatalf("authorization session id = %q, want 00001", items[0].SessionID)
	}
}
