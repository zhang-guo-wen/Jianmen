package store

import (
	"strings"
	"testing"
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
	if !strings.Contains(err.Error(), "host key verification is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientConfigForTargetAllowsExplicitHostKeyModes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target TargetConfig
	}{
		{
			name: "explicit insecure mode",
			target: TargetConfig{
				Host:                  "127.0.0.1",
				Port:                  22,
				Username:              "root",
				Password:              "secret",
				InsecureIgnoreHostKey: true,
			},
		},
		{
			name: "fingerprint mode",
			target: TargetConfig{
				Host:               "127.0.0.1",
				Port:               22,
				Username:           "root",
				Password:           "secret",
				HostKeyFingerprint: "SHA256:test-fingerprint",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ClientConfigForTarget(tc.target)
			if err != nil {
				t.Fatalf("ClientConfigForTarget error: %v", err)
			}
			if cfg.HostKeyCallback == nil {
				t.Fatal("HostKeyCallback was not configured")
			}
		})
	}
}
