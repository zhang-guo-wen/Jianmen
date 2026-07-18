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
	tlsEnabled := enabled &&
		strings.TrimSpace(listener.CertFile) != "" &&
		strings.TrimSpace(listener.KeyFile) != ""
	response := map[string]any{
		"enabled":     enabled,
		"protocol":    protocol,
		"listen_addr": listener.Address,
		"host":        host,
		"port":        port,
		"tls_enabled": tlsEnabled,
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
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "", "mysql":
		return "mysql", gateway.MySQL, true
	case "postgres", "postgresql":
		return "postgresql", gateway.PostgreSQL, true
	case "redis":
		return "redis", gateway.Redis, true
	default:
		return "", config.DatabaseProtocolListener{}, false
	}
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
