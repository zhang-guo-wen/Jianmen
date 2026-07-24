package sshserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/online"
	"jianmen/internal/proxy/sshproxy"
	"jianmen/internal/rbac"
	"jianmen/internal/recording"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

type Server struct {
	cfg            *config.Config
	authenticator  credentialAuthenticator
	targetResolver targetResolver
	userSessions   userSessionFinder
	auditSessions  auditSessionWriter
	auditEvents    auditEventWriter
	authorizer     connectionAuthorizer
	logger         *slog.Logger
	onlineSessions *online.Registry
}

// connectionAuthorizer is the protocol boundary shared with the authorization
// service without coupling the SSH server to service-owned types.
type connectionAuthorizer interface {
	AuthorizeConnection(ctx context.Context, userID string, actions []string, resourceType, resourceID string) (bool, error)
}

// auditStore adapts the SSH audit event boundary to recording.AuditSink.
type auditStore struct {
	ctx            context.Context
	store          auditEventWriter
	sessionID      string
	onlineSessions *online.Registry
}

func (a *auditStore) WriteCommand(sessionID string, timestamp time.Time, command string) error {
	return a.store.CreateAuditSSHCommand(a.ctx, &model.AuditSSHCommand{
		AuditSessionID: a.sessionID,
		Timestamp:      timestamp,
		Command:        command,
	})
}

func (a *auditStore) WriteFileEvent(sessionID string, timestamp time.Time, action, path string, size int64, result string) error {
	return a.store.CreateAuditSFTPEvent(a.ctx, &model.AuditSFTPEvent{
		AuditSessionID: a.sessionID,
		Timestamp:      timestamp,
		Action:         action,
		Path:           path,
		Size:           size,
		Result:         result,
	})
}

func (a *auditStore) UpdateProtocol(sessionID string, protocol string) error {
	a.onlineSessions.UpdateProtocolSubtype(a.sessionID, protocol)
	return a.store.UpdateAuditProtocol(a.ctx, a.sessionID, protocol)
}

func New(cfg *config.Config, repository runtimeRepository, authorizer connectionAuthorizer, logger *slog.Logger, onlineSessions *online.Registry) (*Server, error) {
	switch {
	case cfg == nil:
		return nil, errors.New("ssh server config is required")
	case authorizer == nil:
		return nil, errors.New("ssh server authorization service is required")
	case repository == nil:
		return nil, errors.New("ssh server repository is required")
	case logger == nil:
		return nil, errors.New("ssh server logger is required")
	case onlineSessions == nil:
		return nil, errors.New("ssh server online session registry is required")
	}
	return &Server{
		cfg:            cfg,
		authenticator:  repository,
		targetResolver: repository,
		userSessions:   repository,
		auditSessions:  repository,
		auditEvents:    repository,
		authorizer:     authorizer,
		logger:         logger,
		onlineSessions: onlineSessions,
	}, nil
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	signers, err := loadOrCreateHostSigners(s.cfg.HostKeyPath)
	if err != nil {
		return err
	}

	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			user, err := s.authenticator.Authenticate(ctx, conn.User(), string(password))
			if err != nil {
				return nil, err
			}
			return permissionsForUser(user), nil
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			user, err := s.authenticator.AuthenticatePublicKey(ctx, conn.User(), key)
			if err != nil {
				return nil, err
			}
			return permissionsForUser(user), nil
		},
		KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := client("", "", []string{"Password: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) != 1 {
				return nil, errors.New("keyboard-interactive password answer is required")
			}
			user, err := s.authenticator.Authenticate(ctx, conn.User(), answers[0])
			if err != nil {
				return nil, err
			}
			return permissionsForUser(user), nil
		},
	}
	for _, signer := range signers {
		serverConfig.AddHostKey(signer)
	}

	listener, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	s.logger.Info("ssh bastion listening", "addr", s.cfg.ListenAddr)

	var wg sync.WaitGroup
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "closed") {
				wg.Wait()
				return nil
			}
			return err
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handleConn(ctx, conn, serverConfig)
		}()
	}
}

func (s *Server) handleConn(ctx context.Context, rawConn net.Conn, serverConfig *ssh.ServerConfig) {
	ctx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()

	rawConn = &idempotentCloseConn{Conn: rawConn}
	stopConnectionWatcher := make(chan struct{})
	connectionWatcherDone := make(chan struct{})
	go func() {
		defer close(connectionWatcherDone)
		select {
		case <-ctx.Done():
			_ = rawConn.Close()
		case <-stopConnectionWatcher:
		}
	}()
	defer func() {
		close(stopConnectionWatcher)
		_ = rawConn.Close()
		<-connectionWatcherDone
	}()

	serverConn, chans, reqs, err := ssh.NewServerConn(rawConn, serverConfig)
	if err != nil {
		s.logger.Warn("ssh handshake failed", "remote", rawConn.RemoteAddr().String(), "error", err)
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(reqs)

	user := userFromPermissions(serverConn)
	// 将用户 ID 注入 context，供 GORM 审计 Hook 使用
	ctx = model.WithAuditUserID(ctx, user.ID)
	target, err := s.targetResolver.DefaultTarget(ctx, user)
	if err != nil {
		s.logger.Warn("failed to resolve target", "user", user.Username, "error", err)
		return
	}

	if reason := targetUnavailableReason(target, time.Now().UTC()); reason != "" {
		s.logger.Warn("target is unavailable", "user", user.Username, "target", target.ID, "reason", reason, "expires_at", target.ExpiresAt)
		return
	}

	access, err := s.authorizeTarget(ctx, user.ID, target.ID)
	if err != nil {
		s.logger.Warn("SSH target authorization failed", "user", user.Username, "target", target.ID, "error", err)
		return
	}

	clientConfig, err := store.ClientConfigForTarget(target)
	if err != nil {
		s.logger.Warn("failed to build target client config", "target", target.Name, "error", err)
		return
	}
	targetClient, err := ssh.Dial("tcp", target.Addr(), clientConfig)
	if err != nil {
		s.logger.Warn("failed to connect target", "target", target.Name, "addr", target.Addr(), "error", err)
		return
	}
	defer targetClient.Close()

	session := model.NewSession(user, target.ID, target.Addr(), remoteIP(rawConn.RemoteAddr()))
	session.AccountUsername = target.Username

	// Look up UserSession from compact username to link the audit record.
	userSession, _ := s.userSessions.FindUserSessionByCompactUsername(serverConn.User())

	auditSession := newSSHAuditSession(user, target, session, s.cfg.ReplayDir)
	if userSession != nil {
		auditSession.UserSessionID = userSession.ID
	}
	auditSession.BeforeCreate(nil)
	if err := s.auditSessions.CreateAuditSession(ctx, &auditSession); err != nil {
		s.logger.Warn("failed to create SSH audit session", "session", session.ID, "error", err)
		return
	}

	defer s.endAuditSession(ctx, auditSession.ID)

	var recorder *recording.SessionRecorder
	if s.cfg.Recording.Enabled {
		recorder, err = recording.NewSessionRecorder(
			s.cfg.ReplayDir,
			session,
			s.cfg.Recording.RecordInput,
			s.cfg.Recording.RecordCommands,
			service.NewAuditPolicy(s.cfg.Recording.RetentionDays, s.cfg.Recording.RecordInput),
			func(error) {
				_ = targetClient.Close()
				_ = serverConn.Close()
				_ = rawConn.Close()
			},
			s.logger,
			&auditStore{
				ctx: ctx, store: s.auditEvents, sessionID: auditSession.ID,
				onlineSessions: s.onlineSessions,
			},
		)
		if err != nil {
			s.logger.Warn("failed to initialize recorder", "session", session.ID, "error", err)
			return
		}
		defer recorder.Close()
	}

	accountName := target.Name
	if accountName == "" {
		accountName = target.Username
	}
	unregisterOnline := s.onlineSessions.Register(online.Session{
		ID:             auditSession.ID,
		AuditSessionID: auditSession.ID,
		ResourceType:   model.ResourceTypeHost,
		ResourceID:     target.HostID,
		AccountID:      target.ID,
		Instance:       target.HostName,
		Protocol:       "ssh",
		Account:        accountName,
		Operator:       user.Username,
		StartedAt:      auditSession.StartedAt,
		HasReplay:      recorder != nil,
	}, func() {
		_ = targetClient.Close()
		_ = serverConn.Close()
		_ = rawConn.Close()
	})
	defer unregisterOnline()

	s.logger.Info("session started",
		"session", session.ID,
		"user", user.Username,
		"target", target.Name,
		"client", session.ClientIP,
	)
	defer s.logger.Info("session ended", "session", session.ID)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			s.logger.Warn("failed to accept channel", "session", session.ID, "error", err)
			continue
		}
		proxy := sshproxy.NewSession(targetClient, channel, requests, recorder, access, s.logger)
		go proxy.Serve(ctx)
	}
}

type idempotentCloseConn struct {
	net.Conn
	once sync.Once
	err  error
}

func (c *idempotentCloseConn) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
	})
	return c.err
}

func (s *Server) authorizeTarget(ctx context.Context, userID, targetID string) (sshproxy.Access, error) {
	if s.authorizer == nil {
		return sshproxy.Access{}, errors.New("ssh authorization unavailable")
	}

	sshAllowed, err := s.authorizer.AuthorizeConnection(
		ctx,
		userID,
		[]string{rbac.ActionSessionConnect},
		model.ResourceTypeHostAccount,
		targetID,
	)
	if err != nil {
		return sshproxy.Access{}, fmt.Errorf("authorize ssh connection: %w", err)
	}
	sftpAllowed, err := s.authorizer.AuthorizeConnection(
		ctx,
		userID,
		[]string{rbac.ActionSFTPConnect},
		model.ResourceTypeHostAccount,
		targetID,
	)
	if err != nil {
		return sshproxy.Access{}, fmt.Errorf("authorize sftp connection: %w", err)
	}
	access := sshproxy.Access{SSH: sshAllowed, SFTP: sftpAllowed}
	if !access.SSH && !access.SFTP {
		return sshproxy.Access{}, errors.New("ssh and sftp access denied")
	}
	return access, nil
}

func newSSHAuditSession(user model.User, target store.TargetConfig, session model.Session, replayRoot string) model.AuditSession {
	return model.AuditSession{
		UserID:          user.ID,
		Username:        user.Username,
		Protocol:        "ssh",
		TargetName:      target.HostName,
		TargetAddress:   target.Addr(),
		AccountName:     target.Name,
		AccountUsername: target.Username,
		ClientIP:        session.ClientIP,
		StartedAt:       session.StartedAt,
		State:           "started",
		ReplayDir:       filepath.Join(replayRoot, "ssh", session.ID),
	}
}

func targetUnavailableReason(target store.TargetConfig, now time.Time) string {
	if target.Disabled {
		return "disabled"
	}
	if target.Expired(now) {
		return "expired"
	}
	return ""
}

func permissionsForUser(user model.User) *ssh.Permissions {
	return &ssh.Permissions{
		Extensions: map[string]string{
			"user_id":             user.ID,
			"username":            user.Username,
			"requested_target_id": user.RequestedTargetID,
		},
	}
}

func userFromPermissions(conn *ssh.ServerConn) model.User {
	if conn.Permissions == nil {
		return model.User{Username: conn.User()}
	}
	return model.User{
		ID:                conn.Permissions.Extensions["user_id"],
		Username:          conn.Permissions.Extensions["username"],
		RequestedTargetID: conn.Permissions.Extensions["requested_target_id"],
	}
}

func remoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

func copyAndClose(dst io.Closer) {
	if dst != nil {
		_ = dst.Close()
	}
}
