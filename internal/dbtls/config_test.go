package dbtls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

func TestIsVerifiedRejectsUnmarkedTLSConnection(t *testing.T) {
	server, client := tlsServerPair(t)
	defer server.Close()
	defer client.Close()
	if IsVerified(client) {
		t.Fatal("an arbitrary TLS connection was treated as verified")
	}
}

func TestClientConfigVerifyFullUsesCustomCAAndServerName(t *testing.T) {
	ca := testCertificatePEM(t)
	config, err := ClientConfig(Config{Mode: ModeVerifyFull, ServerName: "db.internal", CAPEM: ca}, "10.0.0.10:3306")
	if err != nil {
		t.Fatalf("ClientConfig() error = %v", err)
	}
	if config.InsecureSkipVerify || config.ServerName != "db.internal" || config.RootCAs == nil {
		t.Fatalf("unsafe verify-full config: %#v", config)
	}
}

func TestClientConfigVerifyCAUsesExplicitChainVerifier(t *testing.T) {
	config, err := ClientConfig(Config{Mode: ModeVerifyCA}, "db.internal:6379")
	if err != nil {
		t.Fatalf("ClientConfig() error = %v", err)
	}
	if !config.InsecureSkipVerify || config.VerifyConnection == nil {
		t.Fatalf("verify-ca must use an explicit certificate-chain verifier: %#v", config)
	}
}

func TestNormalizeRejectsUnknownMode(t *testing.T) {
	if _, err := NormalizeMode("trust-everything"); err == nil {
		t.Fatal("NormalizeMode accepted unknown TLS mode")
	}
}

func TestNormalizeDefaultsToDisabled(t *testing.T) {
	mode, err := NormalizeMode("  ")
	if err != nil {
		t.Fatalf("NormalizeMode() error = %v", err)
	}
	if mode != ModeDisable {
		t.Fatalf("NormalizeMode() = %q, want %q", mode, ModeDisable)
	}
}

func testCertificatePEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test ca"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	certificate, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}))
}

func tlsServerPair(t *testing.T) (*tls.Conn, *tls.Conn) {
	t.Helper()
	certificatePEM := testCertificatePEM(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(certificatePEM))
	if block == nil {
		t.Fatal("decode certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	_ = certificate
	serverRaw, clientRaw := net.Pipe()
	server := tls.Server(serverRaw, &tls.Config{Certificates: []tls.Certificate{mustLeafCertificate(t, key)}})
	client := tls.Client(clientRaw, &tls.Config{InsecureSkipVerify: true})
	errCh := make(chan error, 1)
	go func() { errCh <- server.Handshake() }()
	if err := client.Handshake(); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	return server, client
}

func mustLeafCertificate(t *testing.T, key *rsa.PrivateKey) tls.Certificate {
	t.Helper()
	template := x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "server"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	encoded, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: encoded}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}))
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}
