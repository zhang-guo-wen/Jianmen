package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

type containerEndpointPayload struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Group          string `json:"group"`
	Runtime        string `json:"runtime"`
	ConnectionMode string `json:"connection_mode"`
	Address        string `json:"address"`
	Port           int    `json:"port"`
	HostID         string `json:"host_id"`
	HostAccountID  string `json:"host_account_id"`
	Remark         string `json:"remark"`
	Status         string `json:"status"`
}

func (s *Server) handleContainerEndpoints(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requireAnyPermission(r, rbac.ActionContainerView, rbac.ActionContainerConnect) {
			s.forbidden(w, r)
			return
		}
		pageNumber := positiveIntRequestQuery(r, "page", 1)
		pageSize := positiveIntRequestQuery(r, "page_size", defaultPageSize)
		if pageSize > 200 {
			pageSize = 200
		}
		items, err := s.listContainerEndpoints(r)
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		authorized := make([]store.ContainerEndpointView, 0, len(items))
		authorizationCache := make(map[string]bool)
		for _, item := range items {
			key := item.ID + "\x00view-connect"
			allowed, ok := authorizationCache[key]
			if !ok {
				allowed = s.authorizeContainerEndpoint(r, []string{rbac.ActionContainerView, rbac.ActionContainerConnect}, item.ID)
				authorizationCache[key] = allowed
			}
			if !allowed {
				continue
			}
			item.CanManage = s.authorizeContainerEndpoint(r, []string{rbac.ActionContainerUpdate}, item.ID) ||
				s.authorizeContainerEndpoint(r, []string{rbac.ActionContainerDelete}, item.ID)
			authorized = append(authorized, item)
		}
		start := (pageNumber - 1) * pageSize
		if start > len(authorized) {
			start = len(authorized)
		}
		end := start + pageSize
		if end > len(authorized) {
			end = len(authorized)
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: authorized[start:end], Total: len(authorized), Page: pageNumber, PageSize: pageSize})
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionContainerCreate) {
			s.forbidden(w, r)
			return
		}
		payload, ok := s.decodeContainerEndpointPayload(w, r)
		if !ok {
			return
		}
		if !s.requireContainerHostAccount(w, r, payload.HostID, payload.HostAccountID) {
			return
		}
		view, err := s.store.AddContainerEndpoint(containerEndpointInput(payload))
		if err != nil {
			writeContainerStoreError(w, r, err)
			return
		}
		view.CanManage = true
		s.writeJSON(w, r, http.StatusCreated, view)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleContainerEndpoint(w http.ResponseWriter, r *http.Request) {
	id, child, containerID, ok := containerEndpointPathParts(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if child == "containers" {
		s.handleContainerRuntime(w, r, id, containerID)
		return
	}
	if child != "" {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !s.authorizeContainerEndpoint(r, []string{rbac.ActionContainerView}, id) {
			s.forbidden(w, r)
			return
		}
		view, err := s.store.ContainerEndpoint(id)
		if err != nil {
			writeContainerStoreError(w, r, err)
			return
		}
		view.CanManage = s.authorizeContainerEndpoint(r, []string{rbac.ActionContainerUpdate}, id) ||
			s.authorizeContainerEndpoint(r, []string{rbac.ActionContainerDelete}, id)
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.authorizeContainerEndpoint(r, []string{rbac.ActionContainerUpdate}, id) {
			s.forbidden(w, r)
			return
		}
		payload, ok := s.decodeContainerEndpointPayload(w, r)
		if !ok {
			return
		}
		if !s.requireContainerHostAccount(w, r, payload.HostID, payload.HostAccountID) {
			return
		}
		view, err := s.store.UpdateContainerEndpoint(id, containerEndpointInput(payload))
		if err != nil {
			writeContainerStoreError(w, r, err)
			return
		}
		view.CanManage = true
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodDelete:
		if !s.authorizeContainerEndpoint(r, []string{rbac.ActionContainerDelete}, id) {
			s.forbidden(w, r)
			return
		}
		if err := s.store.DeleteContainerEndpoint(id); err != nil {
			writeContainerStoreError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleContainerConnectionTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionContainerCreate) && !s.requirePermission(r, rbac.ActionContainerUpdate) {
		s.forbidden(w, r)
		return
	}
	payload, ok := s.decodeContainerEndpointPayload(w, r)
	if !ok {
		return
	}
	if !s.requireContainerHostAccount(w, r, payload.HostID, payload.HostAccountID) {
		return
	}
	config, err := s.containerServiceConfig(containerEndpointInput(payload))
	if err != nil {
		writeContainerStoreError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, _ := s.containerService.Test(ctx, config)
	s.writeJSON(w, r, http.StatusOK, result)
}

func (s *Server) handleContainerRuntime(w http.ResponseWriter, r *http.Request, endpointID, containerID string) {
	if !s.authorizeContainerEndpoint(r, []string{rbac.ActionContainerConnect}, endpointID) {
		s.forbidden(w, r)
		return
	}
	view, err := s.store.ContainerEndpoint(endpointID)
	if err != nil {
		writeContainerStoreError(w, r, err)
		return
	}
	if view.Status != "active" {
		s.writeErrorText(w, r, http.StatusConflict, "container endpoint is disabled")
		return
	}
	config, err := s.containerServiceConfig(store.ContainerEndpointInput{
		Name: view.Name, Runtime: view.Runtime, ConnectionMode: view.ConnectionMode,
		Address: view.Address, Port: view.Port, HostID: view.HostID, HostAccountID: view.HostAccountID,
	})
	if err != nil {
		writeContainerStoreError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if containerID == "" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		items, err := s.containerService.List(ctx, config)
		if err != nil {
			s.writeErrorText(w, r, http.StatusBadGateway, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
		return
	}
	if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/logs") {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	logs, err := s.containerService.Logs(ctx, config, containerID, tail)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadGateway, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"logs": logs})
}

func (s *Server) listContainerEndpoints(r *http.Request) ([]store.ContainerEndpointView, error) {
	const fetchPageSize = 200
	params := store.ContainerEndpointListParams{
		Page: 1, Size: fetchPageSize, Query: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status"),
	}
	items := make([]store.ContainerEndpointView, 0)
	for {
		pageItems, total, err := s.store.ListContainerEndpoints(r.Context(), params)
		if err != nil {
			return nil, err
		}
		items = append(items, pageItems...)
		if len(items) >= int(total) || len(pageItems) == 0 {
			return items, nil
		}
		params.Page++
	}
}

func (s *Server) authorizeContainerEndpoint(r *http.Request, actions []string, endpointID string) bool {
	allowed, err := s.authorizeAnyConnection(
		r.Context(), userIDFromRequest(r), actions, model.ResourceTypeContainerEndpoint, endpointID,
	)
	if err != nil {
		s.logger.Warn("container endpoint authorization failed", "endpoint_id", endpointID, "actions", actions, "error", err)
		return false
	}
	return allowed
}

func (s *Server) decodeContainerEndpointPayload(w http.ResponseWriter, r *http.Request) (containerEndpointPayload, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload containerEndpointPayload
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return containerEndpointPayload{}, false
	}
	return payload, true
}

func containerEndpointInput(payload containerEndpointPayload) store.ContainerEndpointInput {
	return store.ContainerEndpointInput{
		ID: payload.ID, Name: payload.Name, Group: payload.Group, Runtime: payload.Runtime,
		ConnectionMode: payload.ConnectionMode, Address: payload.Address, Port: payload.Port,
		HostID: payload.HostID, HostAccountID: payload.HostAccountID, Remark: payload.Remark, Status: payload.Status,
	}
}

func (s *Server) containerServiceConfig(input store.ContainerEndpointInput) (service.ContainerEndpointConfig, error) {
	config := service.ContainerEndpointConfig{
		Runtime: input.Runtime, ConnectionMode: input.ConnectionMode, Address: input.Address, Port: input.Port,
	}
	if input.ConnectionMode == model.ContainerConnectionSSH || input.ConnectionMode == model.ContainerConnectionContainerd {
		target, err := s.store.TargetConfig(input.HostAccountID)
		if err != nil {
			return service.ContainerEndpointConfig{}, err
		}
		if strings.TrimSpace(target.HostID) != strings.TrimSpace(input.HostID) {
			return service.ContainerEndpointConfig{}, fmt.Errorf("host account %q does not belong to host %q", input.HostAccountID, input.HostID)
		}
		sshConfig, err := store.ClientConfigForTarget(target)
		if err != nil {
			return service.ContainerEndpointConfig{}, err
		}
		config.SSHAddress = target.Addr()
		config.SSHConfig = sshConfig
		config.SSHCacheKey = target.ID + "@" + target.Addr()
		config.Unavailable = target.Disabled || target.Expired(time.Now().UTC())
	}
	return config, nil
}

// requireContainerHostAccount validates the host/account relationship and
// requires session:connect on the concrete host account. Container SSH and
// containerd connections execute commands through that account, so the
// account-level session action is the least privilege needed to use it.
func (s *Server) requireContainerHostAccount(w http.ResponseWriter, r *http.Request, hostID, hostAccountID string) bool {
	hostID = strings.TrimSpace(hostID)
	hostAccountID = strings.TrimSpace(hostAccountID)
	if hostID == "" && hostAccountID == "" {
		return true
	}
	if hostID == "" || hostAccountID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "host_id and host_account_id must be provided together")
		return false
	}
	if _, err := s.store.Host(hostID); err != nil {
		writeContainerStoreError(w, r, err)
		return false
	}
	target, err := s.store.TargetConfig(hostAccountID)
	if err != nil {
		writeContainerStoreError(w, r, err)
		return false
	}
	if strings.TrimSpace(target.HostID) != hostID {
		s.writeErrorText(w, r, http.StatusBadRequest, fmt.Sprintf("host account %q does not belong to host %q", hostAccountID, hostID))
		return false
	}
	allowed, err := s.authorizeConnection(r.Context(), userIDFromRequest(r), rbac.ActionSessionConnect, model.ResourceTypeHostAccount, hostAccountID)
	if err != nil {
		s.logger.Warn("container host account authorization failed", "host_account_id", hostAccountID, "error", err)
		s.forbidden(w, r)
		return false
	}
	if !allowed {
		s.forbidden(w, r)
		return false
	}
	return true
}

func containerEndpointPathParts(path string) (endpointID, child, containerID string, ok bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/containers/endpoints/"), "/")
	if trimmed == "" {
		return "", "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		return parts[0], "", "", true
	}
	if len(parts) == 2 && parts[1] == "containers" {
		return parts[0], "containers", "", true
	}
	if len(parts) == 4 && parts[1] == "containers" && parts[3] == "logs" {
		return parts[0], "containers", parts[2], true
	}
	return "", "", "", false
}

func writeContainerStoreError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadRequest
	code := apiresp.CodeValidation
	if errors.Is(err, store.ErrContainerEndpointNotFound) || errors.Is(err, store.ErrTargetNotFound) {
		status = http.StatusNotFound
		code = apiresp.CodeNotFound
	}
	apiresp.WriteError(w, status, code, err.Error(), nil, apiresp.RequestID(r.Context()))
}
