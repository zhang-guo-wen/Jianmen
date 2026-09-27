package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

var (
	ErrAIAccessTokenInvalid  = errors.New("ai access token is invalid")
	ErrAIAccessTokenNotFound = errors.New("ai access token not found")
)

func (s *DBStore) CreateAIAccessToken(ctx context.Context, token model.AIAccessToken) error {
	if token.UserID == "" || token.Name == "" || token.AccessTokenHash == "" || token.RefreshTokenHash == "" {
		return errors.New("AI access token fields are required")
	}
	if token.AccessExpiresAt.IsZero() || token.RefreshExpiresAt.IsZero() {
		return errors.New("AI access token expiry is required")
	}
	if err := s.db.WithContext(ctx).Create(&token).Error; err != nil {
		return fmt.Errorf("create AI access token: %w", err)
	}
	return nil
}

func (s *DBStore) ListAIAccessTokens(ctx context.Context, userID string) ([]model.AIAccessToken, error) {
	var tokens []model.AIAccessToken
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Where("user_id = ?", userID).Order("created_at DESC").Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("list AI access tokens: %w", err)
	}
	return tokens, nil
}

func (s *DBStore) AIAccessToken(ctx context.Context, userID, tokenID string) (model.AIAccessToken, error) {
	var token model.AIAccessToken
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.AIAccessToken{}, ErrAIAccessTokenNotFound
		}
		return model.AIAccessToken{}, fmt.Errorf("get AI access token: %w", err)
	}
	return token, nil
}

func (s *DBStore) AuthenticateAIAccessToken(ctx context.Context, accessHash string, now time.Time) (model.AIAccessToken, error) {
	var token model.AIAccessToken
	err := s.db.WithContext(ctx).Scopes(ActiveScope).
		Preload("User", ActiveScope).Preload("TemporaryAccount", ActiveScope).
		Where("access_token_hash = ? AND revoked_at IS NULL AND access_expires_at > ?", accessHash, now.UTC()).
		First(&token).Error
	if err != nil {
		return model.AIAccessToken{}, ErrAIAccessTokenInvalid
	}
	if token.User.Status != "active" || token.User.IsExpired(now) {
		return model.AIAccessToken{}, ErrAIAccessTokenInvalid
	}
	if token.TemporaryAccountID != "" && (token.TemporaryAccount.Status != "active" || (token.TemporaryAccount.ExpiresAt != nil && !token.TemporaryAccount.ExpiresAt.After(now))) {
		return model.AIAccessToken{}, ErrAIAccessTokenInvalid
	}
	usedAt := now.UTC()
	if err := s.db.WithContext(ctx).Model(&model.AIAccessToken{}).Scopes(ActiveScope).Where("id = ?", token.ID).Update("last_used_at", usedAt).Error; err != nil {
		return model.AIAccessToken{}, fmt.Errorf("touch AI access token: %w", err)
	}
	token.LastUsedAt = &usedAt
	return token, nil
}

func (s *DBStore) RotateAIAccessToken(ctx context.Context, refreshHash string, replacement model.AIAccessToken, now time.Time) (model.AIAccessToken, error) {
	var rotated model.AIAccessToken
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.AIAccessToken
		if err := tx.Scopes(ActiveScope).
			Preload("User", ActiveScope).Preload("TemporaryAccount", ActiveScope).Where(
			"refresh_token_hash = ? AND revoked_at IS NULL AND refresh_expires_at > ?", refreshHash, now.UTC(),
		).First(&current).Error; err != nil {
			return ErrAIAccessTokenInvalid
		}
		if current.User.Status != "active" || current.User.IsExpired(now) {
			return ErrAIAccessTokenInvalid
		}
		if current.TemporaryAccountID != "" && (current.TemporaryAccount.Status != "active" || (current.TemporaryAccount.ExpiresAt != nil && !current.TemporaryAccount.ExpiresAt.After(now))) {
			return ErrAIAccessTokenInvalid
		}
		updates := map[string]any{
			"access_token_hash":  replacement.AccessTokenHash,
			"refresh_token_hash": replacement.RefreshTokenHash,
			"access_expires_at":  replacement.AccessExpiresAt,
			"refresh_expires_at": replacement.RefreshExpiresAt,
			"last_used_at":       now.UTC(),
		}
		result := tx.Model(&model.AIAccessToken{}).Scopes(ActiveScope).
			Where("id = ? AND refresh_token_hash = ? AND revoked_at IS NULL", current.ID, refreshHash).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAIAccessTokenInvalid
		}
		current.AccessTokenHash = replacement.AccessTokenHash
		current.RefreshTokenHash = replacement.RefreshTokenHash
		current.AccessExpiresAt = replacement.AccessExpiresAt
		current.RefreshExpiresAt = replacement.RefreshExpiresAt
		usedAt := now.UTC()
		current.LastUsedAt = &usedAt
		rotated = current
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrAIAccessTokenInvalid) {
			return model.AIAccessToken{}, ErrAIAccessTokenInvalid
		}
		return model.AIAccessToken{}, fmt.Errorf("rotate AI access token: %w", err)
	}
	return rotated, nil
}

func (s *DBStore) RevokeAIAccessToken(ctx context.Context, userID, tokenID string, now time.Time) error {
	result := s.db.WithContext(ctx).Model(&model.AIAccessToken{}).Scopes(ActiveScope).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", tokenID, userID).
		Update("revoked_at", now.UTC())
	if result.Error != nil {
		return fmt.Errorf("revoke AI access token: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrAIAccessTokenNotFound
	}
	return nil
}
