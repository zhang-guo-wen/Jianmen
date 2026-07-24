package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"jianmen/internal/service"
)

type InitStatusResponse struct {
	Initialized         bool `json:"initialized"`
	LoginCaptchaEnabled bool `json:"login_captcha_enabled"`
}

type SetupRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type SetupResponse struct {
	CSRFToken string `json:"csrf_token"`
}

type EncryptionKeyResponse struct {
	Key string `json:"key"`
}

type EncryptionKeyRequest struct {
	Password string `json:"password"`
}

func (s *Server) handleInitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.adminAuth == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	initialized, err := s.adminAuth.Initialized(r.Context())
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to check setup status")
		return
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	s.writeJSON(w, r, http.StatusOK, InitStatusResponse{
		Initialized:         initialized,
		LoginCaptchaEnabled: s.cfg.Admin.LoginCaptchaEnabled,
	})
}

func (s *Server) handleLoginCaptchaChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.cfg.Admin.LoginCaptchaEnabled {
		s.writeErrorText(w, r, http.StatusNotFound, "login captcha is disabled")
		return
	}
	if s.loginCaptcha == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "login captcha unavailable")
		return
	}

	challenge, err := s.loginCaptcha.CreateChallenge()
	if err != nil {
		s.logger.Error("failed to create login captcha challenge", "error", err)
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to create login captcha challenge")
		return
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(challenge); err != nil {
		s.logger.Error("failed to encode login captcha challenge", "error", err)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.adminAuth == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)

	var req struct {
		Username       string `json:"username"`
		Password       string `json:"password"`
		CaptchaPayload string `json:"captcha_payload"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "username and password are required")
		return
	}
	now := time.Now().UTC()
	limiter := s.loginLimiterForRequest()
	limitKey := loginLimitKey(r, username)
	if retryAfter := limiter.retryAfter(limitKey, now); retryAfter > 0 {
		setRetryAfter(w, retryAfter)
		s.logLogin(r, username, "", "blocked", "rate_limited", http.StatusTooManyRequests)
		s.writeErrorText(w, r, http.StatusTooManyRequests, "too many failed login attempts; try again later")
		return
	}
	if s.cfg.Admin.LoginCaptchaEnabled {
		if s.loginCaptcha == nil {
			s.writeErrorText(w, r, http.StatusServiceUnavailable, "login captcha unavailable")
			return
		}
		if err := s.loginCaptcha.Verify(req.CaptchaPayload); err != nil {
			s.logLogin(r, username, "", "failure", "captcha_failed", http.StatusBadRequest)
			message := "security verification failed; please try again"
			if errors.Is(err, service.ErrLoginCaptchaMissing) {
				message = "security verification is required"
			} else if errors.Is(err, service.ErrLoginCaptchaExpired) {
				message = "security verification expired; please try again"
			}
			s.writeErrorText(w, r, http.StatusBadRequest, message)
			return
		}
	}

	login, err := s.adminAuth.VerifyLogin(r.Context(), username, req.Password)
	if errors.Is(err, service.ErrAdminInvalidCredentials) {
		limiter.recordFailure(limitKey, now)
		s.logLogin(r, username, "", "failure", "invalid_credentials", http.StatusUnauthorized)
		s.writeErrorText(w, r, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err != nil {
		s.logger.Error("admin login credential lookup failed", "error", err)
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to authenticate")
		return
	}

	intentID, err := s.beginLoginAudit(r, username, login.UserID)
	if err != nil {
		s.logger.Error("admin login audit gate failed", "user_id", login.UserID, "error", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "login audit unavailable")
		return
	}
	session, err := s.adminAuth.CompleteLogin(r.Context(), login)
	if err != nil {
		reason := "login_state_persist_failed"
		message := "failed to save login state"
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrAdminSessionCreate) {
			reason = "session_create_failed"
			message = "failed to create browser session"
		} else if errors.Is(err, service.ErrAdminInvalidCredentials) {
			reason = "credentials_changed"
			message = "invalid username or password"
			status = http.StatusUnauthorized
		}
		s.recordLoginAuditFailure(r, username, login.UserID, intentID, reason, status)
		s.writeErrorText(w, r, status, message)
		return
	}
	if err := s.recordLoginAuditResult(r, username, login.UserID, intentID, "success", "", http.StatusOK); err != nil {
		s.logger.Error("admin login result audit failed", "user_id", login.UserID, "intent_id", intentID, "error", err)
		ctx, cancel := detachedAuditWriteContext(r.Context())
		revokeErr := s.browserSessions.Revoke(ctx, session.SessionID)
		cancel()
		if revokeErr != nil {
			s.logger.Error("failed to revoke unaudited admin session", "user_id", login.UserID, "session_id", session.SessionID, "error", revokeErr)
		}
		s.recordLoginAuditFailure(r, username, login.UserID, intentID, "success_audit_failed", http.StatusServiceUnavailable)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "login audit unavailable")
		return
	}

	limiter.reset(limitKey)
	s.logLoginOutcome(s.loginLogger(), r, username, "success", "")
	setBrowserSessionCookie(w, r, session.Secret, session.ExpiresAt, s.cfg.Admin.PublicURL)
	s.writeJSON(w, r, http.StatusOK, map[string]string{"csrf_token": session.CSRFToken})
}

func setBrowserSessionCookie(w http.ResponseWriter, r *http.Request, secret string, expiresAt time.Time, publicURL string) {
	http.SetCookie(w, &http.Cookie{
		Name: "jianmen_session", Value: secret, Path: "/", HttpOnly: true,
		Secure: secureRequest(r, publicURL), SameSite: http.SameSiteLaxMode, Expires: expiresAt,
	})
}

func clearBrowserSessionCookie(w http.ResponseWriter, r *http.Request, publicURL string) {
	http.SetCookie(w, &http.Cookie{
		Name: "jianmen_session", Value: "", Path: "/", HttpOnly: true,
		Secure: secureRequest(r, publicURL), SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func secureRequest(r *http.Request, publicURL string) bool {
	if r != nil && r.TLS != nil {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}

func (s *Server) handleInitSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.adminAuth == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req SetupRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := s.adminAuth.Setup(r.Context(), service.AdminSetupInput{
		Username: req.Username, Password: req.Password, Email: req.Email, DisplayName: req.DisplayName,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.writeErrorText(w, r, http.StatusRequestTimeout, "setup request canceled")
			return
		}
		switch {
		case errors.Is(err, service.ErrAdminAlreadyInitialized):
			s.writeErrorText(w, r, http.StatusForbidden, "already initialized")
		case errors.Is(err, service.ErrAdminInvalidSetup):
			s.writeErrorText(w, r, http.StatusBadRequest, adminSetupErrorMessage(err))
		case errors.Is(err, service.ErrAdminSessionCreate):
			s.writeErrorText(w, r, http.StatusInternalServerError, "failed to create browser session")
		default:
			s.writeErrorText(w, r, http.StatusInternalServerError, "failed to create user")
		}
		return
	}
	setBrowserSessionCookie(w, r, session.Secret, session.ExpiresAt, s.cfg.Admin.PublicURL)
	s.writeJSON(w, r, http.StatusCreated, SetupResponse{CSRFToken: session.CSRFToken})
}

func adminSetupErrorMessage(err error) string {
	message := strings.TrimSpace(strings.TrimPrefix(err.Error(), service.ErrAdminInvalidSetup.Error()+":"))
	if message == "" {
		return "invalid setup request"
	}
	return message
}

func (s *Server) handleInitEncryptionKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.adminAuth == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var req EncryptionKeyRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	key, err := s.adminAuth.ClaimEncryptionKey(r.Context(), userIDFromRequest(r), req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAdminSetupNotCompleted):
			s.writeErrorText(w, r, http.StatusPreconditionFailed, "setup not completed")
		case errors.Is(err, service.ErrAdminEncryptionKeyClaimed):
			s.writeErrorText(w, r, http.StatusForbidden, "encryption key has already been retrieved")
		case errors.Is(err, service.ErrAdminEncryptionKeyDenied):
			s.forbidden(w, r)
		default:
			s.writeErrorText(w, r, http.StatusInternalServerError, "failed to retrieve encryption key")
		}
		return
	}
	s.writeJSON(w, r, http.StatusOK, EncryptionKeyResponse{Key: key})
}
