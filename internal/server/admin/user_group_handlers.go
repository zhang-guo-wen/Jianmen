package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

// handleUserGroups handles user group CRUD operations
func (s *Server) handleUserGroups(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listUserGroups(w, r)
	case http.MethodPost:
		s.createUserGroup(w, r)
	default:
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleUserGroupOrMembers handles user group and member operations
func (s *Server) handleUserGroupOrMembers(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/user-groups/")

	// Check if this is a member operation: /api/user-groups/{id}/members/...
	if strings.Contains(path, "/members/") {
		s.handleUserGroupMember(w, r)
	} else if strings.HasSuffix(path, "/members") {
		s.handleUserGroupMembers(w, r)
	} else {
		s.handleUserGroupRoute(w, r)
	}
}

// handleUserGroupRoute handles single user group operations
func (s *Server) handleUserGroupRoute(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/user-groups/")
	if id == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getUserGroup(w, r, id)
	case http.MethodPut:
		s.updateUserGroup(w, r, id)
	case http.MethodDelete:
		s.deleteUserGroup(w, r, id)
	default:
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleUserGroupMembers handles user group member operations
func (s *Server) handleUserGroupMembers(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/user-groups/")
	groupID := strings.TrimSuffix(path, "/members")
	if groupID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "group id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.listUserGroupMembers(w, r, groupID)
	case http.MethodPost:
		s.addUserGroupMember(w, r, groupID)
	default:
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleUserGroupMember handles single user group member operations
func (s *Server) handleUserGroupMember(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/user-groups/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[1] != "members" {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid path")
		return
	}
	groupID := parts[0]
	userID := parts[2]

	switch r.Method {
	case http.MethodDelete:
		s.removeUserGroupMember(w, r, groupID, userID)
	default:
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listUserGroups(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tx := s.db.Model(&model.UserGroup{})
	if q != "" {
		like := "%" + q + "%"
		tx = tx.Where("name LIKE ? OR description LIKE ?", like, like)
	}

	var total int64
	tx.Count(&total)

	page := positiveIntRequestQuery(r, "page", 1)
	pageSize := positiveIntRequestQuery(r, "page_size", 20)
	if pageSize > 200 {
		pageSize = 200
	}

	var groups []model.UserGroup
	if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&groups).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, pageResponse{Items: groups, Total: int(total), Page: page, PageSize: pageSize})
}

func (s *Server) createUserGroup(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var group model.UserGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	if group.Name == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "name is required")
		return
	}

	if err := s.db.Create(&group).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusCreated, group)
}

func (s *Server) getUserGroup(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var group model.UserGroup
	if err := s.db.First(&group, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "user group not found")
		return
	}

	s.writeJSON(w, r, http.StatusOK, group)
}

func (s *Server) updateUserGroup(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var group model.UserGroup
	if err := s.db.First(&group, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "user group not found")
		return
	}

	var update model.UserGroup
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	if update.Name != "" {
		group.Name = update.Name
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

func (s *Server) deleteUserGroup(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	if err := s.db.Delete(&model.UserGroup{}, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "user group deleted"})
}

func (s *Server) listUserGroupMembers(w http.ResponseWriter, r *http.Request, groupID string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var members []model.UserGroupMember
	if err := s.db.Where("group_id = ?", groupID).Find(&members).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, members)
}

func (s *Server) addUserGroupMember(w http.ResponseWriter, r *http.Request, groupID string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	var member model.UserGroupMember
	if err := json.NewDecoder(r.Body).Decode(&member); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	member.GroupID = groupID

	if err := s.db.Create(&member).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusCreated, member)
}

func (s *Server) removeUserGroupMember(w http.ResponseWriter, r *http.Request, groupID, userID string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	if err := s.db.Delete(&model.UserGroupMember{}, "group_id = ? AND user_id = ?", groupID, userID).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "member removed"})
}
