# 数据库管理功能增强 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 MySQL 数据库代理、补齐 DBStore 实例 CRUD、新增测试连接端点、账号列表分页、连接弹窗增强。

**Architecture:** 6 个独立任务，Task1(MySQL握手解析) → Task2(MySQL代理) | Task3(DBStore实例CRUD)，Task4(测试连接) | Task5(账号分页后端→前端) | Task6(连接弹窗增强)。Tasks 3-6 可并行。

**Tech Stack:** Go (GORM, net), Vue 3 (Element Plus), TypeScript

## Global Constraints

- 后端编译必须通过: `go build ./...`
- 后端测试必须通过: `go test ./... -count=1`
- 前端 typecheck 必须通过: `npm run typecheck`（在 web/ 目录）
- 前端 build 必须通过: `npm run build`（在 web/ 目录）
- 每个 Task 提交前验证上述四项
- 认证插件: 主支持 `mysql_native_password`

---

### Task 1: MySQL 握手解析与密码哈希

**Files:** Modify `internal/server/dbproxy/account_auth.go`

**Changes:**
1. 将 `mysqlLoginParser` 重命名为 `MySQLLoginParser`（导出供 server.go 使用）
2. 新增 `MySQLHandshake` 结构体（导出）
3. 新增 `ParseMySQLHandshake(payload []byte) (*MySQLHandshake, error)` 函数
4. 新增 `BuildMySQLNativePassword(password string, salt []byte) []byte` 函数
5. 新增 `"crypto/sha1"` import

**MySQLHandshake 结构体字段:** ProtocolVersion, ServerVersion, ConnectionID, AuthData (20-byte salt), CapabilityFlags, CharacterSet, StatusFlags, AuthPluginName

**ParseMySQLHandshake 逻辑:** 解析 MySQL Protocol::HandshakeV10 包 — protocol version → server version (null-term) → connection ID (4B LE) → auth-part-1 (8B) → filler → cap-lower (2B) → charset → status (2B) → cap-upper (2B) → auth-data-len → reserved (10B) → auth-part-2 (12B+) → auth-plugin (null-term if CLIENT_PLUGIN_AUTH)

**BuildMySQLNativePassword 算法:** SHA1(password) XOR SHA1(salt + SHA1(SHA1(password)))

**验证:** `go build ./internal/server/dbproxy/...`

**Commit:** `feat: add MySQL handshake parser and mysql_native_password hash builder`

---

### Task 2: 修复协议检测 + MySQL 代理实现

**Files:** Modify `internal/server/dbproxy/server.go`

**背景:** MySQL 客户端连接后等待服务器先发握手包（不像 PG 客户端先发包）。当前 `handleConn` 通过 `io.ReadFull` 检测协议字节对 MySQL 会永久阻塞。

**Changes:**
1. **修改 `handleConn` 协议检测** — 使用 200ms 读超时：
   - 超时 → 进入 handleMySQL（客户端在等握手包）
   - 读到 0x00 → 进入 handlePG（客户端先发了 StartupMessage）
   - 其他字节 → 未知协议，丢弃

2. **替换 `handleMySQL` stub** — 完整代理流程：
   - 读客户端初始包 → 解析用户名 → 查 DBAccount → RBAC + 禁用/过期检查
   - 连上游 MySQL → 读上游握手包 → 解析握手
   - 转发握手给客户端 → 读客户端认证响应
   - 用 `UpstreamPassword` 构建新登录包 → 发上游
   - 读上游认证结果 → 转发给客户端 → 返回 gatewayConn

3. **新增辅助类型/函数:**
   - `mysqlPacket` struct（raw + payload + seq）
   - `readMySQLPacket(conn net.Conn) (*mysqlPacket, error)`
   - `BuildMySQLUpstreamLogin(hs *MySQLHandshake, username, password, authPlugin string) []byte`（导出供 Task 4 使用）

4. **导出符号**（供 Task 4 test_db.go 使用）:
   - `BuildMySQLUpstreamLogin`
   - `ParseMySQLErrorMessage`（从 observer.go 导出）

**验证:** `go build ./... && go test ./internal/server/dbproxy/... -count=1`

**Commit:** `feat: implement MySQL proxy with protocol detection and mysql_native_password auth`

---

### Task 3: DBStore 数据库实例 CRUD 补齐

**Files:** Modify `internal/store/dbstore.go`

**Changes:** 替换第383-387行的四个 stub 方法，参考 `StaticStore` 实现：

- `DatabaseInstances()` → GORM Find + 统计 AccountCount
- `DatabaseInstance(id)` → GORM First + 统计 AccountCount
- `AddDatabaseInstance(...)` → 协议规范化 (pg→postgres) + SplitHostPort 校验 + GORM Create
- `UpdateDatabaseInstance(...)` → 协议规范化 + SplitHostPort 校验 + GORM Save
- `DeleteDatabaseInstance(id)` → 事务中先删 accounts 再删 instance

**Import 添加:** `"net"`, `"strings"`（如未引入）

**验证:** `go build ./... && go test ./internal/store/... -count=1`

**Commit:** `feat: implement DBStore database instance CRUD methods`

---

### Task 4: 测试连接端点

**Files:** Create `internal/server/admin/test_db.go`, Modify `internal/server/admin/server.go`, Modify `web/src/api/client.ts`

**Changes:**

**server.go:** 在 `/api/db/accounts/` 路由前添加:
```go
mux.HandleFunc("/api/db/accounts/test/", s.withAuthAndUser(s.handleTestDBConnection))
```

**test_db.go（新文件）:**
- `handleTestDBConnection` — POST 处理器：
  1. 从 URL 提取 account ID
  2. 用 `s.db.Preload("Instance")` 查 model.DatabaseAccount（获取密码）
  3. TCP DialTimeout(5s) 连上游
  4. 调用 `testDBAuth(conn, protocol, username, password)` 尝试认证
  5. 返回 `{ ok, error?, latency_ms }`
- `testDBAuth` → 根据协议分发到 `testPostgresAuth` / `testMySQLAuth`
- `testPostgresAuth` → 发送 StartupMessage + 响应 CleartextPassword/MD5 挑战
- `testMySQLAuth` → 读握手包 → 调用 `dbproxy.ParseMySQLHandshake` + `dbproxy.BuildMySQLUpstreamLogin` → 发送登录 → 检查 OK/ERR

**client.ts:** 新增 API 方法:
```typescript
testDBConnection: (id: string) =>
  request<ApiEnvelope<{ ok: boolean; error?: string; latency_ms: number }>>(
    `/api/db/accounts/test/${encodeURIComponent(id)}`, { method: 'POST' }
  ),
```

**验证:** `go build ./... && go test ./internal/server/admin/... -count=1 && npm --prefix web run typecheck && npm --prefix web run build`

**Commit:** `feat: add database account test connection endpoint`

---

### Task 5: 账号列表分页

**Files:** Modify `internal/server/admin/server.go`, Modify `web/src/views/DatabaseView.vue`

**后端 — server.go:**
修改 `handleDBInstance` 中 `accounts` GET 处理：解析 `?page=1&size=20&search=` 参数 → 从 store 取全部 accounts → 内存过滤(search) → 切片分页 → 返回 `{ items, total }`

**前端 — DatabaseView.vue:**
- 新增状态: `accountPage`, `accountPageSize`, `accountTotal`
- 修改 `loadAccounts`: 使用 URLSearchParams 传 page/size，解析 `{ items, total }` 响应
- 在账号表格后添加 `<el-pagination>` 组件（v-model:current-page / v-model:page-size / @current-change / @size-change）
- 新增 `handleAccountPageSizeChange` 函数

**验证:** `go build ./... && npm --prefix web run typecheck && npm --prefix web run build`

**Commit:** `feat: add pagination to database account list`

---

### Task 6: 连接弹窗增强

**Files:** Modify `web/src/views/DatabaseView.vue`, Modify `web/src/i18n/index.ts`

**弹窗模板 — 三区布局:**
1. **状态区** — 弹窗打开时自动调用 `testDBConnection`，显示 🟢/🔴 状态 + 延迟 + 错误信息 + 账号启用/禁用/过期标签
2. **参数区** — 主机/端口/用户名 逐行展示，每行带独立的复制按钮
3. **命令区** — CLI 命令行（mysql/psql），带复制按钮

**Script 新增:**
- `connectTesting`, `connectTestResult` ref
- `connectParams` computed（主机/端口/用户名数组）
- `isExpired(expiresAt)` 函数
- `onConnectDialogOpened()` — 触发 testDBConnection API 调用
- 使用 `@opened` 事件触发测试（Element Plus 支持）

**i18n — database.connect 区域新增:**
- status, reachable, unreachable, latency, expired, expires, params, command, host, port, username, copyAll（中英文）

**验证:** `npm --prefix web run typecheck && npm --prefix web run build`

**Commit:** `feat: enhance database connection dialog with status, params and CLI command`

---

### 最终验证

```bash
go build ./...
go test ./... -count=1 -short
npm --prefix web run typecheck
npm --prefix web run build
```
