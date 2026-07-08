package admin

import (
	"encoding/json"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleDBGateway(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg := s.cfg.DatabaseGateway
	host, port := parseListenAddr(cfg.ListenAddr)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     cfg.Enabled,
		"listen_addr": cfg.ListenAddr,
		"host":        host,
		"port":        port,
	})
}

func parseListenAddr(addr string) (host string, port int) {
	if addr == "" {
		return "127.0.0.1", 33060
	}
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 33060
	}
	host = h
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if n, err := strconv.Atoi(p); err == nil {
		port = n
	} else {
		port = 33060
	}
	return
}

// ensureDBPort 确保数据库地址包含端口，缺省则按协议补默认端口
func ensureDBPort(address, protocol string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return address
	}
	// 已含端口则原样返回
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	defaultPort := "3306"
	if protocol == "postgres" {
		defaultPort = "5432"
	}
	return address + ":" + defaultPort
}

// -- db instances --

func (s *Server) handleDBInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionDBProxyView) {
			s.forbidden(w)
			return
		}
		instances := s.store.DatabaseInstances()
		resp := paginateSlice(instances, r, func(v store.DatabaseInstanceView, q string) bool {
			return strings.Contains(strings.ToLower(v.Name), q) ||
				strings.Contains(strings.ToLower(v.Address), q) ||
				strings.Contains(strings.ToLower(v.Protocol), q) ||
				strings.Contains(strings.ToLower(v.Group), q) ||
				strings.Contains(strings.ToLower(v.Remark), q)
		})
		writeJSON(w, http.StatusOK, resp)
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
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Address  string `json:"address"`
		Port     int    `json:"port"`
		Group    string `json:"group"`
		Remark   string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.AddDatabaseInstance(payload.Name, payload.Protocol, payload.Address, payload.Port, payload.Group, payload.Remark)
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

			resp := paginateSlice(accounts, r, func(v store.DatabaseAccountView, q string) bool {
				return strings.Contains(strings.ToLower(v.UniqueName), q) ||
					strings.Contains(strings.ToLower(v.Username), q) ||
					strings.Contains(strings.ToLower(v.Group), q) ||
					strings.Contains(strings.ToLower(v.Remark), q)
			})
			writeJSON(w, http.StatusOK, resp)
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
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Address  string `json:"address"`
		Port     int    `json:"port"`
		Group    string `json:"group"`
		Remark   string `json:"remark"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.UpdateDatabaseInstance(id, payload.Name, payload.Protocol, payload.Address, payload.Port, payload.Group, payload.Remark, payload.Status)
	if err != nil {
		writeDBStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleCreateDBAccount(w http.ResponseWriter, r *http.Request, instanceID string) {
	defer r.Body.Close()
	var payload struct {
		Username  string     `json:"username"`
		Password  string     `json:"password"`
		Group     string     `json:"group"`
		Remark    string     `json:"remark"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.AddDatabaseAccount(instanceID, payload.Username, payload.Password, payload.Group, payload.Remark, payload.ExpiresAt)
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
	var payload struct {
		Username  string     `json:"username"`
		Password  string     `json:"password"`
		Group     string     `json:"group"`
		Remark    string     `json:"remark"`
		ExpiresAt *time.Time `json:"expires_at"`
		Status    string     `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.UpdateDatabaseAccount(id, payload.Username, payload.Password, payload.Group, payload.Remark, payload.ExpiresAt, payload.Status)
	if err != nil {
		writeDBStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
