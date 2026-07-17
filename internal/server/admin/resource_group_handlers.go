package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

// handleResourceGroups handles resource group CRUD operations
func (s *Server) handleResourceGroups(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/resource-groups")
	if path == "" || path == "/" {
		switch r.Method {
		case http.MethodGet:
			s.listResourceGroups(w, r)
		case http.MethodPost:
			s.createResourceGroup(w, r)
		default:
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	// /api/resource-groups/{id}
	id := strings.TrimPrefix(path, "/")
	switch r.Method {
	case http.MethodGet:
		s.getResourceGroup(w, r, id)
	case http.MethodPut:
		s.updateResourceGroup(w, r, id)
	case http.MethodDelete:
		s.deleteResourceGroup(w, r, id)
	default:
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// registered in server.go as:
//
//	s.muxHandle(mux, "/api/resource-groups", s.withAuthAndUser(s.handleResourceGroups))
//	s.muxHandle(mux, "/api/resource-groups/", s.withAuthAndUser(s.handleResourceGroups))

func (s *Server) listResourceGroups(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	groupType := r.URL.Query().Get("group_type")
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	tx := s.db.Model(&model.ResourceGroup{})
	if groupType != "" {
		tx = tx.Where("group_type = ?", groupType)
	}
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

	var groups []model.ResourceGroup
	if err := tx.Order("group_type, name").Offset((page - 1) * pageSize).Limit(pageSize).Find(&groups).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	type groupWithCount struct {
		model.ResourceGroup
		HostCount        int64 `json:"host_count"`
		DatabaseCount    int64 `json:"database_count"`
		ApplicationCount int64 `json:"application_count"`
		PlatformCount    int64 `json:"platform_count"`
		AccountCount     int64 `json:"account_count"`
	}

	result := make([]groupWithCount, 0, len(groups))
	for _, g := range groups {
		gwc := groupWithCount{ResourceGroup: g}
		if g.GroupType == model.ResourceGroupTypeResource {
			s.db.Model(&model.Host{}).Where("group_name = ?", g.Name).Count(&gwc.HostCount)
			s.db.Model(&model.DatabaseInstance{}).Where("group_name = ?", g.Name).Count(&gwc.DatabaseCount)
			s.db.Model(&model.Application{}).Where("app_group = ?", g.Name).Count(&gwc.ApplicationCount)
		} else {
			s.db.Model(&model.HostAccount{}).Where("group_name = ?", g.Name).Count(&gwc.AccountCount)
			var databaseCount int64
			s.db.Model(&model.DatabaseAccount{}).Where("group_name = ?", g.Name).Count(&databaseCount)
			var platformCount int64
			s.db.Model(&model.PlatformAccount{}).Where("group_name = ?", g.Name).Count(&platformCount)
			gwc.PlatformCount = platformCount
			gwc.AccountCount += databaseCount + platformCount
		}
		result = append(result, gwc)
	}

	s.writeJSON(w, r, http.StatusOK, pageResponse{Items: result, Total: int(total), Page: page, PageSize: pageSize})
}

func (s *Server) createResourceGroup(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var group model.ResourceGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	if strings.TrimSpace(group.Name) == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "name is required")
		return
	}

	if err := s.db.Create(&group).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusCreated, group)
}

func (s *Server) getResourceGroup(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var group model.ResourceGroup
	if err := s.db.First(&group, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "resource group not found")
		return
	}

	s.writeJSON(w, r, http.StatusOK, group)
}

func (s *Server) updateResourceGroup(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var group model.ResourceGroup
	if err := s.db.First(&group, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "resource group not found")
		return
	}

	var update model.ResourceGroup
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	oldName := group.Name
	if name := strings.TrimSpace(update.Name); name != "" {
		group.Name = name
	}
	if update.Description != "" {
		group.Description = update.Description
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if oldName != group.Name {
			if err := updateGroupedResources(tx, group.GroupType, oldName, group.Name); err != nil {
				return err
			}
		}
		return tx.Save(&group).Error
	}); err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, group)
}

func (s *Server) deleteResourceGroup(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var group model.ResourceGroup
	if err := s.db.First(&group, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "resource group not found")
		return
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := updateGroupedResources(tx, group.GroupType, group.Name, ""); err != nil {
			return err
		}
		return tx.Delete(&group).Error
	}); err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "resource group deleted"})
}

func updateGroupedResources(tx *gorm.DB, groupType, oldName, newName string) error {
	type groupedModel struct {
		model  any
		column string
	}
	var items []groupedModel
	if groupType == model.ResourceGroupTypeResource {
		items = []groupedModel{
			{model: &model.Host{}, column: "group_name"},
			{model: &model.DatabaseInstance{}, column: "group_name"},
			{model: &model.Application{}, column: "app_group"},
		}
	} else {
		items = []groupedModel{
			{model: &model.HostAccount{}, column: "group_name"},
			{model: &model.DatabaseAccount{}, column: "group_name"},
			{model: &model.PlatformAccount{}, column: "group_name"},
		}
	}
	for _, item := range items {
		if err := tx.Model(item.model).Where(item.column+" = ?", oldName).Update(item.column, newName).Error; err != nil {
			return err
		}
	}
	return nil
}
