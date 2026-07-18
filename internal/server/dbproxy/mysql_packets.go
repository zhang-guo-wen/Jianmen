package dbproxy

import (
	"fmt"
	"io"
	"net"

	"jianmen/internal/proxy/mysqlwire"
)

// mysqlPacket represents a parsed MySQL packet
type mysqlPacket struct {
	raw     []byte
	payload []byte
	seq     byte
}

const maxMySQLControlPacketBytes = 1 << 20

// readMySQLPacket reads a single MySQL packet (4-byte header + payload) from conn
func readMySQLPacket(conn net.Conn) (*mysqlPacket, error) {
	return readMySQLPacketLimited(conn, maxMySQLControlPacketBytes)
}

func readMySQLPacketLimited(conn net.Conn, maxPayloadBytes int) (*mysqlPacket, error) {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	payloadLen := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if payloadLen > maxPayloadBytes {
		return nil, fmt.Errorf("invalid mysql packet length %d", payloadLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	raw := make([]byte, 4+payloadLen)
	copy(raw, header[:])
	copy(raw[4:], payload)
	return &mysqlPacket{raw: raw, payload: payload, seq: header[3]}, nil
}

// BuildMySQLUpstreamLogin builds a MySQL login packet for the upstream server.
// Exported for use by test connection in admin package.
func BuildMySQLUpstreamLogin(hs *MySQLHandshake, username, password, authPlugin string, seq byte) ([]byte, error) {
	if hs == nil {
		return nil, fmt.Errorf("build MySQL upstream login: nil handshake")
	}
	packet, err := mysqlwire.BuildHandshakeResponse41(
		mysqlwire.Handshake{
			ProtocolVersion: hs.ProtocolVersion,
			ServerVersion:   hs.ServerVersion,
			ConnectionID:    hs.ConnectionID,
			AuthData:        hs.AuthData,
			CapabilityFlags: hs.CapabilityFlags,
			CharacterSet:    hs.CharacterSet,
			StatusFlags:     hs.StatusFlags,
			AuthPluginName:  hs.AuthPluginName,
		},
		mysqlwire.LoginOptions{
			Username: username, Password: password, AuthPlugin: authPlugin,
			Sequence: seq, TLS: seq == 2,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("build MySQL upstream login: %w", err)
	}
	return packet, nil
}
