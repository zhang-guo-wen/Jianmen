package admin

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/service"
)

func (s *Server) withAuthAndUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if token == "" || token == auth {
			s.writeErrorText(w, r, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		if s.adminAuth == nil {
			s.writeErrorText(w, r, http.StatusServiceUnavailable, "authentication service unavailable")
			return
		}

		user, err := s.adminAuth.Authenticate(r.Context(), hashToken(token), time.Now().UTC(), s.isSuperAdmin)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrAdminUserExpired):
				s.writeErrorText(w, r, http.StatusUnauthorized, "user account expired")
			case errors.Is(err, service.ErrInvalidAdminToken):
				s.writeErrorText(w, r, http.StatusUnauthorized, "invalid token")
			default:
				s.logger.Error("admin authentication failed", "error", err)
				s.writeErrorText(w, r, http.StatusInternalServerError, "authentication failed")
			}
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyUserID, user.ID)
		ctx = context.WithValue(ctx, ctxKeyUsername, user.Username)
		authenticatedRequest := r.WithContext(ctx)
		if isAuditableMutation(authenticatedRequest) {
			aw := &auditResponseWriter{ResponseWriter: w}
			next(aw, authenticatedRequest)
			s.recordOperation(authenticatedRequest, aw.statusCode())
			return
		}
		next(w, authenticatedRequest)
	}
}

func userIDFromRequest(r *http.Request) string {
	if id, ok := r.Context().Value(ctxKeyUserID).(string); ok {
		return id
	}
	return ""
}

func usernameFromRequest(r *http.Request) string {
	if name, ok := r.Context().Value(ctxKeyUsername).(string); ok {
		return name
	}
	return ""
}

func (s *Server) requirePermission(r *http.Request, action string) bool {
	userID := userIDFromRequest(r)
	if userID == "" {
		s.logger.Warn("permission denied: missing authenticated user", "action", action)
		return false
	}
	// 超级管理员拥有全部权限，无需 RBAC 检查
	if s.isSuperAdmin(userID) {
		return true
	}
	if s.rbacChecker == nil {
		s.logger.Warn("permission denied: rbac checker unavailable", "user_id", userID, "action", action)
		return false
	}
	allowed, err := s.rbacChecker.HasPermission(userID, action, "", "")
	if err != nil {
		s.logger.Warn("rbac check error", "user_id", userID, "action", action, "error", err)
		return false
	}
	return allowed
}

func (s *Server) requireAnyPermission(r *http.Request, actions ...string) bool {
	for _, action := range actions {
		if s.requirePermission(r, action) {
			return true
		}
	}
	return false
}

func (s *Server) withAnyPermission(actions []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAnyPermission(r, actions...) {
			s.forbidden(w, r)
			return
		}
		next(w, r)
	}
}

func (s *Server) withPermission(action string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requirePermission(r, action) {
			s.forbidden(w, r)
			return
		}
		next(w, r)
	}
}

// isSuperAdmin 判断用户是否为超级管理员（配置文件中定义的用户 或 setup 向导创建的管理员）。
func (s *Server) isSuperAdmin(userID string) bool {
	if s.superAdminIDs == nil {
		return false
	}
	return s.superAdminIDs[userID]
}

const SuperAdminIDsFile = ".super_admin_ids"

// LoadSuperAdminIDs 收集所有超级管理员 ID：配置文件中的用户 + 持久化文件中的用户。
func LoadSuperAdminIDs(cfg *config.Config, dataDir string) map[string]bool {
	ids := make(map[string]bool)
	for _, u := range cfg.Users {
		id := strings.TrimSpace(u.ID)
		if id == "" {
			id = strings.TrimSpace(u.Username)
		}
		if id != "" {
			ids[id] = true
		}
	}
	if dataDir != "" {
		loadSuperAdminIDsFromFile(dataDir, ids)
	}
	return ids
}

// loadSuperAdminIDsFromFile 从持久化文件读取 setup 向导创建的管理员 ID。
func loadSuperAdminIDsFromFile(dataDir string, ids map[string]bool) {
	path := filepath.Join(dataDir, SuperAdminIDsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		id := strings.TrimSpace(line)
		if id != "" {
			ids[id] = true
		}
	}
}

// saveSuperAdminID 将 setup 向导创建的管理员 ID 持久化到文件。
func saveSuperAdminID(dataDir, userID string) {
	path := filepath.Join(dataDir, SuperAdminIDsFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(userID + "\n")
}
