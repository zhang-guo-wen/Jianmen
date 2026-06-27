# 数据库管理功能增强 — 设计文档

日期：2026-06-28

## 概述

完善数据库管理功能，使数据库代理支持 PostgreSQL 和 MySQL 两种协议，用户可在数据库管理页面完成实例管理、账号管理、连接信息获取和账号认证测试。

## 核心目标

- MySQL 代理与 PostgreSQL 代理对等完整
- 数据库实例 CRUD 完全走 DBStore（GORM）
- 账号认证测试（测试连接）
- 账号列表分页
- 连接弹窗增强（状态 + 参数明细 + 命令行）

## 架构

```
前端 DatabaseView.vue
  ├── 实例管理 Tab（CRUD + 启用/禁用 + 分页）
  └── 账号管理 Tab（CRUD + 分页 + 连接弹窗增强）
       ↓ 连接按钮 → 弹窗（状态区 / 参数区 / 命令区）
       ↓ 测试按钮 → POST /api/db/accounts/{id}/test
       ↓ API
后端 Admin API
  ├── /api/db/instances          实例 CRUD
  ├── /api/db/accounts           账号 CRUD
  ├── /api/db/accounts/{id}/test 测试连接（新增）
  └── /api/db/instances/{id}/accounts?page=&size=  账号分页
       ↓
DBStore / StaticStore (GORM)
  ├── DatabaseInstance CRUD（补齐 DBStore stub）
  ├── DatabaseAccount CRUD
  └── AuthenticateDirect（实现）
       ↓
DB Proxy Gateway (TCP)
  ├── PostgreSQL 代理 ✅
  └── MySQL 代理 ❌→✅
```

## 变更详情

### 1. MySQL 代理（`internal/server/dbproxy/`）

#### 1.1 handleMySQL — `server.go:372-379`

替换当前 stub，实现完整代理流程：

```
Client → Gateway → Upstream MySQL
  1. Gateway 连接上游 MySQL，接收 Initial Handshake
  2. 解析 Handshake（server_version, auth_plugin, salt）
  3. 转发 Handshake 给客户端
  4. 接收客户端 Login Request
  5. 解析 username（紧凑用户名格式 db-xxxx）
  6. RBAC 检查 + 禁用/过期检查
  7. 查 DatabaseAccount → UpstreamUsername + UpstreamPassword
  8. 用 mysql_native_password 算法重新计算 auth hash
  9. 发送 Login Request 给上游
  10. 转发认证结果给客户端
  11. 双向透传 + 查询录制
```

#### 1.2 握手解析 — `account_auth.go`

新增函数：
- `parseMySQLHandshake(payload []byte) (*mysqlHandshake, error)` — 解析上游握手包
- `buildMySQLNativePassword(password string, salt []byte) []byte` — 计算 mysql_native_password hash

支持的认证插件：
- `mysql_native_password`（必须）
- `caching_sha2_password`（常见，MySQL 8.0 默认）

#### 1.3 查询录制 — `observer.go`

完善 `mySQLObserver`：
- 解析 `COM_QUERY`（0x03）包，提取 SQL 文本
- 解析服务端响应（OK/ERR/ResultSet）
- 写入 `queries.jsonl`，格式与 PostgreSQL 一致

### 2. DBStore 实例 CRUD — `internal/store/dbstore.go`

补齐四个 stub 方法，直接操作 GORM：

| 方法 | 实现 |
|------|------|
| `DatabaseInstances(ctx, opts)` | 分页查询 + 搜索（name/address LIKE） |
| `AddDatabaseInstance(ctx, instance)` | GORM Create |
| `UpdateDatabaseInstance(ctx, instance)` | GORM Save |
| `DeleteDatabaseInstance(ctx, id)` | GORM Delete（级联删除账号） |

### 3. 测试连接 — `POST /api/db/accounts/{id}/test`

**后端逻辑：**
1. 查 DatabaseAccount + 关联 DatabaseInstance
2. TCP 连接到上游数据库
3. 用 UpstreamUsername + UpstreamPassword 尝试认证握手
4. 关闭连接，返回结果

**请求：** 无 body
**响应：** `{ ok: bool, error?: string, latency_ms: number }`
**超时：** 5 秒
**审计：** 不记录

### 4. 账号列表分页

**后端：** `GET /api/db/instances/{id}/accounts?page=1&size=20&search=`

响应格式：
```json
{
  "items": [...],
  "total": 100
}
```

**前端：** 账号 Tab 表格底部加分页组件，默认 20 条/页，支持 20/50/100 切换。

### 5. 连接弹窗增强 — `DatabaseView.vue`

弹窗三区布局：

**状态区** — 点击连接按钮时自动检测：
- 连通状态图标（🟢 正常 / 🔴 不可达）
- 延迟显示
- 账号状态（启用/禁用/过期）

**参数区** — 逐项展示，每项带复制按钮：
- 主机（代理地址）
- 端口（代理端口 = 上游端口 + 30000）
- 用户名（紧凑用户名）
- 数据库名

**命令区** — 一键复制命令行：
- MySQL: `mysql -h <host> -P <port> -u <user> -p`
- PostgreSQL: `psql -h <host> -p <port> -U <user>`

## 不做（后续迭代）

- SQL 查询面板
- Web Terminal
- 查询策略 UI（SQL 模式允许/拒绝）
- MySQL caching_sha2_password 完整实现（先做 mysql_native_password，sha2 做基础支持）

## 测试要点

- MySQL 代理：`go test ./internal/server/dbproxy/... -count=1`
- 后端编译：`go build ./...`
- 前端 typecheck：`npm run typecheck`
- 前端 build：`npm run build`
