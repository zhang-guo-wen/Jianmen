package dbproxy

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

const mysqlAuthSaltAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateMySQLAuthSalt(size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("mysql auth salt size must be positive")
	}

	salt := make([]byte, size)
	random := make([]byte, size*2)
	limit := byte(256 - 256%len(mysqlAuthSaltAlphabet))
	for filled := 0; filled < size; {
		if _, err := rand.Read(random); err != nil {
			return nil, fmt.Errorf("read random bytes: %w", err)
		}
		for _, value := range random {
			if value >= limit {
				continue
			}
			salt[filled] = mysqlAuthSaltAlphabet[int(value)%len(mysqlAuthSaltAlphabet)]
			filled++
			if filled == size {
				break
			}
		}
	}
	return salt, nil
}

// sendFakeMySQLHandshake 向客户端发送一个伪装 MySQL 8.0 握手包，
// 让客户端先发送 login 包（包含用户名），以便网关解析账号。
func sendFakeMySQLHandshake(conn net.Conn) ([]byte, error) {
	return sendFakeMySQLHandshakeWithTLS(conn, false)
}

func sendFakeMySQLHandshakeWithTLS(conn net.Conn, tlsEnabled bool) ([]byte, error) {
	// 生成 20 字节随机 salt
	salt, err := generateMySQLAuthSalt(20)
	if err != nil {
		return nil, err
	}
	capFlags := uint32(mysqlClientProtocol41 | mysqlClientSecureConnection | mysqlClientPluginAuth)
	if tlsEnabled {
		capFlags |= mysqlClientSSL
	}
	serverVersion := "8.0.28"
	authPluginName := "mysql_native_password"

	// 构建 HandshakeV10 payload
	var p []byte
	p = append(p, 10) // protocol version
	p = append(p, []byte(serverVersion)...)
	p = append(p, 0) // null terminator
	connID := make([]byte, 4)
	binary.LittleEndian.PutUint32(connID, uint32(salt[0])|uint32(salt[1])<<8|uint32(salt[2])<<16|uint32(salt[3])<<24)
	p = append(p, connID...)
	p = append(p, salt[:8]...) // auth data part 1 (8 bytes)
	p = append(p, 0)           // filler
	capLower := make([]byte, 2)
	binary.LittleEndian.PutUint16(capLower, uint16(capFlags&0xFFFF))
	p = append(p, capLower...)
	p = append(p, 33)         // utf8mb4
	status := make([]byte, 2) // status flags (2 bytes zeros)
	p = append(p, status...)
	capUpper := make([]byte, 2)
	binary.LittleEndian.PutUint16(capUpper, uint16(capFlags>>16))
	p = append(p, capUpper...)
	p = append(p, 21)                  // auth plugin data length (8 + 12 + 1 null)
	p = append(p, make([]byte, 10)...) // reserved
	p = append(p, salt[8:20]...)       // auth data part 2 (12 bytes)
	p = append(p, 0)                   // terminating NUL for auth data
	p = append(p, []byte(authPluginName)...)
	p = append(p, 0) // null terminator

	// 包装为 MySQL packet: 3 字节 length + 1 字节 seq(0)
	pkt := make([]byte, 4+len(p))
	pkt[0] = byte(len(p))
	pkt[1] = byte(len(p) >> 8)
	pkt[2] = byte(len(p) >> 16)
	pkt[3] = 0 // seq=0
	copy(pkt[4:], p)

	_, err = conn.Write(pkt)
	return salt, err
}

// handleMySQL implements MySQL proxy authentication:
// 1. Send fake handshake to client (so client sends login with username)
// 2. Read client's initial login packet
// 3. Extract username using MySQLLoginParser
// 3. Look up DatabaseAccount by unique name
// 4. RBAC check
// 5. Check account disabled/expiry
// 6. Connect to upstream MySQL
// 7. Read upstream handshake, parse with ParseMySQLHandshake
// 8. Forward handshake to client
// 9. Read client's auth response
// 10. Build upstream login with upstream credentials (mysql_native_password)
// 11. Send to upstream, read auth result
// 12. Check auth OK (0x00) or ERR (0xff), forward result to client
// 13. Return gatewayConn for data relay
func (g *Gateway) handleMySQL(ctx context.Context, client net.Conn) *gatewayConn {
	return g.handleMySQLWithListener(ctx, client, config.DatabaseProtocolListener{})
}

func (g *Gateway) handleMySQLWithListener(ctx context.Context, client net.Conn, listener config.DatabaseProtocolListener) *gatewayConn {
	var tlsConfig *tls.Config
	if listener.CertFile != "" && listener.KeyFile != "" {
		loaded, err := databaseListenerTLSConfig(listener)
		if err != nil {
			g.logger.Error("load MySQL listener certificate", "error", err)
			return nil
		}
		tlsConfig = loaded
	}
	// 发送伪装 handshake，让 MySQL 客户端先发 login 包
	fakeSalt, err := sendFakeMySQLHandshakeWithTLS(client, tlsConfig != nil)
	if err != nil {
		g.logger.Warn("mysql gateway failed to send fake handshake")
		return nil
	}

	// Read initial login packet from client
	clientLoginPkt, err := readMySQLPacket(client)
	if err != nil {
		g.logger.Warn("mysql gateway failed to read initial packet")
		return nil
	}
	if tlsConfig != nil && !isMySQLTLSRequest(clientLoginPkt) {
		g.logger.Warn("mysql gateway rejected plaintext login on TLS listener")
		_ = writeMySQLClientAuthError(client, mysqlClientAuthResponseSequence(clientLoginPkt.seq))
		return nil
	}
	if isMySQLTLSRequest(clientLoginPkt) {
		if tlsConfig == nil {
			g.logger.Warn("mysql client requested TLS but listener has no certificate")
			return nil
		}
		secured := tls.Server(client, tlsConfig)
		if err := secured.HandshakeContext(ctx); err != nil {
			g.logger.Warn("mysql gateway TLS handshake failed")
			return nil
		}
		client = secured
		clientLoginPkt, err = readMySQLPacket(client)
		if err != nil {
			g.logger.Warn("mysql gateway failed to read TLS login packet")
			return nil
		}
	}
	clientAuthSeq := mysqlClientAuthResponseSequence(clientLoginPkt.seq)

	// Parse username
	parser := &MySQLLoginParser{}
	fullPacket := make([]byte, 4+len(clientLoginPkt.payload))
	fullPacket[0] = byte(len(clientLoginPkt.payload))
	fullPacket[1] = byte(len(clientLoginPkt.payload) >> 8)
	fullPacket[2] = byte(len(clientLoginPkt.payload) >> 16)
	fullPacket[3] = clientLoginPkt.seq
	copy(fullPacket[4:], clientLoginPkt.payload)
	obs, _, err := parser.Observe(fullPacket)
	if err != nil {
		g.logger.Warn("mysql gateway failed to parse login")
		return nil
	}
	if obs.User == "" {
		g.logger.Warn("mysql gateway empty username in login")
		return nil
	}
	authResponse, err := mysqlLoginAuthResponse(clientLoginPkt.payload)
	if err != nil {
		g.logger.Warn("mysql gateway failed to parse authentication response")
		return nil
	}

	resolved, err := g.resolveAccount(ctx, obs.User)
	if err != nil {
		g.logger.Warn("mysql gateway account resolution failed")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		return nil
	}
	acct := resolved.account
	if err := validateResolvedAccountProtocol(resolved, databaseProtocolMySQL); err != nil {
		g.logger.Warn("mysql gateway rejected cross-protocol account")
		if writeErr := writeMySQLClientAuthError(client, clientAuthSeq); writeErr != nil {
			g.logger.Warn("mysql gateway failed to send protocol rejection")
		}
		return nil
	}
	if err := g.authenticateMySQLConnection(ctx, resolved, fakeSalt, authResponse); err != nil {
		g.logger.Warn("mysql gateway authentication or authorization failed")
		if errors.Is(err, errDatabaseAuthentication) {
			if err := writeMySQLClientAuthError(client, clientAuthSeq); err != nil {
				g.logger.Warn("mysql gateway failed to send auth error")
			}
		}
		return nil
	}

	rbacUserID := resolved.user.ID

	// Check account disabled and expiry
	if acct.Status == "disabled" {
		g.logger.Warn("mysql gateway account disabled")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		return nil
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		g.logger.Warn("mysql gateway account expired")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		return nil
	}

	// Connect to upstream and negotiate its configured TLS policy before sending credentials.
	upstream, hs, err := dialMySQLUpstream(ctx, acct.Instance)
	if err != nil {
		g.logger.Warn("mysql gateway upstream connect failed")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		return nil
	}

	// 直接用存储凭据构建上游登录，不要求客户端二次认证
	upstreamLoginSequence := byte(1)
	if dbtls.IsVerified(upstream) {
		upstreamLoginSequence = 2
	}
	upstreamLogin, err := BuildMySQLUpstreamLogin(hs, acct.Username, acct.Password.GetPlaintext(), hs.AuthPluginName, upstreamLoginSequence)
	if err != nil {
		g.logger.Warn("mysql gateway failed to build upstream login")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		upstream.Close()
		return nil
	}
	if _, err := upstream.Write(upstreamLogin); err != nil {
		g.logger.Warn("mysql gateway failed to send upstream login")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		upstream.Close()
		return nil
	}

	// Read upstream auth result
	authPkt, err := readMySQLPacket(upstream)
	if err != nil {
		g.logger.Warn("mysql gateway failed to read upstream auth result")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		upstream.Close()
		return nil
	}

	// Check auth result
	if len(authPkt.payload) > 0 && authPkt.payload[0] == 0xff {
		g.logger.Warn("mysql gateway upstream auth failed")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		upstream.Close()
		return nil
	}

	// Handle AuthSwitchRequest (0xfe) — MySQL 8.0 可能要求切换 auth plugin
	if len(authPkt.payload) > 1 && authPkt.payload[0] == 0xfe {
		switched, err := g.handleMySQLAuthSwitch(upstream, acct, hs, authPkt)
		if err != nil {
			g.logger.Warn("mysql gateway auth switch failed")
			_ = writeMySQLClientAuthError(client, clientAuthSeq)
			upstream.Close()
			return nil
		}
		authPkt = switched
	}

	// Handle caching_sha2_password full auth: 0x01 (more data) + 0x03 (fast auth success)
	if len(authPkt.payload) > 0 && authPkt.payload[0] == 0x01 {
		extraPkt := authPkt
		if len(authPkt.payload) == 1 {
			var err error
			extraPkt, err = readMySQLPacket(upstream)
			if err != nil {
				g.logger.Warn("mysql gateway failed to read auth continuation")
				_ = writeMySQLClientAuthError(client, clientAuthSeq)
				upstream.Close()
				return nil
			}
		}
		moreCode, ok := MySQLCachingSHA2AuthMoreData(extraPkt.payload)
		if !ok {
			g.logger.Warn("mysql gateway invalid caching_sha2_password more-data packet")
			_ = writeMySQLClientAuthError(client, clientAuthSeq)
			upstream.Close()
			return nil
		}
		if moreCode == 0x03 {
			finalPkt, err := readMySQLPacket(upstream)
			if err != nil {
				g.logger.Warn("mysql gateway failed to read final auth ok")
				_ = writeMySQLClientAuthError(client, clientAuthSeq)
				upstream.Close()
				return nil
			}
			authPkt = finalPkt
		}
		// 0x04 = full auth with public key — 实现完整认证
		if moreCode == 0x04 {
			fullAuthPkt, err := g.handleMySQLCachingSha2FullAuth(upstream, acct.Password.GetPlaintext(), extraPkt.seq)
			if err != nil {
				g.logger.Warn("mysql gateway caching_sha2_password full auth failed")
				_ = writeMySQLClientAuthError(client, clientAuthSeq)
				upstream.Close()
				return nil
			}
			authPkt = fullAuthPkt
		}
	}

	if len(authPkt.payload) > 0 && authPkt.payload[0] == 0xff {
		g.logger.Warn("mysql gateway upstream auth failed")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		upstream.Close()
		return nil
	}
	if len(authPkt.payload) == 0 || authPkt.payload[0] != 0x00 {
		g.logger.Warn("mysql gateway unexpected upstream auth result")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		upstream.Close()
		return nil
	}
	if err := upstream.SetDeadline(time.Time{}); err != nil {
		g.logger.Warn("mysql gateway failed to clear upstream authentication deadline")
		_ = writeMySQLClientAuthError(client, clientAuthSeq)
		upstream.Close()
		return nil
	}

	if err := writeMySQLClientAuthOK(client, clientAuthSeq); err != nil {
		upstream.Close()
		return nil
	}

	return &gatewayConn{
		client:        client,
		protocol:      "mysql",
		accountID:     acct.ID,
		instanceID:    acct.InstanceID,
		accountName:   resolved.rawName,
		upstream:      upstream,
		upstreamAddr:  upstreamAddress(acct.Instance),
		userID:        rbacUserID,
		accountUser:   acct.Username,
		instanceName:  acct.Instance.Name,
		userSessionID: resolved.userSessionID,
	}
}

// BuildMySQLAuthResponse 计算指定 auth plugin 的认证响应（仅 auth bytes，不含完整 login 包）
func BuildMySQLAuthResponse(plugin, password string, salt []byte) []byte {
	switch plugin {
	case "mysql_native_password":
		return BuildMySQLNativePassword(password, salt)
	case "caching_sha2_password":
		return BuildMySQLCachingSha2Password(password, salt)
	default:
		return nil
	}
}

// handleMySQLAuthSwitch 处理 MySQL AuthSwitchRequest (0xfe)
// payload 是 0xfe 之后的部分：plugin name (null-terminated) + auth data
// AuthSwitch 响应只需发送 raw auth response（不是完整 login 包）
func (g *Gateway) handleMySQLAuthSwitch(upstream net.Conn, acct *model.DatabaseAccount, _ *MySQLHandshake, request *mysqlPacket) (*mysqlPacket, error) {
	if request == nil || len(request.payload) < 2 || request.payload[0] != 0xfe {
		return nil, errors.New("malformed MySQL authentication switch request")
	}
	newPlugin, authData, err := parseMySQLAuthSwitch(request.payload[1:])
	if err != nil {
		return nil, err
	}

	// 构建 raw auth response
	authResp := BuildMySQLAuthResponse(newPlugin, acct.Password.GetPlaintext(), authData)
	if authResp == nil {
		return nil, fmt.Errorf("unsupported auth switch plugin: %s", newPlugin)
	}

	// 发送 auth switch 响应：仅 auth response bytes，seq=3
	if _, err := upstream.Write(mysqlPacketWithSeq(mysqlResponseSequence(request.seq), authResp)); err != nil {
		return nil, fmt.Errorf("write auth switch: %w", err)
	}

	return readMySQLPacket(upstream)
}

// handleMySQLCachingSha2FullAuth sends the NUL-terminated password only after
// the configured TLS verification policy succeeds. The response sequence is
// derived from the server's AuthMoreData packet.
func (g *Gateway) handleMySQLCachingSha2FullAuth(upstream net.Conn, password string, serverSequences ...byte) (*mysqlPacket, error) {
	if err := requireVerifiedMySQLTLS(upstream); err != nil {
		return nil, err
	}
	if len(serverSequences) != 1 {
		return nil, errors.New("mysql full authentication requires the server packet sequence")
	}
	g.observeMySQLAuthEvent("caching_sha2_full_auth")
	passwordPayload := append([]byte(password), 0)
	if _, err := upstream.Write(mysqlPacketWithSeq(mysqlResponseSequence(serverSequences[0]), passwordPayload)); err != nil {
		return nil, fmt.Errorf("send TLS-protected full-auth password: %w", err)
	}
	return readMySQLPacket(upstream)

}

func (g *Gateway) observeMySQLAuthEvent(event string) {
	if g == nil || g.logger == nil {
		return
	}
	g.logger.Debug("mysql upstream authentication event", "event", event)
}

func requireVerifiedMySQLTLS(conn net.Conn) error {
	if !dbtls.IsVerified(conn) {
		return errVerifiedTLSRequired
	}
	return nil
}

// MySQLCachingSHA2AuthMoreData recognizes the standard complete payload and
// the continuation packet emitted by peers that split the more-data response.
func MySQLCachingSHA2AuthMoreData(payload []byte) (byte, bool) {
	if len(payload) == 2 && payload[0] == 0x01 && (payload[1] == 0x03 || payload[1] == 0x04) {
		return payload[1], true
	}
	if len(payload) == 1 && (payload[0] == 0x03 || payload[0] == 0x04) {
		return payload[0], true
	}
	return 0, false
}

func mysqlResponseSequence(serverSequence byte) byte {
	return serverSequence + 1
}
