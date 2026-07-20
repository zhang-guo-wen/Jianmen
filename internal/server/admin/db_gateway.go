package admin

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"jianmen/internal/config"
	"jianmen/internal/dbtls"
	"jianmen/internal/rbac"
)

const databaseGatewayTLSMaterialUnavailable = "database gateway TLS identity material is unavailable"

const (
	databaseGatewayUnavailableGatewayDisabled    = "gateway_disabled"
	databaseGatewayUnavailableListenerDisabled   = "listener_disabled"
	databaseGatewayUnavailableTLSIdentityMissing = "tls_identity_missing"
)

func (s *Server) handleDBGateway(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAnyPermission(r, rbac.ActionDBProxyView, rbac.ActionDBConnect) {
		s.forbidden(w, r)
		return
	}
	cfg := s.cfg.DatabaseGateway
	protocol, listener, ok := databaseProtocolListener(cfg, r.URL.Query().Get("protocol"))
	if !ok {
		s.writeErrorText(w, r, http.StatusBadRequest, "unsupported database protocol")
		return
	}
	host, port := parseListenAddr(listener.Address)
	enabled := cfg.Enabled && listener.Enabled
	tlsConfigured := strings.TrimSpace(listener.CertFile) != "" &&
		strings.TrimSpace(listener.KeyFile) != ""
	connectable, unavailableReason := databaseGatewayAvailability(
		cfg.Enabled,
		listener.Enabled,
		protocol,
		tlsConfigured,
	)
	tlsEnabled := enabled && tlsConfigured
	response := map[string]any{
		"enabled":                  enabled,
		"connectable":              connectable,
		"unavailable_reason":       unavailableReason,
		"mode":                     cfg.EffectiveMode(),
		"protocol":                 protocol,
		"listen_addr":              listener.Address,
		"host":                     host,
		"port":                     port,
		"tls_enabled":              tlsEnabled,
		"mysql_detection_delay_ms": databaseGatewayMySQLDetectionDelay(cfg),
	}
	if tlsEnabled {
		caPEM, fingerprint, err := databaseGatewayTLSIdentityMaterial(listener)
		if err != nil {
			s.writeErrorText(w, r, http.StatusServiceUnavailable, databaseGatewayTLSMaterialUnavailable)
			return
		}
		response["tls_server_name"] = strings.TrimSpace(listener.ServerName)
		response["tls_ca_pem"] = caPEM
		response["tls_cert_sha256"] = fingerprint
	}
	s.writeJSON(w, r, http.StatusOK, response)
}

func databaseGatewayTLSIdentityMaterial(listener config.DatabaseProtocolListener) (string, string, error) {
	identity, err := dbtls.LoadServerIdentity(listener.CertFile, listener.CAFile, listener.ServerName)
	if err != nil {
		return "", "", err
	}
	return identity.CAPEM, identity.LeafSHA256, nil
}

func databaseProtocolListener(gateway config.DatabaseGatewayConfig, requested string) (string, config.DatabaseProtocolListener, bool) {
	protocol := ""
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "", "mysql":
		protocol = "mysql"
	case "postgres", "postgresql":
		protocol = "postgresql"
	case "redis":
		protocol = "redis"
	default:
		return "", config.DatabaseProtocolListener{}, false
	}
	if gateway.EffectiveMode() == config.DatabaseGatewayModeUnified {
		unified := gateway.Unified
		return protocol, config.DatabaseProtocolListener{
			Enabled:    unified.Enabled,
			Address:    unified.Address,
			CertFile:   unified.CertFile,
			KeyFile:    unified.KeyFile,
			CAFile:     unified.CAFile,
			ServerName: unified.ServerName,
		}, true
	}
	switch protocol {
	case "postgresql":
		return protocol, gateway.PostgreSQL, true
	case "redis":
		return protocol, gateway.Redis, true
	default:
		return protocol, gateway.MySQL, true
	}
}

func databaseGatewayAvailability(
	gatewayEnabled bool,
	listenerEnabled bool,
	protocol string,
	tlsConfigured bool,
) (bool, string) {
	if !gatewayEnabled {
		return false, databaseGatewayUnavailableGatewayDisabled
	}
	if !listenerEnabled {
		return false, databaseGatewayUnavailableListenerDisabled
	}
	if protocol == "postgresql" && !tlsConfigured {
		return false, databaseGatewayUnavailableTLSIdentityMissing
	}
	return true, ""
}

func databaseGatewayMySQLDetectionDelay(gateway config.DatabaseGatewayConfig) int {
	if gateway.EffectiveMode() != config.DatabaseGatewayModeUnified || !gateway.Unified.Enabled {
		return 0
	}
	return gateway.Unified.DetectionTimeoutMS
}

func parseListenAddr(addr string) (host string, port int) {
	if addr == "" {
		return "", 33060
	}
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 33060
	}
	host = h
	// Wildcard bind addresses are not client connection addresses; the UI falls back to its current host.
	if host == "0.0.0.0" || host == "::" {
		host = ""
	}
	if n, err := strconv.Atoi(p); err == nil {
		port = n
	} else {
		port = 33060
	}
	return
}
