package store

import (
	"context"
	"testing"

	"jianmen/internal/model"
)

func TestHostAndTargetViewsExposeRawLifecycleWhileTargetStatusFailsClosed(t *testing.T) {
	tests := []struct {
		status           string
		wantTargetStatus string
	}{
		{status: "active", wantTargetStatus: "enabled"},
		{status: "enabled", wantTargetStatus: "enabled"},
		{status: "disabled", wantTargetStatus: "disabled"},
		{status: "pending", wantTargetStatus: "disabled"},
		{status: "revoked", wantTargetStatus: "disabled"},
		{status: "", wantTargetStatus: "disabled"},
		{status: "unknown", wantTargetStatus: "disabled"},
	}
	repository := &DBStore{}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			host := model.Host{
				ID: "host", Address: "127.0.0.1", Port: 22, Protocol: "ssh", Status: test.status,
			}
			hostView := repository.hostView(context.Background(), host)
			if hostView.LifecycleStatus != test.status {
				t.Fatalf("host lifecycle status = %q, want %q", hostView.LifecycleStatus, test.status)
			}
			account := model.HostAccount{
				ID: "account", HostID: host.ID, Username: "root", Status: test.status, Host: host,
			}
			targetView := repository.targetView(context.Background(), nil, account)
			if targetView.LifecycleStatus != test.status {
				t.Fatalf("target lifecycle status = %q, want %q", targetView.LifecycleStatus, test.status)
			}
			if targetView.Status != test.wantTargetStatus {
				t.Fatalf("target status = %q, want %q", targetView.Status, test.wantTargetStatus)
			}
		})
	}
}

func TestTargetViewKeepsStrictNormalizedProtocol(t *testing.T) {
	tests := []struct {
		protocol string
		want     string
	}{
		{protocol: " SSH ", want: "ssh"},
		{protocol: "RDP", want: "rdp"},
		{protocol: "", want: ""},
		{protocol: "TELNET", want: "telnet"},
	}
	repository := &DBStore{}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			account := model.HostAccount{
				ID: "account", HostID: "host", Username: "root", Status: "active",
				Host: model.Host{
					ID: "host", Address: "127.0.0.1", Port: 22,
					Protocol: test.protocol, Status: "active",
				},
			}
			if got := repository.targetView(context.Background(), nil, account).Protocol; got != test.want {
				t.Fatalf("target protocol = %q, want %q", got, test.want)
			}
		})
	}
}
