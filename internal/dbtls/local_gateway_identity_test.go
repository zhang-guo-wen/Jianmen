package dbtls

import (
	"bytes"
	"crypto/tls"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"jianmen/internal/config"
)

func TestEnsureLocalUnifiedGatewayIdentityGeneratesServerAuthIdentity(t *testing.T) {
	dataDir := t.TempDir()
	gateway := newLocalUnifiedGatewayConfig()
	gateway.Unified.Address = "0.0.0.0:33060"

	generated, err := EnsureLocalUnifiedGatewayIdentity(&gateway, dataDir)
	if err != nil {
		t.Fatalf("EnsureLocalUnifiedGatewayIdentity() error = %v", err)
	}
	if !generated {
		t.Fatal("EnsureLocalUnifiedGatewayIdentity() did not report a generated identity")
	}
	expectedDir := filepath.Join(dataDir, localGatewayIdentityDirectory)
	if gateway.Unified.CertFile != filepath.Join(expectedDir, localGatewayCertificateFile) ||
		gateway.Unified.KeyFile != filepath.Join(expectedDir, localGatewayPrivateKeyFile) {
		t.Fatalf("managed identity paths = %#v", gateway.Unified)
	}
	if gateway.Unified.ServerName != localGatewayDefaultServerName {
		t.Fatalf("server name = %q, want %q", gateway.Unified.ServerName, localGatewayDefaultServerName)
	}
	if gateway.Unified.CAFile != "" {
		t.Fatalf("managed self-signed identity unexpectedly set ca_file = %q", gateway.Unified.CAFile)
	}
	if _, err := tls.LoadX509KeyPair(gateway.Unified.CertFile, gateway.Unified.KeyFile); err != nil {
		t.Fatalf("load generated key pair: %v", err)
	}
	if _, err := LoadServerIdentity(
		gateway.Unified.CertFile,
		gateway.Unified.CAFile,
		gateway.Unified.ServerName,
	); err != nil {
		t.Fatalf("load generated server identity: %v", err)
	}
	_, certificates, err := readCertificateFile(gateway.Unified.CertFile)
	if err != nil {
		t.Fatalf("read generated certificate: %v", err)
	}
	leaf := certificates[0]
	if leaf.IsCA {
		t.Fatal("generated server identity is a CA certificate")
	}
	for _, requiredName := range []string{"localhost", "127.0.0.1", "::1"} {
		if err := leaf.VerifyHostname(requiredName); err != nil {
			t.Fatalf("generated certificate does not cover %q: %v", requiredName, err)
		}
	}
}

func TestEnsureLocalUnifiedGatewayIdentityPreservesCustomServerName(t *testing.T) {
	gateway := newLocalUnifiedGatewayConfig()
	gateway.Unified.ServerName = "gateway.local"

	if _, err := EnsureLocalUnifiedGatewayIdentity(&gateway, t.TempDir()); err != nil {
		t.Fatalf("EnsureLocalUnifiedGatewayIdentity() error = %v", err)
	}
	_, certificates, err := readCertificateFile(gateway.Unified.CertFile)
	if err != nil {
		t.Fatalf("read generated certificate: %v", err)
	}
	if err := certificates[0].VerifyHostname("gateway.local"); err != nil {
		t.Fatalf("generated certificate does not cover custom server name: %v", err)
	}
	if gateway.Unified.ServerName != "gateway.local" {
		t.Fatalf("server name = %q, want custom value", gateway.Unified.ServerName)
	}
}

func TestEnsureLocalUnifiedGatewayIdentityReusesValidIdentity(t *testing.T) {
	dataDir := t.TempDir()
	first := newLocalUnifiedGatewayConfig()
	generated, err := EnsureLocalUnifiedGatewayIdentity(&first, dataDir)
	if err != nil || !generated {
		t.Fatalf("first EnsureLocalUnifiedGatewayIdentity() = generated %t, err %v", generated, err)
	}
	firstCert := readLocalGatewayTestFile(t, first.Unified.CertFile)
	firstKey := readLocalGatewayTestFile(t, first.Unified.KeyFile)

	second := newLocalUnifiedGatewayConfig()
	generated, err = EnsureLocalUnifiedGatewayIdentity(&second, dataDir)
	if err != nil {
		t.Fatalf("second EnsureLocalUnifiedGatewayIdentity() error = %v", err)
	}
	if generated {
		t.Fatal("second EnsureLocalUnifiedGatewayIdentity() replaced a valid identity")
	}
	if !bytes.Equal(firstCert, readLocalGatewayTestFile(t, second.Unified.CertFile)) ||
		!bytes.Equal(firstKey, readLocalGatewayTestFile(t, second.Unified.KeyFile)) {
		t.Fatal("persisted database gateway identity changed across restart")
	}
}

func TestEnsureLocalGatewayIdentitiesCoversIndependentListeners(t *testing.T) {
	dataDir := t.TempDir()
	gateway := config.DatabaseGatewayConfig{
		Enabled: true,
		Mode:    config.DatabaseGatewayModeIndependent,
		MySQL: config.DatabaseProtocolListener{
			Enabled: true, Address: "0.0.0.0:33061",
		},
		PostgreSQL: config.DatabaseProtocolListener{
			Enabled: true, Address: "[::]:33062",
		},
		Redis: config.DatabaseProtocolListener{
			Enabled: true, Address: "0.0.0.0:33063",
		},
	}

	generated, err := EnsureLocalGatewayIdentities(&gateway, dataDir)
	if err != nil {
		t.Fatalf("EnsureLocalGatewayIdentities() error = %v", err)
	}
	if !generated {
		t.Fatal("EnsureLocalGatewayIdentities() did not generate identities")
	}
	listeners := []struct {
		name     string
		listener config.DatabaseProtocolListener
	}{
		{name: "mysql", listener: gateway.MySQL},
		{name: "postgresql", listener: gateway.PostgreSQL},
		{name: "redis", listener: gateway.Redis},
	}
	seenCertificates := make(map[string]struct{}, len(listeners))
	for _, item := range listeners {
		if item.listener.CertFile == "" ||
			item.listener.KeyFile == "" ||
			item.listener.ServerName != localGatewayDefaultServerName {
			t.Fatalf("%s managed identity = %#v", item.name, item.listener)
		}
		if _, exists := seenCertificates[item.listener.CertFile]; exists {
			t.Fatalf("%s unexpectedly shares a managed certificate", item.name)
		}
		seenCertificates[item.listener.CertFile] = struct{}{}
		if _, err := LoadServerIdentity(
			item.listener.CertFile,
			item.listener.CAFile,
			item.listener.ServerName,
		); err != nil {
			t.Fatalf("load %s managed identity: %v", item.name, err)
		}
	}

	restarted := gateway
	restarted.MySQL.CertFile, restarted.MySQL.KeyFile = "", ""
	restarted.PostgreSQL.CertFile, restarted.PostgreSQL.KeyFile = "", ""
	restarted.Redis.CertFile, restarted.Redis.KeyFile = "", ""
	generated, err = EnsureLocalGatewayIdentities(&restarted, dataDir)
	if err != nil {
		t.Fatalf("reuse independent identities: %v", err)
	}
	if generated {
		t.Fatal("valid independent identities were replaced")
	}
	if restarted.MySQL.CertFile != gateway.MySQL.CertFile ||
		restarted.PostgreSQL.CertFile != gateway.PostgreSQL.CertFile ||
		restarted.Redis.CertFile != gateway.Redis.CertFile {
		t.Fatalf("independent managed identity paths changed: %#v", restarted)
	}
}

func TestEnsureLocalUnifiedGatewayIdentityOnlyHandlesEligibleListener(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.DatabaseGatewayConfig)
	}{
		{name: "gateway disabled", mutate: func(g *config.DatabaseGatewayConfig) { g.Enabled = false }},
		{name: "independent mode", mutate: func(g *config.DatabaseGatewayConfig) {
			g.Mode = config.DatabaseGatewayModeIndependent
		}},
		{name: "unified listener disabled", mutate: func(g *config.DatabaseGatewayConfig) {
			g.Unified.Enabled = false
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			gateway := newLocalUnifiedGatewayConfig()
			tt.mutate(&gateway)
			before := gateway
			generated, err := EnsureLocalUnifiedGatewayIdentity(&gateway, dataDir)
			if err != nil {
				t.Fatalf("EnsureLocalUnifiedGatewayIdentity() error = %v", err)
			}
			if generated {
				t.Fatal("ineligible listener generated a managed identity")
			}
			if !reflect.DeepEqual(gateway, before) {
				t.Fatalf("ineligible listener changed from %#v to %#v", before, gateway)
			}
			if _, err := os.Stat(filepath.Join(dataDir, localGatewayIdentityDirectory)); !os.IsNotExist(err) {
				t.Fatalf("managed identity directory exists for ineligible listener: %v", err)
			}
		})
	}
}

func TestEnsureLocalGatewayIdentitiesPreservesValidExplicitIdentity(t *testing.T) {
	dataDir := t.TempDir()
	managed := newLocalUnifiedGatewayConfig()
	if _, err := EnsureLocalUnifiedGatewayIdentity(&managed, dataDir); err != nil {
		t.Fatalf("generate source identity: %v", err)
	}
	explicit := newLocalUnifiedGatewayConfig()
	explicit.Unified.CertFile = managed.Unified.CertFile
	explicit.Unified.KeyFile = managed.Unified.KeyFile
	explicit.Unified.ServerName = managed.Unified.ServerName
	before := explicit

	generated, err := EnsureLocalGatewayIdentities(&explicit, t.TempDir())
	if err != nil {
		t.Fatalf("validate explicit identity: %v", err)
	}
	if generated {
		t.Fatal("valid explicit identity was replaced")
	}
	if !reflect.DeepEqual(explicit, before) {
		t.Fatalf("explicit identity changed from %#v to %#v", before, explicit)
	}
}

func newLocalUnifiedGatewayConfig() config.DatabaseGatewayConfig {
	return config.DatabaseGatewayConfig{
		Enabled: true,
		Mode:    config.DatabaseGatewayModeUnified,
		Unified: config.DatabaseUnifiedListener{
			Enabled: true,
			Address: "127.0.0.1:33060",
		},
	}
}

func readLocalGatewayTestFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}
