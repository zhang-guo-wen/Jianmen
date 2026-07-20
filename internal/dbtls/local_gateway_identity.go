package dbtls

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"jianmen/internal/config"
)

const (
	localGatewayIdentityDirectory    = "database-gateway-tls"
	localMySQLIdentityDirectory      = "database-gateway-tls-mysql"
	localPostgreSQLIdentityDirectory = "database-gateway-tls-postgresql"
	localRedisIdentityDirectory      = "database-gateway-tls-redis"
	localGatewayCertificateFile      = "server.crt"
	localGatewayPrivateKeyFile       = "server.key"
	localGatewayDefaultServerName    = "localhost"
)

// EnsureLocalGatewayIdentities prepares managed TLS identities for every
// enabled listener in the selected mode that is bound to a loopback address.
// Explicit certificate and key paths are always left untouched.
func EnsureLocalGatewayIdentities(
	gateway *config.DatabaseGatewayConfig,
	dataDir string,
) (bool, error) {
	if gateway == nil {
		return false, errors.New("database gateway config is required")
	}
	if !gateway.Enabled {
		return false, nil
	}
	if gateway.EffectiveMode() == config.DatabaseGatewayModeUnified {
		return EnsureLocalUnifiedGatewayIdentity(gateway, dataDir)
	}

	listeners := []struct {
		directory string
		listener  *config.DatabaseProtocolListener
	}{
		{directory: localMySQLIdentityDirectory, listener: &gateway.MySQL},
		{directory: localPostgreSQLIdentityDirectory, listener: &gateway.PostgreSQL},
		{directory: localRedisIdentityDirectory, listener: &gateway.Redis},
	}
	generatedAny := false
	for _, item := range listeners {
		generated, err := ensureLocalListenerIdentity(
			item.listener.Enabled,
			item.listener.Address,
			&item.listener.CertFile,
			&item.listener.KeyFile,
			item.listener.CAFile,
			&item.listener.ServerName,
			dataDir,
			item.directory,
		)
		if err != nil {
			return false, err
		}
		generatedAny = generatedAny || generated
	}
	return generatedAny, nil
}

// EnsureLocalUnifiedGatewayIdentity prepares a managed TLS identity for an
// enabled unified gateway bound to a loopback listener.
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
		!listener.Enabled {
		return false, nil
	}
	return ensureLocalListenerIdentity(
		listener.Enabled,
		listener.Address,
		&listener.CertFile,
		&listener.KeyFile,
		listener.CAFile,
		&listener.ServerName,
		dataDir,
		localGatewayIdentityDirectory,
	)
}

func ensureLocalListenerIdentity(
	enabled bool,
	address string,
	certFile *string,
	keyFile *string,
	caFile string,
	serverName *string,
	dataDir string,
	identityDirectory string,
) (bool, error) {
	if !enabled {
		return false, nil
	}
	certificateConfigured := strings.TrimSpace(*certFile) != ""
	keyConfigured := strings.TrimSpace(*keyFile) != ""
	if certificateConfigured != keyConfigured {
		return false, errors.New("database gateway certificate and key must be configured together")
	}
	if certificateConfigured {
		if _, err := tls.LoadX509KeyPair(*certFile, *keyFile); err != nil {
			return false, fmt.Errorf("load database gateway key pair: %w", err)
		}
		if _, err := LoadServerIdentity(*certFile, caFile, strings.TrimSpace(*serverName)); err != nil {
			return false, fmt.Errorf("validate database gateway server identity: %w", err)
		}
		return false, nil
	}
	if !isLoopbackDatabaseListener(address) {
		return false, nil
	}
	if strings.TrimSpace(caFile) != "" {
		return false, errors.New("cannot create managed database gateway identity while ca_file is configured")
	}
	if strings.TrimSpace(dataDir) == "" {
		return false, errors.New("metadata data directory is required")
	}

	effectiveServerName := strings.TrimSpace(*serverName)
	if effectiveServerName == "" {
		effectiveServerName = localGatewayDefaultServerName
	}
	identityDir := filepath.Join(filepath.Clean(dataDir), identityDirectory)
	managedCertFile := filepath.Join(identityDir, localGatewayCertificateFile)
	managedKeyFile := filepath.Join(identityDir, localGatewayPrivateKeyFile)
	generated, err := ensureManagedLocalGatewayIdentity(
		filepath.Clean(dataDir),
		identityDir,
		managedCertFile,
		managedKeyFile,
		effectiveServerName,
	)
	if err != nil {
		return false, err
	}

	*certFile = managedCertFile
	*keyFile = managedKeyFile
	*serverName = effectiveServerName
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
