package admin

import (
	"net/http"
	"path/filepath"

	"jianmen/internal/model"
	"jianmen/internal/recording"
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

func (s *Server) startWebTerminalAudit(session model.Session, target store.TargetConfig) *model.AuditSession {
	auditSession := &model.AuditSession{
		UserID:          session.UserID,
		Username:        session.UserUsername,
		Protocol:        "ssh",
		ProtocolSubtype: session.ProtocolSubtype,
		TargetName:      target.HostName,
		TargetAddress:   target.Addr(),
		AccountName:     target.Name,
		AccountUsername: target.Username,
		ClientIP:        session.ClientIP,
		StartedAt:       session.StartedAt,
		State:           "started",
		ReplayDir:       filepath.Join(s.cfg.ReplayDir, "ssh", session.ID),
	}
	if err := s.store.CreateAuditSession(auditSession); err != nil {
		s.logger.Warn("failed to create web terminal audit session", "session", session.ID, "error", err)
		return nil
	}
	return auditSession
}

func (s *Server) newWebTerminalRecorder(session model.Session, auditSession *model.AuditSession) *recording.SessionRecorder {
	if s == nil || s.cfg == nil || !s.cfg.Recording.Enabled {
		return nil
	}
	var sink recording.AuditSink
	if auditSession != nil {
		sink = &webTerminalAuditSink{store: s.store, sessionID: auditSession.ID}
	}

	recorder, err := recording.NewSessionRecorder(
		s.cfg.ReplayDir,
		session,
		s.cfg.Recording.RecordInput,
		s.cfg.Recording.RecordCommands,
		s.logger,
		sink,
	)
	if err != nil {
		s.logger.Warn("failed to initialize web terminal recorder", "target", session.TargetID, "error", err)
		return nil
	}
	s.logger.Info("web terminal recording started",
		"session", session.ID,
		"target", session.Target,
		"client", session.ClientIP,
	)
	return recorder
}
