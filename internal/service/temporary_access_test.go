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
	_, err = service.Extend(context.Background(), "missing", now.Add(time.Hour), now)
	if !errors.Is(err, ErrTemporaryAccessNotFound) {
		t.Fatalf("error = %v, want temporary access not found", err)
	}
}

func TestTemporaryAccessServiceListNormalizesQueryAndPagination(t *testing.T) {
	repository := &temporaryAccessRepositoryStub{
		listResult: TemporaryAccessPage{
			Items: []TemporaryAccessDetails{{ID: "temporary-1"}},
			Total: 1,
		},
	}
	temporaryAccess, err := NewTemporaryAccessService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	page, err := temporaryAccess.List(context.Background(), "  incident  ", 0, 500, time.Time{})
	if err != nil {
		t.Fatalf("list temporary access: %v", err)
	}
	if repository.listParams.Query != "incident" || repository.listParams.Page != 1 || repository.listParams.PageSize != 200 || repository.listParams.Now.IsZero() {
		t.Fatalf("unexpected normalized list params: %#v", repository.listParams)
	}
	if page.Page != 1 || page.PageSize != 200 || len(page.Items) != 1 {
		t.Fatalf("unexpected page: %#v", page)
	}
}

func TestTemporaryAccessServiceConnectionTargetRejectsMissingResource(t *testing.T) {
	repository := &temporaryAccessRepositoryStub{targetErr: ErrTemporaryAccessNotFound}
	temporaryAccess, err := NewTemporaryAccessService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = temporaryAccess.ConnectionTarget(context.Background(), model.ResourceTypeHostAccount, "missing")
	if !errors.Is(err, ErrTemporaryAccessNotFound) {
		t.Fatalf("error = %v, want temporary access not found", err)
	}
}

type temporaryAccessRepositoryStub struct {
	createInput CreateTemporaryAccessInput
	createErr   error
	extendErr   error
	disableErr  error
	listParams  TemporaryAccessListParams
	listResult  TemporaryAccessPage
	targetErr   error
}

func (s *temporaryAccessRepositoryStub) CreateTemporaryAccess(_ context.Context, input CreateTemporaryAccessInput) (TemporaryAccessResult, error) {
	s.createInput = input
	if s.createErr != nil {
		return TemporaryAccessResult{}, s.createErr
	}
	return TemporaryAccessResult{Account: model.TemporaryAccount{ID: "temporary-1"}}, nil
}

func (s *temporaryAccessRepositoryStub) CreateTemporaryAIAccess(_ context.Context, input CreateTemporaryAIAccessInput) (TemporaryAIAccessResult, error) {
	return TemporaryAIAccessResult{Token: input.Token}, nil
}

func (s *temporaryAccessRepositoryStub) ExtendTemporaryAccess(_ context.Context, _ string, _, _ time.Time) error {
	return s.extendErr
}

func (s *temporaryAccessRepositoryStub) DisableTemporaryAccess(_ context.Context, _ string, _ time.Time) error {
	return s.disableErr
}

func (s *temporaryAccessRepositoryStub) ListTemporaryAccess(_ context.Context, params TemporaryAccessListParams) (TemporaryAccessPage, error) {
	s.listParams = params
	return s.listResult, nil
}

func (s *temporaryAccessRepositoryStub) GetTemporaryAccess(_ context.Context, id string) (TemporaryAccessDetails, error) {
	return TemporaryAccessDetails{ID: id}, nil
}

func (s *temporaryAccessRepositoryStub) TemporaryConnectionTarget(_ context.Context, _, _ string) (TemporaryConnectionTarget, error) {
	return TemporaryConnectionTarget{}, s.targetErr
}
