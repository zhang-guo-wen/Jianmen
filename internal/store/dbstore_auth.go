package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"jianmen/internal/model"
)

// -- auth --

func (s *DBStore) Authenticate(ctx context.Context, username, password string) (model.User, error) {
	// Try token-based auth first.
	hash := sha256.Sum256([]byte(password))
	hashStr := hex.EncodeToString(hash[:])

	var user model.User
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Where("token_hash = ? AND status = ?", hashStr, "active").First(&user).Error; err == nil {
		if user.IsExpired(time.Now().UTC()) {
			_ = s.db.WithContext(ctx).Model(&user).Scopes(ActiveScope).Update("status", "disabled").Error
			return model.User{}, errors.New("user account expired")
		}
		return user, nil
	}

	// Parse compact username and authenticate via session.
	login, err := parseLoginName(username)
	if err != nil {
		return model.User{}, err
	}
	return s.authenticateCompact(ctx, login, password)
}

func (s *DBStore) AuthenticatePublicKey(ctx context.Context, username string, key ssh.PublicKey) (model.User, error) {
	login, err := parseLoginName(username)
	if err != nil {
		return model.User{}, err
	}
	return s.authenticateCompactPublicKey(ctx, login, key)
}

func (s *DBStore) authenticateCompact(ctx context.Context, login LoginName, password string) (model.User, error) {
	var userSession model.UserSession
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Where("session_id = ? AND status = ?", login.SessionID, "active").First(&userSession).Error; err != nil {
		return model.User{}, fmt.Errorf("invalid session: %w", err)
	}
	if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
		s.db.WithContext(ctx).Model(&userSession).Scopes(ActiveScope).Update("status", "expired")
		return model.User{}, errors.New("session expired")
	}
	var user model.User
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Where("id = ? AND status = ?", userSession.UserID, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, errors.New("user is disabled or not found")
		}
		return model.User{}, err
	}
	if user.IsExpired(time.Now().UTC()) {
		_ = s.db.WithContext(ctx).Model(&user).Scopes(ActiveScope).Update("status", "disabled").Error
		return model.User{}, errors.New("user account expired")
	}
	if login.ResourceID == "" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			return model.User{}, errors.New("authentication failed")
		}
		return user, nil
	}
	var account model.HostAccount
	if err := s.db.Scopes(activeHostAccountScope).
		Where("host_accounts.resource_id = ? AND host_accounts.status = ?", login.ResourceID, "active").
		First(&account).Error; err != nil {
		return model.User{}, errors.New("authentication failed")
	}
	if err := s.AuthenticateConnectionPassword(ctx, user.ID, model.ResourceTypeHostAccount, account.ID, password); err != nil {
		return model.User{}, errors.New("authentication failed")
	}
	user.RequestedTargetID = account.ID
	return user, nil
}

func (s *DBStore) authenticateCompactPublicKey(ctx context.Context, login LoginName, key ssh.PublicKey) (model.User, error) {
	var userSession model.UserSession
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Where("session_id = ? AND status = ?", login.SessionID, "active").First(&userSession).Error; err != nil {
		return model.User{}, fmt.Errorf("invalid session: %w", err)
	}
	if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
		s.db.WithContext(ctx).Model(&userSession).Scopes(ActiveScope).Update("status", "expired")
		return model.User{}, errors.New("session expired")
	}
	var user model.User
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Where("id = ? AND status = ?", userSession.UserID, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, errors.New("user is disabled or not found")
		}
		return model.User{}, err
	}
	if user.IsExpired(time.Now().UTC()) {
		_ = s.db.WithContext(ctx).Model(&user).Scopes(ActiveScope).Update("status", "disabled").Error
		return model.User{}, errors.New("user account expired")
	}
	var pubKeys []model.UserPublicKey
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Where("user_id = ? AND revoked_at IS NULL", user.ID).Find(&pubKeys).Error; err != nil {
		return model.User{}, fmt.Errorf("load public keys: %w", err)
	}
	keyMatched := false
	for _, pk := range pubKeys {
		parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pk.PublicKey))
		if err != nil {
			continue
		}
		if publicKeysEqual(key, parsed) {
			keyMatched = true
			break
		}
	}
	if !keyMatched {
		return model.User{}, errors.New("invalid username or public key")
	}
	if login.ResourceID != "" {
		var account model.HostAccount
		if err := s.db.Scopes(activeHostAccountScope).
			Where("host_accounts.resource_id = ? AND host_accounts.status = ?", login.ResourceID, "active").
			First(&account).Error; err != nil {
			return model.User{}, errors.New("invalid username or public key")
		}
		user.RequestedTargetID = account.ID
	}
	return user, nil
}

func (s *DBStore) Users() []UserView {
	var users []model.User
	if err := s.db.Scopes(ActiveScope).Where("status = ?", "active").Order("username ASC").Find(&users).Error; err != nil {
		return nil
	}
	out := make([]UserView, len(users))
	for i := range users {
		out[i] = UserView{ID: users[i].ID, Username: users[i].Username}
	}
	return out
}

func (s *DBStore) AuthenticateDirect(ctx context.Context, username, password string) (model.User, error) {
	var user model.User
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).Where("username = ? AND status = ?", username, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, errors.New("invalid username or password")
		}
		return model.User{}, err
	}
	if user.IsExpired(time.Now().UTC()) {
		_ = s.db.WithContext(ctx).Model(&user).Scopes(ActiveScope).Update("status", "disabled").Error
		return model.User{}, errors.New("user account expired")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return model.User{}, errors.New("invalid username or password")
	}
	return user, nil
}
