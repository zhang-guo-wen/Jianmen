# 审计数据分层存储 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将审计元数据、SSH命令、SFTP事件、DB查询存入SQLite数据库，支持快速检索和分页；大文件（terminal.cast、完整输出）继续文件存储。

**Architecture:** 新增4个 GORM model 对应4张审计表；扩展 Store 接口增加审计写入/查询方法；SSH server 和 DB gateway 通过回调接口写入审计；admin server 新增审计 API handler 替代文件扫描。

**Tech Stack:** Go + GORM + SQLite, Vue 3 + Composition API

## Global Constraints

- 前端保持 SSH/DB 两个独立 Tab，不合并，只改 API 路径
- SSH server 和 DB gateway 统一走 store 接口写审计
- 旧接口 `/api/sessions`、`/api/db/connections` 替换后废弃
- 命令/查询都独立 INSERT，不做批量攒批

## File Structure

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/model/audit.go` | 新建 | 4个审计 GORM 实体定义 |
| `internal/model/core.go` | 修改 | 注册新实体到 AllModels |
| `internal/store/store.go` | 修改 | 新增审计接口方法、参数/视图类型 |
| `internal/store/dbstore_audit.go` | 新建 | 审计数据访问实现 |
| `internal/recording/session.go` | 修改 | 新增 AuditSink 回调接口，录制时写 DB |
| `internal/recording/command.go` | 修改 | CommandRecorder 注入 auditSink 和 sessionID |
| `internal/server/sshserver/server.go` | 修改 | 连接时创建 audit_sessions，关闭时更新状态 |
| `internal/server/dbproxy/server.go` | 修改 | 连接时创建 audit_sessions，查询时写 audit_db_queries |
| `internal/server/admin/audit_handlers.go` | 新建 | 审计 API handler |
| `internal/server/admin/server.go` | 修改 | 注册新路由 |
| `web/src/views/AuditView.vue` | 修改 | API 路径切换为新接口 |

---

### Task 1: 创建审计 GORM 模型

**Files:**
- Create: `internal/model/audit.go`
- Modify: `internal/model/core.go`

**Interfaces:**
- Produces: `model.AuditSession`, `model.AuditSSHCommand`, `model.AuditDBQuery`, `model.AuditSFTPEvent`

- [ ] **Step 1: 创建 `internal/model/audit.go`**

```go
package model

import "time"

// AuditSession 审计会话元数据，所有协议共用。
type AuditSession struct {
	ID            string     `gorm:"primaryKey;size:64" json:"id"`
	UserSessionID string     `gorm:"index;size:64" json:"user_session_id,omitempty"`
	UserID        string     `gorm:"index;size:64" json:"user_id"`
	Username      string     `gorm:"index;size:128" json:"username"`
	Protocol      string     `gorm:"index:idx_audit_sessions_protocol_started,priority:1;size:32" json:"protocol"`
	TargetName    string     `gorm:"size:255" json:"target_name"`
	AccountName   string     `gorm:"size:128" json:"account_name"`
	ClientIP      string     `gorm:"size:128" json:"client_ip"`
	StartedAt     time.Time  `gorm:"index:idx_audit_sessions_protocol_started,priority:2;index:idx_audit_sessions_user_started,priority:2;index:idx_audit_sessions_session_started,priority:2" json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	State         string     `gorm:"size:32" json:"state"`
	ReplayDir     string     `gorm:"size:512" json:"replay_dir,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (AuditSession) TableName() string { return "audit_sessions" }

func (s *AuditSession) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = NewID()
	}
	return nil
}

// AuditSSHCommand 从终端输入推断出的 shell 命令。
type AuditSSHCommand struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index" json:"timestamp"`
	Command        string    `gorm:"type:text" json:"command"`
}

func (AuditSSHCommand) TableName() string { return "audit_ssh_commands" }

func (c *AuditSSHCommand) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	return nil
}

// AuditDBQuery SQL 或 Redis 命令记录。
type AuditDBQuery struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index" json:"timestamp"`
	SQLText        string    `gorm:"type:text" json:"sql_text"`
	QueryKind      string    `gorm:"size:32" json:"query_kind,omitempty"`
	DurationMs     int64     `json:"duration_ms,omitempty"`
}

func (AuditDBQuery) TableName() string { return "audit_db_queries" }

func (q *AuditDBQuery) BeforeCreate(tx *gorm.DB) error {
	if q.ID == "" {
		q.ID = NewID()
	}
	return nil
}

// AuditSFTPEvent SFTP 文件操作事件。
type AuditSFTPEvent struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index" json:"timestamp"`
	Action         string    `gorm:"size:32" json:"action"`
	Path           string    `gorm:"size:1024" json:"path"`
	Size           int64     `json:"size,omitempty"`
	Result         string    `gorm:"size:32" json:"result"`
}

func (AuditSFTPEvent) TableName() string { return "audit_sftp_events" }

func (e *AuditSFTPEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	return nil
}
```

- [ ] **Step 2: 注册到 AllModels**

在 `internal/model/core.go` 的 `AllModels()` 返回切片中加入：

```go
&AuditSession{},
&AuditSSHCommand{},
&AuditDBQuery{},
&AuditSFTPEvent{},
```

- [ ] **Step 3: 验证编译**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

- [ ] **Step 4: 提交**

```bash
git add internal/model/audit.go internal/model/core.go
git commit -m "feat: add audit GORM models (AuditSession, AuditSSHCommand, AuditDBQuery, AuditSFTPEvent)"
```

---

### Task 2: 扩展 Store 接口

**Files:**
- Modify: `internal/store/store.go`

**Interfaces:**
- Produces: Store 接口新增审计方法 + 参数类型

- [ ] **Step 1: 在 `store.go` 中添加审计参数类型和列表视图**

在 `store.go` 末尾（`Store` interface 定义之后）添加：

```go
// AuditListParams 审计列表查询参数。
type AuditListParams struct {
	Protocol  string // 空表示不过滤，可逗号分隔多个协议
	Search    string // 模糊搜索用户名/目标名
	Date      string // YYYY-MM-DD 格式
	Page      int
	Size      int
}

// AuditSessionView 审计列表视图。
type AuditSessionView struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	Protocol    string  `json:"protocol"`
	TargetName  string  `json:"target_name"`
	AccountName string  `json:"account_name,omitempty"`
	ClientIP    string  `json:"client_ip"`
	StartedAt   string  `json:"started_at"`
	EndedAt     string  `json:"ended_at,omitempty"`
	State       string  `json:"state"`
	ReplayDir   string  `json:"replay_dir,omitempty"`
}

// PageOpts 分页参数。
type PageOpts struct {
	Limit  int
	Offset int
}
```

- [ ] **Step 2: 在 Store 接口中添加审计方法**

在 Store 接口末尾添加：

```go
	// -- audit --

	CreateAuditSession(session *model.AuditSession) error
	EndAuditSession(id string) error
	GetAuditSession(id string) (*model.AuditSession, error)
	ListAuditSessions(params AuditListParams) ([]AuditSessionView, int64, error)

	CreateAuditSSHCommand(cmd *model.AuditSSHCommand) error
	ListAuditSSHCommands(sessionID string, opts PageOpts) ([]model.AuditSSHCommand, int64, error)

	CreateAuditSFTPEvent(event *model.AuditSFTPEvent) error
	ListAuditSFTPEvents(sessionID string, opts PageOpts) ([]model.AuditSFTPEvent, int64, error)

	CreateAuditDBQuery(query *model.AuditDBQuery) error
	ListAuditDBQueries(sessionID string, opts PageOpts) ([]model.AuditDBQuery, int64, error)

	FindUserSessionByCompactUsername(username string) (*model.UserSession, error)
```

- [ ] **Step 3: 验证编译（预期因接口未实现而失败）**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

预期报错：`DBStore` 未实现新增的 Store 接口方法。

- [ ] **Step 4: 提交**

```bash
git add internal/store/store.go
git commit -m "feat: add audit methods to Store interface"
```

---

### Task 3: 实现 Store 审计方法

**Files:**
- Create: `internal/store/dbstore_audit.go`

**Interfaces:**
- Implements: Store 接口中新增的审计方法

- [ ] **Step 1: 创建 `internal/store/dbstore_audit.go`**

```go
package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

// -- audit sessions --

func (s *DBStore) CreateAuditSession(session *model.AuditSession) error {
	return s.db.Create(session).Error
}

func (s *DBStore) EndAuditSession(id string) error {
	now := time.Now().UTC()
	return s.db.Model(&model.AuditSession{}).
		Where("id = ?", id).
		Updates(map[string]any{"state": "ended", "ended_at": now}).Error
}

func (s *DBStore) GetAuditSession(id string) (*model.AuditSession, error) {
	var session model.AuditSession
	if err := s.db.Where("id = ?", id).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("audit session %q: %w", id, err)
		}
		return nil, err
	}
	return &session, nil
}

func (s *DBStore) ListAuditSessions(params AuditListParams) ([]AuditSessionView, int64, error) {
	q := s.db.Model(&model.AuditSession{})
	if params.Protocol != "" {
		protos := splitCSV(params.Protocol)
		q = q.Where("protocol IN ?", protos)
	}
	if params.Search != "" {
		like := "%" + strings.ToLower(params.Search) + "%"
		q = q.Where("LOWER(username) LIKE ? OR LOWER(target_name) LIKE ? OR LOWER(account_name) LIKE ?", like, like, like)
	}
	if params.Date != "" {
		date, err := time.Parse("2006-01-02", params.Date)
		if err == nil {
			nextDate := date.Add(24 * time.Hour)
			q = q.Where("started_at >= ? AND started_at < ?", date, nextDate)
		}
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if params.Size <= 0 {
		params.Size = 20
	}
	if params.Page <= 0 {
		params.Page = 1
	}
	var sessions []model.AuditSession
	if err := q.Order("started_at DESC").Offset((params.Page - 1) * params.Size).Limit(params.Size).Find(&sessions).Error; err != nil {
		return nil, 0, err
	}
	views := make([]AuditSessionView, len(sessions))
	for i, sess := range sessions {
		views[i] = AuditSessionView{
			ID:          sess.ID,
			Username:    sess.Username,
			Protocol:    sess.Protocol,
			TargetName:  sess.TargetName,
			AccountName: sess.AccountName,
			ClientIP:    sess.ClientIP,
			StartedAt:   sess.StartedAt.Format(time.RFC3339Nano),
			State:       sess.State,
			ReplayDir:   sess.ReplayDir,
		}
		if sess.EndedAt != nil {
			views[i].EndedAt = sess.EndedAt.Format(time.RFC3339Nano)
		}
	}
	return views, total, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// -- audit SSH commands --

func (s *DBStore) CreateAuditSSHCommand(cmd *model.AuditSSHCommand) error {
	return s.db.Create(cmd).Error
}

func (s *DBStore) ListAuditSSHCommands(sessionID string, opts PageOpts) ([]model.AuditSSHCommand, int64, error) {
	q := s.db.Model(&model.AuditSSHCommand{}).Where("audit_session_id = ?", sessionID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var cmds []model.AuditSSHCommand
	if opts.Limit <= 0 {
		opts.Limit = 500
	}
	if err := q.Order("timestamp ASC").Offset(opts.Offset).Limit(opts.Limit).Find(&cmds).Error; err != nil {
		return nil, 0, err
	}
	return cmds, total, nil
}

// -- audit SFTP events --

func (s *DBStore) CreateAuditSFTPEvent(event *model.AuditSFTPEvent) error {
	return s.db.Create(event).Error
}

func (s *DBStore) ListAuditSFTPEvents(sessionID string, opts PageOpts) ([]model.AuditSFTPEvent, int64, error) {
	q := s.db.Model(&model.AuditSFTPEvent{}).Where("audit_session_id = ?", sessionID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []model.AuditSFTPEvent
	if opts.Limit <= 0 {
		opts.Limit = 1000
	}
	if err := q.Order("timestamp ASC").Offset(opts.Offset).Limit(opts.Limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

// -- audit DB queries --

func (s *DBStore) CreateAuditDBQuery(query *model.AuditDBQuery) error {
	return s.db.Create(query).Error
}

func (s *DBStore) ListAuditDBQueries(sessionID string, opts PageOpts) ([]model.AuditDBQuery, int64, error) {
	q := s.db.Model(&model.AuditDBQuery{}).Where("audit_session_id = ?", sessionID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var queries []model.AuditDBQuery
	if opts.Limit <= 0 {
		opts.Limit = 1000
	}
	if err := q.Order("timestamp ASC").Offset(opts.Offset).Limit(opts.Limit).Find(&queries).Error; err != nil {
		return nil, 0, err
	}
	return queries, total, nil
}

// -- user session lookup --

func (s *DBStore) FindUserSessionByCompactUsername(username string) (*model.UserSession, error) {
	login, err := parseLoginName(username)
	if err != nil {
		return nil, fmt.Errorf("parse compact username %q: %w", username, err)
	}
	var sess model.UserSession
	if err := s.db.Where("session_id = ? AND status = ?", login.SessionID, "active").First(&sess).Error; err != nil {
		return nil, fmt.Errorf("lookup user session by session_id %q: %w", login.SessionID, err)
	}
	return &sess, nil
}
```

- [ ] **Step 2: 验证编译**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

- [ ] **Step 3: 提交**

```bash
git add internal/store/dbstore_audit.go
git commit -m "feat: implement audit store methods on DBStore"
```

---

### Task 4: 录制层添加审计回调接口

**Files:**
- Modify: `internal/recording/session.go`
- Modify: `internal/recording/command.go`

**Interfaces:**
- Produces: `AuditSink` 接口定义
- Modifies: `SessionRecorder`、`CommandRecorder`、`NewSessionRecorder`、`NewCommandRecorder` 增加审计 sink

- [ ] **Step 1: 在 `session.go` 顶部添加 AuditSink 接口**

在 `package recording` 之后、`type SessionRecorder struct` 之前加入：

```go
// AuditSink receives audit events during recording.
type AuditSink interface {
	WriteCommand(sessionID string, timestamp time.Time, command string) error
	WriteFileEvent(sessionID string, timestamp time.Time, action, path string, size int64, result string) error
}
```

- [ ] **Step 2: 修改 SessionRecorder 结构体，增加 auditSink 字段**

在 `SessionRecorder` struct 中添加：

```go
	auditSink      AuditSink
```

- [ ] **Step 3: 修改 NewSessionRecorder 函数签名和内部调用**

签名改为：

```go
func NewSessionRecorder(root string, session model.Session, recordInput, recordCommands bool, logger *slog.Logger, sink AuditSink) (*SessionRecorder, error) {
```

`NewCommandRecorder(commandsFile, startedAt)` 改为 `NewCommandRecorder(commandsFile, startedAt, sink, session.ID)`。

在 `rec` 构造中设置 `auditSink: sink`：

```go
	rec := &SessionRecorder{
		session:        session,
		startedAt:      startedAt,
		recordInput:    recordInput,
		recordCommands: recordCommands,
		logger:         logger,
		dir:            dir,
		terminal:       NewAsciinemaWriter(terminalFile, startedAt, 80, 24),
		eventsFile:     eventsFile,
		filesFile:      filesFile,
		commands:       NewCommandRecorder(commandsFile, startedAt, sink, session.ID),
		auditSink:      sink,
		files:          make(map[string]*FileSummary),
	}
```

- [ ] **Step 4: 修改 RecordFileEvent，写入 audit sink**

在 `RecordFileEvent` 方法中，`updateFileSummaryLocked(event)` 调用之后，`raw, err := json.Marshal(event)` 之前，添加：

```go
	if r.auditSink != nil {
		_ = r.auditSink.WriteFileEvent(r.session.ID, time.UnixMilli(event.StartedAt), event.Action, event.Path, event.Size, event.Result)
	}
```

- [ ] **Step 5: 修改 CommandRecorder，注入 auditSink**

修改 `internal/recording/command.go`：

在 `CommandRecorder` struct 中添加两个字段：

```go
	auditSink       AuditSink
	sessionID       string
```

修改 `NewCommandRecorder` 签名和实现：

```go
func NewCommandRecorder(file *os.File, startedAt time.Time, sink AuditSink, sessionID string) *CommandRecorder {
	return &CommandRecorder{
		file:      file,
		startedAt: startedAt,
		line:      make([]rune, 0, 128),
		auditSink: sink,
		sessionID: sessionID,
	}
}
```

在 `flushCurrentLocked()` 方法末尾，`r.current = nil` 之前，添加 DB 写入：

```go
	if r.auditSink != nil && r.current != nil {
		r.auditSink.WriteCommand(r.sessionID, time.UnixMilli(r.current.StartedAt), r.current.Command)
	}
```

- [ ] **Step 6: 验证编译**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

- [ ] **Step 7: 提交**

```bash
git add internal/recording/
git commit -m "feat: add AuditSink callback to recording layer"
```

---

### Task 5: SSH Server 写入审计记录

**Files:**
- Modify: `internal/server/sshserver/server.go`

**Interfaces:**
- Consumes: `store.Store`（已有）、`recording.AuditSink`

- [ ] **Step 1: 创建 AuditSink 适配结构体**

在任何一行的顶级区域添加（`Server` struct 定义之后即可）：

```go
// auditStore adapts store.Store to recording.AuditSink.
type auditStore struct {
	store     store.Store
	sessionID string
}

func (a *auditStore) WriteCommand(sessionID string, timestamp time.Time, command string) error {
	return a.store.CreateAuditSSHCommand(&model.AuditSSHCommand{
		AuditSessionID: sessionID,
		Timestamp:      timestamp,
		Command:        command,
	})
}

func (a *auditStore) WriteFileEvent(sessionID string, timestamp time.Time, action, path string, size int64, result string) error {
	return a.store.CreateAuditSFTPEvent(&model.AuditSFTPEvent{
		AuditSessionID: sessionID,
		Timestamp:      timestamp,
		Action:         action,
		Path:           path,
		Size:           size,
		Result:         result,
	})
}
```

- [ ] **Step 2: 修改 handleConn，连接时创建 audit session 并传入 sink**

在 `session := model.NewSession(...)` 之后，`recorder, err = ...` 之前，加入：

```go
	// 查找 UserSession（从 compact username 中间部分解析）
	userSession, _ := s.store.FindUserSessionByCompactUsername(serverConn.User())

	auditSession := model.AuditSession{
		UserID:      user.ID,
		Username:    user.Username,
		Protocol:    "ssh",
		TargetName:  target.Name,
		AccountName: target.Username,
		ClientIP:    session.ClientIP,
		StartedAt:   time.Now().UTC(),
		State:       "started",
		ReplayDir:   filepath.Join(s.cfg.ReplayDir, "ssh", session.ID),
	}
	if userSession != nil {
		auditSession.UserSessionID = userSession.ID
	}
	auditSession.BeforeCreate(nil)
	s.store.CreateAuditSession(&auditSession)
```

- [ ] **Step 3: 将 audit sink 传入 recorder**

`recording.NewSessionRecorder(...)` 增加最后一个参数 `&auditStore{store: s.store, sessionID: session.ID}`。

- [ ] **Step 4: 连接关闭时更新 audit session 状态**

在 `recorder.Close()` 的 defer 之后添加：

```go
	defer func() {
		s.store.EndAuditSession(auditSession.ID)
	}()
```

- [ ] **Step 5: 补充 import**

需要补充 `filepath`、`time`、`model` 等包的 import（其中大部分已有，确认就行）。

- [ ] **Step 6: 验证编译**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

- [ ] **Step 7: 提交**

```bash
git add internal/server/sshserver/
git commit -m "feat: write SSH audit sessions and commands to DB"
```

---

### Task 6: DB Gateway 写入审计记录

**Files:**
- Modify: `internal/server/dbproxy/server.go`

**Interfaces:**
- Consumes: `auditWriter` 接口（本次定义）
- Modifies: `resolvedDBAccount`、`gatewayConn` 增加 `userSessionID`、`connectionRecorder` 增加 audit 字段

- [ ] **Step 1: 定义 auditWriter 接口**

在 `databaseAccountResolver` 接口之后添加：

```go
type auditWriter interface {
	CreateAuditSession(session *model.AuditSession) error
	EndAuditSession(id string) error
	CreateAuditDBQuery(query *model.AuditDBQuery) error
}
```

- [ ] **Step 2: 修改 resolvedDBAccount，增加 userSessionID**

```go
type resolvedDBAccount struct {
	account       *model.DatabaseAccount
	user          *model.User
	isCompact     bool
	rawName       string
	userSessionID string  // 新增
}
```

- [ ] **Step 3: 修改 resolveCompactAccount，设置 userSessionID**

在 return 语句前（已有 `sess` 变量），设置 `resolved.userSessionID = sess.ID`。

完整 return：

```go
	return &resolvedDBAccount{
		account:       &acct,
		user:          &user,
		isCompact:     true,
		rawName:       username,
		userSessionID: sess.ID,
	}, nil
```

- [ ] **Step 4: 修改 gatewayConn，增加 userSessionID**

```go
type gatewayConn struct {
	protocol      string
	accountID     string
	accountName   string
	upstream      net.Conn
	upstreamAddr  string
	userID        string
	accountUser   string
	instanceName  string
	userSessionID string  // 新增
}
```

- [ ] **Step 5: 修改 handlePG handleMySQL handleRedis 创建 gatewayConn 时传入 userSessionID**

每个 handler 的 `return &gatewayConn{...}` 中增加 `userSessionID: resolved.userSessionID,`。

handlePG（第421行附近）：
```go
	return &gatewayConn{
		protocol: "postgres", accountID: acct.ID, accountName: resolved.rawName,
		upstream: upstream, upstreamAddr: upstreamAddress(acct.Instance), userID: userID,
		accountUser: acct.Username, instanceName: acct.Instance.Name,
		userSessionID: resolved.userSessionID,
	}
```

handleMySQL（第650行附近）：
```go
	return &gatewayConn{
		protocol: "mysql", accountID: acct.ID, accountName: resolved.rawName,
		upstream: upstream, upstreamAddr: upstreamAddress(acct.Instance),
		userID: rbacUserID, accountUser: acct.Username,
		instanceName: acct.Instance.Name,
		userSessionID: resolved.userSessionID,
	}
```

handleRedis（`redis.go` 第221行附近）类似修改。

- [ ] **Step 6: 修改 Gateway 结构体，增加 audit 字段**

```go
	audit auditWriter
```

- [ ] **Step 7: 修改 NewGateway 构造加参数**

```go
func NewGateway(cfg config.DatabaseGatewayConfig, store databaseAccountResolver, replayDir string, logger *slog.Logger, db *gorm.DB, superAdminIDs map[string]bool, auditStore auditWriter) *Gateway {
```

设置 `audit: auditStore`。

- [ ] **Step 8: 修改 handleConn，连接时创建 audit session**

在 `recorder, _ := g.newRecorder(conn)` 之前加入：

```go
	// 查找操作者用户名
	var authUser string
	if g.db != nil {
		var u model.User
		if err := g.db.First(&u, "id = ?", conn.userID).Error; err == nil {
			authUser = u.Username
		}
	}

	auditSession := &model.AuditSession{
		UserID:      conn.userID,
		Username:    authUser,
		Protocol:    conn.protocol,
		TargetName:  conn.instanceName,
		AccountName: conn.accountUser,
		ClientIP:    "", // DB 网关直接从监听端口进来的连接，没有 client IP
		StartedAt:   time.Now().UTC(),
		State:       "started",
		ReplayDir:   filepath.Join(g.replayDir, "db", model.NewID()),
	}
	if conn.userSessionID != "" {
		auditSession.UserSessionID = conn.userSessionID
	}
	auditSession.BeforeCreate(nil)
	if g.audit != nil {
		g.audit.CreateAuditSession(auditSession)
	}
```

- [ ] **Step 9: 修改 connectionRecorder，增加 audit 字段和 auditSessionID**

```go
type connectionRecorder struct {
	mu              sync.Mutex
	id              string
	protocol        string
	metaPath        string
	meta            DBConnectionMeta
	file            *os.File
	seq             int64
	startedAt       time.Time
	audit           auditWriter     // 新增
	auditSessionID  string          // 新增
}
```

- [ ] **Step 10: 修改 newRecorder 签名，接收 auditSessionID 和 audit**

```go
func (g *Gateway) newRecorder(conn *gatewayConn, auditSessionID string) (*connectionRecorder, error) {
```

recorder 初始化时设置：

```go
	recorder := &connectionRecorder{
		id:             id,
		protocol:       conn.protocol,
		metaPath:       filepath.Join(dir, "meta.json"),
		meta:           meta,
		file:           file,
		startedAt:      startedAt,
		audit:          g.audit,
		auditSessionID: auditSessionID,
	}
```

- [ ] **Step 11: 修改 writeFinishLocked，写入 audit_db_queries**

在 `writeFinishLocked` 方法末尾添加：

```go
	if r.audit != nil && r.auditSessionID != "" {
		completedAt := time.Now().UTC()
		r.audit.CreateAuditDBQuery(&model.AuditDBQuery{
			AuditSessionID: r.auditSessionID,
			Timestamp:      completedAt,
			SQLText:        record.sql,
			QueryKind:      record.queryKind,
			DurationMs:     completedAt.Sub(record.startedAt).Milliseconds(),
		})
	}
```

- [ ] **Step 12: handleConn 调用 newRecorder 时传入 auditSessionID**

```go
	recorder, _ := g.newRecorder(conn, auditSession.ID)
```

- [ ] **Step 13: 连接关闭时更新状态**

在 `handleConn` 的 defer 中（`recorder.Close()` 之后）：

```go
	if g.audit != nil {
		g.audit.EndAuditSession(auditSession.ID)
	}
```

- [ ] **Step 14: 修复 observer_test.go 编译**

`startQuery` 的 `captureSink` 需要实现新增的接口方法。检查 `internal/server/dbproxy/observer_test.go`，如果 captureSink 不满足新的 `auditWriter` 接口则加桩。

- [ ] **Step 15: 验证编译**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

- [ ] **Step 16: 提交**

```bash
git add internal/server/dbproxy/
git commit -m "feat: write DB proxy audit sessions and queries to DB"
```

---

### Task 7: 新建 Admin 审计 API Handlers

**Files:**
- Create: `internal/server/admin/audit_handlers.go`
- Modify: `internal/server/admin/server.go`

**Interfaces:**
- Consumes: `store.Store` 审计方法（通过 `s.store`）
- Produces: HTTP API handlers

- [ ] **Step 1: 创建 `internal/server/admin/audit_handlers.go`**

```go
package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func (s *Server) handleAuditSSH(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionSessionView) {
		s.forbidden(w)
		return
	}
	params := store.AuditListParams{
		Protocol: "ssh,sftp",
		Search:   strings.ToLower(r.URL.Query().Get("search")),
		Date:     r.URL.Query().Get("date"),
	}
	params.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	params.Size, _ = strconv.Atoi(r.URL.Query().Get("size"))

	items, total, err := s.store.ListAuditSessions(params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total,
		"page": params.Page, "size": params.Size,
	})
}

func (s *Server) handleAuditDB(w http.ResponseWriter, r *http.Request) {
	params := store.AuditListParams{
		Protocol: "mysql,postgres,redis",
		Search:   strings.ToLower(r.URL.Query().Get("search")),
		Date:     r.URL.Query().Get("date"),
	}
	protocolFilter := r.URL.Query().Get("protocol")
	if protocolFilter != "" {
		params.Protocol = protocolFilter
	}
	params.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	params.Size, _ = strconv.Atoi(r.URL.Query().Get("size"))

	items, total, err := s.store.ListAuditSessions(params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total,
		"page": params.Page, "size": params.Size,
	})
}

func (s *Server) handleAuditArtifact(w http.ResponseWriter, r *http.Request) {
	// /api/audit/{protocol}/{id}/{artifact}
	path := strings.TrimPrefix(r.URL.Path, "/api/audit/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	protocol := parts[0]
	sessionID := parts[1]
	var artifact string
	if len(parts) == 3 {
		artifact = parts[2]
	}

	session, err := s.store.GetAuditSession(sessionID)
	if err != nil {
		writeErrorText(w, http.StatusNotFound, "audit session not found")
		return
	}

	switch {
	case artifact == "":
		writeJSON(w, http.StatusOK, session)
	case artifact == "commands" && (protocol == "ssh" || protocol == "sftp"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.store.ListAuditSSHCommands(sessionID, store.PageOpts{Limit: limit, Offset: offset})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "files" && (protocol == "ssh" || protocol == "sftp"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.store.ListAuditSFTPEvents(sessionID, store.PageOpts{Limit: limit, Offset: offset})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
	case artifact == "replay" && (protocol == "ssh" || protocol == "sftp"):
		replayPath := session.ReplayDir
		if replayPath == "" {
			writeErrorText(w, http.StatusNotFound, "no replay available")
			return
		}
		writeTextFile(w, replayPath+"/terminal.cast", "application/x-asciicast; charset=utf-8")
	case artifact == "queries" && (protocol == "mysql" || protocol == "postgres" || protocol == "redis"):
		limit, offset := pageFromQuery(r)
		items, total, err := s.store.ListAuditDBQueries(sessionID, store.PageOpts{Limit: limit, Offset: offset})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
	default:
		writeErrorText(w, http.StatusNotFound, "not found")
	}
}

func pageFromQuery(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size <= 0 {
		size = 500
	}
	if page <= 0 {
		page = 1
	}
	return size, (page - 1) * size
}
```

- [ ] **Step 2: 在 `server.go` 中注册路由**

在现有会话路由之后添加：

```go
		// 新版审计 API（替代旧的 sessions / db/connections）
		mux.HandleFunc("/api/audit/ssh", s.withAuthAndUser(s.handleAuditSSH))
		mux.HandleFunc("/api/audit/db", s.withAuthAndUser(s.handleAuditDB))
		mux.HandleFunc("/api/audit/", s.withAuthAndUser(s.handleAuditArtifact))
```

- [ ] **Step 3: 验证编译**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

- [ ] **Step 4: 提交**

```bash
git add internal/server/admin/
git commit -m "feat: add audit API handlers (SSH list, DB list, artifact details)"
```

---

### Task 8: 前端切换 API 路径

**Files:**
- Modify: `web/src/views/AuditView.vue`

- [ ] **Step 1: 读取 AuditView.vue 确定所有 API 调用点**

先读取文件确认所有 API URL。

- [ ] **Step 2: 替换 API 路径**

| 旧路径 | 新路径 |
|-------|-------|
| `/api/sessions` | `/api/audit/ssh` |
| `/api/db/connections` | `/api/audit/db` |
| `/api/sessions/${id}/meta` | `/api/audit/ssh/${id}` |
| `/api/sessions/${id}/commands` | `/api/audit/ssh/${id}/commands` |
| `/api/sessions/${id}/files` | `/api/audit/ssh/${id}/files` |
| `/api/sessions/${id}/file-summary` | `/api/audit/ssh/${id}/files` |
| `/api/sessions/${id}/replay` | `/api/audit/ssh/${id}/replay` |
| `/api/db/connections/${id}/meta` | `/api/audit/db/${id}` |
| `/api/db/connections/${id}/queries` | `/api/audit/db/${id}/queries` |

列表响应字段名变化需要对照：
- SSH: `user`→`username`, `target`→`target_name`, 原字段如 `client_ip`/`started_at`/`protocol` 不变
- DB: `auth_user`→`username`, `instance_name`→`target_name`, `account_name` 不变

新增 `page`/`size` 查询参数支持分页。

- [ ] **Step 3: 验证前端编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 4: 提交**

```bash
git add web/src/views/AuditView.vue
git commit -m "feat: switch audit frontend to new DB-backed API"
```

---

### Task 9: cmd/ 入口接线 & 端到端验证

**Files:**
- Modify: `cmd/bastion-core/main.go`

- [ ] **Step 1: 修改 Gateway 构造调用**

`cmd/bastion-core/main.go` 第111行：

```go
// 旧
dbGateway := dbproxy.NewGateway(cfg.DatabaseGateway, appStore, cfg.ReplayDir, logger, metadataDB, admin.LoadSuperAdminIDs(cfg, dataDir))
// 新
dbGateway := dbproxy.NewGateway(cfg.DatabaseGateway, appStore, cfg.ReplayDir, logger, metadataDB, admin.LoadSuperAdminIDs(cfg, dataDir), appStore)
```

`appStore` 是 `*store.DBStore`，已经实现了 `auditWriter` 接口。

- [ ] **Step 2: 全量编译**

```bash
cd C:\02-codespace\Jianmen && go build ./...
```

- [ ] **Step 3: 运行测试**

```bash
cd C:\02-codespace\Jianmen && go test ./... -count=1
```

- [ ] **Step 4: 前端编译验证**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 5: 提交**

```bash
git add cmd/
git commit -m "chore: wire audit store into DB proxy gateway"
```
