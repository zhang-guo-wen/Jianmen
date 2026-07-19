package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func (s *DBStore) platformAccountView(a model.PlatformAccount) PlatformAccountView {
	view := PlatformAccountView{
		ID:           a.ID,
		Name:         a.Name,
		PlatformName: a.PlatformName,
		URL:          a.URL,
		Group:        a.GroupName,
		Username:     a.Username,
		HasPassword:  a.Password.GetPlaintext() != "",
		Remark:       a.Remark,
		OwnerID:      a.OwnerID,
		Status:       a.Status,
		ExpiresAt:    a.ExpiresAt,
		CreatedAt:    a.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    a.UpdatedAt.Format(time.RFC3339),
	}
	if a.Owner.Username != "" {
		view.OwnerName = a.Owner.Username
	}
	return view
}

func (s *DBStore) PlatformAccounts(ctx context.Context, params PlatformAccountListParams) ([]PlatformAccountView, int64, error) {
	q := s.db.WithContext(ctx).Model(&model.PlatformAccount{})
	if params.Platform != "" {
		q = q.Where("platform_name = ?", params.Platform)
	}
	if params.Search != "" {
		like := "%" + params.Search + "%"
		q = q.Where("name LIKE ? OR platform_name LIKE ? OR username LIKE ? OR url LIKE ? OR group_name LIKE ?", like, like, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count platform accounts: %w", err)
	}

	query := q.Preload("Owner").Order("created_at DESC")
	if !params.Unpaged {
		page := params.Page
		if page < 1 {
			page = 1
		}
		pageSize := params.PageSize
		if pageSize < 1 || pageSize > 200 {
			pageSize = 50
		}
		query = query.Offset((page - 1) * pageSize).Limit(pageSize)
	}
	var accounts []model.PlatformAccount
	if err := query.Find(&accounts).Error; err != nil {
		return nil, 0, fmt.Errorf("list platform accounts: %w", err)
	}

	views := make([]PlatformAccountView, len(accounts))
	for i := range accounts {
		views[i] = s.platformAccountView(accounts[i])
	}
	return views, total, nil
}

func (s *DBStore) PlatformAccount(ctx context.Context, id string) (PlatformAccountView, error) {
	var a model.PlatformAccount
	if err := s.db.WithContext(ctx).Preload("Owner").First(&a, "id = ?", id).Error; err != nil {
		return PlatformAccountView{}, fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
	}
	return s.platformAccountView(a), nil
}

func (s *DBStore) AddPlatformAccount(ctx context.Context, acc model.PlatformAccount) (PlatformAccountView, error) {
	if acc.Username == "" {
		return PlatformAccountView{}, errors.New("username is required")
	}
	if acc.Name == "" {
		acc.Name = acc.Username
	}
	if acc.Status == "" {
		acc.Status = "active"
	}
	acc.GroupName = strings.TrimSpace(acc.GroupName)

	var createdView PlatformAccountView
	var loadErr error
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&acc).Error; err != nil {
			return err
		}
		if err := ensureAccountGroup(tx, acc.GroupName); err != nil {
			return err
		}
		if err := s.syncResourceTx(tx, model.ResourceTypePlatformAccount, acc.ID, acc.Name, ""); err != nil {
			return err
		}
		var created model.PlatformAccount
		if err := tx.Preload("Owner").First(&created, "id = ?", acc.ID).Error; err != nil {
			loadErr = fmt.Errorf("load created platform account: %w", err)
			return loadErr
		}
		createdView = s.platformAccountView(created)
		return nil
	}); err != nil {
		if loadErr != nil {
			return PlatformAccountView{}, loadErr
		}
		return PlatformAccountView{}, fmt.Errorf("create platform account: %w", err)
	}
	return createdView, nil
}

func (s *DBStore) UpdatePlatformAccount(ctx context.Context, id string, acc model.PlatformAccount) (PlatformAccountView, error) {
	var existing model.PlatformAccount
	if err := s.db.WithContext(ctx).First(&existing, "id = ?", id).Error; err != nil {
		return PlatformAccountView{}, fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
	}

	if acc.PlatformName != "" {
		existing.PlatformName = acc.PlatformName
	}
	if acc.URL != "" {
		existing.URL = acc.URL
	}
	if acc.GroupName != "" {
		existing.GroupName = strings.TrimSpace(acc.GroupName)
	}
	if acc.Username != "" {
		existing.Username = acc.Username
	}
	if acc.Name != "" {
		existing.Name = acc.Name
	}
	if acc.Remark != "" {
		existing.Remark = acc.Remark
	}
	if acc.Password.GetPlaintext() != "" {
		existing.Password = acc.Password
	}
	if acc.Status != "" {
		existing.Status = acc.Status
	}
	if acc.ExpiresAt != nil {
		existing.ExpiresAt = acc.ExpiresAt
	}

	var updatedView PlatformAccountView
	var loadErr error
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&existing).Error; err != nil {
			return err
		}
		if err := ensureAccountGroup(tx, existing.GroupName); err != nil {
			return err
		}
		if err := s.syncResourceTx(tx, model.ResourceTypePlatformAccount, existing.ID, existing.Name, ""); err != nil {
			return err
		}
		var updated model.PlatformAccount
		if err := tx.Preload("Owner").First(&updated, "id = ?", id).Error; err != nil {
			loadErr = fmt.Errorf("load updated platform account: %w", err)
			return loadErr
		}
		updatedView = s.platformAccountView(updated)
		return nil
	}); err != nil {
		if loadErr != nil {
			return PlatformAccountView{}, loadErr
		}
		return PlatformAccountView{}, fmt.Errorf("update platform account: %w", err)
	}
	return updatedView, nil
}

func (s *DBStore) DeletePlatformAccount(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var acc model.PlatformAccount
		if err := tx.First(&acc, "id = ?", id).Error; err != nil {
			return fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypePlatformAccount, acc.ID); err != nil {
			return err
		}
		return tx.Delete(&acc).Error
	})
}

func (s *DBStore) GetPlatformAccountPassword(ctx context.Context, id string) (string, error) {
	var acc model.PlatformAccount
	if err := s.db.WithContext(ctx).First(&acc, "id = ?", id).Error; err != nil {
		return "", fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
	}
	return acc.Password.GetPlaintext(), nil
}
