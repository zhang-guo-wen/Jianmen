package dbproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

// ProbeDatabaseAuthentication validates credentials through the same upstream
// TLS policy used by the database gateway. It never sends a cleartext password
// before the connection has passed that policy.
func ProbeDatabaseAuthentication(ctx context.Context, instance model.DatabaseInstance, username, password string) error {
	var protocol string
	var err error
	switch strings.ToLower(instance.Protocol) {
	case "mysql":
		protocol = "mysql"
		err = probeMySQLAuthentication(ctx, instance, username, password)
	case "postgres", "postgresql":
		protocol = "postgres"
		err = probePostgresAuthentication(ctx, instance, username, password)
	case "redis":
		protocol = "redis"
		err = probeRedisAuthentication(ctx, instance, username, password)
	default:
		return &databaseProbeError{message: "database authentication probe failed: unsupported protocol"}
	}
	if err == nil {
		return nil
	}
	message := protocol + " database authentication probe failed"
	if errors.Is(err, errVerifiedTLSRequired) {
		message += ": verified TLS required"
	}
	return &databaseProbeError{message: message, cause: err}
}

type databaseProbeError struct {
	message string
	cause   error
}

func (err *databaseProbeError) Error() string { return err.message }
func (err *databaseProbeError) Unwrap() error { return err.cause }

func probeMySQLAuthentication(ctx context.Context, instance model.DatabaseInstance, username, password string) error {
	conn, handshake, err := dialMySQLUpstream(ctx, instance, "")
	if err != nil {
		return fmt.Errorf("connect MySQL upstream: %w", err)
	}
	defer conn.Close()
	sequence := byte(1)
	if dbtls.IsVerified(conn) {
		sequence = 2
	}
	login, err := BuildMySQLUpstreamLogin(handshake, username, password, handshake.AuthPluginName, sequence)
	if err != nil {
		return fmt.Errorf("build MySQL login: %w", err)
	}
	if _, err := conn.Write(login); err != nil {
		return fmt.Errorf("write MySQL login: %w", err)
	}
	for {
		packet, err := readMySQLPacket(conn)
		if err != nil {
			return fmt.Errorf("read MySQL authentication response: %w", err)
		}
		if len(packet.payload) == 0 {
			return errors.New("empty MySQL authentication response")
		}
		switch packet.payload[0] {
		case 0x00:
			return conn.SetDeadline(time.Time{})
		case 0xff:
			return errors.New("MySQL authentication denied")
		case 0xfe:
			if len(packet.payload) < 2 {
				return errors.New("truncated MySQL authentication switch request")
			}
			plugin, authData, err := parseMySQLAuthSwitch(packet.payload[1:])
			if err != nil {
				return err
			}
			response := BuildMySQLAuthResponse(plugin, password, authData)
			if response == nil {
				return errors.New("unsupported MySQL authentication plugin")
			}
			if _, err := conn.Write(mysqlPacketWithSeq(mysqlResponseSequence(packet.seq), response)); err != nil {
				return fmt.Errorf("write MySQL authentication switch response: %w", err)
			}
		case 0x01:
			code, source, err := readMySQLCachingSHA2ProbeMoreData(conn, packet)
			if err != nil {
				return err
			}
			switch code {
			case 0x03:
				continue
			case 0x04:
				if err := requireVerifiedMySQLTLS(conn); err != nil {
					return err
				}
				if _, err := conn.Write(mysqlPacketWithSeq(mysqlResponseSequence(source.seq), append([]byte(password), 0))); err != nil {
					return fmt.Errorf("write MySQL TLS-protected full-auth password: %w", err)
				}
			default:
				return errors.New("unsupported MySQL caching_sha2_password response")
			}
		default:
			return errors.New("unexpected MySQL authentication response")
		}
	}
}

func parseMySQLAuthSwitch(payload []byte) (string, []byte, error) {
	separator := bytesIndex(payload, 0)
	if separator < 0 {
		return "", nil, errors.New("malformed MySQL authentication switch request")
	}
	return string(payload[:separator]), payload[separator+1:], nil
}

func readMySQLCachingSHA2ProbeMoreData(conn net.Conn, packet *mysqlPacket) (byte, *mysqlPacket, error) {
	if code, ok := MySQLCachingSHA2AuthMoreData(packet.payload); ok {
		return code, packet, nil
	}
	if len(packet.payload) != 1 || packet.payload[0] != 0x01 {
		return 0, nil, errors.New("invalid MySQL caching_sha2_password response")
	}
	continuation, err := readMySQLPacket(conn)
	if err != nil {
		return 0, nil, fmt.Errorf("read MySQL caching_sha2_password continuation: %w", err)
	}
	code, ok := MySQLCachingSHA2AuthMoreData(continuation.payload)
	if !ok {
		return 0, nil, errors.New("invalid MySQL caching_sha2_password continuation")
	}
	return code, continuation, nil
}

func probePostgresAuthentication(ctx context.Context, instance model.DatabaseInstance, username, password string) error {
	conn, err := dialPostgresUpstream(ctx, instance)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL upstream: %w", err)
	}
	defer conn.Close()
	if _, err := conn.Write(BuildPostgresUpstreamStartupMessage(username, "postgres")); err != nil {
		return fmt.Errorf("write PostgreSQL startup: %w", err)
	}
	for {
		message, err := readPostgresMessage(conn, maxPostgresAuthMessageBytes)
		if err != nil {
			return err
		}
		if message.kind == 'E' {
			return errors.New("PostgreSQL authentication denied")
		}
		if message.kind == 'Z' {
			return conn.SetDeadline(time.Time{})
		}
		if message.kind != 'R' || len(message.payload) < 4 {
			continue
		}
		authType := binary.BigEndian.Uint32(message.payload[:4])
		switch authType {
		case 0:
			continue
		case 3:
			if err := validatePostgresCleartextPasswordTransport(conn, instance.TLSMode); err != nil {
				return err
			}
			if err := writePostgresProbePassword(conn, password); err != nil {
				return err
			}
		case 5:
			if len(message.payload) != 8 {
				return errors.New("truncated PostgreSQL MD5 authentication challenge")
			}
			if err := writePostgresProbePassword(conn, BuildPostgresPasswordResponse(5, username, password, message.payload[4:8])); err != nil {
				return err
			}
		case 10:
			if err := runPostgresSCRAM(conn, username, password, message.payload[4:]); err != nil {
				return err
			}
		default:
			return errors.New("unsupported PostgreSQL authentication type")
		}
	}
}

func readPostgresProbeMessage(conn net.Conn) (byte, []byte, error) {
	message, err := readPostgresMessage(conn, maxPostgresAuthMessageBytes)
	return message.kind, message.payload, err
}

func writePostgresProbePassword(conn net.Conn, password string) error {
	return writePostgresProbeSASLResponse(conn, append([]byte(password), 0))
}

func writePostgresProbeSASLResponse(conn net.Conn, payload []byte) error {
	return writePostgresMessage(conn, 'p', payload)
}

func probeRedisAuthentication(ctx context.Context, instance model.DatabaseInstance, username, password string) error {
	conn, err := dialRedisUpstream(ctx, instance)
	if err != nil {
		return fmt.Errorf("connect Redis upstream: %w", err)
	}
	defer conn.Close()
	return probeRedisAuthenticationOnConnection(conn, username, password)
}

func probeRedisAuthenticationOnConnection(conn net.Conn, username, password string) error {
	parts := []string{"AUTH"}
	if username != "" {
		parts = append(parts, username)
	}
	parts = append(parts, password)
	if _, err := conn.Write(redisProbeCommand(parts...)); err != nil {
		return fmt.Errorf("write Redis AUTH: %w", err)
	}
	response := make([]byte, 4096)
	n, err := conn.Read(response)
	if err != nil {
		return fmt.Errorf("read Redis AUTH: %w", err)
	}
	if strings.HasPrefix(string(response[:n]), "+") {
		return conn.SetDeadline(time.Time{})
	}
	return errors.New("Redis authentication denied")
}

func redisProbeCommand(parts ...string) []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "*%d\r\n", len(parts))
	for _, part := range parts {
		fmt.Fprintf(&builder, "$%d\r\n%s\r\n", len(part), part)
	}
	return []byte(builder.String())
}

func bytesIndex(data []byte, target byte) int {
	for index, value := range data {
		if value == target {
			return index
		}
	}
	return -1
}
