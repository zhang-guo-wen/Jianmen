package admin

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	"jianmen/internal/model"
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
	Token string `json:"token"`
}

// EncryptionKeyResponse 加密密钥响应
type EncryptionKeyResponse struct {
	Key string `json:"key"`
}

// handleInitStatus 返回系统初始化状态（检查是否已有用户）
func (s *Server) handleInitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	var count int64
	if err := s.db.Model(&model.User{}).Count(&count).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check setup status"})
		return
	}
	writeJSON(w, http.StatusOK, InitStatusResponse{Initialized: count > 0})
}

// handleLogin handles username+password login, returns an API token.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	username := strings.TrimSpace(req.Username)
	password := req.Password
	if username == "" || password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	now := time.Now().UTC()
	limiter := s.loginLimiterForRequest()
	limitKey := loginLimitKey(r, username)
	if retryAfter := limiter.retryAfter(limitKey, now); retryAfter > 0 {
		setRetryAfter(w, retryAfter)
		s.logLogin(r, username, "blocked", "rate_limited")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many failed login attempts; try again later"})
		return
	}

	// Find user by username
	var user model.User
	if err := s.db.Where("username = ? AND status = ?", username, "active").First(&user).Error; err != nil {
		limiter.recordFailure(limitKey, now)
		s.logLogin(r, username, "failure", "invalid_credentials")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}

	if !verifyPassword(user.PasswordHash, password) {
		limiter.recordFailure(limitKey, now)
		s.logLogin(r, username, "failure", "invalid_credentials")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}

	token, tokenHashStr, err := newAPIToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate token"})
		return
	}

	// Store token hash and update last login time
	user.TokenHash = tokenHashStr
	user.LastLoginAt = &now
	if err := s.db.Save(&user).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save token"})
		return
	}

	limiter.reset(limitKey)
	s.logLogin(r, username, "success", "")
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// handleInitSetup 创建超级管理员用户（事务保护 TOCTOU）
func (s *Server) handleInitSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req SetupRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	email := strings.TrimSpace(req.Email)

	if username == "" || password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if len(password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		return
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
		return
	}

	token, tokenHashStr, err := newAPIToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate token"})
		return
	}

	var createdUserID string
	var created bool
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil // 已初始化，不创建
		}

		user := model.User{
			ID:           model.NewID(),
			Username:     username,
			PasswordHash: string(passwordHash),
			DisplayName:  strings.TrimSpace(req.DisplayName),
			TokenHash:    tokenHashStr,
			Email:        email,
			Status:       "active",
		}

		if err := tx.Create(&user).Error; err != nil {
			return err
		}

		createdUserID = user.ID
		created = true
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user: " + err.Error()})
		return
	}
	if !created {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "already initialized"})
		return
	}

	// setup 向导创建的用户即为超级管理员，直接拥有全部权限
	// 同时持久化到文件，确保重启后仍然有效
	if s.superAdminIDs != nil {
		s.superAdminIDs[createdUserID] = true
	}
	if s.dataDir != "" {
		saveSuperAdminID(s.dataDir, createdUserID)
		// 清理旧的加密密钥标记文件，避免重置数据库后无法重新获取密钥
		os.Remove(filepath.Join(s.dataDir, ".encryption_key_shown"))
	}

	writeJSON(w, http.StatusCreated, SetupResponse{Token: token})
}

// handleInitEncryptionKey 返回加密密钥（一次性读取，原子标记防并发）
func (s *Server) handleInitEncryptionKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	if !s.isSuperAdmin(userIDFromRequest(r)) {
		s.forbidden(w)
		return
	}
	// 检查是否已初始化
	var count int64
	if err := s.db.Model(&model.User{}).Count(&count).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check setup status"})
		return
	}
	if count == 0 {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "setup not completed"})
		return
	}

	keyPath := filepath.Join(s.dataDir, "encryption.key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read encryption key"})
		return
	}

	// 使用 O_CREATE|O_EXCL 原子创建标记文件，避免 TOCTOU 竞态
	markerPath := filepath.Join(s.dataDir, ".encryption_key_shown")
	f, err := os.OpenFile(markerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "encryption key has already been retrieved"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to mark key as shown"})
		return
	}
	defer f.Close()
	if _, err := f.Write([]byte("1")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to mark key as shown"})
		return
	}

	writeJSON(w, http.StatusOK, EncryptionKeyResponse{
		Key: hex.EncodeToString(keyData),
	})
}
