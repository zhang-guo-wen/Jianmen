package dbproxy

import (
	"encoding/binary"
	"errors"
	"fmt"
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

type accountAuthState struct {
	enabled     bool
	enforce     bool
	allowed     map[string]struct{}
	parser      databaseLoginParser
	ready       bool
	observation loginObservation
}

type databaseLoginParser interface {
	Observe(data []byte) (loginObservation, bool, error)
}

func newAccountAuth(protocol string, allowedUsers []string) (*accountAuthState, error) {
	var parser databaseLoginParser
	switch protocol {
	case "mysql":
		parser = &mysqlLoginParser{}
	case "postgres":
		parser = &postgresLoginParser{}
	default:
		if len(allowedUsers) == 0 {
			return &accountAuthState{}, nil
		}
		return nil, fmt.Errorf("database account auth is not supported for protocol %q", protocol)
	}

	allowed := make(map[string]struct{}, len(allowedUsers))
	for _, user := range allowedUsers {
		allowed[strings.ToLower(strings.TrimSpace(user))] = struct{}{}
	}
	return &accountAuthState{
		enabled: true,
		enforce: len(allowedUsers) > 0,
		allowed: allowed,
		parser:  parser,
	}, nil
}

func (a *accountAuthState) Enabled() bool {
	return a != nil && a.enabled
}

func (a *accountAuthState) Enforced() bool {
	return a != nil && a.enforce
}

func (a *accountAuthState) Ready() bool {
	return a == nil || !a.enabled || a.ready
}

func (a *accountAuthState) Observation() loginObservation {
	if a == nil {
		return loginObservation{}
	}
	return a.observation
}

func (a *accountAuthState) ObserveClientBytes(data []byte) (bool, error) {
	if a == nil || !a.enabled || a.ready {
		return true, nil
	}
	observation, ready, err := a.parser.Observe(data)
	if err != nil {
		return false, err
	}
	if !ready {
		return false, nil
	}
	if observation.Observation == "" {
		observation.Observation = "plaintext"
	}
	a.observation = observation
	if a.enforce {
		if observation.User == "" {
			return false, errors.New("database username is not visible for account auth")
		}
		if _, ok := a.allowed[strings.ToLower(observation.User)]; !ok {
			return false, fmt.Errorf("database user %q is not allowed", observation.User)
		}
	}
	a.ready = true
	return true, nil
}

type mysqlLoginParser struct {
	buf []byte
}

func (p *mysqlLoginParser) Observe(data []byte) (loginObservation, bool, error) {
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
