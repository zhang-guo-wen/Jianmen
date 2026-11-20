package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
)

var auditBatchTestStart = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

func batchTestSession(id string, offset time.Duration) model.AuditSession {
	return model.AuditSession{ID: id, UserID: "user-a", UserSessionID: "auth-a", ClientIP: "192.0.2.10", Protocol: "mysql", ResourceType: model.ResourceTypeDatabaseAccount, ResourceID: "resource-a", BatchTargetID: "instance-a", AccountID: "account-a", TargetAddress: "db.example:3306", StartedAt: auditBatchTestStart.Add(offset), State: "started"}
}

func batchTestWrite(t *testing.T, db *DBStore, s model.AuditSession) string {
	t.Helper()
	if err := db.CreateAuditSession(context.Background(), &s); err != nil {
		t.Fatal(err)
	}
	persisted, err := db.GetAuditSession(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.BatchID != s.BatchID {
		t.Fatalf("stored batch %q differs from assigned %q", persisted.BatchID, s.BatchID)
	}
	return persisted.BatchID
}

func TestAuditBatchSlidingWindow(t *testing.T) {
	db, closeDB := newAuditLeaseTestStore(t, auditBatchTestStart)
	defer closeDB()
	first := batchTestWrite(t, db, batchTestSession("a", 0))
	if first == "" {
		t.Fatal("expected a batch for a complete first connection")
	}
	if got := batchTestWrite(t, db, batchTestSession("b", 25*time.Minute)); got != first {
		t.Fatalf("10:25 = %q, want first batch", got)
	}
	if got := batchTestWrite(t, db, batchTestSession("c", 50*time.Minute)); got != first {
		t.Fatalf("10:50 = %q, want renewed batch", got)
	}
	if got := batchTestWrite(t, db, batchTestSession("d", 80*time.Minute)); got == "" || got == first {
		t.Fatalf("11:20 = %q, want a new batch", got)
	}
}

func TestAuditBatchKeyIsolation(t *testing.T) {
	db, closeDB := newAuditLeaseTestStore(t, auditBatchTestStart)
	defer closeDB()
	first := batchTestWrite(t, db, batchTestSession("a", 0))
	for i, c := range []struct {
		name     string
		change   func(*model.AuditSession)
		expected string
	}{
		{"operator", func(s *model.AuditSession) { s.UserID = "user-b" }, "distinct"},
		{"source", func(s *model.AuditSession) { s.ClientIP = "192.0.2.11" }, "distinct"},
		{"resource type", func(s *model.AuditSession) { s.ResourceType = "other" }, "empty"},
		{"resource", func(s *model.AuditSession) { s.ResourceID = "resource-b" }, "distinct"},
		{"target", func(s *model.AuditSession) { s.BatchTargetID = "instance-b" }, "distinct"},
		{"address", func(s *model.AuditSession) { s.TargetAddress = "other.example:3306" }, "distinct"},
		{"account", func(s *model.AuditSession) { s.AccountID = "account-b" }, "distinct"},
		{"protocol", func(s *model.AuditSession) { s.Protocol = "postgres" }, "distinct"},
		{"access", func(s *model.AuditSession) { s.ProtocolSubtype = "web_sql" }, "distinct"},
		{"authorization", func(s *model.AuditSession) { s.UserSessionID = "auth-b" }, "same"},
		{"mapped IP", func(s *model.AuditSession) { s.ClientIP = "::ffff:192.0.2.10" }, "same"},
		{"missing target", func(s *model.AuditSession) { s.ResourceID = "" }, "empty"},
		{"missing source", func(s *model.AuditSession) { s.ClientIP = "" }, "empty"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := batchTestSession(fmt.Sprintf("isolation-%d", i), time.Minute)
			c.change(&s)
			got := batchTestWrite(t, db, s)
			switch c.expected {
			case "same":
				if got != first || got == "" {
					t.Fatalf("expected same batch, got %q", got)
				}
			case "distinct":
				if got == first || got == "" {
					t.Fatalf("expected independent batch, got %q", got)
				}
			default:
				if got != "" {
					t.Fatalf("expected no batch, got %q", got)
				}
			}
		})
	}
}

func TestAuditBatchConcurrentAndRestart(t *testing.T) {
	db, closeDB := newAuditLeaseTestStore(t, auditBatchTestStart)
	defer closeDB()
	const size = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, size)
	ids := make(chan string, size)
	for i := 0; i < size; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			s := batchTestSession(fmt.Sprintf("concurrent-%d", i), 0)
			if err := db.CreateAuditSession(context.Background(), &s); err != nil {
				errs <- err
				return
			}
			ids <- s.BatchID
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Fatal(err)
	}
	first := <-ids
	if first == "" {
		t.Fatal("missing first batch ID")
	}
	for id := range ids {
		if id != first {
			t.Fatalf("concurrent batch %q, want %q", id, first)
		}
	}
	restarted := NewDBStore(db.db)
	if got := batchTestWrite(t, restarted, batchTestSession("restart", time.Minute)); got == "" || got == first {
		t.Fatalf("after restart = %q, want new batch", got)
	}
	stored, err := restarted.GetAuditSession(context.Background(), "concurrent-0")
	if err != nil || stored.BatchID != first {
		t.Fatalf("old batch lost: %v, %v", stored, err)
	}
}

func TestAuditBatchWriteFailureDoesNotExtend(t *testing.T) {
	db, closeDB := newAuditLeaseTestStore(t, auditBatchTestStart)
	defer closeDB()
	first := batchTestWrite(t, db, batchTestSession("duplicate", 0))
	failed := batchTestSession("duplicate", 25*time.Minute)
	if err := db.CreateAuditSession(context.Background(), &failed); err == nil {
		t.Fatal("expected duplicate write to fail")
	}
	if failed.BatchID != "" {
		t.Fatal("failed write exposed an uncommitted batch")
	}
	if got := batchTestWrite(t, db, batchTestSession("later", 30*time.Minute)); got == first || got == "" {
		t.Fatalf("failed connection extended batch, got %q", got)
	}
}

func TestAuditBatchUnrelatedKeysDoNotShareWriteLock(t *testing.T) {
	testNew := func(id string) model.AuditSession { s := batchTestSession(id, 0); s.UserID = id; return s }
	// 65 个不同键超过旧实现的 64 个写入分片，至少有两个会互相阻塞。
	const count = 65
	var tracker auditBatchTracker
	var wg sync.WaitGroup
	entered := make(chan struct{}, count)
	release := make(chan struct{})
	defer func() { close(release); wg.Wait() }()
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := testNew(fmt.Sprintf("isolated-%d", i))
			_ = tracker.persist(&s, auditBatchTestStart, func() error { entered <- struct{}{}; <-release; return nil })
		}(i)
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for i := 0; i < count; i++ {
		select {
		case <-entered:
		case <-timer.C:
			t.Fatalf("only %d unrelated audit writes reached the writer", i)
		}
	}
}

func TestAuditBatchOutOfOrder(t *testing.T) {
	db, closeDB := newAuditLeaseTestStore(t, auditBatchTestStart)
	defer closeDB()
	first := batchTestWrite(t, db, batchTestSession("first", 0))
	if got := batchTestWrite(t, db, batchTestSession("renewal", 25*time.Minute)); got != first {
		t.Fatalf("renewal changed batch: %q", got)
	}
	if got := batchTestWrite(t, db, batchTestSession("late", 10*time.Minute)); got != first {
		t.Fatalf("late arrival changed batch: %q", got)
	}
	if got := batchTestWrite(t, db, batchTestSession("older", -time.Minute)); got == first || got == "" {
		t.Fatalf("old connection should get a standalone batch, got %q", got)
	}
	if got := batchTestWrite(t, db, batchTestSession("after", 50*time.Minute)); got != first {
		t.Fatalf("out of order input shortened active batch: %q", got)
	}
}
