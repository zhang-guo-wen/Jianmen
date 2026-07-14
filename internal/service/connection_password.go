package service

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type IssuedConnectionPassword struct {
	Plaintext       string
	Hash            string
	ExpiresAt       time.Time
	MySQLNativeHash string
}

func IssueConnectionPassword(now time.Time, ttl time.Duration) (IssuedConnectionPassword, error) {
	if ttl <= 0 {
		return IssuedConnectionPassword{}, fmt.Errorf("connection password ttl must be positive")
	}
	secretBytes := make([]byte, 24)
	if _, err := rand.Read(secretBytes); err != nil {
		return IssuedConnectionPassword{}, fmt.Errorf("generate connection password: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(secretBytes)
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return IssuedConnectionPassword{}, fmt.Errorf("hash connection password: %w", err)
	}
	stage1 := sha1.Sum([]byte(plaintext))
	stage2 := sha1.Sum(stage1[:])
	return IssuedConnectionPassword{
		Plaintext:       plaintext,
		Hash:            string(hash),
		ExpiresAt:       now.UTC().Add(ttl),
		MySQLNativeHash: hex.EncodeToString(stage2[:]),
	}, nil
}
