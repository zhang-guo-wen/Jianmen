package admin

import (
	"bufio"
	"context"
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

	"gorm.io/gorm"
	"jianmen/internal/access"
	"jianmen/internal/config"
)

type Server struct {
	cfg            *config.Config
	store          *access.StaticStore
	db             *gorm.DB
	logger         *slog.Logger
	dbProxyApplier databaseProxyApplier
}

type databaseProxyApplier interface {
	Apply([]config.DatabaseProxyConfig) error
}

type sessionListItem struct {
	ID         string `json:"id"`
	User       string `json:"user"`
	Target     string `json:"target"`
	ClientIP   string `json:"client_ip"`
	StartedAt  string `json:"started_at"`
	Path       string `json:"path"`
	HasReplay  bool   `json:"has_replay"`
	ReplaySize int64  `json:"replay_size"`
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

type dbProxyListItem struct {
	Name                 string                           `json:"name"`
	Enabled              bool                             `json:"enabled"`
	Protocol             string                           `json:"protocol"`
	ListenAddr           string                           `json:"listen_addr"`
	UpstreamAddr         string                           `json:"upstream_addr"`
	AllowedUsersEnforced bool                             `json:"allowed_users_enforced"`
	AllowedUsers         []string                         `json:"allowed_users,omitempty"`
	Accounts             []dbProxyAccountListItem         `json:"accounts,omitempty"`
	QueryPolicy          config.DatabaseQueryPolicyConfig `json:"query_policy"`
}

type dbProxyAccountListItem struct {
	Username     string `json:"username"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

type pagedHostList struct {
	Data     []access.HostView `json:"data"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int               `json:"total"`
}

func New(cfg *config.Config, store *access.StaticStore, logger *slog.Logger, dbs ...*gorm.DB) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	var db *gorm.DB
	if len(dbs) > 0 {
		db = dbs[0]
	}
	return &Server{cfg: cfg, store: store, db: db, logger: logger}
}

func (s *Server) SetDatabaseProxyApplier(applier databaseProxyApplier) {
	s.dbProxyApplier = applier
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/health", s.withAuth(s.handleHealth))
	mux.HandleFunc("/api/users", s.withAuth(s.handleUsers))
	mux.HandleFunc("/api/hosts", s.withAuth(s.handleHosts))
	mux.HandleFunc("/api/hosts/", s.withAuth(s.handleHost))
	mux.HandleFunc("/api/targets", s.withAuth(s.handleTargets))
	mux.HandleFunc("/api/targets/", s.withAuth(s.handleTarget))
	mux.HandleFunc(webTerminalPath, s.handleWebTerminal)
	mux.HandleFunc("/api/sessions", s.withAuth(s.handleSessions))
	mux.HandleFunc("/api/sessions/", s.withAuth(s.handleSessionArtifact))
	mux.HandleFunc("/api/db/proxies", s.withAuth(s.handleDBProxies))
	mux.HandleFunc("/api/db/proxies/", s.withAuth(s.handleDBProxy))
	mux.HandleFunc("/api/db/connections", s.withAuth(s.handleDBConnections))
	mux.HandleFunc("/api/db/connections/", s.withAuth(s.handleDBConnectionArtifact))
	mux.HandleFunc("/api/rbac/roles", s.withAuth(s.handleRBACRoles))
	mux.HandleFunc("/api/rbac/roles/", s.withAuth(s.handleRBACRole))
	mux.HandleFunc("/api/rbac/permissions", s.withAuth(s.handleRBACPermissions))
	mux.HandleFunc("/api/rbac/permissions/", s.withAuth(s.handleRBACPermission))
	mux.HandleFunc("/api/rbac/user-roles", s.withAuth(s.handleRBACUserRoles))
	mux.HandleFunc("/api/rbac/user-roles/", s.withAuth(s.handleRBACUserRole))
	mux.HandleFunc("/api/rbac/role-permissions", s.withAuth(s.handleRBACRolePermissions))
	mux.HandleFunc("/api/rbac/role-permissions/", s.withAuth(s.handleRBACRolePermission))
	mux.HandleFunc("/api/rbac/effective", s.withAuth(s.handleRBACEffective))

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

func (s *Server) handleUsers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Users())
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, paginateHosts(s.store.Hosts(), r))
	case http.MethodPost:
		s.handleCreateHost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var host access.HostRecord
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
	var host access.HostRecord
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
		writeJSON(w, http.StatusOK, s.store.Targets())
	case http.MethodPost:
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

func (s *Server) handleTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := targetIDFromPath(r.URL.Path)
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
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

func (s *Server) handleDBProxies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.store.DatabaseProxies())
	case http.MethodPost:
		s.handleCreateDBProxy(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateDBProxy(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var proxy config.DatabaseProxyConfig
	if err := json.NewDecoder(r.Body).Decode(&proxy); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.AddDatabaseProxy(proxy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.applyDatabaseProxies(); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleDBProxy(w http.ResponseWriter, r *http.Request) {
	name, child, account, ok := dbProxyPathParts(r.URL.Path)
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	if child == "accounts" {
		s.handleDBProxyAccounts(w, r, name, account)
		return
	}
	if child != "" {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		view, err := s.store.DatabaseProxy(name)
		if err != nil {
			writeDBStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var proxy config.DatabaseProxyConfig
		if err := json.NewDecoder(r.Body).Decode(&proxy); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		view, err := s.store.UpdateDatabaseProxy(name, proxy)
		if err != nil {
			writeDBStoreError(w, err)
			return
		}
		if err := s.applyDatabaseProxies(); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodDelete:
		if err := s.store.DeleteDatabaseProxy(name); err != nil {
			writeDBStoreError(w, err)
			return
		}
		if err := s.applyDatabaseProxies(); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDBProxyAccounts(w http.ResponseWriter, r *http.Request, name, account string) {
	if account == "" {
		switch r.Method {
		case http.MethodGet:
			accounts, err := s.store.DatabaseAccounts(name)
			if err != nil {
				writeDBStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, accounts)
		case http.MethodPost:
			defer r.Body.Close()
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			var payload config.DatabaseAccountConfig
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			view, err := s.store.AddDatabaseAccount(name, payload)
			if err != nil {
				writeDBStoreError(w, err)
				return
			}
			if err := s.applyDatabaseProxies(); err != nil {
				writeError(w, http.StatusConflict, err)
				return
			}
			writeJSON(w, http.StatusCreated, view)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	switch r.Method {
	case http.MethodPut:
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var payload config.DatabaseAccountConfig
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		view, err := s.store.UpdateDatabaseAccount(name, account, payload)
		if err != nil {
			writeDBStoreError(w, err)
			return
		}
		if err := s.applyDatabaseProxies(); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodDelete:
		if err := s.store.DeleteDatabaseAccount(name, account); err != nil {
			writeDBStoreError(w, err)
			return
		}
		if err := s.applyDatabaseProxies(); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSessions(w http.ResponseWriter, _ *http.Request) {
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
			SessionID string `json:"session_id"`
			User      string `json:"user"`
			Target    string `json:"target"`
			ClientIP  string `json:"client_ip"`
			StartedAt string `json:"started_at"`
		}
		if err := readJSON(filepath.Join(dir, "meta.json"), &meta); err != nil {
			continue
		}
		replaySize := int64(0)
		if info, err := os.Stat(filepath.Join(dir, "terminal.cast")); err == nil {
			replaySize = info.Size()
		}
		out = append(out, sessionListItem{
			ID:         firstNonEmpty(meta.SessionID, entry.Name()),
			User:       meta.User,
			Target:     meta.Target,
			ClientIP:   meta.ClientIP,
			StartedAt:  meta.StartedAt,
			Path:       dir,
			HasReplay:  replaySize > 0,
			ReplaySize: replaySize,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt > out[j].StartedAt
	})
	return out, nil
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

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Admin.Token != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+s.cfg.Admin.Token {
				writeErrorText(w, http.StatusUnauthorized, "missing or invalid bearer token")
				return
			}
		}
		next(w, r)
	}
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

func paginateHosts(hosts []access.HostView, r *http.Request) pagedHostList {
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

func dbProxyPathParts(path string) (name, child, account string, ok bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/db/proxies/"), "/")
	if trimmed == "" {
		return "", "", "", false
	}
	parts := strings.Split(trimmed, "/")
	switch len(parts) {
	case 1:
		return parts[0], "", "", true
	case 2:
		if parts[1] == "accounts" {
			return parts[0], parts[1], "", true
		}
	case 3:
		if parts[1] == "accounts" && parts[2] != "" {
			return parts[0], parts[1], parts[2], true
		}
	}
	return "", "", "", false
}

func writeHostStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, access.ErrHostNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}

func writeDBStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, access.ErrDBProxyNotFound), errors.Is(err, access.ErrDBAccountNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}

func (s *Server) applyDatabaseProxies() error {
	if s.dbProxyApplier == nil {
		return nil
	}
	return s.dbProxyApplier.Apply(s.store.DatabaseProxyConfigs())
}

func writeTargetStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, access.ErrTargetNotFound):
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
