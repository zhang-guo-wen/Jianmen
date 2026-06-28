package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"jianmen/internal/model"
	"gorm.io/gorm"
)

// InitStatusResponse 系统初始化状态
type InitStatusResponse struct {
	Initialized bool `json:"initialized"`
}

// SetupRequest 初始化设置请求
type SetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
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
	var count int64
	s.db.Model(&model.User{}).Count(&count)
	writeJSON(w, http.StatusOK, InitStatusResponse{Initialized: count > 0})
}

// handleLogin handles username+password login, returns an API token.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	username := strings.TrimSpace(req.Username)
	password := req.Password
	if username == "" || password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}

	// Find user by username
	var user model.User
	if err := s.db.Where("username = ? AND status = ?", username, "active").First(&user).Error; err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}

	// Generate new API token
	tokenBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate token"})
		return
	}
	token := hex.EncodeToString(tokenBytes)
	tokenHash := sha256.Sum256([]byte(token))
	tokenHashStr := hex.EncodeToString(tokenHash[:])

	// Store token hash
	user.TokenHash = tokenHashStr
	if err := s.db.Save(&user).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save token"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// handleInitSetup 创建超级管理员用户（事务保护 TOCTOU）
func (s *Server) handleInitSetup(w http.ResponseWriter, r *http.Request) {
	var req SetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	// bcrypt 哈希密码
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
		return
	}

	// 生成 API token
	tokenBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate token"})
		return
	}
	token := hex.EncodeToString(tokenBytes)

	// SHA-256 哈希 token 存储
	tokenHash := sha256.Sum256([]byte(token))
	tokenHashStr := hex.EncodeToString(tokenHash[:])

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
	if s.superAdminIDs != nil {
		s.superAdminIDs[createdUserID] = true
	}

	writeJSON(w, http.StatusCreated, SetupResponse{Token: token})
}

// handleInitEncryptionKey 返回加密密钥（一次性读取，原子标记防并发）
func (s *Server) handleInitEncryptionKey(w http.ResponseWriter, r *http.Request) {
	// 检查是否已初始化
	var count int64
	s.db.Model(&model.User{}).Count(&count)
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

