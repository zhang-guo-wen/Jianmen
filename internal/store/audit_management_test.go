package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestLogicalAuditRowConditionsUsePortableConcatenation(t *testing.T) {
	for _, condition := range []string{
		logicalAuditEventRowsCondition("mysql"),
		logicalLoginAuditRowsCondition("mysql"),
	} {
		if !strings.Contains(condition, "CONCAT(") || strings.Contains(condition, " || ") {
			t.Fatalf("MySQL condition uses unsupported concatenation: %s", condition)
		}
	}
	for _, dialect := range []string{"postgres", "sqlite"} {
		for _, condition := range []string{
			logicalAuditEventRowsCondition(dialect),
			logicalLoginAuditRowsCondition(dialect),
		} {
			if strings.Contains(condition, "CONCAT(") || !strings.Contains(condition, " || ") {
				t.Fatalf("%s condition uses unsupported concatenation: %s", dialect, condition)
			}
		}
	}
}

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
	if err := st.CreateAuditEvent(context.Background(), &model.AuditEvent{
		ActorID: "u1", ActorUsername: "alice", Action: "update", ResourceType: "hosts", ResourceID: "host-1", ResourceName: "/api/hosts/host-1", ClientIP: "127.0.0.1", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create operation log: %v", err)
	}
	if err := st.CreateLoginAuditLog(context.Background(), &model.LoginAuditLog{
		Username: "alice", Outcome: "failure", Reason: "invalid_credentials", ClientIP: "127.0.0.1", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create login log: %v", err)
	}

	operations, total, err := st.ListAuditEvents(context.Background(), AuditEventListParams{Search: "host-1", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if total != 1 || len(operations) != 1 || operations[0].Action != "update" {
		t.Fatalf("operation logs = total:%d items:%+v", total, operations)
	}
	logins, total, err := st.ListLoginAuditLogs(context.Background(), LoginAuditListParams{Outcome: "failure", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("list login logs: %v", err)
	}
	if total != 1 || len(logins) != 1 || logins[0].Reason != "invalid_credentials" {
		t.Fatalf("login logs = total:%d items:%+v", total, logins)
	}
}

func TestListAuditEventsCollapsesCompletedPairsBeforePagination(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	st := NewDBStore(db)
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	events := []model.AuditEvent{
		{
			ID: "operation-intent", ActorID: "u1", ActorUsername: "alice", Action: "update",
			ResourceType: "hosts", ResourceName: "structured-host", Phase: "intent", Result: "pending",
			Detail: `{"phase":"intent","result":"pending"}`, CreatedAt: base,
		},
		{
			ID: "operation-result", ActorID: "u1", ActorUsername: "alice", Action: "update",
			ResourceType: "hosts", ResourceName: "structured-host", Phase: "result", Result: "success",
			IntentID: "operation-intent", StatusCode: 200,
			Detail:    `{"phase":"result","result":"success","intent_id":"operation-intent","status":200}`,
			CreatedAt: base.Add(time.Minute),
		},
		{
			ID: "operation-orphan", ActorID: "u1", ActorUsername: "alice", Action: "delete",
			ResourceType: "hosts", ResourceName: "orphan-host", Phase: "intent", Result: "pending",
			Detail: `{"phase":"intent","result":"pending"}`, CreatedAt: base.Add(2 * time.Minute),
		},
		{
			ID: "legacy-operation-intent", ActorID: "u1", ActorUsername: "alice", Action: "create",
			ResourceType: "users", Detail: `{"phase":"intent","result":"pending"}`,
			CreatedAt: base.Add(3 * time.Minute),
		},
		{
			ID: "legacy-operation-result", ActorID: "u1", ActorUsername: "alice", Action: "create",
			ResourceType: "users",
			Detail:       `{"phase":"result","result":"failure","intent_id":"legacy-operation-intent","status":500}`,
			CreatedAt:    base.Add(4 * time.Minute),
		},
		{
			ID: "standalone-operation", ActorID: "system", ActorUsername: "system", Action: "use",
			ResourceType: "database", Detail: `{"result":"success"}`, CreatedAt: base.Add(5 * time.Minute),
		},
	}
	for i := range events {
		if err := st.CreateAuditEvent(context.Background(), &events[i]); err != nil {
			t.Fatalf("create operation %q: %v", events[i].ID, err)
		}
	}

	firstPage, total, err := st.ListAuditEvents(context.Background(), AuditEventListParams{Page: 1, Size: 2})
	if err != nil {
		t.Fatalf("list first operation page: %v", err)
	}
	if total != 4 || len(firstPage) != 2 ||
		firstPage[0].ID != "standalone-operation" ||
		firstPage[1].ID != "legacy-operation-result" {
		t.Fatalf("first operation page = total:%d items:%+v", total, firstPage)
	}
	secondPage, total, err := st.ListAuditEvents(context.Background(), AuditEventListParams{Page: 2, Size: 2})
	if err != nil {
		t.Fatalf("list second operation page: %v", err)
	}
	if total != 4 || len(secondPage) != 2 ||
		secondPage[0].ID != "operation-orphan" || secondPage[0].Result != "pending" ||
		secondPage[1].ID != "operation-result" {
		t.Fatalf("second operation page = total:%d items:%+v", total, secondPage)
	}
	filtered, total, err := st.ListAuditEvents(context.Background(), AuditEventListParams{
		Search: "structured-host", Page: 1, Size: 10,
	})
	if err != nil {
		t.Fatalf("filter operation logs: %v", err)
	}
	if total != 1 || len(filtered) != 1 || filtered[0].ID != "operation-result" {
		t.Fatalf("filtered operation logs = total:%d items:%+v", total, filtered)
	}
}

func TestListLoginAuditLogsCollapsesCompletedPairsBeforePagination(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	st := NewDBStore(db)
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	logs := []model.LoginAuditLog{
		{
			ID: "login-intent", Username: "alice", Phase: "intent", Result: "pending",
			Outcome: "pending", Reason: "intent", ClientIP: "127.0.0.1", CreatedAt: base,
		},
		{
			ID: "login-result", Username: "alice", Phase: "result", Result: "success",
			IntentID: "login-intent", StatusCode: 200, Outcome: "success",
			ClientIP: "127.0.0.1", CreatedAt: base.Add(time.Minute),
		},
		{
			ID: "login-orphan", Username: "bob", Phase: "intent", Result: "pending",
			Outcome: "pending", Reason: "intent", ClientIP: "127.0.0.2",
			CreatedAt: base.Add(2 * time.Minute),
		},
		{
			ID: "legacy-login-intent", Username: "carol", Outcome: "pending", Reason: "intent",
			ClientIP: "127.0.0.3", CreatedAt: base.Add(3 * time.Minute),
		},
		{
			ID: "legacy-login-result", Username: "carol", Outcome: "failure",
			Reason: "intent_id=legacy-login-intent;invalid_credentials", ClientIP: "127.0.0.3",
			CreatedAt: base.Add(4 * time.Minute),
		},
		{
			ID: "standalone-login", Username: "dave", Phase: "result", Result: "blocked",
			Outcome: "blocked", Reason: "rate_limited", StatusCode: 429, ClientIP: "127.0.0.4",
			CreatedAt: base.Add(5 * time.Minute),
		},
	}
	for i := range logs {
		if err := st.CreateLoginAuditLog(context.Background(), &logs[i]); err != nil {
			t.Fatalf("create login %q: %v", logs[i].ID, err)
		}
	}

	firstPage, total, err := st.ListLoginAuditLogs(context.Background(), LoginAuditListParams{Page: 1, Size: 2})
	if err != nil {
		t.Fatalf("list first login page: %v", err)
	}
	if total != 4 || len(firstPage) != 2 ||
		firstPage[0].ID != "standalone-login" ||
		firstPage[1].ID != "legacy-login-result" {
		t.Fatalf("first login page = total:%d items:%+v", total, firstPage)
	}
	secondPage, total, err := st.ListLoginAuditLogs(context.Background(), LoginAuditListParams{Page: 2, Size: 2})
	if err != nil {
		t.Fatalf("list second login page: %v", err)
	}
	if total != 4 || len(secondPage) != 2 ||
		secondPage[0].ID != "login-orphan" || secondPage[0].Result != "pending" ||
		secondPage[1].ID != "login-result" {
		t.Fatalf("second login page = total:%d items:%+v", total, secondPage)
	}
	failures, total, err := st.ListLoginAuditLogs(context.Background(), LoginAuditListParams{
		Outcome: "failure", Page: 1, Size: 10,
	})
	if err != nil {
		t.Fatalf("filter failed login logs: %v", err)
	}
	if total != 1 || len(failures) != 1 || failures[0].ID != "legacy-login-result" {
		t.Fatalf("failed login logs = total:%d items:%+v", total, failures)
	}
}
