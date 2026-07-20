package store

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/sshhost"
	"jianmen/internal/util"
)

// ClientConfigForTarget builds an ssh.ClientConfig from a TargetConfig.
func ClientConfigForTarget(target TargetConfig) (*ssh.ClientConfig, error) {
	authMethods := make([]ssh.AuthMethod, 0, 2)
	if target.Password != "" {
		authMethods = append(authMethods, ssh.Password(target.Password))
		authMethods = append(authMethods, ssh.KeyboardInteractive(func(_ string, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = target.Password
			}
			return answers, nil
		}))
	}
	if target.PrivateKeyPEM != "" || target.PrivateKeyPath != "" {
		signer, err := loadSigner(target.PrivateKeyPath, target.PrivateKeyPEM, target.Passphrase)
		if err != nil {
			return nil, err
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if len(authMethods) == 0 {
		return nil, errors.New("target has no usable auth method")
	}

	hostKeyCallback, err := hostKeyCallback(target)
	if err != nil {
		return nil, err
	}

	return &ssh.ClientConfig{
		User:            target.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
	}, nil
}

func hostKeyCallback(target TargetConfig) (ssh.HostKeyCallback, error) {
	return sshhost.VerificationCallback(
		target.HostID,
		target.HostKeyFingerprint,
		target.KnownHosts,
		target.HostKeyChangeHandler,
	)
}

func loadSigner(keyPath, keyPEM, passphrase string) (ssh.Signer, error) {
	var raw []byte
	if keyPEM != "" {
		raw = []byte(keyPEM)
	} else if keyPath != "" {
		var err error
		raw, err = os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read private key %q: %w", keyPath, err)
		}
	} else {
		return nil, errors.New("no private key provided")
	}

	if passphrase != "" {
		signer, err := ssh.ParsePrivateKeyWithPassphrase(raw, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("parse private key with passphrase: %w", err)
		}
		return signer, nil
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return signer, nil
}

// -------- helpers shared from access package ----------

func formatHostAddress(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	if port == 0 {
		port = 22
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func parseLoginName(username string) (LoginName, error) {
	if len(username) != 10 {
		return LoginName{}, fmt.Errorf("connection username must be 10 characters, got %d", len(username))
	}
	prefix, _, _, err := util.ParseCompactUsername(username)
	if err != nil {
		return LoginName{}, err
	}
	if prefix != util.PrefixHost && prefix != util.PrefixDatabase {
		return LoginName{}, fmt.Errorf("unknown resource prefix: %s", prefix)
	}
	return LoginName{
		ResourceID: username[1:5],
		SessionID:  username[5:10],
	}, nil
}

func publicKeysEqual(a, b ssh.PublicKey) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Type() == b.Type() && bytes.Equal(a.Marshal(), b.Marshal())
}

type signerLoader interface {
	load(keyPath, keyPEM, passphrase string) (ssh.Signer, error)
}

// parseAuthorizedKeys parses an ssh authorized_keys file.
func parseAuthorizedKeys(raw []byte) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		keys = append(keys, key)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

// tokenHash returns the SHA-256 hex digest of a token string.
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
