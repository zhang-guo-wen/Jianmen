package dbtls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadServerIdentityRejectsUnverifiableIdentity(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		serverName string
		build      func(t *testing.T) (string, string)
	}{
		{
			name:       "wrong SAN",
			serverName: "other.example.test",
			build: func(t *testing.T) (string, string) {
				return writeCAIssuedIdentity(t, "gateway.example.test", now.Add(-time.Hour), now.Add(time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
			},
		},
		{
			name:       "wrong CA",
			serverName: "gateway.example.test",
			build: func(t *testing.T) (string, string) {
				certFile, _ := writeCAIssuedIdentity(t, "gateway.example.test", now.Add(-time.Hour), now.Add(time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
				_, unrelatedCAFile := writeCAIssuedIdentity(t, "unrelated.example.test", now.Add(-time.Hour), now.Add(time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
				return certFile, unrelatedCAFile
			},
		},
		{
			name:       "expired leaf",
			serverName: "gateway.example.test",
			build: func(t *testing.T) (string, string) {
				return writeCAIssuedIdentity(t, "gateway.example.test", now.Add(-2*time.Hour), now.Add(-time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
			},
		},
		{
			name:       "non CA trust anchor",
			serverName: "gateway.example.test",
			build: func(t *testing.T) (string, string) {
				certFile, _ := writeCAIssuedIdentity(t, "gateway.example.test", now.Add(-time.Hour), now.Add(time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
				return certFile, certFile
			},
		},
		{
			name:       "leaf without server auth",
			serverName: "gateway.example.test",
			build: func(t *testing.T) (string, string) {
				return writeCAIssuedIdentity(t, "gateway.example.test", now.Add(-time.Hour), now.Add(time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certFile, caFile := tt.build(t)
			if _, err := LoadServerIdentity(certFile, caFile, tt.serverName); err == nil {
				t.Fatalf("LoadServerIdentity() accepted %s", tt.name)
			}
		})
	}
}

func TestLoadServerIdentityFallbackRequiresSelfSignedLeaf(t *testing.T) {
	now := time.Now()
	caSignedCertFile, _ := writeCAIssuedIdentity(
		t,
		"gateway.example.test",
		now.Add(-time.Hour),
		now.Add(time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	)
	if _, err := LoadServerIdentity(caSignedCertFile, "", "gateway.example.test"); err == nil {
		t.Fatal("LoadServerIdentity() treated an arbitrary CA-signed leaf as a trust anchor")
	}

	selfSignedCertFile := writeSelfSignedIdentity(t, "gateway.example.test", now.Add(-time.Hour), now.Add(time.Hour))
	identity, err := LoadServerIdentity(selfSignedCertFile, "", "gateway.example.test")
	if err != nil {
		t.Fatalf("LoadServerIdentity() rejected self-signed pin: %v", err)
	}
	if identity.CAPEM != string(readIdentityTestFile(t, selfSignedCertFile)) || identity.LeafSHA256 == "" {
		t.Fatalf("unexpected self-signed identity material: %#v", identity)
	}
}

func TestLoadServerIdentityRejectsCAFileMixedWithNonCA(t *testing.T) {
	now := time.Now()
	certFile, caFile := writeCAIssuedIdentity(
		t,
		"gateway.example.test",
		now.Add(-time.Hour),
		now.Add(time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	)
	mixedCAFile := filepath.Join(t.TempDir(), "mixed-ca.pem")
	mixedPEM := append(
		append([]byte(nil), readIdentityTestFile(t, caFile)...),
		readIdentityTestFile(t, certFile)...,
	)
	writeIdentityTestFile(t, mixedCAFile, mixedPEM)

	if _, err := LoadServerIdentity(certFile, mixedCAFile, "gateway.example.test"); err == nil {
		t.Fatal("LoadServerIdentity() accepted a CA file containing a non-CA certificate")
	}
}

func writeCAIssuedIdentity(t *testing.T, serverName string, notBefore, notAfter time.Time, usages []x509.ExtKeyUsage) (string, string) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "identity test CA"},
		NotBefore:             now.Add(-24 * time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usages,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile := filepath.Join(dir, "gateway.crt")
	caFile := filepath.Join(dir, "gateway-ca.crt")
	writeIdentityTestFile(t, certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}))
	writeIdentityTestFile(t, caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	return certFile, caFile
}

func writeSelfSignedIdentity(t *testing.T, serverName string, notBefore, notAfter time.Time) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "self-signed.crt")
	writeIdentityTestFile(t, path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return path
}

func writeIdentityTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readIdentityTestFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}
