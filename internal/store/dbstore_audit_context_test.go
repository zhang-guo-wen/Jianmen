package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func newAuditContextTestStore(t *testing.T) (*DBStore, func()) {
	t.Helper()
	db, err := storage.Open(storage.Config{
		Driver:       storage.DriverSQLite,
		DSN:          t.TempDir() + "/audit-context.db",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("open audit context database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.AuditSession{},
		&model.AuditSSHCommand{},
		&model.AuditSFTPEvent{},
		&model.AuditDBQuery{},
		&model.AuditEvent{},
		&model.LoginAuditLog{},
	); err != nil {
		t.Fatalf("migrate audit context database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get audit context database: %v", err)
	}
	return NewDBStore(db), func() { _ = sqlDB.Close() }
}

func TestCreateAuditSessionCanceledDoesNotWriteOrTrackLease(t *testing.T) {
	repository, closeStore := newAuditContextTestStore(t)
	defer closeStore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := &model.AuditSession{
		ID: "canceled-create", Protocol: "ssh", State: "started", StartedAt: time.Now().UTC(),
	}
	err := repository.CreateAuditSession(ctx, session)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("create canceled audit session error = %v, want context.Canceled", err)
	}
	if ids := repository.activeAuditSessionIDs(); len(ids) != 0 {
		t.Fatalf("canceled audit session leases = %#v, want none", ids)
	}
	var count int64
	if err := repository.db.Model(&model.AuditSession{}).
		Where("id = ?", session.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count canceled audit session: %v", err)
	}
	if count != 0 {
		t.Fatalf("canceled audit session rows = %d, want 0", count)
	}
}

func TestEndAuditSessionCanceledUntracksBeforeCASWithoutWriting(t *testing.T) {
	repository, closeStore := newAuditContextTestStore(t)
	defer closeStore()

	session := &model.AuditSession{
		ID: "canceled-end", Protocol: "ssh", State: "started", StartedAt: time.Now().UTC(),
	}
	if err := repository.CreateAuditSession(context.Background(), session); err != nil {
		t.Fatalf("create audit session: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := repository.EndAuditSession(ctx, session.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("end canceled audit session error = %v, want context.Canceled", err)
	}
	if ids := repository.activeAuditSessionIDs(); len(ids) != 0 {
		t.Fatalf("ended audit session leases = %#v, want none", ids)
	}
	var stored model.AuditSession
	if err := repository.db.First(&stored, "id = ?", session.ID).Error; err != nil {
		t.Fatalf("load canceled end audit session: %v", err)
	}
	if stored.State != "started" || stored.EndedAt != nil {
		t.Fatalf("canceled end wrote audit row: %#v", stored)
	}
}

func TestCanceledAuditWritesDoNotPersistRows(t *testing.T) {
	repository, closeStore := newAuditContextTestStore(t)
	defer closeStore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writeCases := []struct {
		name  string
		write func() error
		model any
	}{
		{
			name: "SSH command",
			write: func() error {
				return repository.CreateAuditSSHCommand(ctx, &model.AuditSSHCommand{
					ID: "canceled-command", AuditSessionID: "session-1", Timestamp: time.Now().UTC(),
				})
			},
			model: &model.AuditSSHCommand{},
		},
		{
			name: "SFTP event",
			write: func() error {
				return repository.CreateAuditSFTPEvent(ctx, &model.AuditSFTPEvent{
					ID: "canceled-file", AuditSessionID: "session-1", Timestamp: time.Now().UTC(),
				})
			},
			model: &model.AuditSFTPEvent{},
		},
		{
			name: "operation event",
			write: func() error {
				return repository.CreateAuditEvent(ctx, &model.AuditEvent{ID: "canceled-operation"})
			},
			model: &model.AuditEvent{},
		},
		{
			name: "login event",
			write: func() error {
				return repository.CreateLoginAuditLog(ctx, &model.LoginAuditLog{ID: "canceled-login"})
			},
			model: &model.LoginAuditLog{},
		},
	}
	for _, test := range writeCases {
		t.Run(test.name, func(t *testing.T) {
			if err := test.write(); !errors.Is(err, context.Canceled) {
				t.Fatalf("write error = %v, want context.Canceled", err)
			}
			var count int64
			if err := repository.db.Model(test.model).Count(&count).Error; err != nil {
				t.Fatalf("count rows: %v", err)
			}
			if count != 0 {
				t.Fatalf("persisted rows = %d, want 0", count)
			}
		})
	}
}

func TestAuditQueriesReturnNoPartialResultsAfterCancellation(t *testing.T) {
	repository, closeStore := newAuditContextTestStore(t)
	defer closeStore()

	session := model.AuditSession{
		ID: "query-session", Protocol: "ssh", State: "ended", StartedAt: time.Now().UTC(),
	}
	if err := repository.db.Create(&session).Error; err != nil {
		t.Fatalf("seed audit session: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sessionItems, sessionTotal, err := repository.ListAuditSessions(ctx, AuditListParams{})
	assertCanceledAuditList(t, "sessions", sessionItems, sessionTotal, err)
	commandItems, commandTotal, err := repository.ListAuditSSHCommands(ctx, session.ID, PageOpts{})
	assertCanceledAuditList(t, "SSH commands", commandItems, commandTotal, err)
	eventItems, eventTotal, err := repository.ListAuditSFTPEvents(ctx, session.ID, PageOpts{})
	assertCanceledAuditList(t, "SFTP events", eventItems, eventTotal, err)
	operationItems, operationTotal, err := repository.ListAuditEvents(ctx, AuditEventListParams{})
	assertCanceledAuditList(t, "operation events", operationItems, operationTotal, err)
	loginItems, loginTotal, err := repository.ListLoginAuditLogs(ctx, LoginAuditListParams{})
	assertCanceledAuditList(t, "login events", loginItems, loginTotal, err)

	if _, err := repository.GetAuditSession(ctx, session.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("get audit session error = %v, want context.Canceled chain", err)
	}
	counts, err := repository.auditLogCounts(ctx, []model.AuditSession{session})
	if counts != nil || !errors.Is(err, context.Canceled) ||
		!strings.Contains(err.Error(), "count ssh audit logs") {
		t.Fatalf("audit log counts = %#v, error = %v", counts, err)
	}
}

func assertCanceledAuditList[T any](
	t *testing.T,
	name string,
	items []T,
	total int64,
	err error,
) {
	t.Helper()
	if items != nil || total != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("%s = items:%#v total:%d error:%v, want nil/0/context.Canceled", name, items, total, err)
	}
}
