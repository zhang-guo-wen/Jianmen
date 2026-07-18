package admin

import (
	"net/http"
	"strings"
	"time"

	"jianmen/internal/service"
)

const rbacMetadataUnavailable = "metadata database unavailable"

type rbacRoleRequest struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Builtin     *bool  `json:"builtin,omitempty"`
	Status      string `json:"status,omitempty"`
}
type rbacPermissionRequest struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Action       string `json:"action,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	Effect       string `json:"effect,omitempty"`
	Description  string `json:"description,omitempty"`
}
type rbacUserRoleRequest struct {
	ID        string     `json:"id,omitempty"`
	UserID    string     `json:"user_id"`
	RoleID    string     `json:"role_id"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
type rbacRolePermissionRequest struct {
	ID           string `json:"id,omitempty"`
	RoleID       string `json:"role_id"`
	PermissionID string `json:"permission_id"`
}
type rbacEffectiveResponse struct {
	Allowed      bool   `json:"allowed"`
	UserID       string `json:"user_id"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
}

func (s *Server) handleRBACRoles(w http.ResponseWriter, r *http.Request) {
	roles, ok := s.requireRoleService(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, total, params, err := roles.List(r.Context(), rbacListParams(r))
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: items, Total: int(total), Page: params.Page, PageSize: params.PageSize})
	case http.MethodPost:
		var req rbacRoleRequest
		if !decodeRBACJSON(w, r, &req) {
			return
		}
		role, err := roles.Create(r.Context(), service.RoleInput{ID: req.ID, Name: req.Name, Description: req.Description, Status: req.Status, Builtin: req.Builtin})
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusCreated, role)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRBACRole(w http.ResponseWriter, r *http.Request) {
	if id, ok := rbacRoleActionsIDFromPath(r.URL.Path); ok {
		s.handleRBACRoleActions(w, r, id)
		return
	}
	roles, ok := s.requireRoleService(w, r)
	if !ok {
		return
	}
	id, valid := rbacIDFromPath(r.URL.Path, "/api/rbac/roles/")
	if !valid {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		role, err := roles.Get(r.Context(), id)
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, role)
	case http.MethodPut:
		var req rbacRoleRequest
		if !decodeRBACJSON(w, r, &req) {
			return
		}
		role, err := roles.Update(r.Context(), id, service.RoleInput{ID: req.ID, Name: req.Name, Description: req.Description, Status: req.Status, Builtin: req.Builtin})
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, role)
	case http.MethodDelete:
		if err := roles.Delete(r.Context(), id); err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRBACPermissions(w http.ResponseWriter, r *http.Request) {
	roles, ok := s.requireRoleService(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, total, params, err := roles.ListPermissions(r.Context(), rbacListParams(r))
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: items, Total: int(total), Page: params.Page, PageSize: params.PageSize})
	case http.MethodPost:
		var req rbacPermissionRequest
		if !decodeRBACJSON(w, r, &req) {
			return
		}
		permission, err := roles.CreatePermission(r.Context(), permissionInput(req))
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusCreated, permission)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRBACPermission(w http.ResponseWriter, r *http.Request) {
	roles, ok := s.requireRoleService(w, r)
	if !ok {
		return
	}
	id, valid := rbacIDFromPath(r.URL.Path, "/api/rbac/permissions/")
	if !valid {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		permission, err := roles.GetPermission(r.Context(), id)
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, permission)
	case http.MethodPut:
		var req rbacPermissionRequest
		if !decodeRBACJSON(w, r, &req) {
			return
		}
		permission, err := roles.UpdatePermission(r.Context(), id, permissionInput(req))
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, permission)
	case http.MethodDelete:
		if err := roles.DeletePermission(r.Context(), id); err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRBACUserRoles(w http.ResponseWriter, r *http.Request) {
	roles, ok := s.requireRoleService(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, total, params, err := roles.ListUserRoles(r.Context(), rbacListParams(r))
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: items, Total: int(total), Page: params.Page, PageSize: params.PageSize})
	case http.MethodPost:
		var req rbacUserRoleRequest
		if !decodeRBACJSON(w, r, &req) {
			return
		}
		binding, err := roles.CreateUserRole(r.Context(), service.UserRoleInput{ID: req.ID, UserID: req.UserID, RoleID: req.RoleID, ExpiresAt: req.ExpiresAt})
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusCreated, binding)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRBACUserRole(w http.ResponseWriter, r *http.Request) {
	roles, ok := s.requireRoleService(w, r)
	if !ok {
		return
	}
	id, valid := rbacIDFromPath(r.URL.Path, "/api/rbac/user-roles/")
	if !valid {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := roles.DeleteUserRole(r.Context(), id); err != nil {
		writeRBACServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRBACRolePermissions(w http.ResponseWriter, r *http.Request) {
	roles, ok := s.requireRoleService(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, total, params, err := roles.ListRolePermissions(r.Context(), rbacListParams(r))
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: items, Total: int(total), Page: params.Page, PageSize: params.PageSize})
	case http.MethodPost:
		var req rbacRolePermissionRequest
		if !decodeRBACJSON(w, r, &req) {
			return
		}
		binding, err := roles.CreateRolePermission(r.Context(), service.RolePermissionInput{ID: req.ID, RoleID: req.RoleID, PermissionID: req.PermissionID})
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusCreated, binding)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRBACRolePermission(w http.ResponseWriter, r *http.Request) {
	roles, ok := s.requireRoleService(w, r)
	if !ok {
		return
	}
	id, valid := rbacIDFromPath(r.URL.Path, "/api/rbac/role-permissions/")
	if !valid {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := roles.DeleteRolePermission(r.Context(), id); err != nil {
		writeRBACServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRBACEffective(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRoleService(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := r.URL.Query()
	userID, action := strings.TrimSpace(query.Get("user_id")), strings.TrimSpace(query.Get("action"))
	resourceType, resourceID := strings.TrimSpace(query.Get("resource_type")), strings.TrimSpace(query.Get("resource_id"))
	if userID == "" || action == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "user_id and action are required")
		return
	}
	if (resourceType == "") != (resourceID == "") {
		s.writeErrorText(w, r, http.StatusBadRequest, "resource_type and resource_id must be provided together")
		return
	}
	allowed, err := s.authorizeConnection(r.Context(), userID, action, resourceType, resourceID)
	if err != nil {
		writeRBACServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, rbacEffectiveResponse{Allowed: allowed, UserID: userID, Action: action, ResourceType: resourceType, ResourceID: resourceID})
}

func rbacListParams(r *http.Request) service.RoleListParams {
	return service.RoleListParams{Query: r.URL.Query().Get("q"), Page: positiveIntRequestQuery(r, "page", 1), PageSize: positiveIntRequestQuery(r, "page_size", defaultPageSize)}
}
func permissionInput(req rbacPermissionRequest) service.PermissionInput {
	return service.PermissionInput{ID: req.ID, Name: req.Name, Action: req.Action, ResourceType: req.ResourceType, ResourceID: req.ResourceID, Effect: req.Effect, Description: req.Description}
}
