package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
)

type Config struct {
	ListenAddr      string                `json:"listen_addr"`
	HostKeyPath     string                `json:"host_key_path"`
	ReplayDir       string                `json:"replay_dir"`
	TargetsFile     string                `json:"targets_file"`
	Admin           AdminConfig           `json:"admin"`
	Database        DatabaseConfig        `json:"database"`
	Recording       RecordingConfig       `json:"recording"`
	Users           []User                `json:"users"`
	Targets         []Target              `json:"targets"`
	DefaultTarget   string                `json:"default_target"`
	DatabaseProxies []DatabaseProxyConfig `json:"database_proxies"`
}

type AdminConfig struct {
	Enabled            bool     `json:"enabled"`
	ListenAddr         string   `json:"listen_addr"`
	Token              string   `json:"token"`
	CORSAllowedOrigins []string `json:"cors_allowed_origins"`
}

type DatabaseConfig struct {
	Enabled                bool   `json:"enabled"`
	Driver                 string `json:"driver"`
	DSN                    string `json:"dsn"`
	AutoMigrate            bool   `json:"auto_migrate"`
	MaxOpenConns           int    `json:"max_open_conns"`
	MaxIdleConns           int    `json:"max_idle_conns"`
	ConnMaxLifetimeSeconds int    `json:"conn_max_lifetime_seconds"`
}

type RecordingConfig struct {
	Enabled        bool `json:"enabled"`
	RecordInput    bool `json:"record_input"`
	RecordCommands bool `json:"record_commands"`
}

type User struct {
	ID                 string   `json:"id"`
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	PublicKeys         []string `json:"public_keys"`
	AuthorizedKeysPath string   `json:"authorized_keys_path"`
}

type Target struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	Password              string `json:"password"`
	PrivateKeyPath        string `json:"private_key_path"`
	PrivateKeyPEM         string `json:"private_key_pem"`
	Passphrase            string `json:"passphrase"`
	InsecureIgnoreHostKey bool   `json:"insecure_ignore_host_key"`
	HostKeyFingerprint    string `json:"host_key_fingerprint"`
	KnownHostsPath        string `json:"known_hosts_path"`
}

type DatabaseProxyConfig struct {
	Enabled      bool     `json:"enabled"`
	Name         string   `json:"name"`
	Protocol     string   `json:"protocol"`
	ListenAddr   string   `json:"listen_addr"`
	UpstreamAddr string   `json:"upstream_addr"`
	AllowedUsers []string `json:"allowed_users"`
}

func Load(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
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

func (c *Config) applyDefaults() {
	adminEmpty := !c.Admin.Enabled &&
		c.Admin.ListenAddr == "" &&
		c.Admin.Token == "" &&
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
	if c.Admin.ListenAddr == "" {
		c.Admin.ListenAddr = "127.0.0.1:47100"
	}
	if c.Admin.Token == "" {
		c.Admin.Token = "dev-admin-token"
	}
	if adminEmpty && len(c.Admin.CORSAllowedOrigins) == 0 {
		c.Admin.CORSAllowedOrigins = []string{"http://127.0.0.1:47101", "http://localhost:47101"}
	}
	if !c.Recording.Enabled && !c.Recording.RecordCommands && !c.Recording.RecordInput {
		c.Recording.Enabled = true
		c.Recording.RecordCommands = true
	}
	if adminEmpty {
		c.Admin.Enabled = true
	}
	if databaseEmpty {
		c.Database.Enabled = true
		c.Database.Driver = "sqlite"
		c.Database.DSN = "data/bastion.db"
		c.Database.AutoMigrate = true
	}
	if c.Database.Enabled && c.Database.Driver == "" {
		c.Database.Driver = "sqlite"
	}
	if c.Database.Enabled && c.Database.Driver == "sqlite" && c.Database.DSN == "" {
		c.Database.DSN = "data/bastion.db"
	}
	for i := range c.Targets {
		if c.Targets[i].Port == 0 {
			c.Targets[i].Port = 22
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
	if len(c.Users) == 0 {
		return errors.New("at least one user is required")
	}
	if len(c.Targets) == 0 {
		return errors.New("at least one target is required")
	}
	if c.DefaultTarget == "" {
		c.DefaultTarget = c.Targets[0].ID
	}
	for _, target := range c.Targets {
		if target.ID == c.DefaultTarget {
			return c.validateDatabaseProxies()
		}
	}
	return fmt.Errorf("default_target %q does not match any configured target", c.DefaultTarget)
}

func (c *Config) validateDatabaseProxies() error {
	for _, proxy := range c.DatabaseProxies {
		if !proxy.Enabled {
			continue
		}
		if proxy.Name == "" {
			return errors.New("enabled database proxy is missing name")
		}
		switch proxy.Protocol {
		case "mysql", "postgres", "tcp":
		default:
			return fmt.Errorf("database proxy %q has unsupported protocol %q", proxy.Name, proxy.Protocol)
		}
		if _, _, err := net.SplitHostPort(proxy.ListenAddr); err != nil {
			return fmt.Errorf("database proxy %q has invalid listen_addr %q: %w", proxy.Name, proxy.ListenAddr, err)
		}
		if _, _, err := net.SplitHostPort(proxy.UpstreamAddr); err != nil {
			return fmt.Errorf("database proxy %q has invalid upstream_addr %q: %w", proxy.Name, proxy.UpstreamAddr, err)
		}
		for _, user := range proxy.AllowedUsers {
			if user == "" {
				return fmt.Errorf("database proxy %q has empty allowed_users entry", proxy.Name)
			}
		}
	}
	return nil
}

func (t Target) Addr() string {
	return net.JoinHostPort(t.Host, fmt.Sprintf("%d", t.Port))
}
