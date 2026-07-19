package admin

import (
	"errors"

	"jianmen/internal/service"
)

func (s *Server) userManagementService() (*service.UserService, error) {
	if s.userManagement != nil {
		return s.userManagement, nil
	}
	if s.userRepository == nil {
		return nil, errors.New("user management service is unavailable")
	}
	return service.NewUserService(s.userRepository)
}

func (s *Server) userGroupManagementService() (*service.UserGroupService, error) {
	if s.userGroups != nil {
		return s.userGroups, nil
	}
	if s.userGroupRepository == nil {
		return nil, errors.New("user group management service is unavailable")
	}
	return service.NewUserGroupService(s.userGroupRepository)
}
