package config

import (
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
