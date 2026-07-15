package dbproxy

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net"
	"strings"
	"time"

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
	// 生成 20 字节随机 salt
	salt, err := generateMySQLAuthSalt(20)
	if err != nil {
		return nil, err
	}
	capFlags := uint32(mysqlClientProtocol41 | mysqlClientSecureConnection | mysqlClientPluginAuth)
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
	// 发送伪装 handshake，让 MySQL 客户端先发 login 包
	fakeSalt, err := sendFakeMySQLHandshake(client)
	if err != nil {
		g.logger.Warn("mysql gateway failed to send fake handshake", "error", err)
		return nil
	}

	// Read initial login packet from client
	clientLoginPkt, err := readMySQLPacket(client)
	if err != nil {
		g.logger.Warn("mysql gateway failed to read initial packet", "error", err)
		return nil
	}

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
		g.logger.Warn("mysql gateway failed to parse login", "error", err)
		return nil
	}
	if obs.User == "" {
		g.logger.Warn("mysql gateway empty username in login")
		return nil
	}
	authResponse, err := mysqlLoginAuthResponse(clientLoginPkt.payload)
	if err != nil {
		g.logger.Warn("mysql gateway failed to parse authentication response", "error", err)
		return nil
	}

	resolved, err := g.resolveAccount(obs.User)
	if err != nil {
		g.logger.Warn("mysql gateway account resolution failed", "username", obs.User, "error", err)
		return nil
	}
	acct := resolved.account
	if g.store == nil || g.store.AuthenticateMySQLConnectionPassword(ctx, resolved.user.ID, acct.ID, fakeSalt, authResponse) != nil {
		g.logger.Warn("mysql gateway auth failed", "user", resolved.rawName)
		if err := writeMySQLClientAuthError(client); err != nil {
			g.logger.Warn("mysql gateway failed to send auth error", "error", err)
		}
		return nil
	}

	// RBAC check
	rbacUserID := resolved.user.ID
	resourceID := acct.ID
	if err := g.authorizeConnect(rbacUserID, resolved.rawName, resourceID); err != nil {
		g.logger.Warn("mysql gateway rbac denied", "resource", resourceID, "error", err)
		return nil
	}

	// Check account disabled and expiry
	if acct.Status == "disabled" {
		g.logger.Warn("mysql gateway account disabled", "account", resolved.rawName)
		return nil
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		g.logger.Warn("mysql gateway account expired", "account", resolved.rawName, "expires_at", acct.ExpiresAt)
		return nil
	}

	// Connect to upstream
	upstream, err := net.DialTimeout("tcp", upstreamAddress(acct.Instance), 10*time.Second)
	if err != nil {
		g.logger.Warn("mysql gateway upstream connect failed", "upstream", upstreamAddress(acct.Instance), "error", err)
		return nil
	}

	// Read upstream handshake — 不转发给客户端（客户端已经响应了 fake handshake）
	hsPkt, err := readMySQLPacket(upstream)
	if err != nil {
		g.logger.Warn("mysql gateway failed to read upstream handshake", "error", err)
		upstream.Close()
		return nil
	}
	hs, err := ParseMySQLHandshake(hsPkt.payload)
	if err != nil {
		g.logger.Warn("mysql gateway failed to parse handshake", "error", err)
		upstream.Close()
		return nil
	}

	// 直接用存储凭据构建上游登录，不要求客户端二次认证
	upstreamLogin := BuildMySQLUpstreamLogin(hs, acct.Username, acct.Password.GetPlaintext(), hs.AuthPluginName, 1)
	if _, err := upstream.Write(upstreamLogin); err != nil {
		g.logger.Warn("mysql gateway failed to send upstream login", "error", err)
		upstream.Close()
		return nil
	}

	// Read upstream auth result
	authPkt, err := readMySQLPacket(upstream)
	if err != nil {
		g.logger.Warn("mysql gateway failed to read upstream auth result", "error", err)
		upstream.Close()
		return nil
	}

	// Check auth result
	if len(authPkt.payload) > 0 && authPkt.payload[0] == 0xff {
		errMsg := ParseMySQLErrorMessage(authPkt.payload)
		g.logger.Warn("mysql gateway upstream auth failed", "error", errMsg)
		_ = writeMySQLPacketWithClientAuthSeq(client, authPkt)
		upstream.Close()
		return nil
	}

	// Handle AuthSwitchRequest (0xfe) — MySQL 8.0 可能要求切换 auth plugin
	if len(authPkt.payload) > 1 && authPkt.payload[0] == 0xfe {
		switched, err := g.handleMySQLAuthSwitch(upstream, acct, hs, authPkt.payload[1:])
		if err != nil {
			g.logger.Warn("mysql gateway auth switch failed", "error", err)
			upstream.Close()
			return nil
		}
		authPkt = switched
	}

	// Handle caching_sha2_password full auth: 0x01 (more data) + 0x03 (fast auth success)
	if len(authPkt.payload) > 0 && authPkt.payload[0] == 0x01 {
		extraPkt, err := readMySQLPacket(upstream)
		if err != nil {
			g.logger.Warn("mysql gateway failed to read auth extra", "error", err)
			upstream.Close()
			return nil
		}
		if len(extraPkt.payload) > 0 && extraPkt.payload[0] == 0x03 {
			finalPkt, err := readMySQLPacket(upstream)
			if err != nil {
				g.logger.Warn("mysql gateway failed to read final auth ok", "error", err)
				upstream.Close()
				return nil
			}
			authPkt = finalPkt
		}
		// 0x04 = full auth with public key — 实现完整认证
		if len(extraPkt.payload) > 0 && extraPkt.payload[0] == 0x04 {
			fullAuthPkt, err := g.handleMySQLCachingSha2FullAuth(upstream, acct.Password.GetPlaintext())
			if err != nil {
				g.logger.Warn("mysql gateway caching_sha2_password full auth failed", "error", err)
				upstream.Close()
				return nil
			}
			authPkt = fullAuthPkt
		}
	}

	if len(authPkt.payload) > 0 && authPkt.payload[0] == 0xff {
		errMsg := ParseMySQLErrorMessage(authPkt.payload)
		g.logger.Warn("mysql gateway upstream auth failed", "error", errMsg)
		_ = writeMySQLPacketWithClientAuthSeq(client, authPkt)
		upstream.Close()
		return nil
	}
	if len(authPkt.payload) == 0 || authPkt.payload[0] != 0x00 {
		g.logger.Warn("mysql gateway unexpected upstream auth result", "payload_len", len(authPkt.payload))
		upstream.Close()
		return nil
	}

	if err := writeMySQLClientAuthOK(client); err != nil {
		upstream.Close()
		return nil
	}

	return &gatewayConn{
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

func writeMySQLClientAuthOK(conn net.Conn) error {
	// The fake server handshake is seq=0 and the client login packet is seq=1,
	// so the client-side auth result must be seq=2 regardless of upstream auth
	// switches or multi-step authentication.
	payload := []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00}
	_, err := conn.Write(mysqlPacketWithSeq(2, payload))
	return err
}

func writeMySQLClientAuthError(conn net.Conn) error {
	payload := []byte{0xff, 0x15, 0x04, '#', '2', '8', '0', '0', '0'}
	payload = append(payload, "access denied for bastion connection"...)
	_, err := conn.Write(mysqlPacketWithSeq(2, payload))
	return err
}

func writeMySQLPacketWithClientAuthSeq(conn net.Conn, pkt *mysqlPacket) error {
	if pkt == nil || len(pkt.raw) < 4 {
		return nil
	}
	raw := append([]byte(nil), pkt.raw...)
	raw[3] = 2
	_, err := conn.Write(raw)
	return err
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
func (g *Gateway) handleMySQLAuthSwitch(upstream net.Conn, acct *model.DatabaseAccount, hs *MySQLHandshake, payload []byte) (*mysqlPacket, error) {
	// 解析新 auth plugin name
	nullPos := 0
	for nullPos < len(payload) && payload[nullPos] != 0 {
		nullPos++
	}
	newPlugin := string(payload[:nullPos])
	authData := payload[nullPos+1:]

	// 构建 raw auth response
	authResp := BuildMySQLAuthResponse(newPlugin, acct.Password.GetPlaintext(), authData)
	if authResp == nil {
		return nil, fmt.Errorf("unsupported auth switch plugin: %s", newPlugin)
	}

	// 发送 auth switch 响应：仅 auth response bytes，seq=3
	resp := make([]byte, 4+len(authResp))
	resp[0] = byte(len(authResp))
	resp[1] = byte(len(authResp) >> 8)
	resp[2] = byte(len(authResp) >> 16)
	resp[3] = 3
	copy(resp[4:], authResp)
	if _, err := upstream.Write(resp); err != nil {
		return nil, fmt.Errorf("write auth switch: %w", err)
	}

	return readMySQLPacket(upstream)
}

// handleMySQLCachingSha2FullAuth 处理 caching_sha2_password 完整认证流程。
// 当密码未在服务器缓存中时（0x01/0x04），执行以下步骤：
//  1. 发送 0x02 请求服务器公钥
//  2. 读取服务器返回的 RSA 公钥（0x01 + PEM）
//  3. 用 RSA-OAEP 加密密码
//  4. 发送加密后的密码
//  5. 返回最终认证结果
func (g *Gateway) handleMySQLCachingSha2FullAuth(upstream net.Conn, password string) (*mysqlPacket, error) {
	// Step 1: 请求服务器公钥
	reqKey := []byte{0x02}
	pkt := make([]byte, 4+len(reqKey))
	pkt[0] = byte(len(reqKey))
	pkt[1] = byte(len(reqKey) >> 8)
	pkt[2] = byte(len(reqKey) >> 16)
	pkt[3] = 3
	copy(pkt[4:], reqKey)
	if _, err := upstream.Write(pkt); err != nil {
		return nil, fmt.Errorf("request public key: %w", err)
	}

	// Step 2: 读取服务器公钥
	keyPkt, err := readMySQLPacket(upstream)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	if len(keyPkt.payload) < 2 || keyPkt.payload[0] != 0x01 {
		return nil, fmt.Errorf("unexpected public key response type=%x", keyPkt.payload[0])
	}
	pubKeyPEM := keyPkt.payload[1:]

	// Step 3: 解析 PEM 公钥
	block, _ := pem.Decode(pubKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM public key")
	}
	var pubKey *rsa.PublicKey
	if parsed, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		pubKey, _ = parsed.(*rsa.PublicKey)
	}
	if pubKey == nil {
		if parsed, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
			pubKey = parsed
		}
	}
	if pubKey == nil {
		return nil, fmt.Errorf("public key is not RSA")
	}

	// Step 4: 用 RSA-OAEP 加密密码（null-terminated，与 MySQL 协议一致）
	plaintext := append([]byte(password), 0)
	encrypted, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, plaintext, nil)
	if err != nil {
		return nil, fmt.Errorf("rsa encrypt: %w", err)
	}

	// Step 5: 发送加密后的密码
	resp := make([]byte, 4+len(encrypted))
	resp[0] = byte(len(encrypted))
	resp[1] = byte(len(encrypted) >> 8)
	resp[2] = byte(len(encrypted) >> 16)
	resp[3] = 4
	copy(resp[4:], encrypted)
	if _, err := upstream.Write(resp); err != nil {
		return nil, fmt.Errorf("send encrypted password: %w", err)
	}

	// Step 6: 读取最终认证结果
	return readMySQLPacket(upstream)
}

// TODO(SCRAM): pgSCRAMExchange SASL PasswordMessage 格式有问题，上游不响应。
// pgSCRAMExchange 用存储凭据完成 SCRAM-SHA-256 交换
func (g *Gateway) pgSCRAMExchange(upstream net.Conn, username, password string, saslMsg []byte) error {
	// SASLInitialResponse: 选 SCRAM-SHA-256
	clientNonce := make([]byte, 18)
	if _, err := rand.Read(clientNonce); err != nil {
		return err
	}
	nonceStr := base64.StdEncoding.EncodeToString(clientNonce)
	cfm := "n,,n=" + username + ",r=" + nonceStr
	irPayload := []byte("SCRAM-SHA-256\x00" + cfm)
	irMsg := make([]byte, 5+len(irPayload))
	irMsg[0] = 'p'
	binary.BigEndian.PutUint32(irMsg[1:5], uint32(4+len(irPayload)))
	copy(irMsg[5:], irPayload)
	if _, err := upstream.Write(irMsg); err != nil {
		return fmt.Errorf("scram initial: %w", err)
	}

	// 读 SASLContinue (type 11)
	buf := make([]byte, 8192)
	n, err := upstream.Read(buf)
	if err != nil || n < 9 {
		return fmt.Errorf("scram continue: %w", err)
	}
	if buf[0] != 'R' || binary.BigEndian.Uint32(buf[5:9]) != 11 {
		return fmt.Errorf("expected SASLContinue, got type=%d", binary.BigEndian.Uint32(buf[5:9]))
	}
	sfm := string(buf[9:n])
	attrs := ParseSCRAMAttrs(sfm)
	salt, _ := base64.StdEncoding.DecodeString(attrs["s"])
	iter := 4096
	if attrs["i"] != "" {
		fmt.Sscanf(attrs["i"], "%d", &iter)
	}
	combinedNonce := attrs["r"]

	// 计算 SCRAM
	saltedPwd := PBKDF2Key([]byte(password), salt, iter, 32)
	clientKey := HMACSHA256(saltedPwd, []byte("Client Key"))
	storedKey := SHA256Hash(clientKey)
	authMsg := "n=" + username + ",r=" + nonceStr + "," + sfm + ",c=biws,r=" + combinedNonce
	clientSig := HMACSHA256(storedKey, []byte(authMsg))
	proof := XORBytes(clientKey, clientSig)

	cFin := "c=biws,r=" + combinedNonce + ",p=" + base64.StdEncoding.EncodeToString(proof)
	cfPayload := []byte(cFin + "\x00")
	cfMsg := make([]byte, 5+len(cfPayload))
	cfMsg[0] = 'p'
	binary.BigEndian.PutUint32(cfMsg[1:5], uint32(4+len(cfPayload)))
	copy(cfMsg[5:], cfPayload)
	if _, err := upstream.Write(cfMsg); err != nil {
		return fmt.Errorf("scram final: %w", err)
	}

	// 读最终结果
	n2, err := upstream.Read(buf)
	if err != nil || n2 < 5 {
		return fmt.Errorf("scram final read: %w", err)
	}
	if buf[0] == 'R' && binary.BigEndian.Uint32(buf[5:9]) == 0 {
		return nil
	}
	if buf[0] == 'Z' {
		return nil
	}
	if buf[0] == 'R' && binary.BigEndian.Uint32(buf[5:9]) == 12 {
		return nil // SASLFinal
	}
	return fmt.Errorf("scram auth denied")
}

// --- SCRAM-SHA-256 helpers ---

func ParseSCRAMAttrs(s string) map[string]string {
	m := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		if len(part) > 2 {
			m[string(part[0])] = part[2:]
		}
	}
	return m
}

func PBKDF2Key(password, salt []byte, iter, keyLen int) []byte {
	prf := func(p, s []byte) []byte {
		mac := hmac.New(sha256.New, p)
		mac.Write(s)
		return mac.Sum(nil)
	}
	hLen := 32
	blocks := (keyLen + hLen - 1) / hLen
	result := make([]byte, 0, blocks*hLen)
	for block := 1; block <= blocks; block++ {
		u := append(salt, byte(block>>24), byte(block>>16), byte(block>>8), byte(block))
		t := prf(password, u)
		tn := make([]byte, len(t))
		copy(tn, t)
		for i := 2; i <= iter; i++ {
			tn = prf(password, tn)
			for j := range t {
				t[j] ^= tn[j]
			}
		}
		result = append(result, t...)
	}
	return result[:keyLen]
}

func HMACSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func SHA256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func XORBytes(a, b []byte) []byte {
	r := make([]byte, len(a))
	for i := range a {
		r[i] = a[i] ^ b[i]
	}
	return r
}
