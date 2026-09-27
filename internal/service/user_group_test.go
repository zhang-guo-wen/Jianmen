package service

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
)

type fakeUserGroupRepository struct {
	groups       map[string]model.UserGroup
	users        map[string]model.User
	nameUsed     bool
	createErr    error
	updateErr    error
	members      map[string]model.UserGroupMember
	createMember int
	deleteMember int
}

func (f *fakeUserGroupRepository) SearchUserGroups(context.Context, string, int, int) ([]model.UserGroup, int64, error) {
	groups := make([]model.UserGroup, 0, len(f.groups))
	for _, group := range f.groups {
		groups = append(groups, group)
	}
	return groups, int64(len(groups)), nil
}

func (f *fakeUserGroupRepository) FindUserGroup(_ context.Context, id string) (model.UserGroup, bool, error) {
	group, ok := f.groups[id]
	return group, ok, nil
}

func (f *fakeUserGroupRepository) UserGroupNameExists(context.Context, string, string) (bool, error) {
	return f.nameUsed, nil
}

func (f *fakeUserGroupRepository) CreateUserGroup(_ context.Context, group model.UserGroup) (model.UserGroup, error) {
	if f.createErr != nil {
		return model.UserGroup{}, f.createErr
	}
	group.ID = "created-group"
	f.groups[group.ID] = group
	return group, nil
}

func (f *fakeUserGroupRepository) UpdateUserGroup(_ context.Context, group model.UserGroup) (model.UserGroup, error) {
	if f.updateErr != nil {
		return model.UserGroup{}, f.updateErr
	}
	f.groups[group.ID] = group
	return group, nil
}

type repositoryGroupConflictTestError struct{ cause error }

func (e repositoryGroupConflictTestError) Error() string {
	return "repository conflict: " + e.cause.Error()
}
func (e repositoryGroupConflictTestError) Unwrap() error  { return e.cause }
func (e repositoryGroupConflictTestError) Conflict() bool { return true }

func (f *fakeUserGroupRepository) DeleteUserGroup(_ context.Context, group model.UserGroup) error {
	delete(f.groups, group.ID)
	return nil
}

func (f *fakeUserGroupRepository) FindUser(_ context.Context, id string) (model.User, bool, error) {
	user, ok := f.users[id]
	return user, ok, nil
}

func (f *fakeUserGroupRepository) ListUserGroupMembers(context.Context, string) ([]model.UserGroupMember, error) {
	members := make([]model.UserGroupMember, 0, len(f.members))
	for _, member := range f.members {
		members = append(members, member)
	}
	return members, nil
}

func (f *fakeUserGroupRepository) AddUserGroupMember(_ context.Context, member model.UserGroupMember) (model.UserGroupMember, bool, error) {
	key := member.GroupID + ":" + member.UserID
	if existing, ok := f.members[key]; ok {
		return existing, false, nil
	}
	member.ID = "member-" + member.UserID
	f.members[key] = member
	f.createMember++
	return member, true, nil
}

func (f *fakeUserGroupRepository) RemoveUserGroupMember(_ context.Context, groupID, userID string) (bool, error) {
	key := groupID + ":" + userID
	if _, ok := f.members[key]; !ok {
		return false, nil
	}
	delete(f.members, key)
	f.deleteMember++
	return true, nil
}

func TestUserGroupServiceRejectsDuplicateName(t *testing.T) {
	groups, err := NewUserGroupService(&fakeUserGroupRepository{groups: map[string]model.UserGroup{}})
	if err != nil {
		t.Fatalf("new user group service: %v", err)
	}
	groups.repository.(*fakeUserGroupRepository).nameUsed = true

	_, err = groups.Create(context.Background(), UserGroupCreateInput{Name: "operators"})
	if !errors.Is(err, ErrUserGroupConflict) {
		t.Fatalf("create error = %v, want ErrUserGroupConflict", err)
	}
}

func TestUserGroupServiceMemberChangesAreIdempotent(t *testing.T) {
	repository := &fakeUserGroupRepository{
		groups:  map[string]model.UserGroup{"group": {ID: "group", Name: "operators"}},
		users:   map[string]model.User{"user": {ID: "user", Username: "alice"}},
		members: map[string]model.UserGroupMember{},
	}
	groups, err := NewUserGroupService(repository)
	if err != nil {
		t.Fatalf("new user group service: %v", err)
	}

	_, created, err := groups.AddMember(context.Background(), "group", "user")
	if err != nil {
		t.Fatalf("first add member: %v", err)
	}
	if !created {
		t.Fatal("first add member reported existing membership")
	}
	_, created, err = groups.AddMember(context.Background(), "group", "user")
	if err != nil {
		t.Fatalf("second add member: %v", err)
	}
	if created {
		t.Fatal("second add member reported a new membership")
	}
	if repository.createMember != 1 {
		t.Fatalf("member created %d times, want 1", repository.createMember)
	}
	if err := groups.RemoveMember(context.Background(), "group", "user"); err != nil {
		t.Fatalf("first remove member: %v", err)
	}
	if err := groups.RemoveMember(context.Background(), "group", "user"); err != nil {
		t.Fatalf("second remove member: %v", err)
	}
	if repository.deleteMember != 1 {
		t.Fatalf("member deleted %d times, want 1", repository.deleteMember)
	}
}

func TestUserGroupServiceMapsRepositoryConflictAndPreservesCause(t *testing.T) {
	cause := errors.New("duplicate group name")
	repository := &fakeUserGroupRepository{
		groups: map[string]model.UserGroup{}, createErr: repositoryGroupConflictTestError{cause: cause},
	}
	groups, err := NewUserGroupService(repository)
	if err != nil {
		t.Fatalf("new user group service: %v", err)
	}
	_, err = groups.Create(context.Background(), UserGroupCreateInput{Name: "operators"})
	if !errors.Is(err, ErrUserGroupConflict) || !errors.Is(err, cause) {
		t.Fatalf("create error = %v, want conflict and cause in chain", err)
	}
}

func TestUserGroupServiceUpdateCanClearDescription(t *testing.T) {
	repository := &fakeUserGroupRepository{
		groups:  map[string]model.UserGroup{"group": {ID: "group", Name: "operators", Description: "old"}},
		users:   map[string]model.User{},
		members: map[string]model.UserGroupMember{},
	}
	groups, err := NewUserGroupService(repository)
	if err != nil {
		t.Fatalf("new user group service: %v", err)
	}

	empty := ""
	if _, err := groups.Update(context.Background(), "group", UserGroupUpdateInput{Description: &empty}); err != nil {
		t.Fatalf("clear description: %v", err)
	}
	if repository.groups["group"].Description != "" {
		t.Fatalf("description = %q, want empty", repository.groups["group"].Description)
	}
}

func TestUserGroupServiceRejectsEmptyNameUpdate(t *testing.T) {
	repository := &fakeUserGroupRepository{
		groups:  map[string]model.UserGroup{"group": {ID: "group", Name: "operators"}},
		users:   map[string]model.User{},
		members: map[string]model.UserGroupMember{},
	}
	groups, err := NewUserGroupService(repository)
	if err != nil {
		t.Fatalf("new user group service: %v", err)
	}

	empty := "   "
	if _, err := groups.Update(context.Background(), "group", UserGroupUpdateInput{Name: &empty}); !errors.Is(err, ErrInvalidUserGroup) {
		t.Fatalf("update error = %v, want ErrInvalidUserGroup", err)
	}
}

func TestUserGroupServiceReturnsExplicitNotFoundErrors(t *testing.T) {
	groups, err := NewUserGroupService(&fakeUserGroupRepository{
		groups: map[string]model.UserGroup{}, users: map[string]model.User{}, members: map[string]model.UserGroupMember{},
	})
	if err != nil {
		t.Fatalf("new user group service: %v", err)
	}

	if _, err := groups.Get(context.Background(), "missing"); !errors.Is(err, ErrUserGroupNotFound) {
		t.Fatalf("get group error = %v, want ErrUserGroupNotFound", err)
	}
	if _, _, err := groups.AddMember(context.Background(), "missing", "also-missing"); !errors.Is(err, ErrUserGroupNotFound) {
		t.Fatalf("add member error = %v, want ErrUserGroupNotFound", err)
	}
}

func TestUserGroupServiceRejectsBlankMemberUserID(t *testing.T) {
	repository := &fakeUserGroupRepository{
		groups:  map[string]model.UserGroup{"group": {ID: "group", Name: "operators"}},
		users:   map[string]model.User{},
		members: map[string]model.UserGroupMember{},
	}
	groups, err := NewUserGroupService(repository)
	if err != nil {
		t.Fatalf("new user group service: %v", err)
	}

	if _, _, err := groups.AddMember(context.Background(), "group", "   "); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("add blank member error = %v, want ErrInvalidUser", err)
	}
}
