package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"jianmen/internal/pkg/apiresp"
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
		page, err := s.containerManagement.List(r.Context(), containerActor(r), service.ContainerListRequest{Page: positiveIntRequestQuery(r, "page", 1), PageSize: positiveIntRequestQuery(r, "page_size", defaultPageSize), Query: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status")})
		if err != nil {
			s.writeContainerServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, page)
	case http.MethodPost:
		payload, ok := s.decodeContainerEndpointPayload(w, r)
		if !ok {
			return
		}
		view, err := s.containerManagement.Create(r.Context(), containerActor(r), containerEndpointRequest(payload))
		if err != nil {
			s.writeContainerServiceError(w, r, err)
			return
		}
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
		view, err := s.containerManagement.Get(r.Context(), containerActor(r), id)
		if err != nil {
			s.writeContainerServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		payload, ok := s.decodeContainerEndpointPayload(w, r)
		if !ok {
			return
		}
		view, err := s.containerManagement.Update(r.Context(), containerActor(r), id, containerEndpointRequest(payload))
		if err != nil {
			s.writeContainerServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodDelete:
		if err := s.containerManagement.Delete(r.Context(), containerActor(r), id); err != nil {
			s.writeContainerServiceError(w, r, err)
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
	payload, ok := s.decodeContainerEndpointPayload(w, r)
	if !ok {
		return
	}
	result, err := s.containerManagement.Test(r.Context(), containerActor(r), containerEndpointRequest(payload))
	if err != nil {
		s.writeContainerServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, result)
}

func (s *Server) handleContainerRuntime(w http.ResponseWriter, r *http.Request, endpointID, containerID string) {
	if containerID == "" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		items, err := s.containerManagement.ListRuntime(r.Context(), containerActor(r), endpointID)
		if err != nil {
			s.writeContainerServiceError(w, r, err)
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
	logs, err := s.containerManagement.Logs(r.Context(), containerActor(r), endpointID, containerID, tail)
	if err != nil {
		s.writeContainerServiceError(w, r, err)
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

func containerEndpointRequest(payload containerEndpointPayload) service.ContainerEndpointRequest {
	return service.ContainerEndpointRequest{ID: payload.ID, Name: payload.Name, Group: payload.Group, Runtime: payload.Runtime, ConnectionMode: payload.ConnectionMode, Address: payload.Address, Port: payload.Port, HostID: payload.HostID, HostAccountID: payload.HostAccountID, Remark: payload.Remark, Status: payload.Status}
}
func containerActor(r *http.Request) service.ContainerActor {
	return service.ContainerActor{UserID: userIDFromRequest(r), SuperAdmin: isSuperAdminRequest(r)}
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

func (s *Server) writeContainerServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if s.writeSSHHostIdentityError(w, r, err) {
		return
	}
	switch {
	case errors.Is(err, service.ErrContainerForbidden):
		s.forbidden(w, r)
	case errors.Is(err, store.ErrContainerEndpointNotFound), errors.Is(err, store.ErrTargetNotFound):
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
	case errors.Is(err, service.ErrContainerUnavailable):
		s.writeErrorText(w, r, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrContainerRuntime):
		s.writeErrorText(w, r, http.StatusBadGateway, err.Error())
	case errors.Is(err, service.ErrInvalidContainer):
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
	default:
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
	}
}
