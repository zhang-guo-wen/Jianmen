package admin

import (
	"context"
	"encoding/json"
	"errors"
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
		if !s.requirePermission(r, rbac.ActionContainerView) {
			s.forbidden(w, r)
			return
		}
		items := s.store.ContainerEndpoints()
		canManage := s.isSuperAdmin(userIDFromRequest(r)) || s.requirePermission(r, rbac.ActionContainerUpdate) || s.requirePermission(r, rbac.ActionContainerDelete)
		for i := range items {
			items[i].CanManage = canManage
		}
		page := paginateSlice(items, r, func(item store.ContainerEndpointView, q string) bool {
			return strings.Contains(strings.ToLower(item.Name), q) ||
				strings.Contains(strings.ToLower(item.Runtime), q) ||
				strings.Contains(strings.ToLower(item.Address), q) ||
				strings.Contains(strings.ToLower(item.Group), q)
		})
		s.writeJSON(w, r, http.StatusOK, page)
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionContainerCreate) {
			s.forbidden(w, r)
			return
		}
		payload, ok := s.decodeContainerEndpointPayload(w, r)
		if !ok {
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
		if !s.requirePermission(r, rbac.ActionContainerView) {
			s.forbidden(w, r)
			return
		}
		view, err := s.store.ContainerEndpoint(id)
		if err != nil {
			writeContainerStoreError(w, r, err)
			return
		}
		view.CanManage = s.isSuperAdmin(userIDFromRequest(r)) || s.requirePermission(r, rbac.ActionContainerUpdate) || s.requirePermission(r, rbac.ActionContainerDelete)
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requirePermission(r, rbac.ActionContainerUpdate) {
			s.forbidden(w, r)
			return
		}
		payload, ok := s.decodeContainerEndpointPayload(w, r)
		if !ok {
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
		if !s.requirePermission(r, rbac.ActionContainerDelete) {
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
	if !s.requirePermission(r, rbac.ActionContainerConnect) {
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
		sshConfig, err := store.ClientConfigForTarget(target)
		if err != nil {
			return service.ContainerEndpointConfig{}, err
		}
		config.SSHAddress = target.Addr()
		config.SSHConfig = sshConfig
		config.Unavailable = target.Disabled || target.Expired(time.Now().UTC())
	}
	return config, nil
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
