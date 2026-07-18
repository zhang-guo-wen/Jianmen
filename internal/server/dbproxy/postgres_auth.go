package dbproxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

func authenticatePostgresUpstream(upstream, client net.Conn, username, password string) error {
	for {
		message, err := readPostgresMessage(upstream, maxPostgresAuthMessageBytes)
		if err != nil {
			return fmt.Errorf("read PostgreSQL upstream authentication message: %w", err)
		}
		if message.kind == 'E' {
			return errors.New("PostgreSQL upstream authentication denied")
		}
		if message.kind != 'R' || len(message.payload) < 4 {
			return fmt.Errorf("unexpected PostgreSQL authentication message type %q", message.kind)
		}
		authType := binary.BigEndian.Uint32(message.payload[:4])
		switch authType {
		case 0:
			if _, err := client.Write(message.raw()); err != nil {
				return fmt.Errorf("forward PostgreSQL AuthenticationOk: %w", err)
			}
			if err := upstream.SetDeadline(time.Time{}); err != nil {
				return fmt.Errorf("clear PostgreSQL upstream authentication deadline: %w", err)
			}
			return nil
		case 3:
			if err := requireVerifiedPostgresTLS(upstream); err != nil {
				return err
			}
			if err := writePostgresMessage(upstream, 'p', append([]byte(password), 0)); err != nil {
				return fmt.Errorf("write PostgreSQL cleartext password response: %w", err)
			}
		case 5:
			if len(message.payload) != 8 {
				return errors.New("malformed PostgreSQL MD5 authentication challenge")
			}
			response := BuildPostgresPasswordResponse(5, username, password, message.payload[4:8])
			if err := writePostgresMessage(upstream, 'p', append([]byte(response), 0)); err != nil {
				return fmt.Errorf("write PostgreSQL MD5 password response: %w", err)
			}
		case 10:
			if err := runPostgresSCRAM(upstream, username, password, message.payload[4:]); err != nil {
				return fmt.Errorf("complete PostgreSQL SCRAM authentication: %w", err)
			}
		default:
			return fmt.Errorf("unsupported PostgreSQL authentication type %d", authType)
		}
	}
}
