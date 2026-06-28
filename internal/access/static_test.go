package access

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestAuthenticatePublicKeyWithRequestedTarget(t *testing.T) {
	publicKey := newTestPublicKey(t)
	db := openTestDB(t)
	userID := "u-test"
	password := "testpass"
	seedTestUser(t, db, userID, "testuser", password)
	seedTestUserPublicKey(t, db, userID, publicKey)
	seedTestUserSession(t, db, userID, "00001")
	accountID := seedTestHostAccount(t, db, "0001")
	store := newTestStoreWithDB(t, db, config.User{ID: userID, Username: "testuser"})

	user, err := store.AuthenticatePublicKey(context.Background(), "H000100001", publicKey)
	if err != nil {
		t.Fatalf("AuthenticatePublicKey returned error: %v", err)
	}
	if user.ID != userID || user.Username != "testuser" || user.RequestedTargetID != accountID {
		t.Fatalf("unexpected user: ID=%s Username=%s RequestedTargetID=%s", user.ID, user.Username, user.RequestedTargetID)
	}
}

func TestAuthenticatePublicKeyRejectsUnknownKey(t *testing.T) {
	allowedKey := newTestPublicKey(t)
	presentedKey := newTestPublicKey(t)
	db := openTestDB(t)
	userID := "u-test"
	password := "testpass"
	seedTestUser(t, db, userID, "testuser", password)
	seedTestUserPublicKey(t, db, userID, allowedKey)
	seedTestUserSession(t, db, userID, "00001")
	seedTestHostAccount(t, db, "0001")
	store := newTestStoreWithDB(t, db, config.User{ID: userID, Username: "testuser"})

	if _, err := store.AuthenticatePublicKey(context.Background(), "H000100001", presentedKey); err == nil {
		t.Fatal("AuthenticatePublicKey accepted an unknown key")
	}
}

func TestAuthenticateRejectsEmptyPasswordWhenPasswordUnset(t *testing.T) {
	db := openTestDB(t)
	userID := "u-test"
	password := "testpass"
	seedTestUser(t, db, userID, "testuser", password)
	seedTestUserSession(t, db, userID, "00001")
	seedTestHostAccount(t, db, "0001")
	store := newTestStoreWithDB(t, db, config.User{ID: userID, Username: "testuser"})

	if _, err := store.Authenticate(context.Background(), "H000100001", ""); err == nil {
		t.Fatal("Authenticate accepted an empty password for a user without password auth")
	}
}

func TestHostKeyCallbackForTargetAcceptsConfiguredFingerprint(t *testing.T) {
	publicKey := newTestPublicKey(t)
	callback, err := hostKeyCallbackForTarget(config.Target{
		InsecureIgnoreHostKey: false,
		HostKeyFingerprint:    ssh.FingerprintSHA256(publicKey),
	})
	if err != nil {
		t.Fatalf("hostKeyCallbackForTarget returned error: %v", err)
	}
	if err := callback("target-a", nil, publicKey); err != nil {
		t.Fatalf("callback rejected matching fingerprint: %v", err)
	}
}

func TestHostKeyCallbackForTargetRequiresStrictConfiguration(t *testing.T) {
	_, err := hostKeyCallbackForTarget(config.Target{InsecureIgnoreHostKey: false})
	if err == nil {
		t.Fatal("hostKeyCallbackForTarget accepted strict mode without fingerprint or known_hosts")
	}
}

func TestUpdateTargetPersistsRuntimeTarget(t *testing.T) {
	targetsFile := filepath.Join(t.TempDir(), "targets.json")
	store := newTestStoreWithTargetsFile(t, targetsFile, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})
	if _, err := store.AddTarget(config.Target{
		ID:       "runtime-a",
		Name:     "runtime-a",
		Host:     "127.0.0.2",
		Username: "root",
		Password: "old-secret",
	}); err != nil {
		t.Fatalf("AddTarget returned error: %v", err)
	}

	view, err := store.UpdateTarget("runtime-a", config.Target{
		ID:       "runtime-a",
		Name:     "updated runtime",
		Host:     "10.0.0.2",
		Port:     2200,
		Username: "ubuntu",
	})
	if err != nil {
		t.Fatalf("UpdateTarget returned error: %v", err)
	}
	if view.ID != "runtime-a" || view.Name != "updated runtime" || view.Host != "10.0.0.2" || view.Port != 2200 || view.Username != "ubuntu" {
		t.Fatalf("unexpected view: %#v", view)
	}
	if view.Static {
		t.Fatalf("updated runtime target was marked static: %#v", view)
	}
	if len(view.AuthMethods) != 1 || view.AuthMethods[0] != "password" {
		t.Fatalf("auth methods = %#v, want [password]", view.AuthMethods)
	}

	targets := readRuntimeTargets(t, targetsFile)
	if len(targets) != 1 {
		t.Fatalf("runtime target count = %d, want 1: %#v", len(targets), targets)
	}
	if targets[0].ID != "runtime-a" || targets[0].Host != "10.0.0.2" || targets[0].Password != "old-secret" {
		t.Fatalf("unexpected persisted target: %#v", targets[0])
	}
}

func TestDeleteTargetPersistsRuntimeRemoval(t *testing.T) {
	targetsFile := filepath.Join(t.TempDir(), "targets.json")
	store := newTestStoreWithTargetsFile(t, targetsFile, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})
	if _, err := store.AddTarget(config.Target{
		ID:       "runtime-a",
		Name:     "runtime-a",
		Host:     "127.0.0.2",
		Username: "root",
		Password: "secret",
	}); err != nil {
		t.Fatalf("AddTarget returned error: %v", err)
	}

	if err := store.DeleteTarget("runtime-a"); err != nil {
		t.Fatalf("DeleteTarget returned error: %v", err)
	}
	if _, err := store.Target("runtime-a"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("Target error = %v, want ErrTargetNotFound", err)
	}
	targets := readRuntimeTargets(t, targetsFile)
	if len(targets) != 0 {
		t.Fatalf("runtime target count = %d, want 0: %#v", len(targets), targets)
	}
}

func TestDeletePageManagedTarget(t *testing.T) {
	store := newTestStore(t, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})

	if err := store.DeleteTarget("target-a"); err != nil {
		t.Fatalf("DeleteTarget returned error: %v", err)
	}
	if _, err := store.Target("target-a"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("Target error = %v, want ErrTargetNotFound", err)
	}
}

func TestTargetViewExposesHostAccountResource(t *testing.T) {
	store := newTestStore(t, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})

	view, err := store.Target("target-a")
	if err != nil {
		t.Fatalf("Target returned error: %v", err)
	}
	if view.ResourceType != "host_account" || view.ResourceID != "target-a" {
		t.Fatalf("unexpected resource identity: %#v", view)
	}
	if view.HostResourceID == "" {
		t.Fatalf("HostResourceID was empty: %#v", view)
	}
}

func TestHostResourcePersistsAndOwnsAccounts(t *testing.T) {
	dir := t.TempDir()
	targetsFile := filepath.Join(dir, "targets.json")
	store := newTestStoreWithTargetsFile(t, targetsFile, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})

	host, err := store.AddHost(HostRecord{
		ID:     "prod-a",
		Name:   "Production A",
		Group:  "prod",
		Host:   "10.0.0.10",
		Port:   2201,
		Remark: "primary host",
	})
	if err != nil {
		t.Fatalf("AddHost returned error: %v", err)
	}
	if host.ID != "prod-a" || host.Group != "prod" || host.Remark != "primary host" || host.AccountCount != 0 {
		t.Fatalf("unexpected host view: %#v", host)
	}

	account, err := store.AddTarget(config.Target{
		ID:       "prod-root",
		HostID:   "prod-a",
		Name:     "root account",
		Group:    "ops",
		Remark:   "break glass",
		Host:     "192.0.2.10",
		Port:     2222,
		Username: "root",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("AddTarget returned error: %v", err)
	}
	if account.HostID != "prod-a" || account.HostResourceID != "prod-a" || account.Host != "10.0.0.10" || account.Port != 2201 {
		t.Fatalf("account did not inherit host resource address: %#v", account)
	}
	if account.Group != "ops" || account.Remark != "break glass" {
		t.Fatalf("account group/remark not exposed: %#v", account)
	}

	host, err = store.Host("prod-a")
	if err != nil {
		t.Fatalf("Host returned error: %v", err)
	}
	if host.AccountCount != 1 || host.Group != "prod" || host.Remark != "primary host" {
		t.Fatalf("unexpected aggregated host view: %#v", host)
	}

	accounts, err := store.HostAccounts("prod-a")
	if err != nil {
		t.Fatalf("HostAccounts returned error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != "prod-root" || accounts[0].Group != "ops" || accounts[0].Remark != "break glass" {
		t.Fatalf("unexpected host accounts: %#v", accounts)
	}

	hosts := readRuntimeHosts(t, filepath.Join(dir, "hosts.json"))
	if len(hosts) != 1 || hosts[0].ID != "prod-a" || hosts[0].Group != "prod" || hosts[0].Remark != "primary host" {
		t.Fatalf("unexpected persisted hosts: %#v", hosts)
	}
	targets := readRuntimeTargets(t, targetsFile)
	if len(targets) != 1 || targets[0].HostID != "prod-a" || targets[0].Host != "10.0.0.10" || targets[0].Group != "ops" || targets[0].Remark != "break glass" {
		t.Fatalf("unexpected persisted targets: %#v", targets)
	}

	reloaded := newTestStoreWithTargetsFile(t, targetsFile, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})
	reloadedHost, err := reloaded.Host("prod-a")
	if err != nil {
		t.Fatalf("reloaded Host returned error: %v", err)
	}
	if reloadedHost.AccountCount != 1 || reloadedHost.Group != "prod" || reloadedHost.Remark != "primary host" {
		t.Fatalf("unexpected reloaded host: %#v", reloadedHost)
	}
}

func TestHostNameDefaultsToAddressPortAndParsesEmbeddedPort(t *testing.T) {
	targetsFile := filepath.Join(t.TempDir(), "targets.json")
	store := newTestStoreWithTargetsFile(t, targetsFile, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})

	host, err := store.AddHost(HostRecord{
		ID:   "prod-a",
		Host: "db.example.com:2222",
	})
	if err != nil {
		t.Fatalf("AddHost returned error: %v", err)
	}
	if host.Name != "db.example.com:2222" || host.Host != "db.example.com" || host.Port != 2222 {
		t.Fatalf("unexpected default host view: %#v", host)
	}

	hosts := readRuntimeHosts(t, filepath.Join(filepath.Dir(targetsFile), "hosts.json"))
	if len(hosts) != 1 || hosts[0].Name != "db.example.com:2222" || hosts[0].Host != "db.example.com" || hosts[0].Port != 2222 {
		t.Fatalf("unexpected persisted host: %#v", hosts)
	}
}

func TestHostEmbeddedPortConflictRejected(t *testing.T) {
	store := newTestStoreWithTargetsFile(t, filepath.Join(t.TempDir(), "targets.json"), config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})

	_, err := store.AddHost(HostRecord{
		ID:   "prod-a",
		Host: "db.example.com:2222",
		Port: 22,
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("AddHost error = %v, want conflicting port error", err)
	}
}

func TestDefaultTargetRejectsDisabledExpiredOrHostDisabledAccount(t *testing.T) {
	targetsFile := filepath.Join(t.TempDir(), "targets.json")
	store := newTestStoreWithTargetsFile(t, targetsFile, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})
	if _, err := store.AddHost(HostRecord{
		ID:   "prod-a",
		Host: "10.0.0.10",
	}); err != nil {
		t.Fatalf("AddHost returned error: %v", err)
	}
	for _, target := range []config.Target{
		{
			ID:       "disabled-account",
			HostID:   "prod-a",
			Username: "root",
			Password: "secret",
			Disabled: true,
		},
		{
			ID:        "expired-account",
			HostID:    "prod-a",
			Username:  "deploy",
			Password:  "secret",
			ExpiresAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		},
		{
			ID:        "active-account",
			HostID:    "prod-a",
			Username:  "ops",
			Password:  "secret",
			ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
		},
	} {
		if _, err := store.AddTarget(target); err != nil {
			t.Fatalf("AddTarget(%q) returned error: %v", target.ID, err)
		}
	}

	user := model.User{ID: "u-admin", Username: "admin", RequestedTargetID: "disabled-account"}
	if _, err := store.DefaultTarget(context.Background(), user); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("disabled account error = %v, want ErrTargetUnavailable", err)
	}
	user.RequestedTargetID = "expired-account"
	if _, err := store.DefaultTarget(context.Background(), user); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("expired account error = %v, want ErrTargetUnavailable", err)
	}
	user.RequestedTargetID = "active-account"
	if _, err := store.DefaultTarget(context.Background(), user); err != nil {
		t.Fatalf("active account returned error: %v", err)
	}

	if _, err := store.UpdateHost("prod-a", HostRecord{
		ID:       "prod-a",
		Host:     "10.0.0.10",
		Disabled: true,
	}); err != nil {
		t.Fatalf("UpdateHost returned error: %v", err)
	}
	if _, err := store.DefaultTarget(context.Background(), user); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("host disabled account error = %v, want ErrTargetUnavailable", err)
	}
}

func TestUpdateHostRewritesRuntimeAccountAddress(t *testing.T) {
	targetsFile := filepath.Join(t.TempDir(), "targets.json")
	store := newTestStoreWithTargetsFile(t, targetsFile, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})
	if _, err := store.AddHost(HostRecord{
		ID:   "prod-a",
		Name: "Production A",
		Host: "10.0.0.10",
		Port: 2201,
	}); err != nil {
		t.Fatalf("AddHost returned error: %v", err)
	}
	if _, err := store.AddTarget(config.Target{
		ID:       "prod-root",
		HostID:   "prod-a",
		Name:     "root account",
		Username: "root",
		Password: "secret",
	}); err != nil {
		t.Fatalf("AddTarget returned error: %v", err)
	}

	host, err := store.UpdateHost("prod-a", HostRecord{
		ID:   "prod-a",
		Name: "Production A",
		Host: "10.0.0.20",
		Port: 2202,
	})
	if err != nil {
		t.Fatalf("UpdateHost returned error: %v", err)
	}
	if host.AccountCount != 1 || host.Host != "10.0.0.20" || host.Port != 2202 {
		t.Fatalf("unexpected updated host: %#v", host)
	}

	accounts, err := store.HostAccounts("prod-a")
	if err != nil {
		t.Fatalf("HostAccounts returned error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].Host != "10.0.0.20" || accounts[0].Port != 2202 {
		t.Fatalf("account address was not rewritten: %#v", accounts)
	}
	targets := readRuntimeTargets(t, targetsFile)
	if len(targets) != 1 || targets[0].Host != "10.0.0.20" || targets[0].Port != 2202 {
		t.Fatalf("unexpected persisted targets: %#v", targets)
	}
}

func TestUsersFallbackIDToUsername(t *testing.T) {
	store := newTestStore(t, config.User{
		Username: "operator",
		Password: "operator",
	})

	users := store.Users()
	if len(users) != 1 {
		t.Fatalf("users = %d, want 1: %#v", len(users), users)
	}
	if users[0].ID != "operator" || users[0].Username != "operator" {
		t.Fatalf("unexpected user view: %#v", users[0])
	}
}

func newTestPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		t.Fatalf("NewPublicKey returned error: %v", err)
	}
	return sshPublicKey
}

func newTestStore(t *testing.T, user config.User) *StaticStore {
	t.Helper()
	targetsFile := filepath.Join(t.TempDir(), "targets.json")
	writeRuntimeTargets(t, targetsFile, []config.Target{{
		ID:       "target-a",
		Name:     "target-a",
		Host:     "127.0.0.1",
		Port:     22,
		Username: "root",
		Password: "password",
	}})
	return newTestStoreWithTargetsFile(t, targetsFile, user)
}

func newTestStoreWithTargetsFile(t *testing.T, targetsFile string, user config.User) *StaticStore {
	t.Helper()
	store, err := NewStaticStore(&config.Config{
		TargetsFile: targetsFile,
		Users:       []config.User{user},
	}, nil)
	if err != nil {
		t.Fatalf("NewStaticStore returned error: %v", err)
	}
	return store
}

func writeRuntimeTargets(t *testing.T, path string, targets []config.Target) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", filepath.Dir(path), err)
	}
	raw, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent runtime targets returned error: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", path, err)
	}
}

func readRuntimeTargets(t *testing.T, path string) []config.Target {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", path, err)
	}
	var targets []config.Target
	if err := json.Unmarshal(raw, &targets); err != nil {
		t.Fatalf("Unmarshal runtime targets returned error: %v", err)
	}
	return targets
}

func readRuntimeHosts(t *testing.T, path string) []HostRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", path, err)
	}
	var hosts []HostRecord
	if err := json.Unmarshal(raw, &hosts); err != nil {
		t.Fatalf("Unmarshal runtime hosts returned error: %v", err)
	}
	return hosts
}

// ---- DB-backed test helpers ----

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate test db: %v", err)
	}
	return db
}

func testPasswordHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("generate bcrypt hash: %v", err)
	}
	return string(hash)
}

func seedTestUser(t *testing.T, db *gorm.DB, id, username, password string) {
	t.Helper()
	user := model.User{
		ID:           id,
		Username:     username,
		PasswordHash: testPasswordHash(t, password),
		Status:       "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func seedTestUserPublicKey(t *testing.T, db *gorm.DB, userID string, key ssh.PublicKey) {
	t.Helper()
	pk := model.UserPublicKey{
		UserID:    userID,
		PublicKey: string(ssh.MarshalAuthorizedKey(key)),
	}
	if err := db.Create(&pk).Error; err != nil {
		t.Fatalf("seed public key: %v", err)
	}
}

func seedTestUserSession(t *testing.T, db *gorm.DB, userID, sessionID string) {
	t.Helper()
	sess := model.UserSession{
		UserID:    userID,
		SessionID: sessionID,
		SessionSeq: 1,
		Type:      "permanent",
		Status:    "active",
	}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatalf("seed user session: %v", err)
	}
}

func seedTestHostAccount(t *testing.T, db *gorm.DB, resourceID string) string {
	t.Helper()
	// Need a Host first
	host := model.Host{
		ID:      "h-test",
		Name:    "test-host",
		Address: "127.0.0.1",
		Port:    22,
		Status:  "active",
	}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("seed host: %v", err)
	}
	account := model.HostAccount{
		HostID:      host.ID,
		Username:    "root",
		AuthType:    "password",
		Status:      "active",
		ResourceSeq: 1,
		ResourceID:  resourceID,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("seed host account: %v", err)
	}
	return account.ID
}

func newTestStoreWithDB(t *testing.T, db *gorm.DB, user config.User) *StaticStore {
	t.Helper()
	targetsFile := filepath.Join(t.TempDir(), "targets.json")
	writeRuntimeTargets(t, targetsFile, nil)
	store, err := NewStaticStore(&config.Config{
		TargetsFile: targetsFile,
		Users:       []config.User{user},
	}, db)
	if err != nil {
		t.Fatalf("NewStaticStore returned error: %v", err)
	}
	return store
}
