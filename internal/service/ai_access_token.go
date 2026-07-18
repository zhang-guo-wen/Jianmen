package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
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

type AIAccessTokenRepository interface {
	AIAccessToken(ctx context.Context, userID, tokenID string) (model.AIAccessToken, error)
	RotateAIAccessToken(
		ctx context.Context,
		refreshHash string,
		replacement model.AIAccessToken,
		now time.Time,
	) (model.AIAccessToken, error)
}

type AIAccessTokenService struct {
	repository AIAccessTokenRepository
}

type ReissuedAIAccessToken struct {
	Token        model.AIAccessToken
	AccessToken  string
	RefreshToken string
}

func NewAIAccessTokenService(repository AIAccessTokenRepository) (*AIAccessTokenService, error) {
	if repository == nil {
		return nil, errors.New("ai access token repository is required")
	}
	return &AIAccessTokenService{repository: repository}, nil
}

func (s *AIAccessTokenService) Reissue(
	ctx context.Context,
	userID string,
	tokenID string,
	now time.Time,
	accessTTL time.Duration,
	refreshTTL time.Duration,
) (ReissuedAIAccessToken, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(tokenID) == "" {
		return ReissuedAIAccessToken{}, errors.New("ai access token user and token ID are required")
	}
	current, err := s.repository.AIAccessToken(ctx, userID, tokenID)
	if err != nil {
		return ReissuedAIAccessToken{}, fmt.Errorf("load ai access token for reissue: %w", err)
	}
	issued, err := IssueAIAccessToken(now, accessTTL, refreshTTL)
	if err != nil {
		return ReissuedAIAccessToken{}, err
	}
	rotated, err := s.repository.RotateAIAccessToken(ctx, current.RefreshTokenHash, model.AIAccessToken{
		AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash,
		AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt,
	}, now)
	if err != nil {
		return ReissuedAIAccessToken{}, fmt.Errorf("rotate ai access token for reissue: %w", err)
	}
	return ReissuedAIAccessToken{
		Token:        rotated,
		AccessToken:  issued.AccessToken,
		RefreshToken: issued.RefreshToken,
	}, nil
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
