package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

type resourceGrantRequest struct {
	PrincipalType string     `json:"principal_type"`
	PrincipalID   string     `json:"principal_id"`
	ResourceType  string     `json:"resource_type"`
	ResourceID    string     `json:"resource_id"`
	Effect        string     `json:"effect"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

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
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleResourceGrant(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/resource-grants/"))
	if id == "" || strings.Contains(id, "/") {
		s.writeErrorText(w, r, http.StatusBadRequest, "id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getResourceGrant(w, r, id)
	case http.MethodDelete:
		s.deleteResourceGrant(w, r, id)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleResourceGrantCheck(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	resourceGrants, ok := s.requireResourceGrantService(w, r)
	if !ok {
		return
	}
	allowed, err := resourceGrants.Check(
		r.Context(),
		r.URL.Query().Get("user_id"),
		r.URL.Query().Get("resource_type"),
		r.URL.Query().Get("resource_id"),
	)
	if err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]bool{"allowed": allowed})
}

func (s *Server) listResourceGrants(w http.ResponseWriter, r *http.Request) {
	resourceGrants, ok := s.requireResourceGrantService(w, r)
	if !ok {
		return
	}
	page, err := resourceGrants.List(
		r.Context(),
		userIDFromRequest(r),
		s.isSuperAdmin(userIDFromRequest(r)),
		r.URL.Query().Get("q"),
		positiveIntRequestQuery(r, "page", 1),
		positiveIntRequestQuery(r, "page_size", defaultPageSize),
	)
	if err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{
		Items: page.Items, Total: page.Total, Page: page.Page, PageSize: page.PageSize,
	})
}

func (s *Server) createResourceGrant(w http.ResponseWriter, r *http.Request) {
	resourceGrants, ok := s.requireResourceGrantService(w, r)
	if !ok {
		return
	}
	var request resourceGrantRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	grant, err := resourceGrants.Create(r.Context(), userIDFromRequest(r), s.isSuperAdmin(userIDFromRequest(r)), model.ResourceGrant{
		PrincipalType: request.PrincipalType,
		PrincipalID:   request.PrincipalID,
		ResourceType:  request.ResourceType,
		ResourceID:    request.ResourceID,
		Effect:        request.Effect,
		ExpiresAt:     request.ExpiresAt,
	})
	if err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, grant)
}

func (s *Server) getResourceGrant(w http.ResponseWriter, r *http.Request, id string) {
	resourceGrants, ok := s.requireResourceGrantService(w, r)
	if !ok {
		return
	}
	grant, err := resourceGrants.Get(r.Context(), userIDFromRequest(r), s.isSuperAdmin(userIDFromRequest(r)), id)
	if err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, grant)
}

func (s *Server) deleteResourceGrant(w http.ResponseWriter, r *http.Request, id string) {
	resourceGrants, ok := s.requireResourceGrantService(w, r)
	if !ok {
		return
	}
	if err := resourceGrants.Delete(r.Context(), userIDFromRequest(r), s.isSuperAdmin(userIDFromRequest(r)), id); err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "resource grant deleted"})
}

func (s *Server) requireResourceGrantService(w http.ResponseWriter, r *http.Request) (*service.ResourceGrantService, bool) {
	if s.resourceGrants == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "resource grant service unavailable")
		return nil, false
	}
	return s.resourceGrants, true
}

func (s *Server) writeResourceGrantError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidResourceGrant):
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrResourceGrantNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, "resource grant not found")
	case errors.Is(err, service.ErrResourceGrantForbidden):
		s.forbidden(w, r)
	default:
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
	}
}
