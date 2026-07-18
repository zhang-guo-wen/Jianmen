package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestTemporaryAccessServiceCreateIssuesPasswordAfterRepositorySucceeds(t *testing.T) {
	repository := &temporaryAccessRepositoryStub{}
	service, err := NewTemporaryAccessService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	result, err := service.Create(context.Background(), CreateTemporaryAccessInput{
		AuthorizedUserID: "user-1", ResourceType: model.ResourceTypeHostAccount,
		ResourceID: "host-account-1", ExpiresAt: now.Add(time.Hour), Now: now,
	})
	if err != nil {
		t.Fatalf("create temporary access: %v", err)
	}
	if result.ConnectionPassword == "" {
		t.Fatal("connection password is empty")
	}
	if repository.createInput.ConnectionPassword.SecretHash == "" {
		t.Fatal("repository did not receive a hashed connection password")
	}
}

func TestTemporaryAccessServiceCreateRejectsInvalidDuration(t *testing.T) {
	repository := &temporaryAccessRepositoryStub{}
	service, err := NewTemporaryAccessService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	now := time.Now().UTC()
	_, err = service.Create(context.Background(), CreateTemporaryAccessInput{
		AuthorizedUserID: "user-1", ResourceType: model.ResourceTypeHostAccount,
		ResourceID: "host-account-1", ExpiresAt: now.Add(8 * 24 * time.Hour), Now: now,
	})
	if !errors.Is(err, ErrInvalidTemporaryAccess) {
		t.Fatalf("error = %v, want invalid temporary access", err)
	}
}

func TestTemporaryAccessServiceExtendMapsNotFound(t *testing.T) {
	repository := &temporaryAccessRepositoryStub{extendErr: ErrTemporaryAccessNotFound}
	service, err := NewTemporaryAccessService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	now := time.Now().UTC()
	err = service.Extend(context.Background(), "missing", now.Add(time.Hour), now)
	if !errors.Is(err, ErrTemporaryAccessNotFound) {
		t.Fatalf("error = %v, want temporary access not found", err)
	}
}

type temporaryAccessRepositoryStub struct {
	createInput CreateTemporaryAccessInput
	createErr   error
	extendErr   error
	disableErr  error
}

func (s *temporaryAccessRepositoryStub) CreateTemporaryAccess(_ context.Context, input CreateTemporaryAccessInput) (TemporaryAccessResult, error) {
	s.createInput = input
	if s.createErr != nil {
		return TemporaryAccessResult{}, s.createErr
	}
	return TemporaryAccessResult{Account: model.TemporaryAccount{ID: "temporary-1"}}, nil
}

func (s *temporaryAccessRepositoryStub) ExtendTemporaryAccess(_ context.Context, _ string, _ time.Time) error {
	return s.extendErr
}

func (s *temporaryAccessRepositoryStub) DisableTemporaryAccess(_ context.Context, _ string, _ time.Time) error {
	return s.disableErr
}
