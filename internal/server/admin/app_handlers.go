package admin

import (
	"encoding/json"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
	"net/http"
	"strings"
)

func (s *Server) handleApplications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionAppView) {
			s.forbidden(w, r)
			return
		}
		apps := s.store.Applications()
		resp := paginateSlice(apps, r, func(v store.ApplicationView, q string) bool {
			return strings.Contains(strings.ToLower(v.Name), q) ||
				strings.Contains(strings.ToLower(v.InternalHost), q) ||
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
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload struct {
		Name       string `json:"name"`
		Scheme     string `json:"scheme"`
		Host       string `json:"host"`
		Port       int    `json:"port"`
		ListenPort int    `json:"listen_port"`
		Group      string `json:"group"`
		Remark     string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.store.AddApplication(payload.Name, payload.Scheme, payload.Host, payload.Port, payload.ListenPort, payload.Group, payload.Remark)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if s.appProxy != nil && view.Status == "active" {
		if err := s.appProxy.AddProxy(model.Application{
			ID:             view.ID,
			Name:           view.Name,
			ListenPort:     view.ListenPort,
			InternalScheme: view.InternalScheme,
			InternalHost:   view.InternalHost,
			InternalPort:   view.InternalPort,
			Status:         view.Status,
		}); err != nil {
			s.logger.Warn("failed to start app proxy", "name", view.Name, "error", err)
		}
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleApplication(w http.ResponseWriter, r *http.Request) {
	id, child, ok := appPathParts(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if child != "" {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionAppView) {
			s.forbidden(w, r)
			return
		}
		view, err := s.store.Application(id)
		if err != nil {
			writeApplicationStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requirePermission(r, rbac.ActionAppUpdate) {
			s.forbidden(w, r)
			return
		}
		s.handleUpdateApplication(w, r, id)
	case http.MethodDelete:
		if !s.requirePermission(r, rbac.ActionAppDelete) {
			s.forbidden(w, r)
			return
		}
		view, err := s.store.Application(id)
		if err != nil {
			writeApplicationStoreError(w, r, err)
			return
		}
		if err := s.store.DeleteApplication(id); err != nil {
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
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload struct {
		Name       string `json:"name"`
		Scheme     string `json:"scheme"`
		Host       string `json:"host"`
		Port       int    `json:"port"`
		ListenPort int    `json:"listen_port"`
		Group      string `json:"group"`
		Remark     string `json:"remark"`
		Status     string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.store.UpdateApplication(id, payload.Name, payload.Scheme, payload.Host, payload.Port, payload.ListenPort, payload.Group, payload.Remark, payload.Status)
	if err != nil {
		writeApplicationStoreError(w, r, err)
		return
	}
	if s.appProxy != nil {
		if view.Status == "active" {
			if err := s.appProxy.UpdateProxy(model.Application{
				ID:             view.ID,
				Name:           view.Name,
				ListenPort:     view.ListenPort,
				InternalScheme: view.InternalScheme,
				InternalHost:   view.InternalHost,
				InternalPort:   view.InternalPort,
				Status:         view.Status,
			}); err != nil {
				s.logger.Warn("failed to update app proxy", "name", view.Name, "error", err)
			}
		} else {
			s.appProxy.RemoveProxy(view.ListenPort)
		}
	}
	s.writeJSON(w, r, http.StatusOK, view)
}
