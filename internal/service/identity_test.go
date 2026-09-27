package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeIdentityRepository struct {
	subject   IdentitySubject
	found     bool
	err       error
	userID    string
	tokenHash string
	ctxErr    error
}

func (f *fakeIdentityRepository) FindIdentitySubject(ctx context.Context, userID string) (IdentitySubject, bool, error) {
	f.userID = userID
	f.ctxErr = ctx.Err()
	return f.subject, f.found, f.err
}

func (f *fakeIdentityRepository) FindIdentitySubjectByTokenHash(
	ctx context.Context,
	tokenHash string,
) (IdentitySubject, bool, error) {
	f.tokenHash = tokenHash
	f.ctxErr = ctx.Err()
	return f.subject, f.found, f.err
}

func TestIdentityServiceReturnsActiveSubject(t *testing.T) {
	repository := &fakeIdentityRepository{
		subject: IdentitySubject{
			ID:         "u-admin",
			Username:   "admin",
			SuperAdmin: true,
			Status:     "active",
		},
		found: true,
	}
	identity, err := NewIdentityService(repository)
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	identity.now = func() time.Time { return time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC) }

	subject, found, err := identity.FindIdentitySubject(context.Background(), " u-admin ")
	if err != nil {
		t.Fatalf("find identity subject: %v", err)
	}
	if !found || subject.ID != "u-admin" || !subject.SuperAdmin {
		t.Fatalf("subject = %#v found=%t", subject, found)
	}
	if repository.userID != "u-admin" {
		t.Fatalf("repository user id = %q, want trimmed id", repository.userID)
	}
}

func TestIdentityServiceRejectsDisabledSubject(t *testing.T) {
	repository := &fakeIdentityRepository{
		subject: IdentitySubject{ID: "u-disabled", Status: "disabled"},
		found:   true,
	}
	identity, err := NewIdentityService(repository)
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}

	_, found, err := identity.FindIdentitySubject(context.Background(), "u-disabled")
	if err != nil {
		t.Fatalf("find identity subject: %v", err)
	}
	if found {
		t.Fatal("disabled subject must not be returned as active")
	}
}

func TestIdentityServiceRejectsExpiredNormalSubject(t *testing.T) {
	expiresAt := time.Date(2026, 7, 18, 9, 59, 0, 0, time.UTC)
	repository := &fakeIdentityRepository{
		subject: IdentitySubject{
			ID:        "u-expired",
			Status:    "active",
			ExpiresAt: &expiresAt,
		},
		found: true,
	}
	identity, err := NewIdentityService(repository)
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	identity.now = func() time.Time { return time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC) }

	_, found, err := identity.FindIdentitySubject(context.Background(), "u-expired")
	if err != nil {
		t.Fatalf("find identity subject: %v", err)
	}
	if found {
		t.Fatal("expired normal subject must not be returned as active")
	}
}

func TestIdentityServiceAllowsExpiredSuperAdministrator(t *testing.T) {
	expiresAt := time.Date(2026, 7, 18, 9, 59, 0, 0, time.UTC)
	repository := &fakeIdentityRepository{
		subject: IdentitySubject{
			ID:         "u-admin",
			SuperAdmin: true,
			Status:     "active",
			ExpiresAt:  &expiresAt,
		},
		found: true,
	}
	identity, err := NewIdentityService(repository)
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	identity.now = func() time.Time { return time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC) }

	subject, found, err := identity.FindIdentitySubject(context.Background(), "u-admin")
	if err != nil {
		t.Fatalf("find identity subject: %v", err)
	}
	if !found || !subject.SuperAdmin {
		t.Fatalf("subject = %#v found=%t", subject, found)
	}
}

func TestIdentityServicePropagatesRepositoryError(t *testing.T) {
	repositoryErr := errors.New("database unavailable")
	identity, err := NewIdentityService(&fakeIdentityRepository{err: repositoryErr})
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}

	_, _, err = identity.FindIdentitySubject(context.Background(), "u1")
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("find identity subject error = %v, want wrapped repository error", err)
	}
}

func TestIdentityServiceResolvesTokenHashUsingCanonicalIdentityRules(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	expiredAt := now.Add(-time.Minute)
	tests := []struct {
		name      string
		subject   IdentitySubject
		found     bool
		wantFound bool
	}{
		{
			name:      "active normal user",
			subject:   IdentitySubject{ID: "u-active", Status: "active"},
			found:     true,
			wantFound: true,
		},
		{
			name:    "disabled user",
			subject: IdentitySubject{ID: "u-disabled", Status: "disabled"},
			found:   true,
		},
		{
			name:    "expired normal user",
			subject: IdentitySubject{ID: "u-expired", Status: "active", ExpiresAt: &expiredAt},
			found:   true,
		},
		{
			name: "expired super administrator",
			subject: IdentitySubject{
				ID:         "u-admin",
				Status:     "active",
				SuperAdmin: true,
				ExpiresAt:  &expiredAt,
			},
			found:     true,
			wantFound: true,
		},
		{
			name: "missing token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &fakeIdentityRepository{subject: tt.subject, found: tt.found}
			identity, err := NewIdentityService(repository)
			if err != nil {
				t.Fatalf("new identity service: %v", err)
			}
			identity.now = func() time.Time { return now }

			subject, found, err := identity.FindIdentitySubjectByTokenHash(context.Background(), " token-hash ")
			if err != nil {
				t.Fatalf("find identity by token hash: %v", err)
			}
			if found != tt.wantFound {
				t.Fatalf("found = %t, want %t; subject=%#v", found, tt.wantFound, subject)
			}
			if found && subject.ID != tt.subject.ID {
				t.Fatalf("subject ID = %q, want %q", subject.ID, tt.subject.ID)
			}
			if repository.tokenHash != "token-hash" {
				t.Fatalf("repository token hash = %q, want trimmed hash", repository.tokenHash)
			}
		})
	}
}

func TestIdentityServiceTokenHashLookupPropagatesErrorAndContext(t *testing.T) {
	repositoryErr := errors.New("database unavailable")
	repository := &fakeIdentityRepository{err: repositoryErr}
	identity, err := NewIdentityService(repository)
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err = identity.FindIdentitySubjectByTokenHash(ctx, "token-hash")
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("find identity by token hash error = %v, want wrapped %v", err, repositoryErr)
	}
	if !errors.Is(repository.ctxErr, context.Canceled) {
		t.Fatalf("repository context error = %v, want context canceled", repository.ctxErr)
	}
}

func TestNewIdentityServiceRejectsNilRepository(t *testing.T) {
	if _, err := NewIdentityService(nil); err == nil {
		t.Fatal("expected nil repository error")
	}
}
