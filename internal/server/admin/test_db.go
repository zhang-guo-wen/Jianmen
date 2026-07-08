package admin

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/server/dbproxy"
)

// handleTestDBConnection handles POST /api/db/accounts/test and POST /api/db/accounts/test/{id}
func (s *Server) handleTestDBConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.TrimSuffix(r.URL.Path, "/") == "/api/db/accounts/test" {
		s.handleTestDBConnectionPayload(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/db/accounts/test/")
	if id == "" || strings.Contains(id, "/") {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}

	var acct model.DatabaseAccount
	if err := s.db.Preload("Instance").First(&acct, "id = ?", id).Error; err != nil {
		writeErrorText(w, http.StatusNotFound, "account not found")
		return
	}

	if acct.Status == "disabled" {
		writeErrorText(w, http.StatusForbidden, "account is disabled")
		return
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		writeErrorText(w, http.StatusForbidden, "account has expired")
		return
	}
	if acct.Instance.Status == "disabled" {
		writeErrorText(w, http.StatusForbidden, "database instance is disabled")
		return
	}

	start := time.Now()
	upstreamAddr := acct.Instance.Address
	if acct.Instance.Port > 0 {
		upstreamAddr = fmt.Sprintf("%s:%d", acct.Instance.Address, acct.Instance.Port)
	}
	conn, err := net.DialTimeout("tcp", upstreamAddr, 5*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "error": fmt.Sprintf("connect: %v", err), "latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}
	defer conn.Close()

	err = testDBAuth(conn, acct.Instance.Protocol, acct.Username, acct.Password.GetPlaintext())
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "error": err.Error(), "latency_ms": latencyMs,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "latency_ms": latencyMs,
	})
}

type testDBConnectionPayload struct {
	InstanceID string `json:"instance_id"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

func (s *Server) handleTestDBConnectionPayload(w http.ResponseWriter, r *http.Request) {
	var payload testDBConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeErrorText(w, http.StatusBadRequest, "invalid json body")
		return
	}
	payload.InstanceID = strings.TrimSpace(payload.InstanceID)
	payload.Username = strings.TrimSpace(payload.Username)
	if payload.InstanceID == "" || payload.Username == "" || payload.Password == "" {
		writeErrorText(w, http.StatusBadRequest, "instance_id, username and password are required")
		return
	}

	var inst model.DatabaseInstance
	if err := s.db.First(&inst, "id = ?", payload.InstanceID).Error; err != nil {
		writeErrorText(w, http.StatusNotFound, "instance not found")
		return
	}
	if inst.Status == "disabled" {
		writeErrorText(w, http.StatusForbidden, "database instance is disabled")
		return
	}

	start := time.Now()
	upstreamAddr := inst.Address
	if inst.Port > 0 {
		upstreamAddr = fmt.Sprintf("%s:%d", inst.Address, inst.Port)
	}
	conn, err := net.DialTimeout("tcp", upstreamAddr, 5*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "error": fmt.Sprintf("connect: %v", err), "latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}
	defer conn.Close()

	err = testDBAuth(conn, inst.Protocol, payload.Username, payload.Password)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "error": err.Error(), "latency_ms": latencyMs,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "latency_ms": latencyMs,
	})
}

func testDBAuth(conn net.Conn, protocol, username, password string) error {
	switch strings.ToLower(protocol) {
	case "postgres", "postgresql":
		return testPostgresAuth(conn, username, password)
	case "mysql":
		return testMySQLAuth(conn, username, password)
	case "redis":
		return testRedisAuth(conn, username, password)
	default:
		return fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func testPostgresAuth(conn net.Conn, username, password string) error {
	var sb strings.Builder
	sb.WriteString("user")
	sb.WriteByte(0)
	sb.WriteString(username)
	sb.WriteByte(0)
	sb.WriteString("database")
	sb.WriteByte(0)
	sb.WriteString("postgres")
	sb.WriteByte(0)
	sb.WriteByte(0)
	payload := sb.String()
	msgLen := 4 + 4 + len(payload)
	msg := make([]byte, msgLen)
	binary.BigEndian.PutUint32(msg[0:4], uint32(msgLen))
	binary.BigEndian.PutUint32(msg[4:8], 196608)
	copy(msg[8:], payload)
	if _, err := conn.Write(msg); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n < 5 {
		return fmt.Errorf("auth: %w", err)
	}

	if buf[0] == 'R' {
		authType := binary.BigEndian.Uint32(buf[5:9])
		if authType == 0 {
			return nil
		}
		var authPassword string
		switch authType {
		case 3:
			authPassword = password
		case 5:
			if n < 13 {
				return fmt.Errorf("auth: truncated md5 auth message")
			}
			salt := buf[9:13]
			h1 := md5.Sum([]byte(password + username))
			h1Hex := hex.EncodeToString(h1[:])
			h2Input := make([]byte, len(h1Hex)+4)
			copy(h2Input, h1Hex)
			copy(h2Input[len(h1Hex):], salt)
			h2 := md5.Sum(h2Input)
			authPassword = "md5" + hex.EncodeToString(h2[:])
		case 10:
			return testPGScramAuth(conn, username, password, buf[:n])
		default:
			return fmt.Errorf("auth: unsupported auth type %d", authType)
		}
		pwdMsg := make([]byte, 5+len(authPassword)+1)
		pwdMsg[0] = 'p'
		binary.BigEndian.PutUint32(pwdMsg[1:5], uint32(4+len(authPassword)+1))
		copy(pwdMsg[5:], authPassword)
		pwdMsg[5+len(authPassword)] = 0
		if _, err := conn.Write(pwdMsg); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		n2, err := conn.Read(buf)
		if err != nil || n2 < 5 {
			return fmt.Errorf("auth: %w", err)
		}
		if buf[0] == 'R' && binary.BigEndian.Uint32(buf[5:9]) == 0 {
			return nil
		}
		if buf[0] == 'Z' {
			return nil
		}
		return fmt.Errorf("auth denied")
	}
	return fmt.Errorf("auth failed")
}

// TODO(SCRAM): 同 server.go pgSCRAMExchange
// testPGScramAuth 处理 SASL/SCRAM-SHA-256 认证
func testPGScramAuth(conn net.Conn, username, password string, initMsg []byte) error {
	// 跳过 SASL mechanism list，直接选 SCRAM-SHA-256
	clientNonce := make([]byte, 18)
	if _, err := rand.Read(clientNonce); err != nil {
		return fmt.Errorf("scram nonce: %w", err)
	}
	nonceStr := base64.StdEncoding.EncodeToString(clientNonce)

	// SASLInitialResponse: "SCRAM-SHA-256" + client-first-message
	cfm := "n,,n=" + username + ",r=" + nonceStr
	irPayload := []byte("SCRAM-SHA-256\x00" + cfm)
	irMsg := make([]byte, 5+len(irPayload))
	irMsg[0] = 'p'
	binary.BigEndian.PutUint32(irMsg[1:5], uint32(4+len(irPayload)))
	copy(irMsg[5:], irPayload)
	if _, err := conn.Write(irMsg); err != nil {
		return fmt.Errorf("scram initial: %w", err)
	}

	// 读服务器 SASLContinue
	buf := make([]byte, 8192)
	n, err := conn.Read(buf)
	if err != nil || n < 5 {
		return fmt.Errorf("scram continue: %w", err)
	}
	if buf[0] == 'R' && binary.BigEndian.Uint32(buf[5:9]) == 11 {
		// 解析 server-first-message: r=...,s=...,i=...
		sfm := string(buf[9:n])
		attrs := dbproxy.ParseSCRAMAttrs(sfm)
		saltB64 := attrs["s"]
		iterStr := attrs["i"]
		salt, _ := base64.StdEncoding.DecodeString(saltB64)
		iter := 4096
		if iterStr != "" {
			fmt.Sscanf(iterStr, "%d", &iter)
		}
		combinedNonce := attrs["r"]

		// 计算 SCRAM 值
		saltedPwd := dbproxy.PBKDF2Key([]byte(password), salt, iter, 32)
		clientKey := dbproxy.HMACSHA256(saltedPwd, []byte("Client Key"))
		storedKey := dbproxy.SHA256Hash(clientKey)
		authMsg := "n=" + username + ",r=" + nonceStr + "," + sfm + ",c=biws,r=" + combinedNonce
		clientSig := dbproxy.HMACSHA256(storedKey, []byte(authMsg))
		proof := dbproxy.XORBytes(clientKey, clientSig)

		// client-final-message
		cFin := "c=biws,r=" + combinedNonce + ",p=" + base64.StdEncoding.EncodeToString(proof)
		cfPayload := []byte(cFin + "\x00")
		cfMsg := make([]byte, 5+len(cfPayload))
		cfMsg[0] = 'p'
		binary.BigEndian.PutUint32(cfMsg[1:5], uint32(4+len(cfPayload)))
		copy(cfMsg[5:], cfPayload)
		if _, err := conn.Write(cfMsg); err != nil {
			return fmt.Errorf("scram final: %w", err)
		}

		// 读最终结果
		n2, err := conn.Read(buf)
		if err != nil || n2 < 5 {
			return fmt.Errorf("scram final read: %w", err)
		}
		if buf[0] == 'R' && binary.BigEndian.Uint32(buf[5:9]) == 0 {
			return nil
		}
		if buf[0] == 'Z' {
			return nil
		}
		return fmt.Errorf("auth denied")
	}
	return fmt.Errorf("scram unexpected: type=%d", binary.BigEndian.Uint32(buf[5:9]))
}

func testMySQLAuth(conn net.Conn, username, password string) error {
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n < 4 {
		return fmt.Errorf("auth: %w", err)
	}
	hsPayloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if 4+hsPayloadLen > n {
		remaining := make([]byte, 4+hsPayloadLen-n)
		if _, err := io.ReadFull(conn, remaining); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		buf = append(buf[:n], remaining...)
	}
	hsPayload := buf[4 : 4+hsPayloadLen]
	hs, err := dbproxy.ParseMySQLHandshake(hsPayload)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// 使用 handshake 中声明的 auth plugin，而非硬编码
	authPlugin := hs.AuthPluginName
	loginPkt := dbproxy.BuildMySQLUpstreamLogin(hs, username, password, authPlugin, 1)
	if _, err := conn.Write(loginPkt); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	n2, err := conn.Read(buf)
	if err != nil || n2 < 4 {
		return fmt.Errorf("auth: %w", err)
	}
	authPayloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if len(buf) >= 4+authPayloadLen && buf[4] == 0xff {
		return fmt.Errorf("auth denied: %s", dbproxy.ParseMySQLErrorMessage(buf[4:4+authPayloadLen]))
	}
	if len(buf) >= 4+authPayloadLen && buf[4] == 0x00 {
		return nil
	}
	// AuthSwitchRequest (0xfe): 服务器要求切换 auth plugin
	if len(buf) >= 4+authPayloadLen && buf[4] == 0xfe {
		payload := buf[5 : 4+authPayloadLen]
		nullPos := 0
		for nullPos < len(payload) && payload[nullPos] != 0 {
			nullPos++
		}
		newPlugin := string(payload[:nullPos])
		authData := payload[nullPos+1:]
		if len(authData) > 0 {
			hs.AuthData = authData
		}
		authRespBytes := dbproxy.BuildMySQLAuthResponse(newPlugin, password, authData)
		if authRespBytes == nil {
			return fmt.Errorf("unsupported auth switch plugin: %s", newPlugin)
		}
		resp := make([]byte, 4+len(authRespBytes))
		resp[0] = byte(len(authRespBytes))
		resp[1] = byte(len(authRespBytes) >> 8)
		resp[2] = byte(len(authRespBytes) >> 16)
		resp[3] = 3
		copy(resp[4:], authRespBytes)
		if _, err := conn.Write(resp); err != nil {
			return fmt.Errorf("auth switch: %w", err)
		}
		n3, err := conn.Read(buf)
		if err != nil || n3 < 4 {
			return fmt.Errorf("auth switch read: %w", err)
		}
		payloadLen2 := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
		if len(buf) >= 4+payloadLen2 && buf[4] == 0x00 {
			return nil
		}
		if len(buf) >= 4+payloadLen2 && buf[4] == 0xff {
			return fmt.Errorf("auth denied after switch: %s", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen2]))
		}
		return fmt.Errorf("unexpected auth after switch: payload[4]=0x%02x", buf[4])
	}
	// caching_sha2_password fast auth 第二阶段：0x01 + 0x03
	if len(buf) >= 4+authPayloadLen && buf[4] == 0x01 {
		n3, err := conn.Read(buf)
		if err != nil || n3 < 4 {
			return fmt.Errorf("auth phase 2: %w", err)
		}
		payloadLen2 := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
		if len(buf) >= 4+payloadLen2 && buf[4] == 0x03 {
			return nil
		}
		if len(buf) >= 4+payloadLen2 && buf[4] == 0x00 {
			return nil
		}
		return fmt.Errorf("auth phase 2 unexpected: payload[4]=0x%02x", buf[4])
	}
	return fmt.Errorf("unexpected auth result: payload[4]=0x%02x", buf[4])
}

// testRedisAuth 测试 Redis 上游认证。
// 使用 ACL 模式（AUTH username password），若 username 为空则使用单密码模式。
func testRedisAuth(conn net.Conn, username, password string) error {
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})

	var authCmd string
	if username != "" {
		authCmd = fmt.Sprintf("*3\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
			len(username), username, len(password), password)
	} else {
		authCmd = fmt.Sprintf("*2\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n",
			len(password), password)
	}

	if _, err := fmt.Fprint(conn, authCmd); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	resp := strings.TrimSpace(string(buf[:n]))
	if strings.HasPrefix(resp, "+") {
		return nil
	}
	if strings.HasPrefix(resp, "-") {
		return fmt.Errorf("auth denied: %s", resp[1:])
	}
	return fmt.Errorf("unexpected auth response: %s", resp)
}
