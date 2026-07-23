package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"
	"jianmen/internal/util"

	"gorm.io/gorm"
)

// SQLite permits only one writer and deferred transactions can fail immediately
// with SQLITE_BUSY_SNAPSHOT while upgrading from a read to a write transaction.
// Serializing this aggregate in-process avoids making the sequence transaction
// and aggregate transaction invalidate each other's snapshots.
var sqliteTemporaryAccessWriteSlot = func() chan struct{} {
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	return slot
}()

func (s *DBStore) CreateTemporaryAccess(ctx context.Context, input service.CreateTemporaryAccessInput) (service.TemporaryAccessResult, error) {
	var result service.TemporaryAccessResult
	err := s.withTemporarySessionIdentity(ctx, func(sessionSeq int, sessionID string) error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := verifyTemporaryAccessUser(tx, input.AuthorizedUserID, input.Now); err != nil {
				return err
			}
			if err := verifyTemporaryAccessResource(tx, input.ResourceType, input.ResourceID); err != nil {
				return err
			}
			session := model.UserSession{UserID: input.AuthorizedUserID, SessionSeq: sessionSeq, SessionID: sessionID, Type: "temporary", Status: "active", ExpiresAt: &input.ExpiresAt, FullAudit: model.FullAudit{CreatedBy: input.CreatedBy}}
			if err := tx.Create(&session).Error; err != nil {
				return fmt.Errorf("create temporary user session: %w", err)
			}
			account := temporaryAccountFromValues(
				model.TemporaryAccountTypeUser,
				input.AuthorizedUserID,
				&input.ExpiresAt,
				input.Remark,
				input.CreatedBy,
				sessionID,
				input.Now,
			)
			if err := tx.Create(&account).Error; err != nil {
				return fmt.Errorf("create temporary account: %w", err)
			}
			grant := model.TemporaryAccountGrant{TemporaryAccountID: account.ID, UserID: input.AuthorizedUserID, Action: "session:connect", ResourceType: input.ResourceType, ResourceID: input.ResourceID, StartsAt: &input.Now, ExpiresAt: &input.ExpiresAt, FullAudit: model.FullAudit{CreatedBy: input.CreatedBy}}
			if err := tx.Create(&grant).Error; err != nil {
				return fmt.Errorf("create temporary account grant: %w", err)
			}
			input.ConnectionPassword.UserID = input.AuthorizedUserID
			input.ConnectionPassword.ResourceType = input.ResourceType
			input.ConnectionPassword.ResourceID = input.ResourceID
			input.ConnectionPassword.TemporaryAccountID = account.ID
			if err := tx.Create(&input.ConnectionPassword).Error; err != nil {
				return fmt.Errorf("create temporary connection password: %w", err)
			}
			result = service.TemporaryAccessResult{Account: account, Grant: grant}
			return nil
		})
	})
	if err != nil {
		return service.TemporaryAccessResult{}, err
	}
	return result, nil
}

func (s *DBStore) CreateTemporaryAIAccess(ctx context.Context, input service.CreateTemporaryAIAccessInput) (service.TemporaryAIAccessResult, error) {
	var result service.TemporaryAIAccessResult
	err := s.withTemporarySessionIdentity(ctx, func(sessionSeq int, sessionID string) error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := verifyTemporaryAccessUser(tx, input.UserID, input.Now); err != nil {
				return err
			}
			// 创建 UserSession 记录，使 FindUserSessionBySessionID 可以查到 AI 临时会话
			session := model.UserSession{UserID: input.UserID, SessionSeq: sessionSeq, SessionID: sessionID, Type: "temporary", Status: "active", ExpiresAt: input.ExpiresAt, FullAudit: model.FullAudit{CreatedBy: input.CreatedBy}}
			if err := tx.Create(&session).Error; err != nil {
				return fmt.Errorf("create temporary AI user session: %w", err)
			}
			account := temporaryAccountFromValues(
				model.TemporaryAccountTypeAI,
				input.UserID,
				input.ExpiresAt,
				input.Remark,
				input.CreatedBy,
				sessionID,
				input.Now,
			)
			if err := tx.Create(&account).Error; err != nil {
				return fmt.Errorf("create temporary AI account: %w", err)
			}
			input.Token.TemporaryAccountID = account.ID
			if err := tx.Create(&input.Token).Error; err != nil {
				return fmt.Errorf("create temporary AI token: %w", err)
			}
			result = service.TemporaryAIAccessResult{Account: account, Token: input.Token}
			return nil
		})
	})
	if err != nil {
		return service.TemporaryAIAccessResult{}, err
	}
	return result, nil
}

func (s *DBStore) ExtendTemporaryAccess(ctx context.Context, id string, expiresAt, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		account, err := findTemporaryAccount(tx, id)
		if err != nil {
			return err
		}
		if account.Status != "active" || (account.ExpiresAt != nil && !account.ExpiresAt.After(now)) {
			return service.ErrTemporaryAccessInactive
		}
		updated := tx.Model(&model.TemporaryAccount{}).
			Where("id = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)", account.ID, "active", now).
			Update("expires_at", expiresAt)
		if err := requireTemporaryAccessActiveUpdate(updated, "extend temporary account"); err != nil {
			return err
		}
		if account.Type == model.TemporaryAccountTypeUser {
			updated = tx.Model(&model.TemporaryAccountGrant{}).Where("temporary_account_id = ? AND revoked_at IS NULL", account.ID).Update("expires_at", expiresAt)
			if err := requireTemporaryAccessUpdate(updated, "extend temporary account grant"); err != nil {
				return err
			}
			updated = tx.Model(&model.UserSession{}).Where("session_id = ? AND status = ?", account.SessionID, "active").Update("expires_at", expiresAt)
			if err := requireTemporaryAccessUpdate(updated, "extend temporary user session"); err != nil {
				return err
			}
			updated = tx.Model(&model.ConnectionPassword{}).
				Where("temporary_account_id = ? AND revoked_at IS NULL", account.ID).
				Update("expires_at", expiresAt)
			if err := requireTemporaryAccessUpdate(updated, "extend temporary connection password"); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *DBStore) DisableTemporaryAccess(ctx context.Context, id string, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		account, err := findTemporaryAccount(tx, id)
		if err != nil {
			return err
		}
		updated := tx.Model(&model.TemporaryAccount{}).Where("id = ?", account.ID).Update("status", "disabled")
		if err := requireTemporaryAccessUpdate(updated, "disable temporary account"); err != nil {
			return err
		}
		if account.Type == model.TemporaryAccountTypeUser {
			updated = tx.Model(&model.TemporaryAccountGrant{}).Where("temporary_account_id = ?", account.ID).Update("revoked_at", now)
			if err := requireTemporaryAccessUpdate(updated, "revoke temporary account grant"); err != nil {
				return err
			}
			updated = tx.Model(&model.UserSession{}).Where("session_id = ?", account.SessionID).Update("status", "disabled")
			if err := requireTemporaryAccessUpdate(updated, "disable temporary user session"); err != nil {
				return err
			}
			updated = tx.Model(&model.ConnectionPassword{}).
				Where("temporary_account_id = ? AND revoked_at IS NULL", account.ID).
				Update("revoked_at", now)
			if err := requireTemporaryAccessUpdate(updated, "revoke temporary connection password"); err != nil {
				return err
			}
		}
		if account.Type == model.TemporaryAccountTypeAI {
			updated = tx.Model(&model.AIAccessToken{}).Where("temporary_account_id = ?", account.ID).Update("revoked_at", now)
			if updated.Error != nil {
				return fmt.Errorf("revoke temporary AI token: %w", updated.Error)
			}
			// 同时禁用对应的 UserSession，防止会话被继续使用
			updated = tx.Model(&model.UserSession{}).Where("session_id = ?", account.SessionID).Update("status", "disabled")
			if updated.Error != nil {
				return fmt.Errorf("disable temporary AI user session: %w", updated.Error)
			}
		}
		return nil
	})
}

func (s *DBStore) ListTemporaryAccess(ctx context.Context, params service.TemporaryAccessListParams) (service.TemporaryAccessPage, error) {
	query := s.db.WithContext(ctx).Model(&model.TemporaryAccount{})
	if params.Query != "" {
		like := "%" + params.Query + "%"
		query = query.Where("session_id LIKE ? OR username LIKE ? OR remark LIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return service.TemporaryAccessPage{}, fmt.Errorf("count temporary access: %w", err)
	}
	var accounts []model.TemporaryAccount
	if err := query.Order("created_at DESC").
		Offset((params.Page - 1) * params.PageSize).
		Limit(params.PageSize).
		Find(&accounts).Error; err != nil {
		return service.TemporaryAccessPage{}, fmt.Errorf("load temporary access page: %w", err)
	}
	items := make([]service.TemporaryAccessDetails, 0, len(accounts))
	for _, account := range accounts {
		details, err := loadTemporaryAccessDetails(s.db.WithContext(ctx), account, params.Now)
		if err != nil {
			return service.TemporaryAccessPage{}, err
		}
		items = append(items, details)
	}
	return service.TemporaryAccessPage{Items: items, Total: int(total)}, nil
}

func (s *DBStore) GetTemporaryAccess(ctx context.Context, id string) (service.TemporaryAccessDetails, error) {
	account, err := findTemporaryAccount(s.db.WithContext(ctx), id)
	if err != nil {
		return service.TemporaryAccessDetails{}, err
	}
	return loadTemporaryAccessDetails(s.db.WithContext(ctx), account, time.Now().UTC())
}

func (s *DBStore) TemporaryConnectionTarget(ctx context.Context, resourceType, resourceID string) (service.TemporaryConnectionTarget, error) {
	switch resourceType {
	case model.ResourceTypeHostAccount:
		var account model.HostAccount
		if err := s.db.WithContext(ctx).Where("id = ?", resourceID).First(&account).Error; err != nil {
			return service.TemporaryConnectionTarget{}, mapTemporaryAccessReadError(err, "find host account connection target")
		}
		return service.TemporaryConnectionTarget{
			ResourceType: resourceType, ResourceID: resourceID,
			UsernamePrefix: util.PrefixHost, CompactResourceID: account.ResourceID,
			Protocol: "ssh", Gateway: service.TemporaryGatewaySSH,
		}, nil
	case model.ResourceTypeDatabaseAccount:
		var account model.DatabaseAccount
		if err := s.db.WithContext(ctx).Preload("Instance").Where("id = ?", resourceID).First(&account).Error; err != nil {
			return service.TemporaryConnectionTarget{}, mapTemporaryAccessReadError(err, "find database account connection target")
		}
		prefix := util.PrefixDatabase
		if strings.EqualFold(account.Instance.Protocol, "redis") {
			prefix = util.PrefixRedis
		}
		return service.TemporaryConnectionTarget{
			ResourceType: resourceType, ResourceID: resourceID,
			UsernamePrefix: prefix, CompactResourceID: account.ResourceID,
			Protocol: account.Instance.Protocol, Gateway: service.TemporaryGatewayDatabase,
		}, nil
	default:
		return service.TemporaryConnectionTarget{}, fmt.Errorf("%w: unsupported resource type", service.ErrInvalidTemporaryAccess)
	}
}

func verifyTemporaryAccessUser(tx *gorm.DB, userID string, now time.Time) error {
	var user model.User
	if err := tx.Where("id = ? AND status = ?", userID, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: authorized user", service.ErrTemporaryAccessNotFound)
		}
		return fmt.Errorf("find authorized user: %w", err)
	}
	if user.IsExpired(now) {
		return fmt.Errorf("%w: authorized user account expired", service.ErrInvalidTemporaryAccess)
	}
	return nil
}

func verifyTemporaryAccessResource(tx *gorm.DB, resourceType, resourceID string) error {
	var count int64
	var err error
	switch resourceType {
	case model.ResourceTypeHostAccount:
		err = tx.Model(&model.HostAccount{}).Where("id = ?", resourceID).Count(&count).Error
	case model.ResourceTypeDatabaseAccount:
		err = tx.Model(&model.DatabaseAccount{}).Where("id = ?", resourceID).Count(&count).Error
	default:
		return fmt.Errorf("%w: unsupported resource type", service.ErrInvalidTemporaryAccess)
	}
	if err != nil {
		return fmt.Errorf("find temporary access resource: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("%w: resource", service.ErrTemporaryAccessNotFound)
	}
	return nil
}

func (s *DBStore) withTemporarySessionIdentity(ctx context.Context, operation func(int, string) error) error {
	if s.db.Dialector.Name() == "sqlite" {
		select {
		case <-ctx.Done():
			return fmt.Errorf("serialize temporary access write: %w", ctx.Err())
		case <-sqliteTemporaryAccessWriteSlot:
		}
		defer func() { sqliteTemporaryAccessWriteSlot <- struct{}{} }()
	}

	const maxAttempts = 12
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		sessionSeq, sessionID, err := s.allocateTemporarySessionIdentity(ctx)
		if err == nil {
			err = operation(sessionSeq, sessionID)
		}
		if err == nil {
			return nil
		}
		if !isRetryableTemporaryAccessError(err) {
			return err
		}
		lastErr = err
		delay := time.Duration(5*(1<<attempt)) * time.Millisecond
		if delay > 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("allocate temporary session identity: %w", ctx.Err())
		case <-timer.C:
		}
	}
	return fmt.Errorf("temporary access transaction retries exhausted: %w", lastErr)
}

func (s *DBStore) allocateTemporarySessionIdentity(ctx context.Context) (int, string, error) {
	var maxSequence int
	db := s.db.WithContext(ctx)
	if err := db.Model(&model.UserSession{}).Select("COALESCE(MAX(session_seq), 0)").Scan(&maxSequence).Error; err != nil {
		return 0, "", fmt.Errorf("read temporary session sequence floor: %w", err)
	}
	if err := storage.EnsureSequenceNextValue(db, storage.SequenceUserSession, maxSequence+1); err != nil {
		return 0, "", fmt.Errorf("ensure temporary session sequence floor: %w", err)
	}
	value, err := storage.NextSequenceValue(db, storage.SequenceUserSession, storage.MaxCompactSessionSeq)
	if err != nil {
		return 0, "", fmt.Errorf("allocate temporary session sequence: %w", err)
	}
	return value, util.EncodeBase62Padded(uint64(value), 5), nil
}

func isRetryableTemporaryAccessError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"database is locked",
		"sqlite_busy",
		"deadlock",
		"serialization failure",
		"could not serialize",
		"lock wait timeout",
		"duplicate key",
		"unique constraint",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func temporaryAccountFromValues(accountType, authorizedUserID string, expiresAt *time.Time, remark, createdBy, sessionID string, now time.Time) model.TemporaryAccount {
	usernameSuffix := sessionID
	if len(usernameSuffix) > 12 {
		usernameSuffix = usernameSuffix[:12]
	}
	return model.TemporaryAccount{
		ID: model.NewID(), SessionID: sessionID, Type: accountType, Username: "tmp_" + usernameSuffix,
		AuthorizedUserID: authorizedUserID, Status: "active", StartsAt: now.UTC(), ExpiresAt: expiresAt,
		Remark: strings.TrimSpace(remark), FullAudit: model.FullAudit{CreatedBy: createdBy},
	}
}

func loadTemporaryAccessDetails(db *gorm.DB, account model.TemporaryAccount, now time.Time) (service.TemporaryAccessDetails, error) {
	details := service.TemporaryAccessDetails{
		ID: account.ID, SessionID: account.SessionID, Type: account.Type,
		AuthorizedUserID: account.AuthorizedUserID, Status: account.Status,
		StartsAt: account.StartsAt, ExpiresAt: account.ExpiresAt, Remark: account.Remark,
		CreatedBy: account.CreatedBy, CreatedAt: account.CreatedAt,
	}
	if details.Status == "active" && details.ExpiresAt != nil && !details.ExpiresAt.After(now) {
		details.Status = "disabled"
	}
	if account.AuthorizedUserID != "" {
		var user model.User
		err := db.Where("id = ?", account.AuthorizedUserID).First(&user).Error
		if err == nil {
			details.AuthorizedUser = user.DisplayName
			if details.AuthorizedUser == "" {
				details.AuthorizedUser = user.Username
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return service.TemporaryAccessDetails{}, fmt.Errorf("load temporary access user: %w", err)
		}
	}
	var grant model.TemporaryAccountGrant
	err := db.Where("temporary_account_id = ?", account.ID).Order("created_at DESC").First(&grant).Error
	if err == nil {
		details.ResourceType = grant.ResourceType
		details.ResourceID = grant.ResourceID
		resourceName, accountName, displayErr := temporaryAccessResourceNames(db, grant.ResourceType, grant.ResourceID)
		if displayErr != nil {
			return service.TemporaryAccessDetails{}, displayErr
		}
		details.ResourceName = resourceName
		details.AccountName = accountName
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return service.TemporaryAccessDetails{}, fmt.Errorf("load temporary access grant: %w", err)
	}
	return details, nil
}

func temporaryAccessResourceNames(db *gorm.DB, resourceType, resourceID string) (string, string, error) {
	switch resourceType {
	case model.ResourceTypeHostAccount:
		var account model.HostAccount
		err := db.Preload("Host").Where("id = ?", resourceID).First(&account).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", nil
		}
		if err != nil {
			return "", "", fmt.Errorf("load temporary host account display: %w", err)
		}
		return account.Host.Name, account.Name, nil
	case model.ResourceTypeDatabaseAccount:
		var account model.DatabaseAccount
		err := db.Preload("Instance").Where("id = ?", resourceID).First(&account).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", nil
		}
		if err != nil {
			return "", "", fmt.Errorf("load temporary database account display: %w", err)
		}
		return account.Instance.Name, account.UniqueName, nil
	default:
		return "", "", nil
	}
}

func mapTemporaryAccessReadError(err error, operation string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %s", service.ErrTemporaryAccessNotFound, operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func findTemporaryAccount(tx *gorm.DB, id string) (model.TemporaryAccount, error) {
	var account model.TemporaryAccount
	if err := tx.Where("id = ?", id).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.TemporaryAccount{}, service.ErrTemporaryAccessNotFound
		}
		return model.TemporaryAccount{}, fmt.Errorf("find temporary account: %w", err)
	}
	return account, nil
}

func requireTemporaryAccessUpdate(result *gorm.DB, operation string) error {
	if result.Error != nil {
		return fmt.Errorf("%s: %w", operation, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: %s affected %d rows", service.ErrTemporaryAccessNotFound, operation, result.RowsAffected)
	}
	return nil
}

func requireTemporaryAccessActiveUpdate(result *gorm.DB, operation string) error {
	if result.Error != nil {
		return fmt.Errorf("%s: %w", operation, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %s lost active state", service.ErrTemporaryAccessInactive, operation)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: %s affected %d rows", service.ErrTemporaryAccessNotFound, operation, result.RowsAffected)
	}
	return nil
}
