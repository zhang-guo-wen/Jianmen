package dbtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	ModeDisable    = "disable"
	ModeVerifyCA   = "verify-ca"
	ModeVerifyFull = "verify-full"
)

// Config is the transport security policy for one upstream database instance.
type Config struct {
	Mode       string
	ServerName string
	CAPEM      string
}

func NormalizeMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return ModeDisable, nil
	}
	switch mode {
	case ModeDisable, ModeVerifyCA, ModeVerifyFull:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported upstream TLS mode %q", mode)
	}
}

// ClientConfig returns a certificate-verifying TLS configuration. verify-ca uses
// an explicit verifier because Go's built-in verifier always checks hostnames.
func ClientConfig(config Config, address string) (*tls.Config, error) {
	mode, err := NormalizeMode(config.Mode)
	if err != nil {
		return nil, err
	}
	if mode == ModeDisable {
		return nil, errors.New("TLS client config requested with disabled mode")
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if pem := strings.TrimSpace(config.CAPEM); pem != "" && !roots.AppendCertsFromPEM([]byte(pem)) {
		return nil, errors.New("invalid upstream TLS CA PEM")
	}
	serverName := strings.TrimSpace(config.ServerName)
	if serverName == "" {
		host, _, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			return nil, fmt.Errorf("derive TLS server name: %w", splitErr)
		}
		serverName = strings.Trim(host, "[]")
	}
	if serverName == "" {
		return nil, errors.New("upstream TLS server name is required")
	}
	if mode == ModeVerifyFull {
		return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: serverName}, nil
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		RootCAs:            roots,
		InsecureSkipVerify: true, // VerifyConnection validates the chain without a DNS name.
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("upstream TLS peer did not provide a certificate")
			}
			intermediates := x509.NewCertPool()
			for _, certificate := range state.PeerCertificates[1:] {
				intermediates.AddCert(certificate)
			}
			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{
				Roots:         roots,
				Intermediates: intermediates,
				KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			})
			return err
		},
	}, nil
}

func IsVerified(conn net.Conn) bool {
	tlsConn, ok := conn.(*verifiedConn)
	if !ok {
		return false
	}
	state := tlsConn.ConnectionState()
	return state.HandshakeComplete && len(state.PeerCertificates) > 0
}

// HandshakeClient performs a policy-controlled client handshake and returns a
// connection that can later be proven to have passed that policy.
func HandshakeClient(ctx context.Context, conn net.Conn, config Config, address string) (net.Conn, error) {
	tlsConfig, err := ClientConfig(config, address)
	if err != nil {
		return nil, err
	}
	secured := tls.Client(conn, tlsConfig)
	if err := secured.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	return &verifiedConn{Conn: secured}, nil
}

type verifiedConn struct {
	*tls.Conn
}
