package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jianmen/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *DBStore) SearchUserGroups(ctx context.Context, query string, page, pageSize int) ([]model.UserGroup, int64, error) {
	buildQuery := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Model(&model.UserGroup{}).Scopes(ActiveScope)
		if query != "" {
			like := "%" + query + "%"
			tx = tx.Where("name LIKE ? OR description LIKE ?", like, like)
		}
		return tx
	}
	var total int64
	if err := buildQuery().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count user groups: %w", err)
	}
	var groups []model.UserGroup
	if err := buildQuery().Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&groups).Error; err != nil {
		return nil, 0, fmt.Errorf("list user groups: %w", err)
	}
	return groups, total, nil
}

func (s *DBStore) FindUserGroup(ctx context.Context, id string) (model.UserGroup, bool, error) {
	var group model.UserGroup
	err := s.db.WithContext(ctx).Scopes(ActiveScope).First(&group, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UserGroup{}, false, nil
	}
	if err != nil {
		return model.UserGroup{}, false, fmt.Errorf("find user group: %w", err)
	}
	return group, true, nil
}

func (s *DBStore) UserGroupNameExists(ctx context.Context, name, excludeID string) (bool, error) {
	tx := s.db.WithContext(ctx).Model(&model.UserGroup{}).Scopes(ActiveScope).Where("name = ?", strings.TrimSpace(name))
	if strings.TrimSpace(excludeID) != "" {
		tx = tx.Where("id <> ?", strings.TrimSpace(excludeID))
	}
	var count int64
	if err := tx.Count(&count).Error; err != nil {
		return false, fmt.Errorf("count user group names: %w", err)
	}
	return count > 0, nil
}

func (s *DBStore) CreateUserGroup(ctx context.Context, group model.UserGroup) (model.UserGroup, error) {
	if err := s.db.WithContext(ctx).Create(&group).Error; err != nil {
		if isUniqueConstraintError(err) {
			return model.UserGroup{}, fmt.Errorf("create user group: %w", &repositoryConflictError{err: err})
		}
		return model.UserGroup{}, fmt.Errorf("create user group: %w", err)
	}
	return group, nil
}

func (s *DBStore) UpdateUserGroup(ctx context.Context, group model.UserGroup) (model.UserGroup, error) {
	if err := s.db.WithContext(ctx).Save(&group).Error; err != nil {
		if isUniqueConstraintError(err) {
			return model.UserGroup{}, fmt.Errorf("update user group: %w", &repositoryConflictError{err: err})
		}
		return model.UserGroup{}, fmt.Errorf("update user group: %w", err)
	}
	return group, nil
}

func (s *DBStore) DeleteUserGroup(ctx context.Context, group model.UserGroup) error {
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", group.ID).Delete(&model.UserGroupMember{}).Error; err != nil {
			return fmt.Errorf("delete user group members: %w", err)
		}
		if err := SoftDelete(ctx, tx, "user_groups", group.ID); err != nil {
			return fmt.Errorf("delete user group: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("delete user group transaction: %w", err)
	}
	return nil
}

func (s *DBStore) ListUserGroupMembers(ctx context.Context, groupID string) ([]model.UserGroupMember, error) {
	var members []model.UserGroupMember
	if err := s.db.WithContext(ctx).Model(&model.UserGroupMember{}).
		Joins("JOIN user_groups ON user_groups.id = user_group_members.group_id").
		Joins("JOIN users ON users.id = user_group_members.user_id").
		Where("user_group_members.group_id = ?", strings.TrimSpace(groupID)).
		Where("user_groups.active_marker = ? AND users.active_marker = ?", model.ActiveMarkerValue, model.ActiveMarkerValue).
		Order("user_group_members.created_at DESC").Find(&members).Error; err != nil {
		return nil, fmt.Errorf("list user group members: %w", err)
	}
	return members, nil
}

func (s *DBStore) AddUserGroupMember(ctx context.Context, member model.UserGroupMember) (model.UserGroupMember, bool, error) {
	tx := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "group_id"}, {Name: "user_id"}},
		DoNothing: true,
	})
	result := tx.Create(&member)
	if result.Error != nil {
		return model.UserGroupMember{}, false, fmt.Errorf("add user group member: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return member, true, nil
	}
	var existing model.UserGroupMember
	if err := s.db.WithContext(ctx).Where("group_id = ? AND user_id = ?", member.GroupID, member.UserID).First(&existing).Error; err != nil {
		return model.UserGroupMember{}, false, fmt.Errorf("find existing user group member: %w", err)
	}
	return existing, false, nil
}

func (s *DBStore) RemoveUserGroupMember(ctx context.Context, groupID, userID string) (bool, error) {
	result := s.db.WithContext(ctx).Where("group_id = ? AND user_id = ?", strings.TrimSpace(groupID), strings.TrimSpace(userID)).Delete(&model.UserGroupMember{})
	if result.Error != nil {
		return false, fmt.Errorf("remove user group member: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}
