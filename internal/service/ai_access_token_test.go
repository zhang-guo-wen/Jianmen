package service

import (
	"strings"
	"testing"
	"time"
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
