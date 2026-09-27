package sshserver

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestLoadOrCreateHostSignersCreatesRSAFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "host_key")

	signers, err := loadOrCreateHostSigners(path)
	if err != nil {
		t.Fatalf("loadOrCreateHostSigners: %v", err)
	}
	if len(signers) != 2 {
		t.Fatalf("expected 2 signers, got %d", len(signers))
	}
	if got := signers[0].PublicKey().Type(); got != ssh.KeyAlgoED25519 {
		t.Fatalf("expected primary host key %q, got %q", ssh.KeyAlgoED25519, got)
	}
	if got := signers[1].PublicKey().Type(); got != ssh.KeyAlgoRSA {
		t.Fatalf("expected fallback host key %q, got %q", ssh.KeyAlgoRSA, got)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected primary host key file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "host_key_rsa")); err != nil {
		t.Fatalf("expected rsa host key file: %v", err)
	}

	signersAgain, err := loadOrCreateHostSigners(path)
	if err != nil {
		t.Fatalf("reload host signers: %v", err)
	}
	for i := range signers {
		if !bytes.Equal(signers[i].PublicKey().Marshal(), signersAgain[i].PublicKey().Marshal()) {
			t.Fatalf("signer %d was not reused", i)
		}
	}
}

func TestLoadOrCreateHostSignersKeepsPrimaryRSA(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "host.key")
	key, err := generateRSAHostKey()
	if err != nil {
		t.Fatalf("generate rsa host key: %v", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatalf("write rsa host key: %v", err)
	}

	signers, err := loadOrCreateHostSigners(path)
	if err != nil {
		t.Fatalf("loadOrCreateHostSigners: %v", err)
	}
	if len(signers) != 1 {
		t.Fatalf("expected one signer for existing rsa host key path, got %d", len(signers))
	}
	if got := signers[0].PublicKey().Type(); got != ssh.KeyAlgoRSA {
		t.Fatalf("expected rsa host key, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "host_rsa.key")); !os.IsNotExist(err) {
		t.Fatalf("expected derived rsa key not to be created, stat err: %v", err)
	}
}

func TestLoadOrCreateHostSignersRejectsNonRSAFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "host_key")
	key, err := generateEd25519HostKey()
	if err != nil {
		t.Fatalf("generate ed25519 host key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "host_key_rsa"), key, 0o600); err != nil {
		t.Fatalf("write non-rsa fallback host key: %v", err)
	}

	_, err = loadOrCreateHostSigners(path)
	if err == nil {
		t.Fatal("expected non-rsa fallback host key to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("expected unsupported type error, got %v", err)
	}
}

func TestDeriveRSAHostKeyPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"data/host_key", "data/host_key_rsa"},
		{"data/host.key", "data/host_rsa.key"},
		{"tp_ssh_server.key", "tp_ssh_server_rsa.key"},
		{"host", "host_rsa"},
		{"data/archive/key.v1", "data/archive/key_rsa.v1"},
	}

	for _, test := range tests {
		path := filepath.FromSlash(test.path)
		want := filepath.FromSlash(test.want)
		if got := deriveRSAHostKeyPath(path); got != want {
			t.Fatalf("deriveRSAHostKeyPath(%q) = %q, want %q", path, got, want)
		}
	}
}
