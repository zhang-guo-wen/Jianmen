package sshhost

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Identity is the public identity presented by an SSH server.
type Identity struct {
	Fingerprint string
	KnownHosts  string
}

// Change describes a host-key mismatch without carrying the public key bytes.
type Change struct {
	HostID         string
	OldFingerprint string
	NewFingerprint string
}

type ChangeHandler func(Change) (hostDisabled bool, err error)

// KeyChangedError is returned when the server no longer presents the stored
// SSH identity. DisableErr is intentionally excluded from API serialization.
type KeyChangedError struct {
	HostID         string
	OldFingerprint string
	NewFingerprint string
	HostDisabled   bool
	DisableErr     error
}

func (e *KeyChangedError) Error() string {
	if e == nil {
		return "ssh host key changed"
	}
	if e.DisableErr != nil {
		return fmt.Sprintf("ssh host key changed for host %q; disabling host failed: %v", e.HostID, e.DisableErr)
	}
	return fmt.Sprintf("ssh host key changed for host %q", e.HostID)
}

func (e *KeyChangedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.DisableErr
}

// IdentityUnavailableError prevents a connection from falling back to
// trust-on-every-use when a host has no stored identity.
type IdentityUnavailableError struct {
	HostID string
}

func (e *IdentityUnavailableError) Error() string {
	if e == nil || strings.TrimSpace(e.HostID) == "" {
		return "ssh host identity is unavailable"
	}
	return fmt.Sprintf("ssh host identity is unavailable for host %q", e.HostID)
}

// VerificationCallback builds a fail-closed callback from stored public
// identity material. A known_hosts record is compared by key bytes; a legacy
// fingerprint remains supported as a secure transition path.
func VerificationCallback(hostID, fingerprint, knownHosts string, onChange ChangeHandler) (ssh.HostKeyCallback, error) {
	fingerprint = canonicalFingerprint(fingerprint)
	expectedKey, err := parseKnownHostsKey(knownHosts)
	if err != nil {
		return nil, fmt.Errorf("parse stored ssh host identity: %w", err)
	}
	if expectedKey != nil {
		keyFingerprint := ssh.FingerprintSHA256(expectedKey)
		if fingerprint != "" && fingerprint != keyFingerprint {
			return nil, errors.New("stored ssh host identity fingerprint does not match known_hosts key")
		}
		fingerprint = keyFingerprint
	}
	if fingerprint == "" {
		return nil, &IdentityUnavailableError{HostID: strings.TrimSpace(hostID)}
	}

	return func(_ string, _ net.Addr, actualKey ssh.PublicKey) error {
		actualFingerprint := ssh.FingerprintSHA256(actualKey)
		matches := actualFingerprint == fingerprint
		if expectedKey != nil {
			matches = expectedKey.Type() == actualKey.Type() && bytes.Equal(expectedKey.Marshal(), actualKey.Marshal())
		}
		if matches {
			return nil
		}
		change := Change{
			HostID:         strings.TrimSpace(hostID),
			OldFingerprint: fingerprint,
			NewFingerprint: actualFingerprint,
		}
		var (
			hostDisabled bool
			disableErr   error
		)
		if onChange != nil {
			hostDisabled, disableErr = onChange(change)
		}
		return &KeyChangedError{
			HostID:         change.HostID,
			OldFingerprint: change.OldFingerprint,
			NewFingerprint: change.NewFingerprint,
			HostDisabled:   hostDisabled,
			DisableErr:     disableErr,
		}
	}, nil
}

func canonicalFingerprint(fingerprint string) string {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return ""
	}
	const prefix = "SHA256:"
	if len(fingerprint) >= len(prefix) && strings.EqualFold(fingerprint[:len(prefix)], prefix) {
		fingerprint = fingerprint[len(prefix):]
	}
	return prefix + fingerprint
}

func parseKnownHostsKey(value string) (ssh.PublicKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	_, _, key, _, rest, err := ssh.ParseKnownHosts([]byte(value + "\n"))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("multiple known_hosts records are not supported")
	}
	return key, nil
}
