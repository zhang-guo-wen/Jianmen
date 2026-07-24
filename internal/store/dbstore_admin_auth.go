package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
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
	if err := s.db.WithContext(ctx).Model(&model.User{}).Scopes(ActiveScope).Limit(1).Count(&count).Error; err != nil {
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
	result := s.db.WithContext(ctx).Scopes(ActiveScope).
		Where("username = ?", strings.TrimSpace(username)).
		Limit(1).
		Find(&user)
	if result.Error != nil {
		return service.AdminLoginCredential{}, false, fmt.Errorf("find admin login credential: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return service.AdminLoginCredential{}, false, nil
	}
	return adminLoginCredential(user), true, nil
}

func (s *DBStore) FindAdminEncryptionKeyCredential(
	ctx context.Context,
	userID string,
) (service.AdminLoginCredential, bool, error) {
	if err := ctx.Err(); err != nil {
		return service.AdminLoginCredential{}, false, err
	}
	var user model.User
	result := s.db.WithContext(ctx).Scopes(ActiveScope).
		Where("id = ?", strings.TrimSpace(userID)).
		Limit(1).
		Find(&user)
	if result.Error != nil {
		return service.AdminLoginCredential{}, false, fmt.Errorf("find admin encryption key credential: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return service.AdminLoginCredential{}, false, nil
	}
	return adminLoginCredential(user), true, nil
}

func adminLoginCredential(user model.User) service.AdminLoginCredential {
	return service.AdminLoginCredential{
		UserID:          user.ID,
		Username:        user.Username,
		PasswordHash:    user.PasswordHash,
		MySQLNativeHash: user.MySQLNativeHash,
		Status:          user.Status,
		SuperAdmin:      user.IsSuperAdmin,
		ExpiresAt:       user.ExpiresAt,
	}
}

func (s *DBStore) PersistAdminLoginState(
	ctx context.Context,
	userID string,
	expectedPasswordHash string,
	mysqlNativeHash string,
	loggedInAt time.Time,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).
		Session(&gorm.Session{Logger: gormlogger.Discard}).
		Model(&model.User{}).Scopes(ActiveScope).
		Where(
			"id = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?) AND password_hash = ?",
			strings.TrimSpace(userID), "active", loggedInAt, expectedPasswordHash,
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

func (s *DBStore) SetupInitialAdmin(
	ctx context.Context,
	record service.AdminSetupRecord,
	session service.AdminSetupSessionRecord,
) error {
	return s.withAdminAuthWrite(ctx, func() error {
		alreadyInitialized := false
		err := s.db.WithContext(ctx).
			Session(&gorm.Session{Logger: gormlogger.Discard}).
			Transaction(func(tx *gorm.DB) error {
				guard := model.SystemInitialization{
					Key:       model.SystemInitializationSetup,
					FullAudit: model.FullAudit{CreatedAt: record.CreatedAt},
				}
				if err := tx.Create(&guard).Error; err != nil {
					if isUniqueConstraintError(err) {
						alreadyInitialized = true
						return nil
					}
					return fmt.Errorf("create admin setup guard: %w", err)
				}

				var count int64
				if err := tx.Model(&model.User{}).Scopes(ActiveScope).Limit(1).Count(&count).Error; err != nil {
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
					FullAudit:       model.FullAudit{CreatedAt: record.CreatedAt},
				}
				if err := tx.Create(&user).Error; err != nil {
					return fmt.Errorf("create initial admin user: %w", err)
				}
				adminSession := model.AdminSession{
					ID:         session.SessionID,
					UserID:     session.UserID,
					SecretHash: session.SecretHash,
					CSRFHash:   session.CSRFHash,
					ExpiresAt:  session.ExpiresAt,
					FullAudit:  model.FullAudit{CreatedAt: session.CreatedAt},
				}
				if err := tx.Create(&adminSession).Error; err != nil {
					return fmt.Errorf("%w: %w", service.ErrAdminSessionCreate, err)
				}
				return nil
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

func (s *DBStore) ClaimAdminEncryptionKey(
	ctx context.Context,
	userID string,
	expectedPasswordHash string,
	claimedAt time.Time,
) error {
	return s.withAdminAuthWrite(ctx, func() error {
		return s.db.WithContext(ctx).
			Session(&gorm.Session{Logger: gormlogger.Discard}).
			Transaction(func(tx *gorm.DB) error {
				if err := lockAdminSetupRow(tx); err != nil {
					return err
				}

				var authorized []model.User
				authorizedResult := adminEncryptionKeyClaimerLockQuery(
					tx,
					userID,
					expectedPasswordHash,
					claimedAt,
				).Limit(1).Find(&authorized)
				if authorizedResult.Error != nil {
					return fmt.Errorf("revalidate admin encryption key claimer: %w", authorizedResult.Error)
				}
				if len(authorized) != 1 {
					return service.ErrAdminEncryptionKeyDenied
				}

				var existing []model.SystemInitialization
				existingClaim := tx.Scopes(ActiveScope).Where("key = ?", adminEncryptionKeyClaim).
					Limit(1).
					Find(&existing)
				if existingClaim.Error != nil {
					return fmt.Errorf("check admin encryption key claim: %w", existingClaim.Error)
				}
				if len(existing) != 0 {
					return service.ErrAdminEncryptionKeyClaimed
				}

				claim := model.SystemInitialization{
					Key:       adminEncryptionKeyClaim,
					FullAudit: model.FullAudit{CreatedAt: claimedAt},
				}
				if err := tx.Create(&claim).Error; err != nil {
					return mapAdminEncryptionKeyClaimInsertError(err)
				}
				return nil
			})
	})
}

func lockAdminSetupRow(tx *gorm.DB) error {
	var setup []model.SystemInitialization
	result := adminSetupLockQuery(tx).Limit(1).Find(&setup)
	if result.Error != nil {
		return fmt.Errorf("lock admin setup row: %w", result.Error)
	}
	if len(setup) != 1 {
		return service.ErrAdminSetupNotCompleted
	}
	return nil
}

func adminSetupLockQuery(tx *gorm.DB) *gorm.DB {
	query := tx.Scopes(ActiveScope).Where("key = ?", model.SystemInitializationSetup)
	if tx.Dialector.Name() == "sqlite" {
		return query
	}
	return query.Clauses(clause.Locking{Strength: "UPDATE"})
}

func adminEncryptionKeyClaimerLockQuery(
	tx *gorm.DB,
	userID string,
	expectedPasswordHash string,
	claimedAt time.Time,
) *gorm.DB {
	query := tx.Scopes(ActiveScope).Where(
		"id = ? AND status = ? AND is_super_admin = ? AND "+
			"(expires_at IS NULL OR expires_at > ?) AND password_hash = ?",
		strings.TrimSpace(userID), "active", true, claimedAt, expectedPasswordHash,
	)
	if tx.Dialector.Name() == "sqlite" {
		return query
	}
	return query.Clauses(clause.Locking{Strength: "UPDATE"})
}

func mapAdminEncryptionKeyClaimInsertError(err error) error {
	if isUniqueConstraintError(err) {
		return fmt.Errorf("%w: %w", service.ErrAdminEncryptionKeyClaimed, err)
	}
	return fmt.Errorf("create admin encryption key claim: %w", err)
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
