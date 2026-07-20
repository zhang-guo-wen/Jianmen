package store

import (
	"errors"
	"testing"

	"jianmen/internal/sshhost"
)

func TestClientConfigForTargetRequiresHostKeyVerification(t *testing.T) {
	_, err := ClientConfigForTarget(TargetConfig{
		Host:     "127.0.0.1",
		Port:     22,
		Username: "root",
		Password: "secret",
	})
	if err == nil {
		t.Fatal("expected host key verification error")
	}
	var unavailable *sshhost.IdentityUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientConfigForTargetAllowsStrictFingerprint(t *testing.T) {
	cfg, err := ClientConfigForTarget(TargetConfig{
		Host:               "127.0.0.1",
		Port:               22,
		Username:           "root",
		Password:           "secret",
		HostKeyFingerprint: "SHA256:test-fingerprint",
	})
	if err != nil {
		t.Fatalf("ClientConfigForTarget error: %v", err)
	}
	if cfg.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback was not configured")
	}
}

func TestClientConfigForTargetRejectsLegacyInsecureMode(t *testing.T) {
	_, err := ClientConfigForTarget(TargetConfig{
		Host:                  "127.0.0.1",
		Port:                  22,
		Username:              "root",
		Password:              "secret",
		InsecureIgnoreHostKey: true,
	})
	var unavailable *sshhost.IdentityUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %T %v, want fail-closed identity error", err, err)
	}
}
