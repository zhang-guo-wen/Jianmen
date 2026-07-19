package admin

import (
	"encoding/json"
	"jianmen/internal/config"
	"jianmen/internal/model"
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
		if !s.requireAuthenticatedUser(w, r) {
			return
		}
		hosts, err := s.hostTargets.Hosts(r.Context())
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		hosts, err = s.visibleHosts(r, hosts)
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, paginateHosts(hosts, r))
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
	view, err := s.hostTargets.AddHost(r.Context(), host)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.grantCreatedResource(r, model.ResourceTypeHost, view.ID); err != nil {
		_ = s.hostTargets.DeleteHost(r.Context(), view.ID)
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
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
		if !s.requireAuthenticatedUser(w, r) {
			return
		}
		var actions []string
		if connectableOnly(r) {
			if !s.requireAnyPermission(r, rbac.ActionSessionConnect, rbac.ActionSFTPConnect, rbac.ActionRDPConnect) {
				s.forbidden(w, r)
				return
			}
		} else {
			actions = []string{rbac.ActionTargetView}
		}
		accounts, err := s.resourceAccess.ListHostAccounts(r.Context(), id)
		if err != nil {
			writeHostStoreError(w, r, err)
			return
		}
		if connectableOnly(r) {
			accounts, err = s.visibleConnectableTargets(r, accounts)
		} else {
			accounts, err = s.visibleTargetsForActions(r, accounts, actions)
		}
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
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
		visible, err := s.hostVisible(r, id)
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if !visible {
			s.forbidden(w, r)
			return
		}
		view, err := s.hostTargets.Host(r.Context(), id)
		if err != nil {
			writeHostStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requireResourceAction(w, r, rbac.ActionHostUpdate, model.ResourceTypeHost, id) {
			return
		}
		s.handleUpdateHost(w, r, id)
	case http.MethodDelete:
		if !s.requireResourceAction(w, r, rbac.ActionHostDelete, model.ResourceTypeHost, id) {
			return
		}
		if err := s.hostTargets.DeleteHost(r.Context(), id); err != nil {
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
	view, err := s.hostTargets.UpdateHost(r.Context(), id, host)
	if err != nil {
		writeHostStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requireAuthenticatedUser(w, r) {
			return
		}
		var actions []string
		if connectableOnly(r) {
			if !s.requireAnyPermission(r, rbac.ActionSessionConnect, rbac.ActionSFTPConnect, rbac.ActionRDPConnect) {
				s.forbidden(w, r)
				return
			}
		} else {
			actions = []string{rbac.ActionTargetView}
		}
		var err error
		targets, err := s.hostTargets.Targets(r.Context())
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if connectableOnly(r) {
			targets, err = s.visibleConnectableTargets(r, targets)
		} else {
			targets, err = s.visibleTargetsForActions(r, targets, actions)
		}
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		resp := paginateSlice(targets, r, func(v store.TargetView, q string) bool {
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
	if strings.TrimSpace(target.HostID) == "" {
		if !isSuperAdminRequest(r) {
			s.writeErrorText(w, r, http.StatusBadRequest, "host_id is required")
			return
		}
	} else if !s.requireResourceAction(w, r, rbac.ActionTargetCreate, model.ResourceTypeHost, target.HostID) {
		return
	}
	view, err := s.hostTargets.AddTarget(r.Context(), target)
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
	if target.ID != "" {
		if !s.requireResourceAction(w, r, rbac.ActionTargetCreate, model.ResourceTypeHostAccount, target.ID) {
			return
		}
	} else if target.HostID != "" && !s.requireResourceAction(w, r, rbac.ActionTargetCreate, model.ResourceTypeHost, target.HostID) {
		return
	}
	hostKeyConfigProvided := target.InsecureIgnoreHostKey || target.HostKeyFingerprint != "" || target.KnownHostsPath != ""

	targetCfg := store.TargetConfig{
		ID:                    target.ID,
		Name:                  target.Name,
		Host:                  target.Host,
		Port:                  target.Port,
		Protocol:              target.Protocol,
		Username:              target.Username,
		Domain:                target.Domain,
		Password:              target.Password,
		PrivateKeyPath:        target.PrivateKeyPath,
		PrivateKeyPEM:         target.PrivateKeyPEM,
		Passphrase:            target.Passphrase,
		InsecureIgnoreHostKey: target.InsecureIgnoreHostKey,
		HostKeyFingerprint:    target.HostKeyFingerprint,
		KnownHostsPath:        target.KnownHostsPath,
		RDPSecurity:           target.RDPSecurity,
		RDPIgnoreCertificate:  target.RDPIgnoreCertificate,
		RDPCertFingerprints:   target.RDPCertFingerprints,
		Disabled:              target.Disabled,
		ExpiresAt:             target.ExpiresAt,
		HostID:                target.HostID,
	}
	if targetCfg.Password == "" && targetCfg.PrivateKeyPath == "" && targetCfg.PrivateKeyPEM == "" && targetCfg.ID != "" {
		storedTarget, err := s.hostTargets.TargetConfig(r.Context(), targetCfg.ID)
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
		storedTarget.Protocol = firstNonEmpty(targetCfg.Protocol, storedTarget.Protocol)
		storedTarget.Domain = firstNonEmpty(targetCfg.Domain, storedTarget.Domain)
		storedTarget.RDPSecurity = firstNonEmpty(targetCfg.RDPSecurity, storedTarget.RDPSecurity)
		storedTarget.RDPIgnoreCertificate = targetCfg.RDPIgnoreCertificate
		storedTarget.RDPCertFingerprints = firstNonEmpty(
			targetCfg.RDPCertFingerprints, storedTarget.RDPCertFingerprints,
		)
		if hostKeyConfigProvided {
			storedTarget.InsecureIgnoreHostKey = targetCfg.InsecureIgnoreHostKey
			storedTarget.HostKeyFingerprint = targetCfg.HostKeyFingerprint
			storedTarget.KnownHostsPath = targetCfg.KnownHostsPath
		}
		storedTarget.Disabled = targetCfg.Disabled
		storedTarget.ExpiresAt = targetCfg.ExpiresAt
		targetCfg = storedTarget
	}

	if targetCfg.HostID != "" && (targetCfg.Host == "" || targetCfg.Port == 0) {
		host, err := s.hostTargets.Host(r.Context(), targetCfg.HostID)
		if err != nil {
			s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": false, "error": "configuration error: " + err.Error()})
			return
		}
		targetCfg.Host = firstNonEmpty(targetCfg.Host, host.Address)
		targetCfg.Protocol = firstNonEmpty(targetCfg.Protocol, host.Protocol)
		if targetCfg.Port == 0 {
			targetCfg.Port = host.Port
		}
	}

	addr := targetCfg.Addr()
	if addr == "" || targetCfg.Username == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "host, port, and username are required")
		return
	}
	if strings.EqualFold(targetCfg.Protocol, "rdp") {
		if s.webRDP == nil {
			s.writeJSON(w, r, http.StatusOK, map[string]any{
				"ok": false, "error": "Web RDP 未启用，无法测试 RDP 账号",
			})
			return
		}
		start := time.Now()
		err := s.webRDP.TestConnection(r.Context(), targetCfg)
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			s.logger.Warn("RDP connection test failed", "target", targetCfg.ID, "address", addr, "error", err)
			s.writeJSON(w, r, http.StatusOK, map[string]any{
				"ok": false, "latency_ms": latencyMs, "verification_scope": "guacd_handshake",
				"authentication_verified": false, "error": "RDP 代理握手失败",
			})
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			"ok": true, "latency_ms": latencyMs, "verification_scope": "guacd_handshake",
			"authentication_verified": false,
			"message":                 "RDP 代理握手成功；目标 Windows 账号将在实际 Web RDP 会话中认证 (" + addr + ")",
		})
		return
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
		s.logger.Warn("ssh connection test failed", "target", targetCfg.ID, "address", addr, "error", err)
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
		if !s.requireResourceAction(w, r, rbac.ActionTargetView, model.ResourceTypeHostAccount, id) {
			return
		}
		view, err := s.hostTargets.Target(r.Context(), id)
		if err != nil {
			writeTargetStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requireResourceAction(w, r, rbac.ActionTargetUpdate, model.ResourceTypeHostAccount, id) {
			return
		}
		s.handleUpdateTarget(w, r, id)
	case http.MethodDelete:
		if !s.requireResourceAction(w, r, rbac.ActionTargetDelete, model.ResourceTypeHostAccount, id) {
			return
		}
		if err := s.hostTargets.DeleteTarget(r.Context(), id); err != nil {
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
	view, err := s.hostTargets.UpdateTarget(r.Context(), id, target)
	if err != nil {
		writeTargetStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

// -- db gateway config --
