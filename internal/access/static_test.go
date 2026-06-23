package access

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/config"
)

func TestAuthenticatePublicKeyWithRequestedTarget(t *testing.T) {
	publicKey := newTestPublicKey(t)
	store := newTestStore(t, config.User{
		ID:         "u-admin",
		Username:   "admin",
		PublicKeys: []string{string(ssh.MarshalAuthorizedKey(publicKey))},
	})

	user, err := store.AuthenticatePublicKey(context.Background(), "admin+target-a", publicKey)
	if err != nil {
		t.Fatalf("AuthenticatePublicKey returned error: %v", err)
	}
	if user.ID != "u-admin" || user.Username != "admin" || user.RequestedTargetID != "target-a" {
		t.Fatalf("unexpected user: %#v", user)
	}
}

func TestAuthenticatePublicKeyRejectsUnknownKey(t *testing.T) {
	allowedKey := newTestPublicKey(t)
	presentedKey := newTestPublicKey(t)
	store := newTestStore(t, config.User{
		ID:         "u-admin",
		Username:   "admin",
		PublicKeys: []string{string(ssh.MarshalAuthorizedKey(allowedKey))},
	})

	if _, err := store.AuthenticatePublicKey(context.Background(), "admin", presentedKey); err == nil {
		t.Fatal("AuthenticatePublicKey accepted an unknown key")
	}
}

func TestAuthenticateRejectsEmptyPasswordWhenPasswordUnset(t *testing.T) {
	store := newTestStore(t, config.User{
		ID:       "u-admin",
		Username: "admin",
	})

	if _, err := store.Authenticate(context.Background(), "admin", ""); err == nil {
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

func TestDeleteStaticTargetRejected(t *testing.T) {
	store := newTestStore(t, config.User{
		ID:       "u-admin",
		Username: "admin",
		Password: "admin",
	})

	if err := store.DeleteTarget("target-a"); !errors.Is(err, ErrStaticTargetDelete) {
		t.Fatalf("DeleteTarget error = %v, want ErrStaticTargetDelete", err)
	}
	view, err := store.Target("target-a")
	if err != nil {
		t.Fatalf("static target disappeared after rejected delete: %v", err)
	}
	if !view.Static {
		t.Fatalf("static target view was not marked static: %#v", view)
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
	return newTestStoreWithTargetsFile(t, "", user)
}

func newTestStoreWithTargetsFile(t *testing.T, targetsFile string, user config.User) *StaticStore {
	t.Helper()
	store, err := NewStaticStore(&config.Config{
		TargetsFile: targetsFile,
		Users:       []config.User{user},
		Targets: []config.Target{
			{
				ID:       "target-a",
				Name:     "target-a",
				Host:     "127.0.0.1",
				Port:     22,
				Username: "root",
				Password: "password",
			},
		},
		DefaultTarget: "target-a",
	})
	if err != nil {
		t.Fatalf("NewStaticStore returned error: %v", err)
	}
	return store
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
