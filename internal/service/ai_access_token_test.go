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

func TestAIAccessTokenServiceScopesListGetAndRevokeToUser(t *testing.T) {
	now := time.Now().UTC()
	repository := &aiAccessTokenRepositoryStub{
		current: validAIAccessToken(now),
		tokens:  []model.AIAccessToken{validAIAccessToken(now)},
	}
	tokenService, err := NewAIAccessTokenService(repository)
	if err != nil {
		t.Fatalf("NewAIAccessTokenService: %v", err)
	}

	tokens, err := tokenService.List(context.Background(), " user-1 ")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if repository.listUserID != "user-1" || len(tokens) != 1 {
		t.Fatalf("list scope/result = %q %#v", repository.listUserID, tokens)
	}
	if tokens[0].AccessTokenHash != "" || tokens[0].RefreshTokenHash != "" {
		t.Fatal("list returned credential hashes")
	}

	token, err := tokenService.Get(context.Background(), "user-1", "token-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if repository.getUserID != "user-1" || repository.getTokenID != "token-1" {
		t.Fatalf("get scope = %q/%q", repository.getUserID, repository.getTokenID)
	}
	if token.AccessTokenHash != "" || token.RefreshTokenHash != "" {
		t.Fatal("get returned credential hashes")
	}

	if err := tokenService.Revoke(context.Background(), "user-1", "token-1", now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if repository.revokeUserID != "user-1" || repository.revokeTokenID != "token-1" {
		t.Fatalf("revoke scope = %q/%q", repository.revokeUserID, repository.revokeTokenID)
	}
}

func TestAIAccessTokenServiceReissueDelegatesAtomicRotation(t *testing.T) {
	now := time.Now().UTC()
	repository := &aiAccessTokenRepositoryStub{current: validAIAccessToken(now)}
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
	if result.Token.AccessTokenHash != "" || result.Token.RefreshTokenHash != "" {
		t.Fatal("reissue returned credential hashes")
	}
}

func TestAIAccessTokenServiceRepositoryFailureReturnsNoPlaintext(t *testing.T) {
	repositoryError := errors.New("repository failed")
	now := time.Now().UTC()
	repository := &aiAccessTokenRepositoryStub{
		current:   validAIAccessToken(now),
		rotateErr: repositoryError,
	}
	tokenService, err := NewAIAccessTokenService(repository)
	if err != nil {
		t.Fatalf("NewAIAccessTokenService: %v", err)
	}
	reissued, err := tokenService.Reissue(context.Background(), "user-1", "token-1", now, time.Hour, 24*time.Hour)
	if !errors.Is(err, repositoryError) {
		t.Fatalf("Reissue error = %v, want wrapped repository error", err)
	}
	if reissued.AccessToken != "" || reissued.RefreshToken != "" {
		t.Fatalf("reissue returned plaintext on failure: %#v", reissued)
	}

	refreshed, err := tokenService.Refresh(context.Background(), "jm_ai_rt_original", now, time.Hour, 24*time.Hour)
	if !errors.Is(err, repositoryError) {
		t.Fatalf("Refresh error = %v, want wrapped repository error", err)
	}
	if refreshed.AccessToken != "" || refreshed.RefreshToken != "" {
		t.Fatalf("refresh returned plaintext on failure: %#v", refreshed)
	}
}

func TestAIAccessTokenServiceAuthenticateFailsClosedForInactiveState(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		mutate func(*model.AIAccessToken)
	}{
		{name: "revoked", mutate: func(token *model.AIAccessToken) {
			token.RevokedAt = &now
		}},
		{name: "expired", mutate: func(token *model.AIAccessToken) {
			token.AccessExpiresAt = now
		}},
		{name: "disabled temporary account", mutate: func(token *model.AIAccessToken) {
			token.TemporaryAccount.Status = "disabled"
		}},
		{name: "expired temporary account", mutate: func(token *model.AIAccessToken) {
			token.TemporaryAccount.ExpiresAt = &now
		}},
		{name: "future temporary account", mutate: func(token *model.AIAccessToken) {
			token.TemporaryAccount.StartsAt = now.Add(time.Minute)
		}},
		{name: "wrong temporary owner", mutate: func(token *model.AIAccessToken) {
			token.TemporaryAccount.AuthorizedUserID = "user-2"
		}},
		{name: "disabled user", mutate: func(token *model.AIAccessToken) {
			token.User.Status = "disabled"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := validAIAccessToken(now)
			test.mutate(&token)
			tokenService, err := NewAIAccessTokenService(&aiAccessTokenRepositoryStub{authenticated: token})
			if err != nil {
				t.Fatalf("NewAIAccessTokenService: %v", err)
			}
			got, err := tokenService.Authenticate(context.Background(), "jm_ai_at_original", now)
			if !errors.Is(err, ErrInvalidAIAccessToken) {
				t.Fatalf("Authenticate error = %v, want invalid", err)
			}
			if got.ID != "" {
				t.Fatalf("Authenticate returned token on failure: %#v", got)
			}
		})
	}
}

func validAIAccessToken(now time.Time) model.AIAccessToken {
	return model.AIAccessToken{
		ID: "token-1", UserID: "user-1", TemporaryAccountID: "temporary-1", Name: "agent",
		AccessTokenHash: "old-access-hash", RefreshTokenHash: "old-refresh-hash",
		AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour),
		User: model.User{ID: "user-1", Username: "user-1", Status: "active"},
		TemporaryAccount: model.TemporaryAccount{
			ID: "temporary-1", AuthorizedUserID: "user-1", Status: "active", StartsAt: now.Add(-time.Hour),
		},
	}
}

type aiAccessTokenRepositoryStub struct {
	tokens            []model.AIAccessToken
	current           model.AIAccessToken
	authenticated     model.AIAccessToken
	replacement       model.AIAccessToken
	rotateRefreshHash string
	listUserID        string
	getUserID         string
	getTokenID        string
	revokeUserID      string
	revokeTokenID     string
	listErr           error
	getErr            error
	authenticateErr   error
	rotateErr         error
	revokeErr         error
}

func (s *aiAccessTokenRepositoryStub) ListAIAccessTokens(_ context.Context, userID string) ([]model.AIAccessToken, error) {
	s.listUserID = userID
	return s.tokens, s.listErr
}

func (s *aiAccessTokenRepositoryStub) AIAccessToken(_ context.Context, userID, tokenID string) (model.AIAccessToken, error) {
	s.getUserID = userID
	s.getTokenID = tokenID
	return s.current, s.getErr
}

func (s *aiAccessTokenRepositoryStub) AuthenticateAIAccessToken(
	_ context.Context,
	_ string,
	_ time.Time,
) (model.AIAccessToken, error) {
	return s.authenticated, s.authenticateErr
}

func (s *aiAccessTokenRepositoryStub) RotateAIAccessToken(
	_ context.Context,
	refreshHash string,
	replacement model.AIAccessToken,
	_ time.Time,
) (model.AIAccessToken, error) {
	if s.rotateErr != nil {
		return model.AIAccessToken{}, s.rotateErr
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

func (s *aiAccessTokenRepositoryStub) RevokeAIAccessToken(
	_ context.Context,
	userID string,
	tokenID string,
	_ time.Time,
) error {
	s.revokeUserID = userID
	s.revokeTokenID = tokenID
	return s.revokeErr
}
