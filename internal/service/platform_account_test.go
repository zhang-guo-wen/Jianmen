package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"jianmen/internal/model"
)

type platformAccountServiceRepository struct {
	records       map[string]model.PlatformAccount
	passwords     map[string]string
	metadataReads int
	passwordReads int
	listErr       error
	passwordErr   error
}

func (r *platformAccountServiceRepository) ListPlatformAccountMetadata(_ context.Context, _, _ string) ([]model.PlatformAccount, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	result := make([]model.PlatformAccount, 0, len(r.records))
	for _, record := range r.records {
		result = append(result, record)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ID > result[right].ID
	})
	return result, nil
}

func (r *platformAccountServiceRepository) GetPlatformAccountMetadata(_ context.Context, id string) (model.PlatformAccount, error) {
	r.metadataReads++
	record, found := r.records[id]
	if !found {
		return model.PlatformAccount{}, errors.New("not found")
	}
	return record, nil
}

func (r *platformAccountServiceRepository) CreateManagedPlatformAccount(_ context.Context, record model.PlatformAccount, _ string) (model.PlatformAccount, error) {
	record.ID = "created"
	r.records[record.ID] = record
	return record, nil
}

func (r *platformAccountServiceRepository) UpdateManagedPlatformAccount(_ context.Context, id string, input model.PlatformAccount) (model.PlatformAccount, error) {
	record, found := r.records[id]
	if !found {
		return model.PlatformAccount{}, errors.New("not found")
	}
	if input.Name != "" {
		record.Name = input.Name
	}
	if input.Password.GetPlaintext() != "" {
		r.passwords[id] = input.Password.GetPlaintext()
	}
	r.records[id] = record
	return record, nil
}

func (r *platformAccountServiceRepository) DeleteManagedPlatformAccount(_ context.Context, id string) error {
	delete(r.records, id)
	return nil
}

func (r *platformAccountServiceRepository) GetPlatformAccountPassword(_ context.Context, id string) (string, error) {
	r.passwordReads++
	if r.passwordErr != nil {
		return "", r.passwordErr
	}
	return r.passwords[id], nil
}

type platformAccountServiceAuthorizer struct {
	allowed      bool
	err          error
	batch        []AuthorizationDecision
	batchErr     error
	requests     []AuthorizationRequest
	connectCalls int
}

func (a *platformAccountServiceAuthorizer) AuthorizeConnection(_ context.Context, _ string, _ []string, _, _ string) (bool, error) {
	a.connectCalls++
	return a.allowed, a.err
}

func (a *platformAccountServiceAuthorizer) AuthorizeBatch(_ context.Context, _ string, requests []AuthorizationRequest) ([]AuthorizationDecision, error) {
	a.requests = requests
	if a.batchErr != nil {
		return nil, a.batchErr
	}
	return a.batch, nil
}

func newPlatformAccountServiceFixture() (*platformAccountServiceRepository, *platformAccountServiceAuthorizer, *PlatformAccountService) {
	repository := &platformAccountServiceRepository{
		records: map[string]model.PlatformAccount{
			"visible": {ID: "visible", Name: "visible", Username: "alice", Status: "active", HasPassword: true},
			"hidden":  {ID: "hidden", Name: "hidden", Username: "bob", Status: "active", HasPassword: true},
		},
		passwords: map[string]string{"visible": "secret"},
	}
	authorizer := &platformAccountServiceAuthorizer{allowed: true, batch: []AuthorizationDecision{{Allowed: true}, {Allowed: false}}}
	service, _ := NewPlatformAccountService(repository, authorizer)
	return repository, authorizer, service
}

func TestPlatformAccountServiceListFiltersUnauthorizedRecords(t *testing.T) {
	_, authorizer, service := newPlatformAccountServiceFixture()
	accounts, err := service.List(context.Background(), PlatformAccountActor{UserID: "user"}, "", "")
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != "visible" {
		t.Fatalf("visible accounts = %#v", accounts)
	}
	if len(authorizer.requests) != 2 {
		t.Fatalf("authorization requests = %#v", authorizer.requests)
	}
}

func TestPlatformAccountServiceGetAndPasswordAuthorizeBeforeRead(t *testing.T) {
	repository, authorizer, service := newPlatformAccountServiceFixture()
	authorizer.allowed = false
	if _, err := service.Get(context.Background(), PlatformAccountActor{UserID: "user"}, "visible"); !errors.Is(err, ErrPlatformAccountForbidden) {
		t.Fatalf("Get() error = %v", err)
	}
	if repository.metadataReads != 0 {
		t.Fatalf("metadata reads = %d, want 0 before denied get", repository.metadataReads)
	}
	if _, err := service.Password(context.Background(), PlatformAccountActor{UserID: "user"}, "visible"); !errors.Is(err, ErrPlatformAccountForbidden) {
		t.Fatalf("Password() error = %v", err)
	}
	if repository.passwordReads != 0 {
		t.Fatalf("password reads = %d, want 0 before denied use", repository.passwordReads)
	}
}

func TestPlatformAccountServicePasswordRejectsUnavailableBeforeSecretRead(t *testing.T) {
	repository, _, service := newPlatformAccountServiceFixture()
	record := repository.records["visible"]
	record.Status = "disabled"
	repository.records["visible"] = record
	if _, err := service.Password(context.Background(), PlatformAccountActor{UserID: "admin", SuperAdmin: true}, "visible"); !errors.Is(err, ErrPlatformAccountUnavailable) {
		t.Fatalf("disabled Password() error = %v", err)
	}
	if repository.passwordReads != 0 {
		t.Fatalf("password reads for disabled account = %d, want 0", repository.passwordReads)
	}
	expiresAt := time.Now().UTC().Add(-time.Minute)
	record.Status, record.ExpiresAt = "active", &expiresAt
	repository.records["visible"] = record
	if _, err := service.Password(context.Background(), PlatformAccountActor{UserID: "admin", SuperAdmin: true}, "visible"); !errors.Is(err, ErrPlatformAccountUnavailable) {
		t.Fatalf("expired Password() error = %v", err)
	}
	if repository.passwordReads != 0 {
		t.Fatalf("password reads for expired account = %d, want 0", repository.passwordReads)
	}
}

func TestPlatformAccountServiceFailsClosedOnRepositoryAndAuthorizerErrors(t *testing.T) {
	repository, authorizer, service := newPlatformAccountServiceFixture()
	authorizer.err = errors.New("authorization unavailable")
	if _, err := service.Get(context.Background(), PlatformAccountActor{UserID: "user"}, "visible"); err == nil {
		t.Fatal("Get() succeeded despite authorizer error")
	}
	if repository.metadataReads != 0 {
		t.Fatalf("metadata reads = %d, want 0 after authorizer error", repository.metadataReads)
	}
	authorizer.err = nil
	repository.passwordErr = errors.New("password repository failed")
	if password, err := service.Password(context.Background(), PlatformAccountActor{UserID: "user"}, "visible"); err == nil || password != "" {
		t.Fatalf("Password() = %q, %v; want empty password and error", password, err)
	}
}

func TestPlatformAccountServiceUpdateBlankPasswordKeepsStoredPassword(t *testing.T) {
	repository, _, service := newPlatformAccountServiceFixture()
	if _, err := service.Update(context.Background(), PlatformAccountActor{UserID: "admin", SuperAdmin: true}, "visible", PlatformAccountRequest{Name: "renamed"}); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	if repository.passwords["visible"] != "secret" {
		t.Fatalf("password = %q, want preserved secret", repository.passwords["visible"])
	}
}
