package admin

import (
	"encoding/json"
	"errors"
	"fmt"
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

// 批量授权请求
type batchResourceGrantRequest struct {
	Grants []resourceGrantRequest `json:"grants"`
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
	// 检查是否按主体过滤
	principalType := r.URL.Query().Get("principal_type")
	principalID := r.URL.Query().Get("principal_id")
	if principalType != "" && principalID != "" {
		s.listResourceGrantsByPrincipal(w, r, resourceGrants, principalType, principalID)
		return
	}
	page, err := resourceGrants.List(
		r.Context(),
		userIDFromRequest(r),
		isSuperAdminRequest(r),
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

// listResourceGrantsByPrincipal 按主体类型和ID查询授权
func (s *Server) listResourceGrantsByPrincipal(w http.ResponseWriter, r *http.Request, svc *service.ResourceGrantService, principalType, principalID string) {
	grants, err := svc.ListByPrincipal(r.Context(), userIDFromRequest(r), isSuperAdminRequest(r), principalType, principalID)
	if err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{
		Items: grants, Total: len(grants), Page: 1, PageSize: len(grants),
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
	grant, err := resourceGrants.Create(r.Context(), userIDFromRequest(r), isSuperAdminRequest(r), model.ResourceGrant{
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
	grant, err := resourceGrants.Get(r.Context(), userIDFromRequest(r), isSuperAdminRequest(r), id)
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
	if err := resourceGrants.Delete(r.Context(), userIDFromRequest(r), isSuperAdminRequest(r), id); err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"message": "resource grant deleted"})
}

// 批量创建资源授权
func (s *Server) handleBatchResourceGrants(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	resourceGrants, ok := s.requireResourceGrantService(w, r)
	if !ok {
		return
	}

	var req batchResourceGrantRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "无效的JSON: "+err.Error())
		return
	}
	if len(req.Grants) == 0 {
		s.writeErrorText(w, r, http.StatusBadRequest, "grants 不能为空")
		return
	}

	grants := make([]model.ResourceGrant, 0, len(req.Grants))
	for _, g := range req.Grants {
		grants = append(grants, model.ResourceGrant{
			PrincipalType: g.PrincipalType,
			PrincipalID:   g.PrincipalID,
			ResourceType:  g.ResourceType,
			ResourceID:    g.ResourceID,
			Effect:        g.Effect,
			ExpiresAt:     g.ExpiresAt,
		})
	}

	result, err := resourceGrants.BatchCreate(r.Context(), userIDFromRequest(r), isSuperAdminRequest(r), grants)
	if err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}

	message := buildBatchGrantMessage(result.Created, result.Refreshed)
	s.writeJSON(w, r, http.StatusCreated, map[string]interface{}{
		"created":   result.Created,
		"refreshed": result.Refreshed,
		"message":   message,
	})
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

// buildBatchGrantMessage 构建批量授权结果的中文提示
func buildBatchGrantMessage(created, refreshed int) string {
	switch {
	case created == 0 && refreshed > 0:
		return fmt.Sprintf("全部资源已授权，刷新%d项授权", refreshed)
	case created > 0 && refreshed == 0:
		return fmt.Sprintf("新增%d项授权", created)
	case created > 0 && refreshed > 0:
		return fmt.Sprintf("新增%d项授权，刷新%d项授权", created, refreshed)
	default:
		return "未创建任何授权"
	}
}
