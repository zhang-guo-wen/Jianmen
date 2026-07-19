package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAdminPublicURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "empty"},
		{name: "HTTP origin", url: "http://gateway.example.com:47100"},
		{name: "HTTPS origin", url: "https://gateway.example.com"},
		{name: "unsupported scheme", url: "javascript:alert(1)", wantErr: "scheme"},
		{name: "missing host", url: "http:///login", wantErr: "host"},
		{name: "path rejected", url: "https://gateway.example.com/login", wantErr: "path"},
		{name: "query rejected", url: "https://gateway.example.com?x=1", wantErr: "query"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePublicURL(tt.url)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validatePublicURL(%q): %v", tt.url, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validatePublicURL(%q) error = %v, want containing %q", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestAdminTLSValidation(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		publicURL  string
		tls        AdminTLSConfig
		wantErr    string
	}{
		{
			name:       "loopback HTTP is allowed for local development",
			listenAddr: "127.0.0.1:47100",
		},
		{
			name:       "non-loopback HTTP requires explicit override",
			listenAddr: "0.0.0.0:47100",
			wantErr:    "insecure HTTP",
		},
		{
			name:       "explicit insecure HTTP override",
			listenAddr: "0.0.0.0:47100",
			tls:        AdminTLSConfig{AllowInsecureHTTP: true},
		},
		{
			name:       "certificate and key must be provided together",
			listenAddr: "127.0.0.1:47100",
			tls:        AdminTLSConfig{CertFile: "admin.crt"},
			wantErr:    "cert_file and key_file",
		},
		{
			name:       "key and certificate must be provided together",
			listenAddr: "127.0.0.1:47100",
			tls:        AdminTLSConfig{KeyFile: "admin.key"},
			wantErr:    "cert_file and key_file",
		},
		{
			name:       "HTTPS public URL can use loopback HTTP",
			listenAddr: "127.0.0.1:47100",
			publicURL:  "https://localhost.example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ListenAddr: "127.0.0.1:47102",
				Admin: AdminConfig{
					Enabled:    true,
					ListenAddr: tt.listenAddr,
					PublicURL:  tt.publicURL,
					TLS:        tt.tls,
				},
			}
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDockerImageAdminTransportIsSecureByDefault(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config.docker.json"))
	if err != nil {
		t.Fatalf("read config.docker.json: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode config.docker.json: %v", err)
	}
	if cfg.Admin.TLS.AllowInsecureHTTP {
		t.Fatal("Docker image must not enable insecure Admin HTTP by default")
	}
	if strings.TrimSpace(cfg.Admin.TLS.CertFile) == "" || strings.TrimSpace(cfg.Admin.TLS.KeyFile) == "" {
		t.Fatal("Docker image must require a mounted Admin certificate and key by default")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("secure Docker configuration must validate: %v", err)
	}
	if cfg.Recording.RecordInput {
		t.Fatal("Docker image must not record raw terminal input by default")
	}
	if cfg.Recording.RetentionDays != 30 || cfg.Recording.MaxReplayBytes != 10*1024*1024*1024 || cfg.Recording.CleanupBatchSize != 100 {
		t.Fatalf("Docker audit governance defaults = %+v", cfg.Recording)
	}
}

func TestRecordingGovernanceDefaultsAndValidation(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.Recording.RetentionDays != 30 ||
		cfg.Recording.MaxReplayBytes != 10*1024*1024*1024 ||
		cfg.Recording.CleanupBatchSize != 100 {
		t.Fatalf("recording defaults = %+v", cfg.Recording)
	}
	if cfg.Recording.RecordInput {
		t.Fatal("raw terminal input must default to disabled")
	}

	tests := []struct {
		name      string
		recording RecordingConfig
		wantErr   string
	}{
		{name: "valid", recording: RecordingConfig{RetentionDays: 30, MaxReplayBytes: 1024, CleanupBatchSize: 10}},
		{name: "retention too large", recording: RecordingConfig{RetentionDays: 3651}, wantErr: "retention_days"},
		{name: "negative quota", recording: RecordingConfig{MaxReplayBytes: -1}, wantErr: "max_replay_bytes"},
		{name: "batch too large", recording: RecordingConfig{CleanupBatchSize: 1001}, wantErr: "cleanup_batch_size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := &Config{
				ListenAddr: "127.0.0.1:47102",
				Recording:  tt.recording,
			}
			err := candidate.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRecordingGovernanceDistinguishesMissingAndExplicitZeroQuota(t *testing.T) {
	var missing Config
	if err := json.Unmarshal([]byte(`{
		"listen_addr":"127.0.0.1:47102",
		"recording":{"enabled":true,"record_input":false,"record_commands":true}
	}`), &missing); err != nil {
		t.Fatalf("decode old configuration: %v", err)
	}
	missing.applyDefaults()
	if missing.Recording.MaxReplayBytes != 10*1024*1024*1024 {
		t.Fatalf("missing quota default = %d", missing.Recording.MaxReplayBytes)
	}

	var disabled Config
	if err := json.Unmarshal([]byte(`{
		"listen_addr":"127.0.0.1:47102",
		"recording":{
			"enabled":true,
			"record_input":false,
			"record_commands":true,
			"max_replay_bytes":0
		}
	}`), &disabled); err != nil {
		t.Fatalf("decode explicit zero quota: %v", err)
	}
	disabled.applyDefaults()
	if disabled.Recording.MaxReplayBytes != 0 {
		t.Fatalf("explicit zero quota = %d, want disabled", disabled.Recording.MaxReplayBytes)
	}
}

func TestDatabaseProtocolListenerValidation(t *testing.T) {
	tests := []struct {
		name    string
		gateway DatabaseGatewayConfig
		wantErr string
	}{
		{
			name: "unique protocol listeners are accepted",
			gateway: DatabaseGatewayConfig{Enabled: true,
				MySQL:      DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:33060"},
				PostgreSQL: DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:54330", CertFile: "pg.crt", KeyFile: "pg.key", CAFile: "pg-ca.crt", ServerName: "pg-gateway.example.test"},
				Redis:      DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:63790", CertFile: "redis.crt", KeyFile: "redis.key", ServerName: "redis-gateway.example.test"},
			},
		},
		{
			name: "listener addresses must be unique",
			gateway: DatabaseGatewayConfig{Enabled: true,
				MySQL:      DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:33060"},
				PostgreSQL: DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:33060", CertFile: "pg.crt", KeyFile: "pg.key", ServerName: "pg-gateway.example.test"},
			},
			wantErr: "must be unique",
		},
		{
			name: "listener TLS requires certificate and key",
			gateway: DatabaseGatewayConfig{Enabled: true,
				PostgreSQL: DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:54330", CertFile: "pg.crt"},
			},
			wantErr: "cert_file and key_file",
		},
		{
			name: "PostgreSQL listener requires TLS",
			gateway: DatabaseGatewayConfig{Enabled: true,
				PostgreSQL: DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:54330"},
			},
			wantErr: "postgresql requires TLS",
		},
		{
			name: "non-loopback MySQL listener requires TLS",
			gateway: DatabaseGatewayConfig{Enabled: true,
				MySQL: DatabaseProtocolListener{Enabled: true, Address: "0.0.0.0:33060"},
			},
			wantErr: "mysql requires TLS",
		},
		{
			name: "non-loopback Redis listener requires TLS",
			gateway: DatabaseGatewayConfig{Enabled: true,
				Redis: DatabaseProtocolListener{Enabled: true, Address: "[::]:63790"},
			},
			wantErr: "redis requires TLS",
		},
		{
			name: "TLS listener requires a client validation server name",
			gateway: DatabaseGatewayConfig{Enabled: true,
				PostgreSQL: DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:54330", CertFile: "pg.crt", KeyFile: "pg.key"},
			},
			wantErr: "server_name is required",
		},
		{
			name: "CA file requires TLS listener",
			gateway: DatabaseGatewayConfig{Enabled: true,
				MySQL: DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:33060", CAFile: "mysql-ca.crt"},
			},
			wantErr: "ca_file requires cert_file and key_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{ListenAddr: "127.0.0.1:47102", DatabaseGateway: tt.gateway}
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDatabaseGatewayServerNameValidation(t *testing.T) {
	tests := []struct {
		name       string
		serverName string
		wantErr    bool
	}{
		{name: "localhost", serverName: "localhost"},
		{name: "DNS name", serverName: "db-gateway.example.test"},
		{name: "IPv4", serverName: "127.0.0.1"},
		{name: "IPv6", serverName: "2001:db8::1"},
		{name: "leading whitespace", serverName: " db.example.test", wantErr: true},
		{name: "trailing whitespace", serverName: "db.example.test ", wantErr: true},
		{name: "quote", serverName: `db"gateway.example.test`, wantErr: true},
		{name: "shell substitution", serverName: "db$(id).example.test", wantErr: true},
		{name: "semicolon", serverName: "db;id.example.test", wantErr: true},
		{name: "underscore", serverName: "db_gateway.example.test", wantErr: true},
		{name: "leading hyphen", serverName: "-db.example.test", wantErr: true},
		{name: "trailing hyphen", serverName: "db-.example.test", wantErr: true},
		{name: "empty label", serverName: "db..example.test", wantErr: true},
		{name: "overlong label", serverName: strings.Repeat("a", 64) + ".example.test", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ListenAddr: "127.0.0.1:47102",
				DatabaseGateway: DatabaseGatewayConfig{
					Enabled: true,
					MySQL: DatabaseProtocolListener{
						Enabled: true, Address: "127.0.0.1:33060", CertFile: "gateway.crt", KeyFile: "gateway.key", ServerName: tt.serverName,
					},
				},
			}
			err := cfg.Validate()
			if tt.wantErr && (err == nil || !strings.Contains(err.Error(), "server_name")) {
				t.Fatalf("Validate() error = %v, want invalid server_name", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() rejected valid server_name %q: %v", tt.serverName, err)
			}
		})
	}
}

func TestDefaultDatabaseGatewayDoesNotEnableProtocolsThatNeedCertificates(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.DatabaseGateway.MySQL.Enabled {
		t.Fatal("default database gateway must keep the certificate-optional MySQL listener enabled")
	}
	if !isLoopbackListenAddr(cfg.DatabaseGateway.MySQL.Address) {
		t.Fatalf("default MySQL listener address = %q, want a loopback address", cfg.DatabaseGateway.MySQL.Address)
	}
	if cfg.DatabaseGateway.PostgreSQL.Enabled || cfg.DatabaseGateway.Redis.Enabled {
		t.Fatalf(
			"default database gateway enabled certificate-dependent listeners: postgresql=%t redis=%t",
			cfg.DatabaseGateway.PostgreSQL.Enabled,
			cfg.DatabaseGateway.Redis.Enabled,
		)
	}
}

func TestLoadDatabaseGatewayEnabledDefaultsRespectFieldPresence(t *testing.T) {
	tests := []struct {
		name          string
		configJSON    string
		wantEnabled   bool
		wantMySQL     bool
		wantPostgres  bool
		wantRedis     bool
		wantMySQLAddr string
	}{
		{
			name:          "field missing enables a loopback default mysql listener",
			configJSON:    `{}`,
			wantEnabled:   true,
			wantMySQL:     true,
			wantMySQLAddr: "127.0.0.1:33060",
		},
		{
			name:          "explicit true keeps configured listeners",
			configJSON:    `{"database_gateway":{"enabled":true,"mysql":{"enabled":true,"listen_addr":"127.0.0.1:33060"}}}`,
			wantEnabled:   true,
			wantMySQL:     true,
			wantMySQLAddr: "127.0.0.1:33060",
		},
		{
			name:        "explicit false disables gateway and every listener",
			configJSON:  `{"database_gateway":{"enabled":false}}`,
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tt.configJSON), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.DatabaseGateway.Enabled != tt.wantEnabled {
				t.Fatalf("DatabaseGateway.Enabled = %t, want %t", cfg.DatabaseGateway.Enabled, tt.wantEnabled)
			}
			if cfg.DatabaseGateway.MySQL.Enabled != tt.wantMySQL {
				t.Fatalf("MySQL.Enabled = %t, want %t", cfg.DatabaseGateway.MySQL.Enabled, tt.wantMySQL)
			}
			if cfg.DatabaseGateway.PostgreSQL.Enabled != tt.wantPostgres {
				t.Fatalf("PostgreSQL.Enabled = %t, want %t", cfg.DatabaseGateway.PostgreSQL.Enabled, tt.wantPostgres)
			}
			if cfg.DatabaseGateway.Redis.Enabled != tt.wantRedis {
				t.Fatalf("Redis.Enabled = %t, want %t", cfg.DatabaseGateway.Redis.Enabled, tt.wantRedis)
			}
			if cfg.DatabaseGateway.MySQL.Address != tt.wantMySQLAddr {
				t.Fatalf("MySQL.Address = %q, want %q", cfg.DatabaseGateway.MySQL.Address, tt.wantMySQLAddr)
			}
		})
	}
}

func TestDockerfileExposesEveryDefaultDatabaseGatewayPort(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	exposeLine := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "EXPOSE ") {
			exposeLine = line
			break
		}
	}
	for _, port := range []string{"33060", "54330", "63790"} {
		if !strings.Contains(exposeLine, port) {
			t.Fatalf("Dockerfile EXPOSE line %q does not include database gateway port %s", exposeLine, port)
		}
	}
}
