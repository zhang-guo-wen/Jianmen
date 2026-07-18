package admin

import (
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
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
	db, ok := s.metadataDB(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		tx := db.Model(&model.Role{})
		if q != "" {
			like := "%" + q + "%"
			tx = tx.Where("name LIKE ? OR description LIKE ?", like, like)
		}
		var total int64
		tx.Count(&total)
		page := positiveIntRequestQuery(r, "page", 1)
		pageSize := positiveIntRequestQuery(r, "page_size", defaultPageSize)
		if pageSize > 200 {
			pageSize = 200
		}
		var roles []model.Role
		if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&roles).Error; err != nil {
			writeRBACDBError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: roles, Total: int(total), Page: page, PageSize: pageSize})
	case http.MethodPost:
		s.handleCreateRBACRole(w, r, db)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateRBACRole(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	var req rbacRoleRequest
	if !decodeRBACJSON(w, r, &req) {
		return
	}
	role, err := roleFromRequest(req)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Create(&role).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, role)
}

func (s *Server) handleRBACRole(w http.ResponseWriter, r *http.Request) {
	if id, ok := rbacRoleActionsIDFromPath(r.URL.Path); ok {
		s.handleRBACRoleActions(w, r, id)
		return
	}
	db, ok := s.metadataDB(w, r)
	if !ok {
		return
	}
	id, ok := rbacIDFromPath(r.URL.Path, "/api/rbac/roles/")
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		var role model.Role
		if err := db.First(&role, "id = ?", id).Error; err != nil {
			writeRBACDBError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, role)
	case http.MethodPut:
		s.handleUpdateRBACRole(w, r, db, id)
	case http.MethodDelete:
		s.handleDeleteRBACRole(w, r, db, id)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateRBACRole(w http.ResponseWriter, r *http.Request, db *gorm.DB, id string) {
	var req rbacRoleRequest
	if !decodeRBACJSON(w, r, &req) {
		return
	}
	var role model.Role
	if err := db.First(&role, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	if err := applyRoleRequest(&role, req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Save(&role).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, role)
}

func (s *Server) handleDeleteRBACRole(w http.ResponseWriter, r *http.Request, db *gorm.DB, id string) {
	var role model.Role
	if err := db.First(&role, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	if role.Builtin {
		s.writeErrorText(w, r, http.StatusConflict, "builtin role cannot be deleted")
		return
	}
	if err := db.Delete(&role).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRBACPermissions(w http.ResponseWriter, r *http.Request) {
	db, ok := s.metadataDB(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		tx := db.Model(&model.Permission{})
		if q != "" {
			like := "%" + q + "%"
			tx = tx.Where("name LIKE ? OR action LIKE ? OR description LIKE ?", like, like, like)
		}
		var total int64
		tx.Count(&total)
		page := positiveIntRequestQuery(r, "page", 1)
		pageSize := positiveIntRequestQuery(r, "page_size", defaultPageSize)
		if pageSize > 200 {
			pageSize = 200
		}
		var permissions []model.Permission
		if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&permissions).Error; err != nil {
			writeRBACDBError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: permissions, Total: int(total), Page: page, PageSize: pageSize})
	case http.MethodPost:
		s.handleCreateRBACPermission(w, r, db)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateRBACPermission(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	var req rbacPermissionRequest
	if !decodeRBACJSON(w, r, &req) {
		return
	}
	permission, err := permissionFromRequest(req)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Create(&permission).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, permission)
}

func (s *Server) handleRBACPermission(w http.ResponseWriter, r *http.Request) {
	db, ok := s.metadataDB(w, r)
	if !ok {
		return
	}
	id, ok := rbacIDFromPath(r.URL.Path, "/api/rbac/permissions/")
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		var permission model.Permission
		if err := db.First(&permission, "id = ?", id).Error; err != nil {
			writeRBACDBError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, permission)
	case http.MethodPut:
		s.handleUpdateRBACPermission(w, r, db, id)
	case http.MethodDelete:
		s.handleDeleteRBACPermission(w, r, db, id)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdateRBACPermission(w http.ResponseWriter, r *http.Request, db *gorm.DB, id string) {
	var req rbacPermissionRequest
	if !decodeRBACJSON(w, r, &req) {
		return
	}
	var permission model.Permission
	if err := db.First(&permission, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	if err := applyPermissionRequest(&permission, req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Save(&permission).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, permission)
}

func (s *Server) handleDeleteRBACPermission(w http.ResponseWriter, r *http.Request, db *gorm.DB, id string) {
	var permission model.Permission
	if err := db.First(&permission, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	if err := db.Delete(&permission).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRBACUserRoles(w http.ResponseWriter, r *http.Request) {
	db, ok := s.metadataDB(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		tx := db.Model(&model.UserRole{})
		if q != "" {
			like := "%" + q + "%"
			tx = tx.Where("user_id LIKE ? OR role_id LIKE ?", like, like)
		}
		var total int64
		tx.Count(&total)
		page := positiveIntRequestQuery(r, "page", 1)
		pageSize := positiveIntRequestQuery(r, "page_size", defaultPageSize)
		if pageSize > 200 {
			pageSize = 200
		}
		var userRoles []model.UserRole
		if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&userRoles).Error; err != nil {
			writeRBACDBError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: userRoles, Total: int(total), Page: page, PageSize: pageSize})
	case http.MethodPost:
		s.handleCreateRBACUserRole(w, r, db)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateRBACUserRole(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	var req rbacUserRoleRequest
	if !decodeRBACJSON(w, r, &req) {
		return
	}
	userRole, err := userRoleFromRequest(req)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Create(&userRole).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, userRole)
}

func (s *Server) handleRBACUserRole(w http.ResponseWriter, r *http.Request) {
	db, ok := s.metadataDB(w, r)
	if !ok {
		return
	}
	id, ok := rbacIDFromPath(r.URL.Path, "/api/rbac/user-roles/")
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var userRole model.UserRole
	if err := db.First(&userRole, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	if err := db.Delete(&userRole).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRBACRolePermissions(w http.ResponseWriter, r *http.Request) {
	db, ok := s.metadataDB(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		tx := db.Model(&model.RolePermission{})
		if q != "" {
			like := "%" + q + "%"
			tx = tx.Where("role_id LIKE ? OR permission_id LIKE ?", like, like)
		}
		var total int64
		tx.Count(&total)
		page := positiveIntRequestQuery(r, "page", 1)
		pageSize := positiveIntRequestQuery(r, "page_size", defaultPageSize)
		if pageSize > 200 {
			pageSize = 200
		}
		var rolePermissions []model.RolePermission
		if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rolePermissions).Error; err != nil {
			writeRBACDBError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: rolePermissions, Total: int(total), Page: page, PageSize: pageSize})
	case http.MethodPost:
		s.handleCreateRBACRolePermission(w, r, db)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateRBACRolePermission(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	var req rbacRolePermissionRequest
	if !decodeRBACJSON(w, r, &req) {
		return
	}
	rolePermission, err := rolePermissionFromRequest(req)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Create(&rolePermission).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, rolePermission)
}

func (s *Server) handleRBACRolePermission(w http.ResponseWriter, r *http.Request) {
	db, ok := s.metadataDB(w, r)
	if !ok {
		return
	}
	id, ok := rbacIDFromPath(r.URL.Path, "/api/rbac/role-permissions/")
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var rolePermission model.RolePermission
	if err := db.First(&rolePermission, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	if err := db.Delete(&rolePermission).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRBACEffective(w http.ResponseWriter, r *http.Request) {
	_, ok := s.metadataDB(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	query := r.URL.Query()
	userID := strings.TrimSpace(query.Get("user_id"))
	action := strings.TrimSpace(query.Get("action"))
	resourceType := strings.TrimSpace(query.Get("resource_type"))
	resourceID := strings.TrimSpace(query.Get("resource_id"))
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
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, rbacEffectiveResponse{
		Allowed:      allowed,
		UserID:       userID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
	})
}
