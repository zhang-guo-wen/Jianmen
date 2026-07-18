package dbproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// readRESPCommand 从 conn 读取一个完整的 RESP 命令。
// 返回命令名、参数列表、原始字节、错误。
func readRESPCommand(conn net.Conn) (string, []string, []byte, error) {
	reader := bufio.NewReader(conn)

	line, err := readRESPLine(reader)
	if err != nil {
		return "", nil, nil, err
	}
	if len(line) == 0 || line[0] != '*' {
		return "", nil, nil, fmt.Errorf("expected RESP array, got %q", line)
	}

	count, err := strconv.Atoi(string(line[1:]))
	if err != nil || count <= 0 || count > 256 {
		return "", nil, nil, fmt.Errorf("invalid RESP array length: %s", line[1:])
	}

	args := make([]string, 0, count)
	rawLines := make([]string, 0, count+1)
	rawLines = append(rawLines, string(line))

	for i := 0; i < count; i++ {
		argLine, err := readRESPLine(reader)
		if err != nil {
			return "", nil, nil, err
		}
		rawLines = append(rawLines, string(argLine))
		if len(argLine) == 0 || argLine[0] != '$' {
			return "", nil, nil, fmt.Errorf("expected RESP bulk string, got %q", argLine)
		}
		argLen, err := strconv.Atoi(string(argLine[1:]))
		if err != nil || argLen < 0 || argLen > 512*1024*1024 {
			return "", nil, nil, fmt.Errorf("invalid RESP bulk string length: %s", argLine[1:])
		}
		argValue := make([]byte, argLen+2)
		if _, err := io.ReadFull(reader, argValue); err != nil {
			return "", nil, nil, err
		}
		rawLines = append(rawLines, string(argValue[:argLen]))
		args = append(args, string(argValue[:argLen]))
	}

	raw := []byte(strings.Join(rawLines, "\r\n") + "\r\n")
	cmd := ""
	if len(args) > 0 {
		cmd = strings.ToUpper(args[0])
	}
	return cmd, args, raw, nil
}

// readRESPLine reads a CRLF-terminated line
func readRESPLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return nil, err
	}
	// strip trailing \r\n
	if len(line) >= 2 && line[len(line)-2] == '\r' {
		return line[:len(line)-2], nil
	}
	return line, nil
}

// writeRESPSimple writes a simple string RESP response like "+OK\r\n"
func writeRESPSimple(conn net.Conn, s string) error {
	_, err := fmt.Fprintf(conn, "+%s\r\n", s)
	return err
}

// writeRESPError writes a RESP error response like "-ERR message\r\n"
func writeRESPError(conn net.Conn, msg string) error {
	_, err := fmt.Fprintf(conn, "-ERR %s\r\n", msg)
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
		g.logger.Warn("redis gateway failed to read initial command", "error", err)
		return nil
	}

	if cmd != "AUTH" {
		if cmd == "HELLO" {
			// RDM sends HELLO 3 AUTH username password
			if len(args) >= 4 && strings.ToUpper(args[2]) == "AUTH" {
				args = args[2:] // ["AUTH", user, pass]
				cmd = "AUTH"
			} else {
				writeRESPSimple(client, "OK")
				return nil
			}
		}
		if cmd != "AUTH" {
			g.logger.Warn("redis gateway expected AUTH, got", "cmd", cmd)
			writeRESPError(client, "NOAUTH Authentication required.")
			return nil
		}
	}

	// Parse AUTH arguments: AUTH [username] password
	// args[0] is the command name ("AUTH"), skip it
	// Redis 6+ ACL: AUTH username password (total 3 args)
	// Legacy: AUTH password (total 2 args)
	authArgs := args[1:]
	var compactUser, bastionPassword string
	switch len(authArgs) {
	case 1:
		bastionPassword = authArgs[0]
	case 2:
		compactUser = authArgs[0]
		bastionPassword = authArgs[1]
	default:
		g.logger.Warn("redis gateway invalid AUTH args", "count", len(args))
		writeRESPError(client, "invalid password")
		return nil
	}

	// If compactUser not provided by ACL-style AUTH, extract from password arg
	// (client sends: AUTH <compact_username> <bastion_password>)
	// If only 1 arg, client sent compact username as sole argument, so compactUser is the first arg
	if compactUser == "" && len(authArgs) == 1 {
		compactUser = bastionPassword
		// In single-arg mode, the client sends AUTH <compact_username> and we need
		// the bastion password too. But we don't have it. So we expect 2-arg mode.
		g.logger.Warn("redis gateway expected AUTH username password")
		writeRESPError(client, "NOAUTH Authentication required. Use AUTH <username> <password>.")
		return nil
	}

	// Resolve account
	resolved, err := g.resolveAccount(strings.TrimSpace(compactUser))
	if err != nil {
		g.logger.Warn("redis gateway account resolution failed", "username", compactUser, "error", err)
		writeRESPError(client, "WRONGPASS invalid username-password pair or user is disabled.")
		return nil
	}
	acct := resolved.account

	// Validate bastion user password
	if err := g.validateUserPassword(ctx, resolved.user, resolved.account.ID, bastionPassword); err != nil {
		g.logger.Warn("redis gateway auth failed", "user", resolved.rawName, "error", err)
		writeRESPError(client, "WRONGPASS invalid username-password pair or user is disabled.")
		return nil
	}
	userID := resolved.user.ID

	// RBAC check
	resourceID := acct.ID
	if err := g.authorizeConnect(ctx, userID, resourceID); err != nil {
		g.logger.Warn("redis gateway rbac denied", "user", userID, "resource", resourceID, "error", err)
		writeRESPError(client, "NOPERM this user has no permissions to run the command")
		return nil
	}

	// Check account disabled and expiry
	if acct.Status == "disabled" {
		g.logger.Warn("redis gateway account disabled", "account", resolved.rawName)
		writeRESPError(client, "WRONGPASS invalid username-password pair or user is disabled.")
		return nil
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		g.logger.Warn("redis gateway account expired", "account", resolved.rawName, "expires_at", acct.ExpiresAt)
		writeRESPError(client, "WRONGPASS invalid username-password pair or user is disabled.")
		return nil
	}
	if acct.Instance.Status == "disabled" {
		g.logger.Warn("redis gateway instance disabled", "account", resolved.rawName, "instance", acct.InstanceID)
		writeRESPError(client, "WRONGPASS invalid username-password pair or user is disabled.")
		return nil
	}

	// Connect to upstream Redis
	upstream, err := net.DialTimeout("tcp", upstreamAddress(acct.Instance), 10*time.Second)
	if err != nil {
		g.logger.Warn("redis gateway upstream connect failed", "upstream", upstreamAddress(acct.Instance), "error", err)
		writeRESPError(client, "io-error connecting to the server")
		return nil
	}

	// Authenticate to upstream Redis
	if err := authenticateUpstreamRedis(upstream, acct.Username, acct.Password.GetPlaintext()); err != nil {
		g.logger.Warn("redis gateway upstream auth failed", "error", err)
		upstream.Close()
		writeRESPError(client, "WRONGPASS invalid username-password pair or user is disabled.")
		return nil
	}

	// Send OK to client
	if err := writeRESPSimple(client, "OK"); err != nil {
		upstream.Close()
		return nil
	}

	return &gatewayConn{
		protocol: "redis", accountID: acct.ID, instanceID: acct.InstanceID, accountName: resolved.rawName,
		upstream: upstream, upstreamAddr: upstreamAddress(acct.Instance), userID: userID,
		accountUser: acct.Username, instanceName: acct.Instance.Name,
		userSessionID: resolved.userSessionID,
	}
}

// readRESPCommandFromBuf reads a RESP command when we already have the first byte.
// readRESPCommandFromBuf 读取一个 RESP 命令，此时首字节已从协议检测中读取。
// 需要手动拼接首字节来构造第一行，后续数据从 conn 直接读取。
func readRESPCommandFromBuf(conn net.Conn, firstByte byte) (string, []string, []byte, error) {
	reader := bufio.NewReader(conn)

	// 手动构造第一行：首字节已读取，继续读到 \n
	firstLine := []byte{firstByte}
	for {
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
	firstLine = firstLine[:len(firstLine)-2] // strip \r\n

	if firstLine[0] != '*' {
		return "", nil, nil, fmt.Errorf("expected RESP array, got %q", string(firstLine))
	}

	count, err := strconv.Atoi(string(firstLine[1:]))
	if err != nil || count <= 0 || count > 256 {
		return "", nil, nil, fmt.Errorf("invalid RESP array length: %s", string(firstLine[1:]))
	}

	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		argLine, err := readRESPLine(reader)
		if err != nil {
			return "", nil, nil, err
		}
		if len(argLine) == 0 || argLine[0] != '$' {
			return "", nil, nil, fmt.Errorf("expected RESP bulk string, got %q", argLine)
		}
		argLen, err := strconv.Atoi(string(argLine[1:]))
		if err != nil || argLen < 0 || argLen > 512*1024*1024 {
			return "", nil, nil, fmt.Errorf("invalid RESP bulk string length: %s", argLine[1:])
		}
		argValue := make([]byte, argLen+2)
		if _, err := io.ReadFull(reader, argValue); err != nil {
			return "", nil, nil, err
		}
		args = append(args, string(argValue[:argLen]))
	}

	cmd := ""
	if len(args) > 0 {
		cmd = strings.ToUpper(args[0])
	}
	return cmd, args, nil, nil
}

// authenticateUpstreamRedis sends AUTH to the upstream Redis server.
// Uses ACL mode (AUTH username password) when username is provided,
// otherwise falls back to legacy AUTH password.
func authenticateUpstreamRedis(conn net.Conn, username, password string) error {
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})

	var authCmd string
	if username != "" {
		authCmd = fmt.Sprintf("*3\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
			len(username), username, len(password), password)
	} else {
		authCmd = fmt.Sprintf("*2\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n",
			len(password), password)
	}

	if _, err := fmt.Fprint(conn, authCmd); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	reader := bufio.NewReader(conn)
	line, err := readRESPLine(reader)
	if err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}
	if len(line) == 0 {
		return errors.New("empty auth response")
	}
	if line[0] == '+' {
		return nil
	}
	if line[0] == '-' {
		return fmt.Errorf("auth denied: %s", string(line[1:]))
	}
	return fmt.Errorf("unexpected auth response: %s", string(line))
}
