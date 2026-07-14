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

func (s *DBStore) Authenticate(_ context.Context, username, password string) (model.User, error) {
	// Try token-based auth first.
	hash := sha256.Sum256([]byte(password))
	hashStr := hex.EncodeToString(hash[:])

	var user model.User
	if err := s.db.Where("token_hash = ? AND status = ?", hashStr, "active").First(&user).Error; err == nil {
		return user, nil
	}

	// Parse compact username and authenticate via session.
	login, err := parseLoginName(username)
	if err != nil {
		return model.User{}, err
	}
	return s.authenticateCompact(login, password)
}

func (s *DBStore) AuthenticatePublicKey(_ context.Context, username string, key ssh.PublicKey) (model.User, error) {
	login, err := parseLoginName(username)
	if err != nil {
		return model.User{}, err
	}
	return s.authenticateCompactPublicKey(login, key)
}

func (s *DBStore) authenticateCompact(login LoginName, password string) (model.User, error) {
	var userSession model.UserSession
	if err := s.db.Where("session_id = ? AND status = ?", login.SessionID, "active").First(&userSession).Error; err != nil {
		return model.User{}, fmt.Errorf("invalid session: %w", err)
	}
	if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
		s.db.Model(&userSession).Update("status", "expired")
		return model.User{}, errors.New("session expired")
	}
	var user model.User
	if err := s.db.Where("id = ? AND status = ?", userSession.UserID, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, errors.New("user is disabled or not found")
		}
		return model.User{}, err
	}
	if login.ResourceID == "" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			return model.User{}, errors.New("authentication failed")
		}
		return user, nil
	}
	var account model.HostAccount
	if err := s.db.Where("resource_id = ? AND status = ?", login.ResourceID, "active").First(&account).Error; err != nil {
		return model.User{}, errors.New("authentication failed")
	}
	if err := s.AuthenticateConnectionPassword(context.Background(), user.ID, model.ResourceTypeHostAccount, account.ID, password); err != nil {
		return model.User{}, errors.New("authentication failed")
	}
	user.RequestedTargetID = account.ID
	return user, nil
}

func (s *DBStore) authenticateCompactPublicKey(login LoginName, key ssh.PublicKey) (model.User, error) {
	var userSession model.UserSession
	if err := s.db.Where("session_id = ? AND status = ?", login.SessionID, "active").First(&userSession).Error; err != nil {
		return model.User{}, fmt.Errorf("invalid session: %w", err)
	}
	if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
		s.db.Model(&userSession).Update("status", "expired")
		return model.User{}, errors.New("session expired")
	}
	var user model.User
	if err := s.db.Where("id = ? AND status = ?", userSession.UserID, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, errors.New("user is disabled or not found")
		}
		return model.User{}, err
	}
	var pubKeys []model.UserPublicKey
	if err := s.db.Where("user_id = ? AND revoked_at IS NULL", user.ID).Find(&pubKeys).Error; err != nil {
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
		if err := s.db.Where("resource_id = ?", login.ResourceID).First(&account).Error; err == nil {
			user.RequestedTargetID = account.ID
		}
	}
	return user, nil
}

func (s *DBStore) Users() []UserView {
	var users []model.User
	if err := s.db.Where("status = ?", "active").Order("username ASC").Find(&users).Error; err != nil {
		return nil
	}
	out := make([]UserView, len(users))
	for i := range users {
		out[i] = UserView{ID: users[i].ID, Username: users[i].Username}
	}
	return out
}

func (s *DBStore) AuthenticateDirect(_ context.Context, username, password string) (model.User, error) {
	var user model.User
	if err := s.db.Where("username = ? AND status = ?", username, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, errors.New("invalid username or password")
		}
		return model.User{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return model.User{}, errors.New("invalid username or password")
	}
	return user, nil
}
