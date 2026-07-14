package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

// handleResourceGrants handles resource grant CRUD operations
func (s *Server) handleResourceGrants(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listResourceGrants(w, r)
	case http.MethodPost:
		s.createResourceGrant(w, r)
	default:
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleResourceGrant handles single resource grant operations
func (s *Server) handleResourceGrant(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/resource-grants/")
	if id == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getResourceGrant(w, r, id)
	case http.MethodDelete:
		s.deleteResourceGrant(w, r, id)
	default:
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleResourceGrantCheck handles resource grant check requests
func (s *Server) handleResourceGrantCheck(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	if r.Method != http.MethodGet {
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")
	resourceType := r.URL.Query().Get("resource_type")
	resourceID := r.URL.Query().Get("resource_id")

	if userID == "" || resourceType == "" || resourceID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "user_id, resource_type, and resource_id are required")
		return
	}

	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	checker := rbac.NewResourceGrantChecker(s.db)
	allowed, err := checker.HasGrant(userID, resourceType, resourceID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]bool{"allowed": allowed})
}

func (s *Server) listResourceGrants(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tx := s.db.Model(&model.ResourceGrant{})

	if q != "" {
		like := "%" + q + "%"
		// 搜索匹配的主体（用户名/用户组名）
		var principalUserIDs []string
		s.db.Model(&model.User{}).Where("username LIKE ?", like).Pluck("id", &principalUserIDs)
		var principalGroupIDs []string
		s.db.Model(&model.UserGroup{}).Where("name LIKE ?", like).Pluck("id", &principalGroupIDs)
		principalIDs := append(principalUserIDs, principalGroupIDs...)

		// 搜索匹配的资源（主机账号、数据库账号、资源分组）
		var resourceHostAccountIDs []string
		s.db.Model(&model.HostAccount{}).
			Joins("JOIN hosts ON hosts.id = host_accounts.host_id").
			Where("host_accounts.username LIKE ? OR hosts.address LIKE ?", like, like).
			Pluck("host_accounts.id", &resourceHostAccountIDs)
		var resourceDBAccountIDs []string
		s.db.Model(&model.DatabaseAccount{}).Where("unique_name LIKE ?", like).Pluck("id", &resourceDBAccountIDs)
		var resourceGroupIDs []string
		s.db.Model(&model.ResourceGroup{}).Where("name LIKE ?", like).Pluck("id", &resourceGroupIDs)
		resourceIDs := append(resourceHostAccountIDs, resourceDBAccountIDs...)
		resourceIDs = append(resourceIDs, resourceGroupIDs...)

		// 组合搜索条件
		conditions := make([]string, 0)
		args := make([]interface{}, 0)
		if len(principalIDs) > 0 {
			conditions = append(conditions, "principal_id IN ?")
			args = append(args, principalIDs)
		}
		if len(resourceIDs) > 0 {
			conditions = append(conditions, "resource_id IN ?")
			args = append(args, resourceIDs)
		}
		if len(conditions) > 0 {
			tx = tx.Where(strings.Join(conditions, " OR "), args...)
		} else {
			// 没有匹配项时返回空结果
			tx = tx.Where("1 = 0")
		}
	}

	var total int64
	tx.Count(&total)

	page := positiveIntRequestQuery(r, "page", 1)
	pageSize := positiveIntRequestQuery(r, "page_size", 20)
	if pageSize > 200 {
		pageSize = 200
	}

	var grants []model.ResourceGrant
	if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&grants).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, pageResponse{Items: grants, Total: int(total), Page: page, PageSize: pageSize})
}

func (s *Server) createResourceGrant(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var grant model.ResourceGrant
	if err := json.NewDecoder(r.Body).Decode(&grant); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}
	grant.PrincipalType = strings.TrimSpace(grant.PrincipalType)
	grant.PrincipalID = strings.TrimSpace(grant.PrincipalID)
	grant.ResourceType = strings.TrimSpace(grant.ResourceType)
	grant.ResourceID = strings.TrimSpace(grant.ResourceID)
	grant.Effect = strings.ToLower(strings.TrimSpace(grant.Effect))

	// Validate required fields
	if grant.PrincipalType == "" || grant.PrincipalID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "principal_type and principal_id are required")
		return
	}
	if grant.ResourceType == "" || grant.ResourceID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "resource_type and resource_id are required")
		return
	}

	// Validate principal type
	if grant.PrincipalType != "user" && grant.PrincipalType != "user_group" {
		s.writeErrorText(w, r, http.StatusBadRequest, "principal_type must be 'user' or 'user_group'")
		return
	}
	if message := s.validateResourceGrantReferences(grant); message != "" {
		s.writeErrorText(w, r, http.StatusBadRequest, message)
		return
	}

	// Set default effect
	if grant.Effect == "" {
		grant.Effect = model.PermissionEffectAllow
	}

	// Validate effect
	if grant.Effect != model.PermissionEffectAllow && grant.Effect != model.PermissionEffectDeny {
		s.writeErrorText(w, r, http.StatusBadRequest, "effect must be 'allow' or 'deny'")
		return
	}

	if err := s.db.Create(&grant).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusCreated, grant)
}

func (s *Server) validateResourceGrantReferences(grant model.ResourceGrant) string {
	var count int64
	switch grant.PrincipalType {
	case "user":
		s.db.Model(&model.User{}).Where("id = ?", grant.PrincipalID).Count(&count)
	case "user_group":
		s.db.Model(&model.UserGroup{}).Where("id = ?", grant.PrincipalID).Count(&count)
	}
	if count == 0 {
		return "principal not found"
	}

	count = 0
	switch grant.ResourceType {
	case model.ResourceTypeHostAccount:
		s.db.Model(&model.HostAccount{}).Where("id = ?", grant.ResourceID).Count(&count)
	case model.ResourceTypeDatabaseAccount:
		s.db.Model(&model.DatabaseAccount{}).Where("id = ?", grant.ResourceID).Count(&count)
	case model.ResourceTypeApplication:
		s.db.Model(&model.Application{}).Where("id = ?", grant.ResourceID).Count(&count)
	case model.ResourceTypeGroup:
		s.db.Model(&model.ResourceGroup{}).
			Where("id = ? AND group_type = ?", grant.ResourceID, model.ResourceGroupTypeResource).
			Count(&count)
	case model.ResourceTypeAccountGroup:
		s.db.Model(&model.ResourceGroup{}).
			Where("id = ? AND group_type = ?", grant.ResourceID, model.ResourceGroupTypeAccount).
			Count(&count)
	default:
		return "unsupported resource_type"
	}
	if count == 0 {
		return "resource not found or resource_type mismatch"
	}
	return ""
}

func (s *Server) getResourceGrant(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var grant model.ResourceGrant
	if err := s.db.First(&grant, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "resource grant not found")
		return
	}

	s.writeJSON(w, r, http.StatusOK, grant)
}

func (s *Server) deleteResourceGrant(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	if err := s.db.Delete(&model.ResourceGrant{}, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "resource grant deleted"})
}
