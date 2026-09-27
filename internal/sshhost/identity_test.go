package sshhost

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestCollectorCapturesIdentityBeforeAuthentication(t *testing.T) {
	address, signer := startIdentityTestServer(t)
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split address: %v", err)
	}
	port, _ := strconv.Atoi(portText)

	identity, err := NewCollector(time.Second).Collect(context.Background(), host, port)
	if err != nil {
		t.Fatalf("collect identity: %v", err)
	}
	if identity.Fingerprint != ssh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatalf("fingerprint = %q, want %q", identity.Fingerprint, ssh.FingerprintSHA256(signer.PublicKey()))
	}
	if identity.KnownHosts == "" {
		t.Fatal("known_hosts record is empty")
	}
}

func TestVerificationCallbackRejectsChangeWithStructuredError(t *testing.T) {
	_, oldSigner := startIdentityTestServer(t)
	_, newSigner := startIdentityTestServer(t)
	changeCalls := 0
	callback, err := VerificationCallback("host-1", ssh.FingerprintSHA256(oldSigner.PublicKey()), "", func(change Change) (bool, error) {
		changeCalls++
		if change.HostID != "host-1" || change.OldFingerprint == change.NewFingerprint {
			t.Fatalf("unexpected change: %#v", change)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("build callback: %v", err)
	}

	err = callback("host", nil, newSigner.PublicKey())
	var changed *KeyChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("error = %T %v, want KeyChangedError", err, err)
	}
	if !changed.HostDisabled || changed.HostID != "host-1" || changeCalls != 1 {
		t.Fatalf("unexpected changed error: %#v, calls=%d", changed, changeCalls)
	}
}

func TestVerificationCallbackFailsClosedWithoutIdentity(t *testing.T) {
	_, err := VerificationCallback("host-1", "", "", nil)
	var unavailable *IdentityUnavailableError
	if !errors.As(err, &unavailable) || unavailable.HostID != "host-1" {
		t.Fatalf("error = %T %v, want host identity unavailable", err, err)
	}
}

func TestVerificationCallbackNormalizesFingerprintPrefixCase(t *testing.T) {
	_, signer := startIdentityTestServer(t)
	fingerprint := ssh.FingerprintSHA256(signer.PublicKey())
	callback, err := VerificationCallback("host-1", "sha256:"+strings.TrimPrefix(fingerprint, "SHA256:"), "", nil)
	if err != nil {
		t.Fatalf("build callback: %v", err)
	}
	if err := callback("host", nil, signer.PublicKey()); err != nil {
		t.Fatalf("verify lower-case fingerprint prefix: %v", err)
	}
}

func startIdentityTestServer(t *testing.T) (string, ssh.Signer) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _, _, _ = ssh.NewServerConn(conn, config)
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("test SSH server did not stop")
		}
	})
	return listener.Addr().String(), signer
}
