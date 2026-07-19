package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestCreateManagedHostGrantFailureRollsBackHostResourceAndGrant(t *testing.T) {
	repository, db := newHostTargetMutationTestStore(t)
	creator := model.User{ID: "host-creator-rollback", Username: "host-creator-rollback", Status: "active"}
	if err := db.Create(&creator).Error; err != nil {
		t.Fatalf("create host creator: %v", err)
	}
	injectedErr := errors.New("injected host creator grant failure")
	const callbackName = "test:fail-host-creator-grant"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "ResourceGrant" {
			tx.AddError(injectedErr)
		}
	}); err != nil {
		t.Fatalf("register creator grant failure: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callbackName); err != nil {
			t.Errorf("remove creator grant failure: %v", err)
		}
	})

	_, err := repository.CreateManagedHost(context.Background(), HostRecord{
		ID: "host-creator-orphan", Name: "orphan", Address: "10.0.0.99", Port: 22, Protocol: "ssh",
	}, creator.ID)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("CreateManagedHost error = %v, want %v", err, injectedErr)
	}
	assertHostTargetArtifactCount(t, db, &model.Host{}, "id = ?", []any{"host-creator-orphan"}, 0)
	assertHostTargetArtifactCount(t, db, &model.Resource{}, "type = ? AND resource_id = ?", []any{model.ResourceTypeHost, "host-creator-orphan"}, 0)
	assertHostTargetArtifactCount(t, db, &model.ResourceGrant{}, "principal_id = ? AND resource_id = ?", []any{creator.ID, "host-creator-orphan"}, 0)
}

func TestAddTargetRejectsInvalidExpiryWithoutArtifacts(t *testing.T) {
	repository, db := newHostTargetMutationTestStore(t)

	_, err := repository.AddTarget(context.Background(), config.Target{
		ID:        "invalid-expiry-account",
		HostID:    "invalid-expiry-host",
		Host:      "10.0.0.10",
		Port:      22,
		Username:  "root",
		Group:     "invalid-expiry-group",
		ExpiresAt: "not-a-timestamp",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid expires_at") {
		t.Fatalf("AddTarget error = %v, want invalid expires_at", err)
	}

	assertHostTargetArtifactCount(t, db, &model.Host{}, "id = ?", []any{"invalid-expiry-host"}, 0)
	assertHostTargetArtifactCount(t, db, &model.HostAccount{}, "id = ?", []any{"invalid-expiry-account"}, 0)
	assertHostTargetArtifactCount(t, db, &model.Resource{}, "resource_id IN ?", []any{[]string{"invalid-expiry-host", "invalid-expiry-account"}}, 0)
	assertHostTargetArtifactCount(t, db, &model.ResourceGroup{}, "name = ?", []any{"invalid-expiry-group"}, 0)
	assertHostTargetArtifactCount(t, db, &model.ResourceSequence{}, "name = ?", []any{storage.SequenceHostAccount}, 0)
}

func TestAddTargetRollsBackAllArtifactsOnDuplicateAndSyncFailure(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		repository, db := newHostTargetMutationTestStore(t)
		host := model.Host{
			ID: "duplicate-host", Name: "duplicate-host", Address: "10.0.0.20",
			Port: 22, Protocol: "ssh", Status: "active",
		}
		account := model.HostAccount{
			ID: "existing-account", HostID: host.ID, Username: "root",
			Status: "active", ResourceSeq: 1, ResourceID: "H001",
		}
		if err := db.Create(&host).Error; err != nil {
			t.Fatalf("create host: %v", err)
		}
		if err := db.Create(&account).Error; err != nil {
			t.Fatalf("create account: %v", err)
		}

		_, err := repository.AddTarget(context.Background(), config.Target{
			ID:       "duplicate-account",
			HostID:   host.ID,
			Host:     "203.0.113.20",
			Port:     2200,
			Username: account.Username,
			Group:    "duplicate-group",
		})
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("AddTarget error = %v, want duplicate rejection", err)
		}

		var persistedHost model.Host
		if err := db.First(&persistedHost, "id = ?", host.ID).Error; err != nil {
			t.Fatalf("load host: %v", err)
		}
		if persistedHost.Address != host.Address || persistedHost.Port != host.Port || persistedHost.Protocol != host.Protocol {
			t.Fatalf("host changed on duplicate: got %#v, want %#v", persistedHost, host)
		}
		assertHostTargetArtifactCount(t, db, &model.HostAccount{}, "id = ?", []any{"duplicate-account"}, 0)
		assertHostTargetArtifactCount(t, db, &model.Resource{}, "type = ? AND resource_id = ?", []any{model.ResourceTypeHost, host.ID}, 0)
		assertHostTargetArtifactCount(t, db, &model.Resource{}, "type = ? AND resource_id = ?", []any{model.ResourceTypeHostAccount, "duplicate-account"}, 0)
		assertHostTargetArtifactCount(t, db, &model.ResourceGroup{}, "name = ?", []any{"duplicate-group"}, 0)
		assertHostTargetArtifactCount(t, db, &model.ResourceSequence{}, "name = ?", []any{storage.SequenceHostAccount}, 0)
	})

	t.Run("account resource sync failure", func(t *testing.T) {
		repository, db := newHostTargetMutationTestStore(t)
		injectedErr := errors.New("injected account resource sync failure")
		failHostTargetResourceSync(t, db, model.ResourceTypeHostAccount, injectedErr)

		_, err := repository.AddTarget(context.Background(), config.Target{
			ID:       "rollback-account",
			HostID:   "rollback-host",
			Host:     "10.0.0.30",
			Port:     22,
			Username: "deploy",
			Group:    "rollback-group",
		})
		if !errors.Is(err, injectedErr) {
			t.Fatalf("AddTarget error = %v, want %v", err, injectedErr)
		}

		assertHostTargetArtifactCount(t, db, &model.Host{}, "id = ?", []any{"rollback-host"}, 0)
		assertHostTargetArtifactCount(t, db, &model.HostAccount{}, "id = ?", []any{"rollback-account"}, 0)
		assertHostTargetArtifactCount(t, db, &model.Resource{}, "resource_id IN ?", []any{[]string{"rollback-host", "rollback-account"}}, 0)
		assertHostTargetArtifactCount(t, db, &model.ResourceGroup{}, "name = ?", []any{"rollback-group"}, 0)
		assertHostTargetArtifactCount(t, db, &model.ResourceSequence{}, "name = ?", []any{storage.SequenceHostAccount}, 0)
	})
}

func TestUpdateHostRollsBackHostGroupAndResourceTogether(t *testing.T) {
	repository, db := newHostTargetMutationTestStore(t)
	host := model.Host{
		ID: "atomic-host", Name: "old-name", Address: "10.0.0.40",
		Port: 22, Protocol: "ssh", GroupName: "old-group", Remark: "old", Status: "active",
	}
	resource := model.Resource{
		Type: model.ResourceTypeHost, ResourceID: host.ID, Name: host.Name,
	}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource: %v", err)
	}
	injectedErr := errors.New("injected host resource sync failure")
	failHostTargetResourceSync(t, db, model.ResourceTypeHost, injectedErr)

	_, err := repository.UpdateHost(context.Background(), host.ID, HostRecord{
		Name: "new-name", Address: "10.0.0.41", Port: 2222,
		Protocol: "ssh", Group: "new-group", Remark: "new", Status: "disabled",
	})
	if !errors.Is(err, injectedErr) {
		t.Fatalf("UpdateHost error = %v, want %v", err, injectedErr)
	}

	var persistedHost model.Host
	if err := db.First(&persistedHost, "id = ?", host.ID).Error; err != nil {
		t.Fatalf("load host: %v", err)
	}
	if persistedHost.Name != host.Name ||
		persistedHost.Address != host.Address ||
		persistedHost.Port != host.Port ||
		persistedHost.Protocol != host.Protocol ||
		persistedHost.GroupName != host.GroupName ||
		persistedHost.Remark != host.Remark ||
		persistedHost.Status != host.Status {
		t.Fatalf("host was partially updated: got %#v, want %#v", persistedHost, host)
	}
	var persistedResource model.Resource
	if err := db.First(&persistedResource, "type = ? AND resource_id = ?", model.ResourceTypeHost, host.ID).Error; err != nil {
		t.Fatalf("load host resource: %v", err)
	}
	if persistedResource.Name != resource.Name {
		t.Fatalf("resource name = %q, want %q", persistedResource.Name, resource.Name)
	}
	assertHostTargetArtifactCount(t, db, &model.ResourceGroup{}, "name = ?", []any{"new-group"}, 0)
}

func TestHostAndTargetMutationsLockTheHostRow(t *testing.T) {
	t.Run("UpdateHost", func(t *testing.T) {
		repository, db := newHostTargetMutationTestStore(t)
		host := model.Host{
			ID: "locked-update-host", Name: "locked-update-host", Address: "10.0.0.50",
			Port: 22, Protocol: "ssh", Status: "active",
		}
		if err := db.Create(&host).Error; err != nil {
			t.Fatalf("create host: %v", err)
		}
		lockObserved := observeHostUpdateLock(t, db)

		if _, err := repository.UpdateHost(context.Background(), host.ID, HostRecord{
			Name: host.Name, Address: host.Address, Port: host.Port, Protocol: host.Protocol,
		}); err != nil {
			t.Fatalf("UpdateHost: %v", err)
		}
		if !lockObserved() {
			t.Fatal("UpdateHost did not issue an UPDATE lock on the host row")
		}
	})

	t.Run("AddTarget", func(t *testing.T) {
		repository, db := newHostTargetMutationTestStore(t)
		host := model.Host{
			ID: "locked-target-host", Name: "locked-target-host", Address: "10.0.0.60",
			Port: 22, Protocol: "ssh", Status: "active",
		}
		if err := db.Create(&host).Error; err != nil {
			t.Fatalf("create host: %v", err)
		}
		lockObserved := observeHostUpdateLock(t, db)

		if _, err := repository.AddTarget(context.Background(), config.Target{
			ID: "locked-target-account", HostID: host.ID, Username: "root",
		}); err != nil {
			t.Fatalf("AddTarget: %v", err)
		}
		if !lockObserved() {
			t.Fatal("AddTarget did not issue an UPDATE lock on the host row")
		}
	})
}

func newHostTargetMutationTestStore(t *testing.T) (*DBStore, *gorm.DB) {
	t.Helper()
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return NewDBStore(db), db
}

func failHostTargetResourceSync(t *testing.T, db *gorm.DB, resourceType string, injectedErr error) {
	t.Helper()
	name := "test:fail-host-target-resource-sync-" + strings.ReplaceAll(resourceType, "_", "-")
	if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		resource, ok := tx.Statement.Dest.(*model.Resource)
		if ok && resource.Type == resourceType {
			tx.AddError(injectedErr)
		}
	}); err != nil {
		t.Fatalf("register resource failure callback: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(name); err != nil {
			t.Errorf("remove resource failure callback: %v", err)
		}
	})
}

func observeHostUpdateLock(t *testing.T, db *gorm.DB) func() bool {
	t.Helper()
	observed := false
	name := "test:observe-host-update-lock"
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "hosts" {
			return
		}
		forClause, ok := tx.Statement.Clauses["FOR"]
		if !ok {
			return
		}
		locking, ok := forClause.Expression.(clause.Locking)
		if ok && strings.EqualFold(locking.Strength, "UPDATE") {
			observed = true
		}
	}); err != nil {
		t.Fatalf("register host lock observer: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(name); err != nil {
			t.Errorf("remove host lock observer: %v", err)
		}
	})
	return func() bool { return observed }
}

func assertHostTargetArtifactCount(
	t *testing.T,
	db *gorm.DB,
	value any,
	query string,
	args []any,
	want int64,
) {
	t.Helper()
	var count int64
	if err := db.Model(value).Where(query, args...).Count(&count).Error; err != nil {
		t.Fatalf("count %T: %v", value, err)
	}
	if count != want {
		t.Fatalf("%T count = %d, want %d", value, count, want)
	}
}
