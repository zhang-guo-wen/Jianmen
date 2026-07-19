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
	ListAIAccessTokens(ctx context.Context, userID string) ([]model.AIAccessToken, error)
	AIAccessToken(ctx context.Context, userID, tokenID string) (model.AIAccessToken, error)
	AuthenticateAIAccessToken(ctx context.Context, accessHash string, now time.Time) (model.AIAccessToken, error)
	RotateAIAccessToken(
		ctx context.Context,
		refreshHash string,
		replacement model.AIAccessToken,
		now time.Time,
	) (model.AIAccessToken, error)
	RevokeAIAccessToken(ctx context.Context, userID, tokenID string, now time.Time) error
}

type AIAccessTokenService struct {
	repository AIAccessTokenRepository
}

type ReissuedAIAccessToken struct {
	Token        model.AIAccessToken
	AccessToken  string
	RefreshToken string
}

type RefreshedAIAccessToken = ReissuedAIAccessToken

var ErrInvalidAIAccessToken = errors.New("invalid AI access token")

func NewAIAccessTokenService(repository AIAccessTokenRepository) (*AIAccessTokenService, error) {
	if repository == nil {
		return nil, errors.New("ai access token repository is required")
	}
	return &AIAccessTokenService{repository: repository}, nil
}

func (s *AIAccessTokenService) List(ctx context.Context, userID string) ([]model.AIAccessToken, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("%w: user ID is required", ErrInvalidAIAccessToken)
	}
	tokens, err := s.repository.ListAIAccessTokens(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list AI access tokens: %w", err)
	}
	for index := range tokens {
		tokens[index] = publicAIAccessToken(tokens[index])
	}
	return tokens, nil
}

func (s *AIAccessTokenService) Get(ctx context.Context, userID, tokenID string) (model.AIAccessToken, error) {
	userID = strings.TrimSpace(userID)
	tokenID = strings.TrimSpace(tokenID)
	if userID == "" || tokenID == "" {
		return model.AIAccessToken{}, fmt.Errorf("%w: user and token IDs are required", ErrInvalidAIAccessToken)
	}
	token, err := s.repository.AIAccessToken(ctx, userID, tokenID)
	if err != nil {
		return model.AIAccessToken{}, fmt.Errorf("get AI access token: %w", err)
	}
	return publicAIAccessToken(token), nil
}

func (s *AIAccessTokenService) Revoke(ctx context.Context, userID, tokenID string, now time.Time) error {
	userID = strings.TrimSpace(userID)
	tokenID = strings.TrimSpace(tokenID)
	if userID == "" || tokenID == "" {
		return fmt.Errorf("%w: user and token IDs are required", ErrInvalidAIAccessToken)
	}
	if err := s.repository.RevokeAIAccessToken(ctx, userID, tokenID, normalizedAITokenTime(now)); err != nil {
		return fmt.Errorf("revoke AI access token: %w", err)
	}
	return nil
}

func (s *AIAccessTokenService) Reissue(
	ctx context.Context,
	userID string,
	tokenID string,
	now time.Time,
	accessTTL time.Duration,
	refreshTTL time.Duration,
) (ReissuedAIAccessToken, error) {
	userID = strings.TrimSpace(userID)
	tokenID = strings.TrimSpace(tokenID)
	if userID == "" || tokenID == "" {
		return ReissuedAIAccessToken{}, fmt.Errorf("%w: user and token IDs are required", ErrInvalidAIAccessToken)
	}
	now = normalizedAITokenTime(now)
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
	if err := validateActiveAIAccessToken(rotated, now, false); err != nil {
		return ReissuedAIAccessToken{}, err
	}
	return ReissuedAIAccessToken{
		Token:        publicAIAccessToken(rotated),
		AccessToken:  issued.AccessToken,
		RefreshToken: issued.RefreshToken,
	}, nil
}

func (s *AIAccessTokenService) Refresh(
	ctx context.Context,
	refreshToken string,
	now time.Time,
	accessTTL time.Duration,
	refreshTTL time.Duration,
) (RefreshedAIAccessToken, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if !strings.HasPrefix(refreshToken, aiRefreshTokenPrefix) {
		return RefreshedAIAccessToken{}, ErrInvalidAIAccessToken
	}
	now = normalizedAITokenTime(now)
	issued, err := IssueAIAccessToken(now, accessTTL, refreshTTL)
	if err != nil {
		return RefreshedAIAccessToken{}, err
	}
	rotated, err := s.repository.RotateAIAccessToken(ctx, HashAIAccessToken(refreshToken), model.AIAccessToken{
		AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash,
		AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt,
	}, now)
	if err != nil {
		return RefreshedAIAccessToken{}, fmt.Errorf("refresh AI access token: %w", err)
	}
	if err := validateActiveAIAccessToken(rotated, now, false); err != nil {
		return RefreshedAIAccessToken{}, err
	}
	return RefreshedAIAccessToken{
		Token:        publicAIAccessToken(rotated),
		AccessToken:  issued.AccessToken,
		RefreshToken: issued.RefreshToken,
	}, nil
}

func (s *AIAccessTokenService) Authenticate(
	ctx context.Context,
	accessToken string,
	now time.Time,
) (model.AIAccessToken, error) {
	accessToken = strings.TrimSpace(accessToken)
	if !strings.HasPrefix(accessToken, aiAccessTokenPrefix) {
		return model.AIAccessToken{}, ErrInvalidAIAccessToken
	}
	now = normalizedAITokenTime(now)
	token, err := s.repository.AuthenticateAIAccessToken(ctx, HashAIAccessToken(accessToken), now)
	if err != nil {
		return model.AIAccessToken{}, fmt.Errorf("authenticate AI access token: %w", err)
	}
	if err := validateActiveAIAccessToken(token, now, true); err != nil {
		return model.AIAccessToken{}, err
	}
	return publicAIAccessToken(token), nil
}

func IssueAIAccessToken(now time.Time, accessTTL, refreshTTL time.Duration) (IssuedAIAccessToken, error) {
	if accessTTL <= 0 || refreshTTL <= accessTTL {
		return IssuedAIAccessToken{}, fmt.Errorf("invalid AI token TTL")
	}
	now = normalizedAITokenTime(now)
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

func normalizedAITokenTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func validateActiveAIAccessToken(token model.AIAccessToken, now time.Time, requireAccessExpiry bool) error {
	if token.ID == "" || token.UserID == "" || token.RevokedAt != nil {
		return ErrInvalidAIAccessToken
	}
	if requireAccessExpiry && !token.AccessExpiresAt.After(now) {
		return ErrInvalidAIAccessToken
	}
	if !requireAccessExpiry && !token.RefreshExpiresAt.After(now) {
		return ErrInvalidAIAccessToken
	}
	if token.User.ID == "" || token.User.ID != token.UserID ||
		token.User.Status != "active" || token.User.IsExpired(now) {
		return ErrInvalidAIAccessToken
	}
	if token.TemporaryAccountID == "" {
		return nil
	}
	temporary := token.TemporaryAccount
	if temporary.ID == "" || temporary.ID != token.TemporaryAccountID ||
		temporary.AuthorizedUserID != token.UserID ||
		temporary.Status != "active" ||
		temporary.StartsAt.After(now) ||
		(temporary.ExpiresAt != nil && !temporary.ExpiresAt.After(now)) {
		return ErrInvalidAIAccessToken
	}
	return nil
}

func publicAIAccessToken(token model.AIAccessToken) model.AIAccessToken {
	token.AccessTokenHash = ""
	token.RefreshTokenHash = ""
	return token
}
