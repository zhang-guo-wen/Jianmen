package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const adminEncryptionKeyClaim = "encryption_key_claimed"

var sqliteAdminAuthWriteSlot = func() chan struct{} {
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	return slot
}()

func (s *DBStore) AdminInitialized(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).Limit(1).Count(&count).Error; err != nil {
		return false, fmt.Errorf("count admin users: %w", err)
	}
	return count > 0, nil
}

func (s *DBStore) FindAdminLoginCredential(
	ctx context.Context,
	username string,
) (service.AdminLoginCredential, bool, error) {
	if err := ctx.Err(); err != nil {
		return service.AdminLoginCredential{}, false, err
	}
	var user model.User
	err := s.db.WithContext(ctx).Where("username = ?", strings.TrimSpace(username)).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.AdminLoginCredential{}, false, nil
	}
	if err != nil {
		return service.AdminLoginCredential{}, false, fmt.Errorf("find admin login credential: %w", err)
	}
	return service.AdminLoginCredential{
		UserID:          user.ID,
		Username:        user.Username,
		PasswordHash:    user.PasswordHash,
		MySQLNativeHash: user.MySQLNativeHash,
		Status:          user.Status,
		ExpiresAt:       user.ExpiresAt,
	}, true, nil
}

func (s *DBStore) PersistAdminLoginState(
	ctx context.Context,
	userID string,
	mysqlNativeHash string,
	loggedInAt time.Time,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&model.User{}).
		Where(
			"id = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)",
			strings.TrimSpace(userID), "active", loggedInAt,
		).
		Updates(map[string]any{
			"last_login_at": loggedInAt,
			"my_sql_native_hash": gorm.Expr(
				"CASE WHEN my_sql_native_hash IS NULL OR my_sql_native_hash = '' THEN ? ELSE my_sql_native_hash END",
				mysqlNativeHash,
			),
		})
	if result.Error != nil {
		return fmt.Errorf("persist admin login state: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return service.ErrAdminInvalidCredentials
	}
	return nil
}

func (s *DBStore) SetupInitialAdmin(ctx context.Context, record service.AdminSetupRecord) error {
	return s.withAdminAuthWrite(ctx, func() error {
		alreadyInitialized := false
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			guard := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.SystemInitialization{
				Key:       model.SystemInitializationSetup,
				CreatedAt: record.CreatedAt,
			})
			if guard.Error != nil {
				return guard.Error
			}
			if guard.RowsAffected == 0 {
				alreadyInitialized = true
				return nil
			}

			var count int64
			if err := tx.Model(&model.User{}).Limit(1).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				alreadyInitialized = true
				return nil
			}

			user := model.User{
				ID:              record.UserID,
				Username:        record.Username,
				PasswordHash:    record.PasswordHash,
				MySQLNativeHash: record.MySQLNativeHash,
				DisplayName:     record.DisplayName,
				Email:           record.Email,
				Status:          record.Status,
				IsSuperAdmin:    record.SuperAdmin,
				CreatedAt:       record.CreatedAt,
			}
			return tx.Create(&user).Error
		})
		if err != nil {
			return err
		}
		if alreadyInitialized {
			return service.ErrAdminAlreadyInitialized
		}
		return nil
	})
}

func (s *DBStore) ValidateAdminEncryptionKeyClaimer(ctx context.Context, userID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var initialized int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).Limit(1).Count(&initialized).Error; err != nil {
		return fmt.Errorf("check admin setup: %w", err)
	}
	if initialized == 0 {
		return service.ErrAdminSetupNotCompleted
	}
	var authorized int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ? AND status = ? AND is_super_admin = ?", strings.TrimSpace(userID), "active", true).
		Limit(1).
		Count(&authorized).Error; err != nil {
		return fmt.Errorf("validate admin encryption key claimer: %w", err)
	}
	if authorized != 1 {
		return service.ErrAdminEncryptionKeyDenied
	}
	return nil
}

func (s *DBStore) ClaimAdminEncryptionKey(ctx context.Context, userID string, claimedAt time.Time) error {
	return s.withAdminAuthWrite(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var authorized int64
			if err := tx.Model(&model.User{}).
				Where("id = ? AND status = ? AND is_super_admin = ?", strings.TrimSpace(userID), "active", true).
				Limit(1).
				Count(&authorized).Error; err != nil {
				return err
			}
			if authorized != 1 {
				return service.ErrAdminEncryptionKeyDenied
			}

			claim := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.SystemInitialization{
				Key:       adminEncryptionKeyClaim,
				CreatedAt: claimedAt,
			})
			if claim.Error != nil {
				return claim.Error
			}
			if claim.RowsAffected != 1 {
				return service.ErrAdminEncryptionKeyClaimed
			}
			return nil
		})
	})
}

func (s *DBStore) withAdminAuthWrite(ctx context.Context, operation func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.db.Dialector.Name() == "sqlite" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sqliteAdminAuthWriteSlot:
		}
		defer func() { sqliteAdminAuthWriteSlot <- struct{}{} }()
	}

	const maxAttempts = 8
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = operation()
		if lastErr == nil || !isRetryableAdminAuthError(lastErr) {
			return lastErr
		}
		delay := time.Duration(5*(1<<attempt)) * time.Millisecond
		if delay > 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("admin auth write retries exhausted: %w", lastErr)
}

func isRetryableAdminAuthError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"database is locked",
		"database table is locked",
		"sqlite_busy",
		"sqlite_locked",
		"deadlock",
		"serialization failure",
		"could not serialize",
		"lock wait timeout",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
