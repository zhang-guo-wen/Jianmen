package admin

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jianmen/internal/config"
)

func TestListenAndServeUsesTLSWhenCertificateConfigured(t *testing.T) {
	tlsDir := t.TempDir()
	certFile, keyFile := writeTestCertificate(t, tlsDir)
	port := reserveTCPPort(t)

	server := &Server{
		cfg: &config.Config{
			ListenAddr: "127.0.0.1:47102",
			Admin: config.AdminConfig{
				Enabled:    true,
				ListenAddr: "127.0.0.1:" + port,
				Dev:        true,
				TLS: config.AdminTLSConfig{
					CertFile: certFile,
					KeyFile:  keyFile,
				},
			},
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe(ctx) }()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: insecureTLSConfigForTest()}}
	var response *http.Response
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		response, _ = client.Get("https://127.0.0.1:" + port + "/")
		if response != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if response == nil {
		t.Fatalf("TLS admin server did not accept a request")
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ListenAndServe() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListenAndServe() did not stop after context cancellation")
	}
}

func writeTestCertificate(t *testing.T, dir string) (string, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create test certificate: %v", err)
	}
	certFile := filepath.Join(dir, "admin.crt")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write test certificate: %v", err)
	}
	keyFile := filepath.Join(dir, "admin.key")
	keyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}), 0o600); err != nil {
		t.Fatalf("write test key: %v", err)
	}
	return certFile, keyFile
}

func reserveTCPPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve TCP port: %v", err)
	}
	defer listener.Close()
	return fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
}

func insecureTLSConfigForTest() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only client
}
