package admin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listUsers(w, r)
	case http.MethodPost:
		s.createUser(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	id, ok := userIDFromPath(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getUser(w, r, id)
	case http.MethodPut:
		s.updateUser(w, r, id)
	case http.MethodDelete:
		s.deleteUser(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.userManagementService()
	if err != nil {
		logAdminServiceError(s, "user management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user management service unavailable")
		return
	}
	items, total, params, err := users.List(r.Context(), service.UserListParams{
		Query: r.URL.Query().Get("q"), Page: positiveIntRequestQuery(r, "page", 1), PageSize: positiveIntRequestQuery(r, "page_size", defaultPageSize),
	})
	if err != nil {
		s.writeUserServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{Items: items, Total: int(total), Page: params.Page, PageSize: params.PageSize})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	users, err := s.userManagementService()
	if err != nil {
		logAdminServiceError(s, "user management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user management service unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}
	created, err := users.Create(r.Context(), service.UserCreateInput{
		Username: req.Username, Password: req.Password, DisplayName: req.DisplayName, Email: req.Email,
		ExpiresAt: req.ExpiresAt, Permanent: req.Permanent,
	})
	if err != nil {
		s.writeUserServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{"user": created.User, "token": created.Token})
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request, id string) {
	users, err := s.userManagementService()
	if err != nil {
		logAdminServiceError(s, "user management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user management service unavailable")
		return
	}
	user, err := users.Get(r.Context(), id)
	if err != nil {
		s.writeUserServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, user)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, id string) {
	users, err := s.userManagementService()
	if err != nil {
		logAdminServiceError(s, "user management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user management service unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}
	user, err := users.Update(r.Context(), id, service.UserUpdateInput{
		DisplayName: req.DisplayName, Email: req.Email, Status: req.Status, ExpiresAt: req.ExpiresAt, Permanent: req.Permanent,
	})
	if err != nil {
		s.writeUserServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, user)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, id string) {
	users, err := s.userManagementService()
	if err != nil {
		logAdminServiceError(s, "user management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user management service unavailable")
		return
	}
	if err := users.Delete(r.Context(), id, userIDFromRequest(r)); err != nil {
		s.writeUserServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeUserServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrUserNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
	case errors.Is(err, service.ErrUserConflict):
		logAdminServiceError(s, "user conflict", err)
		s.writeErrorText(w, r, http.StatusConflict, "user already exists")
	case errors.Is(err, service.ErrUserForbidden):
		logAdminServiceError(s, "user operation forbidden", err)
		s.writeErrorText(w, r, http.StatusForbidden, "user operation forbidden")
	case errors.Is(err, service.ErrInvalidUser):
		logAdminServiceError(s, "invalid user request", err)
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid user request")
	default:
		logAdminServiceError(s, "user operation failed", err)
		s.writeErrorText(w, r, http.StatusInternalServerError, "user operation failed")
	}
}

func logAdminServiceError(s *Server, message string, err error) {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn(message, "error", err)
}
