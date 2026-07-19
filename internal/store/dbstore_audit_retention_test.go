package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func newAuditRetentionTestStore(t *testing.T) (*DBStore, func()) {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "audit-retention.db"))
	db, err := storage.Open(storage.Config{
		Driver:       storage.DriverSQLite,
		DSN:          "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
		MaxOpenConns: 4,
		MaxIdleConns: 4,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.AuditSession{},
		&model.AuditSSHCommand{},
		&model.AuditDBQuery{},
		&model.AuditSFTPEvent{},
	); err != nil {
		t.Fatalf("migrate audit schema: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	return NewDBStore(db), func() { _ = sqlDB.Close() }
}

func TestDBStoreAuditRetentionClaimsOnlyEligibleEndedSessions(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -60)
	recent := now.AddDate(0, 0, -1)
	staleClaim := now.Add(-time.Hour)
	freshClaim := now.Add(-time.Minute)
	sessions := []model.AuditSession{
		{ID: "ready", State: "ended", EndedAt: &old, StartedAt: old, CleanupStatus: "ready"},
		{ID: "stale-pending", State: "ended", EndedAt: &old, StartedAt: old, CleanupStatus: "pending", CleanupAt: &staleClaim},
		{ID: "fresh-pending", State: "ended", EndedAt: &old, StartedAt: old, CleanupStatus: "pending", CleanupAt: &freshClaim},
		{ID: "recent", State: "ended", EndedAt: &recent, StartedAt: recent, CleanupStatus: "ready"},
		{ID: "active", State: "started", StartedAt: old, CleanupStatus: "ready"},
	}
	for index := range sessions {
		if err := repository.db.Create(&sessions[index]).Error; err != nil {
			t.Fatalf("create audit session %s: %v", sessions[index].ID, err)
		}
	}

	claimed, err := repository.ClaimAuditSessionsForCleanup(
		context.Background(),
		now.AddDate(0, 0, -30),
		now,
		now.Add(-15*time.Minute),
		10,
	)
	if err != nil {
		t.Fatalf("claim cleanup sessions: %v", err)
	}
	if len(claimed) != 2 || claimed[0].ID != "ready" || claimed[1].ID != "stale-pending" {
		t.Fatalf("claimed sessions = %#v", claimed)
	}
	second, err := repository.ClaimAuditSessionsForCleanup(
		context.Background(),
		now.AddDate(0, 0, -30),
		now.Add(time.Minute),
		now.Add(-14*time.Minute),
		10,
	)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second claim = %#v, want none", second)
	}
}

func TestDBStoreAuditRetentionDeletesClaimedAggregate(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()
	now := time.Now().UTC()
	session := model.AuditSession{
		ID: "session-1", State: "ended", StartedAt: now.Add(-time.Hour), EndedAt: &now,
		CleanupStatus: "pending", CleanupAt: &now,
	}
	if err := repository.db.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	children := []any{
		&model.AuditSSHCommand{ID: "ssh-1", AuditSessionID: session.ID, Timestamp: now, Command: "ls"},
		&model.AuditDBQuery{ID: "db-1", AuditSessionID: session.ID, Timestamp: now, SQLText: "select 1"},
		&model.AuditSFTPEvent{ID: "sftp-1", AuditSessionID: session.ID, Timestamp: now, Action: "read"},
	}
	for _, child := range children {
		if err := repository.db.Create(child).Error; err != nil {
			t.Fatalf("create %T: %v", child, err)
		}
	}

	if err := repository.DeleteClaimedAuditSession(context.Background(), session.ID); err != nil {
		t.Fatalf("delete claimed session: %v", err)
	}
	for _, table := range []any{
		&model.AuditSession{},
		&model.AuditSSHCommand{},
		&model.AuditDBQuery{},
		&model.AuditSFTPEvent{},
	} {
		var count int64
		if err := repository.db.Model(table).Count(&count).Error; err != nil {
			t.Fatalf("count %T: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%T count = %d, want 0", table, count)
		}
	}
}

func TestDBStoreAuditRetentionDeleteRollsBackOnChildFailure(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()
	now := time.Now().UTC()
	session := model.AuditSession{
		ID: "session-rollback", State: "ended", StartedAt: now.Add(-time.Hour), EndedAt: &now,
		CleanupStatus: "pending", CleanupAt: &now,
	}
	if err := repository.db.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	command := model.AuditSSHCommand{
		ID: "ssh-rollback", AuditSessionID: session.ID, Timestamp: now, Command: "whoami",
	}
	if err := repository.db.Create(&command).Error; err != nil {
		t.Fatalf("create command: %v", err)
	}
	if err := repository.db.Exec(`
		CREATE TRIGGER reject_audit_command_delete
		BEFORE DELETE ON audit_ssh_commands
		BEGIN
			SELECT RAISE(ABORT, 'delete blocked');
		END
	`).Error; err != nil {
		t.Fatalf("create delete trigger: %v", err)
	}

	if err := repository.DeleteClaimedAuditSession(context.Background(), session.ID); err == nil {
		t.Fatal("delete claimed session succeeded despite child failure")
	}
	for _, table := range []any{&model.AuditSession{}, &model.AuditSSHCommand{}} {
		var count int64
		if err := repository.db.Model(table).Count(&count).Error; err != nil {
			t.Fatalf("count %T: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%T count = %d, want 1 after rollback", table, count)
		}
	}
}

func TestDBStoreAuditRetentionFailureStateIsRetryableOnlyAfterDelay(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	endedAt := now.AddDate(0, 0, -31)
	session := model.AuditSession{
		ID: "session-failed", State: "ended", StartedAt: endedAt, EndedAt: &endedAt,
		CleanupStatus: "pending", CleanupAt: &now,
	}
	if err := repository.db.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := repository.MarkAuditSessionCleanupFailed(
		context.Background(), session.ID, now, "permission denied",
	); err != nil {
		t.Fatalf("mark cleanup failed: %v", err)
	}

	fresh, err := repository.ClaimAuditSessionsForCleanup(
		context.Background(), now, now.Add(time.Minute), now.Add(-15*time.Minute), 1,
	)
	if err != nil {
		t.Fatalf("claim fresh failure: %v", err)
	}
	if len(fresh) != 0 {
		t.Fatalf("fresh failed claim was retried: %#v", fresh)
	}
	stale, err := repository.ClaimAuditSessionsForCleanup(
		context.Background(), now.Add(time.Hour), now.Add(time.Hour), now.Add(45*time.Minute), 1,
	)
	if err != nil {
		t.Fatalf("claim stale failure: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != session.ID {
		t.Fatalf("stale claim = %#v", stale)
	}
}
