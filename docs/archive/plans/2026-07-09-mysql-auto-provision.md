# MySQL 一键自动创建账号 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 管理员在堡垒机页面一键连接目标 MySQL 执行 `SHOW DATABASES` → 选择数据库和权限 → 自动执行 `CREATE USER` + `GRANT` → 注册到堡垒机。

**Architecture:** 新增 service 层处理 MySQL 协议通信（`SHOW DATABASES` / `CREATE USER` / `GRANT`），复用 `dbproxy` 包已有的 MySQL 认证函数。handler 层在现有 `db_handlers.go` 的 `handleDBInstance` 中新增 `databases` 和 `provision-account` 两个子路由分支。前端在 DatabaseView.vue 新增"自动创建"按钮和三步向导弹窗。

**Tech Stack:** Go 后端 + Vue 3 Composition API + Element Plus

## Global Constraints

- Go 后端分层：handler → service → store，handler 不做业务判断
- 路由通过修改 `dbInstancePathParts` + `handleDBInstance` 分发，注册到现有 `/api/db/instances/` 前缀
- 前端只改 `client.ts`（新增 API 方法）和 `DatabaseView.vue`（新增模板 + 逻辑）
- 单文件行数上限：handler 500、service 500

---

### Task 1: 创建 service 层 — MySQL 连接与 SQL 执行

**Files:**
- Create: `C:\02-codespace\Jianmen\internal\service\db_provision.go`

**Interfaces:**
- Consumes: `model.DatabaseAccount`, `model.DatabaseInstance`, `dbproxy.ParseMySQLHandshake`, `dbproxy.BuildMySQLUpstreamLogin`, `dbproxy.ParseMySQLErrorMessage`, `dbproxy.BuildMySQLAuthResponse`, `dbproxy.BuildMySQLCachingSha2Password`, `dbproxy.BuildMySQLNativePassword`
- Produces: `func ListMySQLDatabases(instance model.DatabaseInstance, adminAccount model.DatabaseAccount) ([]string, error)`, `func ProvisionMySQLAccount(instance model.DatabaseInstance, adminAccount model.DatabaseAccount, newUser, password, host string, grants []DBGrant) error`, `type DBGrant struct { Database, Privilege string }`

- [ ] **Step 1: 创建 `internal/service/db_provision.go`**

```go
package service

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/server/dbproxy"
)

type DBGrant struct {
	Database  string `json:"database"`
	Privilege string `json:"privilege"` // "read" | "readwrite"
}

func grantSQL(db, privilege, user, host string) string {
	switch privilege {
	case "read":
		return fmt.Sprintf("GRANT SELECT ON `%s`.* TO '%s'@'%s'", db, user, host)
	case "readwrite":
		return fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON `%s`.* TO '%s'@'%s'", db, user, host)
	default:
		return ""
	}
}

// mysqlConnect 连接目标 MySQL 并完成认证，返回已认证的连接。
func mysqlConnect(addr string, username, password string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n < 4 {
		conn.Close()
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	hsPayloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if 4+hsPayloadLen > n {
		remaining := make([]byte, 4+hsPayloadLen-n)
		if _, err := io.ReadFull(conn, remaining); err != nil {
			conn.Close()
			return nil, fmt.Errorf("read handshake payload: %w", err)
		}
		buf = append(buf[:n], remaining...)
	}
	hsPayload := buf[4 : 4+hsPayloadLen]
	hs, err := dbproxy.ParseMySQLHandshake(hsPayload)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("parse handshake: %w", err)
	}
	loginPkt := dbproxy.BuildMySQLUpstreamLogin(hs, username, password, hs.AuthPluginName, 1)
	if _, err := conn.Write(loginPkt); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write login: %w", err)
	}
	return readMySQLAuthResult(conn, hs, password, buf)
}

// readMySQLAuthResult 读取 MySQL 登录响应，处理 OK/ERR/AuthSwitch/caching_sha2 fast auth。
func readMySQLAuthResult(conn net.Conn, hs *dbproxy.MySQLHandshake, password string, buf []byte) (net.Conn, error) {
	n, err := conn.Read(buf)
	if err != nil || n < 4 {
		conn.Close()
		return nil, fmt.Errorf("read auth response: %w", err)
	}
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	// OK
	if len(buf) >= 4+payloadLen && buf[4] == 0x00 {
		return conn, nil
	}
	// ERR
	if len(buf) >= 4+payloadLen && buf[4] == 0xff {
		conn.Close()
		return nil, fmt.Errorf("auth denied: %s", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	}
	// AuthSwitchRequest (0xfe)
	if len(buf) >= 4+payloadLen && buf[4] == 0xfe {
		payload := buf[5 : 4+payloadLen]
		nullPos := 0
		for nullPos < len(payload) && payload[nullPos] != 0 {
			nullPos++
		}
		newPlugin := string(payload[:nullPos])
		authData := payload[nullPos+1:]
		if len(authData) > 0 {
			hs.AuthData = authData
		}
		authRespBytes := dbproxy.BuildMySQLAuthResponse(newPlugin, password, authData)
		if authRespBytes == nil {
			conn.Close()
			return nil, fmt.Errorf("unsupported auth switch plugin: %s", newPlugin)
		}
		resp := make([]byte, 4+len(authRespBytes))
		resp[0] = byte(len(authRespBytes))
		resp[1] = byte(len(authRespBytes) >> 8)
		resp[2] = byte(len(authRespBytes) >> 16)
		resp[3] = 3 // seq after login
		copy(resp[4:], authRespBytes)
		if _, err := conn.Write(resp); err != nil {
			conn.Close()
			return nil, fmt.Errorf("write auth switch: %w", err)
		}
		return readSimpleAuthResult(conn, buf)
	}
	// caching_sha2_password fast auth 第二阶段: 0x01 -> read again for 0x03 (fast auth success) or 0x04 (full auth)
	if len(buf) >= 4+payloadLen && buf[4] == 0x01 {
		n2, err := conn.Read(buf)
		if err != nil || n2 < 4 {
			conn.Close()
			return nil, fmt.Errorf("read auth phase 2: %w", err)
		}
		payloadLen2 := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
		if len(buf) >= 4+payloadLen2 && buf[4] == 0x04 {
			// Server requests full auth: send password in cleartext over TLS (assumes non-TLS cleartext for now)
			pwdBytes := []byte(password)
			pwdPkt := make([]byte, 4+len(pwdBytes)+1)
			pwdPkt[0] = byte(len(pwdBytes) + 1)
			pwdPkt[1] = byte((len(pwdBytes) + 1) >> 8)
			pwdPkt[2] = byte((len(pwdBytes) + 1) >> 16)
			pwdPkt[3] = 3
			copy(pwdPkt[4:], pwdBytes)
			if _, err := conn.Write(pwdPkt); err != nil {
				conn.Close()
				return nil, fmt.Errorf("write full auth password: %w", err)
			}
			return readSimpleAuthResult(conn, buf)
		}
		if len(buf) >= 4+payloadLen2 && (buf[4] == 0x03 || buf[4] == 0x00) {
			return conn, nil
		}
		conn.Close()
		return nil, fmt.Errorf("auth phase 2 failed: payload[4]=0x%02x", buf[4])
	}
	conn.Close()
	return nil, fmt.Errorf("unexpected auth result: payload[4]=0x%02x", buf[4])
}

func readSimpleAuthResult(conn net.Conn, buf []byte) (net.Conn, error) {
	n, err := conn.Read(buf)
	if err != nil || n < 4 {
		conn.Close()
		return nil, fmt.Errorf("read auth result: %w", err)
	}
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if len(buf) >= 4+payloadLen && buf[4] == 0x00 {
		return conn, nil
	}
	if len(buf) >= 4+payloadLen && buf[4] == 0xff {
		conn.Close()
		return nil, fmt.Errorf("auth denied: %s", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	}
	conn.Close()
	return nil, fmt.Errorf("unexpected result: payload[4]=0x%02x", buf[4])
}

// mysqlQuery 发送 SQL 查询并读取完整结果集（仅用于 SHOW DATABASES 类简单查询）。
func mysqlQuery(conn net.Conn, query string) ([][]string, error) {
	payload := make([]byte, 1+len(query))
	payload[0] = 0x03 // COM_QUERY
	copy(payload[1:], query)
	pkt := mysqlPacketWithSeq(1, payload)
	if _, err := conn.Write(pkt); err != nil {
		return nil, fmt.Errorf("write query: %w", err)
	}

	buf := make([]byte, 65536)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read query response: %w", err)
	}
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if len(buf) >= 4+payloadLen && buf[4] == 0xff {
		return nil, fmt.Errorf("query error: %s", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	}

	colCount, offset := readLenEncInt(buf[4:])
	if colCount == 0 {
		return nil, nil
	}

	// Read column definitions + EOF
	for i := uint64(0); i < colCount; i++ {
		if _, err := readMySQLPacketFromConn(conn); err != nil {
			return nil, fmt.Errorf("read column def %d: %w", i, err)
		}
	}
	if _, err := readMySQLPacketFromConn(conn); err != nil {
		return nil, fmt.Errorf("read columns EOF: %w", err)
	}

	// Read row data
	var rows [][]string
	for {
		pkt2, err := readMySQLPacketFromConn(conn)
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		if len(pkt2.payload) > 0 && pkt2.payload[0] == 0xfe {
			break // EOF
		}
		if len(pkt2.payload) > 0 && pkt2.payload[0] == 0xff {
			return nil, fmt.Errorf("query error: %s", dbproxy.ParseMySQLErrorMessage(pkt2.payload))
		}
		rows = append(rows, parseMySQLTextRow(pkt2.payload))
	}
	return rows, nil
}

// mysqlExec 发送非查询 SQL（CREATE USER, GRANT 等）。
func mysqlExec(conn net.Conn, stmt string) error {
	payload := make([]byte, 1+len(stmt))
	payload[0] = 0x03
	copy(payload[1:], stmt)
	pkt := mysqlPacketWithSeq(1, payload)
	if _, err := conn.Write(pkt); err != nil {
		return fmt.Errorf("write exec: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("read exec response: %w", err)
	}
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if len(buf) >= 4+payloadLen && buf[4] == 0xff {
		return fmt.Errorf("%s", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	}
	// OK packet always starts with 0x00
	if len(buf) >= 4+payloadLen && buf[4] == 0x00 {
		return nil
	}
	return fmt.Errorf("unexpected exec result: payload[4]=0x%02x", buf[4])
}

// ListMySQLDatabases 连接 MySQL 实例并执行 SHOW DATABASES。
func ListMySQLDatabases(instance model.DatabaseInstance, adminAccount model.DatabaseAccount) ([]string, error) {
	addr := instance.Address
	if instance.Port > 0 {
		addr = fmt.Sprintf("%s:%d", instance.Address, instance.Port)
	}
	plainPwd := adminAccount.Password.GetPlaintext()
	if plainPwd == "" {
		return nil, errors.New("admin account password is empty")
	}
	conn, err := mysqlConnect(addr, adminAccount.Username, plainPwd)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := mysqlQuery(conn, "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	dbs := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 && row[0] != "" {
			dbs = append(dbs, row[0])
		}
	}
	return dbs, nil
}

// ProvisionMySQLAccount 连接 MySQL 创建用户、授权。
func ProvisionMySQLAccount(instance model.DatabaseInstance, adminAccount model.DatabaseAccount, newUser, password, host string, grants []DBGrant) error {
	addr := instance.Address
	if instance.Port > 0 {
		addr = fmt.Sprintf("%s:%d", instance.Address, instance.Port)
	}
	plainPwd := adminAccount.Password.GetPlaintext()
	if plainPwd == "" {
		return errors.New("admin account password is empty")
	}
	conn, err := mysqlConnect(addr, adminAccount.Username, plainPwd)
	if err != nil {
		return err
	}
	defer conn.Close()

	createSQL := fmt.Sprintf("CREATE USER '%s'@'%s' IDENTIFIED BY '%s'",
		escapeMySQLString(newUser), escapeMySQLString(host), escapeMySQLString(password))
	if err := mysqlExec(conn, createSQL); err != nil {
		return fmt.Errorf("CREATE USER: %w", err)
	}

	for _, g := range grants {
		sql := grantSQL(g.Database, g.Privilege, newUser, host)
		if sql == "" {
			continue
		}
		if err := mysqlExec(conn, sql); err != nil {
			return fmt.Errorf("GRANT %s on %s: %w", g.Privilege, g.Database, err)
		}
	}
	_ = mysqlExec(conn, "FLUSH PRIVILEGES")
	return nil
}

func escapeMySQLString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, c := range s {
		switch c {
		case '\'', '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(c)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// --- MySQL protocol helpers ---

func mysqlPacketWithSeq(seq byte, payload []byte) []byte {
	packet := make([]byte, 4+len(payload))
	packet[0] = byte(len(payload))
	packet[1] = byte(len(payload) >> 8)
	packet[2] = byte(len(payload) >> 16)
	packet[3] = seq
	copy(packet[4:], payload)
	return packet
}

type mysqlPkt struct {
	payload []byte
	seq     byte
}

func readMySQLPacketFromConn(conn net.Conn) (*mysqlPkt, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	payloadLen := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return &mysqlPkt{payload: payload, seq: header[3]}, nil
}

func readLenEncInt(data []byte) (uint64, int) {
	if len(data) == 0 {
		return 0, 0
	}
	v := data[0]
	if v < 0xfb {
		return uint64(v), 1
	}
	if v == 0xfc && len(data) >= 3 {
		return uint64(data[1]) | uint64(data[2])<<8, 3
	}
	if v == 0xfd && len(data) >= 4 {
		return uint64(data[1]) | uint64(data[2])<<8 | uint64(data[3])<<16, 4
	}
	if v == 0xfe && len(data) >= 9 {
		return binary.LittleEndian.Uint64(data[1:9]), 9
	}
	return 0, 0
}

func parseMySQLTextRow(payload []byte) []string {
	pos := 0
	var cols []string
	for pos < len(payload) {
		if payload[pos] == 0xfb {
			cols = append(cols, "")
			pos++
			continue
		}
		strLen, offset := readLenEncInt(payload[pos:])
		pos += offset
		end := pos + int(strLen)
		if end > len(payload) {
			end = len(payload)
		}
		cols = append(cols, string(payload[pos:end]))
		pos = end
	}
	return cols
}
```

- [ ] **Step 2: 验证编译**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/service/db_provision.go
git commit -m "feat: add MySQL provision service layer

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 2: handler 层 和路由修改

**Files:**
- Create: `C:\02-codespace\Jianmen\internal\server\admin\db_provision.go`
- Modify: `C:\02-codespace\Jianmen\internal\server\admin\request_utils.go:115-128`（`dbInstancePathParts` 增加 `databases` 和 `provision-account`）
- Modify: `C:\02-codespace\Jianmen\internal\server\admin\db_handlers.go:126-148`（`handleDBInstance` 增加子路由分发）

**Interfaces:**
- Consumes: `service.ListMySQLDatabases`, `service.ProvisionMySQLAccount`, `service.DBGrant`, `s.db`, `s.store.AddDatabaseAccount`, `s.requirePermission`, `rbac.ActionDBProxyView`, `rbac.ActionDBProxyCreate`
- Produces: handler 函数 `handleDBDatabases(w, r, instanceID)`, `handleDBProvisionAccount(w, r, instanceID)`, 辅助函数 `generateRandomPassword(length int) string`

- [ ] **Step 1: 修改 `dbInstancePathParts` 在 `request_utils.go` 第 124 行**

将现有:
```go
if len(parts) == 2 && parts[1] == "accounts" {
    return parts[0], parts[1], true
}
```

改为:
```go
if len(parts) == 2 {
    switch parts[1] {
    case "accounts", "databases", "provision-account":
        return parts[0], parts[1], true
    }
}
```

- [ ] **Step 2: 修改 `handleDBInstance` 在 `db_handlers.go` 第 149 行**

在 `if child != "" {` (第 150 行) 之前插入新的 child 分支:

```go
if child == "databases" {
    if r.Method != http.MethodGet {
        w.Header().Set("Allow", http.MethodGet)
        writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }
    s.handleDBDatabases(w, r, id)
    return
}
if child == "provision-account" {
    if r.Method != http.MethodPost {
        w.Header().Set("Allow", http.MethodPost)
        writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }
    s.handleDBProvisionAccount(w, r, id)
    return
}
```

插入位置: `db_handlers.go` 第 149 行 `if child != "" {` 之前（accounts 分支的 `return` 之后）。

- [ ] **Step 3: 创建 `internal/server/admin/db_provision.go`**

```go
package admin

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

// handleDBDatabases handles GET /api/db/instances/{id}/databases
func (s *Server) handleDBDatabases(w http.ResponseWriter, r *http.Request, instanceID string) {
	if !s.requirePermission(r, rbac.ActionDBProxyView) {
		s.forbidden(w)
		return
	}

	adminAccountID := strings.TrimSpace(r.URL.Query().Get("admin_account_id"))
	if adminAccountID == "" {
		writeErrorText(w, http.StatusBadRequest, "admin_account_id is required")
		return
	}

	var acct model.DatabaseAccount
	if err := s.db.Preload("Instance").First(&acct, "id = ? AND instance_id = ?", adminAccountID, instanceID).Error; err != nil {
		writeErrorText(w, http.StatusNotFound, "admin account not found")
		return
	}
	if acct.Status != "active" {
		writeErrorText(w, http.StatusBadRequest, "admin account is not active")
		return
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		writeErrorText(w, http.StatusBadRequest, "admin account has expired")
		return
	}
	if acct.Instance.Status != "active" {
		writeErrorText(w, http.StatusBadRequest, "database instance is disabled")
		return
	}
	if acct.Instance.Protocol != "mysql" {
		writeErrorText(w, http.StatusBadRequest, "only mysql instances support auto-provisioning")
		return
	}

	dbs, err := service.ListMySQLDatabases(acct.Instance, acct)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"databases": dbs})
}

// handleDBProvisionAccount handles POST /api/db/instances/{id}/provision-account
func (s *Server) handleDBProvisionAccount(w http.ResponseWriter, r *http.Request, instanceID string) {
	if !s.requirePermission(r, rbac.ActionDBProxyCreate) {
		s.forbidden(w)
		return
	}

	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var payload struct {
		AdminAccountID string            `json:"admin_account_id"`
		NewUsername    string            `json:"new_username"`
		Password       string            `json:"password"`
		Host           string            `json:"host"`
		Grants         []service.DBGrant `json:"grants"`
		Group          string            `json:"group"`
		Remark         string            `json:"remark"`
		ExpiresAt      *time.Time        `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	payload.AdminAccountID = strings.TrimSpace(payload.AdminAccountID)
	payload.NewUsername = strings.TrimSpace(payload.NewUsername)
	payload.Host = strings.TrimSpace(payload.Host)

	if payload.AdminAccountID == "" {
		writeErrorText(w, http.StatusBadRequest, "admin_account_id is required")
		return
	}
	if payload.Host == "" {
		payload.Host = "%"
	}

	// 生成密码
	generatedPassword := payload.Password
	if generatedPassword == "" {
		generatedPassword = generateRandomPassword(20)
	}

	// 生成用户名
	newUsername := payload.NewUsername
	if newUsername == "" {
		newUsername = "u_" + generateRandomPassword(8)
	}

	// 加载管理员凭据（必须属于本实例）
	var acct model.DatabaseAccount
	if err := s.db.Preload("Instance").First(&acct, "id = ? AND instance_id = ?", payload.AdminAccountID, instanceID).Error; err != nil {
		writeErrorText(w, http.StatusNotFound, "admin account not found for this instance")
		return
	}
	if acct.Status != "active" {
		writeErrorText(w, http.StatusBadRequest, "admin account is not active")
		return
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		writeErrorText(w, http.StatusBadRequest, "admin account has expired")
		return
	}
	if acct.Instance.Status != "active" {
		writeErrorText(w, http.StatusBadRequest, "database instance is disabled")
		return
	}
	if acct.Instance.Protocol != "mysql" {
		writeErrorText(w, http.StatusBadRequest, "only mysql protocol is supported for provisioning")
		return
	}

	// 在目标 MySQL 上执行 CREATE USER + GRANT
	if err := service.ProvisionMySQLAccount(acct.Instance, acct, newUsername, generatedPassword, payload.Host, payload.Grants); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	// 注册到堡垒机
	view, err := s.store.AddDatabaseAccount(instanceID, newUsername, generatedPassword, payload.Group, payload.Remark, payload.ExpiresAt)
	if err != nil {
		writeDBStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":                 true,
		"account":            view,
		"generated_password": generatedPassword,
	})
}

func generateRandomPassword(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&()*+,-./:;<=>?@[]^_"
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[n.Int64()]
	}
	return string(result)
}
```

- [ ] **Step 4: 验证编译**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

Expected: 编译成功

- [ ] **Step 5: 提交**

```bash
git add internal/server/admin/db_provision.go internal/server/admin/db_handlers.go internal/server/admin/request_utils.go
git commit -m "feat: add MySQL auto-provision API endpoints

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 3: 前端 — API 客户端

**Files:**
- Modify: `C:\02-codespace\Jianmen\web\src\api\client.ts`

**Interfaces:**
- Produces: `apiClient.listDBDatabases(instanceId, adminAccountId)`, `apiClient.provisionDBAccount(instanceId, payload)`

- [ ] **Step 1: 在 `client.ts` 的 `apiClient` 对象中添加两个新方法**

在 `getDBAccounts` 方法之后（第 629 行附近）添加。`client.ts` 中所有 API 调用都是 `apiClient` 对象上的方法，使用 `request<T>()` 包装。添加在 `getDBAccounts` 和 `createDBAccount` 之间：

```typescript
// 自动创建账号相关
listDBDatabases: (instanceId: string, adminAccountId: string) =>
  request<{ databases: string[] }>(`/api/db/instances/${encodeURIComponent(instanceId)}/databases?admin_account_id=${encodeURIComponent(adminAccountId)}`),

provisionDBAccount: (instanceId: string, payload: {
  admin_account_id: string
  new_username?: string
  password?: string
  host?: string
  grants: Array<{ database: string; privilege: string }>
  group?: string
  remark?: string
  expires_at?: string
}) =>
  request<{ ok: boolean; account: any; generated_password: string }>(
    `/api/db/instances/${encodeURIComponent(instanceId)}/provision-account`,
    {
      method: 'POST',
      body: JSON.stringify(payload),
    }
  ),
```

- [ ] **Step 2: 验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck
```

```bash
cd C:\02-codespace\Jianmen\web && npm run build
```

Expected: 两个命令均成功

- [ ] **Step 3: 提交**

```bash
git add web/src/api/client.ts
git commit -m "feat: add MySQL provision API client methods

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 4: 前端 — DatabaseView.vue 自动创建 UI

**Files:**
- Modify: `C:\02-codespace\Jianmen\web\src\views\DatabaseView.vue`

DatabaseView.vue 已有 `copyText` 函数（连接弹窗中使用）。`api.apiClient.getDBAccounts` 和 `api.apiClient.createDBAccount` 的模式已知。只需添加按钮、弹窗、逻辑。

- [ ] **Step 1: 在工具栏添加"自动创建"按钮**

搜索 `@click="openCreateAccount"` (第 95 行附近)，在其后添加：
```vue
<el-button type="success" :disabled="!selectedInstance || selectedInstance.protocol !== 'mysql'" @click="openAutoProvision">
  自动创建
</el-button>
```

- [ ] **Step 2: 在账号编辑弹窗 `</FormDialog>`（第 199 行）之后添加自动创建弹窗**

```vue
<!-- 自动创建账号弹窗 -->
<el-dialog
  v-model="autoProvisionVisible"
  title="自动创建 MySQL 账号"
  width="min(720px, calc(100vw - 32px))"
  destroy-on-close
  @closed="resetAutoProvision"
>
  <template v-if="provisionStep === 1">
    <el-form label-width="100px">
      <el-form-item label="管理员凭据">
        <el-select v-model="provision.adminAccountId" placeholder="选择用于创建账号的凭据" style="width:100%">
          <el-option
            v-for="acc in adminAccounts"
            :key="acc.id"
            :label="`${acc.username} (${acc.unique_name})`"
            :value="acc.id"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="新用户名">
        <el-input v-model="provision.newUsername" placeholder="留空自动生成" />
      </el-form-item>
      <el-form-item label="主机">
        <el-input v-model="provision.host" placeholder="%" />
      </el-form-item>
    </el-form>
  </template>

  <template v-else-if="provisionStep === 2">
    <div v-if="loadingDatabases" style="text-align:center;padding:30px 0">
      <el-icon class="is-loading" :size="24"><Loading /></el-icon>
      <p style="margin-top:8px;color:#999">正在获取数据库列表...</p>
    </div>
    <template v-else>
      <div style="margin-bottom:8px;display:flex;gap:8px">
        <el-button size="small" @click="setAllDBGrants('readwrite')">全部读写</el-button>
        <el-button size="small" @click="setAllDBGrants('read')">全部只读</el-button>
        <el-button size="small" @click="setAllDBGrants('')">全部无</el-button>
      </div>
      <el-table :data="dbGrants" size="small" max-height="340">
        <el-table-column prop="database" label="数据库" />
        <el-table-column label="权限" width="180" align="center">
          <template #default="{ row }">
            <el-radio-group v-model="row.privilege" size="small">
              <el-radio-button value="">无</el-radio-button>
              <el-radio-button value="read">读</el-radio-button>
              <el-radio-button value="readwrite">读写</el-radio-button>
            </el-radio-group>
          </template>
        </el-table-column>
      </el-table>
    </template>
  </template>

  <template v-else-if="provisionStep === 3">
    <div v-if="provisioning" style="text-align:center;padding:30px 0">
      <el-icon class="is-loading" :size="28"><Loading /></el-icon>
      <p style="margin-top:10px;color:#667085">正在目标 MySQL 上创建账号...</p>
    </div>
    <template v-else-if="provisionResult">
      <el-alert type="success" title="账号创建成功" :closable="false" show-icon />
      <el-descriptions :column="1" border size="small" style="margin-top:12px">
        <el-descriptions-item label="用户名">
          <code>{{ provisionResult.account.username }}</code>
        </el-descriptions-item>
        <el-descriptions-item label="密码">
          <code>{{ provisionResult.generated_password }}</code>
          <el-button link type="primary" size="small" style="margin-left:8px" @click="copyText(provisionResult.generated_password)">复制</el-button>
        </el-descriptions-item>
        <el-descriptions-item label="主机">{{ provision.host || '%' }}</el-descriptions-item>
      </el-descriptions>
      <el-alert type="warning" :closable="false" style="margin-top:8px" title="密码仅显示一次，请立即复制保存" />
    </template>
    <el-alert v-else-if="provisionError" type="error" :title="provisionError" :closable="false" show-icon />
  </template>

  <template #footer>
    <el-button @click="autoProvisionVisible = false">取消</el-button>
    <el-button v-if="provisionStep === 1" type="primary" :disabled="!provision.adminAccountId" @click="goProvisionStep2">下一步</el-button>
    <el-button v-if="provisionStep === 2" :disabled="loadingDatabases" @click="provisionStep = 1">上一步</el-button>
    <el-button v-if="provisionStep === 2" type="primary" :disabled="provisioning || loadingDatabases" @click="doProvision">创建</el-button>
    <el-button v-if="provisionStep === 3 && !provisioning" type="primary" @click="closeProvisionAndRefresh">完成</el-button>
  </template>
</el-dialog>
```

- [ ] **Step 3: 在 `<script setup>` 末尾（`</script>` 之前）添加逻辑**

```typescript
// ── 自动创建 ──
interface DBGrantRow {
  database: string
  privilege: '' | 'read' | 'readwrite'
}

const autoProvisionVisible = ref(false)
const provisionStep = ref(1)
const provisioning = ref(false)
const loadingDatabases = ref(false)
const provisionError = ref('')
const provisionResult = ref<any>(null)
const dbGrants = ref<DBGrantRow[]>([])
const adminAccounts = ref<any[]>([])

const provision = reactive({
  adminAccountId: '',
  newUsername: '',
  host: '%',
})

async function openAutoProvision() {
  if (!selectedInstance.value) return
  // 加载该实例下的所有活跃账号作为可选凭据
  const instId = selectedInstance.value.id!
  try {
    const res = await api.apiClient.getDBAccounts(instId, { page_size: 200 })
    const items = (res as any).items || (Array.isArray(res) ? res : [])
    adminAccounts.value = items.filter((a: any) => a.status === 'active')
  } catch {
    adminAccounts.value = []
  }
  if (adminAccounts.value.length > 0) {
    provision.adminAccountId = adminAccounts.value[0].id
  } else {
    provision.adminAccountId = ''
  }
  provision.newUsername = ''
  provision.host = '%'
  provisionStep.value = 1
  provisionError.value = ''
  provisionResult.value = null
  autoProvisionVisible.value = true
}

async function goProvisionStep2() {
  if (!provision.adminAccountId || !selectedInstance.value) return
  loadingDatabases.value = true
  try {
    const res = await api.apiClient.listDBDatabases(selectedInstance.value.id!, provision.adminAccountId)
    const dbs: string[] = (res as any).databases || []
    dbGrants.value = dbs.map(db => ({ database: db, privilege: '' as const }))
    provisionStep.value = 2
  } catch (e: any) {
    ElMessage.error('获取数据库列表失败: ' + (e.message || e))
  } finally {
    loadingDatabases.value = false
  }
}

function setAllDBGrants(p: '' | 'read' | 'readwrite') {
  dbGrants.value.forEach(row => { row.privilege = p })
}

async function doProvision() {
  if (!selectedInstance.value) return
  provisioning.value = true
  provisionError.value = ''
  try {
    const grants = dbGrants.value
      .filter(r => r.privilege !== '')
      .map(r => ({ database: r.database, privilege: r.privilege }))
    const res = await api.apiClient.provisionDBAccount(selectedInstance.value.id!, {
      admin_account_id: provision.adminAccountId,
      new_username: provision.newUsername || undefined,
      host: provision.host || '%',
      grants,
    })
    provisionResult.value = (res as any)
    provisionStep.value = 3
  } catch (e: any) {
    provisionError.value = e.message || String(e)
  } finally {
    provisioning.value = false
  }
}

function resetAutoProvision() {
  provisionStep.value = 1
  provisionError.value = ''
  provisionResult.value = null
  dbGrants.value = []
}

function closeProvisionAndRefresh() {
  autoProvisionVisible.value = false
  loadSelectedInstanceAccounts()
}
```

- [ ] **Step 4: 验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck
```

```bash
cd C:\02-codespace\Jianmen\web && npm run build
```

Expected: 两个命令均成功

- [ ] **Step 5: 提交**

```bash
git add web/src/views/DatabaseView.vue
git commit -m "feat: add MySQL auto-provision UI

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---
