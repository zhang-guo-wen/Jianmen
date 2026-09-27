package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
)

var (
	ErrInvalidUserGroup  = errors.New("invalid user group")
	ErrUserGroupNotFound = errors.New("user group not found")
	ErrUserGroupConflict = errors.New("user group already exists")
)

type UserGroupRepository interface {
	SearchUserGroups(ctx context.Context, query string, page, pageSize int) ([]model.UserGroup, int64, error)
	FindUserGroup(ctx context.Context, id string) (model.UserGroup, bool, error)
	UserGroupNameExists(ctx context.Context, name, excludeID string) (bool, error)
	CreateUserGroup(ctx context.Context, group model.UserGroup) (model.UserGroup, error)
	UpdateUserGroup(ctx context.Context, group model.UserGroup) (model.UserGroup, error)
	DeleteUserGroup(ctx context.Context, group model.UserGroup) error
	FindUser(ctx context.Context, id string) (model.User, bool, error)
	ListUserGroupMembers(ctx context.Context, groupID string) ([]model.UserGroupMember, error)
	AddUserGroupMember(ctx context.Context, member model.UserGroupMember) (model.UserGroupMember, bool, error)
	RemoveUserGroupMember(ctx context.Context, groupID, userID string) (bool, error)
}

type UserGroupListParams struct {
	Query    string
	Page     int
	PageSize int
}

type UserGroupCreateInput struct {
	Name        string
	Description string
}

type UserGroupUpdateInput struct {
	Name        *string
	Description *string
}

type UserGroupView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type UserGroupMemberView struct {
	ID        string    `json:"id"`
	GroupID   string    `json:"group_id"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

type UserGroupService struct {
	repository UserGroupRepository
}

func NewUserGroupService(repository UserGroupRepository) (*UserGroupService, error) {
	if repository == nil {
		return nil, errors.New("user group repository is required")
	}
	return &UserGroupService{repository: repository}, nil
}

func (s *UserGroupService) List(ctx context.Context, params UserGroupListParams) ([]UserGroupView, int64, UserGroupListParams, error) {
	params.Query = strings.TrimSpace(params.Query)
	params.Page, params.PageSize = normalizeUserGroupPage(params.Page, params.PageSize)
	groups, total, err := s.repository.SearchUserGroups(ctx, params.Query, params.Page, params.PageSize)
	if err != nil {
		return nil, 0, params, fmt.Errorf("search user groups: %w", err)
	}
	views := make([]UserGroupView, 0, len(groups))
	for _, group := range groups {
		views = append(views, userGroupView(group))
	}
	return views, total, params, nil
}

func (s *UserGroupService) Create(ctx context.Context, input UserGroupCreateInput) (UserGroupView, error) {
	group := model.UserGroup{Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description)}
	if group.Name == "" {
		return UserGroupView{}, fmt.Errorf("%w: name is required", ErrInvalidUserGroup)
	}
	exists, err := s.repository.UserGroupNameExists(ctx, group.Name, "")
	if err != nil {
		return UserGroupView{}, fmt.Errorf("check user group name: %w", err)
	}
	if exists {
		return UserGroupView{}, ErrUserGroupConflict
	}
	created, err := s.repository.CreateUserGroup(ctx, group)
	if err != nil {
		return UserGroupView{}, fmt.Errorf("create user group: %w", mapRepositoryConflict(err, ErrUserGroupConflict))
	}
	return userGroupView(created), nil
}

func (s *UserGroupService) Get(ctx context.Context, id string) (UserGroupView, error) {
	group, err := s.findGroup(ctx, id)
	if err != nil {
		return UserGroupView{}, err
	}
	return userGroupView(group), nil
}

func (s *UserGroupService) Update(ctx context.Context, id string, input UserGroupUpdateInput) (UserGroupView, error) {
	group, err := s.findGroup(ctx, id)
	if err != nil {
		return UserGroupView{}, err
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return UserGroupView{}, fmt.Errorf("%w: name is required", ErrInvalidUserGroup)
		}
		if name != group.Name {
			exists, err := s.repository.UserGroupNameExists(ctx, name, group.ID)
			if err != nil {
				return UserGroupView{}, fmt.Errorf("check user group name: %w", err)
			}
			if exists {
				return UserGroupView{}, ErrUserGroupConflict
			}
			group.Name = name
		}
	}
	if input.Description != nil {
		group.Description = strings.TrimSpace(*input.Description)
	}
	updated, err := s.repository.UpdateUserGroup(ctx, group)
	if err != nil {
		return UserGroupView{}, fmt.Errorf("update user group: %w", mapRepositoryConflict(err, ErrUserGroupConflict))
	}
	return userGroupView(updated), nil
}

func (s *UserGroupService) Delete(ctx context.Context, id string) error {
	group, err := s.findGroup(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.DeleteUserGroup(ctx, group); err != nil {
		return fmt.Errorf("delete user group: %w", err)
	}
	return nil
}

func (s *UserGroupService) ListMembers(ctx context.Context, groupID string) ([]UserGroupMemberView, error) {
	if _, err := s.findGroup(ctx, groupID); err != nil {
		return nil, err
	}
	members, err := s.repository.ListUserGroupMembers(ctx, strings.TrimSpace(groupID))
	if err != nil {
		return nil, fmt.Errorf("list user group members: %w", err)
	}
	views := make([]UserGroupMemberView, 0, len(members))
	for _, member := range members {
		views = append(views, userGroupMemberView(member))
	}
	return views, nil
}

func (s *UserGroupService) AddMember(ctx context.Context, groupID, userID string) (UserGroupMemberView, bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return UserGroupMemberView{}, false, fmt.Errorf("%w: user_id is required", ErrInvalidUser)
	}
	group, err := s.findGroup(ctx, groupID)
	if err != nil {
		return UserGroupMemberView{}, false, err
	}
	user, found, err := s.repository.FindUser(ctx, userID)
	if err != nil {
		return UserGroupMemberView{}, false, fmt.Errorf("find user: %w", err)
	}
	if !found {
		return UserGroupMemberView{}, false, ErrUserNotFound
	}
	member, created, err := s.repository.AddUserGroupMember(ctx, model.UserGroupMember{GroupID: group.ID, UserID: user.ID})
	if err != nil {
		return UserGroupMemberView{}, false, fmt.Errorf("add user group member: %w", err)
	}
	return userGroupMemberView(member), created, nil
}

func (s *UserGroupService) RemoveMember(ctx context.Context, groupID, userID string) error {
	if _, err := s.findGroup(ctx, groupID); err != nil {
		return err
	}
	_, found, err := s.repository.FindUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	if !found {
		return ErrUserNotFound
	}
	if _, err := s.repository.RemoveUserGroupMember(ctx, strings.TrimSpace(groupID), strings.TrimSpace(userID)); err != nil {
		return fmt.Errorf("remove user group member: %w", err)
	}
	return nil
}

func (s *UserGroupService) findGroup(ctx context.Context, id string) (model.UserGroup, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.UserGroup{}, ErrUserGroupNotFound
	}
	group, found, err := s.repository.FindUserGroup(ctx, id)
	if err != nil {
		return model.UserGroup{}, fmt.Errorf("find user group: %w", err)
	}
	if !found {
		return model.UserGroup{}, ErrUserGroupNotFound
	}
	return group, nil
}

func normalizeUserGroupPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

func userGroupView(group model.UserGroup) UserGroupView {
	return UserGroupView{ID: group.ID, Name: group.Name, Description: group.Description, CreatedAt: group.CreatedAt, UpdatedAt: group.UpdatedAt}
}

func userGroupMemberView(member model.UserGroupMember) UserGroupMemberView {
	return UserGroupMemberView{ID: member.ID, GroupID: member.GroupID, UserID: member.UserID, CreatedAt: member.CreatedAt}
}
