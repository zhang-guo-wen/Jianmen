package admin

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/pkg/apiresp"
)

const (
	loginFailureLimit  = 5
	loginFailureWindow = 15 * time.Minute
	loginLockout       = 5 * time.Minute

	loginAuditOutcomePending = "pending"
	loginAuditReasonIntent   = "intent"
)

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
	limit    int
	window   time.Duration
	lockout  time.Duration
}

type loginAttempt struct {
	failures     int
	firstFailure time.Time
	lockedUntil  time.Time
}

func newLoginLimiter(limit int, window, lockout time.Duration) *loginLimiter {
	return &loginLimiter{
		attempts: make(map[string]loginAttempt),
		limit:    limit,
		window:   window,
		lockout:  lockout,
	}
}

func newDefaultLoginLimiter() *loginLimiter {
	return newLoginLimiter(loginFailureLimit, loginFailureWindow, loginLockout)
}

func (l *loginLimiter) retryAfter(key string, now time.Time) time.Duration {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	attempt := l.attempts[key]
	if attempt.lockedUntil.After(now) {
		return attempt.lockedUntil.Sub(now)
	}
	if !attempt.firstFailure.IsZero() && now.Sub(attempt.firstFailure) > l.window {
		delete(l.attempts, key)
	}
	return 0
}

func (l *loginLimiter) recordFailure(key string, now time.Time) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	attempt := l.attempts[key]
	if attempt.firstFailure.IsZero() || now.Sub(attempt.firstFailure) > l.window {
		attempt = loginAttempt{firstFailure: now}
	}
	attempt.failures++
	if attempt.failures >= l.limit {
		attempt.lockedUntil = now.Add(l.lockout)
	}
	l.attempts[key] = attempt
}

func (l *loginLimiter) reset(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (s *Server) loginLimiterForRequest() *loginLimiter {
	if s.loginLimiter == nil {
		s.loginLimiter = newDefaultLoginLimiter()
	}
	return s.loginLimiter
}

func loginLimitKey(r *http.Request, username string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "\x00" + requestClientIP(r)
}

func requestClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func setRetryAfter(w http.ResponseWriter, d time.Duration) {
	seconds := int(math.Ceil(d.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
}

func (s *Server) beginLoginAudit(r *http.Request, username, userID string) (string, error) {
	entry := s.newLoginAuditLog(r, username, userID, loginAuditOutcomePending, loginAuditReasonIntent)
	entry.ID = model.NewID()
	entry.Phase = "intent"
	entry.Result = loginAuditOutcomePending
	entry.StatusCode = 0
	if err := s.createLoginAuditLog(r, entry); err != nil {
		return "", err
	}
	return entry.ID, nil
}

func (s *Server) recordLoginAuditResult(
	r *http.Request,
	username, userID, intentID, outcome, reason string,
	statusCode int,
) error {
	entry := s.newLoginAuditLog(r, username, userID, outcome, reason)
	entry.IntentID = strings.TrimSpace(intentID)
	entry.StatusCode = statusCode
	return s.createLoginAuditLog(r, entry)
}

func (s *Server) recordLoginAuditFailure(
	r *http.Request,
	username, userID, intentID, reason string,
	statusCode int,
) {
	if err := s.recordLoginAuditResult(r, username, userID, intentID, "failure", reason, statusCode); err != nil {
		logger := s.loginLogger()
		logger.Warn("failed to write login audit result", "username", username, "outcome", "failure", "intent_id", intentID, "error", err)
	}
}

func (s *Server) logLogin(r *http.Request, username, userID, outcome, reason string, statusCodes ...int) error {
	entry := s.newLoginAuditLog(r, username, userID, outcome, reason)
	if len(statusCodes) > 0 {
		entry.StatusCode = statusCodes[0]
	}
	auditErr := s.createLoginAuditLog(r, entry)
	logger := s.loginLogger()
	if auditErr != nil {
		logger.Warn("failed to write login audit log", "username", username, "outcome", outcome, "error", auditErr)
	}
	s.logLoginOutcome(logger, r, username, outcome, reason)
	return auditErr
}

func (s *Server) newLoginAuditLog(r *http.Request, username, userID, outcome, reason string) *model.LoginAuditLog {
	return &model.LoginAuditLog{
		UserID:     userID,
		Username:   username,
		Phase:      "result",
		Result:     outcome,
		RequestID:  apiresp.RequestID(r.Context()),
		StatusCode: loginAuditStatusCode(outcome, reason),
		Outcome:    outcome,
		Reason:     strings.TrimSpace(reason),
		ClientIP:   requestClientIP(r),
		UserAgent:  r.UserAgent(),
	}
}

func loginAuditStatusCode(outcome, reason string) int {
	switch strings.TrimSpace(reason) {
	case "rate_limited":
		return http.StatusTooManyRequests
	case "captcha_failed":
		return http.StatusBadRequest
	case "invalid_credentials", "credentials_changed":
		return http.StatusUnauthorized
	case "login_state_persist_failed", "session_create_failed":
		return http.StatusInternalServerError
	case "success_audit_failed":
		return http.StatusServiceUnavailable
	}
	if strings.TrimSpace(outcome) == "success" {
		return http.StatusOK
	}
	return 0
}

func (s *Server) createLoginAuditLog(r *http.Request, entry *model.LoginAuditLog) error {
	if s.audit == nil {
		return errors.New("login audit unavailable")
	}
	ctx, cancel := detachedAuditWriteContext(r.Context())
	defer cancel()
	return s.audit.CreateLoginAuditLog(ctx, entry)
}

func (s *Server) loginLogger() *slog.Logger {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	return logger
}

func (s *Server) logLoginOutcome(logger *slog.Logger, r *http.Request, username, outcome, reason string) {
	attrs := []any{
		"username", username,
		"client_ip", requestClientIP(r),
		"outcome", outcome,
	}
	if reason != "" {
		attrs = append(attrs, "reason", reason)
	}
	if outcome == "success" {
		logger.Info("admin login", attrs...)
		return
	}
	logger.Warn("admin login", attrs...)
}
