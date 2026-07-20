package sshhost

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const DefaultCollectTimeout = 5 * time.Second

var errIdentityCaptured = errors.New("ssh host identity captured")

// Collector performs a bounded SSH handshake and stops immediately after the
// server public key is presented, before user authentication.
type Collector struct {
	timeout time.Duration
}

func NewCollector(timeout time.Duration) *Collector {
	if timeout <= 0 {
		timeout = DefaultCollectTimeout
	}
	return &Collector{timeout: timeout}
}

func (c *Collector) Collect(ctx context.Context, address string, port int) (Identity, error) {
	if ctx == nil {
		return Identity{}, errors.New("collect ssh host identity: context is required")
	}
	endpoint, err := endpointAddress(address, port)
	if err != nil {
		return Identity{}, err
	}
	timeout := DefaultCollectTimeout
	if c != nil && c.timeout > 0 {
		timeout = c.timeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", endpoint)
	if err != nil {
		return Identity{}, fmt.Errorf("connect to collect ssh host identity: %w", err)
	}
	defer conn.Close()
	if deadline, ok := probeCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stopCancelClose := context.AfterFunc(probeCtx, func() {
		_ = conn.Close()
	})
	defer stopCancelClose()

	var identity Identity
	clientConfig := &ssh.ClientConfig{
		User: "jianmen-host-key-probe",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			identity = Identity{
				Fingerprint: ssh.FingerprintSHA256(key),
				KnownHosts:  knownhosts.Line([]string{knownhosts.Normalize(endpoint)}, key),
			}
			return errIdentityCaptured
		},
	}
	_, _, _, handshakeErr := ssh.NewClientConn(conn, endpoint, clientConfig)
	if errors.Is(handshakeErr, errIdentityCaptured) && identity.Fingerprint != "" {
		return identity, nil
	}
	if handshakeErr == nil {
		return Identity{}, errors.New("collect ssh host identity: server did not present a host key")
	}
	return Identity{}, fmt.Errorf("collect ssh host identity: %w", handshakeErr)
}

func endpointAddress(address string, port int) (string, error) {
	address = strings.Trim(strings.TrimSpace(address), "[]")
	if address == "" {
		return "", errors.New("collect ssh host identity: address is required")
	}
	if port <= 0 || port > 65535 {
		return "", errors.New("collect ssh host identity: port must be between 1 and 65535")
	}
	return net.JoinHostPort(address, fmt.Sprintf("%d", port)), nil
}
