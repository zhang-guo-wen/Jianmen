package service

import "testing"

func TestParseApplicationAddress(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		want      ApplicationAddress
		wantError bool
	}{
		{
			name:    "Nacos address with fragment route",
			address: "http://47.121.184.68:18848/nacos/#/login?namespace=&pageSize=&pageNo=",
			want: ApplicationAddress{
				Address:   "http://47.121.184.68:18848/nacos/#/login?namespace=&pageSize=&pageNo=",
				EntryPath: "/nacos/#/login?namespace=&pageSize=&pageNo=",
				Scheme:    "http",
				Host:      "47.121.184.68",
				Port:      18848,
			},
		},
		{
			name:    "HTTPS default port and root path",
			address: "https://console.example.com",
			want: ApplicationAddress{
				Address:   "https://console.example.com/",
				EntryPath: "/",
				Scheme:    "https",
				Host:      "console.example.com",
				Port:      443,
			},
		},
		{name: "missing scheme", address: "console.example.com/path", wantError: true},
		{name: "unsupported scheme", address: "ftp://console.example.com/path", wantError: true},
		{name: "credentials rejected", address: "http://user:pass@console.example.com/", wantError: true},
		{name: "invalid port", address: "http://console.example.com:70000/", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseApplicationAddress(tt.address)
			if tt.wantError {
				if err == nil {
					t.Fatalf("ParseApplicationAddress(%q) succeeded: %#v", tt.address, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseApplicationAddress(%q): %v", tt.address, err)
			}
			if got != tt.want {
				t.Fatalf("ParseApplicationAddress(%q) = %#v, want %#v", tt.address, got, tt.want)
			}
		})
	}
}
