package dbtls

import (
	"bytes"
	"os"
	"testing"
)

func TestEnsureLocalUnifiedGatewayIdentityRejectsCorruptPersistedIdentity(t *testing.T) {
	dataDir := t.TempDir()
	first := newLocalUnifiedGatewayConfig()
	if _, err := EnsureLocalUnifiedGatewayIdentity(&first, dataDir); err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	privateKey := readLocalGatewayTestFile(t, first.Unified.KeyFile)
	corruptCertificate := []byte("not a certificate")
	if err := os.WriteFile(first.Unified.CertFile, corruptCertificate, 0o600); err != nil {
		t.Fatal(err)
	}

	restarted := newLocalUnifiedGatewayConfig()
	if _, err := EnsureLocalUnifiedGatewayIdentity(&restarted, dataDir); err == nil {
		t.Fatal("EnsureLocalUnifiedGatewayIdentity() accepted a corrupt persisted certificate")
	}
	if !bytes.Equal(corruptCertificate, readLocalGatewayTestFile(t, first.Unified.CertFile)) {
		t.Fatal("corrupt managed certificate was silently overwritten")
	}
	if !bytes.Equal(privateKey, readLocalGatewayTestFile(t, first.Unified.KeyFile)) {
		t.Fatal("managed private key changed after validation failure")
	}
	if restarted.Unified.CertFile != "" || restarted.Unified.KeyFile != "" {
		t.Fatalf("failed identity was injected into config: %#v", restarted.Unified)
	}
}

func TestEnsureLocalUnifiedGatewayIdentityRejectsPartialPersistedIdentity(t *testing.T) {
	dataDir := t.TempDir()
	first := newLocalUnifiedGatewayConfig()
	if _, err := EnsureLocalUnifiedGatewayIdentity(&first, dataDir); err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	certificate := readLocalGatewayTestFile(t, first.Unified.CertFile)
	if err := os.Remove(first.Unified.KeyFile); err != nil {
		t.Fatal(err)
	}

	restarted := newLocalUnifiedGatewayConfig()
	if _, err := EnsureLocalUnifiedGatewayIdentity(&restarted, dataDir); err == nil {
		t.Fatal("EnsureLocalUnifiedGatewayIdentity() accepted a partial persisted identity")
	}
	if !bytes.Equal(certificate, readLocalGatewayTestFile(t, first.Unified.CertFile)) {
		t.Fatal("partial managed identity was silently replaced")
	}
	if _, err := os.Stat(first.Unified.KeyFile); !os.IsNotExist(err) {
		t.Fatalf("missing managed key was silently recreated: %v", err)
	}
}

func TestEnsureLocalUnifiedGatewayIdentityRejectsExplicitCAWithoutKeyPair(t *testing.T) {
	dataDir := t.TempDir()
	gateway := newLocalUnifiedGatewayConfig()
	gateway.Unified.CAFile = "explicit-ca.crt"

	if _, err := EnsureLocalUnifiedGatewayIdentity(&gateway, dataDir); err == nil {
		t.Fatal("EnsureLocalUnifiedGatewayIdentity() accepted ca_file without an explicit key pair")
	}
	if gateway.Unified.CertFile != "" || gateway.Unified.KeyFile != "" {
		t.Fatalf("managed identity changed explicit TLS configuration: %#v", gateway.Unified)
	}
}
