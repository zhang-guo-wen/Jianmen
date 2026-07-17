package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"jianmen/internal/model"
)

type fakeAdminAuthRepository struct {
	user       model.User
	found      bool
	findErr    error
	disableErr error
	disabledID string
}

func (f *fakeAdminAuthRepository) FindActiveUserByTokenHash(context.Context, string) (model.User, bool, error) {
	return f.user, f.found, f.findErr
}

func (f *fakeAdminAuthRepository) DisableUser(_ context.Context, userID string) error {
	f.disabledID = userID
	return f.disableErr
}

func TestAdminAuthServiceAuthenticateValidUser(t *testing.T) {
	repository := &fakeAdminAuthRepository{user: model.User{ID: "u1", Username: "alice", Status: "active"}, found: true}
	service, err := NewAdminAuthService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	user, err := service.Authenticate(context.Background(), "hash", time.Now(), nil)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if user.ID != "u1" || repository.disabledID != "" {
		t.Fatalf("user = %#v disabled=%q", user, repository.disabledID)
	}
}

func TestAdminAuthServiceDisablesExpiredUser(t *testing.T) {
	expiresAt := time.Now().Add(-time.Minute)
	repository := &fakeAdminAuthRepository{user: model.User{ID: "u1", Status: "active", ExpiresAt: &expiresAt}, found: true}
	service, err := NewAdminAuthService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.Authenticate(context.Background(), "hash", time.Now(), nil)
	if !errors.Is(err, ErrAdminUserExpired) {
		t.Fatalf("authenticate error = %v, want expired", err)
	}
	if repository.disabledID != "u1" {
		t.Fatalf("disabled id = %q", repository.disabledID)
	}
}

func TestAdminAuthServiceKeepsExpiredExemptUserActive(t *testing.T) {
	expiresAt := time.Now().Add(-time.Minute)
	repository := &fakeAdminAuthRepository{user: model.User{ID: "u-admin", Status: "active", ExpiresAt: &expiresAt}, found: true}
	service, err := NewAdminAuthService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	user, err := service.Authenticate(context.Background(), "hash", time.Now(), func(userID string) bool {
		return userID == "u-admin"
	})
	if err != nil {
		t.Fatalf("authenticate exempt user: %v", err)
	}
	if user.ID != "u-admin" || repository.disabledID != "" {
		t.Fatalf("user = %#v disabled=%q", user, repository.disabledID)
	}
}

func TestAdminAuthServiceRejectsUnknownToken(t *testing.T) {
	service, err := NewAdminAuthService(&fakeAdminAuthRepository{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = service.Authenticate(context.Background(), "hash", time.Now(), nil)
	if !errors.Is(err, ErrInvalidAdminToken) {
		t.Fatalf("authenticate error = %v, want invalid token", err)
	}
}
