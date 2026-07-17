package store

import (
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestAuditLogStoresListAndFilter(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	st := NewDBStore(db)
	now := time.Now().UTC().Truncate(time.Second)
	if err := st.CreateAuditEvent(&model.AuditEvent{
		ActorID: "u1", ActorUsername: "alice", Action: "update", ResourceType: "hosts", ResourceID: "host-1", ResourceName: "/api/hosts/host-1", ClientIP: "127.0.0.1", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create operation log: %v", err)
	}
	if err := st.CreateLoginAuditLog(&model.LoginAuditLog{
		Username: "alice", Outcome: "failure", Reason: "invalid_credentials", ClientIP: "127.0.0.1", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create login log: %v", err)
	}

	operations, total, err := st.ListAuditEvents(AuditEventListParams{Search: "host-1", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if total != 1 || len(operations) != 1 || operations[0].Action != "update" {
		t.Fatalf("operation logs = total:%d items:%+v", total, operations)
	}
	logins, total, err := st.ListLoginAuditLogs(LoginAuditListParams{Outcome: "failure", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("list login logs: %v", err)
	}
	if total != 1 || len(logins) != 1 || logins[0].Reason != "invalid_credentials" {
		t.Fatalf("login logs = total:%d items:%+v", total, logins)
	}
}
