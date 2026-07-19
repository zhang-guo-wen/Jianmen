package sshserver

import (
	"context"
	"time"
)

const auditFinalizeTimeout = 5 * time.Second

func (s *Server) endAuditSession(parent context.Context, sessionID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), auditFinalizeTimeout)
	defer cancel()
	if err := s.auditSessions.EndAuditSession(ctx, sessionID); err != nil && s.logger != nil {
		s.logger.Warn("failed to end SSH audit session", "session", sessionID, "error", err)
	}
}
