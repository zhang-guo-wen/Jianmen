package store

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
)

func TestDBStoreUserGroupsDetectsDuplicateName(t *testing.T) {
	store, _ := newUserStoreTest(t)
	ctx := context.Background()
	if _, err := store.CreateUserGroup(ctx, model.UserGroup{ID: "g1", Name: "operators"}); err != nil {
		t.Fatalf("create group: %v", err)
	}
	exists, err := store.UserGroupNameExists(ctx, "operators", "")
	if err != nil || !exists {
		t.Fatalf("group name exists = %v, %v; want true, nil", exists, err)
	}
	_, err = store.CreateUserGroup(ctx, model.UserGroup{ID: "g2", Name: "operators"})
	var marker interface{ Conflict() bool }
	if !errors.As(err, &marker) || !marker.Conflict() {
		t.Fatalf("duplicate user group error = %v, want repository conflict marker", err)
	}
}

func TestDBStoreUpdateUserGroupReturnsRepositoryConflictMarker(t *testing.T) {
	store, _ := newUserStoreTest(t)
	ctx := context.Background()
	if _, err := store.CreateUserGroup(ctx, model.UserGroup{ID: "g1", Name: "operators"}); err != nil {
		t.Fatalf("create operators: %v", err)
	}
	group, err := store.CreateUserGroup(ctx, model.UserGroup{ID: "g2", Name: "auditors"})
	if err != nil {
		t.Fatalf("create auditors: %v", err)
	}
	group.Name = "operators"
	_, err = store.UpdateUserGroup(ctx, group)
	var marker interface{ Conflict() bool }
	if !errors.As(err, &marker) || !marker.Conflict() {
		t.Fatalf("duplicate user group update error = %v, want repository conflict marker", err)
	}
}

func TestDBStoreDeleteUserGroupRollsBackMembersWhenGroupDeleteFails(t *testing.T) {
	store, db := newUserStoreTest(t)
	ctx := context.Background()
	group := model.UserGroup{ID: "g1", Name: "operators"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&model.User{ID: "u1", Username: "alice", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&model.UserGroupMember{ID: "m1", GroupID: group.ID, UserID: "u1"}).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_group_delete BEFORE UPDATE ON user_groups BEGIN SELECT RAISE(ABORT, 'reject update'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := store.DeleteUserGroup(ctx, group); err == nil {
		t.Fatal("delete group succeeded despite rejecting trigger")
	}
	var count int64
	if err := db.Model(&model.UserGroupMember{}).Where("id = ?", "m1").Count(&count).Error; err != nil {
		t.Fatalf("count members: %v", err)
	}
	if count != 1 {
		t.Fatalf("members after rollback = %d, want 1", count)
	}
}
