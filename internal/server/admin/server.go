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
	"strings"
	"time"

	"jianmen/internal/access"
	"jianmen/internal/config"
)

type Server struct {
	cfg    *config.Config
	store  *access.StaticStore
	logger *slog.Logger
}

type sessionListItem struct {
	ID        string `json:"id"`
	User      string `json:"user"`
	Target    string `json:"target"`
	ClientIP  string `json:"client_ip"`
	StartedAt string `json:"started_at"`
	Path      string `json:"path"`
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

func New(cfg *config.Config, store *access.StaticStore, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, store: store, logger: logger}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/health", s.withAuth(s.handleHealth))
	mux.HandleFunc("/api/users", s.withAuth(s.handleUsers))
	mux.HandleFunc("/api/targets", s.withAuth(s.handleTargets))
	mux.HandleFunc("/api/targets/", s.withAuth(s.handleTarget))
	mux.HandleFunc(webTerminalPath, s.handleWebTerminal)
	mux.HandleFunc("/api/sessions", s.withAuth(s.handleSessions))
	mux.HandleFunc("/api/sessions/", s.withAuth(s.handleSessionArtifact))
	mux.HandleFunc("/api/db/connections", s.withAuth(s.handleDBConnections))
	mux.HandleFunc("/api/db/connections/", s.withAuth(s.handleDBConnectionArtifact))

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
		out = append(out, sessionListItem{
			ID:        firstNonEmpty(meta.SessionID, entry.Name()),
			User:      meta.User,
			Target:    meta.Target,
			ClientIP:  meta.ClientIP,
			StartedAt: meta.StartedAt,
			Path:      dir,
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

func writeTargetStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, access.ErrTargetNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, access.ErrStaticTargetDelete):
		writeError(w, http.StatusConflict, err)
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
