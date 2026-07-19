package admin

import (
	"bufio"
	"encoding/json"
	"errors"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/util"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (s *Server) handleUserSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusUnauthorized, "user not authenticated")
		return
	}

	var req struct {
		TargetID string `json:"target_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "target_id is required")
		return
	}

	// Look up the target to determine resource type and resource_id
	// 先尝试主机账号，再尝试数据库账号
	compactPrefix := util.PrefixHost
	resourceType := "host_account"
	var resourceID string

	var hostAccount model.HostAccount
	connectionActions := []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}
	if err := s.db.Where("id = ? AND status = ?", req.TargetID, "active").First(&hostAccount).Error; err == nil {
		// 主机账号
		var host model.Host
		if err := s.db.Where("id = ? AND status = ?", hostAccount.HostID, "active").First(&host).Error; err != nil {
			s.writeErrorText(w, r, http.StatusForbidden, "host is disabled or not found")
			return
		}
		resourceID = hostAccount.ResourceID
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		// 尝试数据库账号
		var dbAccount model.DatabaseAccount
		if err := s.db.Preload("Instance").Where("id = ? AND status = ?", req.TargetID, "active").First(&dbAccount).Error; err == nil {
			// 验证数据库实例未被禁用
			if dbAccount.Instance.Status == "disabled" {
				s.writeErrorText(w, r, http.StatusForbidden, "database instance is disabled")
				return
			}
			compactPrefix = util.PrefixDatabase
			resourceType = "database_account"
			connectionActions = []string{rbac.ActionDBConnect}
			resourceID = dbAccount.ResourceID
			// Redis 实例使用 R 前缀
			if dbAccount.Instance.Protocol == "redis" {
				compactPrefix = util.PrefixRedis
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			s.writeErrorText(w, r, http.StatusNotFound, "target account not found or disabled")
			return
		} else {
			s.writeErrorText(w, r, http.StatusInternalServerError, "failed to look up target")
			return
		}
	} else {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to look up target")
		return
	}
	allowed, err := s.authorizeAnyConnection(r.Context(), userID, connectionActions, resourceType, req.TargetID)
	if err != nil {
		s.logger.Warn("connection configuration authorization failed", "user_id", userID, "resource_type", resourceType, "resource_id", req.TargetID, "error", err)
		s.forbidden(w, r)
		return
	}
	if !allowed {
		s.forbidden(w, r)
		return
	}

	// 使用用户的永久会话（session ID 固定），不每次新建
	var permSession model.UserSession
	if err := s.db.Where("user_id = ? AND type = ? AND status = ?", userID, "permanent", "active").
		First(&permSession).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 尚未有永久会话则创建一个
			newSess := model.UserSession{
				UserID: userID,
				Type:   "permanent",
				Status: "active",
			}
			created, createErr := s.userSessions.CreateUserSession(newSess)
			if createErr != nil {
				s.writeErrorText(w, r, http.StatusInternalServerError, createErr.Error())
				return
			}
			permSession = *created
		} else {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}

	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"id":               permSession.ID,
		"session_id":       permSession.SessionID,
		"session_seq":      permSession.SessionSeq,
		"type":             permSession.Type,
		"status":           permSession.Status,
		"resource_id":      resourceID,
		"compact_username": compactPrefix + resourceID + permSession.SessionID,
		"resource_type":    resourceType,
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionSessionView) {
		s.forbidden(w, r)
		return
	}
	sessions, err := s.listSessions()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	resp := paginateSlice(sessions, r, func(v sessionListItem, q string) bool {
		return strings.Contains(strings.ToLower(v.User), q) ||
			strings.Contains(strings.ToLower(v.Target), q) ||
			strings.Contains(strings.ToLower(v.Protocol), q) ||
			strings.Contains(strings.ToLower(v.ClientIP), q)
	})
	s.writeJSON(w, r, http.StatusOK, resp)
}

func (s *Server) handleSessionArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.requireAnyPermission(r, rbac.ActionAuditView, rbac.ActionSessionView) {
		s.forbidden(w, r)
		return
	}
	id, artifact, ok := splitArtifactPath(strings.TrimPrefix(r.URL.Path, "/api/sessions/"))
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	dir, ok := safeReplayDir(filepath.Join(s.cfg.ReplayDir, "ssh"), id)
	if !ok {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid session id")
		return
	}
	switch artifact {
	case "meta":
		s.writeJSONFile(w, r, filepath.Join(dir, "meta.json"))
	case "commands":
		s.writeJSONLines(w, r, filepath.Join(dir, "commands.jsonl"), 500)
	case "files":
		s.writeJSONLines(w, r, filepath.Join(dir, "files.jsonl"), 1000)
	case "file-summary":
		s.writeJSONFile(w, r, filepath.Join(dir, "files-summary.json"))
	case "replay":
		s.writeTextFile(w, r, filepath.Join(dir, "terminal.cast"), "application/x-asciicast; charset=utf-8")
	default:
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleDBConnections(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionDBAuditView) {
		s.forbidden(w, r)
		return
	}
	connections, err := s.listDBConnections()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	resp := paginateSlice(connections, r, func(v dbConnectionListItem, q string) bool {
		return strings.Contains(strings.ToLower(v.AccountName), q) ||
			strings.Contains(strings.ToLower(v.InstanceName), q) ||
			strings.Contains(strings.ToLower(v.Protocol), q) ||
			strings.Contains(strings.ToLower(v.AuthUser), q)
	})
	s.writeJSON(w, r, http.StatusOK, resp)
}

func (s *Server) handleDBConnectionArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionDBAuditView) {
		s.forbidden(w, r)
		return
	}
	id, artifact, ok := splitArtifactPath(strings.TrimPrefix(r.URL.Path, "/api/db/connections/"))
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	dir, ok := safeReplayDir(filepath.Join(s.cfg.ReplayDir, "db"), id)
	if !ok {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid connection id")
		return
	}
	switch artifact {
	case "meta":
		s.writeJSONFile(w, r, filepath.Join(dir, "meta.json"))
	case "queries":
		s.writeJSONLines(w, r, filepath.Join(dir, "queries.jsonl"), 1000)
	default:
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
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
			AccountUsername string `json:"account_username"`
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
			AccountUsername: meta.AccountUsername,
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
			EndedAt      string `json:"ended_at"`
			DurationMs   int64  `json:"duration_ms"`
			AccountName  string `json:"account_name"`
			InstanceName string `json:"instance_name"`
			AuthUser     string `json:"auth_user"`
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
			EndedAt:      meta.EndedAt,
			DurationMs:   meta.DurationMs,
			AccountName:  meta.AccountName,
			InstanceName: meta.InstanceName,
			AuthUser:     meta.AuthUser,
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
	ctxKeyUserID         contextKey = "admin_user_id"
	ctxKeyUsername       contextKey = "admin_username"
	ctxKeySuperAdmin     contextKey = "admin_super_admin"
	ctxKeyBrowserSession contextKey = "admin_browser_session"
	ctxKeyAITokenID      contextKey = "ai_token_id"
)
