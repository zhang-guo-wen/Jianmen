package service

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/server/dbproxy"
)

type DBGrant struct {
	Database  string `json:"database"`
	Privilege string `json:"privilege"` // "read" | "readwrite"
}

func grantSQL(db, privilege, user, host string) string {
	switch privilege {
	case "read":
		return fmt.Sprintf("GRANT SELECT ON `%s`.* TO '%s'@'%s'", db, user, host)
	case "readwrite":
		return fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON `%s`.* TO '%s'@'%s'", db, user, host)
	default:
		return ""
	}
}

// mysqlConnect connects to the target MySQL and completes authentication, returning the authenticated connection.
func mysqlConnect(addr string, username, password string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n < 4 {
		conn.Close()
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	hsPayloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if 4+hsPayloadLen > n {
		remaining := make([]byte, 4+hsPayloadLen-n)
		if _, err := io.ReadFull(conn, remaining); err != nil {
			conn.Close()
			return nil, fmt.Errorf("read handshake payload: %w", err)
		}
		buf = append(buf[:n], remaining...)
	}
	hsPayload := buf[4 : 4+hsPayloadLen]
	hs, err := dbproxy.ParseMySQLHandshake(hsPayload)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("parse handshake: %w", err)
	}
	loginPkt := dbproxy.BuildMySQLUpstreamLogin(hs, username, password, hs.AuthPluginName, 1)
	if _, err := conn.Write(loginPkt); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write login: %w", err)
	}
	return readMySQLAuthResult(conn, hs, password, buf)
}

// readMySQLAuthResult reads the MySQL login response, handling OK/ERR/AuthSwitch/caching_sha2 fast auth.
func readMySQLAuthResult(conn net.Conn, hs *dbproxy.MySQLHandshake, password string, buf []byte) (net.Conn, error) {
	n, err := conn.Read(buf)
	if err != nil || n < 4 {
		conn.Close()
		return nil, fmt.Errorf("read auth response: %w", err)
	}
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	// OK
	if len(buf) >= 4+payloadLen && buf[4] == 0x00 {
		return conn, nil
	}
	// ERR
	if len(buf) >= 4+payloadLen && buf[4] == 0xff {
		conn.Close()
		return nil, fmt.Errorf("auth denied: %s", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	}
	// AuthSwitchRequest (0xfe)
	if len(buf) >= 4+payloadLen && buf[4] == 0xfe {
		payload := buf[5 : 4+payloadLen]
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
			conn.Close()
			return nil, fmt.Errorf("unsupported auth switch plugin: %s", newPlugin)
		}
		resp := make([]byte, 4+len(authRespBytes))
		resp[0] = byte(len(authRespBytes))
		resp[1] = byte(len(authRespBytes) >> 8)
		resp[2] = byte(len(authRespBytes) >> 16)
		resp[3] = 3 // seq after login
		copy(resp[4:], authRespBytes)
		if _, err := conn.Write(resp); err != nil {
			conn.Close()
			return nil, fmt.Errorf("write auth switch: %w", err)
		}
		return readSimpleAuthResult(conn, buf)
	}
	// caching_sha2_password fast auth phase 2: 0x01 -> read again for 0x03 (fast auth success) or 0x04 (full auth)
	if len(buf) >= 4+payloadLen && buf[4] == 0x01 {
		n2, err := conn.Read(buf)
		if err != nil || n2 < 4 {
			conn.Close()
			return nil, fmt.Errorf("read auth phase 2: %w", err)
		}
		payloadLen2 := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
		if len(buf) >= 4+payloadLen2 && buf[4] == 0x04 {
			// Server requests full auth: send password in cleartext (assumes non-TLS cleartext for now)
			pwdBytes := []byte(password)
			pwdPkt := make([]byte, 4+len(pwdBytes)+1)
			pwdPkt[0] = byte(len(pwdBytes) + 1)
			pwdPkt[1] = byte((len(pwdBytes) + 1) >> 8)
			pwdPkt[2] = byte((len(pwdBytes) + 1) >> 16)
			pwdPkt[3] = 3
			copy(pwdPkt[4:], pwdBytes)
			if _, err := conn.Write(pwdPkt); err != nil {
				conn.Close()
				return nil, fmt.Errorf("write full auth password: %w", err)
			}
			return readSimpleAuthResult(conn, buf)
		}
		if len(buf) >= 4+payloadLen2 && (buf[4] == 0x03 || buf[4] == 0x00) {
			return conn, nil
		}
		conn.Close()
		return nil, fmt.Errorf("auth phase 2 failed: payload[4]=0x%02x", buf[4])
	}
	conn.Close()
	return nil, fmt.Errorf("unexpected auth result: payload[4]=0x%02x", buf[4])
}

func readSimpleAuthResult(conn net.Conn, buf []byte) (net.Conn, error) {
	n, err := conn.Read(buf)
	if err != nil || n < 4 {
		conn.Close()
		return nil, fmt.Errorf("read auth result: %w", err)
	}
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if len(buf) >= 4+payloadLen && buf[4] == 0x00 {
		return conn, nil
	}
	if len(buf) >= 4+payloadLen && buf[4] == 0xff {
		conn.Close()
		return nil, fmt.Errorf("auth denied: %s", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	}
	conn.Close()
	return nil, fmt.Errorf("unexpected result: payload[4]=0x%02x", buf[4])
}

// mysqlQuery sends a SQL query and reads the complete result set (for simple queries like SHOW DATABASES).
func mysqlQuery(conn net.Conn, query string) ([][]string, error) {
	payload := make([]byte, 1+len(query))
	payload[0] = 0x03 // COM_QUERY
	copy(payload[1:], query)
	pkt := mysqlPacketWithSeq(1, payload)
	if _, err := conn.Write(pkt); err != nil {
		return nil, fmt.Errorf("write query: %w", err)
	}

	buf := make([]byte, 65536)
	_, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read query response: %w", err)
	}
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if len(buf) >= 4+payloadLen && buf[4] == 0xff {
		return nil, fmt.Errorf("query error: %s", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	}

	colCount, _ := readLenEncInt(buf[4:])
	if colCount == 0 {
		return nil, nil
	}

	// Read column definitions + EOF
	for i := uint64(0); i < colCount; i++ {
		if _, err := readMySQLPacketFromConn(conn); err != nil {
			return nil, fmt.Errorf("read column def %d: %w", i, err)
		}
	}
	if _, err := readMySQLPacketFromConn(conn); err != nil {
		return nil, fmt.Errorf("read columns EOF: %w", err)
	}

	// Read row data
	var rows [][]string
	for {
		pkt2, err := readMySQLPacketFromConn(conn)
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		if len(pkt2.payload) > 0 && pkt2.payload[0] == 0xfe {
			break // EOF
		}
		if len(pkt2.payload) > 0 && pkt2.payload[0] == 0xff {
			return nil, fmt.Errorf("query error: %s", dbproxy.ParseMySQLErrorMessage(pkt2.payload))
		}
		rows = append(rows, parseMySQLTextRow(pkt2.payload))
	}
	return rows, nil
}

// mysqlExec sends a non-query SQL statement (CREATE USER, GRANT, etc.).
func mysqlExec(conn net.Conn, stmt string) error {
	payload := make([]byte, 1+len(stmt))
	payload[0] = 0x03
	copy(payload[1:], stmt)
	pkt := mysqlPacketWithSeq(1, payload)
	if _, err := conn.Write(pkt); err != nil {
		return fmt.Errorf("write exec: %w", err)
	}

	buf := make([]byte, 4096)
	_, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("read exec response: %w", err)
	}
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if len(buf) >= 4+payloadLen && buf[4] == 0xff {
		return fmt.Errorf("%s", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	}
	// OK packet always starts with 0x00
	if len(buf) >= 4+payloadLen && buf[4] == 0x00 {
		return nil
	}
	return fmt.Errorf("unexpected exec result: payload[4]=0x%02x", buf[4])
}

// ListMySQLDatabases connects to the MySQL instance and executes SHOW DATABASES.
func ListMySQLDatabases(instance model.DatabaseInstance, adminAccount model.DatabaseAccount) ([]string, error) {
	addr := instance.Address
	if instance.Port > 0 {
		addr = fmt.Sprintf("%s:%d", instance.Address, instance.Port)
	}
	plainPwd := adminAccount.Password.GetPlaintext()
	if plainPwd == "" {
		return nil, errors.New("admin account password is empty")
	}
	conn, err := mysqlConnect(addr, adminAccount.Username, plainPwd)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := mysqlQuery(conn, "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	dbs := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 && row[0] != "" {
			dbs = append(dbs, row[0])
		}
	}
	return dbs, nil
}

// ProvisionMySQLAccount connects to MySQL, creates user, and grants privileges.
func ProvisionMySQLAccount(instance model.DatabaseInstance, adminAccount model.DatabaseAccount, newUser, password, host string, grants []DBGrant) error {
	addr := instance.Address
	if instance.Port > 0 {
		addr = fmt.Sprintf("%s:%d", instance.Address, instance.Port)
	}
	plainPwd := adminAccount.Password.GetPlaintext()
	if plainPwd == "" {
		return errors.New("admin account password is empty")
	}
	conn, err := mysqlConnect(addr, adminAccount.Username, plainPwd)
	if err != nil {
		return err
	}
	defer conn.Close()

	createSQL := fmt.Sprintf("CREATE USER '%s'@'%s' IDENTIFIED BY '%s'",
		escapeMySQLString(newUser), escapeMySQLString(host), escapeMySQLString(password))
	if err := mysqlExec(conn, createSQL); err != nil {
		return fmt.Errorf("CREATE USER: %w", err)
	}

	for _, g := range grants {
		sql := grantSQL(g.Database, g.Privilege, newUser, host)
		if sql == "" {
			continue
		}
		if err := mysqlExec(conn, sql); err != nil {
			return fmt.Errorf("GRANT %s on %s: %w", g.Privilege, g.Database, err)
		}
	}
	_ = mysqlExec(conn, "FLUSH PRIVILEGES")
	return nil
}

func escapeMySQLString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, c := range s {
		switch c {
		case '\'', '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(c)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// --- MySQL protocol helpers ---

func mysqlPacketWithSeq(seq byte, payload []byte) []byte {
	packet := make([]byte, 4+len(payload))
	packet[0] = byte(len(payload))
	packet[1] = byte(len(payload) >> 8)
	packet[2] = byte(len(payload) >> 16)
	packet[3] = seq
	copy(packet[4:], payload)
	return packet
}

type mysqlPkt struct {
	payload []byte
	seq     byte
}

func readMySQLPacketFromConn(conn net.Conn) (*mysqlPkt, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	payloadLen := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return &mysqlPkt{payload: payload, seq: header[3]}, nil
}

func readLenEncInt(data []byte) (uint64, int) {
	if len(data) == 0 {
		return 0, 0
	}
	v := data[0]
	if v < 0xfb {
		return uint64(v), 1
	}
	if v == 0xfc && len(data) >= 3 {
		return uint64(data[1]) | uint64(data[2])<<8, 3
	}
	if v == 0xfd && len(data) >= 4 {
		return uint64(data[1]) | uint64(data[2])<<8 | uint64(data[3])<<16, 4
	}
	if v == 0xfe && len(data) >= 9 {
		return binary.LittleEndian.Uint64(data[1:9]), 9
	}
	return 0, 0
}

func parseMySQLTextRow(payload []byte) []string {
	pos := 0
	var cols []string
	for pos < len(payload) {
		if payload[pos] == 0xfb {
			cols = append(cols, "")
			pos++
			continue
		}
		strLen, offset := readLenEncInt(payload[pos:])
		pos += offset
		end := pos + int(strLen)
		if end > len(payload) {
			end = len(payload)
		}
		cols = append(cols, string(payload[pos:end]))
		pos = end
	}
	return cols
}
