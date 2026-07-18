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

func (s *DBStore) CreateTemporaryAccess(ctx context.Context, input service.CreateTemporaryAccessInput) (service.TemporaryAccessResult, error) {
	var result service.TemporaryAccessResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := verifyTemporaryAccessUser(tx, input.AuthorizedUserID, input.Now); err != nil {
			return err
		}
		if err := verifyTemporaryAccessResource(tx, input.ResourceType, input.ResourceID); err != nil {
			return err
		}
		sessionSeq, sessionID, err := nextTemporarySessionID(tx)
		if err != nil {
			return err
		}
		session := model.UserSession{UserID: input.AuthorizedUserID, SessionSeq: sessionSeq, SessionID: sessionID, Type: "temporary", Status: "active", ExpiresAt: &input.ExpiresAt, CreatedBy: input.CreatedBy}
		if err := tx.Create(&session).Error; err != nil {
			return fmt.Errorf("create temporary user session: %w", err)
		}
		account := temporaryAccountFromInput(service.CreateTemporaryAccountInput{AccountType: model.TemporaryAccountTypeUser, AuthorizedUserID: input.AuthorizedUserID, ExpiresAt: &input.ExpiresAt, Remark: input.Remark, CreatedBy: input.CreatedBy, SessionID: sessionID, Now: input.Now})
		if err := tx.Create(&account).Error; err != nil {
			return fmt.Errorf("create temporary account: %w", err)
		}
		grant := model.TemporaryAccountGrant{TemporaryAccountID: account.ID, UserID: input.AuthorizedUserID, Action: "session:connect", ResourceType: input.ResourceType, ResourceID: input.ResourceID, StartsAt: &input.Now, ExpiresAt: &input.ExpiresAt, CreatedBy: input.CreatedBy}
		if err := tx.Create(&grant).Error; err != nil {
			return fmt.Errorf("create temporary account grant: %w", err)
		}
		if err := tx.Create(&input.ConnectionPassword).Error; err != nil {
			return fmt.Errorf("create temporary connection password: %w", err)
		}
		result = service.TemporaryAccessResult{Account: account, Grant: grant}
		return nil
	})
	if err != nil {
		return service.TemporaryAccessResult{}, err
	}
	return result, nil
}

func (s *DBStore) ExtendTemporaryAccess(ctx context.Context, id string, expiresAt time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		account, err := findTemporaryAccount(tx, id)
		if err != nil {
			return err
		}
		updated := tx.Model(&model.TemporaryAccount{}).Where("id = ?", account.ID).Updates(map[string]any{"expires_at": expiresAt, "status": "active"})
		if err := requireTemporaryAccessUpdate(updated, "extend temporary account"); err != nil {
			return err
		}
		if account.Type == model.TemporaryAccountTypeUser {
			updated = tx.Model(&model.TemporaryAccountGrant{}).Where("temporary_account_id = ?", account.ID).Updates(map[string]any{"expires_at": expiresAt, "revoked_at": nil})
			if err := requireTemporaryAccessUpdate(updated, "extend temporary account grant"); err != nil {
				return err
			}
			updated = tx.Model(&model.UserSession{}).Where("session_id = ?", account.SessionID).Updates(map[string]any{"expires_at": expiresAt, "status": "active"})
			if err := requireTemporaryAccessUpdate(updated, "extend temporary user session"); err != nil {
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
		}
		if account.Type == model.TemporaryAccountTypeAI {
			updated = tx.Model(&model.AIAccessToken{}).Where("temporary_account_id = ?", account.ID).Update("revoked_at", now)
			if err := requireTemporaryAccessUpdate(updated, "revoke temporary AI token"); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *DBStore) CreateTemporaryAccount(ctx context.Context, input service.CreateTemporaryAccountInput) (model.TemporaryAccount, error) {
	var account model.TemporaryAccount
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sessionID := input.SessionID
		if sessionID == "" {
			var err error
			_, sessionID, err = nextTemporarySessionID(tx)
			if err != nil {
				return err
			}
		}
		input.SessionID = sessionID
		account = temporaryAccountFromInput(input)
		if err := tx.Create(&account).Error; err != nil {
			return fmt.Errorf("create temporary account: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.TemporaryAccount{}, err
	}
	return account, nil
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

func nextTemporarySessionID(tx *gorm.DB) (int, string, error) {
	var maxSequence int
	if err := tx.Model(&model.UserSession{}).Select("COALESCE(MAX(session_seq), 0)").Scan(&maxSequence).Error; err != nil {
		return 0, "", fmt.Errorf("read temporary session sequence floor: %w", err)
	}
	var sequence model.ResourceSequence
	result := tx.Where("name = ?", storage.SequenceUserSession).First(&sequence)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return 0, "", fmt.Errorf("find temporary session sequence: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		sequence = model.ResourceSequence{Name: storage.SequenceUserSession, NextValue: maxSequence + 1}
		if sequence.NextValue < 1 {
			sequence.NextValue = 1
		}
		if err := tx.Create(&sequence).Error; err != nil {
			return 0, "", fmt.Errorf("create temporary session sequence: %w", err)
		}
	}
	previousNextValue := sequence.NextValue
	value := previousNextValue
	if value < maxSequence+1 {
		value = maxSequence + 1
	}
	if value > storage.MaxCompactSessionSeq {
		return 0, "", fmt.Errorf("temporary session sequence exhausted at %d", storage.MaxCompactSessionSeq)
	}
	updated := tx.Model(&model.ResourceSequence{}).Where("name = ? AND next_value = ?", sequence.Name, previousNextValue).Update("next_value", value+1)
	if err := requireTemporaryAccessUpdate(updated, "advance temporary session sequence"); err != nil {
		return 0, "", err
	}
	return value, util.EncodeBase62Padded(uint64(value), 5), nil
}

func temporaryAccountFromInput(input service.CreateTemporaryAccountInput) model.TemporaryAccount {
	sessionID := input.SessionID
	usernameSuffix := sessionID
	if len(usernameSuffix) > 12 {
		usernameSuffix = usernameSuffix[:12]
	}
	return model.TemporaryAccount{ID: model.NewID(), SessionID: sessionID, Type: input.AccountType, Username: "tmp_" + usernameSuffix, AuthorizedUserID: input.AuthorizedUserID, Status: "active", StartsAt: input.Now.UTC(), ExpiresAt: input.ExpiresAt, Remark: strings.TrimSpace(input.Remark), CreatedBy: input.CreatedBy}
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
