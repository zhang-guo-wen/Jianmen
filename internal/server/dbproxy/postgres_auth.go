package dbproxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

type postgresUpstreamStartup struct {
	cancelCleanup func()
}

func authenticatePostgresUpstream(
	upstream, client net.Conn,
	username, password string,
	registerCancel func(postgresCancelKey) func(),
) (postgresUpstreamStartup, error) {
	var result postgresUpstreamStartup
	var cancelCleanup func()
	succeeded := false
	defer func() {
		if !succeeded && cancelCleanup != nil {
			cancelCleanup()
		}
	}()
	authenticated := false
	for {
		message, err := readPostgresMessage(upstream, maxPostgresAuthMessageBytes)
		if err != nil {
			return postgresUpstreamStartup{}, fmt.Errorf(
				"read PostgreSQL upstream startup message: %w",
				err,
			)
		}
		if message.kind == 'E' {
			return postgresUpstreamStartup{}, errors.New("PostgreSQL upstream startup denied")
		}
		if !authenticated {
			switch message.kind {
			case 'N':
				if !validPostgresErrorFields(message.payload) {
					return postgresUpstreamStartup{}, errors.New("malformed PostgreSQL startup notice")
				}
				if err := forwardPostgresStartupMessage(client, message); err != nil {
					return postgresUpstreamStartup{}, err
				}
				continue
			case 'R':
				authType, err := postgresAuthenticationType(message.payload)
				if err != nil {
					return postgresUpstreamStartup{}, err
				}
				switch authType {
				case 0:
					if len(message.payload) != 4 {
						return postgresUpstreamStartup{}, errors.New(
							"malformed PostgreSQL AuthenticationOk",
						)
					}
					if err := forwardPostgresStartupMessage(client, message); err != nil {
						return postgresUpstreamStartup{}, err
					}
					authenticated = true
				case 3:
					if len(message.payload) != 4 {
						return postgresUpstreamStartup{}, errors.New(
							"malformed PostgreSQL cleartext authentication challenge",
						)
					}
					if err := requireVerifiedPostgresTLS(upstream); err != nil {
						return postgresUpstreamStartup{}, err
					}
					if err := writePostgresMessage(upstream, 'p', append([]byte(password), 0)); err != nil {
						return postgresUpstreamStartup{}, fmt.Errorf(
							"write PostgreSQL cleartext password response: %w",
							err,
						)
					}
				case 5:
					if len(message.payload) != 8 {
						return postgresUpstreamStartup{}, errors.New(
							"malformed PostgreSQL MD5 authentication challenge",
						)
					}
					response := BuildPostgresPasswordResponse(5, username, password, message.payload[4:8])
					if err := writePostgresMessage(upstream, 'p', append([]byte(response), 0)); err != nil {
						return postgresUpstreamStartup{}, fmt.Errorf(
							"write PostgreSQL MD5 password response: %w",
							err,
						)
					}
				case 10:
					if err := runPostgresSCRAM(upstream, username, password, message.payload[4:]); err != nil {
						return postgresUpstreamStartup{}, fmt.Errorf(
							"complete PostgreSQL SCRAM authentication: %w",
							err,
						)
					}
				default:
					return postgresUpstreamStartup{}, fmt.Errorf(
						"unsupported PostgreSQL authentication type %d",
						authType,
					)
				}
				continue
			default:
				return postgresUpstreamStartup{}, fmt.Errorf(
					"unexpected PostgreSQL authentication message type %q",
					message.kind,
				)
			}
		}

		switch message.kind {
		case 'S':
			if !validPostgresParameterStatus(message.payload) {
				return postgresUpstreamStartup{}, errors.New("malformed PostgreSQL ParameterStatus")
			}
		case 'K':
			key, err := parsePostgresBackendKey(message.payload)
			if err != nil {
				return postgresUpstreamStartup{}, err
			}
			if cancelCleanup != nil {
				return postgresUpstreamStartup{}, errors.New("duplicate PostgreSQL BackendKeyData")
			}
			cancelCleanup = registerCancel(key)
			if cancelCleanup == nil {
				return postgresUpstreamStartup{}, errors.New(
					"PostgreSQL cancellation registrar returned no cleanup",
				)
			}
			result.cancelCleanup = cancelCleanup
		case 'N':
			if !validPostgresErrorFields(message.payload) {
				return postgresUpstreamStartup{}, errors.New("malformed PostgreSQL startup notice")
			}
		case 'Z':
			if len(message.payload) != 1 ||
				(message.payload[0] != 'I' && message.payload[0] != 'T' && message.payload[0] != 'E') {
				return postgresUpstreamStartup{}, errors.New("malformed PostgreSQL ReadyForQuery")
			}
		default:
			return postgresUpstreamStartup{}, fmt.Errorf(
				"unexpected PostgreSQL post-authentication message type %q",
				message.kind,
			)
		}
		if err := forwardPostgresStartupMessage(client, message); err != nil {
			return postgresUpstreamStartup{}, err
		}
		if message.kind != 'Z' {
			continue
		}
		if err := upstream.SetDeadline(time.Time{}); err != nil {
			return postgresUpstreamStartup{}, fmt.Errorf(
				"clear PostgreSQL upstream authentication deadline: %w",
				err,
			)
		}
		succeeded = true
		return result, nil
	}
}

func postgresAuthenticationType(payload []byte) (uint32, error) {
	if len(payload) < 4 {
		return 0, errors.New("malformed PostgreSQL authentication message")
	}
	return binary.BigEndian.Uint32(payload[:4]), nil
}

func forwardPostgresStartupMessage(client net.Conn, message postgresWireMessage) error {
	if err := writePostgresBytes(client, message.raw()); err != nil {
		return fmt.Errorf("forward PostgreSQL startup message: %w", err)
	}
	return nil
}
