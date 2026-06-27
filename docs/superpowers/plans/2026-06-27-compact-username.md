# 紧凑用户名实现计划

> **对于 agentic worker：** 推荐使用 superpowers:subagent-driven-development 或 superpowers:executing-plans 来逐个任务实现此计划。步骤使用复选框 (`- [ ]`) 语法进行跟踪。

**目标：** 将连接用户名从长 hex 格式（`admin+<32位hex>@gateway` / `db-<12位hex>`）重构为 10 位紧凑格式（`H<资源ID4位><会话ID5位>`）。

**架构：** 新增 base62 编解码工具，在 HostAccount 和 DatabaseAccount 模型上添加 resource_seq/resource_id 字段支持按类型独立自增，新增 UserSession 模型管理用户身份会话（永久/临时），修改 SSH 和 DB 代理的登录解析逻辑以支持 10 位用户名解析，更新前端连接命令展示。

**技术栈：** Go 1.23+, GORM (SQLite), Vue 3 + TypeScript + Vite

## 全局约束

- 每次资源模型变化后，后端 API、前端页面、RBAC、审计、快速连接必须一起检查
- 后端改资源模型或代理逻辑后必须跑：`go test ./... -count=1`
- 前端改布局后必须跑：`npm run typecheck`、`npm run build`
- 不保留旧格式兼容，直接全部重构为新10位格式
- 所有数据库迁移必须通过 GORM AutoMigrate 自动执行

---

### Task 1:Base62 编解码工具

**文件：**
- 创建：`internal/util/base62.go`
- 创建：`internal/util/base62_test.go`

**接口：**
- 产出：`func EncodeBase62(n uint64) string` — 整数转62进制字符串
- 产出：`func DecodeBase62(s string) (uint64, error)` — 62进制字符串转整数
- 产出：`func EncodeBase62Padded(n uint64, width int) string` — 固定宽度编码（左补0）
- 产出：`const Base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"`

- [ ] **步骤1：编写测试**

`internal/util/base62_test.go`:
```go
package util

import (
    "testing"
)

func TestEncodeBase62(t *testing.T) {
    cases := []struct {
        n    uint64
        want string
    }{
        {0, "0"},
        {1, "1"},
        {9, "9"},
        {10, "a"},
        {35, "z"},
        {36, "A"},
        {61, "Z"},
        {62, "10"},
        {3844, "100"}, // 62*62 + 0
    }
    for _, c := range cases {
        got := EncodeBase62(c.n)
        if got != c.want {
            t.Errorf("EncodeBase62(%d) = %q, want %q", c.n, got, c.want)
        }
    }
}

func TestDecodeBase62(t *testing.T) {
    cases := []struct {
        s    string
        want uint64
        ok   bool
    }{
        {"0", 0, true},
        {"9", 9, true},
        {"a", 10, true},
        {"z", 35, true},
        {"A", 36, true},
        {"Z", 61, true},
        {"10", 62, true},
        {"100", 3844, true},
        {"", 0, false},
        {"@", 0, false},
    }
    for _, c := range cases {
        got, err := DecodeBase62(c.s)
        if c.ok && (err != nil || got != c.want) {
            t.Errorf("DecodeBase62(%q) = (%d, %v), want (%d, nil)", c.s, got, err, c.want)
        }
        if !c.ok && err == nil {
            t.Errorf("DecodeBase62(%q) should have failed", c.s)
        }
    }
}

func TestEncodeBase62Padded(t *testing.T) {
    cases := []struct {
        n     uint64
        width int
        want  string
    }{
        {0, 4, "0000"},
        {1, 4, "0001"},
        {62, 4, "0010"},
        {14776335, 4, "zzzz"}, // 62^4 - 1
    }
    for _, c := range cases {
        got := EncodeBase62Padded(c.n, c.width)
        if got != c.want {
            t.Errorf("EncodeBase62Padded(%d, %d) = %q, want %q", c.n, c.width, got, c.want)
        }
    }
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
    for _, n := range []uint64{0, 1, 61, 62, 100, 999, 14776335, 999999999} {
        s := EncodeBase62(n)
        got, err := DecodeBase62(s)
        if err != nil {
            t.Errorf("DecodeBase62(%q) failed: %v", s, err)
        }
        if got != n {
            t.Errorf("roundtrip: %d -> %q -> %d", n, s, got)
        }
    }
}
```

- [ ] **步骤2：运行测试确认失败**

```powershell
cd C:\02-codespace\Jianmen; go test ./internal/util/ -run TestEncodeBase62 -count=1 2>&1
```
预期：FAIL（包不存在或函数未定义）

- [ ] **步骤3：编写实现**

`internal/util/base62.go`:
```go
package util

import (
    "errors"
    "strings"
)

const Base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

var charToVal [256]int8

func init() {
    for i := range charToVal {
        charToVal[i] = -1
    }
    for i, c := range []byte(Base62Chars) {
        charToVal[c] = int8(i)
    }
}

func EncodeBase62(n uint64) string {
    if n == 0 {
        return "0"
    }
    var buf [20]byte
    i := len(buf)
    for n > 0 {
        i--
        buf[i] = Base62Chars[n%62]
        n /= 62
    }
    return string(buf[i:])
}

func DecodeBase62(s string) (uint64, error) {
    if s == "" {
        return 0, errors.New("empty string")
    }
    var n uint64
    for _, c := range []byte(s) {
        if c >= 128 || charToVal[c] < 0 {
            return 0, errors.New("invalid base62 character: " + string(c))
        }
        n = n*62 + uint64(charToVal[c])
    }
    return n, nil
}

func EncodeBase62Padded(n uint64, width int) string {
    s := EncodeBase62(n)
    if len(s) >= width {
        return s
    }
    return strings.Repeat("0", width-len(s)) + s
}
```

- [ ] **步骤4：运行测试确认通过**

```powershell
cd C:\02-codespace\Jianmen; go test ./internal/util/ -v -count=1 2>&1
```
预期：全部 PASS

- [ ] **步骤5：提交**

```bash
git add internal/util/base62.go internal/util/base62_test.go
git commit -m "feat: add base62 encoding/decoding utility

- EncodeBase62: uint64 to variable-length base62 string
- DecodeBase62: base62 string to uint64
- EncodeBase62Padded: fixed-width encoding with zero-padding
- Character set: 0-9a-zA-Z (62 chars)"
```

---

### Task 2:HostAccount 模型添加 resource_seq 和 resource_id

**文件：**
- 修改：`internal/model/core.go` — HostAccount 结构体
- 修改：`internal/store/dbstore.go` — 创建 HostAccount 时分配 resource_id
- 修改：`internal/store/store.go` — TargetView 添加 resource_id 字段
- 创建：`internal/store/resource_id.go` — 资源ID分配逻辑

**接口：**
- 消耗：`util.EncodeBase62Padded(n, 4)`（来自任务1）
- 产出：`func (s *DBStore) nextResourceSeq(resourceType string) (int, error)` — 获取下一个自增序号
- 产出：`func ResourceIDFromSeq(prefix string, seq int) string` — 前缀+4位编码

- [ ] **步骤1：在 HostAccount 结构体添加字段**

修改 `internal/model/core.go` 第 113-124 行，在 `HostAccount` 结构体 `Status` 字段后面添加：

```go
ResourceSeq int    `gorm:"index;not null;default:0" json:"resource_seq"`
ResourceID  string `gorm:"uniqueIndex;size:4" json:"resource_id"`
```

- [ ] **步骤2：在 DatabaseAccount 结构体添加字段**

同样在 `internal/model/core.go`，在 `DatabaseAccount` 结构体的 `Disabled` 字段后面添加：

```go
ResourceSeq int    `gorm:"index;not null;default:0" json:"resource_seq"`
ResourceID  string `gorm:"uniqueIndex;size:4" json:"resource_id"`
```

- [ ] **步骤3：创建紧凑用户名工具函数**

`internal/util/compact.go`（放在 util 包中，避免 access/store/dbproxy 之间的循环导入）:
```go
package util

import "fmt"

// 资源前缀常量
const (
    PrefixHost     = "H"
    PrefixDatabase = "D"
)

// ResourceIDFromSeq 从序号生成资源ID（前缀+4位62进制）
func ResourceIDFromSeq(prefix string, seq int) string {
    return prefix + EncodeBase62Padded(uint64(seq), 4)
}

// FullUsername 组装10位完整连接用户名
func FullUsername(prefix string, resourceSeq int, sessionSeq int) string {
    return prefix +
        EncodeBase62Padded(uint64(resourceSeq), 4) +
        EncodeBase62Padded(uint64(sessionSeq), 5)
}

// ParseCompactUsername 解析10位紧凑用户名
// 返回 prefix, resourceSeq, sessionSeq
func ParseCompactUsername(username string) (prefix string, resourceSeq uint64, sessionSeq uint64, err error) {
    if len(username) != 10 {
        return "", 0, 0, fmt.Errorf("compact username must be 10 characters, got %d", len(username))
    }
    prefix = string(username[0])
    rID, err := DecodeBase62(username[1:5])
    if err != nil {
        return "", 0, 0, fmt.Errorf("invalid resource id: %w", err)
    }
    sID, err := DecodeBase62(username[5:10])
    if err != nil {
        return "", 0, 0, fmt.Errorf("invalid session id: %w", err)
    }
    return prefix, rID, sID, nil
}
```

`internal/util/compact_test.go`:
```go
package util

import "testing"

func TestResourceIDFromSeq(t *testing.T) {
    cases := []struct {
        prefix string
        seq    int
        want   string
    }{
        {PrefixHost, 0, "H0000"},
        {PrefixHost, 1, "H0001"},
        {PrefixDatabase, 0, "D0000"},
    }
    for _, c := range cases {
        got := ResourceIDFromSeq(c.prefix, c.seq)
        if got != c.want {
            t.Errorf("ResourceIDFromSeq(%q, %d) = %q, want %q", c.prefix, c.seq, got, c.want)
        }
    }
}

func TestParseCompactUsername(t *testing.T) {
    prefix, rSeq, sSeq, err := ParseCompactUsername("H000100001")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if prefix != "H" || rSeq != 1 || sSeq != 1 {
        t.Errorf("got prefix=%q rSeq=%d sSeq=%d, want H/1/1", prefix, rSeq, sSeq)
    }
    _, _, _, err = ParseCompactUsername("short")
    if err == nil {
        t.Error("expected error for short username")
    }
}
```

`internal/store/resource_id.go`（存储层资源ID分配逻辑）:
```go
package store

import (
    "jianmen/internal/model"
    "jianmen/internal/util"
)

// nextHostResourceSeq 获取主机账号下一个自增序号
func (s *DBStore) nextHostResourceSeq() (int, error) {
    var maxSeq int
    if err := s.db.Model(&model.HostAccount{}).
        Select("COALESCE(MAX(resource_seq), 0)").Scan(&maxSeq).Error; err != nil {
        return 0, err
    }
    return maxSeq + 1, nil
}

// nextDBResourceSeq 获取数据库账号下一个自增序号
func (s *DBStore) nextDBResourceSeq() (int, error) {
    var maxSeq int
    if err := s.db.Model(&model.DatabaseAccount{}).
        Select("COALESCE(MAX(resource_seq), 0)").Scan(&maxSeq).Error; err != nil {
        return 0, err
    }
    return maxSeq + 1, nil
}
```

- [ ] **步骤4：修改 TargetView 添加 resource_id**

修改 `internal/store/store.go` 的 `TargetView` 结构体，添加：
```go
ResourceID  string `json:"resource_id"`
ResourceSeq int    `json:"resource_seq"`
```

- [ ] **步骤5：修改 DBStore.AddTarget 分配 resource_id**

修改 `internal/store/dbstore.go` 中 `AddTarget` 方法（约第 228-270 行），在创建 HostAccount 前添加：
```go
// 分配资源ID
seq, err := s.nextHostResourceSeq()
if err != nil {
    return TargetView{}, err
}
account.ResourceSeq = seq
account.ResourceID = util.ResourceIDFromSeq(util.PrefixHost, seq)
```

同样修改 `targetView()` 方法（约第 180 行），将 `ResourceID` 和 `ResourceSeq` 填入返回的 `TargetView`。

- [ ] **步骤6：修改 DBStore.AddDatabaseAccount 分配 resource_id**

修改 `internal/store/dbstore.go` 中 `AddDatabaseAccount` 方法，创建前添加：
```go
// 分配资源ID
seq, err := s.nextDBResourceSeq()
if err != nil {
    return DatabaseAccountView{}, err
}
acct.ResourceSeq = seq
acct.ResourceID = util.ResourceIDFromSeq(util.PrefixDatabase, seq)
```

- [ ] **步骤7：修改 StaticStore 同步适配**

`internal/access/static.go` 中 `AddDatabaseAccount`（约第 472 行）同样添加 resource_seq 和 resource_id 分配逻辑：
```go
var maxSeq int
s.db.Model(&model.DatabaseAccount{}).Select("COALESCE(MAX(resource_seq), 0)").Scan(&maxSeq)
acct.ResourceSeq = maxSeq + 1
acct.ResourceID = util.ResourceIDFromSeq(util.PrefixDatabase, acct.ResourceSeq)
```

常量 `util.PrefixDatabase`、`util.PrefixHost` 和函数 `util.ResourceIDFromSeq`、`util.ParseCompactUsername` 已在任务1-2中放入 `internal/util` 包，所有包均可直接使用。

- [ ] **步骤8：运行测试和编译检查**

```powershell
cd C:\02-codespace\Jianmen
go build ./...
go test ./internal/util/ -v -count=1 2>&1
go test ./internal/store/ -v -count=1 2>&1
go test ./... -count=1 2>&1
```

预期：编译通过，新测试 PASS，已有测试继续 PASS

- [ ] **步骤9：提交**

```bash
git add internal/util/base62.go internal/util/base62_test.go internal/util/compact.go internal/util/compact_test.go internal/store/resource_id.go internal/model/core.go internal/store/store.go internal/store/dbstore.go internal/access/static.go
git commit -m "feat: add resource_seq and resource_id to HostAccount and DatabaseAccount

- HostAccount and DatabaseAccount get resource_seq (auto-increment per type) and resource_id (4-char base62)
- New util/compact.go: ResourceIDFromSeq, FullUsername, ParseCompactUsername, prefix constants
- New util/base62.go: 62-char encoding/decoding utilities
- TargetView now exposes resource_id in API responses
- Auto-allocation on account creation via DBStore.nextHostResourceSeq/nextDBResourceSeq"
```

---

### Task 3:UserSession 模型与会话管理

**文件：**
- 创建：`internal/model/user_session.go` — UserSession 模型
- 修改：`internal/store/store.go` — Store 接口添加 Session 方法
- 修改：`internal/store/dbstore.go` — Session 存储实现
- 修改：`internal/server/admin/server.go` — Session 管理 API 路由和处理器

**接口：**
- 消耗：`util.EncodeBase62Padded(n, 5)`（会话ID 5位编码）
- 产出：`model.UserSession` 结构体
- 产出：`store.SessionView` 用于 API
- 产出：API 端点 `GET/POST /api/sessions`, `POST /api/sessions/{id}/disable`, `POST /api/sessions/{id}/enable`

- [ ] **步骤1：创建 UserSession 模型**

`internal/model/user_session.go`:
```go
package model

import (
    "time"
)

// UserSession 用户身份会话，用于连接用户名中的会话ID部分
type UserSession struct {
    ID         string     `gorm:"primaryKey;size:64" json:"id"`
    UserID     string     `gorm:"index;size:64;not null" json:"user_id"`
    User       User       `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
    SessionSeq int        `gorm:"not null" json:"session_seq"`       // 用户维度自增序号
    SessionID  string     `gorm:"size:5;not null" json:"session_id"` // 5位62进制
    Type       string     `gorm:"size:16;not null;default:permanent" json:"type"` // permanent / temporary
    Status     string     `gorm:"size:16;not null;default:active" json:"status"`   // active / disabled / expired
    ExpiresAt  *time.Time `gorm:"index" json:"expires_at,omitempty"`
    CreatedBy  string     `gorm:"size:128" json:"created_by,omitempty"`
    CreatedAt  time.Time  `json:"created_at"`
    UpdatedAt  time.Time  `json:"updated_at"`
}

func (UserSession) TableName() string {
    return "user_sessions"
}
```

- [ ] **步骤2：在 Session 模型中添加 UserSession 关联**

修改 `internal/model/session.go` 的 `Session` 结构体，在 `UserID` 字段后添加：
```go
UserSessionID string     `gorm:"index;size:64" json:"user_session_id,omitempty"`
UserSession   UserSession `gorm:"foreignKey:UserSessionID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"-"`
```

- [ ] **步骤3：在 Store 接口添加 SessionView 和方法**

修改 `internal/store/store.go`，添加：
```go
type SessionView struct {
    ID         string     `json:"id"`
    UserID     string     `json:"user_id"`
    Username   string     `json:"username"`
    SessionSeq int        `json:"session_seq"`
    SessionID  string     `json:"session_id"`
    Type       string     `json:"type"`
    Status     string     `json:"status"`
    ExpiresAt  *time.Time `json:"expires_at,omitempty"`
    CreatedBy  string     `json:"created_by,omitempty"`
    CreatedAt  time.Time  `json:"created_at"`
}
```

在 `type Store interface` 中添加方法：
```go
UserSessions(userID string) ([]SessionView, error)
CreateUserSession(session model.UserSession) (*model.UserSession, error)
DisableUserSession(id string) error
EnableUserSession(id string) error
UserSessionByID(sessionID string, userID string) (*model.UserSession, error)
```

- [ ] **步骤4：实现 UserSession 存储方法**

在 `internal/store/dbstore.go` 中实现：

```go
func (s *DBStore) UserSessions(userID string) ([]SessionView, error) {
    var sessions []model.UserSession
    q := s.db.Order("session_seq DESC")
    if userID != "" {
        q = q.Where("user_id = ?", userID)
    }
    if err := q.Find(&sessions).Error; err != nil {
        return nil, err
    }
    views := make([]SessionView, len(sessions))
    for i, sess := range sessions {
        views[i] = s.sessionView(sess)
    }
    return views, nil
}

func (s *DBStore) sessionView(sess model.UserSession) SessionView {
    username := ""
    var user model.User
    if s.db.Where("id = ?", sess.UserID).First(&user).Error == nil {
        username = user.Username
    }
    return SessionView{
        ID: sess.ID, UserID: sess.UserID, Username: username,
        SessionSeq: sess.SessionSeq, SessionID: sess.SessionID,
        Type: sess.Type, Status: sess.Status,
        ExpiresAt: sess.ExpiresAt, CreatedBy: sess.CreatedBy,
        CreatedAt: sess.CreatedAt,
    }
}

func (s *DBStore) CreateUserSession(sess model.UserSession) (*model.UserSession, error) {
    // 用户维度自增
    var maxSeq int
    s.db.Model(&model.UserSession{}).
        Where("user_id = ?", sess.UserID).
        Select("COALESCE(MAX(session_seq), 0)").Scan(&maxSeq)
    sess.SessionSeq = maxSeq + 1
    sess.SessionID = util.EncodeBase62Padded(uint64(sess.SessionSeq), 5)
    if err := s.db.Create(&sess).Error; err != nil {
        return nil, err
    }
    return &sess, nil
}

func (s *DBStore) DisableUserSession(id string) error {
    return s.db.Model(&model.UserSession{}).Where("id = ?", id).Update("status", "disabled").Error
}

func (s *DBStore) EnableUserSession(id string) error {
    return s.db.Model(&model.UserSession{}).Where("id = ?", id).Update("status", "active").Error
}

func (s *DBStore) UserSessionByID(sessionID string, userID string) (*model.UserSession, error) {
    var sess model.UserSession
    q := s.db.Where("session_id = ?", sessionID)
    if userID != "" {
        q = q.Where("user_id = ?", userID)
    }
    if err := q.First(&sess).Error; err != nil {
        return nil, err
    }
    return &sess, nil
}
```

- [ ] **步骤5：添加管理 API 路由和处理器**

修改 `internal/server/admin/server.go`，在 `registerRoutes` 中添加（约第 77 行附近）：
```go
mux.HandleFunc("GET /api/sessions", s.requirePermission(ActionRBACManage, s.handleSessions))
mux.HandleFunc("POST /api/sessions", s.requirePermission(ActionRBACManage, s.handleCreateSession))
mux.HandleFunc("POST /api/sessions/{id}/disable", s.requirePermission(ActionRBACManage, s.handleDisableSession))
mux.HandleFunc("POST /api/sessions/{id}/enable", s.requirePermission(ActionRBACManage, s.handleEnableSession))
```

添加处理器方法：
```go
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
    userID := r.URL.Query().Get("user_id")
    sessions, err := s.store.UserSessions(userID)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    if sessions == nil {
        sessions = []store.SessionView{}
    }
    writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
    var input struct {
        UserID    string `json:"user_id"`
        Type      string `json:"type"`
        ExpiresAt string `json:"expires_at"`
    }
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
        return
    }
    if input.UserID == "" {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id required"})
        return
    }
    if input.Type == "" {
        input.Type = "temporary"
    }
    sess := model.UserSession{
        UserID: input.UserID,
        Type:   input.Type,
    }
    if input.ExpiresAt != "" {
        t, err := time.Parse(time.RFC3339, input.ExpiresAt)
        if err != nil {
            writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expires_at format"})
            return
        }
        sess.ExpiresAt = &t
    }
    created, err := s.store.CreateUserSession(sess)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDisableSession(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if err := s.store.DisableUserSession(id); err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

func (s *Server) handleEnableSession(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if err := s.store.EnableUserSession(id); err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}
```

- [ ] **步骤6：编译和测试**

```powershell
cd C:\02-codespace\Jianmen
go build ./...
go test ./internal/store/ -v -count=1 2>&1
go test ./... -count=1 2>&1
```

预期：编译通过，已有测试全部 PASS

- [ ] **步骤7：提交**

```bash
git add internal/model/user_session.go internal/model/session.go internal/store/store.go internal/store/dbstore.go internal/server/admin/server.go
git commit -m "feat: add UserSession model and session management API

- UserSession model: per-user session with session_seq and 5-char session_id
- Session types: permanent (user creation) / temporary (admin grant)
- Session status: active / disabled / expired
- API: GET /api/sessions, POST /api/sessions, disable/enable endpoints
- DBStore: per-user auto-increment session_seq allocation"
```

---

### Task 4:用户创建时自动分配永久 Session

**文件：**
- 修改：`internal/server/admin/server.go` — 用户创建处理器

- [ ] **步骤1：修改用户创建处理器**

在 `handleCreateUser`（或等效的用户创建逻辑）中，创建用户后自动创建永久 session：

```go
// 在 handleCreateUser 中，用户创建成功后：
permSession := model.UserSession{
    UserID: createdUser.ID,
    Type:   "permanent",
    Status: "active",
}
if _, err := s.store.CreateUserSession(permSession); err != nil {
    slog.Error("failed to create permanent session", "error", err)
}
```

同时也为 `internal/storage/bootstrap.go` 中的内置用户（如 admin）创建永久 session。

- [ ] **步骤2：在 Bootstrap 中为内置用户创建 Session**

修改 `internal/storage/bootstrap.go`，在创建内置 admin 用户后：
```go
// 为内置用户创建永久session
var userSessionCount int64
db.Model(&model.UserSession{}).Where("user_id = ? AND type = ?", adminUser.ID, "permanent").Count(&userSessionCount)
if userSessionCount == 0 {
    permSession := model.UserSession{
        ID:     model.NewID(),
        UserID: adminUser.ID,
        Type:   "permanent",
        Status: "active",
    }
    // 使用原始DB执行创建（或者调用store方法）
    db.Create(&permSession)
}
```

- [ ] **步骤3：编译和测试**

```powershell
cd C:\02-codespace\Jianmen
go build ./...
go test ./... -count=1 2>&1
```

- [ ] **步骤4：提交**

```bash
git add internal/server/admin/server.go internal/storage/bootstrap.go
git commit -m "feat: auto-create permanent UserSession on user creation

- New users get a permanent 'active' session automatically
- Bootstrap creates permanent sessions for built-in admin user
- Session ID generated as per-user auto-increment base62 5-char"
```

---

### Task 5:SSH 登录解析重构（仅新格式）

**文件：**
- 修改：`internal/access/static.go` — `ParseLoginName()` 只解析10位紧凑格式
- 修改：`internal/store/client.go` — `parseLoginName()` 同步更新
- 修改：`internal/store/dbstore.go` — `DefaultTarget()` 支持 resource_id 查找

**接口：**
- 消耗：`util.ParseCompactUsername()`（来自任务2）
- 消耗：`util.EncodeBase62Padded()`（来自任务1）
- 产出：`LoginName` 结构体简化为只有紧凑格式字段

- [ ] **步骤1：简化 LoginName 结构体**

修改 `internal/access/static.go` 的 `LoginName`（第 54-57 行）：
```go
type LoginName struct {
    ResourceID string // 紧凑格式中的资源ID部分 (4位)
    SessionID  string // 紧凑格式中的会话ID部分 (5位)
}
```

- [ ] **步骤2：重写 ParseLoginName（仅解析10位格式）**

修改 `internal/access/static.go` 的 `ParseLoginName()`：
```go
func ParseLoginName(username string) (LoginName, error) {
    if len(username) != 10 {
        return LoginName{}, fmt.Errorf("connection username must be 10 characters, got %d", len(username))
    }
    prefix, _, _, err := util.ParseCompactUsername(username)
    if err != nil {
        return LoginName{}, err
    }
    if prefix != util.PrefixHost && prefix != util.PrefixDatabase {
        return LoginName{}, fmt.Errorf("unknown resource prefix: %s", prefix)
    }
    return LoginName{
        ResourceID: username[1:5],
        SessionID:  username[5:10],
    }, nil
}
```

- [ ] **步骤3：修改 StaticStore.Authenticate（仅紧凑格式）**

修改 `internal/access/static.go` 的 `Authenticate()`：
```go
func (s *StaticStore) Authenticate(_ context.Context, username, password string) (model.User, error) {
    login, err := ParseLoginName(username)
    if err != nil {
        return model.User{}, err
    }
    return s.authenticateCompact(login, password)
}
```
```

新增 `authenticateCompact` 方法：
```go
func (s *StaticStore) authenticateCompact(login LoginName, password string) (model.User, error) {
    // 1. 通过 sessionID 找到用户
    sessID := login.SessionID
    var userSession model.UserSession
    if err := s.db.Where("session_id = ? AND status = ?", sessID, "active").First(&userSession).Error; err != nil {
        return model.User{}, fmt.Errorf("invalid session: %w", err)
    }
    // 2. 检查过期
    if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
        s.db.Model(&userSession).Update("status", "expired")
        return model.User{}, errors.New("session expired")
    }
    // 3. 验证用户密码
    var user model.User
    if err := s.db.Where("id = ?", userSession.UserID).First(&user).Error; err != nil {
        return model.User{}, err
    }
    pwHash := sha256Hex(password)
    if user.PasswordHash != pwHash {
        return model.User{}, errors.New("authentication failed")
    }
    // 4. 查找目标资源
    if login.ResourceID != "" {
        var account model.HostAccount
        if err := s.db.Where("resource_id = ?", login.ResourceID).First(&account).Error; err == nil {
            user.RequestedTargetID = account.ID
        }
    }
    return user, nil
}
```

- [ ] **步骤4：同步修改 store/client.go 的 parseLoginName**

`internal/store/client.go` 第 124-131 行，同样只解析10位格式：
```go
func parseLoginName(username string) (LoginName, error) {
    if len(username) != 10 {
        return LoginName{}, fmt.Errorf("connection username must be 10 characters, got %d", len(username))
    }
    prefix, _, _, err := util.ParseCompactUsername(username)
    if err != nil {
        return LoginName{}, err
    }
    if prefix != util.PrefixHost && prefix != util.PrefixDatabase {
        return LoginName{}, fmt.Errorf("unknown resource prefix: %s", prefix)
    }
    return LoginName{
        ResourceID: username[1:5],
        SessionID:  username[5:10],
    }, nil
}
```

- [ ] **步骤5：修改 DBStore.DefaultTarget 支持 resource_id 查找**

`internal/store/dbstore.go` 第 312 行，当 `RequestedTargetID` 为空但可能有 resource_id 时，修改查找逻辑。在 `userForLoginLocked` 或 `DefaultTarget` 中，通过 resource_id 查找 HostAccount。

由于 `LoginName` 已经通过 `RequestedTargetID`（设为 HostAccount.ID）传递，`DefaultTarget` 基本不需要大改。

- [ ] **步骤6：编译和测试**

```powershell
cd C:\02-codespace\Jianmen
go build ./...
go test ./internal/access/... -v -count=1 2>&1
go test ./internal/store/... -v -count=1 2>&1
```

预期：编译通过

- [ ] **步骤7：提交**

```bash
git add internal/access/static.go internal/store/client.go internal/store/dbstore.go
git commit -m "feat: SSH login now uses 10-char compact username exclusively

- ParseLoginName accepts only 10-char format: prefix+resourceID(4)+sessionID(5)
- authenticateCompact validates session status, expiry, and user credentials
- DefaultTarget resolves resource via resource_id lookup
- Removed legacy 'user+targetID@host' format parsing"
```

---

### Task 6:数据库代理解析重构

**文件：**
- 修改：`internal/server/dbproxy/server.go` — PG 连接解析 10 位用户名
- 修改：`internal/access/static.go` — `DatabaseAccountByResourceID` 新增方法
- 修改：`internal/store/dbstore.go` — 数据库账户查找支持 resource_id
- 修改：`internal/store/store.go` — Store 接口新增方法

- [ ] **步骤1：新增 DatabaseAccountByResourceID 查询**

`internal/store/store.go` Store 接口添加：
```go
DatabaseAccountByResourceID(resourceID string) (*model.DatabaseAccount, error)
```

`internal/store/dbstore.go` 实现：
```go
func (s *DBStore) DatabaseAccountByResourceID(resourceID string) (*model.DatabaseAccount, error) {
    var acct model.DatabaseAccount
    if err := s.db.Preload("Instance").Where("resource_id = ?", resourceID).First(&acct).Error; err != nil {
        return nil, err
    }
    return &acct, nil
}
```

同样在 `internal/access/static.go` 中实现（用于 StaticStore）。

- [ ] **步骤2：重写 PG 代理解析（仅10位格式）**

修改 `internal/server/dbproxy/server.go` 中 `handlePG()`，在提取 `username` 后直接解析：

```go
compactUsername := strings.TrimSpace(username)
if len(compactUsername) != 10 {
    return g.writeErr(w, "connection username must be 10 characters")
}
prefix, _, _, err := util.ParseCompactUsername(compactUsername)
if err != nil || prefix != util.PrefixDatabase {
    return g.writeErr(w, "invalid database connection username format")
}
// 通过 resource_id 查找数据库账户
resourceID := compactUsername[1:5]
acct, err = g.store.DatabaseAccountByResourceID(resourceID)
if err != nil {
    return g.writeErr(w, "database account not found: "+err.Error())
}
// 通过 sessionID 查找用户会话
sessionID := compactUsername[5:10]
user, err = g.authenticateBySession(sessionID, password)
if err != nil {
    return g.writeErr(w, "authentication failed: "+err.Error())
}
```

- [ ] **步骤3：新增 authenticateBySession 辅助方法**

在 `internal/server/dbproxy/server.go` 中添加：
```go
func (g *Gateway) authenticateBySession(sessionID, password string) (model.User, error) {
    var userSession model.UserSession
    if err := g.db.Where("session_id = ? AND status = ?", sessionID, "active").
        First(&userSession).Error; err != nil {
        return model.User{}, fmt.Errorf("invalid session: %w", err)
    }
    if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
        g.db.Model(&userSession).Update("status", "expired")
        return model.User{}, errors.New("session expired")
    }
    // 根据用户名和密码认证用户
    return g.store.AuthenticateDirect(context.Background(), userSession.SessionID, password)
}
```

- [ ] **步骤4：编译和测试**

```powershell
cd C:\02-codespace\Jianmen
go build ./...
go test ./internal/server/dbproxy/... -v -count=1 2>&1
```

- [ ] **步骤5：提交**

```bash
git add internal/server/dbproxy/server.go internal/access/static.go internal/store/dbstore.go internal/store/store.go
git commit -m "feat: database proxy uses 10-char compact username exclusively

- PG proxy parses only 10-char format: D<resourceID4><sessionID5>
- New DatabaseAccountByResourceID lookup replaces uniqueName lookup
- authenticateBySession validates session and user identity
- Removed 'db-<hex>' uniqueName format for connections"
```

---

### Task 7:前端连接命令展示更新

**文件：**
- 修改：`web/src/views/QuickConnectView.vue` — SSH/SFTP 命令格式
- 修改：`web/src/views/HostsView.vue` — 连接弹窗显示
- 修改：`web/src/views/DatabaseView.vue` — 数据库连接命令

- [ ] **步骤1：更新 QuickConnectView 的连接命令**

修改 `web/src/views/QuickConnectView.vue` 第 177-190 行，`connectionItems` computed：

```typescript
const connectionItems = computed<ConnectionItem[]>(() => {
  if (!connectTarget.value) return [];

  const resourcePrefix = connectTarget.value.resource_type === 'database_account' ? 'D' : 'H';
  const resourceId = target.value.resource_id || '0000';
  const sessionId = userSessionId.value || '00000';
  const compactUser = `${resourcePrefix}${resourceId}${sessionId}`;

  const host = bastionHost.value.trim() || '127.0.0.1';
  const port = Number(bastionPort.value) || 47102;

  const items: ConnectionItem[] = [
    {
      label: 'SSH 命令',
      copyTarget: `ssh ${compactUser}@${host} -p ${port}`,
      description: '复制后在终端粘贴执行',
    },
    {
      label: 'SFTP 命令',
      copyTarget: `sftp -P ${port} ${compactUser}@${host}`,
      description: '复制后在终端粘贴执行',
    },
    {
      label: 'Web Terminal',
      copyTarget: `${window.location.origin}/terminal?user=${encodeURIComponent(compactUser)}`,
      description: '在浏览器中直接连接',
    },
    {
      label: '资源标识',
      copyTarget: `${connectTarget.value.resource_type || 'host_account'}:${resourcePrefix}${resourceId}`,
      description: '用于权限配置和审计追踪',
    },
  ];
  return items;
});
```

- [ ] **步骤2：更新 HostsView 的连接弹窗**

修改 `web/src/views/HostsView.vue` 第 536-568 行，`connectionConfigRows` computed：

```typescript
const connectionConfigRows = computed<ConnectionConfigRow[]>(() => {
  if (!connectTarget.value) return [];

  const resourceId = connectTarget.value.resource_id || '0000';
  const sessionId = userSessionId.value || '00000';
  const compactUser = `H${resourceId}${sessionId}`;

  const host = bastionHost.value.trim() || '127.0.0.1';
  const port = numberFrom(bastionPort.value, 47102);

  return [
    {
      label: 'SSH 命令',
      command: `ssh ${compactUser}@${host} -p ${port}`,
      copyTarget: `ssh ${compactUser}@${host} -p ${port}`,
    },
    {
      label: 'SFTP 命令',
      command: `sftp -P ${port} ${compactUser}@${host}`,
      copyTarget: `sftp -P ${port} ${compactUser}@${host}`,
    },
    {
      label: '资源标识',
      command: `host_account:H${resourceId}`,
      copyTarget: `host_account:H${resourceId}`,
    },
  ];
});
```

- [ ] **步骤3：更新 DatabaseView 的连接命令**

修改 `web/src/views/DatabaseView.vue` 第 510-538 行：

```typescript
const connectCommand = computed(() => {
  if (!connectTarget.value) return [];
  const host = instanceHost.value || '127.0.0.1';
  const port = Number(connectPort.value) || 33060;

  const resourceId = connectTarget.value.resource_id || '0000';
  const sessionId = userSessionId.value || '00000';
  const compactUser = `D${resourceId}${sessionId}`;

  const items: CommandItem[] = [
    {
      label: 'MySQL',
      command: `mysql --protocol=tcp -h ${host} -P ${port} -u ${compactUser} -p`,
      copyTarget: `mysql --protocol=tcp -h ${host} -P ${port} -u ${compactUser} -p`,
    },
    {
      label: 'PostgreSQL',
      command: `psql -h ${host} -p ${port} -U ${compactUser}`,
      copyTarget: `psql -h ${host} -p ${port} -U ${compactUser}`,
    },
    {
      label: '资源标识',
      command: `database_account:D${resourceId}`,
      copyTarget: `database_account:D${resourceId}`,
    },
  ];
  return items;
});
```

- [ ] **步骤4：新增 userSessionId 获取逻辑**

在 QuickConnectView 和 HostsView 中，需要获取当前用户的 session_id。添加 computed 或 API 调用：

```typescript
// 从当前用户获取其激活的永久 session
const userSessionId = ref<string>('00000');

async function loadUserSession() {
  try {
    const sessions = await apiClient.get('/api/sessions', {
      params: { user_id: currentUserId.value }
    });
    const activePerm = sessions.data.find(
      (s: any) => s.type === 'permanent' && s.status === 'active'
    );
    if (activePerm) {
      userSessionId.value = activePerm.session_id;
    }
  } catch {
    userSessionId.value = '00000';
  }
}
```

- [ ] **步骤5：更新前端 API client 类型定义**

修改 `web/src/api/client.ts`，在 `TargetRecord` 中添加：
```typescript
resource_id?: string;
resource_seq?: number;
```

在 `DBAccountRecord` 中添加：
```typescript
resource_id?: string;
resource_seq?: number;
```

- [ ] **步骤6：前端验证**

```powershell
cd C:\02-codespace\Jianmen\web
npm run typecheck 2>&1
npm run build 2>&1
```

预期：typecheck 无错误，build 成功

- [ ] **步骤7：提交**

```bash
git add web/src/views/QuickConnectView.vue web/src/views/HostsView.vue web/src/views/DatabaseView.vue web/src/api/client.ts
git commit -m "feat: update frontend connection display to 10-char compact username

- SSH/SFTP commands now use H<resourceID><sessionID>@host -p port
- Database commands now use D<resourceID><sessionID> in -u/-U flags
- Resource identifier format: host_account:H<id>, database_account:D<id>
- Auto-fetch user's active permanent session_id for display"
```

---

### Task 8:集成测试与最终验证

**文件：**
- 修改：`internal/store/dbstore.go` — 迁移现有数据的 resource_seq 和 resource_id

- [ ] **步骤1：添加紧凑格式解析单元测试**

在 `internal/access/static_test.go` 中：
```go
func TestParseLoginName_ValidCompact(t *testing.T) {
    ln, err := ParseLoginName("H000100001")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if ln.ResourceID != "0001" || ln.SessionID != "00001" {
        t.Errorf("got resource=%q session=%q, want 0001/00001", ln.ResourceID, ln.SessionID)
    }
}

func TestParseLoginName_InvalidLength(t *testing.T) {
    _, err := ParseLoginName("short")
    if err == nil {
        t.Error("expected error for non-10-char username")
    }
}

func TestParseLoginName_InvalidPrefix(t *testing.T) {
    _, err := ParseLoginName("X000100001")
    if err == nil {
        t.Error("expected error for unknown prefix 'X'")
    }
}
```

- [ ] **步骤2：为现有数据编写迁移逻辑**

在 `internal/store/dbstore.go` 中添加迁移函数：
```go
func (s *DBStore) migrateResourceIDs() error {
    var accounts []model.HostAccount
    s.db.Where("resource_seq = 0").Order("created_at ASC").Find(&accounts)
    for i, a := range accounts {
        seq := i + 1
        s.db.Model(&a).Updates(map[string]interface{}{
            "resource_seq": seq,
            "resource_id": util.ResourceIDFromSeq(util.PrefixHost, seq),
        })
    }
    var dbAccounts []model.DatabaseAccount
    s.db.Where("resource_seq = 0").Order("created_at ASC").Find(&dbAccounts)
    for i, a := range dbAccounts {
        seq := i + 1
        s.db.Model(&a).Updates(map[string]interface{}{
            "resource_seq": seq,
            "resource_id": util.ResourceIDFromSeq(util.PrefixDatabase, seq),
        })
    }
    return nil
}
```

在 `DBStore` 初始化时调用此迁移。

- [ ] **步骤3：运行完整测试套件**

```powershell
cd C:\02-codespace\Jianmen
go test ./... -count=1 2>&1
cd web
npm run typecheck 2>&1
npm run build 2>&1
```

预期：所有后端测试 PASS，前端 typecheck 无错误，build 成功

- [ ] **步骤4：手动集成测试**

```powershell
# 启动
.\start.ps1

# 测试 Admin API
curl -s --noproxy '*' -H "Authorization: Bearer dev-admin-token" http://127.0.0.1:47100/api/sessions

# 创建主机账号后验证 resource_id
curl -s --noproxy '*' -H "Authorization: Bearer dev-admin-token" -H "Content-Type: application/json" -X POST http://127.0.0.1:47100/api/targets -d '{"host":"192.168.1.1","port":22,"username":"root","password":"test"}'

# 浏览器打开 http://localhost:47101/ 确认连接命令显示10位格式
```

- [ ] **步骤5：提交**

```bash
git add internal/access/static_test.go internal/store/dbstore.go
git commit -m "feat: add backward compatibility and migration for compact usernames

- Legacy 'user+target@host' format continues to work
- Migration: auto-assign resource_seq/resource_id to existing accounts
- Compatibility tests for both formats"
```

---

### 验收检查清单

- [ ] `go build ./...` 编译通过
- [ ] `go test ./... -count=1` 全部 PASS
- [ ] `npm run typecheck`（web 目录）无错误
- [ ] `npm run build`（web 目录）成功
- [ ] SSH 连接使用新格式：`H<资源ID><会话ID>@127.0.0.1`
- [ ] 数据库连接使用新格式：`D<资源ID><会话ID>`
- [ ] 前端快速连接页面展示 10 位用户名
- [ ] 前端主机连接弹窗展示 10 位用户名
- [ ] 前端数据库连接弹窗展示 10 位用户名
- [ ] Session 管理 API 正常工作（列表/创建/禁用/启用）
- [ ] 过期 session 自动拒连
- [ ] 禁用 session 手动拒连
