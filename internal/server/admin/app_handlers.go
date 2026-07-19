package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

type applicationPayload struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	ListenPort int    `json:"listen_port"`
	Group      string `json:"group"`
	Remark     string `json:"remark"`
	Status     string `json:"status"`
}

func (s *Server) handleApplications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requireAuthenticatedUser(w, r) {
			return
		}
		apps, err := s.visibleApplications(r, s.applications.Applications(r.Context()))
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		resp := paginateSlice(apps, r, func(v store.ApplicationView, q string) bool {
			return strings.Contains(strings.ToLower(v.Name), q) ||
				strings.Contains(strings.ToLower(v.Address), q) ||
				strings.Contains(strings.ToLower(v.AppGroup), q) ||
				strings.Contains(strings.ToLower(v.Remark), q)
		})
		s.writeJSON(w, r, http.StatusOK, resp)
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionAppCreate) {
			s.forbidden(w, r)
			return
		}
		s.handleCreateApplication(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateApplication(w http.ResponseWriter, r *http.Request) {
	payload, ok := s.decodeApplicationPayload(w, r)
	if !ok {
		return
	}
	if payload.ListenPort == 0 {
		listenPort, err := s.nextApplicationListenPort(r.Context())
		if err != nil {
			s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
			return
		}
		payload.ListenPort = listenPort
	}
	input, err := applicationInput(payload)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.applications.AddApplication(r.Context(), input)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.grantCreatedResource(r, model.ResourceTypeApplication, view.ID); err != nil {
		_ = s.applications.DeleteApplication(r.Context(), view.ID)
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if s.appProxy != nil && view.Status == "active" {
		if err := s.appProxy.AddProxy(applicationModel(view)); err != nil {
			s.logger.Warn("failed to start app proxy", "name", view.Name, "error", err)
		}
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleApplication(w http.ResponseWriter, r *http.Request) {
	id, child, ok := appPathParts(r.URL.Path)
	if !ok || child != "" {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requireResourceAction(w, r, rbac.ActionAppView, model.ResourceTypeApplication, id) {
			return
		}
		view, err := s.applications.Application(r.Context(), id)
		if err != nil {
			writeApplicationStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requireResourceAction(w, r, rbac.ActionAppUpdate, model.ResourceTypeApplication, id) {
			return
		}
		s.handleUpdateApplication(w, r, id)
	case http.MethodDelete:
		if !s.requireResourceAction(w, r, rbac.ActionAppDelete, model.ResourceTypeApplication, id) {
			return
		}
		view, err := s.applications.Application(r.Context(), id)
		if err != nil {
			writeApplicationStoreError(w, r, err)
			return
		}
		if err := s.applications.DeleteApplication(r.Context(), id); err != nil {
			writeApplicationStoreError(w, r, err)
			return
		}
		if s.appProxy != nil {
			s.appProxy.RemoveProxy(view.ListenPort)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateApplication(w http.ResponseWriter, r *http.Request, id string) {
	previous, err := s.applications.Application(r.Context(), id)
	if err != nil {
		writeApplicationStoreError(w, r, err)
		return
	}
	payload, ok := s.decodeApplicationPayload(w, r)
	if !ok {
		return
	}
	if payload.ListenPort == 0 {
		payload.ListenPort = previous.ListenPort
	}
	input, err := applicationInput(payload)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.applications.UpdateApplication(r.Context(), id, input)
	if err != nil {
		writeApplicationStoreError(w, r, err)
		return
	}
	if s.appProxy != nil {
		if view.Status == "active" {
			if err := s.appProxy.UpdateProxy(previous.ListenPort, applicationModel(view)); err != nil {
				s.logger.Warn("failed to update app proxy", "name", view.Name, "error", err)
			}
		} else {
			s.appProxy.RemoveProxy(previous.ListenPort)
		}
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) decodeApplicationPayload(w http.ResponseWriter, r *http.Request) (applicationPayload, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload applicationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return applicationPayload{}, false
	}
	return payload, true
}

func applicationInput(payload applicationPayload) (store.ApplicationInput, error) {
	parsed, err := service.ParseApplicationAddress(payload.Address)
	if err != nil {
		return store.ApplicationInput{}, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		name = parsed.Host
	}
	return store.ApplicationInput{
		Name:           name,
		Address:        parsed.Address,
		EntryPath:      parsed.EntryPath,
		InternalScheme: parsed.Scheme,
		InternalHost:   parsed.Host,
		InternalPort:   parsed.Port,
		ListenPort:     payload.ListenPort,
		AppGroup:       strings.TrimSpace(payload.Group),
		Remark:         strings.TrimSpace(payload.Remark),
		Status:         strings.TrimSpace(payload.Status),
	}, nil
}

func applicationModel(view store.ApplicationView) model.Application {
	return model.Application{
		ID:             view.ID,
		Name:           view.Name,
		Address:        view.Address,
		EntryPath:      view.EntryPath,
		ListenPort:     view.ListenPort,
		InternalScheme: view.InternalScheme,
		InternalHost:   view.InternalHost,
		InternalPort:   view.InternalPort,
		Status:         view.Status,
	}
}

func (s *Server) nextApplicationListenPort(ctx context.Context) (int, error) {
	start := s.cfg.ApplicationGateway.PortStart
	end := s.cfg.ApplicationGateway.PortEnd
	if start <= 0 || end < start {
		start, end = 47110, 47199
	}
	used := make(map[int]struct{})
	for _, app := range s.applications.Applications(ctx) {
		used[app.ListenPort] = struct{}{}
	}
	for port := start; port <= end; port++ {
		if _, exists := used[port]; !exists {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available application proxy port in range %d-%d", start, end)
}
