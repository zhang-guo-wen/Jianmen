package admin

import (
	"encoding/json"
	"jianmen/internal/config"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionHostView) {
			s.forbidden(w, r)
			return
		}
		s.writeJSON(w, r, http.StatusOK, paginateHosts(s.store.Hosts(), r))
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionHostCreate) {
			s.forbidden(w, r)
			return
		}
		s.handleCreateHost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var host store.HostRecord
	if err := json.NewDecoder(r.Body).Decode(&host); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.store.AddHost(host)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleHost(w http.ResponseWriter, r *http.Request) {
	id, child, ok := hostPathParts(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if child == "accounts" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		accounts, err := s.store.HostAccounts(id)
		if err != nil {
			writeHostStoreError(w, r, err)
			return
		}
		resp := paginateSlice(accounts, r, func(v store.TargetView, q string) bool {
			return strings.Contains(strings.ToLower(v.Username), q) ||
				strings.Contains(strings.ToLower(v.Name), q) ||
				strings.Contains(strings.ToLower(v.Group), q) ||
				strings.Contains(strings.ToLower(v.Remark), q)
		})
		s.writeJSON(w, r, http.StatusOK, resp)
		return
	}
	if child != "" {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionHostView) {
			s.forbidden(w, r)
			return
		}
		view, err := s.store.Host(id)
		if err != nil {
			writeHostStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		s.handleUpdateHost(w, r, id)
	case http.MethodDelete:
		if err := s.store.DeleteHost(id); err != nil {
			writeHostStoreError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var host store.HostRecord
	if err := json.NewDecoder(r.Body).Decode(&host); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.store.UpdateHost(id, host)
	if err != nil {
		writeHostStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionTargetView) {
			s.forbidden(w, r)
			return
		}
		resp := paginateSlice(s.store.Targets(), r, func(v store.TargetView, q string) bool {
			return strings.Contains(strings.ToLower(v.Name), q) ||
				strings.Contains(strings.ToLower(v.Username), q) ||
				strings.Contains(strings.ToLower(v.Host), q) ||
				strings.Contains(strings.ToLower(v.Group), q) ||
				strings.Contains(strings.ToLower(v.Remark), q)
		})
		s.writeJSON(w, r, http.StatusOK, resp)
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionTargetCreate) {
			s.forbidden(w, r)
			return
		}
		s.handleCreateTarget(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var target config.Target
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.store.AddTarget(target)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionTargetCreate) {
		s.forbidden(w, r)
		return
	}
	defer r.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	var target config.Target
	if err := json.Unmarshal(encoded, &target); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	hostKeyConfigProvided := target.InsecureIgnoreHostKey || target.HostKeyFingerprint != "" || target.KnownHostsPath != ""

	targetCfg := store.TargetConfig{
		ID:                    target.ID,
		Name:                  target.Name,
		Host:                  target.Host,
		Port:                  target.Port,
		Username:              target.Username,
		Password:              target.Password,
		PrivateKeyPath:        target.PrivateKeyPath,
		PrivateKeyPEM:         target.PrivateKeyPEM,
		Passphrase:            target.Passphrase,
		InsecureIgnoreHostKey: target.InsecureIgnoreHostKey,
		HostKeyFingerprint:    target.HostKeyFingerprint,
		KnownHostsPath:        target.KnownHostsPath,
		Disabled:              target.Disabled,
		ExpiresAt:             target.ExpiresAt,
		HostID:                target.HostID,
	}
	if targetCfg.Password == "" && targetCfg.PrivateKeyPath == "" && targetCfg.PrivateKeyPEM == "" && targetCfg.ID != "" {
		storedTarget, err := s.store.TargetConfig(targetCfg.ID)
		if err != nil {
			s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": false, "message": "配置错误: " + err.Error()})
			return
		}
		storedTarget.Host = firstNonEmpty(targetCfg.Host, storedTarget.Host)
		if targetCfg.Port != 0 {
			storedTarget.Port = targetCfg.Port
		}
		storedTarget.Username = firstNonEmpty(targetCfg.Username, storedTarget.Username)
		storedTarget.Name = firstNonEmpty(targetCfg.Name, storedTarget.Name)
		storedTarget.HostID = firstNonEmpty(targetCfg.HostID, storedTarget.HostID)
		if hostKeyConfigProvided {
			storedTarget.InsecureIgnoreHostKey = targetCfg.InsecureIgnoreHostKey
			storedTarget.HostKeyFingerprint = targetCfg.HostKeyFingerprint
			storedTarget.KnownHostsPath = targetCfg.KnownHostsPath
		}
		storedTarget.Disabled = targetCfg.Disabled
		storedTarget.ExpiresAt = targetCfg.ExpiresAt
		targetCfg = storedTarget
	}

	addr := targetCfg.Addr()
	if addr == "" || targetCfg.Username == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "host, port, and username are required")
		return
	}

	// 测试连接默认允许跳过主机密钥验证（除非用户明确配置了指纹或 known_hosts）
	if !targetCfg.InsecureIgnoreHostKey && targetCfg.HostKeyFingerprint == "" && targetCfg.KnownHostsPath == "" {
		targetCfg.InsecureIgnoreHostKey = true
	}

	clientConfig, err := store.ClientConfigForTarget(targetCfg)
	if err != nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": false, "error": "配置错误: " + err.Error()})
		return
	}

	clientConfig.Timeout = 10 * time.Second

	start := time.Now()
	conn, err := ssh.Dial("tcp", addr, clientConfig)
	elapsed := time.Since(start)
	latencyMs := elapsed.Milliseconds()
	if err != nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": false, "latency_ms": latencyMs, "error": "连接失败: " + friendlySSHError(err)})
		return
	}
	conn.Close()
	s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "latency_ms": latencyMs, "message": "连接成功 (" + addr + ")"})
}

func (s *Server) handleTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := targetIDFromPath(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionTargetView) {
			s.forbidden(w, r)
			return
		}
		view, err := s.store.Target(id)
		if err != nil {
			writeTargetStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		s.handleUpdateTarget(w, r, id)
	case http.MethodDelete:
		if err := s.store.DeleteTarget(id); err != nil {
			writeTargetStoreError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateTarget(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	var target config.Target
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.store.UpdateTarget(id, target)
	if err != nil {
		writeTargetStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

// -- db gateway config --
