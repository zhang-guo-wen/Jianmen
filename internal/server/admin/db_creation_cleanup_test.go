package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateDatabaseInstanceCleanupIsBoundedAndPreservesErrors(t *testing.T) {
	grantErr := errors.New("creator grant failed")
	deleteErr := errors.New("database instance cleanup failed")
	repository := &databaseInstanceCleanupRepository{deleteErr: deleteErr}
	server := &Server{databases: repository}

	request := httptest.NewRequest(http.MethodPost, "/api/db/instances", nil)
	requestCtx, cancelRequest := context.WithCancel(request.Context())
	cancelRequest()
	request = request.WithContext(requestCtx)

	err := server.cleanupCreatedDatabaseInstance(request, "db-instance-1", grantErr)

	if !repository.deleteCalled {
		t.Fatal("database instance cleanup was not called")
	}
	if repository.deletedID != "db-instance-1" {
		t.Fatalf("deleted database instance = %q, want %q", repository.deletedID, "db-instance-1")
	}
	if repository.deleteContextErr != nil {
		t.Fatalf("cleanup context error = %v, want request cancellation detached", repository.deleteContextErr)
	}
	if !repository.deleteHadDeadline {
		t.Fatal("cleanup context has no deadline")
	}
	now := time.Now()
	if repository.deleteDeadline.Before(now) || repository.deleteDeadline.After(now.Add(5*time.Second)) {
		t.Fatalf("cleanup deadline = %v, want a live deadline no more than 5 seconds away", repository.deleteDeadline)
	}
	if !errors.Is(err, grantErr) {
		t.Fatalf("cleanup error = %v, want original grant error preserved", err)
	}
	if !errors.Is(err, deleteErr) {
		t.Fatalf("cleanup error = %v, want delete error preserved", err)
	}
}

type databaseInstanceCleanupRepository struct {
	adminDatabaseRepository
	deleteErr         error
	deleteCalled      bool
	deletedID         string
	deleteContextErr  error
	deleteDeadline    time.Time
	deleteHadDeadline bool
}

func (r *databaseInstanceCleanupRepository) DeleteDatabaseInstance(ctx context.Context, id string) error {
	r.deleteCalled = true
	r.deletedID = id
	r.deleteContextErr = ctx.Err()
	r.deleteDeadline, r.deleteHadDeadline = ctx.Deadline()
	return r.deleteErr
}
