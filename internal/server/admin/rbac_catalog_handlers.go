package admin

import (
	"errors"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/rbac"

	"gorm.io/gorm"
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
	db, ok := s.metadataDB(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		actions, err := loadRoleActions(db, roleID)
		if err != nil {
			writeRBACDBError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, rbacRoleActionsResponse{RoleID: roleID, Actions: actions})
	case http.MethodPut:
		s.replaceRBACRoleActions(w, r, db, roleID)
	default:
		w.Header().Set("Allow", "GET, PUT")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) replaceRBACRoleActions(w http.ResponseWriter, r *http.Request, db *gorm.DB, roleID string) {
	var req rbacRoleActionsRequest
	if !decodeRBACJSON(w, r, &req) {
		return
	}
	actions, err := rbac.ValidateAssignableActions(req.Actions)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return replaceRoleActions(tx, roleID, actions)
	}); err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, rbacRoleActionsResponse{RoleID: roleID, Actions: actions})
}

func loadRoleActions(db *gorm.DB, roleID string) ([]string, error) {
	var role model.Role
	if err := db.Select("id").First(&role, "id = ?", roleID).Error; err != nil {
		return nil, err
	}
	var actions []string
	err := db.Model(&model.Permission{}).
		Distinct("permissions.action").
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Where("role_permissions.role_id = ?", roleID).
		Where("permissions.action <> '' AND permissions.resource_type = '' AND permissions.resource_id = ''").
		Where("permissions.effect = '' OR permissions.effect = ?", model.PermissionEffectAllow).
		Order("permissions.action").
		Pluck("permissions.action", &actions).Error
	return actions, err
}

func replaceRoleActions(tx *gorm.DB, roleID string, actions []string) error {
	var role model.Role
	if err := tx.Select("id").First(&role, "id = ?", roleID).Error; err != nil {
		return err
	}
	actionPermissionIDs := tx.Model(&model.Permission{}).
		Select("id").
		Where("action <> '' AND resource_type = '' AND resource_id = ''").
		Where("effect = '' OR effect = ?", model.PermissionEffectAllow)
	if err := tx.Where("role_id = ? AND permission_id IN (?)", roleID, actionPermissionIDs).
		Delete(&model.RolePermission{}).Error; err != nil {
		return err
	}
	for _, action := range actions {
		permission, err := findOrCreateActionPermission(tx, action)
		if err != nil {
			return err
		}
		binding := model.RolePermission{RoleID: roleID, PermissionID: permission.ID}
		if err := tx.Create(&binding).Error; err != nil {
			return err
		}
	}
	return nil
}

func findOrCreateActionPermission(tx *gorm.DB, action string) (model.Permission, error) {
	var permission model.Permission
	err := tx.Where("action = ? AND resource_type = '' AND resource_id = ''", action).
		Where("effect = '' OR effect = ?", model.PermissionEffectAllow).
		Order("created_at, id").First(&permission).Error
	if err == nil {
		return permission, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Permission{}, err
	}
	definition, ok := rbac.FindPermissionDefinition(action)
	if !ok || !definition.Assignable {
		return model.Permission{}, errors.New("action is not assignable")
	}
	permission = model.Permission{
		Name: definition.Label, Action: action, Effect: model.PermissionEffectAllow,
		Description: definition.Description,
	}
	if err := tx.Create(&permission).Error; err != nil {
		return model.Permission{}, err
	}
	return permission, nil
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
