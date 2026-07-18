package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"jianmen/internal/service"
	"jianmen/internal/store"
)

type roleActionsService interface {
	Actions(context.Context, string) ([]string, error)
	ReplaceActions(context.Context, string, []string) ([]string, error)
}

func (s *Server) roleManagementService() (*service.RoleService, error) {
	if s.roleManagement != nil {
		return s.roleManagement, nil
	}
	if s.db == nil {
		return nil, errors.New(rbacMetadataUnavailable)
	}
	repository, ok := s.store.(service.RoleManagementRepository)
	if !ok {
		return nil, errors.New("role management service is unavailable")
	}
	return service.NewRoleService(repository)
}

func newRoleManagementService(repository store.Store) (*service.RoleService, error) {
	roles, ok := repository.(service.RoleManagementRepository)
	if !ok {
		return nil, errors.New("admin store does not support role management")
	}
	roleManagement, err := service.NewRoleService(roles)
	if err != nil {
		return nil, fmt.Errorf("initialize role service: %w", err)
	}
	return roleManagement, nil
}

func (s *Server) requireRoleService(w http.ResponseWriter, r *http.Request) (*service.RoleService, bool) {
	roles, err := s.roleManagementService()
	if err != nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, err.Error())
		return nil, false
	}
	return roles, true
}
