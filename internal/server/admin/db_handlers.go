package admin

import (
	"encoding/json"
	"jianmen/internal/model"
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
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionDBProxyView) {
		s.forbidden(w, r)
		return
	}
	cfg := s.cfg.DatabaseGateway
	host, port := parseListenAddr(cfg.ListenAddr)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
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

type databaseAccountResourceView struct {
	store.DatabaseAccountView
	InstanceName    string `json:"instance_name"`
	InstanceAddress string `json:"instance_address"`
}

func (s *Server) handleDBAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionDBProxyView) {
		s.forbidden(w, r)
		return
	}
	accounts, err := s.store.DatabaseAccounts()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	accounts, err = s.visibleDatabaseAccounts(r, accounts)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	instances := make(map[string]store.DatabaseInstanceView)
	for _, instance := range s.store.DatabaseInstances() {
		instances[instance.ID] = instance
	}
	resources := make([]databaseAccountResourceView, 0, len(accounts))
	for _, account := range accounts {
		instance := instances[account.InstanceID]
		resources = append(resources, databaseAccountResourceView{
			DatabaseAccountView: account,
			InstanceName:        instance.Name,
			InstanceAddress:     net.JoinHostPort(instance.Address, strconv.Itoa(instance.Port)),
		})
	}
	resp := paginateSlice(resources, r, func(v databaseAccountResourceView, q string) bool {
		return strings.Contains(strings.ToLower(v.UniqueName), q) ||
			strings.Contains(strings.ToLower(v.Username), q) ||
			strings.Contains(strings.ToLower(v.Group), q) ||
			strings.Contains(strings.ToLower(v.InstanceName), q) ||
			strings.Contains(strings.ToLower(v.InstanceAddress), q)
	})
	s.writeJSON(w, r, http.StatusOK, resp)
}

func (s *Server) handleDBInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionDBProxyView) {
			s.forbidden(w, r)
			return
		}
		instances, err := s.visibleDatabaseInstances(r, s.store.DatabaseInstances())
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		resp := paginateSlice(instances, r, func(v store.DatabaseInstanceView, q string) bool {
			return strings.Contains(strings.ToLower(v.Name), q) ||
				strings.Contains(strings.ToLower(v.Address), q) ||
				strings.Contains(strings.ToLower(v.Protocol), q) ||
				strings.Contains(strings.ToLower(v.Group), q) ||
				strings.Contains(strings.ToLower(v.Remark), q)
		})
		s.writeJSON(w, r, http.StatusOK, resp)
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionDBProxyCreate) {
			s.forbidden(w, r)
			return
		}
		s.handleCreateDBInstance(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
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
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.store.AddDatabaseInstance(payload.Name, payload.Protocol, payload.Address, payload.Port, payload.Group, payload.Remark)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.grantCreatedResource(r, model.ResourceTypeDatabaseInstance, view.ID); err != nil {
		_ = s.store.DeleteDatabaseInstance(view.ID)
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleDBInstance(w http.ResponseWriter, r *http.Request) {
	id, child, ok := dbInstancePathParts(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if child == "accounts" {
		switch r.Method {
		case http.MethodGet:
			if !s.requirePermission(r, rbac.ActionDBProxyView) {
				s.forbidden(w, r)
				return
			}
			accounts, err := s.store.InstanceAccounts(id)
			if err != nil {
				writeDBStoreError(w, r, err)
				return
			}
			accounts, err = s.visibleDatabaseAccounts(r, accounts)
			if err == nil {
				accounts, err = s.connectableDatabaseAccounts(r, accounts)
			}
			if err != nil {
				s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
				return
			}

			resp := paginateSlice(accounts, r, func(v store.DatabaseAccountView, q string) bool {
				return strings.Contains(strings.ToLower(v.UniqueName), q) ||
					strings.Contains(strings.ToLower(v.Username), q) ||
					strings.Contains(strings.ToLower(v.Group), q) ||
					strings.Contains(strings.ToLower(v.Remark), q)
			})
			s.writeJSON(w, r, http.StatusOK, resp)
		case http.MethodPost:
			if !s.requirePermission(r, rbac.ActionDBProxyCreate) {
				s.forbidden(w, r)
				return
			}
			if !s.requireResourceGrant(w, r, model.ResourceTypeDatabaseInstance, id) {
				return
			}
			s.handleCreateDBAccount(w, r, id)
		default:
			w.Header().Set("Allow", "GET, POST")
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if child == "databases" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireResourceGrant(w, r, model.ResourceTypeDatabaseInstance, id) {
			return
		}
		s.handleDBDatabases(w, r, id)
		return
	}
	if child == "provision-account" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireResourceGrant(w, r, model.ResourceTypeDatabaseInstance, id) {
			return
		}
		s.handleDBProvisionAccount(w, r, id)
		return
	}
	if child != "" {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionDBProxyView) {
			s.forbidden(w, r)
			return
		}
		visible, err := s.databaseInstanceVisible(r, id)
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if !visible {
			s.forbidden(w, r)
			return
		}
		view, err := s.store.DatabaseInstance(id)
		if err != nil {
			writeDBStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requirePermission(r, rbac.ActionDBProxyUpdate) {
			s.forbidden(w, r)
			return
		}
		if !s.requireResourceGrant(w, r, model.ResourceTypeDatabaseInstance, id) {
			return
		}
		s.handleUpdateDBInstance(w, r, id)
	case http.MethodDelete:
		if !s.requirePermission(r, rbac.ActionDBProxyDelete) {
			s.forbidden(w, r)
			return
		}
		if !s.requireResourceGrant(w, r, model.ResourceTypeDatabaseInstance, id) {
			return
		}
		if err := s.store.DeleteDatabaseInstance(id); err != nil {
			writeDBStoreError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
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
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.store.UpdateDatabaseInstance(id, payload.Name, payload.Protocol, payload.Address, payload.Port, payload.Group, payload.Remark, payload.Status)
	if err != nil {
		writeDBStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
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
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(payload.Password) == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "password is required")
		return
	}
	view, err := s.store.AddDatabaseAccount(instanceID, payload.Username, payload.Password, payload.Group, payload.Remark, payload.ExpiresAt)
	if err != nil {
		writeDBStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

// -- db accounts (single-account CRUD) --

func (s *Server) handleDBAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := dbAccountIDFromPath(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionDBProxyView) {
			s.forbidden(w, r)
			return
		}
		if !s.requireDatabaseAccountManagement(w, r, id) {
			return
		}
		view, err := s.store.DatabaseAccount(id)
		if err != nil {
			writeDBStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requirePermission(r, rbac.ActionDBProxyUpdate) {
			s.forbidden(w, r)
			return
		}
		if !s.requireDatabaseAccountManagement(w, r, id) {
			return
		}
		s.handleUpdateDBAccount(w, r, id)
	case http.MethodDelete:
		if !s.requirePermission(r, rbac.ActionDBProxyDelete) {
			s.forbidden(w, r)
			return
		}
		if !s.requireResourceGrant(w, r, model.ResourceTypeDatabaseAccount, id) {
			return
		}
		if err := s.store.DeleteDatabaseAccount(id); err != nil {
			writeDBStoreError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
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
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.store.UpdateDatabaseAccount(id, payload.Username, payload.Password, payload.Group, payload.Remark, payload.ExpiresAt, payload.Status)
	if err != nil {
		writeDBStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}
