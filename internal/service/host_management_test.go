package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHostManagementCreateHostCompensatesWithIndependentContext(t *testing.T) {
	repository := &hostManagementCreateRepository{}
	service, err := NewHostManagementService(repository, hostManagementAllowAuthorizer{}, hostManagementFailingGrant{})
	if err != nil {
		t.Fatalf("NewHostManagementService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.CreateHost(ctx, HostManagementActor{ID: "creator"}, HostManagementHostRecord{Name: "host"})
	if !errors.Is(err, ErrHostGrantFailed) {
		t.Fatalf("CreateHost error = %v, want grant failure", err)
	}
	if repository.deletedID != "created" {
		t.Fatalf("cleanup deleted %q, want created", repository.deletedID)
	}
	if repository.cleanupContextErr != nil {
		t.Fatalf("cleanup context error = %v, want nil", repository.cleanupContextErr)
	}
	if !repository.cleanupHasDeadline || repository.cleanupRemaining <= 0 || repository.cleanupRemaining > 5*time.Second {
		t.Fatalf("invalid cleanup deadline: has=%v remaining=%v", repository.cleanupHasDeadline, repository.cleanupRemaining)
	}
}

type hostManagementCreateRepository struct {
	HostManagementRepository
	deletedID          string
	cleanupContextErr  error
	cleanupHasDeadline bool
	cleanupRemaining   time.Duration
}

func (r *hostManagementCreateRepository) AddHost(context.Context, HostManagementHostRecord) (HostManagementHostView, error) {
	return HostManagementHostView{ID: "created"}, nil
}

func (r *hostManagementCreateRepository) DeleteHost(ctx context.Context, id string) error {
	r.deletedID = id
	r.cleanupContextErr = ctx.Err()
	deadline, ok := ctx.Deadline()
	r.cleanupHasDeadline = ok
	if ok {
		r.cleanupRemaining = time.Until(deadline)
	}
	return nil
}

type hostManagementAllowAuthorizer struct{}

func (hostManagementAllowAuthorizer) AuthorizeConnection(context.Context, string, []string, string, string) (bool, error) {
	return true, nil
}

func (hostManagementAllowAuthorizer) AuthorizeBatch(context.Context, string, []AuthorizationRequest) ([]AuthorizationDecision, error) {
	return nil, nil
}

type hostManagementFailingGrant struct{}

func (hostManagementFailingGrant) GrantCreatedResource(context.Context, string, bool, string, string) error {
	return errors.New("grant unavailable")
}
