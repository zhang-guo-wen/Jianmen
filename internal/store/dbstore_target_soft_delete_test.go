package store

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"
)

func TestDeletedTargetsAreExcludedFromReadAndConnectionPaths(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	hosts := []model.Host{
		{
			ID: "ssh-host", Name: "ssh-host", Address: "127.0.0.1",
			Port: 22, Protocol: "ssh", Status: "active",
		},
		{
			ID: "rdp-host", Name: "rdp-host", Address: "127.0.0.2",
			Port: 3389, Protocol: "rdp", Status: "active",
		},
		{
			ID: "deleted-parent-host", Name: "deleted-parent-host", Address: "127.0.0.3",
			Port: 22, Protocol: "ssh", Status: "active",
		},
		{
			ID: "deleted-parent-rdp-host", Name: "deleted-parent-rdp-host", Address: "127.0.0.4",
			Port: 3389, Protocol: "rdp", Status: "active",
		},
	}
	if err := db.Create(&hosts).Error; err != nil {
		t.Fatalf("create hosts: %v", err)
	}
	accounts := []model.HostAccount{
		{
			ID: "active-target", HostID: "ssh-host", Name: "active",
			Username: "active", Status: "active", ResourceID: "H001",
		},
		{
			ID: "deleted-target", HostID: "ssh-host", Name: "deleted",
			Username: "deleted", Status: "active", ResourceID: "H002",
		},
		{
			ID: "deleted-rdp-target", HostID: "rdp-host", Name: "deleted-rdp",
			Username: "deleted-rdp", Status: "active", ResourceID: "H003",
		},
		{
			ID: "orphaned-target", HostID: "deleted-parent-host", Name: "orphaned",
			Username: "orphaned", Status: "active", ResourceID: "H004",
		},
		{
			ID: "orphaned-rdp-target", HostID: "deleted-parent-rdp-host", Name: "orphaned-rdp",
			Username: "orphaned-rdp", Status: "active", ResourceID: "H005",
		},
	}
	if err := db.Create(&accounts).Error; err != nil {
		t.Fatalf("create host accounts: %v", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash test password: %v", err)
	}
	if err := db.Create(&model.User{
		ID: "user", Username: "user", PasswordHash: string(passwordHash), Status: "active",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&model.UserSession{
		UserID: "user", SessionSeq: 1, SessionID: "00001", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create user session: %v", err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate public-key credential: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create public-key signer: %v", err)
	}
	if err := db.Create(&model.UserPublicKey{
		ID: "user-key", UserID: "user", Name: "test",
		PublicKey: string(ssh.MarshalAuthorizedKey(signer.PublicKey())),
	}).Error; err != nil {
		t.Fatalf("create user public key: %v", err)
	}

	repository := NewDBStore(db)
	ctx := context.Background()
	for _, id := range []string{"deleted-target", "deleted-rdp-target"} {
		if err := repository.DeleteTarget(ctx, id); err != nil {
			t.Fatalf("delete target %q: %v", id, err)
		}
	}
	if err := db.Model(&model.Host{}).
		Where("id IN ?", []string{"deleted-parent-host", "deleted-parent-rdp-host"}).
		Update("deleted_at", nil).Error; err != nil {
		t.Fatalf("soft-delete parent hosts: %v", err)
	}

	var tombstoneCount int64
	if err := db.Table("host_accounts").
		Where("id IN ? AND deleted_at IS NULL", []string{"deleted-target", "deleted-rdp-target"}).
		Count(&tombstoneCount).Error; err != nil {
		t.Fatalf("count target tombstones: %v", err)
	}
	if tombstoneCount != 2 {
		t.Fatalf("target tombstone count = %d, want 2", tombstoneCount)
	}

	targets, err := repository.Targets(ctx)
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != "active-target" {
		t.Fatalf("targets after delete = %#v, want only active-target", targets)
	}

	hostAccounts, err := repository.ListHostAccounts(ctx, "ssh-host")
	if err != nil {
		t.Fatalf("list host accounts: %v", err)
	}
	if len(hostAccounts) != 1 || hostAccounts[0].ID != "active-target" {
		t.Fatalf("host accounts after delete = %#v, want only active-target", hostAccounts)
	}
	orphanedHostAccounts, err := repository.ListHostAccounts(ctx, "deleted-parent-host")
	if err != nil {
		t.Fatalf("list orphaned host accounts: %v", err)
	}
	if len(orphanedHostAccounts) != 0 {
		t.Fatalf("orphaned host accounts = %#v, want none", orphanedHostAccounts)
	}

	if _, err := repository.Target(ctx, "deleted-target"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("deleted Target error = %v, want ErrTargetNotFound", err)
	}
	if _, err := repository.TargetConfig(ctx, "deleted-target"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("deleted TargetConfig error = %v, want ErrTargetNotFound", err)
	}
	if _, err := repository.UpdateTarget(ctx, "deleted-target", config.Target{Username: "revived"}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("update deleted target error = %v, want ErrTargetNotFound", err)
	}
	if err := repository.DeleteTarget(ctx, "deleted-target"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("delete target twice error = %v, want ErrTargetNotFound", err)
	}
	if _, err := repository.Target(ctx, "orphaned-target"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("orphaned Target error = %v, want ErrTargetNotFound", err)
	}
	if _, err := repository.TargetConfig(ctx, "orphaned-target"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("orphaned TargetConfig error = %v, want ErrTargetNotFound", err)
	}
	if _, err := repository.UpdateTarget(ctx, "orphaned-target", config.Target{Username: "revived"}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("update orphaned target error = %v, want ErrTargetNotFound", err)
	}
	if _, err := repository.AddTarget(ctx, config.Target{
		ID: "new-on-deleted-host", HostID: "deleted-parent-host",
		Host: "127.0.0.3", Port: 22, Protocol: "ssh", Username: "root",
	}); !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("create target on deleted host error = %v, want ErrHostNotFound", err)
	}

	defaultTarget, err := repository.DefaultTarget(ctx, model.User{ID: "user"})
	if err != nil {
		t.Fatalf("default target: %v", err)
	}
	if defaultTarget.ID != "active-target" {
		t.Fatalf("default target = %q, want active-target", defaultTarget.ID)
	}
	if _, err := repository.DefaultTarget(ctx, model.User{ID: "user", RequestedTargetID: "deleted-target"}); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("deleted requested target error = %v, want ErrTargetUnavailable", err)
	}
	if _, err := repository.DefaultTarget(ctx, model.User{ID: "user", RequestedTargetID: "orphaned-target"}); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("orphaned requested target error = %v, want ErrTargetUnavailable", err)
	}

	if account, found, err := repository.FindActiveHostAccount(ctx, "deleted-target"); err != nil || found {
		t.Fatalf("FindActiveHostAccount deleted target = (%#v, %v, %v), want not found", account, found, err)
	}
	if account, found, err := repository.FindActiveHostAccount(ctx, "orphaned-target"); err != nil || found {
		t.Fatalf("FindActiveHostAccount orphaned target = (%#v, %v, %v), want not found", account, found, err)
	}
	if host, found, err := repository.FindActiveHost(ctx, "deleted-parent-host"); err != nil || found {
		t.Fatalf("FindActiveHost deleted parent = (%#v, %v, %v), want not found", host, found, err)
	}
	if user, err := repository.Authenticate(ctx, "H"+accounts[0].ResourceID+"00001", "test-password"); err != nil || user.RequestedTargetID != "active-target" {
		t.Fatalf("compact authentication active target = (%#v, %v), want active-target", user, err)
	}
	if user, err := repository.AuthenticatePublicKey(ctx, "H"+accounts[0].ResourceID+"00001", signer.PublicKey()); err != nil || user.RequestedTargetID != "active-target" {
		t.Fatalf("public-key authentication active target = (%#v, %v), want active-target", user, err)
	}
	if _, err := repository.Authenticate(ctx, "H"+accounts[1].ResourceID+"00001", "test-password"); err == nil {
		t.Fatal("compact authentication accepted deleted target")
	}
	if _, err := repository.Authenticate(ctx, "H"+accounts[3].ResourceID+"00001", "test-password"); err == nil {
		t.Fatal("compact authentication accepted target with deleted parent host")
	}
	if _, err := repository.AuthenticatePublicKey(ctx, "H"+accounts[1].ResourceID+"00001", signer.PublicKey()); err == nil {
		t.Fatal("public-key authentication accepted deleted target")
	}
	if _, err := repository.AuthenticatePublicKey(ctx, "H"+accounts[3].ResourceID+"00001", signer.PublicKey()); err == nil {
		t.Fatal("public-key authentication accepted target with deleted parent host")
	}
	if _, err := repository.ContainerHostAccount(ctx, "deleted-target"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("deleted container host account error = %v, want ErrTargetNotFound", err)
	}
	if _, err := repository.ContainerHostAccount(ctx, "orphaned-target"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("orphaned container host account error = %v, want ErrTargetNotFound", err)
	}
	if _, err := repository.TemporaryConnectionTarget(ctx, model.ResourceTypeHostAccount, "deleted-target"); !errors.Is(err, service.ErrTemporaryAccessNotFound) {
		t.Fatalf("deleted temporary connection target error = %v, want ErrTemporaryAccessNotFound", err)
	}
	if _, err := repository.TemporaryConnectionTarget(ctx, model.ResourceTypeHostAccount, "orphaned-target"); !errors.Is(err, service.ErrTemporaryAccessNotFound) {
		t.Fatalf("orphaned temporary connection target error = %v, want ErrTemporaryAccessNotFound", err)
	}
	if _, err := repository.WebRDPTarget(ctx, "deleted-rdp-target"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("deleted Web RDP target error = %v, want ErrTargetNotFound", err)
	}
	if _, err := repository.WebRDPTarget(ctx, "orphaned-rdp-target"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("orphaned Web RDP target error = %v, want ErrTargetNotFound", err)
	}
}
