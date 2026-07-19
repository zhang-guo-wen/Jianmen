package admin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func TestTargetUpdatePermissionCannotMutateHostOwnershipOrEndpoint(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	const userID = "target-update-only-user"
	seedResourceAuthorizationUser(t, db, userID)
	seedGlobalAction(t, db, userID, rbac.ActionTargetUpdate)

	firstHost := model.Host{
		ID: "target-owner-host", Name: "owner", Address: "10.0.1.10",
		Port: 22, Protocol: "ssh", GroupName: "owner-group", Remark: "owner", Status: "active",
	}
	secondHost := model.Host{
		ID: "target-spoof-host", Name: "spoof", Address: "10.0.1.20",
		Port: 2222, Protocol: "ssh", GroupName: "spoof-group", Remark: "spoof", Status: "active",
	}
	account := model.HostAccount{
		ID: "target-update-account", HostID: firstHost.ID, Name: "deploy",
		Username: "deploy", Status: "active", ResourceSeq: 901, ResourceID: "H901",
	}
	if err := db.Create(&[]model.Host{firstHost, secondHost}).Error; err != nil {
		t.Fatalf("create hosts: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	seedResourceGrant(t, db, userID, model.ResourceTypeHostAccount, account.ID)

	spoofRequest := asTestUser(
		httptest.NewRequest(
			http.MethodPut,
			"/api/targets/"+account.ID,
			bytes.NewBufferString(`{"host_id":"target-spoof-host","host":"203.0.113.200","port":65022,"name":"forged","username":"forged"}`),
		),
		userID,
		userID,
	)
	spoofRecorder := httptest.NewRecorder()
	server.handleTarget(spoofRecorder, spoofRequest)
	if spoofRecorder.Code != http.StatusBadRequest {
		t.Fatalf("spoofed target update status = %d, want 400; body=%s", spoofRecorder.Code, spoofRecorder.Body.String())
	}
	if !strings.Contains(spoofRecorder.Body.String(), "host_id cannot be changed") {
		t.Fatalf("spoofed target update body = %s", spoofRecorder.Body.String())
	}
	assertAdminHostState(t, db, firstHost)
	assertAdminHostState(t, db, secondHost)
	var persistedAccount model.HostAccount
	if err := db.First(&persistedAccount, "id = ?", account.ID).Error; err != nil {
		t.Fatalf("load account after spoofed update: %v", err)
	}
	if persistedAccount.HostID != firstHost.ID || persistedAccount.Username != account.Username {
		t.Fatalf("account changed after spoofed update: %#v", persistedAccount)
	}

	endpointRequest := asTestUser(
		httptest.NewRequest(
			http.MethodPut,
			"/api/targets/"+account.ID,
			bytes.NewBufferString(`{"host_id":"target-owner-host","host":"198.51.100.99","port":62022,"name":"renamed","username":"renamed"}`),
		),
		userID,
		userID,
	)
	endpointRecorder := httptest.NewRecorder()
	server.handleTarget(endpointRecorder, endpointRequest)
	if endpointRecorder.Code != http.StatusOK {
		t.Fatalf("same-host target update status = %d, want 200; body=%s", endpointRecorder.Code, endpointRecorder.Body.String())
	}
	assertAdminHostState(t, db, firstHost)
	assertAdminHostState(t, db, secondHost)
	if err := db.First(&persistedAccount, "id = ?", account.ID).Error; err != nil {
		t.Fatalf("load account after same-host update: %v", err)
	}
	if persistedAccount.HostID != firstHost.ID {
		t.Fatalf("account host_id = %q, want %q", persistedAccount.HostID, firstHost.ID)
	}
	if persistedAccount.Username != "renamed" {
		t.Fatalf("account username = %q, want renamed", persistedAccount.Username)
	}
}

func TestCreateHostCanceledRequestStillUsesBoundedCleanupAndJoinsErrors(t *testing.T) {
	cleanupErr := errors.New("delete host cleanup failed")
	repository := &hostCreateCleanupRepository{cleanupErr: cleanupErr}
	server := &Server{hostTargets: repository}

	request := asTestUser(
		httptest.NewRequest(
			http.MethodPost,
			"/api/hosts",
			bytes.NewBufferString(`{"name":"cleanup","address":"10.0.2.10","port":22}`),
		),
		"cleanup-user",
		"cleanup-user",
	)
	canceledCtx, cancelRequest := context.WithCancel(request.Context())
	cancelRequest()
	request = request.WithContext(canceledCtx)
	recorder := httptest.NewRecorder()

	server.handleCreateHost(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("create host status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if repository.deleteCalls != 1 || repository.deletedID != "cleanup-host" {
		t.Fatalf("cleanup calls = %d, deleted ID = %q", repository.deleteCalls, repository.deletedID)
	}
	if repository.cleanupContextErr != nil {
		t.Fatalf("cleanup context error at call = %v, want nil", repository.cleanupContextErr)
	}
	if !repository.cleanupHasDeadline {
		t.Fatal("cleanup context has no deadline")
	}
	if repository.cleanupRemaining <= 0 || repository.cleanupRemaining > 5*time.Second {
		t.Fatalf("cleanup deadline remaining = %v, want within (0, 5s]", repository.cleanupRemaining)
	}
	if repository.cleanupUserID != "cleanup-user" {
		t.Fatalf("cleanup context user ID = %q, want cleanup-user", repository.cleanupUserID)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "resource grant service unavailable") || !strings.Contains(body, cleanupErr.Error()) {
		t.Fatalf("joined error body = %s", body)
	}
}

type hostCreateCleanupRepository struct {
	adminHostTargetRepository

	cleanupErr         error
	deleteCalls        int
	deletedID          string
	cleanupContextErr  error
	cleanupHasDeadline bool
	cleanupRemaining   time.Duration
	cleanupUserID      string
}

func (r *hostCreateCleanupRepository) AddHost(context.Context, store.HostRecord) (store.HostView, error) {
	return store.HostView{ID: "cleanup-host"}, nil
}

func (r *hostCreateCleanupRepository) DeleteHost(ctx context.Context, id string) error {
	r.deleteCalls++
	r.deletedID = id
	r.cleanupContextErr = ctx.Err()
	deadline, ok := ctx.Deadline()
	r.cleanupHasDeadline = ok
	if ok {
		r.cleanupRemaining = time.Until(deadline)
	}
	r.cleanupUserID, _ = ctx.Value(ctxKeyUserID).(string)
	return r.cleanupErr
}

func assertAdminHostState(t *testing.T, db *gorm.DB, want model.Host) {
	t.Helper()
	var got model.Host
	if err := db.First(&got, "id = ?", want.ID).Error; err != nil {
		t.Fatalf("load host %q: %v", want.ID, err)
	}
	if got.Name != want.Name ||
		got.Address != want.Address ||
		got.Port != want.Port ||
		got.Protocol != want.Protocol ||
		got.GroupName != want.GroupName ||
		got.Remark != want.Remark ||
		got.Status != want.Status {
		t.Fatalf("host %q changed: got %#v, want %#v", want.ID, got, want)
	}
}
