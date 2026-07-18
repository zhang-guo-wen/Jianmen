package admin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

func TestEffectiveGlobalActionsExcludesResourcePermissionsAndHonorsDeny(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	role := model.Role{ID: "role", Name: "role", Status: "active"}
	permissions := []model.Permission{
		{ID: "allow-host", Action: "host:view", Effect: model.PermissionEffectAllow},
		{ID: "deny-host", Action: "host:view", Effect: model.PermissionEffectDeny},
		{ID: "allow-db", Action: "dbproxy:view", Effect: model.PermissionEffectAllow},
		{ID: "resource-app", Action: "app:connect", ResourceType: model.ResourceTypeApplication, ResourceID: "app1", Effect: model.PermissionEffectAllow},
	}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := db.Create(&model.UserRole{ID: "ur", UserID: "u1", RoleID: role.ID}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}
	for _, permission := range permissions {
		if err := db.Create(&permission).Error; err != nil {
			t.Fatalf("create permission: %v", err)
		}
		if err := db.Create(&model.RolePermission{RoleID: role.ID, PermissionID: permission.ID}).Error; err != nil {
			t.Fatalf("create role permission: %v", err)
		}
	}

	actions, err := (&Server{db: db, store: store.NewDBStore(db)}).effectiveGlobalActions(context.Background(), "u1")
	if err != nil {
		t.Fatalf("effectiveGlobalActions: %v", err)
	}
	if want := []string{"dbproxy:view"}; !reflect.DeepEqual(actions, want) {
		t.Fatalf("actions = %#v, want %#v", actions, want)
	}
}

func TestMePermissionsPropagatesRequestCancellation(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	repository := &contextObservingRoleRepository{RoleManagementRepository: store.NewDBStore(db)}
	roles, err := service.NewRoleService(repository)
	if err != nil {
		t.Fatalf("new role service: %v", err)
	}
	server := &Server{
		db:             db,
		roleManagement: roles,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/me/permissions", nil).WithContext(ctx)
	req = withTestUser(req, "regular-user", "regular-user")
	rec := httptest.NewRecorder()

	server.handleMePermissions(rec, req)

	if !errors.Is(repository.contextError, context.Canceled) {
		t.Fatalf("repository context error = %v, want context.Canceled", repository.contextError)
	}
}

func TestMePermissionsDoesNotExposeRepositoryError(t *testing.T) {
	const sensitive = "dial tcp secret.internal:5432 password=top-secret"

	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	repository := &failingEffectiveRoleRepository{
		RoleManagementRepository: store.NewDBStore(db),
		err:                      errors.New(sensitive),
	}
	roles, err := service.NewRoleService(repository)
	if err != nil {
		t.Fatalf("new role service: %v", err)
	}
	var logs bytes.Buffer
	server := &Server{
		db:             db,
		roleManagement: roles,
		logger:         slog.New(slog.NewTextHandler(&logs, nil)),
	}
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/me/permissions", nil), "regular-user", "regular-user")
	rec := httptest.NewRecorder()

	server.handleMePermissions(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), sensitive) {
		t.Fatalf("response leaked repository error: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "effective permissions unavailable") {
		t.Fatalf("response missing fixed error: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), sensitive) {
		t.Fatalf("logger did not receive repository error: %s", logs.String())
	}
}

type contextObservingRoleRepository struct {
	service.RoleManagementRepository
	contextError error
}

func (r *contextObservingRoleRepository) EffectiveGlobalPermissions(ctx context.Context, _ string, _ time.Time) ([]model.Permission, error) {
	r.contextError = ctx.Err()
	return nil, ctx.Err()
}

type failingEffectiveRoleRepository struct {
	service.RoleManagementRepository
	err error
}

func (r *failingEffectiveRoleRepository) EffectiveGlobalPermissions(context.Context, string, time.Time) ([]model.Permission, error) {
	return nil, r.err
}
