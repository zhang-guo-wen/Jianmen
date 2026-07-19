package sshserver

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/online"
	"jianmen/internal/store"
)

type focusedRuntimeRepository struct{}

func (focusedRuntimeRepository) Authenticate(context.Context, string, string) (model.User, error) {
	return model.User{}, nil
}

func (focusedRuntimeRepository) AuthenticatePublicKey(context.Context, string, ssh.PublicKey) (model.User, error) {
	return model.User{}, nil
}

func (focusedRuntimeRepository) DefaultTarget(context.Context, model.User) (store.TargetConfig, error) {
	return store.TargetConfig{}, nil
}

func (focusedRuntimeRepository) FindUserSessionByCompactUsername(string) (*model.UserSession, error) {
	return nil, nil
}

func (focusedRuntimeRepository) CreateAuditSession(*model.AuditSession) error {
	return nil
}

func (focusedRuntimeRepository) EndAuditSession(string) error {
	return nil
}

func (focusedRuntimeRepository) CreateAuditSSHCommand(*model.AuditSSHCommand) error {
	return nil
}

func (focusedRuntimeRepository) CreateAuditSFTPEvent(*model.AuditSFTPEvent) error {
	return nil
}

func (focusedRuntimeRepository) UpdateAuditProtocol(string, string) error {
	return nil
}

func TestNewAcceptsFocusedRuntimeRepository(t *testing.T) {
	t.Parallel()

	server, err := New(
		&config.Config{},
		focusedRuntimeRepository{},
		&captureConnectionAuthorizer{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		online.NewRegistry(),
	)
	if err != nil {
		t.Fatalf("New with focused repository: %v", err)
	}
	if server == nil {
		t.Fatal("New returned nil server")
	}
}
