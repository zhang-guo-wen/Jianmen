package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/pkg/apiresp"

	"gorm.io/gorm"
)

func (s *Server) metadataDB(w http.ResponseWriter, r *http.Request) (*gorm.DB, bool) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, rbacMetadataUnavailable)
		return nil, false
	}
	return s.db, true
}

func decodeRBACJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, err.Error(), nil, apiresp.RequestID(r.Context()))
		return false
	}
	return true
}

func roleFromRequest(req rbacRoleRequest) (model.Role, error) {
	role := model.Role{
		ID:      strings.TrimSpace(req.ID),
		Builtin: req.Builtin != nil && *req.Builtin,
	}
	if err := applyRoleRequest(&role, req); err != nil {
		return model.Role{}, err
	}
	return role, nil
}

func applyRoleRequest(role *model.Role, req rbacRoleRequest) error {
	role.Name = strings.TrimSpace(req.Name)
	role.Description = strings.TrimSpace(req.Description)
	role.Status = strings.TrimSpace(req.Status)
	if role.Status == "" {
		role.Status = "active"
	}
	if req.Builtin != nil {
		role.Builtin = *req.Builtin
	}
	if role.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func permissionFromRequest(req rbacPermissionRequest) (model.Permission, error) {
	permission := model.Permission{
		ID: strings.TrimSpace(req.ID),
	}
	if err := applyPermissionRequest(&permission, req); err != nil {
		return model.Permission{}, err
	}
	return permission, nil
}

func applyPermissionRequest(permission *model.Permission, req rbacPermissionRequest) error {
	permission.Name = strings.TrimSpace(req.Name)
	permission.Action = strings.TrimSpace(req.Action)
	permission.ResourceType = strings.TrimSpace(req.ResourceType)
	permission.ResourceID = strings.TrimSpace(req.ResourceID)
	permission.Effect = strings.ToLower(strings.TrimSpace(req.Effect))
	permission.Description = strings.TrimSpace(req.Description)
	if permission.Effect == "" {
		permission.Effect = model.PermissionEffectAllow
	}
	if permission.Effect != model.PermissionEffectAllow && permission.Effect != model.PermissionEffectDeny {
		return errors.New("effect must be allow or deny")
	}
	if (permission.ResourceType == "") != (permission.ResourceID == "") {
		return errors.New("resource_type and resource_id must be provided together")
	}
	if permission.Action == "" && permission.ResourceType == "" && permission.ResourceID == "" {
		return errors.New("action or resource is required")
	}
	return nil
}

func userRoleFromRequest(req rbacUserRoleRequest) (model.UserRole, error) {
	userRole := model.UserRole{
		ID:        strings.TrimSpace(req.ID),
		UserID:    strings.TrimSpace(req.UserID),
		RoleID:    strings.TrimSpace(req.RoleID),
		ExpiresAt: req.ExpiresAt,
	}
	if userRole.UserID == "" || userRole.RoleID == "" {
		return model.UserRole{}, errors.New("user_id and role_id are required")
	}
	return userRole, nil
}

func rolePermissionFromRequest(req rbacRolePermissionRequest) (model.RolePermission, error) {
	rolePermission := model.RolePermission{
		ID:           strings.TrimSpace(req.ID),
		RoleID:       strings.TrimSpace(req.RoleID),
		PermissionID: strings.TrimSpace(req.PermissionID),
	}
	if rolePermission.RoleID == "" || rolePermission.PermissionID == "" {
		return model.RolePermission{}, errors.New("role_id and permission_id are required")
	}
	return rolePermission, nil
}

func rbacIDFromPath(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func writeRBACDBError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, "not found", nil, apiresp.RequestID(r.Context()))
		return
	}
	apiresp.WriteError(w, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil, apiresp.RequestID(r.Context()))
}
