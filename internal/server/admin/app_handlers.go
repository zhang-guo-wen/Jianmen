package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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
		apps, err := s.applicationService.List(r.Context(), applicationActor(r))
		if err != nil {
			s.writeApplicationServiceError(w, r, err)
			return
		}
		resp := paginateSlice(apps, r, func(v service.Application, q string) bool {
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
	view, err := s.applicationService.Create(r.Context(), applicationActor(r), applicationRequest(payload))
	if err != nil {
		s.writeApplicationServiceError(w, r, err)
		return
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
		view, err := s.applicationService.Get(r.Context(), applicationActor(r), id)
		if err != nil {
			s.writeApplicationServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		s.handleUpdateApplication(w, r, id)
	case http.MethodDelete:
		if err := s.applicationService.Delete(r.Context(), applicationActor(r), id); err != nil {
			s.writeApplicationServiceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateApplication(w http.ResponseWriter, r *http.Request, id string) {
	payload, ok := s.decodeApplicationPayload(w, r)
	if !ok {
		return
	}
	view, err := s.applicationService.Update(r.Context(), applicationActor(r), id, applicationRequest(payload))
	if err != nil {
		s.writeApplicationServiceError(w, r, err)
		return
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

func applicationActor(r *http.Request) service.ApplicationActor {
	return service.ApplicationActor{
		UserID:     userIDFromRequest(r),
		SuperAdmin: isSuperAdminRequest(r),
	}
}

func applicationRequest(payload applicationPayload) service.ApplicationRequest {
	return service.ApplicationRequest{
		Name:       payload.Name,
		Address:    payload.Address,
		ListenPort: payload.ListenPort,
		Group:      payload.Group,
		Remark:     payload.Remark,
		Status:     payload.Status,
	}
}

func (s *Server) writeApplicationServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrApplicationForbidden):
		s.forbidden(w, r)
	case errors.Is(err, store.ErrApplicationNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrInvalidApplication):
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrApplicationRuntime):
		s.writeErrorText(w, r, http.StatusBadGateway, err.Error())
	default:
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
	}
}
