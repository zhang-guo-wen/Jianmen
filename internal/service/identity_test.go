package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeIdentityRepository struct {
	subject IdentitySubject
	found   bool
	err     error
	userID  string
}

func (f *fakeIdentityRepository) FindIdentitySubject(_ context.Context, userID string) (IdentitySubject, bool, error) {
	f.userID = userID
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

func TestNewIdentityServiceRejectsNilRepository(t *testing.T) {
	if _, err := NewIdentityService(nil); err == nil {
		t.Fatal("expected nil repository error")
	}
}
