package store

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"jianmen/internal/model"
	"jianmen/internal/sshhost"
)

func TestHostKeyChangeAtomicallyDisablesMatchingHostSnapshot(t *testing.T) {
	repository, db := newHostTargetMutationTestStore(t)
	oldSigner := newStoreTestSigner(t)
	newSigner := newStoreTestSigner(t)
	host := model.Host{
		ID: "host-key-change", Name: "host-key-change", Address: "127.0.0.1", Port: 22,
		Protocol: "ssh", Status: "active",
		HostKeyFingerprint: ssh.FingerprintSHA256(oldSigner.PublicKey()),
		KnownHosts:         knownhosts.Line([]string{"127.0.0.1"}, oldSigner.PublicKey()),
	}
	account := model.HostAccount{ID: "account-key-change", HostID: host.ID, Username: "root", Status: "active"}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	target, err := repository.TargetConfig(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("load target config: %v", err)
	}
	callback, err := hostKeyCallback(target)
	if err != nil {
		t.Fatalf("build host key callback: %v", err)
	}
	err = callback(host.Address, nil, newSigner.PublicKey())
	var changed *sshhost.KeyChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("callback error = %T %v, want KeyChangedError", err, err)
	}
	if !changed.HostDisabled || changed.HostID != host.ID {
		t.Fatalf("unexpected changed error: %#v", changed)
	}
	var persisted model.Host
	if err := db.First(&persisted, "id = ?", host.ID).Error; err != nil {
		t.Fatalf("load host: %v", err)
	}
	if persisted.Status != "disabled" {
		t.Fatalf("host status = %q, want disabled", persisted.Status)
	}
}

func TestStaleHostKeyChangeCannotDisableConcurrentlyRefreshedIdentity(t *testing.T) {
	repository, db := newHostTargetMutationTestStore(t)
	oldSigner := newStoreTestSigner(t)
	newSigner := newStoreTestSigner(t)
	host := model.Host{
		ID: "host-key-refresh-race", Name: "host-key-refresh-race", Address: "127.0.0.1", Port: 22,
		Protocol: "ssh", Status: "active",
		HostKeyFingerprint: ssh.FingerprintSHA256(oldSigner.PublicKey()),
		KnownHosts:         knownhosts.Line([]string{"127.0.0.1"}, oldSigner.PublicKey()),
	}
	account := model.HostAccount{ID: "account-key-refresh-race", HostID: host.ID, Username: "root", Status: "active"}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	target, err := repository.TargetConfig(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("load target config: %v", err)
	}
	if err := db.Model(&model.Host{}).Where("id = ?", host.ID).Updates(map[string]any{
		"host_key_fingerprint": ssh.FingerprintSHA256(newSigner.PublicKey()),
		"known_hosts":          knownhosts.Line([]string{"127.0.0.1"}, newSigner.PublicKey()),
		"status":               "active",
	}).Error; err != nil {
		t.Fatalf("refresh host identity: %v", err)
	}
	callback, err := hostKeyCallback(target)
	if err != nil {
		t.Fatalf("build host key callback: %v", err)
	}
	err = callback(host.Address, nil, newSigner.PublicKey())
	var changed *sshhost.KeyChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("callback error = %T %v, want stale KeyChangedError", err, err)
	}
	if changed.HostDisabled {
		t.Fatalf("stale callback reported refreshed host disabled: %#v", changed)
	}
	var persisted model.Host
	if err := db.First(&persisted, "id = ?", host.ID).Error; err != nil {
		t.Fatalf("load host: %v", err)
	}
	if persisted.Status != "active" || persisted.HostKeyFingerprint != ssh.FingerprintSHA256(newSigner.PublicKey()) {
		t.Fatalf("refreshed host was changed by stale callback: %#v", persisted)
	}
}

func TestStaleHostKeyChangeCannotDisableChangedEndpointWithSameIdentity(t *testing.T) {
	repository, db := newHostTargetMutationTestStore(t)
	storedSigner := newStoreTestSigner(t)
	unexpectedSigner := newStoreTestSigner(t)
	host := model.Host{
		ID: "host-endpoint-refresh-race", Name: "host-endpoint-refresh-race", Address: "192.0.2.10", Port: 22,
		Protocol: "ssh", Status: "active",
		HostKeyFingerprint: ssh.FingerprintSHA256(storedSigner.PublicKey()),
		KnownHosts:         knownhosts.Line([]string{"192.0.2.10"}, storedSigner.PublicKey()),
	}
	account := model.HostAccount{ID: "account-endpoint-refresh-race", HostID: host.ID, Username: "root", Status: "active"}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	target, err := repository.TargetConfig(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("load target config: %v", err)
	}

	const refreshedAddress = "192.0.2.20"
	if err := db.Model(&model.Host{}).Where("id = ?", host.ID).Updates(map[string]any{
		"address":     refreshedAddress,
		"known_hosts": knownhosts.Line([]string{refreshedAddress}, storedSigner.PublicKey()),
		"status":      "active",
	}).Error; err != nil {
		t.Fatalf("refresh host endpoint: %v", err)
	}
	callback, err := hostKeyCallback(target)
	if err != nil {
		t.Fatalf("build host key callback: %v", err)
	}
	err = callback(host.Address, nil, unexpectedSigner.PublicKey())
	var changed *sshhost.KeyChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("callback error = %T %v, want stale KeyChangedError", err, err)
	}
	if changed.HostDisabled {
		t.Fatalf("stale callback reported refreshed endpoint disabled: %#v", changed)
	}
	var persisted model.Host
	if err := db.First(&persisted, "id = ?", host.ID).Error; err != nil {
		t.Fatalf("load host: %v", err)
	}
	if persisted.Status != "active" || persisted.Address != refreshedAddress ||
		persisted.HostKeyFingerprint != ssh.FingerprintSHA256(storedSigner.PublicKey()) {
		t.Fatalf("refreshed host was changed by stale callback: %#v", persisted)
	}
}

func newStoreTestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create SSH signer: %v", err)
	}
	return signer
}
