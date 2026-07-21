# 审计数据分层存储设计

日期：2026-07-09

## 目标

将审计元数据和命令摘要存入数据库（SQLite），支持快速检索和分页。终端录像等大文件继续文件存储。

## 分层策略

```
数据库存储：audit_sessions + audit_ssh_commands + audit_db_queries + audit_sftp_events
文件保留：  terminal.cast（终端录像）+ 完整输出流
```

## 数据模型

### audit_sessions（审计会话元数据，所有协议共用）

```go
type AuditSession struct {
    ID            string     `gorm:"primaryKey;size:64"`
    UserSessionID string     `gorm:"index;size:64"`
    UserID        string     `gorm:"index;size:64"`
    Username      string     `gorm:"index;size:128"`
    Protocol      string     `gorm:"index;size:32"`    // ssh / mysql / postgres / redis / sftp
    TargetName    string     `gorm:"size:255"`         // 主机名 或 实例名
    AccountName   string     `gorm:"size:128"`         // 目标账号
    ClientIP      string     `gorm:"size:128"`
    StartedAt     time.Time  `gorm:"index"`
    EndedAt       *time.Time
    State         string     `gorm:"size:32"`          // started / ended
    ReplayDir     string     `gorm:"size:512"`         // 文件储存路径
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

索引：`(user_session_id, started_at)`、`(username, started_at)`、`(protocol, started_at)`

### audit_ssh_commands

```go
type AuditSSHCommand struct {
    ID             string    `gorm:"primaryKey;size:64"`
    AuditSessionID string    `gorm:"index;size:64"`
    Timestamp      time.Time `gorm:"index"`
    Command        string    `gorm:"type:text"`
}
```

### audit_db_queries

```go
type AuditDBQuery struct {
    ID             string    `gorm:"primaryKey;size:64"`
    AuditSessionID string    `gorm:"index;size:64"`
    Timestamp      time.Time `gorm:"index"`
    SQLText        string    `gorm:"type:text"`
    QueryKind      string    `gorm:"size:32"`  // SELECT/INSERT/UPDATE/DELETE/REDIS等
    DurationMs     int64
}
```

### audit_sftp_events

```go
type AuditSFTPEvent struct {
    ID             string    `gorm:"primaryKey;size:64"`
    AuditSessionID string    `gorm:"index;size:64"`
    Timestamp      time.Time `gorm:"index"`
    Action         string    `gorm:"size:32"`     // read/write/remove/rename/mkdir
    Path           string    `gorm:"size:1024"`
    Size           int64
    Result         string    `gorm:"size:32"`     // success/error
}
```

### 关于 model.Session

`model.Session` 表已存在但从未写入。本次不复用，新建以上表。老 `Session` 模型在后续清理。

## API 设计

保持前后端分离的 SSH / DB 两个独立审计列表，前端不改 Tab 结构。

### SSH 审计列表

```
GET /api/audit/ssh?page=1&size=20&search=root&date=2026-07-09
  → 查 audit_sessions WHERE protocol IN ('ssh','sftp')
  → 返回字段同现有 sessionListItem
```

### DB 审计列表

```
GET /api/audit/db?page=1&size=20&search=select&protocol=mysql&date=2026-07-09
  → 查 audit_sessions WHERE protocol IN ('mysql','postgres','redis')
  → 返回字段同现有 dbConnectionListItem
```

### SSH 会话详情

```
GET /api/audit/ssh/{id}/commands    → audit_ssh_commands
GET /api/audit/ssh/{id}/files       → audit_sftp_events
GET /api/audit/ssh/{id}/replay      → 读文件 terminal.cast
```

### DB 连接详情

```
GET /api/audit/db/{id}/queries      → audit_db_queries
```

### 旧接口

`/api/sessions`、`/api/db/connections` 后续替换为新接口后废弃删除。

## 写入链路

### 统一走 store 接口

- SSH server 和 DB gateway 都通过 store 接口写入审计数据
- DB gateway 当前持有 `*gorm.DB`，改为注入 store 接口中的审计子集
- 在 `store.Store` 中新增审计写入方法

### store 接口新增方法

```go
CreateAuditSession(session *model.AuditSession) error
UpdateAuditSession(id string, updates map[string]any) error
CreateAuditSSHCommand(cmd *model.AuditSSHCommand) error
CreateAuditDBQuery(query *model.AuditDBQuery) error
CreateAuditSFTPEvent(event *model.AuditSFTPEvent) error

// 查询方法
ListAuditSessions(params AuditListParams) ([]model.AuditSession, int64, error)
GetAuditSession(id string) (*model.AuditSession, error)
ListAuditSSHCommands(sessionID string, limit int) ([]model.AuditSSHCommand, error)
ListAuditDBQueries(sessionID string, limit int) ([]model.AuditDBQuery, error)
ListAuditSFTPEvents(sessionID string, limit int) ([]model.AuditSFTPEvent, error)
```

## 录制流程改造

### SSH 侧（sshserver.handleConn）

1. 连接建立时：`store.CreateAuditSession(&auditSession)`（state=started）
2. 命令推断出后：`store.CreateAuditSSHCommand(&cmd)`（每条命令独立插入，频率可接受）
3. SFTP 事件：`store.CreateAuditSFTPEvent(&event)`
4. 连接关闭时：`store.UpdateAuditSession(id, {ended_at, state=ended})`
5. 文件录制保留不删

### DB Proxy 侧（dbproxy.Gateway）

1. 连接建立时：`store.CreateAuditSession(&auditSession)`
2. 每条查询完成时：`store.CreateAuditDBQuery(&query)`（独立插入，频率可接受）
3. 连接关闭时：`store.UpdateAuditSession(id, {ended_at, state=ended})`
4. `queries.jsonl` 文件继续写

### SSH server 需要拿到 user_session_id

当前 SSH server 的 `handleConn` 中没有 user_session_id。需要从 compact username 解析，或者在连接请求中携带。实际上 SSH/DBC连接中 compact username 已经包含了 session_id，在 `resolveCompactAccount` 等逻辑中已经有 `UserSession` 对象，可以直接拿到 `UserSession.ID`。

## 前端

前端保持现有两个 Tab（SSH / DB）分开查询，仅需：

- 把 API 请求地址从 `/api/sessions`、`/api/db/connections` 改为新接口
- 详情页同样改 API 路径
- 列表字段基本不变，改动最小

## 文件结构

新增/修改文件：

| 文件 | 变更 |
|------|------|
| `internal/model/audit.go` | 新增：4 个审计 GORM 实体 |
| `internal/model/core.go` | 注册新实体到 AllModels |
| `internal/store/store.go` | 新增审计接口方法和参数类型 |
| `internal/store/sqlite/audit.go` | 新增：审计数据访问实现 |
| `internal/server/admin/audit_handlers.go` | 新增：审计 API handler |
| `internal/server/admin/server.go` | 注册新路由 |
| `internal/server/sshserver/server.go` | 连接时写 audit_sessions，命令写表 |
| `internal/server/dbproxy/server.go` | 连接时写 audit_sessions，查询写表 |

## 不做的

- terminal.cast 不入库
- 不建单独的 Redis 命令表（复用 audit_db_queries，QueryKind=REDIS）
- 不做跨 session 聚合（本次只做存储改造，暂不聚合）
- 不修改前端页面结构（后续单独改造）
