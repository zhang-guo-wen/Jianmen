package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/config"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		hosts, err := s.hostManagement.ListHosts(r.Context(), hostManagementActor(r))
		if err != nil {
			s.writeHostManagementError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, paginateHosts(storeHostViews(hosts), r))
	case http.MethodPost:
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
	view, err := s.hostManagement.CreateHost(r.Context(), hostManagementActor(r), hostManagementHostRecord(host))
	if err != nil {
		s.writeHostManagementError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, storeHostView(view))
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
		accounts, err := s.hostManagement.ListHostAccounts(r.Context(), hostManagementActor(r), id, connectableOnly(r))
		if err != nil {
			s.writeHostManagementError(w, r, err)
			return
		}
		resp := paginateSlice(storeTargetViews(accounts), r, func(v store.TargetView, q string) bool {
			return strings.Contains(strings.ToLower(v.Username), q) || strings.Contains(strings.ToLower(v.Name), q) || strings.Contains(strings.ToLower(v.Group), q) || strings.Contains(strings.ToLower(v.Remark), q)
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
		view, err := s.hostManagement.Host(r.Context(), hostManagementActor(r), id)
		if err != nil {
			s.writeHostManagementError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, storeHostView(view))
	case http.MethodPut:
		s.handleUpdateHost(w, r, id)
	case http.MethodDelete:
		if err := s.hostManagement.DeleteHost(r.Context(), hostManagementActor(r), id); err != nil {
			s.writeHostManagementError(w, r, err)
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
	view, err := s.hostManagement.UpdateHost(r.Context(), hostManagementActor(r), id, hostManagementHostRecord(host))
	if err != nil {
		s.writeHostManagementError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, storeHostView(view))
}

func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		targets, err := s.hostManagement.ListTargets(r.Context(), hostManagementActor(r), connectableOnly(r))
		if err != nil {
			s.writeHostManagementError(w, r, err)
			return
		}
		resp := paginateSlice(storeTargetViews(targets), r, func(v store.TargetView, q string) bool {
			return strings.Contains(strings.ToLower(v.Name), q) || strings.Contains(strings.ToLower(v.Username), q) || strings.Contains(strings.ToLower(v.Host), q) || strings.Contains(strings.ToLower(v.Group), q) || strings.Contains(strings.ToLower(v.Remark), q)
		})
		s.writeJSON(w, r, http.StatusOK, resp)
	case http.MethodPost:
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
	view, err := s.hostManagement.CreateTarget(r.Context(), hostManagementActor(r), target)
	if err != nil {
		s.writeHostManagementError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, storeTargetView(view))
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	defer r.Body.Close()
	var target config.Target
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	targetCfg, err := s.hostManagement.ResolveConnectionTest(r.Context(), hostManagementActor(r), target)
	if err != nil {
		s.writeHostManagementError(w, r, err)
		return
	}
	addr := targetCfg.Addr()
	if strings.EqualFold(targetCfg.Protocol, "rdp") {
		if s.webRDP == nil {
			s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": false, "error": "Web RDP is not enabled"})
			return
		}
		start := time.Now()
		err := s.webRDP.TestConnection(r.Context(), storeTargetConfig(targetCfg))
		latencyMS := time.Since(start).Milliseconds()
		if err != nil {
			s.logger.Warn("RDP connection test failed", "target", targetCfg.ID, "address", addr, "error", err)
			s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": false, "latency_ms": latencyMS, "verification_scope": "guacd_handshake", "authentication_verified": false, "error": "RDP proxy handshake failed"})
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "latency_ms": latencyMS, "verification_scope": "guacd_handshake", "authentication_verified": false, "message": "RDP proxy handshake succeeded (" + addr + ")"})
		return
	}
	clientConfig, err := store.ClientConfigForTarget(storeTargetConfig(targetCfg))
	if err != nil {
		if s.writeSSHHostIdentityError(w, r, err) {
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": false, "error": "configuration error: " + err.Error()})
		return
	}
	clientConfig.Timeout = 10 * time.Second
	start := time.Now()
	conn, err := ssh.Dial("tcp", addr, clientConfig)
	latencyMS := time.Since(start).Milliseconds()
	if err != nil {
		s.logger.Warn("ssh connection test failed", "target", targetCfg.ID, "address", addr, "error", err)
		if s.writeSSHHostIdentityError(w, r, err) {
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": false, "latency_ms": latencyMS, "error": "connection failed: " + friendlySSHError(err)})
		return
	}
	_ = conn.Close()
	s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "latency_ms": latencyMS, "message": "connection succeeded (" + addr + ")"})
}

func (s *Server) handleTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := targetIDFromPath(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		view, err := s.hostManagement.Target(r.Context(), hostManagementActor(r), id)
		if err != nil {
			s.writeHostManagementError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, storeTargetView(view))
	case http.MethodPut:
		s.handleUpdateTarget(w, r, id)
	case http.MethodDelete:
		if err := s.hostManagement.DeleteTarget(r.Context(), hostManagementActor(r), id); err != nil {
			s.writeHostManagementError(w, r, err)
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
	view, err := s.hostManagement.UpdateTarget(r.Context(), hostManagementActor(r), id, target)
	if err != nil {
		s.writeHostManagementError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, storeTargetView(view))
}

func hostManagementActor(r *http.Request) service.HostManagementActor {
	return service.HostManagementActor{ID: userIDFromRequest(r), SuperAdmin: isSuperAdminRequest(r)}
}

func (s *Server) writeHostManagementError(w http.ResponseWriter, r *http.Request, err error) {
	if s.writeSSHHostIdentityError(w, r, err) {
		return
	}
	switch {
	case errors.Is(err, service.ErrHostAccessDenied):
		s.forbidden(w, r)
	case errors.Is(err, service.ErrHostTargetUnavailable):
		s.writeErrorText(w, r, http.StatusForbidden, "target is disabled, expired, or unavailable")
	case errors.Is(err, service.ErrHostTargetInvalidInput):
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrHostNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, "host not found")
	case errors.Is(err, store.ErrTargetNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, "target not found")
	default:
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
	}
}

func hostManagementHostRecord(record store.HostRecord) service.HostManagementHostRecord {
	return service.HostManagementHostRecord{ID: record.ID, Name: record.Name, Group: record.Group, Address: record.Address, Port: record.Port, Protocol: record.Protocol, Remark: record.Remark, Status: record.Status, HostKeyFingerprint: record.HostKeyFingerprint, KnownHosts: record.KnownHosts}
}

func storeHostViews(views []service.HostManagementHostView) []store.HostView {
	result := make([]store.HostView, len(views))
	for index := range views {
		result[index] = storeHostView(views[index])
	}
	return result
}

func storeHostView(view service.HostManagementHostView) store.HostView {
	return store.HostView{ID: view.ID, Name: view.Name, Group: view.Group, Address: view.Address, Port: view.Port, Protocol: view.Protocol, Remark: view.Remark, Status: view.Status, HostKeyFingerprint: view.HostKeyFingerprint, KnownHosts: view.KnownHosts, IdentityStatus: view.IdentityStatus, HostKeyChangeHandler: storeHostKeyChangeHandler(view.HostKeyChangeHandler), AccountCount: view.AccountCount, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt, CanManage: view.CanManage}
}

func storeTargetViews(views []service.HostManagementTargetView) []store.TargetView {
	result := make([]store.TargetView, len(views))
	for index := range views {
		result[index] = storeTargetView(views[index])
	}
	return result
}

func storeTargetView(view service.HostManagementTargetView) store.TargetView {
	return store.TargetView{ID: view.ID, HostID: view.HostID, ResourceType: view.ResourceType, ResourceID: view.ResourceID, ResourceSeq: view.ResourceSeq, HostResourceID: view.HostResourceID, Name: view.Name, Group: view.Group, Remark: view.Remark, ExpiresAt: view.ExpiresAt, Status: view.Status, HostStatus: view.HostStatus, Host: view.Host, Port: view.Port, Protocol: view.Protocol, Username: view.Username, Domain: view.Domain, AuthMethods: view.AuthMethods, InsecureIgnoreHostKey: view.InsecureIgnoreHostKey, HostKeyFingerprint: view.HostKeyFingerprint, KnownHostsPath: view.KnownHostsPath, RDPSecurity: view.RDPSecurity, RDPIgnoreCertificate: view.RDPIgnoreCertificate, RDPCertFingerprints: view.RDPCertFingerprints, RDPApprovalRequired: view.RDPApprovalRequired, RDPClipboardRead: view.RDPClipboardRead, RDPClipboardWrite: view.RDPClipboardWrite, RDPFileUpload: view.RDPFileUpload, RDPFileDownload: view.RDPFileDownload, RDPDriveMapping: view.RDPDriveMapping, CanManage: view.CanManage}
}

func storeTargetConfig(config service.HostManagementTargetConfig) store.TargetConfig {
	return store.TargetConfig{ID: config.ID, Name: config.Name, HostName: config.HostName, Host: config.Host, Port: config.Port, Protocol: config.Protocol, Username: config.Username, Domain: config.Domain, Password: config.Password, PrivateKeyPath: config.PrivateKeyPath, PrivateKeyPEM: config.PrivateKeyPEM, Passphrase: config.Passphrase, InsecureIgnoreHostKey: config.InsecureIgnoreHostKey, HostKeyFingerprint: config.HostKeyFingerprint, KnownHosts: config.KnownHosts, KnownHostsPath: config.KnownHostsPath, HostKeyChangeHandler: storeHostKeyChangeHandler(config.HostKeyChangeHandler), RDPSecurity: config.RDPSecurity, RDPIgnoreCertificate: config.RDPIgnoreCertificate, RDPCertFingerprints: config.RDPCertFingerprints, RDPApprovalRequired: config.RDPApprovalRequired, RDPClipboardRead: config.RDPClipboardRead, RDPClipboardWrite: config.RDPClipboardWrite, RDPFileUpload: config.RDPFileUpload, RDPFileDownload: config.RDPFileDownload, RDPDriveMapping: config.RDPDriveMapping, Disabled: config.Disabled, ExpiresAt: config.ExpiresAt, HostID: config.HostID}
}
