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
)

const (
	loginFailureLimit  = 5
	loginFailureWindow = 15 * time.Minute
	loginLockout       = 5 * time.Minute
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

func (s *Server) logLogin(r *http.Request, username, userID, outcome, reason string) error {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	var auditErr error
	if s.audit == nil {
		auditErr = errors.New("login audit unavailable")
	} else {
		ctx, cancel := detachedAuditWriteContext(r.Context())
		defer cancel()
		if err := s.audit.CreateLoginAuditLog(ctx, &model.LoginAuditLog{
			UserID:    userID,
			Username:  username,
			Outcome:   outcome,
			Reason:    reason,
			ClientIP:  requestClientIP(r),
			UserAgent: r.UserAgent(),
		}); err != nil {
			auditErr = err
			logger.Warn("failed to write login audit log", "username", username, "outcome", outcome, "error", err)
		}
	}
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
		return auditErr
	}
	logger.Warn("admin login", attrs...)
	return auditErr
}
