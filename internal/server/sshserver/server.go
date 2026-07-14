package sshserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/proxy/sshproxy"
	"jianmen/internal/rbac"
	"jianmen/internal/recording"
	"jianmen/internal/server/admin"
	"jianmen/internal/store"
)

type Server struct {
	cfg                  *config.Config
	store                store.Store
	rbacChecker          *rbac.Checker
	resourceGrantChecker *rbac.ResourceGrantChecker
	logger               *slog.Logger
	superAdminIDs        map[string]bool
}

// auditStore adapts store.Store to recording.AuditSink.
type auditStore struct {
	store     store.Store
	sessionID string
}

func (a *auditStore) WriteCommand(sessionID string, timestamp time.Time, command string) error {
	return a.store.CreateAuditSSHCommand(&model.AuditSSHCommand{
		AuditSessionID: a.sessionID,
		Timestamp:      timestamp,
		Command:        command,
	})
}

func (a *auditStore) WriteFileEvent(sessionID string, timestamp time.Time, action, path string, size int64, result string) error {
	return a.store.CreateAuditSFTPEvent(&model.AuditSFTPEvent{
		AuditSessionID: a.sessionID,
		Timestamp:      timestamp,
		Action:         action,
		Path:           path,
		Size:           size,
		Result:         result,
	})
}

func (a *auditStore) UpdateProtocol(sessionID string, protocol string) error {
	return a.store.UpdateAuditProtocol(a.sessionID, protocol)
}

func New(cfg *config.Config, s store.Store, logger *slog.Logger, dataDir string, dbs ...*gorm.DB) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	var checker *rbac.Checker
	var resourceGrantChecker *rbac.ResourceGrantChecker
	if len(dbs) > 0 && dbs[0] != nil {
		checker = rbac.NewChecker(dbs[0])
		resourceGrantChecker = rbac.NewResourceGrantChecker(dbs[0])
	}
	return &Server{
		cfg:                  cfg,
		store:                s,
		rbacChecker:          checker,
		resourceGrantChecker: resourceGrantChecker,
		logger:               logger,
		superAdminIDs:        admin.LoadSuperAdminIDs(cfg, dataDir),
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	signers, err := loadOrCreateHostSigners(s.cfg.HostKeyPath)
	if err != nil {
		return err
	}

	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			user, err := s.store.Authenticate(ctx, conn.User(), string(password))
			if err != nil {
				return nil, err
			}
			return permissionsForUser(user), nil
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			user, err := s.store.AuthenticatePublicKey(ctx, conn.User(), key)
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
			user, err := s.store.Authenticate(ctx, conn.User(), answers[0])
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
	defer rawConn.Close()

	serverConn, chans, reqs, err := ssh.NewServerConn(rawConn, serverConfig)
	if err != nil {
		s.logger.Warn("ssh handshake failed", "remote", rawConn.RemoteAddr().String(), "error", err)
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(reqs)

	user := userFromPermissions(serverConn)
	target, err := s.store.DefaultTarget(ctx, user)
	if err != nil {
		s.logger.Warn("failed to resolve target", "user", user.Username, "error", err)
		return
	}

	if reason := targetUnavailableReason(target, time.Now().UTC()); reason != "" {
		s.logger.Warn("target is unavailable", "user", user.Username, "target", target.ID, "reason", reason, "expires_at", target.ExpiresAt)
		return
	}

	if !s.superAdminIDs[user.ID] {
		// 检查菜单权限：session:connect
		if s.rbacChecker != nil {
			allowed, err := s.rbacChecker.HasPermission(user.ID, rbac.ActionSessionConnect, "", "")
			if err != nil {
				s.logger.Warn("rbac check failed", "user", user.Username, "target", target.ID, "error", err)
				return
			}
			if !allowed {
				s.logger.Warn("rbac denied session:connect permission", "user", user.Username, "target", target.ID)
				return
			}
		}

		// 检查资源授权：对目标主机账户的连接权限
		if s.resourceGrantChecker != nil {
			allowed, err := s.resourceGrantChecker.HasGrant(user.ID, model.ResourceTypeHostAccount, target.ID)
			if err != nil {
				s.logger.Warn("resource grant check failed", "user", user.Username, "target", target.ID, "error", err)
				return
			}
			if !allowed {
				s.logger.Warn("resource grant denied", "user", user.Username, "target", target.ID)
				return
			}
		}
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

	session := model.NewSession(user, target.ID, target.Name, remoteIP(rawConn.RemoteAddr()))
	session.AccountUsername = target.Username

	// Look up UserSession from compact username to link the audit record.
	userSession, _ := s.store.FindUserSessionByCompactUsername(serverConn.User())

	auditSession := model.AuditSession{
		UserID:      user.ID,
		Username:    user.Username,
		Protocol:    "ssh",
		TargetName:  target.Name,
		AccountName: target.Username,
		ClientIP:    session.ClientIP,
		StartedAt:   time.Now().UTC(),
		State:       "started",
		ReplayDir:   filepath.Join(s.cfg.ReplayDir, "ssh", session.ID),
	}
	if userSession != nil {
		auditSession.UserSessionID = userSession.ID
	}
	auditSession.BeforeCreate(nil)
	s.store.CreateAuditSession(&auditSession)

	defer func() {
		s.store.EndAuditSession(auditSession.ID)
	}()

	var recorder *recording.SessionRecorder
	if s.cfg.Recording.Enabled {
		recorder, err = recording.NewSessionRecorder(
			s.cfg.ReplayDir,
			session,
			s.cfg.Recording.RecordInput,
			s.cfg.Recording.RecordCommands,
			s.logger,
			&auditStore{store: s.store, sessionID: auditSession.ID},
		)
		if err != nil {
			s.logger.Warn("failed to initialize recorder", "session", session.ID, "error", err)
		} else {
			defer recorder.Close()
		}
	}

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
		proxy := sshproxy.NewSession(targetClient, channel, requests, recorder, s.logger)
		go proxy.Serve(ctx)
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
