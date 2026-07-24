package admin

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"

	"jianmen/internal/model"
	"jianmen/internal/recording"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

func newWebTerminalSession(r *http.Request, user model.User, target store.TargetConfig) model.Session {
	user.RequestedTargetID = target.ID
	session := model.NewSession(user, target.ID, target.Addr(), webTerminalClientIP(r))
	session.Protocol = "ssh"
	session.ProtocolSubtype = "web-terminal"
	session.HostID = target.HostID
	session.AccountID = target.ID
	session.AccountUsername = target.Username
	session.HostIP = target.Host
	session.ConnIP = target.Host
	session.ConnPort = target.Port
	return session
}

func (s *Server) startWebTerminalAudit(
	ctx context.Context,
	session model.Session,
	target store.TargetConfig,
) *model.AuditSession {
	auditSession := &model.AuditSession{
		UserID:          session.UserID,
		Username:        session.UserUsername,
		Protocol:        "ssh",
		ProtocolSubtype: session.ProtocolSubtype,
		ResourceType:    model.ResourceTypeHostAccount,
		ResourceID:      target.ID,
		HostID:          target.HostID,
		AccountID:       target.ID,
		TargetName:      target.HostName,
		TargetAddress:   target.Addr(),
		AccountName:     target.Name,
		AccountUsername: target.Username,
		ClientIP:        session.ClientIP,
		StartedAt:       session.StartedAt,
		State:           "started",
		Outcome:         model.AuditOutcomeActive,
		ReplayDir:       filepath.Join(s.cfg.ReplayDir, "ssh", session.ID),
	}
	if err := s.audit.CreateAuditSession(ctx, auditSession); err != nil {
		s.logger.Warn("failed to create web terminal audit session", "session", session.ID, "error", err)
		return nil
	}
	return auditSession
}

func (s *Server) endWebTerminalAudit(parent context.Context, sessionID string) {
	ctx, cancel := detachedAuditWriteContext(parent)
	defer cancel()
	if err := s.audit.EndAuditSession(ctx, sessionID); err != nil {
		s.logger.Warn("failed to end web terminal audit session", "session", sessionID, "error", err)
	}
}

func (s *Server) newWebTerminalRecorder(
	ctx context.Context,
	session model.Session,
	auditSession *model.AuditSession,
	onFatal func(error),
) (*recording.SessionRecorder, error) {
	if s == nil || s.cfg == nil || !s.cfg.Recording.Enabled {
		return nil, nil
	}
	if auditSession == nil {
		return nil, errors.New("web terminal audit session is required")
	}
	recorder, err := recording.NewSessionRecorder(
		s.cfg.ReplayDir,
		session,
		s.cfg.Recording.RecordInput,
		s.cfg.Recording.RecordCommands,
		service.NewAuditPolicy(s.cfg.Recording.RetentionDays, s.cfg.Recording.RecordInput),
		onFatal,
		s.logger,
		&webTerminalAuditSink{
			ctx: ctx, store: s.audit, sessionID: auditSession.ID,
			onlineSessions: s.onlineSessions,
		},
	)
	if err != nil {
		return nil, err
	}
	s.logger.Info("web terminal recording started",
		"session", session.ID,
		"target", session.Target,
		"client", session.ClientIP,
	)
	return recorder, nil
}
