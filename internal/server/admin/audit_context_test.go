package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/store"
)

type auditContextKey struct{}

type auditContextSnapshot struct {
	err         error
	value       any
	deadline    time.Time
	hasDeadline bool
}

func captureAuditContext(ctx context.Context) auditContextSnapshot {
	deadline, hasDeadline := ctx.Deadline()
	return auditContextSnapshot{
		err: ctx.Err(), value: ctx.Value(auditContextKey{}),
		deadline: deadline, hasDeadline: hasDeadline,
	}
}

type auditContextRepository struct {
	createSession auditContextSnapshot
	endSession    auditContextSnapshot
	operation     auditContextSnapshot
	login         auditContextSnapshot
}

func (r *auditContextRepository) CreateAuditSession(ctx context.Context, _ *model.AuditSession) error {
	r.createSession = captureAuditContext(ctx)
	return nil
}

func (r *auditContextRepository) EndAuditSession(ctx context.Context, _ string) error {
	r.endSession = captureAuditContext(ctx)
	return nil
}

func (*auditContextRepository) GetAuditSession(context.Context, string) (*model.AuditSession, error) {
	return nil, nil
}

func (*auditContextRepository) GetAuditSessionAccessMetadata(context.Context, string) (store.AuditSessionAccessMetadata, error) {
	return store.AuditSessionAccessMetadata{}, nil
}

func (*auditContextRepository) ListAuditSessions(context.Context, store.AuditListParams) ([]store.AuditSessionView, int64, error) {
	return nil, 0, nil
}

func (*auditContextRepository) UpdateAuditProtocol(context.Context, string, string) error {
	return nil
}

func (*auditContextRepository) CreateAuditSSHCommand(context.Context, *model.AuditSSHCommand) error {
	return nil
}

func (*auditContextRepository) ListAuditSSHCommands(context.Context, string, store.PageOpts) ([]model.AuditSSHCommand, int64, error) {
	return nil, 0, nil
}

func (*auditContextRepository) CreateAuditSFTPEvent(context.Context, *model.AuditSFTPEvent) error {
	return nil
}

func (*auditContextRepository) ListAuditSFTPEvents(context.Context, string, store.PageOpts) ([]model.AuditSFTPEvent, int64, error) {
	return nil, 0, nil
}

func (*auditContextRepository) ListAuditDBQueryPreviews(context.Context, string, store.AuditDBQueryPreviewParams) ([]store.AuditDBQueryPreview, int64, error) {
	return nil, 0, nil
}

func (r *auditContextRepository) CreateAuditEvent(ctx context.Context, _ *model.AuditEvent) error {
	r.operation = captureAuditContext(ctx)
	return nil
}

func (*auditContextRepository) ListAuditEvents(context.Context, store.AuditEventListParams) ([]model.AuditEvent, int64, error) {
	return nil, 0, nil
}

func (r *auditContextRepository) CreateLoginAuditLog(ctx context.Context, _ *model.LoginAuditLog) error {
	r.login = captureAuditContext(ctx)
	return nil
}

func (*auditContextRepository) ListLoginAuditLogs(context.Context, store.LoginAuditListParams) ([]model.LoginAuditLog, int64, error) {
	return nil, 0, nil
}

func TestSecurityAuditWritesAndFinalizationDetachCancellationWithBound(t *testing.T) {
	repository := &auditContextRepository{}
	server := &Server{audit: repository, cfg: &config.Config{}}
	parent := context.WithValue(context.Background(), auditContextKey{}, "request-value")
	parent, cancel := context.WithCancel(parent)
	request := httptest.NewRequest(http.MethodPost, "/api/hosts/host-1", nil).WithContext(parent)
	request.RemoteAddr = "127.0.0.1:12345"
	cancel()

	server.recordOperation(request, http.StatusCreated)
	server.logLogin(request, "alice", "user-1", "success", "")
	server.endWebTerminalAudit(parent, "session-1")

	assertDetachedBoundedAuditContext(t, "operation", repository.operation)
	assertDetachedBoundedAuditContext(t, "login", repository.login)
	assertDetachedBoundedAuditContext(t, "finalization", repository.endSession)
}

func TestWebTerminalAuditCreationUsesActiveSessionContext(t *testing.T) {
	repository := &auditContextRepository{}
	server := &Server{audit: repository, cfg: &config.Config{}}
	ctx := context.WithValue(context.Background(), auditContextKey{}, "session-value")
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	auditSession := server.startWebTerminalAudit(
		ctx,
		model.Session{ID: "session-1", StartedAt: time.Now().UTC()},
		store.TargetConfig{ID: "account-1"},
	)
	if auditSession == nil {
		t.Fatal("web terminal audit creation unexpectedly failed")
	}
	if repository.createSession.err != context.Canceled ||
		repository.createSession.value != "session-value" ||
		repository.createSession.hasDeadline {
		t.Fatalf("create session context = %#v, want original canceled session context", repository.createSession)
	}
}

func assertDetachedBoundedAuditContext(
	t *testing.T,
	name string,
	snapshot auditContextSnapshot,
) {
	t.Helper()
	remaining := time.Until(snapshot.deadline)
	if snapshot.err != nil ||
		snapshot.value != "request-value" ||
		!snapshot.hasDeadline ||
		remaining <= 0 ||
		remaining > auditWriteTimeout {
		t.Fatalf("%s audit context = %#v, remaining = %v", name, snapshot, remaining)
	}
}
