package admin

import (
	"encoding/json"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
	"net/http"
	"strings"
)

func (s *Server) handleApplications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionAppView) {
			s.forbidden(w)
			return
		}
		apps := s.store.Applications()
		resp := paginateSlice(apps, r, func(v store.ApplicationView, q string) bool {
			return strings.Contains(strings.ToLower(v.Name), q) ||
				strings.Contains(strings.ToLower(v.InternalHost), q) ||
				strings.Contains(strings.ToLower(v.AppGroup), q) ||
				strings.Contains(strings.ToLower(v.Remark), q)
		})
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionAppCreate) {
			s.forbidden(w)
			return
		}
		s.handleCreateApplication(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
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
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.AddApplication(payload.Name, payload.Scheme, payload.Host, payload.Port, payload.ListenPort, payload.Group, payload.Remark)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// TODO: Task 8 — notify app proxy server to start listening
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleApplication(w http.ResponseWriter, r *http.Request) {
	id, child, ok := appPathParts(r.URL.Path)
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	if child != "" {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionAppView) {
			s.forbidden(w)
			return
		}
		view, err := s.store.Application(id)
		if err != nil {
			writeApplicationStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		if !s.requirePermission(r, rbac.ActionAppUpdate) {
			s.forbidden(w)
			return
		}
		s.handleUpdateApplication(w, r, id)
	case http.MethodDelete:
		if !s.requirePermission(r, rbac.ActionAppDelete) {
			s.forbidden(w)
			return
		}
		if err := s.store.DeleteApplication(id); err != nil {
			writeApplicationStoreError(w, err)
			return
		}
		// TODO: Task 8 — notify app proxy server to stop listening
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
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
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.store.UpdateApplication(id, payload.Name, payload.Scheme, payload.Host, payload.Port, payload.ListenPort, payload.Group, payload.Remark, payload.Status)
	if err != nil {
		writeApplicationStoreError(w, err)
		return
	}
	// TODO: Task 8 — notify app proxy server to update listener
	writeJSON(w, http.StatusOK, view)
}
