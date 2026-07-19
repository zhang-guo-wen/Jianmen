package admin

import (
	"context"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/online"
)

func TestWebTerminalAuditSinkPersistsNarrowEvents(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.WithValue(context.Background(), webTerminalAuditContextKey{}, "session-value")
	repo := &capturingWebTerminalAuditRepo{
		sftpEvents: make(map[string]model.AuditSFTPEvent),
	}
	sink := &webTerminalAuditSink{
		ctx:            ctx,
		store:          repo,
		sessionID:      "session-1",
		onlineSessions: online.NewRegistry(),
	}

	if err := sink.WriteCommand("session-1", now, "whoami"); err != nil {
		t.Fatalf("write command: %v", err)
	}
	if err := sink.WriteFileEvent("session-1", now.Add(time.Second), "get", "/tmp/file", 12, "ok"); err != nil {
		t.Fatalf("write file event: %v", err)
	}
	if err := sink.UpdateProtocol("session-1", "web-terminal"); err != nil {
		t.Fatalf("update protocol: %v", err)
	}

	if repo.commands == 0 || repo.events == 0 {
		t.Fatalf("expected sink to persist audit events")
	}
	if repo.protocol != "web-terminal" {
		t.Fatalf("protocol = %q, want %q", repo.protocol, "web-terminal")
	}
	if repo.command.AuditSessionID != "session-1" || repo.command.Command != "whoami" {
		t.Fatalf("command = %#v, want session session-1 and command whoami", repo.command)
	}
	if repo.sftpEvents["session-1"].Action != "get" {
		t.Fatalf("sftp action = %q, want %q", repo.sftpEvents["session-1"].Action, "get")
	}
	if repo.command.Timestamp.IsZero() || repo.protocol != "web-terminal" {
		t.Fatalf("audit payload not captured")
	}
	if repo.commandContext != ctx || repo.fileContext != ctx || repo.protocolContext != ctx {
		t.Fatal("web terminal audit sink did not preserve its active session context")
	}
}

type webTerminalAuditContextKey struct{}

type capturingWebTerminalAuditRepo struct {
	mu              sync.Mutex
	commands        int
	events          int
	protocol        string
	command         model.AuditSSHCommand
	sftpEvents      map[string]model.AuditSFTPEvent
	commandContext  context.Context
	fileContext     context.Context
	protocolContext context.Context
}

func (r *capturingWebTerminalAuditRepo) CreateAuditSSHCommand(ctx context.Context, cmd *model.AuditSSHCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands++
	r.commandContext = ctx
	if cmd != nil {
		r.command = *cmd
	}
	return nil
}

func (r *capturingWebTerminalAuditRepo) CreateAuditSFTPEvent(ctx context.Context, event *model.AuditSFTPEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events++
	r.fileContext = ctx
	if event != nil {
		r.sftpEvents[event.AuditSessionID] = *event
	}
	return nil
}

func (r *capturingWebTerminalAuditRepo) UpdateAuditProtocol(ctx context.Context, id, protocol string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.protocol = protocol
	r.protocolContext = ctx
	return nil
}
