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

// handleInitSetup 创建超级管理员用户
func (s *Server) handleInitSetup(w http.ResponseWriter, r *http.Request) {
	// 检查是否已初始化
	var count int64
	s.db.Model(&model.User{}).Count(&count)
	if count > 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "already initialized"})
		return
	}

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

	user := model.User{
		ID:           model.NewID(),
		Username:     username,
		PasswordHash: string(passwordHash),
		TokenHash:    tokenHashStr,
		Email:        email,
		Status:       "active",
	}

	if err := s.db.Create(&user).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user: " + err.Error()})
		return
	}

	// 分配 admin 角色（如果内置角色已存在）
	assignAdminRole(s.db, user.ID)

	writeJSON(w, http.StatusCreated, SetupResponse{Token: token})
}

// handleInitEncryptionKey 返回加密密钥（一次性读取）
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

	// 标记文件控制一次性显示
	markerPath := filepath.Join(s.dataDir, ".encryption_key_shown")
	if _, err := os.Stat(markerPath); err == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "encryption key has already been retrieved"})
		return
	}

	// 写入标记文件
	if err := os.WriteFile(markerPath, []byte("1"), 0600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to mark key as shown"})
		return
	}

	writeJSON(w, http.StatusOK, EncryptionKeyResponse{
		Key: hex.EncodeToString(keyData),
	})
}

// assignAdminRole 为新创建的管理员用户分配 builtin-admin 角色
func assignAdminRole(db *gorm.DB, userID string) {
	var adminRole model.Role
	if err := db.Where("name = ?", "builtin-admin").First(&adminRole).Error; err != nil {
		return // 角色不存在就跳过
	}
	userRole := model.UserRole{
		ID:     model.NewID(),
		UserID: userID,
		RoleID: adminRole.ID,
	}
	db.Create(&userRole)
}
