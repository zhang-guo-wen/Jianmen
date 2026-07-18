package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

type userGroupRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type userGroupMemberRequest struct {
	UserID string `json:"user_id"`
}

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
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUserGroupOrMembers(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/user-groups/")
	if strings.Contains(path, "/members/") {
		s.handleUserGroupMember(w, r)
	} else if strings.HasSuffix(path, "/members") {
		s.handleUserGroupMembers(w, r)
	} else {
		s.handleUserGroupRoute(w, r)
	}
}

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
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

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
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUserGroupMember(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/user-groups/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "members" || parts[2] == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid path")
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.removeUserGroupMember(w, r, parts[0], parts[2])
}

func (s *Server) listUserGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.userGroupManagementService()
	if err != nil {
		logAdminServiceError(s, "user group management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user group management service unavailable")
		return
	}
	items, total, params, err := groups.List(r.Context(), service.UserGroupListParams{
		Query: r.URL.Query().Get("q"), Page: positiveIntRequestQuery(r, "page", 1), PageSize: positiveIntRequestQuery(r, "page_size", defaultPageSize),
	})
	if err != nil {
		s.writeUserGroupServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{Items: items, Total: int(total), Page: params.Page, PageSize: params.PageSize})
}

func (s *Server) createUserGroup(w http.ResponseWriter, r *http.Request) {
	groups, err := s.userGroupManagementService()
	if err != nil {
		logAdminServiceError(s, "user group management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user group management service unavailable")
		return
	}
	var req userGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}
	name := ""
	if req.Name != nil {
		name = *req.Name
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}
	group, err := groups.Create(r.Context(), service.UserGroupCreateInput{Name: name, Description: description})
	if err != nil {
		s.writeUserGroupServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, group)
}

func (s *Server) getUserGroup(w http.ResponseWriter, r *http.Request, id string) {
	groups, err := s.userGroupManagementService()
	if err != nil {
		logAdminServiceError(s, "user group management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user group management service unavailable")
		return
	}
	group, err := groups.Get(r.Context(), id)
	if err != nil {
		s.writeUserGroupServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, group)
}

func (s *Server) updateUserGroup(w http.ResponseWriter, r *http.Request, id string) {
	groups, err := s.userGroupManagementService()
	if err != nil {
		logAdminServiceError(s, "user group management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user group management service unavailable")
		return
	}
	var req userGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}
	group, err := groups.Update(r.Context(), id, service.UserGroupUpdateInput{Name: req.Name, Description: req.Description})
	if err != nil {
		s.writeUserGroupServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, group)
}

func (s *Server) deleteUserGroup(w http.ResponseWriter, r *http.Request, id string) {
	groups, err := s.userGroupManagementService()
	if err != nil {
		logAdminServiceError(s, "user group management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user group management service unavailable")
		return
	}
	if err := groups.Delete(r.Context(), id); err != nil {
		s.writeUserGroupServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "user group deleted"})
}

func (s *Server) listUserGroupMembers(w http.ResponseWriter, r *http.Request, groupID string) {
	groups, err := s.userGroupManagementService()
	if err != nil {
		logAdminServiceError(s, "user group management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user group management service unavailable")
		return
	}
	members, err := groups.ListMembers(r.Context(), groupID)
	if err != nil {
		s.writeUserGroupServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, members)
}

func (s *Server) addUserGroupMember(w http.ResponseWriter, r *http.Request, groupID string) {
	groups, err := s.userGroupManagementService()
	if err != nil {
		logAdminServiceError(s, "user group management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user group management service unavailable")
		return
	}
	var req userGroupMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.UserID) == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "user_id is required")
		return
	}
	member, created, err := groups.AddMember(r.Context(), groupID, req.UserID)
	if err != nil {
		s.writeUserGroupServiceError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	s.writeJSON(w, r, status, member)
}

func (s *Server) removeUserGroupMember(w http.ResponseWriter, r *http.Request, groupID, userID string) {
	groups, err := s.userGroupManagementService()
	if err != nil {
		logAdminServiceError(s, "user group management service unavailable", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "user group management service unavailable")
		return
	}
	if err := groups.RemoveMember(r.Context(), groupID, userID); err != nil {
		s.writeUserGroupServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "member removed"})
}

func (s *Server) writeUserGroupServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrUserGroupNotFound), errors.Is(err, service.ErrUserNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
	case errors.Is(err, service.ErrUserGroupConflict):
		logAdminServiceError(s, "user group conflict", err)
		s.writeErrorText(w, r, http.StatusConflict, "user group already exists")
	case errors.Is(err, service.ErrInvalidUser):
		logAdminServiceError(s, "invalid user group member request", err)
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid user group member request")
	case errors.Is(err, service.ErrInvalidUserGroup):
		logAdminServiceError(s, "invalid user group request", err)
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid user group request")
	default:
		logAdminServiceError(s, "user group operation failed", err)
		s.writeErrorText(w, r, http.StatusInternalServerError, "user group operation failed")
	}
}
