package sshserver

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const rsaHostKeyBits = 3072

func loadOrCreateHostSigners(path string) ([]ssh.Signer, error) {
	primary, err := loadOrCreateHostSigner(path, generateEd25519HostKey)
	if err != nil {
		return nil, fmt.Errorf("load host key %q: %w", path, err)
	}

	signers := []ssh.Signer{primary}
	if primary.PublicKey().Type() == ssh.KeyAlgoRSA {
		return signers, nil
	}

	rsaPath := deriveRSAHostKeyPath(path)
	rsaSigner, err := loadOrCreateHostSigner(rsaPath, generateRSAHostKey)
	if err != nil {
		return nil, fmt.Errorf("load rsa host key %q: %w", rsaPath, err)
	}
	if rsaSigner.PublicKey().Type() != ssh.KeyAlgoRSA {
		return nil, fmt.Errorf("rsa host key %q has unsupported type %q", rsaPath, rsaSigner.PublicKey().Type())
	}
	return append(signers, rsaSigner), nil
}

func loadOrCreateHostSigner(path string, generate func() ([]byte, error)) (ssh.Signer, error) {
	if key, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(key)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	pemBytes, err := generate()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(pemBytes)
}

func generateEd25519HostKey() ([]byte, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}), nil
}

func generateRSAHostKey() ([]byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, rsaHostKeyBits)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}), nil
}

func deriveRSAHostKeyPath(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + "_rsa"
	}
	return strings.TrimSuffix(path, ext) + "_rsa" + ext
}
