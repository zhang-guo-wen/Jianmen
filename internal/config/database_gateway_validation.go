package config

import (
	"fmt"
	"net"
	"strings"
)

func validateDatabaseGateway(gateway DatabaseGatewayConfig) error {
	if err := validateDatabaseGatewayClientTLSMode(gateway.EffectiveClientTLSMode()); err != nil {
		return err
	}
	maxClientMessageBytes := gateway.MaxClientMessageBytes
	if maxClientMessageBytes == 0 {
		maxClientMessageBytes = DefaultDatabaseGatewayMaxClientMessageBytes
	}
	if maxClientMessageBytes < MinDatabaseGatewayMaxClientMessageBytes ||
		maxClientMessageBytes > MaxDatabaseGatewayMaxClientMessageBytes {
		return fmt.Errorf(
			"database_gateway.max_client_message_bytes must be between %d and %d",
			MinDatabaseGatewayMaxClientMessageBytes,
			MaxDatabaseGatewayMaxClientMessageBytes,
		)
	}
	return gateway.ValidateMode(gateway.EffectiveMode())
}

// ValidateMode verifies that one selectable database gateway mode can start
// with the current listener configuration.
func (gateway DatabaseGatewayConfig) ValidateMode(mode string) error {
	if !gateway.Enabled {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case DatabaseGatewayModeUnified:
		return validateUnifiedDatabaseListener(gateway.Unified)
	case DatabaseGatewayModeIndependent:
		return validateIndependentDatabaseListeners(gateway)
	default:
		return fmt.Errorf(
			"database_gateway.mode must be %q or %q",
			DatabaseGatewayModeUnified,
			DatabaseGatewayModeIndependent,
		)
	}
}

// AvailableModes returns the modes that can safely become effective after a
// restart. A disabled gateway does not constrain the stored preference.
func (gateway DatabaseGatewayConfig) AvailableModes() []string {
	modes := []string{
		DatabaseGatewayModeUnified,
		DatabaseGatewayModeIndependent,
	}
	if !gateway.Enabled {
		return modes
	}
	available := make([]string, 0, len(modes))
	for _, mode := range modes {
		if gateway.ValidateMode(mode) == nil {
			available = append(available, mode)
		}
	}
	return available
}

func validateUnifiedDatabaseListener(listener DatabaseUnifiedListener) error {
	if !listener.Enabled {
		return fmt.Errorf("database_gateway.unified must be enabled in unified mode")
	}
	if listener.DetectionTimeoutMS < 10 || listener.DetectionTimeoutMS > 2000 {
		return fmt.Errorf("database_gateway.unified.detection_timeout_ms must be between 10 and 2000")
	}
	return validateDatabaseListener(
		"unified",
		listener.Address,
		listener.CertFile,
		listener.KeyFile,
		listener.CAFile,
		listener.ServerName,
	)
}

func validateIndependentDatabaseListeners(gateway DatabaseGatewayConfig) error {
	listeners := []struct {
		name     string
		listener DatabaseProtocolListener
	}{
		{name: "mysql", listener: gateway.MySQL},
		{name: "postgresql", listener: gateway.PostgreSQL},
		{name: "redis", listener: gateway.Redis},
	}
	addresses := make(map[string]string, len(listeners))
	for _, item := range listeners {
		if !item.listener.Enabled {
			continue
		}
		if err := validateDatabaseListener(
			item.name,
			item.listener.Address,
			item.listener.CertFile,
			item.listener.KeyFile,
			item.listener.CAFile,
			item.listener.ServerName,
		); err != nil {
			return err
		}
		if other, exists := addresses[item.listener.Address]; exists {
			return fmt.Errorf(
				"database gateway listener addresses must be unique: %s and %s both use %q",
				other,
				item.name,
				item.listener.Address,
			)
		}
		addresses[item.listener.Address] = item.name
	}
	if len(addresses) == 0 {
		return fmt.Errorf("database gateway requires at least one enabled protocol listener")
	}
	return nil
}

func validateDatabaseListener(
	name, address, certFile, keyFile, caFile, serverName string,
) error {
	if _, _, err := net.SplitHostPort(address); err != nil {
		return fmt.Errorf("invalid database_gateway.%s.listen_addr %q: %w", name, address, err)
	}
	certConfigured := strings.TrimSpace(certFile) != ""
	keyConfigured := strings.TrimSpace(keyFile) != ""
	if certConfigured != keyConfigured {
		return fmt.Errorf("database_gateway.%s cert_file and key_file must be configured together", name)
	}
	if strings.TrimSpace(caFile) != "" && !certConfigured {
		return fmt.Errorf("database_gateway.%s ca_file requires cert_file and key_file", name)
	}
	if certConfigured && strings.TrimSpace(serverName) == "" {
		return fmt.Errorf("database_gateway.%s server_name is required when TLS is configured", name)
	}
	if serverName != "" {
		if err := validateDatabaseGatewayServerName(serverName); err != nil {
			return fmt.Errorf("database_gateway.%s server_name is invalid: %w", name, err)
		}
	}
	return nil
}

func validateDatabaseGatewayServerName(serverName string) error {
	if serverName == "" || serverName != strings.TrimSpace(serverName) {
		return fmt.Errorf("must not be empty or contain surrounding whitespace")
	}
	if net.ParseIP(serverName) != nil {
		return nil
	}
	if len(serverName) > 253 {
		return fmt.Errorf("DNS name exceeds 253 bytes")
	}
	for _, label := range strings.Split(serverName, ".") {
		if len(label) == 0 || len(label) > 63 {
			return fmt.Errorf("DNS label length must be between 1 and 63 bytes")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("DNS label must not start or end with a hyphen")
		}
		for index := 0; index < len(label); index++ {
			character := label[index]
			if (character >= 'a' && character <= 'z') ||
				(character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') ||
				character == '-' {
				continue
			}
			return fmt.Errorf("DNS label contains an invalid character")
		}
	}
	return nil
}
