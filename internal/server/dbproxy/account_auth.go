package dbproxy

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
)

const (
	mysqlClientConnectWithDB              = 1 << 3
	mysqlClientProtocol41                 = 1 << 9
	mysqlClientSSL                        = 1 << 11
	mysqlClientSecureConnection           = 1 << 15
	mysqlClientPluginAuth                 = 1 << 19
	mysqlClientConnectAttrs               = 1 << 20
	mysqlClientPluginAuthLenencClientData = 1 << 21
)

// MySQLHandshake represents a parsed MySQL Protocol::HandshakeV10 packet.
type MySQLHandshake struct {
	ProtocolVersion byte
	ServerVersion   string
	ConnectionID    uint32
	AuthData        []byte // full 20-byte salt
	CapabilityFlags uint32
	CharacterSet    byte
	StatusFlags     uint16
	AuthPluginName  string
}

// ParseMySQLHandshake parses a MySQL Protocol::HandshakeV10 payload.
func ParseMySQLHandshake(payload []byte) (*MySQLHandshake, error) {
	if len(payload) < 1 {
		return nil, errors.New("mysql handshake packet too short")
	}

	pos := 0

	// Protocol version (1 byte)
	protocolVersion := payload[pos]
	pos++

	// Server version (null-terminated string)
	serverVersionEnd := bytes.IndexByte(payload[pos:], 0)
	if serverVersionEnd < 0 {
		return nil, errors.New("mysql handshake: missing null terminator for server version")
	}
	serverVersion := string(payload[pos : pos+serverVersionEnd])
	pos += serverVersionEnd + 1

	// Connection ID (4 bytes little-endian)
	if len(payload[pos:]) < 4 {
		return nil, errors.New("mysql handshake: truncated connection id")
	}
	connectionID := binary.LittleEndian.Uint32(payload[pos:])
	pos += 4

	// Auth data part 1 (8 bytes)
	if len(payload[pos:]) < 8 {
		return nil, errors.New("mysql handshake: truncated auth data part 1")
	}
	authData := make([]byte, 20)
	copy(authData[:8], payload[pos:pos+8])
	pos += 8

	// Filler (1 byte, skip)
	if len(payload[pos:]) < 1 {
		return nil, errors.New("mysql handshake: truncated filler")
	}
	pos++

	// Capability flags lower (2 bytes LE)
	if len(payload[pos:]) < 2 {
		return nil, errors.New("mysql handshake: truncated capability flags lower")
	}
	capLower := binary.LittleEndian.Uint16(payload[pos:])
	pos += 2

	// Character set (1 byte)
	if len(payload[pos:]) < 1 {
		return nil, errors.New("mysql handshake: truncated character set")
	}
	characterSet := payload[pos]
	pos++

	// Status flags (2 bytes LE)
	if len(payload[pos:]) < 2 {
		return nil, errors.New("mysql handshake: truncated status flags")
	}
	statusFlags := binary.LittleEndian.Uint16(payload[pos:])
	pos += 2

	// Capability flags upper (2 bytes LE)
	if len(payload[pos:]) < 2 {
		return nil, errors.New("mysql handshake: truncated capability flags upper")
	}
	capUpper := binary.LittleEndian.Uint16(payload[pos:])
	pos += 2

	capabilityFlags := uint32(capLower) | uint32(capUpper)<<16

	// Auth plugin data len (1 byte)
	if len(payload[pos:]) < 1 {
		return nil, errors.New("mysql handshake: truncated auth plugin data len")
	}
	authPluginDataLen := int(payload[pos])
	pos++

	// Reserved (10 bytes, skip)
	if len(payload[pos:]) < 10 {
		return nil, errors.New("mysql handshake: truncated reserved bytes")
	}
	pos += 10

	// Auth data part 2
	var part2Len int
	if authPluginDataLen > 0 {
		part2Len = authPluginDataLen - 8
	} else {
		part2Len = 12 // default
	}
	if part2Len > len(payload[pos:]) {
		part2Len = len(payload[pos:])
	}
	if part2Len > 0 {
		copy(authData[8:], payload[pos:pos+part2Len])
		pos += part2Len
	}

	// Auth plugin name (null-terminated, if CLIENT_PLUGIN_AUTH cap flag set)
	authPluginName := "mysql_native_password"
	if capabilityFlags&mysqlClientPluginAuth != 0 {
		if pos < len(payload) {
			pluginNameEnd := bytes.IndexByte(payload[pos:], 0)
			if pluginNameEnd >= 0 {
				authPluginName = string(payload[pos : pos+pluginNameEnd])
			}
		}
	}

	return &MySQLHandshake{
		ProtocolVersion: protocolVersion,
		ServerVersion:   serverVersion,
		ConnectionID:    connectionID,
		AuthData:        authData,
		CapabilityFlags: capabilityFlags,
		CharacterSet:    characterSet,
		StatusFlags:     statusFlags,
		AuthPluginName:  authPluginName,
	}, nil
}

// BuildMySQLNativePassword computes a mysql_native_password authentication response.
// Algorithm: SHA1(password) XOR SHA1(salt + SHA1(SHA1(password)))
func BuildMySQLNativePassword(password string, salt []byte) []byte {
	if len(salt) > 20 {
		salt = salt[:20]
	}

	// SHA1(password)
	h1 := sha1.Sum([]byte(password))

	// SHA1(SHA1(password))
	h2 := sha1.Sum(h1[:])

	// SHA1(salt + SHA1(SHA1(password)))
	combined := make([]byte, 0, len(salt)+20)
	combined = append(combined, salt...)
	combined = append(combined, h2[:]...)
	h3 := sha1.Sum(combined)

	// XOR h1 with h3 for final 20-byte result
	result := make([]byte, 20)
	for i := 0; i < 20; i++ {
		result[i] = h1[i] ^ h3[i]
	}

	return result
}

// BuildMySQLCachingSha2Password computes a caching_sha2_password authentication response.
// Algorithm: SHA256(password) XOR SHA256(salt + SHA256(SHA256(password)))
func BuildMySQLCachingSha2Password(password string, salt []byte) []byte {
	if len(salt) > 20 {
		salt = salt[:20]
	}

	// SHA256(password)
	h1 := sha256.Sum256([]byte(password))

	// SHA256(SHA256(password))
	h2 := sha256.Sum256(h1[:])

	// SHA256(salt + SHA256(SHA256(password)))
	combined := make([]byte, 0, len(salt)+32)
	combined = append(combined, salt...)
	combined = append(combined, h2[:]...)
	h3 := sha256.Sum256(combined)

	// XOR h1 with h3 for final 32-byte result
	result := make([]byte, 32)
	for i := 0; i < 32; i++ {
		result[i] = h1[i] ^ h3[i]
	}

	return result
}

type databaseLoginParser interface {
	Observe(data []byte) (loginObservation, bool, error)
}

type MySQLLoginParser struct {
	buf []byte
}

func (p *MySQLLoginParser) Observe(data []byte) (loginObservation, bool, error) {
	p.buf = append(p.buf, data...)
	if len(p.buf) < 4 {
		return loginObservation{}, false, nil
	}
	payloadLen := int(p.buf[0]) | int(p.buf[1])<<8 | int(p.buf[2])<<16
	total := 4 + payloadLen
	if payloadLen <= 0 || payloadLen > 128*1024*1024 {
		return loginObservation{}, false, errors.New("invalid mysql login packet length")
	}
	if len(p.buf) < total {
		return loginObservation{}, false, nil
	}
	payload := p.buf[4:total]
	if len(payload) < 4 {
		return loginObservation{}, false, errors.New("invalid mysql login packet")
	}
	capabilities := binary.LittleEndian.Uint32(payload[:4])
	if capabilities&mysqlClientSSL != 0 && len(payload) == 32 {
		return loginObservation{
			TLSRequested:    true,
			MetadataVisible: false,
			Observation:     "hidden_by_tls",
		}, true, nil
	}
	if capabilities&mysqlClientProtocol41 == 0 {
		return loginObservation{}, false, errors.New("unsupported mysql pre-4.1 login packet")
	}
	if len(payload) < 33 {
		return loginObservation{}, false, errors.New("invalid mysql protocol 4.1 login packet")
	}
	pos := 32
	username, rest := splitCString(payload[pos:])
	username = strings.TrimSpace(username)
	if username == "" {
		return loginObservation{}, false, errors.New("mysql login packet has empty username")
	}
	pos = len(payload) - len(rest)

	pos = skipMySQLAuthResponse(payload, pos, capabilities)
	if pos > len(payload) {
		return loginObservation{}, false, errors.New("invalid mysql auth response")
	}

	observation := loginObservation{
		User:            username,
		MetadataVisible: true,
		Observation:     "plaintext",
	}
	if capabilities&mysqlClientConnectWithDB != 0 && pos < len(payload) {
		database, rest := splitCString(payload[pos:])
		observation.Database = strings.TrimSpace(database)
		pos = len(payload) - len(rest)
	}
	if capabilities&mysqlClientPluginAuth != 0 && pos < len(payload) {
		_, rest := splitCString(payload[pos:])
		pos = len(payload) - len(rest)
	}
	if capabilities&mysqlClientConnectAttrs != 0 && pos < len(payload) {
		attrs, err := parseMySQLConnectAttrs(payload[pos:])
		if err == nil && len(attrs) > 0 {
			observation.ConnectAttrs = attrs
		}
	}
	return observation, true, nil
}

func skipMySQLAuthResponse(payload []byte, pos int, capabilities uint32) int {
	if pos >= len(payload) {
		return pos
	}
	if capabilities&mysqlClientPluginAuthLenencClientData != 0 {
		length, n, ok := readLengthEncodedInt(payload[pos:])
		if !ok {
			return len(payload) + 1
		}
		return pos + n + int(length)
	}
	if capabilities&mysqlClientSecureConnection != 0 {
		length := int(payload[pos])
		return pos + 1 + length
	}
	_, rest := splitCString(payload[pos:])
	return len(payload) - len(rest)
}

func parseMySQLConnectAttrs(data []byte) (map[string]string, error) {
	totalLen, n, ok := readLengthEncodedInt(data)
	if !ok {
		return nil, errors.New("invalid mysql connect attrs length")
	}
	data = data[n:]
	if totalLen > uint64(len(data)) {
		return nil, errors.New("truncated mysql connect attrs")
	}
	data = data[:totalLen]
	attrs := make(map[string]string)
	for len(data) > 0 {
		keyLen, kn, ok := readLengthEncodedInt(data)
		if !ok || keyLen > uint64(len(data[kn:])) {
			return nil, errors.New("invalid mysql connect attr key")
		}
		data = data[kn:]
		key := string(data[:keyLen])
		data = data[keyLen:]
		valueLen, vn, ok := readLengthEncodedInt(data)
		if !ok || valueLen > uint64(len(data[vn:])) {
			return nil, errors.New("invalid mysql connect attr value")
		}
		data = data[vn:]
		attrs[key] = string(data[:valueLen])
		data = data[valueLen:]
	}
	return attrs, nil
}

func readLengthEncodedInt(data []byte) (uint64, int, bool) {
	if len(data) == 0 {
		return 0, 0, false
	}
	first := data[0]
	switch first {
	case 0xfc:
		if len(data) < 3 {
			return 0, 0, false
		}
		return uint64(binary.LittleEndian.Uint16(data[1:3])), 3, true
	case 0xfd:
		if len(data) < 4 {
			return 0, 0, false
		}
		return uint64(data[1]) | uint64(data[2])<<8 | uint64(data[3])<<16, 4, true
	case 0xfe:
		if len(data) < 9 {
			return 0, 0, false
		}
		return binary.LittleEndian.Uint64(data[1:9]), 9, true
	default:
		return uint64(first), 1, true
	}
}

type postgresLoginParser struct {
	buf []byte
}

func (p *postgresLoginParser) Observe(data []byte) (loginObservation, bool, error) {
	p.buf = append(p.buf, data...)
	for {
		if len(p.buf) < 4 {
			return loginObservation{}, false, nil
		}
		msgLen := int(binary.BigEndian.Uint32(p.buf[:4]))
		if msgLen < 8 || msgLen > 128*1024*1024 {
			return loginObservation{}, false, errors.New("invalid postgres startup packet length")
		}
		if len(p.buf) < msgLen {
			return loginObservation{}, false, nil
		}
		payload := p.buf[:msgLen]
		p.buf = p.buf[msgLen:]
		code := binary.BigEndian.Uint32(payload[4:8])
		switch code {
		case 80877103:
			return loginObservation{
				TLSRequested:    true,
				MetadataVisible: false,
				Observation:     "hidden_by_tls",
			}, true, nil
		case 80877104:
			continue
		default:
			params := postgresStartupParams(payload[8:])
			user := strings.TrimSpace(params["user"])
			if user == "" {
				return loginObservation{}, false, errors.New("postgres startup packet has empty user")
			}
			return loginObservation{
				User:            user,
				Database:        strings.TrimSpace(params["database"]),
				ApplicationName: strings.TrimSpace(params["application_name"]),
				MetadataVisible: true,
				Observation:     "plaintext",
			}, true, nil
		}
	}
}

func postgresStartupUser(payload []byte) string {
	return postgresStartupParams(payload)["user"]
}

func postgresStartupParams(payload []byte) map[string]string {
	params := make(map[string]string)
	for len(payload) > 0 {
		key, rest := splitCString(payload)
		if key == "" {
			return params
		}
		value, next := splitCString(rest)
		params[key] = value
		payload = next
	}
	return params
}
