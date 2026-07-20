package dbtls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
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
	if identity.TrustMode != ServerIdentityTrustModeCustom {
		t.Fatalf("self-signed trust mode = %q, want %q", identity.TrustMode, ServerIdentityTrustModeCustom)
	}

	_, unrelatedCAFile := writeCAIssuedIdentity(
		t,
		"unrelated.example.test",
		now.Add(-time.Hour),
		now.Add(time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	)
	selfSignedBundle := filepath.Join(t.TempDir(), "self-signed-with-extra.crt")
	bundlePEM := append(
		append([]byte(nil), readIdentityTestFile(t, selfSignedCertFile)...),
		readIdentityTestFile(t, unrelatedCAFile)...,
	)
	writeIdentityTestFile(t, selfSignedBundle, bundlePEM)
	if _, err := loadServerIdentity(
		selfSignedBundle,
		"",
		"gateway.example.test",
		func() (*x509.CertPool, error) {
			t.Fatal("self-signed identity with extra certificates loaded system roots")
			return nil, nil
		},
	); err == nil {
		t.Fatal("loadServerIdentity() accepted a self-signed leaf with extra certificates")
	}
}

func TestLoadServerIdentityUsesSystemTrustForCAIssuedIdentity(t *testing.T) {
	now := time.Now()
	certFile, caFile := writeCAIssuedIdentity(
		t,
		"gateway.example.test",
		now.Add(-time.Hour),
		now.Add(time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	)
	_, anchors, err := readCertificateFile(caFile)
	if err != nil {
		t.Fatalf("read test CA: %v", err)
	}
	systemRoots := x509.NewCertPool()
	systemRoots.AddCert(anchors[0])

	identity, err := loadServerIdentity(
		certFile,
		"",
		"gateway.example.test",
		func() (*x509.CertPool, error) {
			return systemRoots, nil
		},
	)
	if err != nil {
		t.Fatalf("load system-trusted identity: %v", err)
	}
	if identity.TrustMode != ServerIdentityTrustModeSystem {
		t.Fatalf("system-trusted mode = %q, want %q", identity.TrustMode, ServerIdentityTrustModeSystem)
	}
	if identity.CAPEM != string(readIdentityTestFile(t, caFile)) || identity.LeafSHA256 == "" {
		t.Fatalf("unexpected system-trusted identity material: %#v", identity)
	}
}

func TestLoadServerIdentitySystemTrustRejectsInvalidLeafIdentity(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		serverName string
		notBefore  time.Time
		notAfter   time.Time
		usages     []x509.ExtKeyUsage
	}{
		{
			name:       "wrong SAN",
			serverName: "other.example.test",
			notBefore:  now.Add(-time.Hour),
			notAfter:   now.Add(time.Hour),
			usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		},
		{
			name:       "expired",
			serverName: "gateway.example.test",
			notBefore:  now.Add(-2 * time.Hour),
			notAfter:   now.Add(-time.Hour),
			usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		},
		{
			name:       "client auth only",
			serverName: "gateway.example.test",
			notBefore:  now.Add(-time.Hour),
			notAfter:   now.Add(time.Hour),
			usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certFile, caFile := writeCAIssuedIdentity(
				t,
				"gateway.example.test",
				tt.notBefore,
				tt.notAfter,
				tt.usages,
			)
			_, anchors, err := readCertificateFile(caFile)
			if err != nil {
				t.Fatalf("read test CA: %v", err)
			}
			systemRoots := x509.NewCertPool()
			systemRoots.AddCert(anchors[0])
			if _, err := loadServerIdentity(
				certFile,
				"",
				tt.serverName,
				func() (*x509.CertPool, error) {
					return systemRoots, nil
				},
			); err == nil {
				t.Fatalf("loadServerIdentity() accepted system-trusted leaf with %s", tt.name)
			}
		})
	}
}

func TestLoadServerIdentitySystemTrustRequiresCompleteIntermediateChain(t *testing.T) {
	fullChainFile, leafOnlyFile, caFile := writeIntermediateCAIssuedIdentity(t, "gateway.example.test")
	_, anchors, err := readCertificateFile(caFile)
	if err != nil {
		t.Fatalf("read test root CA: %v", err)
	}
	systemRoots := x509.NewCertPool()
	systemRoots.AddCert(anchors[0])
	loadRoots := func() (*x509.CertPool, error) {
		return systemRoots, nil
	}

	identity, err := loadServerIdentity(
		fullChainFile,
		"",
		"gateway.example.test",
		loadRoots,
	)
	if err != nil {
		t.Fatalf("load full system-trusted chain: %v", err)
	}
	if identity.TrustMode != ServerIdentityTrustModeSystem ||
		identity.CAPEM != string(readIdentityTestFile(t, caFile)) {
		t.Fatalf("unexpected full-chain identity: %#v", identity)
	}

	_, configuredChain, err := readCertificateFile(fullChainFile)
	if err != nil {
		t.Fatalf("read configured full chain: %v", err)
	}
	_, rootChain, err := readCertificateFile(caFile)
	if err != nil {
		t.Fatalf("read configured root: %v", err)
	}
	verifiedChain := []*x509.Certificate{configuredChain[0], configuredChain[1], rootChain[0]}
	if got := verifiedChainMatchingConfiguredCertificates(
		[][]*x509.Certificate{verifiedChain},
		nil,
	); got != nil {
		t.Fatal("verifiedChainMatchingConfiguredCertificates() accepted a missing intermediate")
	}
	if got := verifiedChainMatchingConfiguredCertificates(
		[][]*x509.Certificate{verifiedChain},
		configuredChain[1:],
	); len(got) != len(verifiedChain) {
		t.Fatal("verifiedChainMatchingConfiguredCertificates() rejected the configured intermediate")
	}
	if got := verifiedChainMatchingConfiguredCertificates(
		[][]*x509.Certificate{verifiedChain},
		[]*x509.Certificate{rootChain[0], configuredChain[1]},
	); got != nil {
		t.Fatal("verifiedChainMatchingConfiguredCertificates() accepted certificates in the wrong order")
	}

	misorderedFile := filepath.Join(t.TempDir(), "gateway-misordered-chain.crt")
	misorderedPEM := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: configuredChain[0].Raw}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootChain[0].Raw})...,
	)
	misorderedPEM = append(
		misorderedPEM,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: configuredChain[1].Raw})...,
	)
	writeIdentityTestFile(t, misorderedFile, misorderedPEM)
	if _, err := loadServerIdentity(
		misorderedFile,
		"",
		"gateway.example.test",
		loadRoots,
	); err == nil {
		t.Fatal("loadServerIdentity() accepted a system-trusted certificate chain in the wrong order")
	}

	if _, err := loadServerIdentity(
		leafOnlyFile,
		"",
		"gateway.example.test",
		loadRoots,
	); err == nil {
		t.Fatal("loadServerIdentity() accepted a system-trusted leaf without its intermediate certificate")
	}
}

func TestLoadServerIdentityRejectsUnavailableSystemTrust(t *testing.T) {
	now := time.Now()
	certFile, _ := writeCAIssuedIdentity(
		t,
		"gateway.example.test",
		now.Add(-time.Hour),
		now.Add(time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	)
	if _, err := loadServerIdentity(
		certFile,
		"",
		"gateway.example.test",
		func() (*x509.CertPool, error) {
			return nil, nil
		},
	); err == nil {
		t.Fatal("loadServerIdentity() accepted an unavailable system certificate pool")
	}
	if _, err := loadServerIdentity(
		certFile,
		"",
		"gateway.example.test",
		func() (*x509.CertPool, error) {
			return nil, errors.New("system roots unavailable")
		},
	); err == nil {
		t.Fatal("loadServerIdentity() ignored a system certificate pool error")
	}
}

func TestLoadServerIdentityExplicitCADoesNotFallBackToSystemTrust(t *testing.T) {
	now := time.Now()
	certFile, correctCAFile := writeCAIssuedIdentity(
		t,
		"gateway.example.test",
		now.Add(-time.Hour),
		now.Add(time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	)
	_, wrongCAFile := writeCAIssuedIdentity(
		t,
		"unrelated.example.test",
		now.Add(-time.Hour),
		now.Add(time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	)
	_, correctAnchors, err := readCertificateFile(correctCAFile)
	if err != nil {
		t.Fatalf("read correct test CA: %v", err)
	}
	systemRoots := x509.NewCertPool()
	systemRoots.AddCert(correctAnchors[0])
	systemRootsCalled := false

	if _, err := loadServerIdentity(
		certFile,
		wrongCAFile,
		"gateway.example.test",
		func() (*x509.CertPool, error) {
			systemRootsCalled = true
			return systemRoots, nil
		},
	); err == nil {
		t.Fatal("loadServerIdentity() bypassed an explicit wrong CA with system trust")
	}
	if systemRootsCalled {
		t.Fatal("loadServerIdentity() loaded system roots despite an explicit ca_file")
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

func writeIntermediateCAIssuedIdentity(t *testing.T, serverName string) (fullChainFile, leafOnlyFile, caFile string) {
	t.Helper()
	now := time.Now()
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(10),
		Subject:               pkix.Name{CommonName: "identity test root CA"},
		NotBefore:             now.Add(-24 * time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	rootDER, err := x509.CreateCertificate(
		rand.Reader,
		rootTemplate,
		rootTemplate,
		&rootKey.PublicKey,
		rootKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	intermediateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	intermediateTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(11),
		Subject:               pkix.Name{CommonName: "identity test intermediate CA"},
		NotBefore:             now.Add(-12 * time.Hour),
		NotAfter:              now.Add(12 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	intermediateDER, err := x509.CreateCertificate(
		rand.Reader,
		intermediateTemplate,
		rootTemplate,
		&intermediateKey.PublicKey,
		rootKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(12),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(
		rand.Reader,
		leafTemplate,
		intermediateTemplate,
		&leafKey.PublicKey,
		intermediateKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	fullChainFile = filepath.Join(dir, "gateway-fullchain.crt")
	leafOnlyFile = filepath.Join(dir, "gateway-leaf.crt")
	caFile = filepath.Join(dir, "gateway-root-ca.crt")
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	intermediatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: intermediateDER})
	writeIdentityTestFile(t, fullChainFile, append(append([]byte(nil), leafPEM...), intermediatePEM...))
	writeIdentityTestFile(t, leafOnlyFile, leafPEM)
	writeIdentityTestFile(t, caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}))
	return fullChainFile, leafOnlyFile, caFile
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
