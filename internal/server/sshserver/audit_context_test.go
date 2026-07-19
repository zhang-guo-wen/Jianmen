package sshserver

import (
	"context"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/online"
)

type sshAuditContextKey struct{}

type sshAuditContextSnapshot struct {
	err         error
	value       any
	deadline    time.Time
	hasDeadline bool
}

func captureSSHAuditContext(ctx context.Context) sshAuditContextSnapshot {
	deadline, hasDeadline := ctx.Deadline()
	return sshAuditContextSnapshot{
		err: ctx.Err(), value: ctx.Value(sshAuditContextKey{}),
		deadline: deadline, hasDeadline: hasDeadline,
	}
}

type sshAuditContextRepository struct {
	command  sshAuditContextSnapshot
	file     sshAuditContextSnapshot
	protocol sshAuditContextSnapshot
	end      sshAuditContextSnapshot
}

func (*sshAuditContextRepository) CreateAuditSession(context.Context, *model.AuditSession) error {
	return nil
}

func (r *sshAuditContextRepository) EndAuditSession(ctx context.Context, _ string) error {
	r.end = captureSSHAuditContext(ctx)
	return nil
}

func (r *sshAuditContextRepository) CreateAuditSSHCommand(ctx context.Context, _ *model.AuditSSHCommand) error {
	r.command = captureSSHAuditContext(ctx)
	return nil
}

func (r *sshAuditContextRepository) CreateAuditSFTPEvent(ctx context.Context, _ *model.AuditSFTPEvent) error {
	r.file = captureSSHAuditContext(ctx)
	return nil
}

func (r *sshAuditContextRepository) UpdateAuditProtocol(ctx context.Context, _, _ string) error {
	r.protocol = captureSSHAuditContext(ctx)
	return nil
}

func TestSSHAuditSinkUsesActiveSessionContext(t *testing.T) {
	repository := &sshAuditContextRepository{}
	ctx := context.WithValue(context.Background(), sshAuditContextKey{}, "session-value")
	sink := &auditStore{
		ctx: ctx, store: repository, sessionID: "session-1",
		onlineSessions: online.NewRegistry(),
	}

	if err := sink.WriteCommand("", time.Now(), "whoami"); err != nil {
		t.Fatalf("write command: %v", err)
	}
	if err := sink.WriteFileEvent("", time.Now(), "get", "/tmp/file", 1, "ok"); err != nil {
		t.Fatalf("write file event: %v", err)
	}
	if err := sink.UpdateProtocol("", "sftp"); err != nil {
		t.Fatalf("update protocol: %v", err)
	}
	for name, snapshot := range map[string]sshAuditContextSnapshot{
		"command": repository.command, "file": repository.file, "protocol": repository.protocol,
	} {
		if snapshot.err != nil || snapshot.value != "session-value" || snapshot.hasDeadline {
			t.Fatalf("%s context = %#v, want active session context", name, snapshot)
		}
	}
}

func TestSSHAuditFinalizationDetachesCancellationWithBound(t *testing.T) {
	repository := &sshAuditContextRepository{}
	server := &Server{auditSessions: repository}
	parent := context.WithValue(context.Background(), sshAuditContextKey{}, "session-value")
	parent, cancel := context.WithCancel(parent)
	cancel()

	server.endAuditSession(parent, "session-1")

	remaining := time.Until(repository.end.deadline)
	if repository.end.err != nil ||
		repository.end.value != "session-value" ||
		!repository.end.hasDeadline ||
		remaining <= 0 ||
		remaining > auditFinalizeTimeout {
		t.Fatalf("SSH finalization context = %#v, remaining = %v", repository.end, remaining)
	}
}
