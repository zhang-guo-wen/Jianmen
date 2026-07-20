package dbtls

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"jianmen/internal/config"
)

const (
	localGatewayIdentityDirectory = "database-gateway-tls"
	localGatewayCertificateFile   = "server.crt"
	localGatewayPrivateKeyFile    = "server.key"
	localGatewayDefaultServerName = "localhost"
)

// EnsureLocalUnifiedGatewayIdentity prepares a managed TLS identity only for
// an enabled unified database gateway bound to a loopback listener. Explicit
// certificate and key paths are always left untouched.
func EnsureLocalUnifiedGatewayIdentity(
	gateway *config.DatabaseGatewayConfig,
	dataDir string,
) (bool, error) {
	if gateway == nil {
		return false, errors.New("database gateway config is required")
	}
	listener := &gateway.Unified
	if !gateway.Enabled ||
		gateway.EffectiveMode() != config.DatabaseGatewayModeUnified ||
		!listener.Enabled ||
		!isLoopbackDatabaseListener(listener.Address) {
		return false, nil
	}
	if strings.TrimSpace(listener.CertFile) != "" ||
		strings.TrimSpace(listener.KeyFile) != "" {
		return false, nil
	}
	if strings.TrimSpace(listener.CAFile) != "" {
		return false, errors.New("cannot create managed database gateway identity while ca_file is configured")
	}
	if strings.TrimSpace(dataDir) == "" {
		return false, errors.New("metadata data directory is required")
	}

	serverName := strings.TrimSpace(listener.ServerName)
	if serverName == "" {
		serverName = localGatewayDefaultServerName
	}
	identityDir := filepath.Join(filepath.Clean(dataDir), localGatewayIdentityDirectory)
	certFile := filepath.Join(identityDir, localGatewayCertificateFile)
	keyFile := filepath.Join(identityDir, localGatewayPrivateKeyFile)
	generated, err := ensureManagedLocalGatewayIdentity(
		filepath.Clean(dataDir),
		identityDir,
		certFile,
		keyFile,
		serverName,
	)
	if err != nil {
		return false, err
	}

	listener.CertFile = certFile
	listener.KeyFile = keyFile
	listener.ServerName = serverName
	return generated, nil
}

func ensureManagedLocalGatewayIdentity(
	dataDir, identityDir, certFile, keyFile, serverName string,
) (bool, error) {
	info, err := os.Lstat(identityDir)
	switch {
	case err == nil:
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("managed database gateway TLS path %q is not a directory", identityDir)
		}
		return false, validateManagedLocalGatewayIdentity(certFile, keyFile, serverName)
	case !errors.Is(err, os.ErrNotExist):
		return false, fmt.Errorf("inspect managed database gateway TLS directory: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return false, fmt.Errorf("create metadata data directory: %w", err)
	}
	temporaryDir, err := os.MkdirTemp(dataDir, ".database-gateway-tls-")
	if err != nil {
		return false, fmt.Errorf("create temporary database gateway TLS directory: %w", err)
	}
	defer os.RemoveAll(temporaryDir)

	temporaryCert := filepath.Join(temporaryDir, localGatewayCertificateFile)
	temporaryKey := filepath.Join(temporaryDir, localGatewayPrivateKeyFile)
	if err := generateLocalGatewayIdentity(temporaryCert, temporaryKey, serverName); err != nil {
		return false, err
	}
	if err := validateManagedLocalGatewayIdentity(temporaryCert, temporaryKey, serverName); err != nil {
		return false, fmt.Errorf("validate generated database gateway TLS identity: %w", err)
	}
	if err := os.Rename(temporaryDir, identityDir); err != nil {
		existing, statErr := os.Lstat(identityDir)
		if statErr != nil {
			return false, fmt.Errorf("persist managed database gateway TLS identity: %w", err)
		}
		if !existing.IsDir() || existing.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("managed database gateway TLS path %q is not a directory", identityDir)
		}
		if validateErr := validateManagedLocalGatewayIdentity(certFile, keyFile, serverName); validateErr != nil {
			return false, validateErr
		}
		return false, nil
	}
	return true, nil
}

func isLoopbackDatabaseListener(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
