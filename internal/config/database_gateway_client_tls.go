package config

import (
	"fmt"
	"strings"
)

const (
	DatabaseGatewayClientTLSModeRequired = "required"
	DatabaseGatewayClientTLSModeOptional = "optional"
)

// EffectiveClientTLSMode returns the client-facing database gateway TLS
// policy. An omitted value intentionally defaults to optional so clients can
// choose whether to negotiate TLS.
func (c DatabaseGatewayConfig) EffectiveClientTLSMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.ClientTLSMode))
	if mode == "" {
		return DatabaseGatewayClientTLSModeOptional
	}
	return mode
}

func (c DatabaseGatewayConfig) ClientTLSRequired() bool {
	return c.EffectiveClientTLSMode() == DatabaseGatewayClientTLSModeRequired
}

func validateDatabaseGatewayClientTLSMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case DatabaseGatewayClientTLSModeRequired, DatabaseGatewayClientTLSModeOptional:
		return nil
	default:
		return fmt.Errorf(
			"database_gateway.client_tls_mode must be %q or %q",
			DatabaseGatewayClientTLSModeRequired,
			DatabaseGatewayClientTLSModeOptional,
		)
	}
}
