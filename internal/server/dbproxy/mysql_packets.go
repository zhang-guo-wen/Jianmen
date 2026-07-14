package dbproxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// mysqlPacket represents a parsed MySQL packet
type mysqlPacket struct {
	raw     []byte
	payload []byte
	seq     byte
}

// readMySQLPacket reads a single MySQL packet (4-byte header + payload) from conn
func readMySQLPacket(conn net.Conn) (*mysqlPacket, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	payloadLen := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if payloadLen == 0 || payloadLen > 128*1024*1024 {
		return nil, fmt.Errorf("invalid mysql packet length %d", payloadLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	raw := make([]byte, 4+payloadLen)
	copy(raw, header)
	copy(raw[4:], payload)
	return &mysqlPacket{raw: raw, payload: payload, seq: header[3]}, nil
}

// BuildMySQLUpstreamLogin builds a MySQL login packet for the upstream server.
// Exported for use by test connection in admin package.
func BuildMySQLUpstreamLogin(hs *MySQLHandshake, username, password, authPlugin string, seq byte) []byte {
	var authResp []byte
	switch authPlugin {
	case "mysql_native_password":
		authResp = BuildMySQLNativePassword(password, hs.AuthData)
	case "caching_sha2_password":
		authResp = BuildMySQLCachingSha2Password(password, hs.AuthData)
	}

	capFlags := uint32(mysqlClientProtocol41 | mysqlClientSecureConnection | mysqlClientPluginAuth)

	var payload []byte
	capBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(capBytes, capFlags)
	payload = append(payload, capBytes...)
	maxPkt := make([]byte, 4)
	binary.LittleEndian.PutUint32(maxPkt, 16777215)
	payload = append(payload, maxPkt...)
	payload = append(payload, hs.CharacterSet)
	reserved := make([]byte, 23)
	payload = append(payload, reserved...)
	payload = append(payload, []byte(username)...)
	payload = append(payload, 0)
	payload = append(payload, byte(len(authResp)))
	payload = append(payload, authResp...)
	payload = append(payload, 0) // empty database
	payload = append(payload, []byte(authPlugin)...)
	payload = append(payload, 0)

	pkt := make([]byte, 4+len(payload))
	pkt[0] = byte(len(payload))
	pkt[1] = byte(len(payload) >> 8)
	pkt[2] = byte(len(payload) >> 16)
	pkt[3] = seq
	copy(pkt[4:], payload)
	return pkt
}
