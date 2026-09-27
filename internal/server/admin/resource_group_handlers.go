package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

type createResourceGroupRequest struct {
	Name        string `json:"name"`
	GroupType   string `json:"group_type"`
	Description string `json:"description,omitempty"`
}

type updateResourceGroupRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type resourceGroupView struct {
	model.ResourceGroup
	HostCount        int64 `json:"host_count"`
	DatabaseCount    int64 `json:"database_count"`
	ApplicationCount int64 `json:"application_count"`
	ContainerCount   int64 `json:"container_count"`
	PlatformCount    int64 `json:"platform_count"`
	AccountCount     int64 `json:"account_count"`
}

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
			w.Header().Set("Allow", "GET, POST")
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	id := strings.Trim(strings.TrimPrefix(path, "/"), "/")
	if id == "" || strings.Contains(id, "/") {
		s.writeErrorText(w, r, http.StatusNotFound, "resource group not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getResourceGroup(w, r, id)
	case http.MethodPut:
		s.updateResourceGroup(w, r, id)
	case http.MethodDelete:
		s.deleteResourceGroup(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listResourceGroups(w http.ResponseWriter, r *http.Request) {
	resourceGroups, ok := s.requireResourceGroupService(w, r)
	if !ok {
		return
	}
	summaries, total, params, err := resourceGroups.List(r.Context(), service.ResourceGroupListParams{
		GroupType: r.URL.Query().Get("group_type"),
		Query:     r.URL.Query().Get("q"),
		Page:      positiveIntRequestQuery(r, "page", 1),
		PageSize:  positiveIntRequestQuery(r, "page_size", defaultPageSize),
	})
	if err != nil {
		s.writeResourceGroupError(w, r, err)
		return
	}
	views := make([]resourceGroupView, 0, len(summaries))
	for _, summary := range summaries {
		views = append(views, resourceGroupView{
			ResourceGroup:    summary.Group,
			HostCount:        summary.HostCount,
			DatabaseCount:    summary.DatabaseCount,
			ApplicationCount: summary.ApplicationCount,
			ContainerCount:   summary.ContainerCount,
			PlatformCount:    summary.PlatformCount,
			AccountCount:     summary.AccountCount,
		})
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{
		Items: views, Total: int(total), Page: params.Page, PageSize: params.PageSize,
	})
}

func (s *Server) createResourceGroup(w http.ResponseWriter, r *http.Request) {
	resourceGroups, ok := s.requireResourceGroupService(w, r)
	if !ok {
		return
	}
	var request createResourceGroupRequest
	if !s.decodeResourceGroupJSON(w, r, &request) {
		return
	}
	group, err := resourceGroups.Create(r.Context(), service.CreateResourceGroupInput{
		Name: request.Name, GroupType: request.GroupType, Description: request.Description,
	})
	if err != nil {
		s.writeResourceGroupError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, group)
}

func (s *Server) getResourceGroup(w http.ResponseWriter, r *http.Request, id string) {
	resourceGroups, ok := s.requireResourceGroupService(w, r)
	if !ok {
		return
	}
	group, err := resourceGroups.Get(r.Context(), id)
	if err != nil {
		s.writeResourceGroupError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, group)
}

func (s *Server) updateResourceGroup(w http.ResponseWriter, r *http.Request, id string) {
	resourceGroups, ok := s.requireResourceGroupService(w, r)
	if !ok {
		return
	}
	var request updateResourceGroupRequest
	if !s.decodeResourceGroupJSON(w, r, &request) {
		return
	}
	group, err := resourceGroups.Update(r.Context(), id, service.UpdateResourceGroupInput{
		Name: request.Name, Description: request.Description,
	})
	if err != nil {
		s.writeResourceGroupError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, group)
}

func (s *Server) deleteResourceGroup(w http.ResponseWriter, r *http.Request, id string) {
	resourceGroups, ok := s.requireResourceGroupService(w, r)
	if !ok {
		return
	}
	if err := resourceGroups.Delete(r.Context(), id); err != nil {
		s.writeResourceGroupError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "resource group deleted"})
}

func (s *Server) requireResourceGroupService(w http.ResponseWriter, r *http.Request) (*service.ResourceGroupService, bool) {
	if s.resourceGroups == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "resource group service unavailable")
		return nil, false
	}
	return s.resourceGroups, true
}

func (s *Server) writeResourceGroupError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidResourceGroup):
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrResourceGroupNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, "resource group not found")
	case errors.Is(err, service.ErrResourceGroupConflict):
		s.writeErrorText(w, r, http.StatusConflict, "resource group already exists")
	default:
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) decodeResourceGroupJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}
