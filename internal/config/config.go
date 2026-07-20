package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

const (
	DefaultDatabaseGatewayMaxClientMessageBytes = 10 * 1024 * 1024
	MinDatabaseGatewayMaxClientMessageBytes     = 64 * 1024
	MaxDatabaseGatewayMaxClientMessageBytes     = 16 * 1024 * 1024
)

type Config struct {
	ListenAddr         string                   `json:"listen_addr"`
	HostKeyPath        string                   `json:"host_key_path"`
	ReplayDir          string                   `json:"replay_dir"`
	TargetsFile        string                   `json:"targets_file"`
	Admin              AdminConfig              `json:"admin"`
	Database           DatabaseConfig           `json:"database"`
	DatabaseGateway    DatabaseGatewayConfig    `json:"database_gateway"`
	ApplicationGateway ApplicationGatewayConfig `json:"application_gateway"`
	WebRDP             WebRDPConfig             `json:"web_rdp"`
	ObjectStorage      ObjectStorageConfig      `json:"object_storage"`
	Recording          RecordingConfig          `json:"recording"`
	Users              []User                   `json:"users"`
	Targets            []Target                 `json:"targets"`
	DefaultTarget      string                   `json:"default_target"`
}

type AdminConfig struct {
	Enabled            bool           `json:"enabled"`
	ListenAddr         string         `json:"listen_addr"`
	PublicURL          string         `json:"public_url"`
	CORSAllowedOrigins []string       `json:"cors_allowed_origins"`
	Dev                bool           `json:"dev"`
	TLS                AdminTLSConfig `json:"tls"`
}

type AdminTLSConfig struct {
	CertFile          string `json:"cert_file"`
	KeyFile           string `json:"key_file"`
	AllowInsecureHTTP bool   `json:"allow_insecure_http"`
}

type DatabaseConfig struct {
	Enabled                bool   `json:"enabled"`
	Driver                 string `json:"driver"`
	DSN                    string `json:"dsn"`
	MaxOpenConns           int    `json:"max_open_conns"`
	MaxIdleConns           int    `json:"max_idle_conns"`
	ConnMaxLifetimeSeconds int    `json:"conn_max_lifetime_seconds"`
}

type ApplicationGatewayConfig struct {
	Enabled   bool `json:"enabled"`
	PortStart int  `json:"port_start"`
	PortEnd   int  `json:"port_end"`
}

type RecordingConfig struct {
	Enabled           bool  `json:"enabled"`
	RecordInput       bool  `json:"record_input"`
	RecordCommands    bool  `json:"record_commands"`
	RetentionDays     int   `json:"retention_days"`
	MaxReplayBytes    int64 `json:"max_replay_bytes"`
	CleanupBatchSize  int   `json:"cleanup_batch_size"`
	enabledSet        bool
	recordInputSet    bool
	recordCommandsSet bool
	maxReplayBytesSet bool
}

func (c *RecordingConfig) UnmarshalJSON(data []byte) error {
	var value struct {
		Enabled          *bool  `json:"enabled"`
		RecordInput      *bool  `json:"record_input"`
		RecordCommands   *bool  `json:"record_commands"`
		RetentionDays    int    `json:"retention_days"`
		MaxReplayBytes   *int64 `json:"max_replay_bytes"`
		CleanupBatchSize int    `json:"cleanup_batch_size"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*c = RecordingConfig{
		RetentionDays:    value.RetentionDays,
		CleanupBatchSize: value.CleanupBatchSize,
	}
	if value.Enabled != nil {
		c.Enabled = *value.Enabled
		c.enabledSet = true
	}
	if value.RecordInput != nil {
		c.RecordInput = *value.RecordInput
		c.recordInputSet = true
	}
	if value.RecordCommands != nil {
		c.RecordCommands = *value.RecordCommands
		c.recordCommandsSet = true
	}
	if value.MaxReplayBytes != nil {
		c.MaxReplayBytes = *value.MaxReplayBytes
		c.maxReplayBytesSet = true
	}
	return nil
}

type User struct {
	ID                 string   `json:"id"`
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	ApiToken           string   `json:"api_token"`
	SuperAdmin         bool     `json:"super_admin"`
	PublicKeys         []string `json:"public_keys"`
	AuthorizedKeysPath string   `json:"authorized_keys_path"`
}

type Target struct {
	ID                    string `json:"id"`
	HostID                string `json:"host_id"`
	Protocol              string `json:"protocol"`
	Name                  string `json:"name"`
	Group                 string `json:"group"`
	Remark                string `json:"remark"`
	Disabled              bool   `json:"disabled"`
	ExpiresAt             string `json:"expires_at"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	Domain                string `json:"domain"`
	Password              string `json:"password"`
	PrivateKeyPath        string `json:"private_key_path"`
	PrivateKeyPEM         string `json:"private_key_pem"`
	Passphrase            string `json:"passphrase"`
	InsecureIgnoreHostKey bool   `json:"insecure_ignore_host_key"`
	HostKeyFingerprint    string `json:"host_key_fingerprint"`
	KnownHostsPath        string `json:"known_hosts_path"`
	RDPSecurity           string `json:"rdp_security"`
	RDPIgnoreCertificate  bool   `json:"rdp_ignore_certificate"`
	RDPCertFingerprints   string `json:"rdp_cert_fingerprints"`
	RDPApprovalRequired   bool   `json:"rdp_approval_required"`
	RDPClipboardRead      bool   `json:"rdp_clipboard_read"`
	RDPClipboardWrite     bool   `json:"rdp_clipboard_write"`
	RDPFileUpload         bool   `json:"rdp_file_upload"`
	RDPFileDownload       bool   `json:"rdp_file_download"`
	RDPDriveMapping       bool   `json:"rdp_drive_mapping"`
}

func Load(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		// 配置文件不存在时使用默认值，零配置启动
		if os.IsNotExist(err) {
			cfg := defaultConfig()
			if err := cfg.Validate(); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, err
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func defaultConfig() *Config {
	cfg := &Config{}
	cfg.applyDefaults()
	return cfg
}

func (c *Config) applyDefaults() {
	adminEmpty := !c.Admin.Enabled &&
		c.Admin.ListenAddr == "" &&
		len(c.Admin.CORSAllowedOrigins) == 0
	databaseEmpty := c.Database == (DatabaseConfig{})
	if c.ListenAddr == "" {
		c.ListenAddr = "0.0.0.0:47102"
	}
	if c.HostKeyPath == "" {
		c.HostKeyPath = "data/host_key"
	}
	if c.ReplayDir == "" {
		c.ReplayDir = "data/replay"
	}
	if c.TargetsFile == "" {
		c.TargetsFile = "data/targets.json"
	}
	if adminEmpty {
		c.Admin.ListenAddr = "127.0.0.1:47100"
		c.Admin.Enabled = true
		c.Admin.CORSAllowedOrigins = []string{"http://127.0.0.1:47101", "http://localhost:47101"}
	}
	if !c.Recording.enabledSet {
		c.Recording.Enabled = true
	}
	if !c.Recording.recordInputSet {
		c.Recording.RecordInput = false
	}
	if !c.Recording.recordCommandsSet {
		c.Recording.RecordCommands = true
	}
	if c.Recording.RetentionDays == 0 {
		c.Recording.RetentionDays = 30
	}
	if c.Recording.CleanupBatchSize == 0 {
		c.Recording.CleanupBatchSize = 100
	}
	if !c.Recording.maxReplayBytesSet && c.Recording.MaxReplayBytes == 0 {
		c.Recording.MaxReplayBytes = 10 * 1024 * 1024 * 1024
	}
	if databaseEmpty {
		c.Database.Enabled = true
		c.Database.Driver = "sqlite"
		c.Database.DSN = "data/bastion.db"
	}
	c.DatabaseGateway.applyDefaults()
	if !c.ApplicationGateway.Enabled && c.ApplicationGateway.PortStart == 0 && c.ApplicationGateway.PortEnd == 0 {
		c.ApplicationGateway.Enabled = true
		c.ApplicationGateway.PortStart = 47110
		c.ApplicationGateway.PortEnd = 47199
	}
	c.applyWebRDPDefaults()
	if c.Database.Enabled && c.Database.Driver == "" {
		c.Database.Driver = "sqlite"
	}
	if c.Database.Enabled && c.Database.Driver == "sqlite" && c.Database.DSN == "" {
		c.Database.DSN = "data/bastion.db"
	}
	for i := range c.Targets {
		if c.Targets[i].Port == 0 {
			if strings.EqualFold(strings.TrimSpace(c.Targets[i].Protocol), "rdp") {
				c.Targets[i].Port = 3389
			} else {
				c.Targets[i].Port = 22
			}
		}
	}
}

func (c *Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		return fmt.Errorf("invalid listen_addr %q: %w", c.ListenAddr, err)
	}
	if c.Admin.Enabled {
		if _, _, err := net.SplitHostPort(c.Admin.ListenAddr); err != nil {
			return fmt.Errorf("invalid admin.listen_addr %q: %w", c.Admin.ListenAddr, err)
		}
		if err := validatePublicURL(c.Admin.PublicURL); err != nil {
			return fmt.Errorf("invalid admin.public_url: %w", err)
		}
		if err := validateAdminTransport(c.Admin); err != nil {
			return fmt.Errorf("invalid admin transport: %w", err)
		}
	}
	if c.Database.Enabled {
		switch c.Database.Driver {
		case "sqlite", "sqlite3", "mysql", "postgres", "postgresql":
		default:
			return fmt.Errorf("database.driver %q is not supported", c.Database.Driver)
		}
		if (c.Database.Driver == "mysql" || c.Database.Driver == "postgres" || c.Database.Driver == "postgresql") && c.Database.DSN == "" {
			return fmt.Errorf("database.dsn is required for driver %q", c.Database.Driver)
		}
	}
	if err := validateDatabaseGateway(c.DatabaseGateway); err != nil {
		return err
	}
	if c.Recording.RetentionDays < 0 || c.Recording.RetentionDays > 3650 {
		return fmt.Errorf("recording.retention_days must be between 1 and 3650")
	}
	if c.Recording.MaxReplayBytes < 0 {
		return fmt.Errorf("recording.max_replay_bytes must not be negative")
	}
	if c.Recording.CleanupBatchSize < 0 || c.Recording.CleanupBatchSize > 1000 {
		return fmt.Errorf("recording.cleanup_batch_size must be between 1 and 1000")
	}
	if c.ApplicationGateway.Enabled {
		if c.ApplicationGateway.PortStart <= 0 || c.ApplicationGateway.PortStart > 65535 {
			return fmt.Errorf("invalid application_gateway.port_start %d", c.ApplicationGateway.PortStart)
		}
		if c.ApplicationGateway.PortEnd <= 0 || c.ApplicationGateway.PortEnd > 65535 {
			return fmt.Errorf("invalid application_gateway.port_end %d", c.ApplicationGateway.PortEnd)
		}
		if c.ApplicationGateway.PortStart > c.ApplicationGateway.PortEnd {
			return fmt.Errorf("application_gateway.port_start (%d) > port_end (%d)", c.ApplicationGateway.PortStart, c.ApplicationGateway.PortEnd)
		}
	}
	if err := c.validateWebRDP(); err != nil {
		return err
	}
	// Users may be empty — the setup wizard creates the first admin user.
	if len(c.Users) == 0 {
		// No hard error; admin user is created via the setup wizard at /api/init/setup
	}
	return nil
}

func validateAdminTransport(admin AdminConfig) error {
	certConfigured := strings.TrimSpace(admin.TLS.CertFile) != ""
	keyConfigured := strings.TrimSpace(admin.TLS.KeyFile) != ""
	if certConfigured != keyConfigured {
		return fmt.Errorf("cert_file and key_file must be configured together")
	}
	if certConfigured {
		return nil
	}
	if admin.TLS.AllowInsecureHTTP || isLoopbackListenAddr(admin.ListenAddr) {
		return nil
	}
	return fmt.Errorf("insecure HTTP on non-loopback admin.listen_addr requires tls.allow_insecure_http=true")
}

func isLoopbackListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
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

func validatePublicURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("host is required")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("credentials, query, and fragment are not allowed")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("path must be empty")
	}
	return nil
}

func (t Target) Addr() string {
	return net.JoinHostPort(t.Host, fmt.Sprintf("%d", t.Port))
}
