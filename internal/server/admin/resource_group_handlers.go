package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"jianmen/internal/model"
)

// handleResourceGroups handles resource group CRUD operations
func (s *Server) handleResourceGroups(w http.ResponseWriter, r *http.Request) {
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

	query := s.db
	if groupType != "" {
		query = query.Where("group_type = ?", groupType)
	}

	var groups []model.ResourceGroup
	if err := query.Order("group_type, name").Find(&groups).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	type groupWithCount struct {
		model.ResourceGroup
		HostCount     int64 `json:"host_count"`
		DatabaseCount int64 `json:"database_count"`
		AccountCount  int64 `json:"account_count"`
	}

	result := make([]groupWithCount, 0, len(groups))
	for _, g := range groups {
		gwc := groupWithCount{ResourceGroup: g}
		if g.GroupType == model.ResourceGroupTypeResource {
			s.db.Model(&model.Host{}).Where("group_name = ?", g.Name).Count(&gwc.HostCount)
			s.db.Model(&model.DatabaseInstance{}).Where("group_name = ?", g.Name).Count(&gwc.DatabaseCount)
		} else {
			s.db.Model(&model.HostAccount{}).Where("group_name = ?", g.Name).Count(&gwc.AccountCount)
			var dbCount int64
			s.db.Model(&model.DatabaseAccount{}).Where("group_name = ?", g.Name).Count(&dbCount)
			gwc.AccountCount += dbCount
		}
		result = append(result, gwc)
	}

	s.writeJSON(w, r, http.StatusOK, result)
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

	if strings.TrimSpace(update.Name) != "" {
		oldName := group.Name
		group.Name = update.Name
		if group.GroupType == model.ResourceGroupTypeResource {
			s.db.Model(&model.Host{}).Where("group_name = ?", oldName).Update("group_name", group.Name)
			s.db.Model(&model.DatabaseInstance{}).Where("group_name = ?", oldName).Update("group_name", group.Name)
		} else {
			s.db.Model(&model.HostAccount{}).Where("group_name = ?", oldName).Update("group_name", group.Name)
			s.db.Model(&model.DatabaseAccount{}).Where("group_name = ?", oldName).Update("group_name", group.Name)
		}
	}
	if update.Description != "" {
		group.Description = update.Description
	}

	if err := s.db.Save(&group).Error; err != nil {
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

	// 按分组类型清空对应资源的 group_name
	if group.GroupType == model.ResourceGroupTypeResource {
		s.db.Model(&model.Host{}).Where("group_name = ?", group.Name).Update("group_name", "")
		s.db.Model(&model.DatabaseInstance{}).Where("group_name = ?", group.Name).Update("group_name", "")
	} else {
		s.db.Model(&model.HostAccount{}).Where("group_name = ?", group.Name).Update("group_name", "")
		s.db.Model(&model.DatabaseAccount{}).Where("group_name = ?", group.Name).Update("group_name", "")
	}

	if err := s.db.Delete(&group).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "resource group deleted"})
}
