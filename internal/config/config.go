package config

import (
	"encoding/json"
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
	DatabaseGateway DatabaseGatewayConfig `json:"database_gateway"`
	Recording       RecordingConfig       `json:"recording"`
	Users           []User                `json:"users"`
	Targets         []Target              `json:"targets"`
	DefaultTarget   string   `json:"default_target"`
}

type AdminConfig struct {
	Enabled            bool     `json:"enabled"`
	ListenAddr         string   `json:"listen_addr"`
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

type DatabaseGatewayConfig struct {
	Enabled    bool   `json:"enabled"`
	ListenAddr string `json:"listen_addr"`
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
	ApiToken           string   `json:"api_token"`
	PublicKeys         []string `json:"public_keys"`
	AuthorizedKeysPath string   `json:"authorized_keys_path"`
}

type Target struct {
	ID                    string `json:"id"`
	HostID                string `json:"host_id"`
	Name                  string `json:"name"`
	Group                 string `json:"group"`
	Remark                string `json:"remark"`
	Disabled              bool   `json:"disabled"`
	ExpiresAt             string `json:"expires_at"`
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
	if !c.Recording.Enabled && !c.Recording.RecordCommands && !c.Recording.RecordInput {
		c.Recording.Enabled = true
		c.Recording.RecordCommands = true
	}
	if databaseEmpty {
		c.Database.Enabled = true
		c.Database.Driver = "sqlite"
		c.Database.DSN = "data/bastion.db"
		c.Database.AutoMigrate = true
	}
	if !c.DatabaseGateway.Enabled && c.DatabaseGateway.ListenAddr == "" {
		c.DatabaseGateway.Enabled = true
		c.DatabaseGateway.ListenAddr = "0.0.0.0:33060"
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
	if c.DatabaseGateway.Enabled {
		if c.DatabaseGateway.ListenAddr != "" {
			if _, _, err := net.SplitHostPort(c.DatabaseGateway.ListenAddr); err != nil {
				return fmt.Errorf("invalid database_gateway.listen_addr %q: %w", c.DatabaseGateway.ListenAddr, err)
			}
		}
	}
	// Users may be empty — the setup wizard creates the first admin user.
	if len(c.Users) == 0 {
		// No hard error; admin user is created via the setup wizard at /api/init/setup
	}
	return nil
}

func (t Target) Addr() string {
	return net.JoinHostPort(t.Host, fmt.Sprintf("%d", t.Port))
}
