package admin

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	"jianmen/internal/access"
	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

type Server struct {
	cfg         *config.Config
	store       store.Store
	db          *gorm.DB
	rbacChecker *rbac.Checker
	logger      *slog.Logger
	dataDir     string
}

type sessionListItem struct {
	ID              string  `json:"id"`
	User            string  `json:"user"`
	Target          string  `json:"target"`
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
	Path         string `json:"path"`
}

type pagedHostList struct {
	Data     []store.HostView `json:"data"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int               `json:"total"`
}

var menuOrder = []struct {
	key    string
	action string
}{
	{"dashboard", "dashboard:view"},
	{"hosts", "host:view"},
	{"databases", "db:view"},
	{"quickConnect", "session:connect"},
	{"sessions", "session:view"},
	{"rbac", "rbac:manage"},
	{"audit", "audit:view"},
	{"webTerminal", "session:connect"},
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

func New(cfg *config.Config, store store.Store, logger *slog.Logger, dataDir string, dbs ...*gorm.DB) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	var db *gorm.DB
	var checker *rbac.Checker
	if len(dbs) > 0 {
		db = dbs[0]
		checker = rbac.NewChecker(db)
	}
	return &Server{cfg: cfg, store: store, db: db, rbacChecker: checker, logger: logger, dataDir: dataDir}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/init/status", s.handleInitStatus)
	mux.HandleFunc("/api/init/setup", s.handleInitSetup)
	mux.HandleFunc("/api/init/encryption-key", s.handleInitEncryptionKey)
	mux.HandleFunc("/api/health", s.withAuthAndUser(s.handleHealth))
	mux.HandleFunc("/api/users", s.withAuthAndUser(s.handleUsers))
	mux.HandleFunc("/api/users/", s.withAuthAndUser(s.handleUser))
	mux.HandleFunc("/api/hosts", s.withAuthAndUser(s.handleHosts))
	mux.HandleFunc("/api/hosts/", s.withAuthAndUser(s.handleHost))
	mux.HandleFunc("/api/targets", s.withAuthAndUser(s.handleTargets))
	mux.HandleFunc("/api/targets/test-connection", s.withAuthAndUser(s.handleTestConnection))
	mux.HandleFunc("/api/targets/", s.withAuthAndUser(s.handleTarget))
	mux.HandleFunc(webTerminalPath, s.handleWebTerminal)
	mux.HandleFunc("/api/sessions", s.withAuthAndUser(s.handleSessions))
	mux.HandleFunc("/api/sessions/", s.withAuthAndUser(s.handleSessionArtifact))
	mux.HandleFunc("/api/db/instances", s.withAuthAndUser(s.handleDBInstances))
	mux.HandleFunc("/api/db/instances/", s.withAuthAndUser(s.handleDBInstance))
	mux.HandleFunc("/api/db/accounts/test/", s.withAuthAndUser(s.handleTestDBConnection))
	mux.HandleFunc("/api/db/accounts/", s.withAuthAndUser(s.handleDBAccount))
	mux.HandleFunc("/api/db/connections", s.withAuthAndUser(s.handleDBConnections))
	mux.HandleFunc("/api/db/connections/", s.withAuthAndUser(s.handleDBConnectionArtifact))
	mux.HandleFunc("/api/rbac/roles", s.withAuthAndUser(s.handleRBACRoles))
	mux.HandleFunc("/api/rbac/roles/", s.withAuthAndUser(s.handleRBACRole))
	mux.HandleFunc("/api/rbac/permissions", s.withAuthAndUser(s.handleRBACPermissions))
	mux.HandleFunc("/api/rbac/permissions/", s.withAuthAndUser(s.handleRBACPermission))
	mux.HandleFunc("/api/rbac/user-roles", s.withAuthAndUser(s.handleRBACUserRoles))
	mux.HandleFunc("/api/rbac/user-roles/", s.withAuthAndUser(s.handleRBACUserRole))
	mux.HandleFunc("/api/rbac/role-permissions", s.withAuthAndUser(s.handleRBACRolePermissions))
	mux.HandleFunc("/api/rbac/role-permissions/", s.withAuthAndUser(s.handleRBACRolePermission))
	mux.HandleFunc("/api/rbac/effective", s.withAuthAndUser(s.handleRBACEffective))
	mux.HandleFunc("/api/me", s.withAuthAndUser(s.handleMe))
	mux.HandleFunc("/api/me/permissions", s.withAuthAndUser(s.handleMePermissions))
	mux.HandleFunc("/api/me/menus", s.withAuthAndUser(s.handleMeMenus))

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
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "api-only",
		"message":  "legacy HTML admin console is disabled; use the API or Vue frontend",
		"frontend": "http://127.0.0.1:47101",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		writeErrorText(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"user_id":  userID,
		"username": usernameFromRequest(r),
	})
}

func (s *Server) handleMePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		writeErrorText(w, http.StatusNotFound, "user not found")
		return
	}
	if s.db == nil || s.rbacChecker == nil {
		writeJSON(w, http.StatusOK, map[string]any{"actions": []string{}})
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
		writeError(w, http.StatusInternalServerError, err)
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
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

func (s *Server) handleMeMenus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		writeErrorText(w, http.StatusNotFound, "user not found")
		return
	}
	if s.db == nil || s.rbacChecker == nil {
		// No metadata DB: return all menus in deterministic order
		all := make([]string, 0, len(menuOrder))
		for _, entry := range menuOrder {
			all = append(all, entry.key)
		}
		writeJSON(w, http.StatusOK, map[string]any{"menus": all})
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
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	actionSet := make(map[string]struct{}, len(rawPerms))
	for _, p := range rawPerms {
		if p.Action != "" {
			actionSet[p.Action] = struct{}{}
		}
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
	writeJSON(w, http.StatusOK, map[string]any{"menus": menus})
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listUsers(w, r)
	case http.MethodPost:
		s.createUser(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w)
		return
	}
	id, ok := userIDFromPath(r.URL.Path)
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getUser(w, id)
	case http.MethodPut:
		s.updateUser(w, r, id)
	case http.MethodDelete:
		s.deleteUser(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listUsers(w http.ResponseWriter, _ *http.Request) {
	if s.db != nil {
		var users []model.User
		if err := s.db.Order("created_at DESC").Find(&users).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, users)
		return
	}
	// Fallback to store-based listing
	writeJSON(w, http.StatusOK, s.store.Users())
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	if username == "" {
		writeErrorText(w, http.StatusBadRequest, "username is required")
		return
	}
	if password == "" {
		writeErrorText(w, http.StatusBadRequest, "password is required")
		return
	}
	// Hash password
	pwHash := sha256.Sum256([]byte(password))
	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	rawToken := hex.EncodeToString(tokenBytes)
	tokenHash := sha256.Sum256([]byte(rawToken))

	user := model.User{
		ID:           model.NewID(),
		Username:     username,
		PasswordHash: hex.EncodeToString(pwHash[:]),
		TokenHash:    hex.EncodeToString(tokenHash[:]),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Email:        strings.TrimSpace(req.Email),
		Status:       "active",
	}
	if err := s.db.Create(&user).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":  user,
		"token": rawToken,
	})
}

func (s *Server) getUser(w http.ResponseWriter, id string) {
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	var user model.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	var user model.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Email != nil {
		user.Email = strings.TrimSpace(*req.Email)
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if status != "active" && status != "disabled" {
			writeErrorText(w, http.StatusBadRequest, "status must be active or disabled")
			return
		}
		user.Status = status
	}
	if err := s.db.Save(&user).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	currentUserID := userIDFromRequest(r)
	if currentUserID != "" && currentUserID == id {
		writeErrorText(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	var user model.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	// Cascade delete user_roles
	if err := s.db.Where("user_id = ?", id).Delete(&model.UserRole{}).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.db.Delete(&user).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionHostView) {
			s.forbidden(w)
			return
		}
		writeJSON(w, http.StatusOK, paginateHosts(s.store.Hosts(), r))
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionHostCreate) {
			s.forbidden(w)
			return
		}
		s.handleCreateHost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var host store.HostRecord
	if err := json.NewDecoder(r.Body).Decode(&host); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.AddHost(host)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleHost(w http.ResponseWriter, r *http.Request) {
	id, child, ok := hostPathParts(r.URL.Path)
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	if child == "accounts" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		accounts, err := s.store.HostAccounts(id)
		if err != nil {
			writeHostStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, accounts)
		return
	}
	if child != "" {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionHostView) {
			s.forbidden(w)
			return
		}
		view, err := s.store.Host(id)
		if err != nil {
			writeHostStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		s.handleUpdateHost(w, r, id)
	case http.MethodDelete:
		if err := s.store.DeleteHost(id); err != nil {
			writeHostStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var host store.HostRecord
	if err := json.NewDecoder(r.Body).Decode(&host); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.UpdateHost(id, host)
	if err != nil {
		writeHostStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionTargetView) {
			s.forbidden(w)
			return
		}
		writeJSON(w, http.StatusOK, s.store.Targets())
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionTargetCreate) {
			s.forbidden(w)
			return
		}
		s.handleCreateTarget(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var target config.Target
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.AddTarget(target)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionTargetCreate) {
		s.forbidden(w)
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var target config.Target
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	addr := target.Addr()
	if addr == "" || target.Username == "" {
		writeErrorText(w, http.StatusBadRequest, "host, port, and username are required")
		return
	}

	// 测试连接默认允许跳过主机密钥验证（除非用户明确配置了指纹或 known_hosts）
	if !target.InsecureIgnoreHostKey && target.HostKeyFingerprint == "" && target.KnownHostsPath == "" {
		target.InsecureIgnoreHostKey = true
	}

	clientConfig, err := access.ClientConfigForTarget(target)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "配置错误: " + err.Error()})
		return
	}

	clientConfig.Timeout = 10 * time.Second

	conn, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "连接失败: " + friendlySSHError(err)})
		return
	}
	conn.Close()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "连接成功 (" + addr + ")"})
}

func (s *Server) handleTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := targetIDFromPath(r.URL.Path)
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionTargetView) {
			s.forbidden(w)
			return
		}
		view, err := s.store.Target(id)
		if err != nil {
			writeTargetStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		s.handleUpdateTarget(w, r, id)
	case http.MethodDelete:
		if err := s.store.DeleteTarget(id); err != nil {
			writeTargetStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateTarget(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var target config.Target
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.UpdateTarget(id, target)
	if err != nil {
		writeTargetStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// -- db instances --

func (s *Server) handleDBInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionDBProxyView) {
			s.forbidden(w)
			return
		}
		writeJSON(w, http.StatusOK, s.store.DatabaseInstances())
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionDBProxyCreate) {
			s.forbidden(w)
			return
		}
		s.handleCreateDBInstance(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateDBInstance(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload struct {
		Name      string `json:"name"`
		Protocol  string `json:"protocol"`
		Address   string `json:"address"`
		GroupName string `json:"group_name"`
		Remark    string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.AddDatabaseInstance(payload.Name, payload.Protocol, payload.Address, payload.GroupName, payload.Remark)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleDBInstance(w http.ResponseWriter, r *http.Request) {
	id, child, ok := dbInstancePathParts(r.URL.Path)
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	if child == "accounts" {
		switch r.Method {
		case http.MethodGet:
			accounts, err := s.store.InstanceAccounts(id)
			if err != nil {
				writeDBStoreError(w, err)
				return
			}

			page := positiveIntRequestQuery(r, "page", 1)
			size := positiveIntRequestQuery(r, "size", 20)
			if size > 100 {
				size = 100
			}
			search := strings.TrimSpace(r.URL.Query().Get("search"))

			filtered := accounts
			if search != "" {
				searchLower := strings.ToLower(search)
				matched := make([]store.DatabaseAccountView, 0, len(accounts))
				for _, acc := range accounts {
					if strings.Contains(strings.ToLower(acc.UniqueName), searchLower) ||
						strings.Contains(strings.ToLower(acc.UpstreamUsername), searchLower) ||
						strings.Contains(strings.ToLower(acc.GroupName), searchLower) {
						matched = append(matched, acc)
					}
				}
				filtered = matched
			}

			total := len(filtered)
			start := (page - 1) * size
			if start > total {
				start = total
			}
			end := start + size
			if end > total {
				end = total
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"items": filtered[start:end],
				"total": total,
			})
		case http.MethodPost:
			s.handleCreateDBAccount(w, r, id)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if child != "" {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionDBProxyView) {
			s.forbidden(w)
			return
		}
		view, err := s.store.DatabaseInstance(id)
		if err != nil {
			writeDBStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		s.handleUpdateDBInstance(w, r, id)
	case http.MethodDelete:
		if err := s.store.DeleteDatabaseInstance(id); err != nil {
			writeDBStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateDBInstance(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload struct {
		Name      string `json:"name"`
		Protocol  string `json:"protocol"`
		Address   string `json:"address"`
		GroupName string `json:"group_name"`
		Remark    string `json:"remark"`
		Disabled  bool   `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.UpdateDatabaseInstance(id, payload.Name, payload.Protocol, payload.Address, payload.GroupName, payload.Remark, payload.Disabled)
	if err != nil {
		writeDBStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleCreateDBAccount(w http.ResponseWriter, r *http.Request, instanceID string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload struct {
		UpstreamUsername string     `json:"upstream_username"`
		UpstreamPassword string     `json:"upstream_password"`
		GroupName        string     `json:"group_name"`
		Remark           string     `json:"remark"`
		ExpiresAt        *time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.AddDatabaseAccount(instanceID, payload.UpstreamUsername, payload.UpstreamPassword, payload.GroupName, payload.Remark, payload.ExpiresAt)
	if err != nil {
		writeDBStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

// -- db accounts (single-account CRUD) --

func (s *Server) handleDBAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := dbAccountIDFromPath(r.URL.Path)
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionDBProxyView) {
			s.forbidden(w)
			return
		}
		view, err := s.store.DatabaseAccount(id)
		if err != nil {
			writeDBStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		s.handleUpdateDBAccount(w, r, id)
	case http.MethodDelete:
		if err := s.store.DeleteDatabaseAccount(id); err != nil {
			writeDBStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateDBAccount(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload struct {
		UpstreamUsername string     `json:"upstream_username"`
		UpstreamPassword string     `json:"upstream_password"`
		GroupName        string     `json:"group_name"`
		Remark           string     `json:"remark"`
		ExpiresAt        *time.Time `json:"expires_at"`
		Disabled         bool       `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.UpdateDatabaseAccount(id, payload.UpstreamUsername, payload.UpstreamPassword, payload.GroupName, payload.Remark, payload.ExpiresAt, payload.Disabled)
	if err != nil {
		writeDBStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionSessionView) {
		s.forbidden(w)
		return
	}
	sessions, err := s.listSessions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleSessionArtifact(w http.ResponseWriter, r *http.Request) {
	id, artifact, ok := splitArtifactPath(strings.TrimPrefix(r.URL.Path, "/api/sessions/"))
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	dir, ok := safeReplayDir(filepath.Join(s.cfg.ReplayDir, "ssh"), id)
	if !ok {
		writeErrorText(w, http.StatusBadRequest, "invalid session id")
		return
	}
	switch artifact {
	case "meta":
		writeJSONFile(w, filepath.Join(dir, "meta.json"))
	case "commands":
		writeJSONLines(w, filepath.Join(dir, "commands.jsonl"), 500)
	case "files":
		writeJSONLines(w, filepath.Join(dir, "files.jsonl"), 1000)
	case "file-summary":
		writeJSONFile(w, filepath.Join(dir, "files-summary.json"))
	case "replay":
		writeTextFile(w, filepath.Join(dir, "terminal.cast"), "application/x-asciicast; charset=utf-8")
	default:
		writeErrorText(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleDBConnections(w http.ResponseWriter, _ *http.Request) {
	connections, err := s.listDBConnections()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, connections)
}

func (s *Server) handleDBConnectionArtifact(w http.ResponseWriter, r *http.Request) {
	id, artifact, ok := splitArtifactPath(strings.TrimPrefix(r.URL.Path, "/api/db/connections/"))
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	dir, ok := safeReplayDir(filepath.Join(s.cfg.ReplayDir, "db"), id)
	if !ok {
		writeErrorText(w, http.StatusBadRequest, "invalid connection id")
		return
	}
	switch artifact {
	case "meta":
		writeJSONFile(w, filepath.Join(dir, "meta.json"))
	case "queries":
		writeJSONLines(w, filepath.Join(dir, "queries.jsonl"), 1000)
	default:
		writeErrorText(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) listSessions() ([]sessionListItem, error) {
	root := filepath.Join(s.cfg.ReplayDir, "ssh")
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []sessionListItem{}, nil
		}
		return nil, err
	}
	out := make([]sessionListItem, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		var meta struct {
			SessionID       string `json:"session_id"`
			User            string `json:"user"`
			Target          string `json:"target"`
			ClientIP        string `json:"client_ip"`
			StartedAt       string `json:"started_at"`
			EndedAt         string `json:"ended_at"`
			Protocol        string `json:"protocol"`
			ProtocolSubtype string `json:"protocol_subtype"`
		}
		if err := readJSON(filepath.Join(dir, "meta.json"), &meta); err != nil {
			continue
		}
		replaySize := int64(0)
		if info, err := os.Stat(filepath.Join(dir, "terminal.cast")); err == nil {
			replaySize = info.Size()
		}

		duration := calcSessionDuration(meta.StartedAt, meta.EndedAt, filepath.Join(dir, "terminal.cast"))

		out = append(out, sessionListItem{
			ID:              firstNonEmpty(meta.SessionID, entry.Name()),
			User:            meta.User,
			Target:          meta.Target,
			ClientIP:        meta.ClientIP,
			StartedAt:       meta.StartedAt,
			EndedAt:         meta.EndedAt,
			DurationSeconds: duration,
			Protocol:        meta.Protocol,
			ProtocolSubtype: meta.ProtocolSubtype,
			Path:            dir,
			HasReplay:       replaySize > 0,
			ReplaySize:      replaySize,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt > out[j].StartedAt
	})
	return out, nil
}

// calcSessionDuration computes session duration in seconds.
// Prefers ended_at from meta.json; falls back to the last frame timestamp
// in the cast file.
func calcSessionDuration(startedAt, endedAt, castPath string) float64 {
	if startedAt != "" && endedAt != "" {
		start, err1 := time.Parse(time.RFC3339Nano, startedAt)
		end, err2 := time.Parse(time.RFC3339Nano, endedAt)
		if err1 == nil && err2 == nil {
			return end.Sub(start).Seconds()
		}
	}
	// Fallback: read the last frame from the cast file.
	f, err := os.Open(castPath)
	if err != nil {
		return 0
	}
	defer f.Close()
	// Read last non-empty line
	var lastLine string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && line[0] == '[' {
			lastLine = line
		}
	}
	// Parse [time, stream, data]
	if lastLine != "" {
		var frame []any
		if json.Unmarshal([]byte(lastLine), &frame) == nil && len(frame) > 0 {
			if t, ok := frame[0].(float64); ok {
				return t
			}
		}
	}
	return 0
}

func (s *Server) listDBConnections() ([]dbConnectionListItem, error) {
	root := filepath.Join(s.cfg.ReplayDir, "db")
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []dbConnectionListItem{}, nil
		}
		return nil, err
	}
	out := make([]dbConnectionListItem, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		var meta struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Protocol     string `json:"protocol"`
			ClientAddr   string `json:"client_addr"`
			UpstreamAddr string `json:"upstream_addr"`
			StartedAt    string `json:"started_at"`
		}
		if err := readJSON(filepath.Join(dir, "meta.json"), &meta); err != nil {
			continue
		}
		out = append(out, dbConnectionListItem{
			ID:           firstNonEmpty(meta.ID, entry.Name()),
			Name:         meta.Name,
			Protocol:     meta.Protocol,
			ClientAddr:   meta.ClientAddr,
			UpstreamAddr: meta.UpstreamAddr,
			StartedAt:    meta.StartedAt,
			Path:         dir,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt > out[j].StartedAt
	})
	return out, nil
}

type contextKey string

const (
	ctxKeyUserID   contextKey = "admin_user_id"
	ctxKeyUsername contextKey = "admin_username"
)

func (s *Server) withAuthAndUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if token == "" || token == auth {
			writeErrorText(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}

		// Try per-user token lookup first
		if s.db != nil {
			hash := sha256.Sum256([]byte(token))
			hashStr := hex.EncodeToString(hash[:])
			var user model.User
			if err := s.db.Where("token_hash = ?", hashStr).First(&user).Error; err == nil {
				ctx := context.WithValue(r.Context(), ctxKeyUserID, user.ID)
				ctx = context.WithValue(ctx, ctxKeyUsername, user.Username)
				next(w, r.WithContext(ctx))
				return
			}
		}

		// Fallback to shared admin token
		if s.cfg.Admin.Token != "" && token == s.cfg.Admin.Token {
			next(w, r)
			return
		}

		writeErrorText(w, http.StatusUnauthorized, "invalid token")
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
		// Shared token fallback — no per-user RBAC, allow all
		return true
	}
	if s.rbacChecker == nil {
		return true
	}
	allowed, err := s.rbacChecker.HasPermission(userID, action, "", "")
	if err != nil {
		s.logger.Warn("rbac check error", "user_id", userID, "action", action, "error", err)
		return false
	}
	return allowed
}

func (s *Server) forbidden(w http.ResponseWriter) {
	writeErrorText(w, http.StatusForbidden, "forbidden")
}

func splitArtifactPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func targetIDFromPath(path string) (string, bool) {
	id := strings.TrimPrefix(path, "/api/targets/")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func userIDFromPath(path string) (string, bool) {
	const prefix = "/api/users/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func paginateHosts(hosts []store.HostView, r *http.Request) pagedHostList {
	query := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
	if query != "" {
		filtered := hosts[:0]
		for _, host := range hosts {
			values := []string{
				host.ID,
				host.Name,
				host.Group,
				host.Host,
				strconv.Itoa(host.Port),
				host.Remark,
			}
			for _, value := range values {
				if strings.Contains(strings.ToLower(value), query) {
					filtered = append(filtered, host)
					break
				}
			}
		}
		hosts = filtered
	}

	page := positiveIntRequestQuery(r, "page", 1)
	pageSize := positiveIntRequestQuery(r, "page_size", 20)
	if pageSize > 200 {
		pageSize = 200
	}
	total := len(hosts)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return pagedHostList{
		Data:     hosts[start:end],
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}
}

func positiveIntRequestQuery(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func hostPathParts(path string) (string, string, bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/hosts/"), "/")
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		return parts[0], "", true
	}
	if len(parts) == 2 && parts[1] == "accounts" {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func dbInstancePathParts(path string) (id, child string, ok bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/db/instances/"), "/")
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		return parts[0], "", true
	}
	if len(parts) == 2 && parts[1] == "accounts" {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func dbAccountIDFromPath(path string) (string, bool) {
	id := strings.TrimPrefix(path, "/api/db/accounts/")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func writeHostStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrHostNotFound) || errors.Is(err, access.ErrHostNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}

func writeDBStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrDBProxyNotFound) || errors.Is(err, store.ErrDBAccountNotFound) || errors.Is(err, store.ErrDBInstanceNotFound) ||
		errors.Is(err, access.ErrDBProxyNotFound) || errors.Is(err, access.ErrDBAccountNotFound) || errors.Is(err, access.ErrDBInstanceNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}

func writeTargetStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrTargetNotFound) || errors.Is(err, access.ErrTargetNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}

func safeReplayDir(root, id string) (string, bool) {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return "", false
	}
	return filepath.Join(root, id), true
}

func writeJSONFile(w http.ResponseWriter, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErrorText(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(raw)
}

func writeTextFile(w http.ResponseWriter, path, contentType string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErrorText(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(raw)
}

func writeJSONLines(w http.ResponseWriter, path string, limit int) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer file.Close()

	items := make([]map[string]any, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if limit > 0 && len(items) >= limit {
			break
		}
		var item map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &item); err == nil {
			items = append(items, item)
		}
	}
	if err := scanner.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeErrorText(w, status, err.Error())
}

func writeErrorText(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Debug("admin request", "method", r.Method, "path", r.URL.Path, "elapsed", time.Since(started))
	})
}

func withCORS(allowedOrigins []string, next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := allowed[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func friendlySSHError(err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "unable to authenticate") || strings.Contains(lower, "no supported methods"):
		return "认证失败，请检查用户名和密码/私钥是否正确"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "i/o timeout"):
		return "连接超时，请检查主机地址和端口是否可达"
	case strings.Contains(lower, "connection refused"):
		return "连接被拒绝，请检查主机地址和端口是否正确，以及 SSH 服务是否已启动"
	case strings.Contains(lower, "no route to host") || strings.Contains(lower, "no such host"):
		return "无法访问主机，请检查主机地址和网络连接"
	case strings.Contains(lower, "host key") && strings.Contains(lower, "mismatch"):
		return "主机密钥不匹配，可能目标主机已变更或存在中间人攻击"
	default:
		return msg
	}
}
