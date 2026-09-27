package sshserver

import (
	"context"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/model"
	"jianmen/internal/store"
)

type credentialAuthenticator interface {
	Authenticate(ctx context.Context, username, password string) (model.User, error)
	AuthenticatePublicKey(ctx context.Context, username string, key ssh.PublicKey) (model.User, error)
}

type targetResolver interface {
	DefaultTarget(ctx context.Context, user model.User) (store.TargetConfig, error)
}

type userSessionFinder interface {
	FindUserSessionByCompactUsername(compactUsername string) (*model.UserSession, error)
}

type auditSessionWriter interface {
	CreateAuditSession(context.Context, *model.AuditSession) error
	EndAuditSession(context.Context, string) error
}

type auditEventWriter interface {
	CreateAuditSSHCommand(context.Context, *model.AuditSSHCommand) error
	CreateAuditSFTPEvent(context.Context, *model.AuditSFTPEvent) error
	UpdateAuditProtocol(context.Context, string, string) error
}

// runtimeRepository is the construction boundary. Server fields retain the
// smallest capability interface needed by each SSH runtime concern.
type runtimeRepository interface {
	credentialAuthenticator
	targetResolver
	userSessionFinder
	auditSessionWriter
	auditEventWriter
}
