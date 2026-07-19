package admin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/util"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InitStatusResponse 系统初始化状态
type InitStatusResponse struct {
	Initialized bool `json:"initialized"`
}

// SetupRequest 初始化设置请求
type SetupRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// SetupResponse 初始化设置响应
type SetupResponse struct {
	CSRFToken string `json:"csrf_token"`
}

// EncryptionKeyResponse 加密密钥响应
type EncryptionKeyResponse struct {
	Key string `json:"key"`
}

// handleInitStatus 返回系统初始化状态（检查是否已有用户）
func (s *Server) handleInitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	var count int64
	if err := s.db.Model(&model.User{}).Count(&count).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to check setup status")
		return
	}
	resp := InitStatusResponse{Initialized: count > 0}
	s.writeJSON(w, r, http.StatusOK, resp)
}

// handleLoginCaptchaChallenge returns a short-lived, single-use ALTCHA challenge.
func (s *Server) handleLoginCaptchaChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
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

// handleLogin handles username+password login, returns an API token.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.db == nil {
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
	password := req.Password
	if username == "" || password == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "username and password are required")
		return
	}
	if s.loginCaptcha == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "login captcha unavailable")
		return
	}
	now := time.Now().UTC()
	limiter := s.loginLimiterForRequest()
	limitKey := loginLimitKey(r, username)
	if retryAfter := limiter.retryAfter(limitKey, now); retryAfter > 0 {
		setRetryAfter(w, retryAfter)
		s.logLogin(r, username, "", "blocked", "rate_limited")
		s.writeErrorText(w, r, http.StatusTooManyRequests, "too many failed login attempts; try again later")
		return
	}

	if err := s.loginCaptcha.Verify(req.CaptchaPayload); err != nil {
		s.logLogin(r, username, "", "failure", "captcha_failed")
		message := "security verification failed; please try again"
		if errors.Is(err, service.ErrLoginCaptchaMissing) {
			message = "security verification is required"
		} else if errors.Is(err, service.ErrLoginCaptchaExpired) {
			message = "security verification expired; please try again"
		}
		s.writeErrorText(w, r, http.StatusBadRequest, message)
		return
	}

	// Verify the CAPTCHA before the expensive password hash check.
	// Find user by username
	var user model.User
	if err := s.db.Where("username = ? AND status = ?", username, "active").First(&user).Error; err != nil {
		limiter.recordFailure(limitKey, now)
		s.logLogin(r, username, "", "failure", "invalid_credentials")
		s.writeErrorText(w, r, http.StatusUnauthorized, "invalid username or password")
		return
	}

	if !verifyPassword(user.PasswordHash, password) {
		limiter.recordFailure(limitKey, now)
		s.logLogin(r, username, user.ID, "failure", "invalid_credentials")
		s.writeErrorText(w, r, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if user.MySQLNativeHash == "" {
		mysqlHash := util.MySQLNativePasswordHash(password)
		if err := s.db.Model(&user).Update("my_sql_native_hash", mysqlHash).Error; err != nil {
			s.logger.Warn("failed to backfill mysql password verifier", "user", user.ID, "error", err)
		} else {
			user.MySQLNativeHash = mysqlHash
		}
	}

	// Browser logins use an HttpOnly server-side session.  User token hashes are
	// retained exclusively for CLI and protocol identities.
	if err := s.logLogin(r, username, user.ID, "success", ""); err != nil {
		s.logger.Error("admin login audit gate failed", "user_id", user.ID, "error", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "login audit unavailable")
		return
	}
	user.LastLoginAt = &now
	if err := s.db.Save(&user).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to save login state")
		return
	}
	session, err := s.browserSessions.Create(r.Context(), user.ID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to create browser session")
		return
	}

	limiter.reset(limitKey)
	setBrowserSessionCookie(w, r, session.Secret, session.ExpiresAt, s.cfg.Admin.PublicURL)
	s.writeJSON(w, r, http.StatusOK, map[string]string{"csrf_token": session.CSRFToken})
}

func setBrowserSessionCookie(w http.ResponseWriter, r *http.Request, secret string, expiresAt time.Time, publicURL string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "jianmen_session",
		Value:    secret,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureRequest(r, publicURL),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

func clearBrowserSessionCookie(w http.ResponseWriter, r *http.Request, publicURL string) {
	http.SetCookie(w, &http.Cookie{Name: "jianmen_session", Value: "", Path: "/", HttpOnly: true, Secure: secureRequest(r, publicURL), SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func secureRequest(r *http.Request, publicURL string) bool {
	if r != nil && r.TLS != nil {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}

// handleInitSetup 创建超级管理员用户（事务保护 TOCTOU）
func (s *Server) handleInitSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.db == nil {
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

	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	email := strings.TrimSpace(req.Email)

	if username == "" || password == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "username and password are required")
		return
	}
	if len(password) < 8 {
		s.writeErrorText(w, r, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to hash password")
		return
	}

	releaseSetup, err := s.acquireSetupSlot(r.Context())
	if err != nil {
		s.writeErrorText(w, r, http.StatusRequestTimeout, "setup request canceled")
		return
	}
	defer releaseSetup()

	var created bool
	var alreadyInitialized bool
	var createdUserID string
	err = runSetupTransaction(r.Context(), s.db, func(tx *gorm.DB) error {
		created = false
		alreadyInitialized = false
		guard := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.SystemInitialization{
			Key:       model.SystemInitializationSetup,
			CreatedAt: time.Now().UTC(),
		})
		if guard.Error != nil {
			return guard.Error
		}
		if guard.RowsAffected == 0 {
			alreadyInitialized = true
			return nil
		}

		var count int64
		if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			alreadyInitialized = true
			return nil // 已初始化，不创建
		}

		user := model.User{
			ID:              model.NewID(),
			Username:        username,
			PasswordHash:    string(passwordHash),
			MySQLNativeHash: util.MySQLNativePasswordHash(password),
			DisplayName:     strings.TrimSpace(req.DisplayName),
			Email:           email,
			Status:          "active",
			IsSuperAdmin:    true,
		}

		if err := tx.Create(&user).Error; err != nil {
			return err
		}

		created = true
		createdUserID = user.ID
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.writeErrorText(w, r, http.StatusRequestTimeout, "setup request canceled")
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to create user: "+err.Error())
		return
	}
	if alreadyInitialized || !created {
		s.writeErrorText(w, r, http.StatusForbidden, "already initialized")
		return
	}

	// setup 创建的超级管理员身份已经与用户一起持久化到数据库。
	if s.dataDir != "" {
		// 清理旧的加密密钥标记文件，避免重置数据库后无法重新获取密钥
		os.Remove(filepath.Join(s.dataDir, ".encryption_key_shown"))
	}

	userSession, err := s.browserSessions.Create(r.Context(), createdUserID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to create browser session")
		return
	}
	setBrowserSessionCookie(w, r, userSession.Secret, userSession.ExpiresAt, s.cfg.Admin.PublicURL)
	s.writeJSON(w, r, http.StatusCreated, SetupResponse{CSRFToken: userSession.CSRFToken})
}

// handleInitEncryptionKey 返回加密密钥（一次性读取，原子标记防并发）
func (s *Server) handleInitEncryptionKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	if !isSuperAdminRequest(r) {
		s.forbidden(w, r)
		return
	}
	// 检查是否已初始化
	var count int64
	if err := s.db.Model(&model.User{}).Count(&count).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to check setup status")
		return
	}
	if count == 0 {
		s.writeErrorText(w, r, http.StatusPreconditionFailed, "setup not completed")
		return
	}

	keyPath := filepath.Join(s.dataDir, "encryption.key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to read encryption key")
		return
	}

	// 使用 O_CREATE|O_EXCL 原子创建标记文件，避免 TOCTOU 竞态
	markerPath := filepath.Join(s.dataDir, ".encryption_key_shown")
	f, err := os.OpenFile(markerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			s.writeErrorText(w, r, http.StatusForbidden, "encryption key has already been retrieved")
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to mark key as shown")
		return
	}
	defer f.Close()
	if _, err := f.Write([]byte("1")); err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to mark key as shown")
		return
	}

	s.writeJSON(w, r, http.StatusOK, EncryptionKeyResponse{
		Key: hex.EncodeToString(keyData),
	})
}
