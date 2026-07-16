package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	aiAccessTokenPrefix  = "jm_ai_at_"
	aiRefreshTokenPrefix = "jm_ai_rt_"
)

type IssuedAIAccessToken struct {
	AccessToken      string
	AccessTokenHash  string
	RefreshToken     string
	RefreshTokenHash string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

func IssueAIAccessToken(now time.Time, accessTTL, refreshTTL time.Duration) (IssuedAIAccessToken, error) {
	if accessTTL <= 0 || refreshTTL <= accessTTL {
		return IssuedAIAccessToken{}, fmt.Errorf("invalid AI token TTL")
	}
	accessToken, err := randomToken(aiAccessTokenPrefix)
	if err != nil {
		return IssuedAIAccessToken{}, err
	}
	refreshToken, err := randomToken(aiRefreshTokenPrefix)
	if err != nil {
		return IssuedAIAccessToken{}, err
	}
	return IssuedAIAccessToken{
		AccessToken:      accessToken,
		AccessTokenHash:  HashAIAccessToken(accessToken),
		RefreshToken:     refreshToken,
		RefreshTokenHash: HashAIAccessToken(refreshToken),
		AccessExpiresAt:  now.UTC().Add(accessTTL),
		RefreshExpiresAt: now.UTC().Add(refreshTTL),
	}, nil
}

func HashAIAccessToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate AI token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
