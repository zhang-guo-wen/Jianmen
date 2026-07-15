package admin

import "testing"

func TestParseListenAddrDoesNotAdvertiseWildcardAsLoopback(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		wantHost string
		wantPort int
	}{
		{name: "empty", addr: "", wantHost: "", wantPort: 33060},
		{name: "ipv4 wildcard", addr: "0.0.0.0:33060", wantHost: "", wantPort: 33060},
		{name: "ipv6 wildcard", addr: "[::]:33060", wantHost: "", wantPort: 33060},
		{name: "loopback", addr: "127.0.0.1:33061", wantHost: "127.0.0.1", wantPort: 33061},
		{name: "hostname", addr: "db.example.com:33062", wantHost: "db.example.com", wantPort: 33062},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := parseListenAddr(tt.addr)
			if host != tt.wantHost || port != tt.wantPort {
				t.Fatalf("parseListenAddr(%q) = %q:%d, want %q:%d", tt.addr, host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}
