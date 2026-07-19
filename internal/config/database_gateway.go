package config

import (
	"bytes"
	"encoding/json"
	"strings"
)

const (
	DatabaseGatewayModeUnified     = "unified"
	DatabaseGatewayModeIndependent = "independent"

	defaultDatabaseUnifiedDetectionTimeoutMS = 200
)

type DatabaseGatewayConfig struct {
	Enabled               bool                     `json:"enabled"`
	MaxClientMessageBytes int                      `json:"max_client_message_bytes"`
	Mode                  string                   `json:"mode"`
	Unified               DatabaseUnifiedListener  `json:"unified"`
	MySQL                 DatabaseProtocolListener `json:"mysql"`
	PostgreSQL            DatabaseProtocolListener `json:"postgresql"`
	Redis                 DatabaseProtocolListener `json:"redis"`
	enabledSet            bool
}

// DatabaseUnifiedListener accepts MySQL, PostgreSQL and Redis on one address.
// MySQL clients wait for DetectionTimeoutMS because they do not send the first
// protocol bytes until after the server greeting.
type DatabaseUnifiedListener struct {
	Enabled            bool   `json:"enabled"`
	Address            string `json:"listen_addr"`
	CertFile           string `json:"cert_file"`
	KeyFile            string `json:"key_file"`
	CAFile             string `json:"ca_file"`
	ServerName         string `json:"server_name"`
	DetectionTimeoutMS int    `json:"detection_timeout_ms"`
}

// DatabaseProtocolListener configures one protocol-specific database gateway
// listener. TLS is negotiated by the protocol where applicable.
type DatabaseProtocolListener struct {
	Enabled    bool   `json:"enabled"`
	Address    string `json:"listen_addr"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	CAFile     string `json:"ca_file"`
	ServerName string `json:"server_name"`
}

func (c *DatabaseGatewayConfig) UnmarshalJSON(data []byte) error {
	type alias DatabaseGatewayConfig
	aux := struct {
		Enabled *bool `json:"enabled"`
		*alias
	}{alias: (*alias)(c)}

	c.enabledSet = false
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&aux); err != nil {
		return err
	}
	if aux.Enabled != nil {
		c.enabledSet = true
		c.Enabled = *aux.Enabled
	}
	return nil
}

// EffectiveMode preserves directly constructed legacy test configurations:
// an empty mode selects unified only when that listener was explicitly built.
// Config.Load always applies an explicit production default before validation.
func (c DatabaseGatewayConfig) EffectiveMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode != "" {
		return mode
	}
	if c.Unified.Enabled || strings.TrimSpace(c.Unified.Address) != "" {
		return DatabaseGatewayModeUnified
	}
	return DatabaseGatewayModeIndependent
}

func (c *DatabaseGatewayConfig) applyDefaults() {
	empty := !c.Enabled &&
		!c.enabledSet &&
		strings.TrimSpace(c.Mode) == "" &&
		c.Unified == (DatabaseUnifiedListener{}) &&
		c.MySQL == (DatabaseProtocolListener{}) &&
		c.PostgreSQL == (DatabaseProtocolListener{}) &&
		c.Redis == (DatabaseProtocolListener{})
	if empty {
		c.Enabled = true
		c.Unified.Enabled = true
		c.MySQL.Enabled = true
	}
	if strings.TrimSpace(c.Mode) == "" {
		c.Mode = DatabaseGatewayModeUnified
	}
	if strings.TrimSpace(c.Unified.Address) == "" {
		c.Unified.Address = "127.0.0.1:33060"
	}
	if c.Unified.DetectionTimeoutMS == 0 {
		c.Unified.DetectionTimeoutMS = defaultDatabaseUnifiedDetectionTimeoutMS
	}
	if strings.TrimSpace(c.MySQL.Address) == "" {
		c.MySQL.Address = "127.0.0.1:33061"
	}
	if strings.TrimSpace(c.PostgreSQL.Address) == "" {
		c.PostgreSQL.Address = "127.0.0.1:33062"
	}
	if strings.TrimSpace(c.Redis.Address) == "" {
		c.Redis.Address = "127.0.0.1:33063"
	}
	if c.MaxClientMessageBytes == 0 {
		c.MaxClientMessageBytes = DefaultDatabaseGatewayMaxClientMessageBytes
	}
	if c.Enabled && c.EffectiveMode() == DatabaseGatewayModeUnified {
		c.Unified.Enabled = true
	}
}
