package admin

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/server/dbproxy"
)

// handleTestDBConnection handles POST /api/db/accounts/test/{id}
func (s *Server) handleTestDBConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
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

	start := time.Now()
	conn, err := net.DialTimeout("tcp", acct.Instance.Address, 5*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "error": fmt.Sprintf("connect: %v", err), "latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}
	defer conn.Close()

	err = testDBAuth(conn, acct.Instance.Protocol, acct.UpstreamUsername, acct.UpstreamPassword)
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
		if authType == 3 || authType == 5 {
			pwdMsg := make([]byte, 5+len(password)+1)
			pwdMsg[0] = 'p'
			binary.BigEndian.PutUint32(pwdMsg[1:5], uint32(4+len(password)+1))
			copy(pwdMsg[5:], password)
			pwdMsg[5+len(password)] = 0
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
	}
	return fmt.Errorf("auth failed")
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

	loginPkt := dbproxy.BuildMySQLUpstreamLogin(hs, username, password, "mysql_native_password")
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
	return fmt.Errorf("unexpected auth result")
}
