package dbproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	maxRESPAuthLineBytes  = 1024
	maxRESPAuthArguments  = 16
	maxRESPAuthBulkBytes  = 16 * 1024
	maxRESPAuthCommandLen = 64 * 1024

	redisInitialAuthenticationRejectedLog = "redis gateway rejected invalid initial authentication command"
)

// readRESPCommand 从 conn 读取一个完整的 RESP 命令。
// 返回命令名、参数列表、原始字节、错误。
func readRESPCommand(conn net.Conn) (string, []string, []byte, error) {
	reader := &exactRESPReader{Reader: conn}
	line, err := readRESPAuthLine(reader)
	if err != nil {
		return "", nil, nil, err
	}
	return readRESPAuthCommand(reader, line, true)
}

type respAuthReader interface {
	io.Reader
	ReadByte() (byte, error)
}

type exactRESPReader struct {
	io.Reader
}

func (r *exactRESPReader) ReadByte() (byte, error) {
	var value [1]byte
	if _, err := io.ReadFull(r.Reader, value[:]); err != nil {
		return 0, err
	}
	return value[0], nil
}

func readRESPAuthCommand(reader respAuthReader, firstLine []byte, includeRaw bool) (string, []string, []byte, error) {
	if len(firstLine) == 0 || firstLine[0] != '*' {
		return "", nil, nil, errors.New("expected RESP array")
	}
	countValue, ok := parseCanonicalRESPUnsigned(firstLine[1:], maxRESPAuthArguments)
	if !ok || countValue <= 0 {
		return "", nil, nil, errors.New("invalid RESP array length")
	}
	count := int(countValue)
	total := len(firstLine) + 2
	args := make([]string, 0, count)
	var raw []byte
	if includeRaw {
		raw = make([]byte, 0, maxRESPAuthCommandLen)
		raw = append(raw, firstLine...)
		raw = append(raw, '\r', '\n')
	}
	for i := 0; i < count; i++ {
		argLine, err := readRESPAuthLine(reader)
		if err != nil {
			return "", nil, nil, err
		}
		if len(argLine) == 0 || argLine[0] != '$' {
			return "", nil, nil, errors.New("expected RESP bulk string")
		}
		argLenValue, ok := parseCanonicalRESPUnsigned(argLine[1:], maxRESPAuthBulkBytes)
		if !ok {
			return "", nil, nil, errors.New("invalid RESP bulk string length")
		}
		argLen := int(argLenValue)
		if total > maxRESPAuthCommandLen-len(argLine)-argLen-4 {
			return "", nil, nil, errors.New("RESP authentication command exceeds limit")
		}
		total += len(argLine) + argLen + 4
		argValue := make([]byte, argLen+2)
		if _, err := io.ReadFull(reader, argValue); err != nil {
			return "", nil, nil, err
		}
		if argValue[argLen] != '\r' || argValue[argLen+1] != '\n' {
			return "", nil, nil, errors.New("malformed RESP bulk string terminator")
		}
		args = append(args, string(argValue[:argLen]))
		if includeRaw {
			raw = append(raw, argLine...)
			raw = append(raw, '\r', '\n')
			raw = append(raw, argValue...)
		}
	}
	return strings.ToUpper(args[0]), args, raw, nil
}

func readRESPAuthLine(reader respAuthReader) ([]byte, error) {
	line := make([]byte, 0, 64)
	for {
		if len(line) >= maxRESPAuthLineBytes {
			return nil, errors.New("RESP authentication line exceeds limit")
		}
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if len(line) > 0 && line[len(line)-1] == '\r' && b != '\n' {
			return nil, errors.New("malformed RESP line terminator")
		}
		line = append(line, b)
		if b == '\n' {
			if len(line) < 2 || line[len(line)-2] != '\r' {
				return nil, errors.New("malformed RESP line terminator")
			}
			return line[:len(line)-2], nil
		}
	}
}

// readRESPLine reads a CRLF-terminated line
func readRESPLine(reader *bufio.Reader) ([]byte, error) {
	return readRESPAuthLine(reader)
}

// writeRESPSimple writes a simple string RESP response like "+OK\r\n"
func writeRESPSimple(conn net.Conn, s string) error {
	_, err := fmt.Fprintf(conn, "+%s\r\n", s)
	return err
}

// writeRESPError writes a RESP error response like "-ERR message\r\n"
func writeRESPError(conn net.Conn, msg string) error {
	return writeRESPErrorCode(conn, "ERR", msg)
}

func writeRESPErrorCode(conn net.Conn, code, msg string) error {
	_, err := fmt.Fprintf(conn, "-%s %s\r\n", code, msg)
	return err
}

// handleRedis implements the Redis proxy authentication flow:
// 1. Read the first RESP command (expected AUTH or HELLO)
// 2. If HELLO, negotiate protocol then AUTH
// 3. Extract compact username (R prefix) and bastion password
// 4. Resolve account via compact username lookup
// 5. Validate bastion user password
// 6. RBAC + account/instance status checks
// 7. Connect to upstream Redis
// 8. Authenticate to upstream with stored credentials (ACL or legacy)
// 9. Relay upstream response to client
// 10. Return gatewayConn for transparent data relay
func (g *Gateway) handleRedis(ctx context.Context, client net.Conn, firstByte byte) *gatewayConn {
	cmd, args, _, err := readRESPCommandFromBuf(client, firstByte)
	if err != nil {
		g.logRejectedRedisInitialAuthentication()
		return nil
	}

	initialAuthentication, authenticationError, ok := parseRedisInitialAuthentication(cmd, args)
	if !ok {
		g.logRejectedRedisInitialAuthentication()
		switch authenticationError {
		case redisInitialAuthenticationUnsupportedProtocol:
			_ = writeRESPErrorCode(client, "NOPROTO", "unsupported protocol version")
		case redisInitialAuthenticationRequired:
			_ = writeRESPErrorCode(client, "NOAUTH", "Authentication required.")
		default:
			_ = writeRESPError(client, "syntax error")
		}
		return nil
	}
	compactUser := initialAuthentication.username
	bastionPassword := initialAuthentication.password

	// Resolve account
	resolved, err := g.resolveAccount(ctx, strings.TrimSpace(compactUser))
	if err != nil {
		g.logRejectedRedisInitialAuthentication()
		writeRESPErrorCode(client, "WRONGPASS", "invalid username-password pair or user is disabled.")
		return nil
	}
	acct := resolved.account
	if err := validateResolvedAccountProtocol(resolved, databaseProtocolRedis); err != nil {
		g.logRejectedRedisInitialAuthentication()
		writeRESPErrorCode(client, "WRONGPASS", "invalid username-password pair or user is disabled.")
		return nil
	}

	if err := g.authenticateRedisConnection(ctx, resolved, bastionPassword); err != nil {
		g.logRejectedRedisInitialAuthentication()
		if errors.Is(err, errDatabaseAuthentication) {
			writeRESPErrorCode(client, "WRONGPASS", "invalid username-password pair or user is disabled.")
		} else {
			writeRESPErrorCode(client, "NOPERM", "this user has no permissions to run the command")
		}
		return nil
	}
	userID := resolved.user.ID

	// Check account disabled and expiry
	if acct.Status == "disabled" {
		g.logRejectedRedisInitialAuthentication()
		writeRESPErrorCode(client, "WRONGPASS", "invalid username-password pair or user is disabled.")
		return nil
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		g.logRejectedRedisInitialAuthentication()
		writeRESPErrorCode(client, "WRONGPASS", "invalid username-password pair or user is disabled.")
		return nil
	}
	if acct.Instance.Status == "disabled" {
		g.logRejectedRedisInitialAuthentication()
		writeRESPErrorCode(client, "WRONGPASS", "invalid username-password pair or user is disabled.")
		return nil
	}

	// Compute one absolute deadline before dialing so authentication cannot
	// replenish time already spent connecting or negotiating TLS.
	authenticationDeadline := redisUpstreamAuthenticationDeadline(ctx, time.Now())

	// Connect to upstream Redis with the instance TLS policy before sending AUTH.
	upstream, err := dialRedisUpstream(ctx, acct.Instance)
	if err != nil {
		g.logRejectedRedisInitialAuthentication()
		writeRESPError(client, "io-error connecting to the server")
		return nil
	}

	// Authenticate to upstream Redis
	if err := authenticateUpstreamRedis(upstream, acct.Username, acct.Password.GetPlaintext(), authenticationDeadline); err != nil {
		g.logRejectedRedisInitialAuthentication()
		upstream.Close()
		writeRESPErrorCode(client, "WRONGPASS", "invalid username-password pair or user is disabled.")
		return nil
	}

	if initialAuthentication.helloVersion == 0 {
		if err := writeRESPSimple(client, "OK"); err != nil {
			upstream.Close()
			return nil
		}
	} else {
		response, err := negotiateUpstreamRedisHello(
			upstream,
			initialAuthentication.helloVersion,
			initialAuthentication.clientName,
			authenticationDeadline,
		)
		if err != nil {
			g.logRejectedRedisInitialAuthentication()
			upstream.Close()
			_ = writeRESPError(client, "io-error negotiating protocol with the server")
			return nil
		}
		if _, err := client.Write(response); err != nil {
			upstream.Close()
			return nil
		}
	}

	return &gatewayConn{
		protocol: "redis", accountID: acct.ID, instanceID: acct.InstanceID, accountName: resolved.rawName,
		upstream: upstream, upstreamAddr: upstreamAddress(acct.Instance), userID: userID,
		accountUser: acct.Username, instanceName: acct.Instance.Name,
		userSessionID: resolved.userSessionID,
	}
}

func (g *Gateway) logRejectedRedisInitialAuthentication() {
	if g.logger != nil {
		g.logger.Warn(redisInitialAuthenticationRejectedLog)
	}
}

// readRESPCommandFromBuf reads a RESP command when we already have the first byte.
// readRESPCommandFromBuf 读取一个 RESP 命令，此时首字节已从协议检测中读取。
// 需要手动拼接首字节来构造第一行，后续数据从 conn 直接读取。
func readRESPCommandFromBuf(conn net.Conn, firstByte byte) (string, []string, []byte, error) {
	reader := &exactRESPReader{Reader: conn}

	// 手动构造第一行：首字节已读取，继续读到 \n
	firstLine := make([]byte, 0, maxRESPAuthLineBytes)
	firstLine = append(firstLine, firstByte)
	for {
		if len(firstLine) >= maxRESPAuthLineBytes {
			return "", nil, nil, errors.New("RESP authentication line exceeds limit")
		}
		b, err := reader.ReadByte()
		if err != nil {
			return "", nil, nil, fmt.Errorf("read first line: %w", err)
		}
		firstLine = append(firstLine, b)
		if b == '\n' {
			break
		}
	}
	if len(firstLine) < 3 || firstLine[len(firstLine)-2] != '\r' {
		return "", nil, nil, fmt.Errorf("malformed RESP first line")
	}
	return readRESPAuthCommand(reader, firstLine[:len(firstLine)-2], false)

}
