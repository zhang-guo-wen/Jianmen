package dbproxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const (
	mysqlClientProtocol41 = 1 << 9
	mysqlClientSSL        = 1 << 11
)

type accountAuthState struct {
	enabled bool
	allowed map[string]struct{}
	parser  databaseLoginParser
	ready   bool
	user    string
}

type databaseLoginParser interface {
	Observe(data []byte) (string, bool, error)
}

func newAccountAuth(protocol string, allowedUsers []string) (*accountAuthState, error) {
	if len(allowedUsers) == 0 {
		return &accountAuthState{}, nil
	}
	allowed := make(map[string]struct{}, len(allowedUsers))
	for _, user := range allowedUsers {
		allowed[strings.ToLower(strings.TrimSpace(user))] = struct{}{}
	}
	var parser databaseLoginParser
	switch protocol {
	case "mysql":
		parser = &mysqlLoginParser{}
	case "postgres":
		parser = &postgresLoginParser{}
	default:
		return nil, fmt.Errorf("database account auth is not supported for protocol %q", protocol)
	}
	return &accountAuthState{
		enabled: true,
		allowed: allowed,
		parser:  parser,
	}, nil
}

func (a *accountAuthState) Enabled() bool {
	return a != nil && a.enabled
}

func (a *accountAuthState) Ready() bool {
	return a == nil || !a.enabled || a.ready
}

func (a *accountAuthState) ObserveClientBytes(data []byte) (bool, error) {
	if a == nil || !a.enabled || a.ready {
		return true, nil
	}
	user, ready, err := a.parser.Observe(data)
	if err != nil {
		return false, err
	}
	if !ready {
		return false, nil
	}
	a.user = user
	if _, ok := a.allowed[strings.ToLower(user)]; !ok {
		return false, fmt.Errorf("database user %q is not allowed", user)
	}
	a.ready = true
	return true, nil
}

type mysqlLoginParser struct {
	buf []byte
}

func (p *mysqlLoginParser) Observe(data []byte) (string, bool, error) {
	p.buf = append(p.buf, data...)
	if len(p.buf) < 4 {
		return "", false, nil
	}
	payloadLen := int(p.buf[0]) | int(p.buf[1])<<8 | int(p.buf[2])<<16
	total := 4 + payloadLen
	if payloadLen <= 0 || payloadLen > 128*1024*1024 {
		return "", false, errors.New("invalid mysql login packet length")
	}
	if len(p.buf) < total {
		return "", false, nil
	}
	payload := p.buf[4:total]
	if len(payload) < 4 {
		return "", false, errors.New("invalid mysql login packet")
	}
	capabilities := binary.LittleEndian.Uint32(payload[:4])
	if capabilities&mysqlClientSSL != 0 && len(payload) == 32 {
		return "", false, errors.New("mysql TLS login hides database username from account auth")
	}
	if capabilities&mysqlClientProtocol41 == 0 {
		return "", false, errors.New("unsupported mysql pre-4.1 login packet")
	}
	if len(payload) < 33 {
		return "", false, errors.New("invalid mysql protocol 4.1 login packet")
	}
	username, _ := splitCString(payload[32:])
	username = strings.TrimSpace(username)
	if username == "" {
		return "", false, errors.New("mysql login packet has empty username")
	}
	return username, true, nil
}

type postgresLoginParser struct {
	buf []byte
}

func (p *postgresLoginParser) Observe(data []byte) (string, bool, error) {
	p.buf = append(p.buf, data...)
	for {
		if len(p.buf) < 4 {
			return "", false, nil
		}
		msgLen := int(binary.BigEndian.Uint32(p.buf[:4]))
		if msgLen < 8 || msgLen > 128*1024*1024 {
			return "", false, errors.New("invalid postgres startup packet length")
		}
		if len(p.buf) < msgLen {
			return "", false, nil
		}
		payload := p.buf[:msgLen]
		p.buf = p.buf[msgLen:]
		code := binary.BigEndian.Uint32(payload[4:8])
		switch code {
		case 80877103, 80877104:
			continue
		default:
			user := postgresStartupUser(payload[8:])
			if user == "" {
				return "", false, errors.New("postgres startup packet has empty user")
			}
			return user, true, nil
		}
	}
}

func postgresStartupUser(payload []byte) string {
	for len(payload) > 0 {
		key, rest := splitCString(payload)
		if key == "" {
			return ""
		}
		value, next := splitCString(rest)
		if key == "user" {
			return strings.TrimSpace(value)
		}
		payload = next
	}
	return ""
}
