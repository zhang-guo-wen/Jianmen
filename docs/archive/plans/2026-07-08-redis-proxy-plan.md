# Redis 代理实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Jianmen 堡垒机数据库网关新增 Redis 协议代理支持（ACL 认证模式，R 前缀 compact username，复用 33060 端口）

**Architecture:** 在现有 dbproxy 网关中新增 Redis 协议分支。Redis 客户端直接发送 RESP 命令（首字节 `*`），代理解析 AUTH 命令提取紧凑用户名，通过 bcrypt 验证堡垒机密码后再用存储的 AES 加密凭据向上游 Redis 认证。认证后透明转发 RESP 命令，由 redisObserver 做命令审计。

**Tech Stack:** Go 1.21+, RESP 协议手工解析, Vue 3 + Element Plus, TypeScript

## Global Constraints

- 复用现有 `DatabaseGateway` 配置和 33060 端口，不新增监听器
- compact username 使用 `R` 前缀（区别于 MySQL/PG 的 `D`），格式：`R` + 4位resource_id + 5位session_id
- 认证模式：当 `DatabaseAccount.Username` 不为空时使用 ACL 模式 `AUTH username password`，否则回退单密码 `AUTH password`
- 客户端 AUTH 密码是堡垒机登录密码，代理自动替换为上游密码
- 后端 `go build ./... && go test ./... -count=1` 必须通过
- 前端 `npm run typecheck && npm run build` 必须通过

---

### Task 1: 新增 R 前缀并在 resolveAccount 中支持

**Files:**
- Modify: `internal/util/compact.go:5-10`
- Modify: `internal/server/dbproxy/server.go:870-889`

**Interfaces:**
- Consumes: 现有 `PrefixDatabase = "D"` 常量
- Produces: `PrefixRedis = "R"` 常量；`resolveAccount` 接受 `R` 前缀并路由到 `resolveCompactAccount`

- [ ] **Step 1: 在 compact.go 中添加 PrefixRedis**

```go
// internal/util/compact.go line 9 后新增
const (
    PrefixHost     = "H"
    PrefixDatabase = "D"
    PrefixRedis    = "R"
)
```

- [ ] **Step 2: 更新 resolveAccount 以同时接受 D 和 R 前缀**

修改 `internal/server/dbproxy/server.go` 第 885 行：

将：
```go
if prefix != util.PrefixDatabase {
    return nil, fmt.Errorf("unsupported prefix %q in username %q, expected D", prefix, rawUsername)
}
```

改为：
```go
if prefix != util.PrefixDatabase && prefix != util.PrefixRedis {
    return nil, fmt.Errorf("unsupported prefix %q in username %q, expected D or R", prefix, rawUsername)
}
```

也更新第 877-879 行的注释和错误消息：

将：
```go
// 仅支持 compact username 登录（10位，D前缀）
if len(rawUsername) != 10 {
    return nil, fmt.Errorf("invalid username format: must be 10-character compact username (D + resource_id + session_id)")
}
```

改为：
```go
// 仅支持 compact username 登录（10位，D或R前缀）
if len(rawUsername) != 10 {
    return nil, fmt.Errorf("invalid username format: must be 10-character compact username (D/R + resource_id + session_id)")
}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

预期：编译成功

- [ ] **Step 4: 运行相关测试**

```bash
go test ./internal/util/... ./internal/server/dbproxy/... -count=1
```

预期：所有现有测试通过

- [ ] **Step 5: Commit**

```bash
git add internal/util/compact.go internal/server/dbproxy/server.go
git commit -m "feat: add PrefixRedis constant and support R prefix in resolveAccount"
```

---

### Task 2: normalizeDBProtocol 和 upstreamAddress 支持 Redis

**Files:**
- Modify: `internal/store/dbstore_databases.go:49-58`
- Modify: `internal/server/dbproxy/server.go:109-119`

**Interfaces:**
- Consumes: 新增 `"redis"` protocol 字符串；默认端口 6379
- Produces: `normalizeDBProtocol` 接受 `"redis"`；`upstreamAddress` 返回默认 6379 端口

- [ ] **Step 1: 在 normalizeDBProtocol 中添加 Redis**

修改 `internal/store/dbstore_databases.go` 第 54 行：

将：
```go
if protocol != "mysql" && protocol != "postgres" && protocol != "tcp" {
    return "", fmt.Errorf("unsupported database protocol %q", protocol)
}
```

改为：
```go
if protocol != "mysql" && protocol != "postgres" && protocol != "redis" && protocol != "tcp" {
    return "", fmt.Errorf("unsupported database protocol %q", protocol)
}
```

- [ ] **Step 2: 在 upstreamAddress 中添加 Redis 默认端口**

修改 `internal/server/dbproxy/server.go` 第 109-119 行的 `upstreamAddress` 函数：

将：
```go
func upstreamAddress(inst model.DatabaseInstance) string {
    port := inst.Port
    if port == 0 {
        if inst.Protocol == "postgres" {
            port = 5432
        } else {
            port = 3306
        }
    }
    return net.JoinHostPort(inst.Address, strconv.Itoa(port))
}
```

改为：
```go
func upstreamAddress(inst model.DatabaseInstance) string {
    port := inst.Port
    if port == 0 {
        switch inst.Protocol {
        case "postgres":
            port = 5432
        case "redis":
            port = 6379
        default:
            port = 3306
        }
    }
    return net.JoinHostPort(inst.Address, strconv.Itoa(port))
}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

预期：编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/store/dbstore_databases.go internal/server/dbproxy/server.go
git commit -m "feat: add Redis protocol support to normalizeDBProtocol and upstreamAddress"
```

---

### Task 3: 创建 Redis 代理处理器（RESP 解析 + handleRedis）

**Files:**
- Create: `internal/server/dbproxy/redis.go`

**Interfaces:**
- Consumes: `Gateway` 结构体、`gatewayConn`、`resolvedDBAccount` 类型、`util.ParseCompactUsername`、`model.DatabaseAccount`
- Produces: `readRESPCommand(conn net.Conn) (cmd string, args []string, raw []byte, err error)`、`writeRESPSimple(conn net.Conn, s string) error`、`writeRESPError(conn net.Conn, msg string) error` 辅助函数，以及 `(g *Gateway) handleRedis(ctx context.Context, client net.Conn, firstByte byte) *gatewayConn`

- [ ] **Step 1: 创建 redis.go 文件**

文件内容：

```go
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
// 1. Read the first RESP command (expected AUTH)
// 2. Extract compact username (R prefix) and bastion password
// 3. Resolve account via compact username lookup
// 4. Validate bastion user password
// 5. RBAC + account/instance status checks
// 6. Connect to upstream Redis
// 7. Authenticate to upstream with stored credentials (ACL or legacy)
// 8. Relay upstream response to client
// 9. Return gatewayConn for transparent data relay
func (g *Gateway) handleRedis(ctx context.Context, client net.Conn, firstByte byte) *gatewayConn {
	cmd, args, _, err := readRESPCommandFromBuf(client, firstByte)
	if err != nil {
		g.logger.Warn("redis gateway failed to read initial command", "error", err)
		return nil
	}

	if cmd != "AUTH" {
		// Redis clients may send HELLO first (Redis 6+)
		if cmd == "HELLO" {
			// Read the next command which should be AUTH
			cmd2, args2, _, err2 := readRESPCommand(client)
			if err2 != nil || cmd2 != "AUTH" {
				g.logger.Warn("redis gateway expected AUTH after HELLO", "cmd", cmd2, "error", err2)
				return nil
			}
			cmd, args = cmd2, args2
		} else {
			g.logger.Warn("redis gateway expected AUTH, got", "cmd", cmd)
			writeRESPError(client, "NOAUTH Authentication required.")
			return nil
		}
	}

	// Parse AUTH arguments: AUTH [username] password
	// Redis 6+ ACL: AUTH username password (2 args)
	// Legacy: AUTH password (1 arg)
	var compactUser, bastionPassword string
	switch len(args) {
	case 1:
		bastionPassword = args[0]
	case 2:
		compactUser = args[0]
		bastionPassword = args[1]
	default:
		g.logger.Warn("redis gateway invalid AUTH args", "count", len(args))
		writeRESPError(client, "invalid password")
		return nil
	}

	// If compactUser not provided by ACL-style AUTH, extract from password arg
	// (client sends: AUTH <compact_username> <bastion_password>)
	// If only 1 arg, client sent compact username as sole argument, so compactUser is the first arg
	if compactUser == "" && len(args) == 1 {
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
	if err := g.validateUserPassword(resolved.user, []byte(bastionPassword)); err != nil {
		g.logger.Warn("redis gateway auth failed", "user", resolved.rawName, "error", err)
		writeRESPError(client, "WRONGPASS invalid username-password pair or user is disabled.")
		return nil
	}
	userID := resolved.user.ID

	// RBAC check
	resourceID := dbAccountResourceID(acct)
	if err := g.authorizeConnect(userID, resolved.rawName, resourceID); err != nil {
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
		protocol: "redis", accountID: acct.ID, accountName: resolved.rawName,
		upstream: upstream, upstreamAddr: upstreamAddress(acct.Instance), userID: userID,
		accountUser: acct.Username, instanceName: acct.Instance.Name,
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
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

预期：编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/server/dbproxy/redis.go
git commit -m "feat: add Redis proxy handler with RESP parser and AUTH relay"
```

---

### Task 4: 添加 Redis 观察器到 observer.go

**Files:**
- Modify: `internal/server/dbproxy/observer.go:22-31`（newQueryObserver 添加 case "redis"）
- Modify: `internal/server/dbproxy/observer.go`（文件末尾新增 redisObserver 结构体）

**Interfaces:**
- Consumes: `queryObserver` 接口、`querySink` 接口、RESP 命令格式
- Produces: `redisObserver` 实现 `queryObserver`

- [ ] **Step 1: 在 newQueryObserver 中添加 case "redis"**

修改 `internal/server/dbproxy/observer.go` 第 22-31 行：

将：
```go
func newQueryObserver(protocol string, sink querySink) queryObserver {
    switch protocol {
    case "mysql":
        return &mysqlObserver{sink: sink}
    case "postgres":
        return &postgresObserver{sink: sink, startupDone: true}
    default:
        return noopObserver{}
    }
}
```

改为：
```go
func newQueryObserver(protocol string, sink querySink) queryObserver {
    switch protocol {
    case "mysql":
        return &mysqlObserver{sink: sink}
    case "postgres":
        return &postgresObserver{sink: sink, startupDone: true}
    case "redis":
        return &redisObserver{sink: sink}
    default:
        return noopObserver{}
    }
}
```

- [ ] **Step 2: 在 observer.go 文件末尾添加 redisObserver**

在 `internal/server/dbproxy/observer.go` 文件末尾（第 521 行之后）新增：

```go
type redisObserver struct {
	sink    querySink
	buf     []byte
	pending []queryRecord
}

func (o *redisObserver) ObserveClientBytes(data []byte) *queryDecision {
	if len(data) == 0 || o.sink == nil {
		return nil
	}
	o.buf = append(o.buf, data...)
	for {
		if len(o.buf) == 0 {
			return nil
		}
		// RESP 命令必须以 '*' 开头
		if o.buf[0] != '*' {
			// 非 RESP 命令，有可能在 AUTH 之后，但 RESP 都是 * 开头
			// 跳过无法识别的字节
			o.buf = nil
			return nil
		}
		// 找到第一个 \r\n 获取参数数量
		crlfIdx := indexCRLF(o.buf)
		if crlfIdx < 0 {
			// 等待更多数据
			if len(o.buf) > 64*1024 {
				o.buf = nil
			}
			return nil
		}
		count, err := strconv.Atoi(string(o.buf[1:crlfIdx]))
		if err != nil || count <= 0 || count > 256 {
			o.buf = nil
			return nil
		}
		pos := crlfIdx + 2
		args := make([]string, 0, count)
		valid := true
		for i := 0; i < count; i++ {
			if pos >= len(o.buf) || o.buf[pos] != '$' {
				valid = false
				break
			}
			crlf := indexCRLF(o.buf[pos:])
			if crlf < 0 {
				if len(o.buf) > 64*1024 {
					o.buf = nil
				}
				return nil
			}
			argLen, err := strconv.Atoi(string(o.buf[pos+1 : pos+crlf]))
			if err != nil || argLen < 0 || argLen > 512*1024*1024 {
				valid = false
				break
			}
			pos += crlf + 2
			dataEnd := pos + argLen + 2 // arg + \r\n
			if dataEnd > len(o.buf) {
				// 等待更多数据
				return nil
			}
			args = append(args, string(o.buf[pos:pos+argLen]))
			pos = dataEnd
		}
		if !valid {
			o.buf = nil
			return nil
		}

		// 记录命令（跳过 AUTH、PING、QUIT 等非数据命令）
		cmd := ""
		if len(args) > 0 {
			cmd = strings.ToUpper(args[0])
		}
		if shouldRecordRedisCommand(cmd) {
			sql := strings.Join(args, " ")
			record, decision := o.sink.StartQuery(sql, map[string]any{
				"protocol": "redis",
				"command":  cmd,
			})
			if !decision.Allowed {
				return &decision
			}
			o.pending = append(o.pending, record)
		}
		o.buf = o.buf[pos:]
	}
}

func (o *redisObserver) ObserveServerBytes(data []byte) {
	if len(data) == 0 || len(o.pending) == 0 || o.sink == nil {
		return
	}
	// Match RESP response prefixes
	if len(data) > 0 {
		var finish queryFinish
		switch data[0] {
		case '+':
			finish = queryFinish{Status: queryStatusSuccess}
		case '-':
			finish = queryFinish{Status: queryStatusError}
			errMsg := strings.TrimSpace(string(data[1:]))
			if idx := strings.IndexAny(errMsg, "\r\n"); idx >= 0 {
				errMsg = errMsg[:idx]
			}
			// Parse Redis error: "-ERR message" or "-WRONGTYPE ..."
			if strings.HasPrefix(errMsg, "ERR ") {
				errMsg = errMsg[4:]
			}
			finish.ErrorMessage = errMsg
		case ':':
			finish = queryFinish{Status: queryStatusSuccess}
		case '$':
			finish = queryFinish{Status: queryStatusSuccess}
		case '*':
			finish = queryFinish{Status: queryStatusSuccess}
		default:
			return // not a complete response header we recognize
		}
		record := o.pending[0]
		o.pending = o.pending[1:]
		o.sink.FinishQuery(record, finish)
	}
}

func (o *redisObserver) ErrorResponse(decision queryDecision) []byte {
	message := decision.ErrorMessage
	if message == "" {
		message = "command denied by database proxy policy"
	}
	return []byte(fmt.Sprintf("-ERR %s\r\n", message))
}

// shouldRecordRedisCommand returns true for data commands (skips AUTH, PING, QUIT, etc.)
func shouldRecordRedisCommand(cmd string) bool {
	switch cmd {
	case "AUTH", "PING", "QUIT", "ECHO", "SELECT", "HELLO", "COMMAND":
		return false
	default:
		return true
	}
}

// indexCRLF returns the index of first \r\n in b, or -1
func indexCRLF(b []byte) int {
	for i := 0; i < len(b)-1; i++ {
		if b[i] == '\r' && b[i+1] == '\n' {
			return i
		}
	}
	return -1
}
```

注意：需要在 observer.go 文件头部导入中添加 `"fmt"` 和 `"strconv"`（如果还未导入的话）。检查现有 imports。

- [ ] **Step 2b: 检查 imports**

查看 observer.go 头部 imports 是否已有 `fmt` 和 `strconv`。

`observer.go` 第 4-9 行的 imports 有：
```go
import (
    "bytes"
    "encoding/binary"
    "fmt"
    "strings"
    "unicode"
)
```

需要添加 `"strconv"`：

```go
import (
    "bytes"
    "encoding/binary"
    "fmt"
    "strconv"
    "strings"
    "unicode"
)
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

预期：编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/server/dbproxy/observer.go
git commit -m "feat: add redisObserver for RESP command auditing"
```

---

### Task 5: 协议检测中新增 Redis 分支

**Files:**
- Modify: `internal/server/dbproxy/server.go:137-144`

**Interfaces:**
- Consumes: `handleRedis` 方法（Task 3 已定义）
- Produces: `handleConn` 中 `case '*'` → `handleRedis`

- [ ] **Step 1: 在 handleConn switch 中添加 case '*'**

修改 `internal/server/dbproxy/server.go` 第 138-144 行：

将：
```go
switch {
case firstByte[0] == 0x00:
    conn = g.handlePG(ctx, client, firstByte[0])
default:
    g.logger.Warn("db gateway unsupported protocol", "first_byte", firstByte[0])
    return
}
```

改为：
```go
switch {
case firstByte[0] == 0x00:
    conn = g.handlePG(ctx, client, firstByte[0])
case firstByte[0] == '*':
    conn = g.handleRedis(ctx, client, firstByte[0])
default:
    g.logger.Warn("db gateway unsupported protocol", "first_byte", firstByte[0])
    return
}
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

预期：编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/server/dbproxy/server.go
git commit -m "feat: add Redis protocol detection in handleConn"
```

---

### Task 6: 测试连接支持 Redis

**Files:**
- Modify: `internal/server/admin/test_db.go:140-149`

**Interfaces:**
- Consumes: RESP AUTH 协议
- Produces: `testRedisAuth` 函数

- [ ] **Step 1: 在 testDBAuth 中添加 case "redis"**

修改 `internal/server/admin/test_db.go` 第 141-148 行的 `testDBAuth` 函数：

将：
```go
func testDBAuth(conn net.Conn, protocol, username, password string) error {
    switch strings.ToLower(protocol) {
    case "postgres", "postgresql":
        return testPostgresAuth(conn, username, password)
    case "mysql":
        return testMySQLAuth(conn, username, password)
    default:
        return fmt.Errorf("unsupported protocol %q", protocol)
    }
}
```

改为：
```go
func testDBAuth(conn net.Conn, protocol, username, password string) error {
    switch strings.ToLower(protocol) {
    case "postgres", "postgresql":
        return testPostgresAuth(conn, username, password)
    case "mysql":
        return testMySQLAuth(conn, username, password)
    case "redis":
        return testRedisAuth(conn, username, password)
    default:
        return fmt.Errorf("unsupported protocol %q", protocol)
    }
}
```

- [ ] **Step 2: 在文件末尾添加 testRedisAuth 函数**

在 `internal/server/admin/test_db.go` 文件末尾添加：

```go
// testRedisAuth 测试 Redis 上游认证。
// 使用 ACL 模式（AUTH username password），若 username 为空则使用单密码模式。
func testRedisAuth(conn net.Conn, username, password string) error {
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
		return fmt.Errorf("auth: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	resp := strings.TrimSpace(string(buf[:n]))
	if strings.HasPrefix(resp, "+") {
		return nil
	}
	if strings.HasPrefix(resp, "-") {
		return fmt.Errorf("auth denied: %s", resp[1:])
	}
	return fmt.Errorf("unexpected auth response: %s", resp)
}
```

注意：需要添加 `"strings"` 到 `test_db.go` 的 import（如果还没有的话）。查看第 4-16 行的 imports 已有 `"strings"`。

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

预期：编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/server/admin/test_db.go
git commit -m "feat: add Redis test connection authentication"
```

---

### Task 7: 更新前端 DatabaseView.vue

**Files:**
- Modify: `web/src/views/DatabaseView.vue`

**Interfaces:**
- Consumes: 无新增 API，复用现有 `createUserSession`、`getDBGateway` 等
- Produces: 协议下拉加 Redis 选项、连接命令支持 redis-cli

- [ ] **Step 1: 协议下拉加 Redis 选项（第 51-53 行）**

将：
```html
<el-option label="MySQL" value="mysql" />
<el-option label="PostgreSQL" value="postgres" />
```

改为：
```html
<el-option label="MySQL" value="mysql" />
<el-option label="PostgreSQL" value="postgres" />
<el-option label="Redis" value="redis" />
```

- [ ] **Step 2: 协议标签显示支持 Redis（第 20 行）**

将：
```html
<el-tag size="small" :type="row.protocol === 'mysql' ? 'success' : 'primary'" effect="plain">{{ row.protocol === 'mysql' ? 'MySQL' : 'PG' }}</el-tag>
```

改为：
```html
<el-tag size="small" :type="row.protocol === 'mysql' ? 'success' : row.protocol === 'redis' ? 'danger' : 'primary'" effect="plain">{{ row.protocol === 'mysql' ? 'MySQL' : row.protocol === 'redis' ? 'Redis' : 'PG' }}</el-tag>
```

- [ ] **Step 3: compactUser 计算使用 R 前缀（第 375-380 行）**

将：
```typescript
const compactUser = computed(() => {
  if (!connectTarget.value) return ''
  const resourceId = connectTarget.value.resource_id || '0000'
  const sessionId = userSessionId.value
  return sessionId ? 'D' + resourceId + sessionId : ''
})
```

改为：
```typescript
const compactUser = computed(() => {
  if (!connectTarget.value) return ''
  const resourceId = connectTarget.value.resource_id || '0000'
  const sessionId = userSessionId.value
  if (!sessionId) return ''
  const inst = selectedInstance.value
  const prefix = inst?.protocol === 'redis' ? 'R' : 'D'
  return prefix + resourceId + sessionId
})
```

- [ ] **Step 4: connectCommand 支持 Redis（第 384-397 行）**

将：
```typescript
const connectCommand = computed(() => {
  if (!connectTarget.value || !selectedInstance.value) return ''
  const inst = selectedInstance.value
  const protocol = inst.protocol || 'mysql'
  const resourceId = connectTarget.value.resource_id || '0000'
  const sessionId = userSessionId.value || '00001'
  const compactUser = `D${resourceId}${sessionId}`
  const host = gatewayConfig.value.host
  const proxyPort = gatewayConfig.value.port
  if (protocol === 'mysql') {
    return `mysql --protocol=tcp -h ${host} -P ${proxyPort} -u ${compactUser} -p`
  }
  return `psql -h ${host} -p ${proxyPort} -U ${compactUser}`
})
```

改为：
```typescript
const connectCommand = computed(() => {
  if (!connectTarget.value || !selectedInstance.value) return ''
  const inst = selectedInstance.value
  const protocol = inst.protocol || 'mysql'
  const resourceId = connectTarget.value.resource_id || '0000'
  const sessionId = userSessionId.value || '00001'
  const prefix = protocol === 'redis' ? 'R' : 'D'
  const compactUser = `${prefix}${resourceId}${sessionId}`
  const host = gatewayConfig.value.host
  const proxyPort = gatewayConfig.value.port
  if (protocol === 'mysql') {
    return `mysql --protocol=tcp -h ${host} -P ${proxyPort} -u ${compactUser} -p`
  }
  if (protocol === 'redis') {
    return `redis-cli -h ${host} -p ${proxyPort} -a ${compactUser} --user ${connectTarget.value.username || 'default'}`
  }
  return `psql -h ${host} -p ${proxyPort} -U ${compactUser}`
})
```

- [ ] **Step 5: Redis 账号用户名字段可选**

修改 `submitAccount` 中的校验逻辑（第 680 行附近），将 username 必填限制为仅非 Redis 账号时：

修改第 680-681 行：

将：
```typescript
async function submitAccount() {
  if (!accountForm.username.trim()) { ElMessage.warning('请输入目标用户名'); return }
```

改为：
```typescript
async function submitAccount() {
  const isRedis = selectedInstance.value?.protocol === 'redis'
  if (!isRedis && !accountForm.username.trim()) { ElMessage.warning('请输入目标用户名'); return }
```

同时修改账号表单中的 label（第 137-138 行）：

将：
```html
<el-form-item label="目标用户名" required>
  <el-input v-model="accountForm.username" placeholder="数据库登录用户名" />
</el-form-item>
```

改为：
```html
<el-form-item :label="selectedInstance?.protocol === 'redis' ? '目标用户名' : '目标用户名'" :required="selectedInstance?.protocol !== 'redis'">
  <el-input v-model="accountForm.username" :placeholder="selectedInstance?.protocol === 'redis' ? 'Redis ACL 用户名（可选，留空则使用单一密码认证）' : '数据库登录用户名'" />
</el-form-item>
```

- [ ] **Step 6: 修改默认端口逻辑**

修改 `openCreateInstance` 函数的端口默认值（第 476-487 行），根据协议动态设置：

修改 `submitInstance` 之后，添加一个 watch 或计算属性。更简单的方法是修改 `openCreateInstance` 使端口初始化为 3306，然后在用户选择协议后再调整。但 element-plus 的 select 没有直接事件绑定。我们改为在 submitInstance 时补充默认端口。

更简单的做法：在实例表单的端口字段处添加 placeholder 提示默认端口。当前已经有端口输入框，用户能看到即可。

保持端口默认值为 3306 不变，用户切换协议到 Redis 时手动输入 6379。已有端口字段，无需特殊处理。

- [ ] **Step 7: 运行前端验证**

```bash
cd web && npm run typecheck && npm run build
```

预期：typecheck 和 build 均通过

- [ ] **Step 8: Commit**

```bash
git add web/src/views/DatabaseView.vue
git commit -m "feat: add Redis protocol support to database management UI"
```

---

### Task 8: 后端测试和最终验证

**Files:**
- 验证所有改动的文件

- [ ] **Step 1: 运行所有后端测试**

```bash
go build ./...
go test ./... -count=1
```

预期：编译通过，所有已有测试通过

- [ ] **Step 2: 针对性运行 dbproxy 测试**

```bash
go test ./internal/server/dbproxy/... -count=1 -v
```

预期：所有测试通过

- [ ] **Step 3: 检查文件行数上限**

新增 `redis.go` 预计约 250 行，在单文件上限 600 行以内。

- [ ] **Step 4: 最终 commit（如有必要）**

如果通过了所有测试且不需要修改，此步骤可跳过。如有小修复，在此提交。
