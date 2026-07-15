package admin

import (
	"errors"
	"testing"
)

func TestFriendlySSHError(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "unexpected auth failure message",
			raw:  "ssh: handshake failed: ssh: unexpected message type 51 (expected 60)",
			want: "认证失败，请检查登录账号、密码、私钥或私钥口令是否正确",
		},
		{
			name: "standard auth failure",
			raw:  "ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password]",
			want: "认证失败，请检查登录账号、密码、私钥或私钥口令是否正确",
		},
		{
			name: "connection timeout",
			raw:  "dial tcp 10.0.0.1:22: i/o timeout",
			want: "连接超时，请检查主机地址、端口、防火墙和网络连通性",
		},
		{
			name: "connection refused",
			raw:  "dial tcp 127.0.0.1:22: connectex: No connection could be made because the target machine actively refused it",
			want: "连接被拒绝，请检查端口是否正确以及 SSH 服务是否已启动",
		},
		{
			name: "dns failure",
			raw:  "dial tcp: lookup missing.example: no such host",
			want: "无法解析主机地址，请检查 IP 或域名是否正确",
		},
		{
			name: "connection reset",
			raw:  "ssh: handshake failed: read tcp: connection reset by peer",
			want: "SSH 握手被远端中断，请检查端口是否为 SSH 服务以及服务端是否允许连接",
		},
		{
			name: "algorithm mismatch",
			raw:  "ssh: handshake failed: ssh: no common algorithm for host key",
			want: "SSH 算法不兼容，请检查目标服务器的密钥交换、主机密钥或加密算法配置",
		},
		{
			name: "generic protocol error",
			raw:  "ssh: handshake failed: ssh: unexpected message type 2 (expected 20)",
			want: "SSH 握手协议异常，请确认连接端口是 SSH 服务，并检查服务端兼容性",
		},
		{
			name: "unknown error preserved",
			raw:  "custom ssh failure",
			want: "custom ssh failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := friendlySSHError(errors.New(tt.raw)); got != tt.want {
				t.Fatalf("friendlySSHError() = %q, want %q", got, tt.want)
			}
		})
	}
}
