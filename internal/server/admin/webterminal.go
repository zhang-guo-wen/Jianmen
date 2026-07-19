package admin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"jianmen/internal/model"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

type webTerminalAuditSinkStore interface {
	CreateAuditSSHCommand(event *model.AuditSSHCommand) error
	CreateAuditSFTPEvent(event *model.AuditSFTPEvent) error
	UpdateAuditProtocol(id, protocol string) error
}

const (
	webTerminalPath        = "/api/web-terminal"
	defaultTerminalTerm    = "xterm-256color"
	defaultTerminalColumns = 80
	defaultTerminalRows    = 24
	maxTerminalDimension   = 1000
)

var webTerminalUpgrader = websocket.Upgrader{
	CheckOrigin: sameOriginOrNoOrigin,
}

type webTerminalOptions struct {
	TargetID string
	Term     string
	Columns  int
	Rows     int
}

type webTerminalAuditSink struct {
	store          webTerminalAuditSinkStore
	sessionID      string
	onlineSessions *online.Registry
}

func (s *webTerminalAuditSink) WriteCommand(_ string, timestamp time.Time, command string) error {
	return s.store.CreateAuditSSHCommand(&model.AuditSSHCommand{
		AuditSessionID: s.sessionID,
		Timestamp:      timestamp,
		Command:        command,
	})
}

func (s *webTerminalAuditSink) WriteFileEvent(_ string, timestamp time.Time, action, path string, size int64, result string) error {
	return s.store.CreateAuditSFTPEvent(&model.AuditSFTPEvent{
		AuditSessionID: s.sessionID,
		Timestamp:      timestamp,
		Action:         action,
		Path:           path,
		Size:           size,
		Result:         result,
	})
}

func (s *webTerminalAuditSink) UpdateProtocol(_ string, protocol string) error {
	s.onlineSessions.UpdateProtocolSubtype(s.sessionID, protocol)
	return s.store.UpdateAuditProtocol(s.sessionID, protocol)
}

func (s *Server) handleWebTerminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	opts, err := webTerminalOptionsFromRequest(r)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" ||
		r.URL.Query().Get("token") != "" ||
		r.URL.Query().Get("access_token") != "" {
		s.writeErrorText(w, r, http.StatusUnauthorized, "legacy credentials are not accepted for websocket connections")
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("ticket")) == "" {
		s.writeErrorText(w, r, http.StatusUnauthorized, "missing or invalid websocket ticket")
		return
	}
	if s.browserSessions == nil || s.identity == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "browser session service unavailable")
		return
	}
	browserSession, found, err := s.browserSessions.ConsumeWebSocketTicket(r.Context(), r.URL.Query().Get("ticket"), opts.TargetID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to consume websocket ticket")
		return
	}
	if !found {
		s.writeErrorText(w, r, http.StatusUnauthorized, "missing or invalid websocket ticket")
		return
	}
	subject, found, err := s.identity.FindIdentitySubject(r.Context(), browserSession.UserID)
	if err != nil || !found {
		s.writeErrorText(w, r, http.StatusUnauthorized, "invalid websocket session identity")
		return
	}
	user := model.User{ID: subject.ID, Username: subject.Username, IsSuperAdmin: subject.SuperAdmin, Status: subject.Status, ExpiresAt: subject.ExpiresAt}
	target, err := s.resolveWebTerminalTarget(r.Context(), user, opts.TargetID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, err.Error())
		return
	}
	if target.Disabled {
		s.writeErrorText(w, r, http.StatusForbidden, "target is disabled or unavailable")
		return
	}
	if target.Expired(time.Now().UTC()) {
		s.writeErrorText(w, r, http.StatusForbidden, "target account has expired")
		return
	}
	if !strings.EqualFold(target.Protocol, "ssh") {
		s.writeErrorText(w, r, http.StatusBadRequest, "target is not an SSH account")
		return
	}
	allowed, err := s.authorizeConnection(r.Context(), user.ID, rbac.ActionSessionConnect, model.ResourceTypeHostAccount, target.ID)
	if err != nil {
		s.logger.Warn("web terminal authorization failed", "user", user.Username, "target", target.ID, "error", err)
		s.writeErrorText(w, r, http.StatusForbidden, "connection is not authorized")
		return
	}
	if !allowed {
		s.writeErrorText(w, r, http.StatusForbidden, "connection is not authorized")
		return
	}
	targetClient, err := dialWebTerminalTarget(target)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadGateway, err.Error())
		return
	}

	conn, err := webTerminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = targetClient.Close()
		s.logger.Warn("web terminal websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	session := newWebTerminalSession(r, user, target)
	auditSession := s.startWebTerminalAudit(session, target)
	if auditSession == nil {
		_ = targetClient.Close()
		writeWebTerminalClose(conn, errors.New("audit service unavailable"))
		return
	}
	if auditSession != nil {
		defer func() {
			if err := s.audit.EndAuditSession(auditSession.ID); err != nil {
				s.logger.Warn("failed to end web terminal audit session", "session", auditSession.ID, "error", err)
			}
		}()
	}

	recorder, err := s.newWebTerminalRecorder(session, auditSession, func(error) {
		_ = conn.Close()
		_ = targetClient.Close()
	})
	if err != nil {
		_ = targetClient.Close()
		writeWebTerminalClose(conn, errors.New("audit recorder unavailable"))
		s.logger.Warn("failed to initialize web terminal recorder", "target", target.ID, "error", err)
		return
	}
	if recorder != nil {
		defer recorder.Close()
	}

	if auditSession != nil {
		accountName := target.Name
		if accountName == "" {
			accountName = target.Username
		}
		unregisterOnline := s.onlineSessions.Register(online.Session{
			ID:              auditSession.ID,
			AuditSessionID:  auditSession.ID,
			ResourceType:    model.ResourceTypeHost,
			ResourceID:      target.HostID,
			AccountID:       target.ID,
			Instance:        target.HostName,
			Protocol:        "ssh",
			ProtocolSubtype: session.ProtocolSubtype,
			Account:         accountName,
			Operator:        user.Username,
			StartedAt:       auditSession.StartedAt,
			HasReplay:       recorder != nil,
		}, func() {
			_ = conn.Close()
			_ = targetClient.Close()
		})
		defer unregisterOnline()
	}

	if err := serveWebTerminalSSHSession(r.Context(), conn, targetClient, opts, recorder, s.logger); err != nil && r.Context().Err() == nil {
		writeWebTerminalClose(conn, err)
		s.logger.Warn("web terminal session ended with error", "target", target.ID, "error", err)
	}
}

func (s *Server) resolveWebTerminalTarget(ctx context.Context, user model.User, targetID string) (store.TargetConfig, error) {
	user.RequestedTargetID = targetID
	target, err := s.hostTargets.DefaultTarget(ctx, user)
	if err != nil {
		return store.TargetConfig{}, err
	}
	return target, nil
}

func webTerminalOptionsFromRequest(r *http.Request) (webTerminalOptions, error) {
	query := r.URL.Query()
	columns, err := positiveIntQuery(query, "cols", defaultTerminalColumns)
	if err != nil {
		return webTerminalOptions{}, err
	}
	rows, err := positiveIntQuery(query, "rows", defaultTerminalRows)
	if err != nil {
		return webTerminalOptions{}, err
	}
	term := strings.TrimSpace(query.Get("term"))
	if term == "" {
		term = defaultTerminalTerm
	}
	return webTerminalOptions{
		TargetID: firstNonEmpty(strings.TrimSpace(query.Get("target_id")), strings.TrimSpace(query.Get("target"))),
		Term:     term,
		Columns:  columns,
		Rows:     rows,
	}, nil
}

func positiveIntQuery(query url.Values, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(query.Get(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > maxTerminalDimension {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return value, nil
}

func dialWebTerminalTarget(target store.TargetConfig) (*ssh.Client, error) {
	clientConfig, err := store.ClientConfigForTarget(target)
	if err != nil {
		return nil, err
	}
	if clientConfig.Timeout == 0 {
		clientConfig.Timeout = 10 * time.Second
	}
	return ssh.Dial("tcp", target.Addr(), clientConfig)
}

func webTerminalClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
