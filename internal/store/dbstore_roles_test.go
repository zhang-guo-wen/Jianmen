package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"

	"gorm.io/gorm"
)

func TestDBStoreCreateRoleMapsUniqueConstraintToConflict(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	st := NewDBStore(db)
	if _, err := st.CreateRole(context.Background(), model.Role{Name: "operators", Status: "active"}); err != nil {
		t.Fatalf("create first role: %v", err)
	}
	if _, err := st.CreateRole(context.Background(), model.Role{Name: "operators", Status: "active"}); err == nil {
		t.Fatal("duplicate role creation succeeded")
	}
}

func TestDBStorePermissionBusinessKeyConflictsOnCreateAndUpdate(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	st := NewDBStore(db)
	first, err := st.CreatePermission(context.Background(), model.Permission{
		Action: "host:view", Effect: model.PermissionEffectAllow,
	})
	if err != nil {
		t.Fatalf("create first permission: %v", err)
	}
	if _, err := st.CreatePermission(context.Background(), model.Permission{
		Action: "host:view", Effect: model.PermissionEffectAllow,
	}); !isConflictMarked(err) {
		t.Fatalf("duplicate create error = %v, want conflict marker", err)
	}
	second, err := st.CreatePermission(context.Background(), model.Permission{
		Action: "dbproxy:view", Effect: model.PermissionEffectAllow,
	})
	if err != nil {
		t.Fatalf("create second permission: %v", err)
	}
	second.Action = first.Action
	if _, err := st.UpdatePermission(context.Background(), second); !isConflictMarked(err) {
		t.Fatalf("duplicate update error = %v, want conflict marker", err)
	}
}

func TestDBStoreReplaceRoleActionsPreservesResourceAndDenyBindings(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	st := NewDBStore(db)
	role, err := st.CreateRole(context.Background(), model.Role{Name: "operators", Status: "active"})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	resource := model.Permission{Action: "session:connect", ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-1", Effect: model.PermissionEffectAllow}
	deny := model.Permission{Action: "host:view", Effect: model.PermissionEffectDeny}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource permission: %v", err)
	}
	if err := db.Create(&deny).Error; err != nil {
		t.Fatalf("create deny permission: %v", err)
	}
	if err := db.Create(&[]model.RolePermission{{RoleID: role.ID, PermissionID: resource.ID}, {RoleID: role.ID, PermissionID: deny.ID}}).Error; err != nil {
		t.Fatalf("create retained bindings: %v", err)
	}
	if err := st.ReplaceRoleActions(context.Background(), role.ID, []model.Permission{{Action: "host:view", Name: "Host view", Effect: model.PermissionEffectAllow}}); err != nil {
		t.Fatalf("replace actions: %v", err)
	}
	actions, err := st.RoleActions(context.Background(), role.ID)
	if err != nil {
		t.Fatalf("list role actions: %v", err)
	}
	if len(actions) != 1 || actions[0] != "host:view" {
		t.Fatalf("role actions = %#v, want [host:view]", actions)
	}
	var bindings []model.RolePermission
	if err := db.Where("role_id = ?", role.ID).Find(&bindings).Error; err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	if len(bindings) != 3 {
		t.Fatalf("binding count = %d, want 3", len(bindings))
	}
}

func TestDBStoreReplaceRoleActionsRollsBackRemovedBindings(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	st := NewDBStore(db)
	role, err := st.CreateRole(context.Background(), model.Role{Name: "operators", Status: "active"})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	oldPermission, err := st.CreatePermission(context.Background(), model.Permission{Action: "host:view", Effect: model.PermissionEffectAllow})
	if err != nil {
		t.Fatalf("create old permission: %v", err)
	}
	oldBinding, err := st.CreateRolePermission(context.Background(), model.RolePermission{RoleID: role.ID, PermissionID: oldPermission.ID})
	if err != nil {
		t.Fatalf("create old binding: %v", err)
	}
	injected := errors.New("injected role binding failure")
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail_role_binding", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "RolePermission" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	defer db.Callback().Create().Remove("test:fail_role_binding")

	err = st.ReplaceRoleActions(context.Background(), role.ID, []model.Permission{{Action: "dbproxy:view", Effect: model.PermissionEffectAllow}})
	if !errors.Is(err, injected) {
		t.Fatalf("replace actions error = %v, want injected failure", err)
	}
	var count int64
	if err := db.Model(&model.RolePermission{}).Where("id = ?", oldBinding.ID).Count(&count).Error; err != nil {
		t.Fatalf("count old binding: %v", err)
	}
	if count != 1 {
		t.Fatalf("old binding count = %d, want 1 after rollback", count)
	}
}

func TestDBStoreReplaceRoleActionsSerializesConcurrentUpdates(t *testing.T) {
	dsn := fmt.Sprintf("file:role-actions-%d?mode=memory&cache=shared&_busy_timeout=5000", time.Now().UnixNano())
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 4})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	st := NewDBStore(db)
	role, err := st.CreateRole(context.Background(), model.Role{Name: "operators", Status: "active"})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			results <- st.ReplaceRoleActions(context.Background(), role.ID, []model.Permission{{Action: "host:view", Effect: model.PermissionEffectAllow}})
		}()
	}
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent replace: %v", err)
		}
	}
	var permissionCount int64
	if err := db.Model(&model.Permission{}).
		Where("action = ? AND resource_type = '' AND resource_id = '' AND effect = ?", "host:view", model.PermissionEffectAllow).
		Count(&permissionCount).Error; err != nil {
		t.Fatalf("count action permissions: %v", err)
	}
	if permissionCount != 1 {
		t.Fatalf("action permission count = %d, want 1", permissionCount)
	}
}

func isConflictMarked(err error) bool {
	var marker interface{ Conflict() bool }
	return errors.As(err, &marker) && marker.Conflict()
}
