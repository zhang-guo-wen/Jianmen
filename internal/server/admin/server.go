package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/frontend"
	"jianmen/internal/model"
	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/rbac"
	"jianmen/internal/server/appproxy"
	"jianmen/internal/store"

	"gorm.io/gorm"
)

type Server struct {
	cfg           *config.Config
	store         store.Store
	db            *gorm.DB
	rbacChecker   *rbac.Checker
	logger        *slog.Logger
	dataDir       string
	superAdminIDs map[string]bool // 超级管理员用户 ID 集合，直接拥有全部权限
	loginLimiter  *loginLimiter
	appProxy      *appproxy.Server
}

type sessionListItem struct {
	ID              string  `json:"id"`
	User            string  `json:"user"`
	Target          string  `json:"target"`
	AccountUsername string  `json:"account_username,omitempty"`
	ClientIP        string  `json:"client_ip"`
	StartedAt       string  `json:"started_at"`
	EndedAt         string  `json:"ended_at,omitempty"`
	DurationSeconds float64 `json:"duration_seconds"`
	Protocol        string  `json:"protocol"`
	ProtocolSubtype string  `json:"protocol_subtype"`
	Path            string  `json:"path"`
	HasReplay       bool    `json:"has_replay"`
	ReplaySize      int64   `json:"replay_size"`
}

type dbConnectionListItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Protocol     string `json:"protocol"`
	ClientAddr   string `json:"client_addr"`
	UpstreamAddr string `json:"upstream_addr"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
	AccountName  string `json:"account_name,omitempty"`
	InstanceName string `json:"instance_name,omitempty"`
	AuthUser     string `json:"auth_user,omitempty"`
	Path         string `json:"path"`
}

// pageResponse 统一分页响应
type pageResponse struct {
	Items    any `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

var menuOrder = []struct {
	key    string
	action string
}{
	{"hosts", "host:view"},
	{"databases", "dbproxy:view"},
	{"platformAccounts", "platform_account:view"},
	{"users", "rbac:manage"},
	{"roles", "rbac:manage"},
	{"audit", "audit:view"},
	{"applications", "application:view"},
	{"quickConnect", "session:connect"},
}

var menuActionMap = func() map[string]string {
	m := make(map[string]string, len(menuOrder))
	for _, entry := range menuOrder {
		m[entry.key] = entry.action
	}
	return m
}()

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
}

type updateUserRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Email       *string `json:"email,omitempty"`
	Status      *string `json:"status,omitempty"`
}

func New(cfg *config.Config, store store.Store, logger *slog.Logger, dataDir string, appProxy *appproxy.Server, dbs ...*gorm.DB) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	var db *gorm.DB
	var checker *rbac.Checker
	if len(dbs) > 0 {
		db = dbs[0]
		checker = rbac.NewChecker(db)
	}
	// 收集所有超级管理员用户 ID
	superAdminIDs := LoadSuperAdminIDs(cfg, dataDir)
	return &Server{cfg: cfg, store: store, db: db, rbacChecker: checker, logger: logger, dataDir: dataDir, superAdminIDs: superAdminIDs, loginLimiter: newDefaultLoginLimiter(), appProxy: appProxy}
}

// muxHandle 注册路由并包裹 requestIDMiddleware
func (s *Server) muxHandle(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	mux.HandleFunc(pattern, requestIDMiddleware(handler))
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	frontendHandler, err := frontend.Handler()
	if err != nil || s.cfg.Admin.Dev {
		s.muxHandle(mux, "/", s.handleIndex)
	} else {
		mux.Handle("/", frontendHandler)
	}
	s.muxHandle(mux, "/api/init/status", s.handleInitStatus)
	s.muxHandle(mux, "/api/init/setup", s.handleInitSetup)
	s.muxHandle(mux, "/api/init/encryption-key", s.withAuthAndUser(s.handleInitEncryptionKey))
	s.muxHandle(mux, "/api/login", s.handleLogin)
	s.muxHandle(mux, "/api/health", s.withAuthAndUser(s.handleHealth))
	s.muxHandle(mux, "/api/users", s.withAuthAndUser(s.handleUsers))
	s.muxHandle(mux, "/api/users/", s.withAuthAndUser(s.handleUser))
	s.muxHandle(mux, "/api/hosts", s.withAuthAndUser(s.handleHosts))
	s.muxHandle(mux, "/api/hosts/", s.withAuthAndUser(s.handleHost))
	s.muxHandle(mux, "/api/targets", s.withAuthAndUser(s.handleTargets))
	s.muxHandle(mux, "/api/targets/test-connection", s.withAuthAndUser(s.handleTestConnection))
	s.muxHandle(mux, "/api/targets/", s.withAuthAndUser(s.handleTarget))
	s.muxHandle(mux, webTerminalPath, s.handleWebTerminal)
	s.muxHandle(mux, "/api/sessions", s.withAuthAndUser(s.handleSessions))
	s.muxHandle(mux, "/api/sessions/", s.withAuthAndUser(s.handleSessionArtifact))
	s.muxHandle(mux, "/api/user-sessions", s.withAuthAndUser(s.handleUserSessions))
	s.muxHandle(mux, "/api/db/gateway", s.withAuthAndUser(s.handleDBGateway))
	s.muxHandle(mux, "/api/db/instances", s.withAuthAndUser(s.handleDBInstances))
	s.muxHandle(mux, "/api/db/instances/", s.withAuthAndUser(s.handleDBInstance))
	s.muxHandle(mux, "/api/db/accounts/test", s.withAuthAndUser(s.handleTestDBConnection))
	s.muxHandle(mux, "/api/db/accounts/test/", s.withAuthAndUser(s.handleTestDBConnection))
	s.muxHandle(mux, "/api/db/accounts/", s.withAuthAndUser(s.handleDBAccount))
	s.muxHandle(mux, "/api/db/connections", s.withAuthAndUser(s.handleDBConnections))
	s.muxHandle(mux, "/api/db/connections/", s.withAuthAndUser(s.handleDBConnectionArtifact))
	s.muxHandle(mux, "/api/rbac/roles", s.withAuthAndUser(s.handleRBACRoles))
	s.muxHandle(mux, "/api/rbac/roles/", s.withAuthAndUser(s.handleRBACRole))
	s.muxHandle(mux, "/api/rbac/permissions", s.withAuthAndUser(s.handleRBACPermissions))
	s.muxHandle(mux, "/api/rbac/permissions/", s.withAuthAndUser(s.handleRBACPermission))
	s.muxHandle(mux, "/api/rbac/user-roles", s.withAuthAndUser(s.handleRBACUserRoles))
	s.muxHandle(mux, "/api/rbac/user-roles/", s.withAuthAndUser(s.handleRBACUserRole))
	s.muxHandle(mux, "/api/rbac/role-permissions", s.withAuthAndUser(s.handleRBACRolePermissions))
	s.muxHandle(mux, "/api/rbac/role-permissions/", s.withAuthAndUser(s.handleRBACRolePermission))
	s.muxHandle(mux, "/api/rbac/effective", s.withAuthAndUser(s.handleRBACEffective))
	// 新版审计 API（替代旧的 sessions / db/connections）
	s.muxHandle(mux, "/api/audit/ssh", s.withAuthAndUser(s.handleAuditSSH))
	s.muxHandle(mux, "/api/audit/db", s.withAuthAndUser(s.handleAuditDB))
	s.muxHandle(mux, "/api/audit/", s.withAuthAndUser(s.handleAuditArtifact))
	s.muxHandle(mux, "/api/me", s.withAuthAndUser(s.handleMe))
	s.muxHandle(mux, "/api/me/permissions", s.withAuthAndUser(s.handleMePermissions))
	s.muxHandle(mux, "/api/applications", s.withAuthAndUser(s.handleApplications))
	s.muxHandle(mux, "/api/applications/", s.withAuthAndUser(s.handleApplication))
	s.muxHandle(mux, "/api/platform-accounts", s.withAuthAndUser(s.handlePlatformAccounts))
	s.muxHandle(mux, "/api/platform-accounts/", s.withAuthAndUser(s.handlePlatformAccount))
	s.muxHandle(mux, "/api/me/menus", s.withAuthAndUser(s.handleMeMenus))

	server := &http.Server{
		Addr:              s.cfg.Admin.ListenAddr,
		Handler:           logRequests(s.logger, withCORS(s.cfg.Admin.CORSAllowedOrigins, mux)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	s.logger.Info("admin server listening", "addr", s.cfg.Admin.ListenAddr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"status":   "api-only",
		"message":  "legacy HTML admin console is disabled; use the API or Vue frontend",
		"frontend": "http://127.0.0.1:47101",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusNotFound, "user not found")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"user_id":  userID,
		"username": usernameFromRequest(r),
	})
}

func (s *Server) handleMePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusNotFound, "user not found")
		return
	}
	// 超级管理员拥有全部权限
	if s.isSuperAdmin(userID) {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"actions": []string{"*"}})
		return
	}
	if s.db == nil || s.rbacChecker == nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"actions": []string{}})
		return
	}
	var rawPerms []model.Permission
	if err := s.db.Table("permissions").
		Select("permissions.action").
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("user_roles.user_id = ?", userID).
		Where("user_roles.expires_at IS NULL OR user_roles.expires_at > ?", time.Now().UTC()).
		Where("roles.status = '' OR roles.status = ?", "active").
		Where("permissions.effect = '' OR permissions.effect = ?", model.PermissionEffectAllow).
		Where("permissions.action != ''").
		Group("permissions.action").
		Find(&rawPerms).Error; err != nil {
		s.writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil)
		return
	}
	actions := make([]string, 0, len(rawPerms))
	seen := make(map[string]struct{}, len(rawPerms))
	for _, p := range rawPerms {
		if _, ok := seen[p.Action]; !ok && p.Action != "" {
			seen[p.Action] = struct{}{}
			actions = append(actions, p.Action)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"actions": actions})
}

func (s *Server) handleMeMenus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusNotFound, "user not found")
		return
	}
	// 超级管理员看到全部菜单
	if s.isSuperAdmin(userID) {
		all := make([]string, 0, len(menuOrder))
		for _, entry := range menuOrder {
			all = append(all, entry.key)
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"menus": all})
		return
	}
	if s.db == nil || s.rbacChecker == nil {
		// No metadata DB: return all menus in deterministic order
		all := make([]string, 0, len(menuOrder))
		for _, entry := range menuOrder {
			all = append(all, entry.key)
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"menus": all})
		return
	}
	var rawPerms []model.Permission
	if err := s.db.Table("permissions").
		Select("permissions.action").
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("user_roles.user_id = ?", userID).
		Where("user_roles.expires_at IS NULL OR user_roles.expires_at > ?", time.Now().UTC()).
		Where("roles.status = '' OR roles.status = ?", "active").
		Where("permissions.effect = '' OR permissions.effect = ?", model.PermissionEffectAllow).
		Where("permissions.action != ''").
		Group("permissions.action").
		Find(&rawPerms).Error; err != nil {
		s.writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil)
		return
	}
	actionSet := make(map[string]struct{}, len(rawPerms))
	for _, p := range rawPerms {
		if p.Action != "" {
			actionSet[p.Action] = struct{}{}
		}
	}
	// If the user has a wildcard action, return all menus
	if _, hasWildcard := actionSet["*"]; hasWildcard {
		all := make([]string, 0, len(menuOrder))
		for _, entry := range menuOrder {
			all = append(all, entry.key)
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"menus": all})
		return
	}
	// Build menu list from actions in deterministic order
	seen := make(map[string]struct{})
	menus := make([]string, 0, len(menuOrder))
	for _, entry := range menuOrder {
		if _, ok := actionSet[entry.action]; ok {
			if _, exists := seen[entry.key]; !exists {
				seen[entry.key] = struct{}{}
				menus = append(menus, entry.key)
			}
		}
	}
	// Always include dashboard
	if _, ok := seen["dashboard"]; !ok {
		menus = append([]string{"dashboard"}, menus...)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"menus": menus})
}
