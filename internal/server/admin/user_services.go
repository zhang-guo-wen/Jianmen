package admin

import (
	"errors"

	"jianmen/internal/service"
)

func (s *Server) userManagementService() (*service.UserService, error) {
	if s.userManagement != nil {
		return s.userManagement, nil
	}
	repository, ok := s.store.(service.UserRepository)
	if !ok {
		return nil, errors.New("user management service is unavailable")
	}
	return service.NewUserService(repository)
}

func (s *Server) userGroupManagementService() (*service.UserGroupService, error) {
	if s.userGroups != nil {
		return s.userGroups, nil
	}
	repository, ok := s.store.(service.UserGroupRepository)
	if !ok {
		return nil, errors.New("user group management service is unavailable")
	}
	return service.NewUserGroupService(repository)
}
