package admin

import (
	"net/http"
	"strings"

	"jianmen/internal/rbac"
)

type rbacCatalogResponse struct {
	Pages []rbac.PermissionPageDefinition `json:"pages"`
}

type rbacRoleActionsRequest struct {
	Actions []string `json:"actions"`
}

type rbacRoleActionsResponse struct {
	RoleID  string   `json:"role_id"`
	Actions []string `json:"actions"`
}

func (s *Server) handleRBACCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.writeJSON(w, r, http.StatusOK, rbacCatalogResponse{Pages: rbac.PermissionPages()})
}

func (s *Server) handleRBACRoleActions(w http.ResponseWriter, r *http.Request, roleID string) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	roles, ok := s.requireRoleService(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		actions, err := roles.Actions(r.Context(), roleID)
		if err != nil {
			writeRBACServiceError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, rbacRoleActionsResponse{RoleID: roleID, Actions: actions})
	case http.MethodPut:
		s.replaceRBACRoleActions(w, r, roles, roleID)
	default:
		w.Header().Set("Allow", "GET, PUT")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) replaceRBACRoleActions(w http.ResponseWriter, r *http.Request, roles roleActionsService, roleID string) {
	var req rbacRoleActionsRequest
	if !decodeRBACJSON(w, r, &req) {
		return
	}
	actions, err := roles.ReplaceActions(r.Context(), roleID, req.Actions)
	if err != nil {
		writeRBACServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, rbacRoleActionsResponse{RoleID: roleID, Actions: actions})
}

func rbacRoleActionsIDFromPath(path string) (string, bool) {
	const prefix = "/api/rbac/roles/"
	const suffix = "/actions"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix))
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}
