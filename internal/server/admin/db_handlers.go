package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

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
	if !s.requireAuthenticatedUser(w, r) {
		return
	}
	actions := []string{rbac.ActionDBProxyView}
	if connectableOnly(r) {
		if !s.requireAnyPermission(r, rbac.ActionDBConnect) {
			s.forbidden(w, r)
			return
		}
		actions = []string{rbac.ActionDBConnect}
	}
	accounts, err := s.databases.DatabaseAccounts(r.Context())
	if err != nil {
		s.writeDatabaseOperationError(w, r, http.StatusInternalServerError, "database operation failed", err)
		return
	}
	accounts, err = s.visibleDatabaseAccountsForActions(r, accounts, actions)
	if err != nil {
		s.writeDatabaseOperationError(w, r, http.StatusInternalServerError, "database operation failed", err)
		return
	}
	instances := make(map[string]store.DatabaseInstanceView)
	for _, instance := range s.databases.DatabaseInstances(r.Context()) {
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
		if !s.requireAuthenticatedUser(w, r) {
			return
		}
		actions := []string{rbac.ActionDBProxyView}
		if connectableOnly(r) {
			if !s.requireAnyPermission(r, rbac.ActionDBConnect) {
				s.forbidden(w, r)
				return
			}
			actions = []string{rbac.ActionDBConnect}
		}
		instances, err := s.visibleDatabaseInstancesForActions(r, s.databases.DatabaseInstances(r.Context()), actions)
		if err != nil {
			s.writeDatabaseOperationError(w, r, http.StatusInternalServerError, "database operation failed", err)
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
		Name          string  `json:"name"`
		Protocol      string  `json:"protocol"`
		Address       string  `json:"address"`
		Port          int     `json:"port"`
		TLSMode       string  `json:"tls_mode"`
		TLSServerName string  `json:"tls_server_name"`
		TLSCAPEM      *string `json:"tls_ca_pem"`
		ClearTLSCA    bool    `json:"clear_tls_ca"`
		Group         string  `json:"group"`
		Remark        string  `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database request")
		return
	}
	view, err := s.databases.AddDatabaseInstance(r.Context(), store.DatabaseInstanceInput{
		Name: payload.Name, Protocol: payload.Protocol, Address: payload.Address, Port: payload.Port,
		TLSMode: payload.TLSMode, TLSServerName: payload.TLSServerName, TLSCAPEM: payload.TLSCAPEM, ClearTLSCA: payload.ClearTLSCA,
		Group: payload.Group, Remark: payload.Remark,
	})
	if err != nil {
		writeDBStoreError(w, r, err)
		return
	}
	if err := s.grantCreatedResource(r, model.ResourceTypeDatabaseInstance, view.ID); err != nil {
		err = s.cleanupCreatedDatabaseInstance(r, view.ID, err)
		s.writeDatabaseOperationError(w, r, http.StatusInternalServerError, "database operation failed", err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) cleanupCreatedDatabaseInstance(r *http.Request, id string, grantErr error) error {
	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancelCleanup()
	if cleanupErr := s.databases.DeleteDatabaseInstance(cleanupCtx, id); cleanupErr != nil {
		return errors.Join(grantErr, cleanupErr)
	}
	return grantErr
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
			if !s.requireAuthenticatedUser(w, r) {
				return
			}
			actions := []string{rbac.ActionDBProxyView}
			if connectableOnly(r) {
				if !s.requireAnyPermission(r, rbac.ActionDBConnect) {
					s.forbidden(w, r)
					return
				}
				actions = []string{rbac.ActionDBConnect}
			}
			accounts, err := s.resourceAccess.ListDatabaseAccountsByInstance(r.Context(), id)
			if err != nil {
				writeDBStoreError(w, r, err)
				return
			}
			accounts, err = s.visibleDatabaseAccountsForActions(r, accounts, actions)
			if err != nil {
				s.writeDatabaseOperationError(w, r, http.StatusInternalServerError, "database operation failed", err)
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
			if !s.requireResourceAction(w, r, rbac.ActionDBProxyCreate, model.ResourceTypeDatabaseInstance, id) {
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
		if !s.requireResourceAction(w, r, rbac.ActionDBProxyView, model.ResourceTypeDatabaseInstance, id) {
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
		if !s.requireResourceAction(w, r, rbac.ActionDBProxyCreate, model.ResourceTypeDatabaseInstance, id) {
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
		visible, err := s.databaseInstanceVisible(r, id)
		if err != nil {
			s.writeDatabaseOperationError(w, r, http.StatusInternalServerError, "database operation failed", err)
			return
		}
		if !visible {
			s.forbidden(w, r)
			return
		}
		view, err := s.databases.DatabaseInstance(r.Context(), id)
		if err != nil {
			writeDBStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requireResourceAction(w, r, rbac.ActionDBProxyUpdate, model.ResourceTypeDatabaseInstance, id) {
			return
		}
		s.handleUpdateDBInstance(w, r, id)
	case http.MethodDelete:
		if !s.requireResourceAction(w, r, rbac.ActionDBProxyDelete, model.ResourceTypeDatabaseInstance, id) {
			return
		}
		if err := s.databases.DeleteDatabaseInstance(r.Context(), id); err != nil {
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
		Name          string  `json:"name"`
		Protocol      string  `json:"protocol"`
		Address       string  `json:"address"`
		Port          int     `json:"port"`
		TLSMode       string  `json:"tls_mode"`
		TLSServerName string  `json:"tls_server_name"`
		TLSCAPEM      *string `json:"tls_ca_pem"`
		ClearTLSCA    bool    `json:"clear_tls_ca"`
		Group         string  `json:"group"`
		Remark        string  `json:"remark"`
		Status        string  `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database request")
		return
	}
	view, err := s.databases.UpdateDatabaseInstance(r.Context(), id, store.DatabaseInstanceInput{
		Name: payload.Name, Protocol: payload.Protocol, Address: payload.Address, Port: payload.Port,
		TLSMode: payload.TLSMode, TLSServerName: payload.TLSServerName, TLSCAPEM: payload.TLSCAPEM, ClearTLSCA: payload.ClearTLSCA,
		Group: payload.Group, Remark: payload.Remark, Status: payload.Status,
	})
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
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database request")
		return
	}
	if strings.TrimSpace(payload.Password) == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "password is required")
		return
	}
	view, err := s.databases.AddDatabaseAccount(r.Context(), instanceID, payload.Username, payload.Password, payload.Group, payload.Remark, payload.ExpiresAt)
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
		if !s.requireResourceAction(w, r, rbac.ActionDBProxyView, model.ResourceTypeDatabaseAccount, id) {
			return
		}
		view, err := s.databases.DatabaseAccount(r.Context(), id)
		if err != nil {
			writeDBStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requireResourceAction(w, r, rbac.ActionDBProxyUpdate, model.ResourceTypeDatabaseAccount, id) {
			return
		}
		s.handleUpdateDBAccount(w, r, id)
	case http.MethodDelete:
		if !s.requireResourceAction(w, r, rbac.ActionDBProxyDelete, model.ResourceTypeDatabaseAccount, id) {
			return
		}
		if err := s.databaseProvisioning.Deprovision(r.Context(), id); err != nil {
			if !errors.Is(err, service.ErrDatabaseAccountNotManaged) {
				s.writeDatabaseDeprovisionServiceError(w, r, err)
				return
			}
			if err := s.databases.DeleteDatabaseAccount(r.Context(), id); err != nil {
				writeDBStoreError(w, r, err)
				return
			}
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
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database request")
		return
	}
	view, err := s.databases.UpdateDatabaseAccount(r.Context(), id, payload.Username, payload.Password, payload.Group, payload.Remark, payload.ExpiresAt, payload.Status)
	if err != nil {
		writeDBStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}
