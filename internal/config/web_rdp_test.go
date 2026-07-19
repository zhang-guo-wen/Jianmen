package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebRDPDefaults(t *testing.T) {
	cfg := defaultConfig()

	if cfg.WebRDP.Enabled {
		t.Fatal("Web RDP must be disabled by default")
	}
	if cfg.WebRDP.GuacdAddress != defaultGuacdAddress {
		t.Fatalf("GuacdAddress = %q, want %q", cfg.WebRDP.GuacdAddress, defaultGuacdAddress)
	}
	if cfg.WebRDP.ConnectTimeoutSecs != 15 {
		t.Fatalf("ConnectTimeoutSecs = %d, want 15", cfg.WebRDP.ConnectTimeoutSecs)
	}
	if cfg.WebRDP.SpoolDir != defaultRDPSpoolDir ||
		cfg.WebRDP.GuacdRecordingRoot != defaultRDPSpoolDir {
		t.Fatalf(
			"recording paths = (%q, %q), want both %q",
			cfg.WebRDP.SpoolDir,
			cfg.WebRDP.GuacdRecordingRoot,
			defaultRDPSpoolDir,
		)
	}
	if cfg.WebRDP.LocalDriveRoot != defaultRDPDriveDir ||
		cfg.WebRDP.GuacdDriveRoot != defaultRDPDriveDir {
		t.Fatalf(
			"drive paths = (%q, %q), want both %q",
			cfg.WebRDP.LocalDriveRoot,
			cfg.WebRDP.GuacdDriveRoot,
			defaultRDPDriveDir,
		)
	}
	if cfg.ObjectStorage.Provider != "filesystem" {
		t.Fatalf("ObjectStorage.Provider = %q, want filesystem", cfg.ObjectStorage.Provider)
	}
	if cfg.ObjectStorage.LocalDir != defaultObjectStorageDir {
		t.Fatalf("ObjectStorage.LocalDir = %q, want %q", cfg.ObjectStorage.LocalDir, defaultObjectStorageDir)
	}
}

func TestWebRDPDefaultsNormalizeConfiguredValues(t *testing.T) {
	cfg := &Config{
		WebRDP: WebRDPConfig{
			GuacdAddress:   " guacd:4822 ",
			SpoolDir:       " /recordings ",
			LocalDriveRoot: " /drive ",
		},
		ObjectStorage: ObjectStorageConfig{
			Provider: " S3 ",
			Prefix:   " /production/rdp/ ",
		},
	}

	cfg.applyWebRDPDefaults()

	if cfg.WebRDP.GuacdAddress != "guacd:4822" {
		t.Fatalf("GuacdAddress = %q, want guacd:4822", cfg.WebRDP.GuacdAddress)
	}
	if cfg.WebRDP.GuacdRecordingRoot != "/recordings" {
		t.Fatalf("GuacdRecordingRoot = %q, want /recordings", cfg.WebRDP.GuacdRecordingRoot)
	}
	if cfg.WebRDP.GuacdDriveRoot != "/drive" {
		t.Fatalf("GuacdDriveRoot = %q, want /drive", cfg.WebRDP.GuacdDriveRoot)
	}
	if cfg.ObjectStorage.Provider != "s3" {
		t.Fatalf("ObjectStorage.Provider = %q, want s3", cfg.ObjectStorage.Provider)
	}
	if cfg.ObjectStorage.Prefix != "production/rdp" {
		t.Fatalf("ObjectStorage.Prefix = %q, want production/rdp", cfg.ObjectStorage.Prefix)
	}
}

func TestLoadObjectStorageSecureDefaultRespectsFieldPresence(t *testing.T) {
	tests := []struct {
		name       string
		configJSON string
		wantSecure bool
	}{
		{
			name: "S3 secure defaults to enabled",
			configJSON: `{"object_storage":{
				"provider":"s3",
				"endpoint":"s3.internal.example:9000",
				"access_key_id":"access-key",
				"secret_access_key":"secret-key",
				"bucket":"recordings"
			}}`,
			wantSecure: true,
		},
		{
			name: "S3 explicit insecure transport is retained",
			configJSON: `{"object_storage":{
				"provider":"s3",
				"endpoint":"s3.internal.example:9000",
				"access_key_id":"access-key",
				"secret_access_key":"secret-key",
				"bucket":"recordings",
				"secure":false
			}}`,
			wantSecure: false,
		},
		{
			name:       "filesystem omitted secure remains disabled",
			configJSON: `{"object_storage":{"provider":"filesystem","local_dir":"data/objects"}}`,
			wantSecure: false,
		},
		{
			name:       "filesystem explicit secure value is retained",
			configJSON: `{"object_storage":{"provider":"filesystem","local_dir":"data/objects","secure":true}}`,
			wantSecure: true,
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
			if cfg.ObjectStorage.Secure != tt.wantSecure {
				t.Fatalf("ObjectStorage.Secure = %t, want %t", cfg.ObjectStorage.Secure, tt.wantSecure)
			}
		})
	}
}

func TestLoadRejectsUnknownObjectStorageField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"object_storage":{"unexpected":true}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `unknown field "unexpected"`) {
		t.Fatalf("Load() error = %v, want unknown object storage field", err)
	}
}

func TestWebRDPValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "filesystem configuration is valid"},
		{
			name: "guacd address requires port",
			mutate: func(cfg *Config) {
				cfg.WebRDP.GuacdAddress = "guacd"
			},
			wantErr: "guacd_address",
		},
		{
			name: "negative timeout is rejected",
			mutate: func(cfg *Config) {
				cfg.WebRDP.ConnectTimeoutSecs = -1
			},
			wantErr: "connect_timeout_seconds",
		},
		{
			name: "excessive timeout is rejected",
			mutate: func(cfg *Config) {
				cfg.WebRDP.ConnectTimeoutSecs = 301
			},
			wantErr: "connect_timeout_seconds",
		},
		{
			name: "recording path is required",
			mutate: func(cfg *Config) {
				cfg.WebRDP.SpoolDir = "."
			},
			wantErr: "web_rdp.spool_dir",
		},
		{
			name: "drive path is required",
			mutate: func(cfg *Config) {
				cfg.WebRDP.GuacdDriveRoot = " "
			},
			wantErr: "web_rdp.guacd_drive_root",
		},
		{
			name: "filesystem directory is required",
			mutate: func(cfg *Config) {
				cfg.ObjectStorage.LocalDir = ""
			},
			wantErr: "object_storage.local_dir",
		},
		{
			name: "filesystem current directory is rejected",
			mutate: func(cfg *Config) {
				cfg.ObjectStorage.LocalDir = "."
			},
			wantErr: "object_storage.local_dir",
		},
		{
			name: "unknown object storage provider is rejected",
			mutate: func(cfg *Config) {
				cfg.ObjectStorage.Provider = "ftp"
			},
			wantErr: "not supported",
		},
		{
			name: "S3 endpoint is required",
			mutate: func(cfg *Config) {
				cfg.ObjectStorage = validS3Config()
				cfg.ObjectStorage.Endpoint = ""
			},
			wantErr: "object_storage.endpoint",
		},
		{
			name: "S3 endpoint must not include URL scheme",
			mutate: func(cfg *Config) {
				cfg.ObjectStorage = validS3Config()
				cfg.ObjectStorage.Endpoint = "https://s3.internal.example:9000"
			},
			wantErr: "without scheme or path",
		},
		{
			name: "S3 endpoint must not include path",
			mutate: func(cfg *Config) {
				cfg.ObjectStorage = validS3Config()
				cfg.ObjectStorage.Endpoint = "s3.internal.example:9000/api"
			},
			wantErr: "without scheme or path",
		},
		{
			name: "S3 bucket is required",
			mutate: func(cfg *Config) {
				cfg.ObjectStorage = validS3Config()
				cfg.ObjectStorage.Bucket = ""
			},
			wantErr: "object_storage.bucket",
		},
		{
			name: "S3 credentials are required",
			mutate: func(cfg *Config) {
				cfg.ObjectStorage = validS3Config()
				cfg.ObjectStorage.SecretAccessKey = ""
			},
			wantErr: "access_key_id and secret_access_key",
		},
		{
			name: "S3 configuration is valid",
			mutate: func(cfg *Config) {
				cfg.ObjectStorage = validS3Config()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.WebRDP.Enabled = true
			if tt.mutate != nil {
				tt.mutate(cfg)
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

func TestLoadRejectsNegativeWebRDPTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{
		"web_rdp": {
			"enabled": true,
			"connect_timeout_seconds": -1
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "connect_timeout_seconds") {
		t.Fatalf("Load() error = %v, want invalid connect_timeout_seconds", err)
	}
}

func TestDisabledWebRDPStillValidatesAuditObjectStorage(t *testing.T) {
	cfg := defaultConfig()
	cfg.WebRDP.Enabled = false
	cfg.ObjectStorage.Provider = "unsupported"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "object_storage.provider") {
		t.Fatalf("Validate() error = %v, want invalid object storage provider", err)
	}
}

func TestConfigurationExamplesLoadAndValidate(t *testing.T) {
	examples := []string{
		"config.example.json",
		"config.docker.json",
		"config.docker.proxy.example.json",
		"config.docker.web-rdp.example.json",
	}

	for _, name := range examples {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", name)
			if _, err := Load(path); err != nil {
				t.Fatalf("load %s: %v", name, err)
			}
		})
	}
}

func validS3Config() ObjectStorageConfig {
	return ObjectStorageConfig{
		Provider:        "s3",
		Endpoint:        "s3.internal.example:9000",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Bucket:          "jianmen-recordings",
		Secure:          true,
	}
}
