package store

import (
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
		Category:     a.Category,
		Group:        a.GroupName,
		Username:     a.Username,
		HasPassword:  a.Password.GetPlaintext() != "",
		HasTOTP:      a.TOTPSecret.GetPlaintext() != "",
		Remark:       a.Remark,
		OwnerID:      a.OwnerID,
		Visibility:   a.Visibility,
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

func (s *DBStore) platformAccountShareView(sh model.PlatformAccountShare) PlatformAccountShareView {
	view := PlatformAccountShareView{
		ID:                sh.ID,
		PlatformAccountID: sh.PlatformAccountID,
		UserID:            sh.UserID,
		RoleID:            sh.RoleID,
		AccessLevel:       sh.AccessLevel,
		ExpiresAt:         sh.ExpiresAt,
		CreatedAt:         sh.CreatedAt.Format(time.RFC3339),
	}
	if sh.User.Username != "" {
		view.Username = sh.User.Username
	}
	if sh.Role.Name != "" {
		view.RoleName = sh.Role.Name
	}
	return view
}

func (s *DBStore) PlatformAccounts(params PlatformAccountListParams) ([]PlatformAccountView, int64, error) {
	q := s.db.Model(&model.PlatformAccount{})

	// 可见性过滤
	if !params.IsAdmin {
		q = q.Where(
			s.db.Where("owner_id = ?", params.UserID).
				Or("id IN (?)", s.db.Model(&model.PlatformAccountShare{}).
					Select("platform_account_id").
					Where("user_id = ? OR role_id IN (?)", params.UserID, params.RoleIDs)),
		)
	}

	if params.OwnerID != "" {
		q = q.Where("owner_id = ?", params.OwnerID)
	}
	if params.Visibility != "" {
		q = q.Where("visibility = ?", params.Visibility)
	}
	if params.Platform != "" {
		q = q.Where("platform_name = ?", params.Platform)
	}
	if params.Category != "" {
		q = q.Where("category = ?", params.Category)
	}
	if params.Search != "" {
		like := "%" + params.Search + "%"
		q = q.Where("name LIKE ? OR platform_name LIKE ? OR username LIKE ?", like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count platform accounts: %w", err)
	}

	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	var accounts []model.PlatformAccount
	if err := q.Preload("Owner").
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&accounts).Error; err != nil {
		return nil, 0, fmt.Errorf("list platform accounts: %w", err)
	}

	views := make([]PlatformAccountView, len(accounts))
	for i := range accounts {
		views[i] = s.platformAccountView(accounts[i])
	}
	return views, total, nil
}

func (s *DBStore) PlatformAccount(id string) (PlatformAccountView, error) {
	var a model.PlatformAccount
	if err := s.db.Preload("Owner").First(&a, "id = ?", id).Error; err != nil {
		return PlatformAccountView{}, fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
	}
	return s.platformAccountView(a), nil
}

func (s *DBStore) AddPlatformAccount(acc model.PlatformAccount) (PlatformAccountView, error) {
	if acc.Username == "" {
		return PlatformAccountView{}, errors.New("username is required")
	}
	if acc.Name == "" {
		acc.Name = acc.Username
	}
	if acc.Status == "" {
		acc.Status = "active"
	}
	if acc.Visibility == "" {
		acc.Visibility = "private"
	}
	acc.GroupName = strings.TrimSpace(acc.GroupName)

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&acc).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, acc.GroupName); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypePlatformAccount, acc.ID, acc.Name, "")
	}); err != nil {
		return PlatformAccountView{}, fmt.Errorf("create platform account: %w", err)
	}

	var created model.PlatformAccount
	if err := s.db.Preload("Owner").First(&created, "id = ?", acc.ID).Error; err != nil {
		return PlatformAccountView{}, nil
	}
	return s.platformAccountView(created), nil
}

func (s *DBStore) UpdatePlatformAccount(id string, acc model.PlatformAccount) (PlatformAccountView, error) {
	var existing model.PlatformAccount
	if err := s.db.First(&existing, "id = ?", id).Error; err != nil {
		return PlatformAccountView{}, fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
	}

	if acc.PlatformName != "" {
		existing.PlatformName = acc.PlatformName
	}
	if acc.URL != "" {
		existing.URL = acc.URL
	}
	if acc.Category != "" {
		existing.Category = acc.Category
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
	// 密码和 TOTP：空字符串表示保留原值
	if acc.Password.GetPlaintext() != "" {
		existing.Password = acc.Password
	}
	if acc.TOTPSecret.GetPlaintext() != "" {
		existing.TOTPSecret = acc.TOTPSecret
	}
	if acc.Visibility != "" {
		existing.Visibility = acc.Visibility
	}
	if acc.Status != "" {
		existing.Status = acc.Status
	}
	if acc.ExpiresAt != nil {
		existing.ExpiresAt = acc.ExpiresAt
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&existing).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, existing.GroupName); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypePlatformAccount, existing.ID, existing.Name, "")
	}); err != nil {
		return PlatformAccountView{}, fmt.Errorf("update platform account: %w", err)
	}

	var updated model.PlatformAccount
	if err := s.db.Preload("Owner").First(&updated, "id = ?", id).Error; err != nil {
		return PlatformAccountView{}, nil
	}
	return s.platformAccountView(updated), nil
}

func (s *DBStore) DeletePlatformAccount(id string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var acc model.PlatformAccount
		if err := tx.First(&acc, "id = ?", id).Error; err != nil {
			return fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypePlatformAccount, acc.ID); err != nil {
			return err
		}
		// 级联删除 shares
		if err := tx.Where("platform_account_id = ?", id).Delete(&model.PlatformAccountShare{}).Error; err != nil {
			return err
		}
		return tx.Delete(&acc).Error
	})
}

func (s *DBStore) GetPlatformAccountPassword(id string) (string, error) {
	var acc model.PlatformAccount
	if err := s.db.First(&acc, "id = ?", id).Error; err != nil {
		return "", fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
	}
	return acc.Password.GetPlaintext(), nil
}

func (s *DBStore) PlatformAccountShares(accountID string) ([]PlatformAccountShareView, error) {
	var shares []model.PlatformAccountShare
	if err := s.db.
		Where("platform_account_id = ?", accountID).
		Preload("User").Preload("Role").
		Order("created_at ASC").
		Find(&shares).Error; err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	views := make([]PlatformAccountShareView, len(shares))
	for i := range shares {
		views[i] = s.platformAccountShareView(shares[i])
	}
	return views, nil
}

func (s *DBStore) AddPlatformAccountShare(share model.PlatformAccountShare) (PlatformAccountShareView, error) {
	if share.UserID == "" && share.RoleID == "" {
		return PlatformAccountShareView{}, errors.New("user_id or role_id is required")
	}
	if share.AccessLevel == "" {
		share.AccessLevel = "view"
	}

	if err := s.db.Create(&share).Error; err != nil {
		return PlatformAccountShareView{}, fmt.Errorf("create share: %w", err)
	}

	var created model.PlatformAccountShare
	if err := s.db.Where("id = ?", share.ID).Preload("User").Preload("Role").First(&created).Error; err != nil {
		return PlatformAccountShareView{}, nil
	}
	return s.platformAccountShareView(created), nil
}

func (s *DBStore) DeletePlatformAccountShare(accountID, shareID string) error {
	result := s.db.Where("id = ? AND platform_account_id = ?", shareID, accountID).Delete(&model.PlatformAccountShare{})
	if result.Error != nil {
		return fmt.Errorf("delete share: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %q", ErrPlatformShareNotFound, shareID)
	}
	return nil
}

func (s *DBStore) GetPlatformAccountSharesForUser(userID string, roleIDs []string) ([]PlatformAccountShareView, error) {
	var shares []model.PlatformAccountShare
	q := s.db.Where("user_id = ? OR role_id IN (?)", userID, roleIDs)
	if err := q.Preload("User").Preload("Role").Find(&shares).Error; err != nil {
		return nil, fmt.Errorf("list user shares: %w", err)
	}
	views := make([]PlatformAccountShareView, len(shares))
	for i := range shares {
		views[i] = s.platformAccountShareView(shares[i])
	}
	return views, nil
}
