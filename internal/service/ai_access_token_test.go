package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestIssueAIAccessTokenCreatesDistinctHashedCredentials(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	issued, err := IssueAIAccessToken(now, time.Hour, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("IssueAIAccessToken: %v", err)
	}
	if !strings.HasPrefix(issued.AccessToken, "jm_ai_at_") || !strings.HasPrefix(issued.RefreshToken, "jm_ai_rt_") {
		t.Fatalf("unexpected token prefixes")
	}
	if issued.AccessToken == issued.RefreshToken || issued.AccessTokenHash == issued.RefreshTokenHash {
		t.Fatalf("access and refresh credentials must be distinct")
	}
	if issued.AccessTokenHash == issued.AccessToken || issued.RefreshTokenHash == issued.RefreshToken {
		t.Fatalf("plaintext tokens must not be stored as hashes")
	}
	if !issued.AccessExpiresAt.Equal(now.Add(time.Hour)) || !issued.RefreshExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("unexpected expiry values: %#v", issued)
	}
}

func TestIssueAIAccessTokenRejectsInvalidTTL(t *testing.T) {
	if _, err := IssueAIAccessToken(time.Now(), time.Hour, time.Hour); err == nil {
		t.Fatal("expected invalid TTL error")
	}
}

func TestAIAccessTokenServiceReissueDelegatesAtomicRotation(t *testing.T) {
	now := time.Now().UTC()
	repository := &aiAccessTokenRepositoryStub{
		current: model.AIAccessToken{
			ID:               "token-1",
			UserID:           "user-1",
			Name:             "agent",
			RefreshTokenHash: "old-refresh-hash",
		},
	}
	tokenService, err := NewAIAccessTokenService(repository)
	if err != nil {
		t.Fatalf("NewAIAccessTokenService: %v", err)
	}

	result, err := tokenService.Reissue(context.Background(), "user-1", "token-1", now, time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatalf("Reissue: %v", err)
	}
	if repository.rotateRefreshHash != "old-refresh-hash" {
		t.Fatalf("rotation refresh hash = %q", repository.rotateRefreshHash)
	}
	if repository.replacement.AccessTokenHash == "" || repository.replacement.RefreshTokenHash == "" {
		t.Fatalf("rotation replacement lacks hashes: %#v", repository.replacement)
	}
	if result.Token.ID != "token-1" || result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("unexpected reissue result: %#v", result)
	}
}

func TestAIAccessTokenServiceReissuePreservesRepositoryError(t *testing.T) {
	repositoryError := errors.New("repository failed")
	tokenService, err := NewAIAccessTokenService(&aiAccessTokenRepositoryStub{err: repositoryError})
	if err != nil {
		t.Fatalf("NewAIAccessTokenService: %v", err)
	}
	if _, err := tokenService.Reissue(context.Background(), "user-1", "token-1", time.Now(), time.Hour, 24*time.Hour); !errors.Is(err, repositoryError) {
		t.Fatalf("Reissue error = %v, want wrapped repository error", err)
	}
}

type aiAccessTokenRepositoryStub struct {
	current           model.AIAccessToken
	replacement       model.AIAccessToken
	rotateRefreshHash string
	err               error
}

func (s *aiAccessTokenRepositoryStub) AIAccessToken(
	context.Context,
	string,
	string,
) (model.AIAccessToken, error) {
	if s.err != nil {
		return model.AIAccessToken{}, s.err
	}
	return s.current, nil
}

func (s *aiAccessTokenRepositoryStub) RotateAIAccessToken(
	_ context.Context,
	refreshHash string,
	replacement model.AIAccessToken,
	_ time.Time,
) (model.AIAccessToken, error) {
	if s.err != nil {
		return model.AIAccessToken{}, s.err
	}
	s.rotateRefreshHash = refreshHash
	s.replacement = replacement
	rotated := s.current
	rotated.AccessTokenHash = replacement.AccessTokenHash
	rotated.RefreshTokenHash = replacement.RefreshTokenHash
	rotated.AccessExpiresAt = replacement.AccessExpiresAt
	rotated.RefreshExpiresAt = replacement.RefreshExpiresAt
	return rotated, nil
}
