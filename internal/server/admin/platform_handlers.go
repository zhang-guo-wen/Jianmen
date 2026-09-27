package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

func platformAccountPathParts(path string) (id, child string, ok bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/platform-accounts/"), "/")
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		return parts[0], "", true
	}
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func (s *Server) writePlatformAccountServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrPlatformAccountForbidden), errors.Is(err, service.ErrPlatformAccountUnavailable):
		s.forbidden(w, r)
	case errors.Is(err, store.ErrPlatformAccountNotFound):
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
	case errors.Is(err, service.ErrInvalidPlatformAccount):
		apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, err.Error(), nil, apiresp.RequestID(r.Context()))
	default:
		s.writeErrorText(w, r, http.StatusInternalServerError, "platform account operation failed")
	}
}

func (s *Server) handlePlatformAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListPlatformAccounts(w, r)
	case http.MethodPost:
		s.handleCreatePlatformAccount(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleListPlatformAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthenticatedUser(w, r) {
		return
	}
	views, err := s.platformAccountService.List(r.Context(), platformAccountActor(r), r.URL.Query().Get("q"), r.URL.Query().Get("platform"))
	if err != nil {
		s.writePlatformAccountServiceError(w, r, err)
		return
	}
	resp := paginateSlice(views, r, func(service.PlatformAccount, string) bool { return true })
	s.writeJSON(w, r, http.StatusOK, resp)
}

func (s *Server) handleCreatePlatformAccount(w http.ResponseWriter, r *http.Request) {
	payload, ok := s.decodePlatformAccountPayload(w, r)
	if !ok {
		return
	}
	view, err := s.platformAccountService.Create(r.Context(), platformAccountActor(r), platformAccountRequest(payload))
	if err != nil {
		s.writePlatformAccountServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handlePlatformAccount(w http.ResponseWriter, r *http.Request) {
	id, child, ok := platformAccountPathParts(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if child == "password" {
		s.handlePlatformAccountPassword(w, r, id)
		return
	}
	if child != "" {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		view, err := s.platformAccountService.Get(r.Context(), platformAccountActor(r), id)
		if err != nil {
			s.writePlatformAccountServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		s.handleUpdatePlatformAccount(w, r, id)
	case http.MethodDelete:
		if err := s.platformAccountService.Delete(r.Context(), platformAccountActor(r), id); err != nil {
			s.writePlatformAccountServiceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdatePlatformAccount(w http.ResponseWriter, r *http.Request, id string) {
	payload, ok := s.decodePlatformAccountPayload(w, r)
	if !ok {
		return
	}
	view, err := s.platformAccountService.Update(r.Context(), platformAccountActor(r), id, platformAccountRequest(payload))
	if err != nil {
		s.writePlatformAccountServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handlePlatformAccountPassword(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	password, err := s.platformAccountService.Password(r.Context(), platformAccountActor(r), id)
	if err != nil {
		s.writePlatformAccountServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"password": password})
}

type platformAccountPayload struct {
	Name         string  `json:"name"`
	PlatformName string  `json:"platform_name"`
	URL          string  `json:"url"`
	Group        string  `json:"group"`
	Username     string  `json:"username"`
	Password     string  `json:"password"`
	Remark       string  `json:"remark"`
	Status       string  `json:"status"`
	ExpiresAt    *string `json:"expires_at"`
}

func (s *Server) decodePlatformAccountPayload(w http.ResponseWriter, r *http.Request) (platformAccountPayload, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload platformAccountPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return platformAccountPayload{}, false
	}
	return payload, true
}

func platformAccountActor(r *http.Request) service.PlatformAccountActor {
	return service.PlatformAccountActor{UserID: userIDFromRequest(r), SuperAdmin: isSuperAdminRequest(r)}
}

func platformAccountRequest(payload platformAccountPayload) service.PlatformAccountRequest {
	return service.PlatformAccountRequest{
		Name: payload.Name, PlatformName: payload.PlatformName, URL: payload.URL, Group: payload.Group,
		Username: payload.Username, Password: payload.Password, Remark: payload.Remark, Status: payload.Status, ExpiresAt: payload.ExpiresAt,
	}
}
