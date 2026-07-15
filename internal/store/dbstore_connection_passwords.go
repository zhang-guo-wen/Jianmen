package store

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"jianmen/internal/model"
)

func (s *DBStore) CreateConnectionPassword(ctx context.Context, credential model.ConnectionPassword) error {
	if credential.UserID == "" || credential.ResourceType == "" || credential.ResourceID == "" || credential.SecretHash == "" {
		return errors.New("connection password fields are required")
	}
	if credential.ExpiresAt.IsZero() {
		return errors.New("connection password expiry is required")
	}
	if err := s.db.WithContext(ctx).Create(&credential).Error; err != nil {
		return fmt.Errorf("create connection password: %w", err)
	}
	return nil
}

func (s *DBStore) AuthenticateConnectionPassword(ctx context.Context, userID, resourceType, resourceID, password string) error {
	var user model.User
	if err := s.db.WithContext(ctx).Where("id = ? AND status = ?", userID, "active").First(&user).Error; err != nil {
		return errors.New("authentication failed")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil {
		return nil
	}

	now := time.Now().UTC()
	credentials, err := s.activeConnectionPasswords(ctx, userID, resourceType, resourceID, now)
	if err != nil {
		return err
	}
	for _, credential := range credentials {
		if bcrypt.CompareHashAndPassword([]byte(credential.SecretHash), []byte(password)) != nil {
			continue
		}
		return nil
	}
	return errors.New("authentication failed")
}

func (s *DBStore) AuthenticateMySQLConnectionPassword(ctx context.Context, userID, resourceID string, salt, response []byte) error {
	if len(salt) == 0 || len(response) != sha1.Size {
		return errors.New("authentication failed")
	}
	now := time.Now().UTC()
	credentials, err := s.activeConnectionPasswords(ctx, userID, model.ResourceTypeDatabaseAccount, resourceID, now)
	if err != nil {
		return err
	}
	for _, credential := range credentials {
		stage2, err := hex.DecodeString(credential.MySQLNativeHash)
		if err != nil || len(stage2) != sha1.Size {
			continue
		}
		scrambleInput := append(append(make([]byte, 0, len(salt)+len(stage2)), salt...), stage2...)
		scramble := sha1.Sum(scrambleInput)
		stage1 := make([]byte, sha1.Size)
		for index := range stage1 {
			stage1[index] = response[index] ^ scramble[index]
		}
		candidateStage2 := sha1.Sum(stage1)
		if !equalBytes(candidateStage2[:], stage2) {
			continue
		}
		return nil
	}
	return errors.New("authentication failed")
}

func (s *DBStore) activeConnectionPasswords(ctx context.Context, userID, resourceType, resourceID string, now time.Time) ([]model.ConnectionPassword, error) {
	var credentials []model.ConnectionPassword
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND resource_type = ? AND resource_id = ?", userID, resourceType, resourceID).
		Where("expires_at > ?", now).
		Order("created_at DESC").
		Limit(50).
		Find(&credentials).Error; err != nil {
		return nil, fmt.Errorf("load connection passwords: %w", err)
	}
	return credentials, nil
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func (s *DBStore) deleteExpiredConnectionPasswords(ctx context.Context, before time.Time) error {
	return s.db.WithContext(ctx).
		Where("expires_at <= ?", before.UTC()).
		Delete(&model.ConnectionPassword{}).Error
}
