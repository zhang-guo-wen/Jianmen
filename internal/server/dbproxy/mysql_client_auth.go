package dbproxy

import (
	"encoding/binary"
	"errors"
	"net"
)

func mysqlLoginAuthResponse(payload []byte) ([]byte, error) {
	if len(payload) < 33 {
		return nil, errors.New("invalid mysql protocol 4.1 login packet")
	}
	capabilities := binary.LittleEndian.Uint32(payload[:4])
	if capabilities&mysqlClientProtocol41 == 0 {
		return nil, errors.New("unsupported mysql pre-4.1 login packet")
	}
	position := 32
	_, rest := splitCString(payload[position:])
	position = len(payload) - len(rest)
	if position >= len(payload) {
		return nil, errors.New("missing mysql auth response")
	}

	var length int
	switch {
	case capabilities&mysqlClientPluginAuthLenencClientData != 0:
		value, count, ok := readLengthEncodedInt(payload[position:])
		if !ok || value > uint64(len(payload)) {
			return nil, errors.New("invalid mysql auth response length")
		}
		position += count
		length = int(value)
	case capabilities&mysqlClientSecureConnection != 0:
		length = int(payload[position])
		position++
	default:
		_, authRest := splitCString(payload[position:])
		length = len(payload[position:]) - len(authRest) - 1
	}
	if length < 0 || position+length > len(payload) {
		return nil, errors.New("truncated mysql auth response")
	}
	return append([]byte(nil), payload[position:position+length]...), nil
}

func isMySQLTLSRequest(packet *mysqlPacket) bool {
	return packet != nil && len(packet.payload) == 32 &&
		binary.LittleEndian.Uint32(packet.payload[:4])&mysqlClientSSL != 0
}

func mysqlClientAuthResponseSequence(loginSequence byte) byte {
	return loginSequence + 1
}

func writeMySQLClientAuthOK(conn net.Conn, sequence byte) error {
	payload := []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00}
	_, err := conn.Write(mysqlPacketWithSeq(sequence, payload))
	return err
}

func writeMySQLClientAuthError(conn net.Conn, sequence byte) error {
	payload := []byte{0xff, 0x15, 0x04, '#', '2', '8', '0', '0', '0'}
	payload = append(payload, "access denied for bastion connection"...)
	_, err := conn.Write(mysqlPacketWithSeq(sequence, payload))
	return err
}
