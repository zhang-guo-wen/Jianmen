package admin

import (
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

type databaseAccountResourceView struct {
	store.DatabaseAccountView
	InstanceName    string `json:"instance_name"`
	InstanceAddress string `json:"instance_address"`
}

type databaseInstancePayload struct {
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
type databaseAccountPayload struct {
	Username  string     `json:"username"`
	Password  string     `json:"password"`
	Group     string     `json:"group"`
	Remark    string     `json:"remark"`
	ExpiresAt *time.Time `json:"expires_at"`
	Status    string     `json:"status"`
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
	accounts, err := s.databaseManagement.ListAccounts(r.Context(), userIDFromRequest(r), "", connectableOnly(r))
	if err != nil {
		s.writeDatabaseManagementError(w, r, err)
		return
	}
	instances, err := s.databaseManagement.ListInstances(r.Context(), userIDFromRequest(r), false)
	if err != nil {
		s.writeDatabaseManagementError(w, r, err)
		return
	}
	byID := make(map[string]store.DatabaseInstanceView, len(instances))
	for _, instance := range instances {
		byID[instance.ID] = databaseInstanceToStore(instance)
	}
	resources := make([]databaseAccountResourceView, 0, len(accounts))
	for _, account := range accounts {
		instance := byID[account.InstanceID]
		resources = append(resources, databaseAccountResourceView{DatabaseAccountView: databaseAccountToStore(account), InstanceName: instance.Name, InstanceAddress: net.JoinHostPort(instance.Address, strconv.Itoa(instance.Port))})
	}
	s.writeJSON(w, r, http.StatusOK, paginateSlice(resources, r, func(v databaseAccountResourceView, q string) bool {
		return strings.Contains(strings.ToLower(v.UniqueName), q) || strings.Contains(strings.ToLower(v.Username), q) || strings.Contains(strings.ToLower(v.Group), q) || strings.Contains(strings.ToLower(v.InstanceName), q) || strings.Contains(strings.ToLower(v.InstanceAddress), q)
	}))
}

func (s *Server) handleDBInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requireAuthenticatedUser(w, r) {
			return
		}
		instances, err := s.databaseManagement.ListInstances(r.Context(), userIDFromRequest(r), connectableOnly(r))
		if err != nil {
			s.writeDatabaseManagementError(w, r, err)
			return
		}
		views := make([]store.DatabaseInstanceView, len(instances))
		for i := range instances {
			views[i] = databaseInstanceToStore(instances[i])
		}
		views = filterByGroup(views, r, func(view store.DatabaseInstanceView) string {
			return view.Group
		})
		s.writeJSON(w, r, http.StatusOK, paginateSlice(views, r, func(v store.DatabaseInstanceView, q string) bool {
			return strings.Contains(strings.ToLower(v.Name), q) || strings.Contains(strings.ToLower(v.Address), q) || strings.Contains(strings.ToLower(v.Protocol), q) || strings.Contains(strings.ToLower(v.Group), q) || strings.Contains(strings.ToLower(v.Remark), q)
		}))
	case http.MethodPost:
		s.handleCreateDBInstance(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func decodeDatabaseInstancePayload(w http.ResponseWriter, r *http.Request) (databaseInstancePayload, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload databaseInstancePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return databaseInstancePayload{}, false
	}
	return payload, true
}
func databaseInstanceInput(payload databaseInstancePayload) service.DatabaseInstanceInput {
	return service.DatabaseInstanceInput{Name: payload.Name, Protocol: payload.Protocol, Address: payload.Address, Port: payload.Port, TLSMode: payload.TLSMode, TLSServerName: payload.TLSServerName, TLSCAPEM: payload.TLSCAPEM, ClearTLSCA: payload.ClearTLSCA, Group: payload.Group, Remark: payload.Remark, Status: payload.Status}
}

func (s *Server) handleCreateDBInstance(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeDatabaseInstancePayload(w, r)
	if !ok {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database request")
		return
	}
	view, err := s.databaseManagement.CreateInstance(r.Context(), userIDFromRequest(r), isSuperAdminRequest(r), databaseInstanceInput(payload))
	if err != nil {
		s.writeDatabaseManagementError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, databaseInstanceToStore(view))
}

func (s *Server) handleDBInstance(w http.ResponseWriter, r *http.Request) {
	id, child, ok := dbInstancePathParts(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if child == "accounts" {
		s.handleDBInstanceAccounts(w, r, id)
		return
	}
	if child == "databases" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
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
		view, err := s.databaseManagement.GetInstance(r.Context(), userIDFromRequest(r), id)
		if err != nil {
			s.writeDatabaseManagementError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, databaseInstanceToStore(view))
	case http.MethodPut:
		s.handleUpdateDBInstance(w, r, id)
	case http.MethodDelete:
		if err := s.databaseManagement.DeleteInstance(r.Context(), userIDFromRequest(r), id); err != nil {
			s.writeDatabaseManagementError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDBInstanceAccounts(w http.ResponseWriter, r *http.Request, instanceID string) {
	switch r.Method {
	case http.MethodGet:
		if !s.requireAuthenticatedUser(w, r) {
			return
		}
		accounts, err := s.databaseManagement.ListAccounts(r.Context(), userIDFromRequest(r), instanceID, connectableOnly(r))
		if err != nil {
			s.writeDatabaseManagementError(w, r, err)
			return
		}
		views := make([]store.DatabaseAccountView, len(accounts))
		for i := range accounts {
			views[i] = databaseAccountToStore(accounts[i])
		}
		s.writeJSON(w, r, http.StatusOK, paginateSlice(views, r, func(v store.DatabaseAccountView, q string) bool {
			return strings.Contains(strings.ToLower(v.UniqueName), q) || strings.Contains(strings.ToLower(v.Username), q) || strings.Contains(strings.ToLower(v.Group), q) || strings.Contains(strings.ToLower(v.Remark), q)
		}))
	case http.MethodPost:
		s.handleCreateDBAccount(w, r, instanceID)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateDBInstance(w http.ResponseWriter, r *http.Request, id string) {
	payload, ok := decodeDatabaseInstancePayload(w, r)
	if !ok {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database request")
		return
	}
	view, err := s.databaseManagement.UpdateInstance(r.Context(), userIDFromRequest(r), id, databaseInstanceInput(payload))
	if err != nil {
		s.writeDatabaseManagementError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, databaseInstanceToStore(view))
}
func (s *Server) handleCreateDBAccount(w http.ResponseWriter, r *http.Request, instanceID string) {
	defer r.Body.Close()
	var payload databaseAccountPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database request")
		return
	}
	view, err := s.databaseManagement.CreateAccount(r.Context(), userIDFromRequest(r), instanceID, payload.Username, payload.Password, payload.Group, payload.Remark, payload.ExpiresAt)
	if err != nil {
		s.writeDatabaseManagementError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, databaseAccountToStore(view))
}

func (s *Server) handleDBAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := dbAccountIDFromPath(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		view, err := s.databaseManagement.GetAccount(r.Context(), userIDFromRequest(r), id)
		if err != nil {
			s.writeDatabaseManagementError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, databaseAccountToStore(view))
	case http.MethodPut:
		s.handleUpdateDBAccount(w, r, id)
	case http.MethodDelete:
		if err := s.databaseManagement.DeleteAccount(r.Context(), userIDFromRequest(r), id); err != nil {
			s.writeDatabaseManagementError(w, r, err)
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
	var payload databaseAccountPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database request")
		return
	}
	view, err := s.databaseManagement.UpdateAccount(r.Context(), userIDFromRequest(r), id, payload.Username, payload.Password, payload.Group, payload.Remark, payload.ExpiresAt, payload.Status)
	if err != nil {
		s.writeDatabaseManagementError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, databaseAccountToStore(view))
}

func (s *Server) writeDatabaseManagementError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrDatabaseManagementForbidden):
		s.forbidden(w, r)
	case errors.Is(err, service.ErrDatabaseManagementDisabled):
		s.writeErrorText(w, r, http.StatusForbidden, "database resource is disabled")
	case errors.Is(err, service.ErrDatabaseManagementExpired):
		s.writeErrorText(w, r, http.StatusForbidden, "account has expired")
	case errors.Is(err, service.ErrDatabaseManagementInvalid):
		s.writeErrorText(w, r, http.StatusBadRequest, strings.TrimPrefix(err.Error(), service.ErrDatabaseManagementInvalid.Error()+": "))
	case errors.Is(err, store.ErrDBInstanceNotFound), errors.Is(err, store.ErrDBAccountNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, "database resource not found")
	default:
		s.writeDatabaseOperationError(w, r, http.StatusInternalServerError, "database operation failed", err)
	}
}
