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
