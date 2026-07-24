package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	q := s.db.WithContext(ctx).Model(&model.PlatformAccount{}).Scopes(ActiveScope)
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

	query := q.Preload("Owner", ActiveScope).Order("created_at DESC")
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

// ListPlatformAccountMetadata returns credential-free records for service
// authorization and presentation. Password is intentionally omitted from the
// select list; HasPassword is a database-side marker, not a decrypted secret.
func (s *DBStore) ListPlatformAccountMetadata(ctx context.Context, search, platform string) ([]model.PlatformAccount, error) {
	query := s.platformAccountMetadataQuery(ctx)
	if platform = strings.TrimSpace(platform); platform != "" {
		query = query.Where("platform_name = ?", platform)
	}
	if search = strings.TrimSpace(search); search != "" {
		like := "%" + search + "%"
		query = query.Where("name LIKE ? OR platform_name LIKE ? OR username LIKE ? OR url LIKE ? OR group_name LIKE ?", like, like, like, like, like)
	}
	var accounts []model.PlatformAccount
	if err := query.Order("created_at DESC").Find(&accounts).Error; err != nil {
		return nil, fmt.Errorf("list platform account metadata: %w", err)
	}
	return accounts, nil
}

// GetPlatformAccountMetadata is the credential-free counterpart to password
// retrieval and must be used before lifecycle checks on use paths.
func (s *DBStore) GetPlatformAccountMetadata(ctx context.Context, id string) (model.PlatformAccount, error) {
	var account model.PlatformAccount
	if err := s.platformAccountMetadataQuery(ctx).First(&account, "platform_accounts.id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.PlatformAccount{}, fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
		}
		return model.PlatformAccount{}, fmt.Errorf("get platform account metadata: %w", err)
	}
	return account, nil
}

func (s *DBStore) PlatformAccount(ctx context.Context, id string) (PlatformAccountView, error) {
	var a model.PlatformAccount
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Preload("Owner", ActiveScope).First(&a, "id = ?", id).Error; err != nil {
		return PlatformAccountView{}, fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
	}
	return s.platformAccountView(a), nil
}

func (s *DBStore) AddPlatformAccount(ctx context.Context, acc model.PlatformAccount) (PlatformAccountView, error) {
	created, err := s.createPlatformAccount(ctx, acc, "")
	if err != nil {
		return PlatformAccountView{}, err
	}
	return s.platformAccountView(created), nil
}

// CreateManagedPlatformAccount atomically creates the account, its resource,
// and the non-super-administrator creator grant.
func (s *DBStore) CreateManagedPlatformAccount(ctx context.Context, acc model.PlatformAccount, creatorID string) (model.PlatformAccount, error) {
	return s.createPlatformAccount(ctx, acc, creatorID)
}

func (s *DBStore) createPlatformAccount(ctx context.Context, acc model.PlatformAccount, creatorID string) (model.PlatformAccount, error) {
	if acc.Username == "" {
		return model.PlatformAccount{}, errors.New("username is required")
	}
	if acc.Name == "" {
		acc.Name = acc.Username
	}
	if acc.Status == "" {
		acc.Status = "active"
	}
	acc.GroupName = strings.TrimSpace(acc.GroupName)

	var created model.PlatformAccount
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
		creatorID = strings.TrimSpace(creatorID)
		if creatorID != "" {
			var creatorCount int64
			if err := tx.Model(&model.User{}).Scopes(ActiveScope).Where("id = ?", creatorID).Count(&creatorCount).Error; err != nil {
				return fmt.Errorf("check platform account creator: %w", err)
			}
			if creatorCount == 0 {
				return fmt.Errorf("platform account creator not found: %q", creatorID)
			}
			grant := model.ResourceGrant{PrincipalType: "user", PrincipalID: creatorID, ResourceType: model.ResourceTypePlatformAccount, ResourceID: acc.ID, Effect: model.PermissionEffectAllow}
			if err := tx.Scopes(ActiveScope).Where(&model.ResourceGrant{PrincipalType: grant.PrincipalType, PrincipalID: grant.PrincipalID, ResourceType: grant.ResourceType, ResourceID: grant.ResourceID, Effect: grant.Effect}).FirstOrCreate(&grant).Error; err != nil {
				return fmt.Errorf("create platform account creator grant: %w", err)
			}
		}
		return s.loadPlatformAccountMetadataTx(tx, acc.ID, &created)
	}); err != nil {
		return model.PlatformAccount{}, fmt.Errorf("create platform account: %w", err)
	}
	return created, nil
}

func (s *DBStore) UpdatePlatformAccount(ctx context.Context, id string, acc model.PlatformAccount) (PlatformAccountView, error) {
	updated, err := s.updatePlatformAccount(ctx, id, acc)
	if err != nil {
		return PlatformAccountView{}, err
	}
	return s.platformAccountView(updated), nil
}

func (s *DBStore) UpdateManagedPlatformAccount(ctx context.Context, id string, acc model.PlatformAccount) (model.PlatformAccount, error) {
	return s.updatePlatformAccount(ctx, id, acc)
}

func (s *DBStore) updatePlatformAccount(ctx context.Context, id string, acc model.PlatformAccount) (model.PlatformAccount, error) {
	id = strings.TrimSpace(id)
	var updated model.PlatformAccount
	deletedBeforeUpdate := false
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.PlatformAccount
		if err := tx.Scopes(ActiveScope).Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
			}
			return fmt.Errorf("lock platform account for update: %w", err)
		}
		result := tx.Model(&model.PlatformAccount{}).Scopes(ActiveScope).
			Where("id = ?", id).
			Updates(platformAccountUpdateFields(acc))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			deletedBeforeUpdate = true
			return nil
		}
		if err := s.loadPlatformAccountMetadataTx(tx, id, &updated); err != nil {
			return fmt.Errorf("load updated platform account: %w", err)
		}
		if err := ensureAccountGroup(tx, updated.GroupName); err != nil {
			return err
		}
		if err := s.syncResourceTx(tx, model.ResourceTypePlatformAccount, updated.ID, updated.Name, ""); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return model.PlatformAccount{}, fmt.Errorf("update platform account: %w", err)
	}
	if deletedBeforeUpdate {
		return model.PlatformAccount{}, fmt.Errorf("update platform account: %w: %q", ErrPlatformAccountNotFound, id)
	}
	return updated, nil
}

func (s *DBStore) DeletePlatformAccount(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var acc model.PlatformAccount
		if err := tx.Scopes(ActiveScope).Clauses(clause.Locking{Strength: "UPDATE"}).First(&acc, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
			}
			return fmt.Errorf("lock platform account for delete: %w", err)
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypePlatformAccount, acc.ID); err != nil {
			return err
		}
		result := softDeleteWhere(ctx, tx, "platform_accounts", "id = ?", id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
		}
		return nil
	})
}

func (s *DBStore) DeleteManagedPlatformAccount(ctx context.Context, id string) error {
	return s.DeletePlatformAccount(ctx, id)
}

func (s *DBStore) GetPlatformAccountPassword(ctx context.Context, id string) (string, error) {
	var acc model.PlatformAccount
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).First(&acc, "id = ?", id).Error; err != nil {
		return "", fmt.Errorf("%w: %q", ErrPlatformAccountNotFound, id)
	}
	return acc.Password.GetPlaintext(), nil
}

func (s *DBStore) platformAccountMetadataQuery(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).
		Model(&model.PlatformAccount{}).Scopes(ActiveScope).
		Select(`platform_accounts.id, platform_accounts.name, platform_accounts.platform_name, platform_accounts.url,
			platform_accounts.group_name, platform_accounts.username, platform_accounts.remark, platform_accounts.owner_id,
			platform_accounts.status, platform_accounts.expires_at, platform_accounts.created_at, platform_accounts.updated_at,
			CASE WHEN platform_accounts.password IS NULL OR platform_accounts.password = '' THEN 0 ELSE 1 END AS has_password`).
		Preload("Owner", ActiveScope)
}

func (s *DBStore) loadPlatformAccountMetadataTx(tx *gorm.DB, id string, destination *model.PlatformAccount) error {
	return tx.Model(&model.PlatformAccount{}).Scopes(ActiveScope).
		Select(`platform_accounts.id, platform_accounts.name, platform_accounts.platform_name, platform_accounts.url,
			platform_accounts.group_name, platform_accounts.username, platform_accounts.remark, platform_accounts.owner_id,
			platform_accounts.status, platform_accounts.expires_at, platform_accounts.created_at, platform_accounts.updated_at,
			CASE WHEN platform_accounts.password IS NULL OR platform_accounts.password = '' THEN 0 ELSE 1 END AS has_password`).
		Preload("Owner", ActiveScope).First(destination, "platform_accounts.id = ?", id).Error
}

func platformAccountUpdateFields(acc model.PlatformAccount) map[string]any {
	updates := make(map[string]any, 10)
	updates["updated_at"] = time.Now().UTC()
	if value := strings.TrimSpace(acc.PlatformName); value != "" {
		updates["platform_name"] = value
	}
	if value := strings.TrimSpace(acc.URL); value != "" {
		updates["url"] = value
	}
	if value := strings.TrimSpace(acc.GroupName); value != "" {
		updates["group_name"] = value
	}
	if value := strings.TrimSpace(acc.Username); value != "" {
		updates["username"] = value
	}
	if value := strings.TrimSpace(acc.Name); value != "" {
		updates["name"] = value
	}
	if value := strings.TrimSpace(acc.Remark); value != "" {
		updates["remark"] = value
	}
	if acc.Password.GetPlaintext() != "" {
		updates["password"] = acc.Password
	}
	if value := strings.TrimSpace(acc.Status); value != "" {
		updates["status"] = value
	}
	if acc.ExpiresAt != nil {
		updates["expires_at"] = acc.ExpiresAt
	}
	return updates
}
