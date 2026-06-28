# 前端页面统一优化 — 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 统一前端页面布局/分页/搜索，统一 SSH 与 DB 数据建模，清理后端死代码和冗余字段。

**Architecture:** 前端新建 DataTableCard/FormDialog/StatusSwitch 三个通用组件统一所有页面；后端删除死表/死字段，统一 Host↔DBInstance、HostAccount↔DBAccount 字段命名，所有列表 API 加后端分页+搜索；删除 access/static_adapter 遗留代码。

**Tech Stack:** Vue 3 + TypeScript + Element Plus (前端), Go + GORM (后端)

## Global Constraints

- 不考虑兼容性，大胆清理无用字段/代码/注释
- 所有列表 API 统一分页参数：`?page=1&page_size=20&q=关键词`
- 所有列表 API 统一响应格式：`{"items":[...], "total":N, "page":1, "page_size":20}`
- 每修改一个后端任务后运行 `go build ./... && go test ./... -count=1`
- 每修改一个前端任务后运行 `npm run typecheck && npm run build`

---

### Task 1: Backend — 删除死表（SessionCommand, SessionFileEvent, Recording, AuditLog）

**Files:**
- Modify: `internal/model/core.go`
- Modify: `internal/server/admin/server.go` (if any reference — check)

**Interfaces:**
- Produces: `AllModels()` 返回的模型切片少了 4 个元素

- [ ] **Step 1: 删除四个死模型 struct 定义**

在 `internal/model/core.go` 中删除以下 struct 的完整定义：
- `SessionCommand`（第 163-177 行）
- `SessionFileEvent`（第 179-196 行）
- `Recording`（第 198-208 行）
- `AuditLog`（第 251-262 行）

- [ ] **Step 2: 从 AllModels() 移除这四个模型**

修改 `internal/model/core.go` 的 `AllModels()` 函数，删除以下四行：
```go
&SessionCommand{},
&SessionFileEvent{},
&Recording{},
&AuditLog{},
```

- [ ] **Step 3: 删除对应的 BeforeCreate 钩子**

在 `internal/model/core.go` 中删除以下四个函数：
```go
func (m *SessionCommand) BeforeCreate(_ *gorm.DB) error      { return ensureID(&m.ID) }
func (m *SessionFileEvent) BeforeCreate(_ *gorm.DB) error    { return ensureID(&m.ID) }
func (m *Recording) BeforeCreate(_ *gorm.DB) error           { return ensureID(&m.ID) }
func (m *AuditLog) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
```

- [ ] **Step 4: 验证编译和测试**

```bash
cd C:\02-codespace\Jianmen && go build ./... && go test ./... -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/model/core.go
git commit -m "refactor: 删除未使用的死表 SessionCommand/SessionFileEvent/Recording/AuditLog"
```

---

### Task 2: Backend — 删除未使用字段

**Files:**
- Modify: `internal/model/core.go`

**Interfaces:**
- Consumes: Task 1 完成后的 `core.go`
- Produces: `HostAccount` 无 `CredentialRef`/`IsPrivileged`，`Resource` 无 `Attributes`

- [ ] **Step 1: 删除 HostAccount 中两个未使用字段**

在 `internal/model/core.go` 的 `HostAccount` struct 中，删除这两行：
```go
CredentialRef string         `gorm:"size:255" json:"credential_ref,omitempty"`
IsPrivileged  bool           `json:"is_privileged"`
```

- [ ] **Step 2: 删除 Resource.Attributes 字段**

在 `Resource` struct 中，删除：
```go
Attributes string    `gorm:"type:text" json:"attributes,omitempty"`
```

- [ ] **Step 3: 验证编译和测试**

```bash
cd C:\02-codespace\Jianmen && go build ./... && go test ./... -count=1
```

- [ ] **Step 4: Commit**

```bash
git add internal/model/core.go
git commit -m "refactor: 删除未使用字段 CredentialRef/IsPrivileged/Attributes"
```

---

### Task 3: Backend — 统一 Host/HostAccount 字段命名

**Files:**
- Modify: `internal/model/core.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/dbstore.go`
- Modify: `internal/server/admin/server.go`
- Modify: `internal/rbac/resources.go` (if references HostAccount fields)

**Interfaces:**
- Consumes: Task 2 完成后的字段
- Produces:
  - `Host.GroupName` (原 `Labels`) → JSON `"group"`
  - `HostAccount.GroupName` (原 `Labels`) → JSON `"group"`
  - `HostAccount.ExpiresAt` 新增
  - `HostView.Group` → JSON `"group"`，`HostView.Address` → JSON `"address"`
  - `HostView.Status` 保留，删除 `HostView.Static` 和 `HostView.Disabled`

- [ ] **Step 1: 修改 Host struct — Labels → GroupName，删除 Protocol**

在 `internal/model/core.go` 的 `Host` struct 中：
- 将 `Labels string \`gorm:"type:text" json:"labels,omitempty"\`` 改为 `GroupName string \`gorm:"size:128" json:"group"\``
- 删除 `Protocol string \`gorm:"size:32;not null;default:ssh" json:"protocol"\``

- [ ] **Step 2: 修改 HostAccount struct — Labels → GroupName，加 ExpiresAt**

在 `HostAccount` struct 中：
- 将 `Labels string \`gorm:"type:text" json:"labels,omitempty"\`` 改为 `GroupName string \`gorm:"size:128" json:"group"\``
- 在 `Remark` 后面新增：`ExpiresAt *time.Time \`gorm:"index" json:"expires_at,omitempty"\``

- [ ] **Step 3: 修改 HostView — 统一 JSON 命名，删除 Static/Disabled**

在 `internal/store/store.go` 的 `HostView` struct 中：
```go
type HostView struct {
    ID           string `json:"id"`
    Name         string `json:"name"`
    Group        string `json:"group"`         // was: json:"group,omitempty"
    Address      string `json:"address"`       // was: Host, json:"host"
    Port         int    `json:"port"`
    Remark       string `json:"remark"`
    Status       string `json:"status"`
    AccountCount int    `json:"account_count"`
    CreatedAt    string `json:"created_at"`
    UpdatedAt    string `json:"updated_at"`
}
```

删除字段：`Disabled`、`Static`（以及 `CreatedAt`/`UpdatedAt` 如原来没有则加上）。

- [ ] **Step 4: 修改 HostRecord — 统一 Group/Address 命名**

在 `internal/store/store.go` 的 `HostRecord` struct 中：
```go
type HostRecord struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    Group   string `json:"group"`
    Address string `json:"address"`           // was: Host json:"host"
    Port    int    `json:"port"`
    Remark  string `json:"remark"`
    Status  string `json:"status"`            // 新增，替代 Disabled
}
```

- [ ] **Step 5: 修改 TargetView — 统一 Group 命名**

在 `TargetView` struct 中，将 `Group` 的 JSON tag 从 `"group,omitempty"` 改为 `"group"`，删除 `Disabled` 字段（用 Status 替代），删除 `Static` 字段。

- [ ] **Step 6: 修改 dbstore.go — 适配新字段名**

在 `internal/store/dbstore.go` 中：
- `hostView()` 函数：`Host` → `Address`，`Group: host.Labels` → `Group: host.GroupName`，`Static: false` 删除，`Disabled` 逻辑改为只设置 `Status`
- `AddHost()`：`Labels: host.Group` → `GroupName: host.Group`
- `UpdateHost()`：`Labels: host.Group` → `GroupName: host.Group`；`Disabled` → `Status`
- 所有 `Accounts()` 查询中的 `Labels` → `GroupName`

- [ ] **Step 7: 修改 server.go — 适配新 HostView 字段**

在 `internal/server/admin/server.go` 中：
- `paginateHosts()` 中 `host.Host` → `host.Address`，`host.Group` 保持不变
- 所有读取 `HostRecord.Host` 的地方 → `HostRecord.Address`
- `handleCreateHost` 中 `json:"host"` → `json:"address"`

- [ ] **Step 8: 修改 RBAC/resources.go — 适配字段改名**

在 `internal/rbac/resources.go` 中，搜索 `Labels`、`Host`（指代地址字段的）并更新。

- [ ] **Step 9: 验证编译和测试**

```bash
cd C:\02-codespace\Jianmen && go build ./... && go test ./... -count=1
```

修复所有编译错误。预期会有多处编译错误因为字段名变更。

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "refactor: 统一 Host/HostAccount 字段命名（Labels→GroupName, Host→Address, 加ExpiresAt, 删Protocol）"
```

---

### Task 4: Backend — 统一 DatabaseInstance/DatabaseAccount 字段命名

**Files:**
- Modify: `internal/model/core.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/dbstore.go`
- Modify: `internal/server/admin/server.go`

**Interfaces:**
- Consumes: Task 3 完成后的代码
- Produces:
  - `DatabaseInstance.Port` 新增，`Disabled` → `Status` (string)
  - `DatabaseAccount.UpstreamUsername` → `Username`，`UpstreamPassword` → `Password`
  - `DatabaseAccount.Disabled` → `Status` (string)
  - `DatabaseAccountView` 相应字段更新

- [ ] **Step 1: 修改 DatabaseInstance struct**

在 `internal/model/core.go` 的 `DatabaseInstance` struct 中：
- 将 `Disabled bool \`json:"disabled"\`` 改为 `Status string \`gorm:"index;size:32;not null;default:active" json:"status"\``
- 在 `Address` 后面新增：`Port int \`gorm:"not null;default:3306" json:"port"\``
- GroupName JSON tag：`json:"group_name,omitempty"` → `json:"group"`

- [ ] **Step 2: 修改 DatabaseAccount struct**

在 `DatabaseAccount` struct 中：
- 将 `UpstreamUsername string` → `Username string \`gorm:"size:128;not null" json:"username"\``
- 将 `UpstreamPassword EncryptedField` → `Password EncryptedField \`gorm:"type:text" json:"-"\``
- 将 `Disabled bool \`json:"disabled"\`` → `Status string \`gorm:"index;size:32;not null;default:active" json:"status"\``
- GroupName JSON tag：`json:"group_name,omitempty"` → `json:"group"`

- [ ] **Step 3: 修改 DatabaseInstanceView**

在 `internal/store/store.go` 中：
```go
type DatabaseInstanceView struct {
    ID           string `json:"id"`
    Name         string `json:"name"`
    Protocol     string `json:"protocol"`
    Address      string `json:"address"`
    Port         int    `json:"port"`
    Group        string `json:"group"`         // was: GroupName json:"group_name"
    Remark       string `json:"remark"`
    Status       string `json:"status"`        // was: Disabled bool
    AccountCount int    `json:"account_count"`
}
```

- [ ] **Step 4: 修改 DatabaseAccountView**

在 `internal/store/store.go` 中：
```go
type DatabaseAccountView struct {
    ID         string     `json:"id"`
    InstanceID string     `json:"instance_id"`
    UniqueName string     `json:"unique_name"`
    Username   string     `json:"username"`       // was: UpstreamUsername
    Group      string     `json:"group"`           // was: GroupName
    Remark     string     `json:"remark"`
    ExpiresAt  *time.Time `json:"expires_at"`
    Status     string     `json:"status"`          // was: Disabled bool
    ResourceID string     `json:"resource_id"`
    ResourceSeq int       `json:"resource_seq"`
}
```

- [ ] **Step 5: 修改 Store 接口签名**

在 `internal/store/store.go` 的 `Store` interface 中，更新方法签名：
- `AddDatabaseInstance(name, protocol, address, groupName, remark string)` → 加 `port int` 参数
- `UpdateDatabaseInstance(...)` → `status` 替代 `disabled bool`
- `AddDatabaseAccount(instanceID, upstreamUsername, upstreamPassword, ...)` → `username, password`
- `UpdateDatabaseAccount(...)` → `username, password, status`

- [ ] **Step 6: 修改 dbstore.go — 适配所有 DB 操作**

在 `internal/store/dbstore.go` 中更新所有涉及上述字段的查询和赋值：
- `DatabaseInstance.Disabled` → `Status`
- `DatabaseAccount.UpstreamUsername` → `Username`
- `DatabaseAccount.UpstreamPassword` → `Password`
- `DatabaseAccount.Disabled` → `Status`
- `DatabaseInstance.GroupName` → `Group` (JSON tag)
- `DatabaseAccount.GroupName` → `Group` (JSON tag)

- [ ] **Step 7: 修改 server.go — 适配 API handler**

在 `internal/server/admin/server.go` 中更新所有 DB handler：
- `handleCreateDBInstance`：请求体字段更新，加 `port`
- `handleUpdateDBInstance`：`disabled` → `status`
- `handleCreateDBAccount`：`upstream_username` → `username`，`upstream_password` → `password`
- `handleUpdateDBAccount`：同上 + `disabled` → `status`
- `dbConnectionListItem`：字段名如有引用的更新

- [ ] **Step 8: 验证编译和测试**

```bash
cd C:\02-codespace\Jianmen && go build ./... && go test ./... -count=1
```

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "refactor: 统一 DatabaseInstance/DatabaseAccount 字段命名（Disabled→Status, Username/Password统一, 加Port）"
```

---

### Task 5: Backend — 删除 access/static_adapter

**Files:**
- Delete: `internal/access/static.go`
- Delete: `internal/access/static_test.go`
- Delete: `internal/store/static_adapter.go`
- Modify: `cmd/bastion-core/main.go`
- Modify: `internal/server/admin/server.go`
- Modify: `internal/server/admin/server_test.go`

**Interfaces:**
- Consumes: Task 4 完成
- Produces: `access` 包完全删除，`StaticAdapter` 删除，所有引用更新为直接用 `DBStore`

- [ ] **Step 1: 删除三个文件**

```bash
rm internal/access/static.go
rm internal/access/static_test.go
rm internal/store/static_adapter.go
```

- [ ] **Step 2: 修改 cmd/bastion-core/main.go**

将 `store.NewStaticAdapter(cfg, metadataDB)` 改为 `store.NewDBStore(metadataDB)`。

找到第 91 行附近的代码，改为：
```go
store := store.NewDBStore(metadataDB)
```

同时删除 `access` 的 import。

- [ ] **Step 3: 修改 server.go — 更新 access 引用**

在 `internal/server/admin/server.go` 中：
- `access.ClientConfigForTarget(target)` → `store.ClientConfigForTarget(target)`（`store/client.go` 已有完全相同的函数）
- 删除 `import "jianmen/internal/access"` 行
- 三处 `access.Err*` 引用改为 `store.Err*`（`store/store.go` 里已定义了同名的 sentinel error）

具体修改：
```go
// Line 717: 改为
clientConfig, err := store.ClientConfigForTarget(target)

// Line 1643: 删除 || errors.Is(err, access.ErrHostNotFound)
// 改为:
case errors.Is(err, store.ErrHostNotFound):

// Line 1653: 删除 access.Err* 引用，只保留 store.Err*
// 改为:
errors.Is(err, store.ErrDBProxyNotFound) || errors.Is(err, store.ErrDBAccountNotFound) || errors.Is(err, store.ErrDBInstanceNotFound):

// Line 1662: 删除 || errors.Is(err, access.ErrTargetNotFound)
// 改为:
case errors.Is(err, store.ErrTargetNotFound):
```

- [ ] **Step 4: 修改 server_test.go — 使用 DBStore**

在 `internal/server/admin/server_test.go` 中，将 `store.NewStaticAdapter(cfg, nil)` 改为 `store.NewDBStore(nil)` 或相应调整为使用内存 SQLite。

如果测试依赖 JSON 文件数据，需要重写测试以使用 DBStore + 预置数据。

- [ ] **Step 5: 验证编译和测试**

```bash
cd C:\02-codespace\Jianmen && go build ./... && go test ./... -count=1
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: 删除 access/static_adapter 遗留代码，统一使用 DBStore"
```

---

### Task 6: Backend — 所有列表 API 加分页+搜索

**Files:**
- Modify: `internal/server/admin/server.go`
- Modify: `internal/server/admin/rbac.go`

**Interfaces:**
- Produces: 统一的 `PageResponse` 类型，所有 GET 列表接口支持 `?page&page_size&q`

- [ ] **Step 1: 定义通用分页响应和辅助函数**

在 `internal/server/admin/server.go` 中添加：

```go
type pageResponse struct {
    Items    any   `json:"items"`
    Total    int   `json:"total"`
    Page     int   `json:"page"`
    PageSize int   `json:"page_size"`
}

// paginateSlice 对内存切片做分页和过滤
func paginateSlice[T any](items []T, r *http.Request, match func(T) bool) pageResponse {
    q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
    if q != "" {
        filtered := items[:0]
        for _, item := range items {
            if match(item) {
                filtered = append(filtered, item)
            }
        }
        items = filtered
    }
    page := positiveIntRequestQuery(r, "page", 1)
    pageSize := positiveIntRequestQuery(r, "page_size", 20)
    if pageSize > 200 { pageSize = 200 }
    total := len(items)
    start := (page - 1) * pageSize
    if start > total { start = total }
    end := start + pageSize
    if end > total { end = total }
    return pageResponse{
        Items:    items[start:end],
        Total:    total,
        Page:     page,
        PageSize: pageSize,
    }
}
```

- [ ] **Step 2: 更新 listUsers — 加分页+搜索**

修改 `server.go` 的 `listUsers` 函数（约第 386 行）：

```go
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
    if s.db != nil {
        var users []model.User
        q := strings.TrimSpace(r.URL.Query().Get("q"))
        tx := s.db.Model(&model.User{})
        if q != "" {
            like := "%" + q + "%"
            tx = tx.Where("username LIKE ? OR display_name LIKE ? OR email LIKE ?", like, like, like)
        }
        var total int64
        tx.Count(&total)
        page := positiveIntRequestQuery(r, "page", 1)
        pageSize := positiveIntRequestQuery(r, "page_size", 20)
        if pageSize > 200 { pageSize = 200 }
        if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
            writeError(w, http.StatusInternalServerError, err)
            return
        }
        type userWithFlag struct {
            model.User
            IsSuperAdmin bool `json:"is_super_admin"`
        }
        out := make([]userWithFlag, len(users))
        for i, u := range users {
            out[i] = userWithFlag{User: u, IsSuperAdmin: s.isSuperAdmin(u.ID)}
        }
        writeJSON(w, http.StatusOK, pageResponse{Items: out, Total: int(total), Page: page, PageSize: pageSize})
        return
    }
    writeJSON(w, http.StatusOK, s.store.Users())
}
```

- [ ] **Step 3: 更新 paginateHosts — 用泛型函数替换**

将 `paginateHosts` 函数简化为调用 `paginateSlice`：

```go
func paginateHosts(hosts []store.HostView, r *http.Request) pageResponse {
    return paginateSlice(hosts, r, func(h store.HostView) bool {
        q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
        return strings.Contains(strings.ToLower(h.Name), q) ||
            strings.Contains(strings.ToLower(h.Address), q) ||
            strings.Contains(strings.ToLower(h.Group), q) ||
            strings.Contains(strings.ToLower(h.Remark), q) ||
            strings.Contains(strings.ToLower(strconv.Itoa(h.Port)), q)
    })
}
```

同时修改 `handleHosts` 中 `pagedHostList` → `pageResponse`：
```go
writeJSON(w, http.StatusOK, paginateHosts(s.store.Hosts(), r))
```

同时删除 `pagedHostList` 类型定义（被 `pageResponse` 替代）。

- [ ] **Step 4: 更新 DB instances handler — 加分页**

修改 `handleDBInstances`（约第 840 行）中的 GET 分支：

```go
case http.MethodGet:
    instances := s.store.DatabaseInstances()
    resp := paginateSlice(instances, r, func(v store.DatabaseInstanceView) bool {
        q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
        return strings.Contains(strings.ToLower(v.Name), q) ||
            strings.Contains(strings.ToLower(v.Address), q) ||
            strings.Contains(strings.ToLower(v.Protocol), q) ||
            strings.Contains(strings.ToLower(v.Group), q) ||
            strings.Contains(strings.ToLower(v.Remark), q)
    })
    writeJSON(w, http.StatusOK, resp)
```

- [ ] **Step 5: 更新 host accounts handler — 加分页**

修改 `handleHost` 中 `child == "accounts"` 分支（约第 591 行）：

```go
accounts, err := s.store.HostAccounts(id)
if err != nil { ... }
resp := paginateSlice(accounts, r, func(v store.TargetView) bool { ... })
writeJSON(w, http.StatusOK, resp)
```

- [ ] **Step 6: 更新 DB instance accounts handler — 加分页**

修改 `handleDBInstance` 中 `child == "accounts"` 分支（约第 888 行），用 `paginateSlice` 替换现有的手动分页逻辑。

- [ ] **Step 7: 更新 sessions handler — 加分页+搜索**

修改 `handleSessions`（约第 1176 行）：

```go
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
    sessions, err := s.listSessions()
    if err != nil { ... }
    resp := paginateSlice(sessions, r, func(v sessionListItem) bool {
        q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
        return strings.Contains(strings.ToLower(v.User), q) ||
            strings.Contains(strings.ToLower(v.Target), q) ||
            strings.Contains(strings.ToLower(v.Protocol), q) ||
            strings.Contains(strings.ToLower(v.ClientIP), q)
    })
    writeJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 8: 更新 DB connections handler — 加分页+搜索**

修改 `handleDBConnections`（约第 1216 行），同 sessions 模式：

```go
func (s *Server) handleDBConnections(w http.ResponseWriter, r *http.Request) {
    connections, err := s.listDBConnections()
    if err != nil { ... }
    resp := paginateSlice(connections, r, func(v dbConnectionListItem) bool {
        q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
        return strings.Contains(strings.ToLower(v.AccountName), q) ||
            strings.Contains(strings.ToLower(v.InstanceName), q) ||
            strings.Contains(strings.ToLower(v.Protocol), q) ||
            strings.Contains(strings.ToLower(v.AuthUser), q)
    })
    writeJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 9: 更新 RBAC handlers — 加分页+搜索**

在 `internal/server/admin/rbac.go` 中，更新以下 handler 的 GET 分支：
- `handleRBACRoles`（第 57 行）— 加分页+搜索
- `handleRBACPermissions` — 加分页+搜索
- `handleRBACUserRoles` — 加分页+搜索
- `handleRBACRolePermissions` GET — 加分页+搜索

每个 handler 的 GET 分支改为类似模式：
```go
case http.MethodGet:
    q := strings.TrimSpace(r.URL.Query().Get("q"))
    tx := db.Model(&model.Role{})
    if q != "" {
        like := "%" + q + "%"
        tx = tx.Where("name LIKE ? OR description LIKE ?", like, like)
    }
    var total int64
    tx.Count(&total)
    page := positiveIntRequestQuery(r, "page", 1)
    pageSize := positiveIntRequestQuery(r, "page_size", 20)
    var roles []model.Role
    tx.Order("created_at DESC").Offset((page-1)*pageSize).Limit(pageSize).Find(&roles)
    writeJSON(w, http.StatusOK, pageResponse{Items: roles, Total: int(total), Page: page, PageSize: pageSize})
```

- [ ] **Step 10: 验证编译和测试**

```bash
cd C:\02-codespace\Jianmen && go build ./... && go test ./... -count=1
```

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "feat: 所有列表 API 统一分页+搜索（page/page_size/q）"
```

---

### Task 7: Frontend — 全局 CSS 清理 + 设计系统变量

**Files:**
- Modify: `web/src/styles/main.css`
- Modify: `web/src/App.vue`

**Interfaces:**
- Produces: CSS 变量、统一布局类、删除响应式断点和废弃 class

- [ ] **Step 1: 添加 CSS 设计变量**

在 `web/src/styles/main.css` 的 `:root` 块中添加：
```css
:root {
  /* 原有变量保持不变... */
  --gap-xs: 4px;
  --gap-sm: 8px;
  --gap-md: 16px;
  --gap-lg: 24px;
  --radius-sm: 4px;
  --radius-md: 8px;
  --color-bg: #f5f7fb;
  --color-card: #ffffff;
  --color-border: #eaecf0;
  --color-text: #101828;
  --color-text-secondary: #667085;
  --sidebar-width: 236px;
  --header-height: 72px;
}
```

- [ ] **Step 2: 修改 app-main 样式 — 占满屏幕**

```css
.app-main {
  height: calc(100vh - var(--header-height));
  padding: var(--gap-md);
  background: var(--color-bg);
  overflow: hidden;
}
```

- [ ] **Step 3: 添加页面容器样式**

```css
.page-container {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: var(--gap-md);
}

.page-card {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  background: var(--color-card);
  border-radius: var(--radius-md);
  border: 1px solid var(--color-border);
  overflow: hidden;
}

.page-card__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--gap-sm);
  padding: var(--gap-md);
  border-bottom: 1px solid var(--color-border);
  flex-shrink: 0;
}

.page-card__body {
  flex: 1;
  overflow: auto;
  min-height: 0;
}

.page-card__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: var(--gap-sm) var(--gap-md);
  border-top: 1px solid var(--color-border);
  flex-shrink: 0;
}
```

- [ ] **Step 4: 删除废弃样式和响应式断点**

删除以下 CSS 规则：
- 整个 `@media (max-width: 780px)` 块
- `.view-stack`、`.metric-grid`、`.metric-card`、`.metric-value`、`.placeholder-panel`、`.empty-state`（如果页面中没用到的话，搜索确认）

- [ ] **Step 5: 修改 App.vue — 使用 CSS 变量**

在 `App.vue` 中，将硬编码的数值替换为 CSS 变量：
- 侧边栏宽度：`:width="236"` → 从 CSS 变量读取
- Header 高度相关

- [ ] **Step 6: 验证前端编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 7: Commit**

```bash
git add web/src/styles/main.css web/src/App.vue
git commit -m "style: 全局 CSS 清理，添加设计变量和统一布局类"
```

---

### Task 8: Frontend — 创建 DataTableCard 组件

**Files:**
- Create: `web/src/components/DataTableCard.vue`

**Interfaces:**
- Consumes: Task 7 的 CSS 变量
- Produces: `<DataTableCard>` 组件，所有页面使用

- [ ] **Step 1: 创建 DataTableCard.vue**

```vue
<template>
  <div class="page-card">
    <div class="page-card__toolbar" v-if="showSearch || $slots['toolbar-extra']">
      <el-input
        v-if="showSearch"
        v-model="searchText"
        :placeholder="searchPlaceholder || '搜索...'"
        clearable
        style="width: 280px"
        @keyup.enter="emit('search', searchText)"
        @clear="emit('search', '')"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      <div style="flex:1" v-if="showSearch"></div>
      <slot name="toolbar-extra"></slot>
    </div>
    <div class="page-card__body">
      <el-table
        :data="data"
        :row-key="rowKey"
        v-loading="loading"
        size="small"
        stripe
        highlight-current-row
        @row-click="emit('row-click', $event)"
        style="width: 100%"
        height="100%"
      >
        <slot></slot>
      </el-table>
    </div>
    <div class="page-card__footer" v-if="total > 0">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="currentPageSize"
        :page-sizes="pageSizes"
        :total="total"
        layout="total, sizes, prev, pager, next"
        size="small"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { Search } from '@element-plus/icons-vue';

const props = withDefaults(defineProps<{
  data: any[];
  loading?: boolean;
  total: number;
  page: number;
  pageSize: number;
  pageSizes?: number[];
  showSearch?: boolean;
  searchPlaceholder?: string;
  rowKey?: string;
}>(), {
  loading: false,
  pageSizes: () => [20, 50, 100],
  showSearch: true,
  rowKey: 'id',
});

const emit = defineEmits<{
  'update:page': [page: number];
  'update:pageSize': [size: number];
  'search': [keyword: string];
  'row-click': [row: any];
}>();

const searchText = ref('');

const currentPage = computed({
  get: () => props.page,
  set: (v) => emit('update:page', v),
});
const currentPageSize = computed({
  get: () => props.pageSize,
  set: (v) => emit('update:pageSize', v),
});
</script>
```

- [ ] **Step 2: 验证前端编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/DataTableCard.vue
git commit -m "feat: 添加 DataTableCard 统一数据表格卡片组件"
```

---

### Task 9: Frontend — 创建 FormDialog + StatusSwitch 组件，删除 PaginationBar

**Files:**
- Create: `web/src/components/FormDialog.vue`
- Create: `web/src/components/StatusSwitch.vue`
- Delete: `web/src/components/PaginationBar.vue`

**Interfaces:**
- Consumes: Task 8
- Produces: `FormDialog`, `StatusSwitch` 组件

- [ ] **Step 1: 创建 FormDialog.vue**

```vue
<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    :title="title"
    :width="width"
    :close-on-click-modal="false"
    destroy-on-close
  >
    <div class="form-dialog-body">
      <slot></slot>
    </div>
    <template #footer>
      <el-button @click="emit('update:visible', false)">取消</el-button>
      <el-button type="primary" :loading="loading" @click="emit('submit')">
        {{ submitText }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  visible: boolean;
  title: string;
  width?: string;
  loading?: boolean;
  submitText?: string;
}>(), {
  width: '480px',
  loading: false,
  submitText: '保存',
});

const emit = defineEmits<{
  'update:visible': [value: boolean];
  'submit': [];
}>();
</script>

<style scoped>
.form-dialog-body {
  max-height: 60vh;
  overflow-y: auto;
  padding-right: 4px;
}
</style>
```

- [ ] **Step 2: 创建 StatusSwitch.vue**

```vue
<template>
  <el-switch
    :model-value="modelValue"
    @update:model-value="emit('update:modelValue', $event)"
    :loading="loading"
    size="small"
  />
</template>

<script setup lang="ts">
defineProps<{
  modelValue: boolean;
  loading?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();
</script>
```

- [ ] **Step 3: 删除 PaginationBar.vue**

```bash
rm web/src/components/PaginationBar.vue
```

搜索项目中是否还有引用 `PaginationBar`，如有则后续在页面迁移任务中一并更新。

- [ ] **Step 4: 验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck
```

- [ ] **Step 5: Commit**

```bash
git add web/src/components/FormDialog.vue web/src/components/StatusSwitch.vue
git rm web/src/components/PaginationBar.vue
git commit -m "feat: 添加 FormDialog/StatusSwitch 组件，删除 PaginationBar"
```

---

### Task 10: Frontend — 更新 API 类型和客户端

**Files:**
- Modify: `web/src/api/client.ts`

**Interfaces:**
- Consumes: Task 6（后端分页 API）完成
- Produces: 统一的 `PageResponse<T>` 类型，更新所有列表方法的返回类型和参数

- [ ] **Step 1: 定义统一类型**

在 `web/src/api/client.ts` 中添加：

```ts
// 统一分页响应
export interface PageResponse<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

// 统一端点视图
export interface EndpointView {
  id: string;
  name: string;
  address: string;
  port: number;
  group: string;
  remark: string;
  status: string;
  account_count: number;
}

// SSH 端点（扩展字段）
export interface HostView extends EndpointView {
  // address 已包含
}

// DB 端点（扩展字段）
export interface DatabaseInstanceView extends EndpointView {
  protocol: string;
}

// 统一账号视图
export interface AccountView {
  id: string;
  username: string;
  group: string;
  remark: string;
  status: string;
  resource_id: string;
}

// SSH 账号
export interface HostAccountView extends AccountView {
  host_id: string;
  auth_type: string;
  expires_at?: string;
  resource_type: string;
  host: string;
  port: number;
}

// DB 账号
export interface DatabaseAccountView extends AccountView {
  instance_id: string;
  unique_name: string;
  expires_at?: string;
}

// 统一审计记录
export interface AuditRecord {
  id: string;
  instance_name: string;
  account_name: string;
  operator: string;
  protocol: string;
  started_at: string;
  duration_ms?: number;
}
```

- [ ] **Step 2: 更新 API 方法签名**

更新 `client.ts` 中所有列表方法，统一接受分页参数并返回 `PageResponse<T>`：

```ts
// Hosts
getHosts(params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<HostView>>

// HostAccounts
getHostAccounts(hostId: string, params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<HostAccountView>>

// DatabaseInstances
getDBInstances(params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<DatabaseInstanceView>>

// InstanceAccounts
getDBInstanceAccounts(instanceId: string, params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<DatabaseAccountView>>

// Sessions
getSessions(params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<SessionItem>>

// DB Connections
getDBConnections(params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<DBConnectionItem>>

// Users
getUsers(params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<UserItem>>

// RBAC Roles
getRBACRoles(params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<Role>>

// RBAC Permissions
getRBACPermissions(params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<Permission>>

// Targets
getTargets(params?: { page?: number; page_size?: number; q?: string }): Promise<PageResponse<TargetItem>>
```

每个方法内部构建 query string：`?page=${p.page || 1}&page_size=${p.page_size || 20}&q=${encodeURIComponent(p.q || '')}`。

对于原来没有分页的方法，需要修改调用 URL 和解析方式（从直接返回数组改为解析 `PageResponse`）。

- [ ] **Step 3: 验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck
```

预期有部分页面报错（因为 API 返回类型变了），这些会在后续页面迁移任务中修复。

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts
git commit -m "refactor: 统一前端 API 类型和分页响应格式"
```

---

### Task 11: Frontend — 迁移 HostsView

**Files:**
- Modify: `web/src/views/HostsView.vue`

**Interfaces:**
- Consumes: DataTableCard, FormDialog, StatusSwitch, 统一 API 类型
- Produces: 改造后的主机管理页面

- [ ] **Step 1: 替换表格为 DataTableCard**

将页面主体替换为：
```vue
<DataTableCard
  :data="hosts"
  :loading="loading"
  :total="total"
  v-model:page="page"
  v-model:page-size="pageSize"
  search-placeholder="搜索主机名称、地址、分组..."
  @search="onSearch"
>
  <template #toolbar-extra>
    <el-button type="primary" @click="showCreate = true">新增主机</el-button>
  </template>
  <el-table-column prop="name" label="主机名称" min-width="140" />
  <el-table-column label="地址" min-width="160">
    <template #default="{ row }">{{ row.address }}:{{ row.port }}</template>
  </el-table-column>
  <el-table-column label="账号数" width="80" align="center">
    <template #default="{ row }">
      <el-button link type="primary" @click="showAccounts(row)">{{ row.account_count }}</el-button>
    </template>
  </el-table-column>
  <el-table-column prop="group" label="分组" width="100" />
  <el-table-column label="状态" width="80" align="center">
    <template #default="{ row }">
      <StatusSwitch
        :model-value="row.status === 'active'"
        @update:model-value="toggleHost(row)"
      />
    </template>
  </el-table-column>
  <el-table-column prop="remark" label="备注" min-width="120" show-overflow-tooltip />
  <el-table-column label="操作" width="120" align="right" fixed="right">
    <template #default="{ row }">
      <el-button link type="primary" size="small" @click="editHost(row)">编辑</el-button>
      <el-button link type="danger" size="small" @click="deleteHost(row)">删除</el-button>
    </template>
  </el-table-column>
</DataTableCard>
```

- [ ] **Step 2: 更新数据逻辑**

```ts
const hosts = ref<HostView[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const keyword = ref('');
const loading = ref(false);

async function fetchHosts() {
  loading.value = true;
  try {
    const res = await api.getHosts({ page: page.value, page_size: pageSize.value, q: keyword.value });
    hosts.value = res.items;
    total.value = res.total;
  } finally {
    loading.value = false;
  }
}

function onSearch(q: string) {
  keyword.value = q;
  page.value = 1;
  fetchHosts();
}

watch([page, pageSize], () => fetchHosts());
onMounted(() => fetchHosts());
```

- [ ] **Step 3: 替换弹窗为 FormDialog**

将创建/编辑主机的 `el-dialog` 替换为：
```vue
<FormDialog
  v-model:visible="showCreate"
  title="新增主机"
  width="640px"
  :loading="submitting"
  @submit="createHost"
>
  <el-form :model="hostForm" label-width="80px">
    <el-form-item label="主机地址" required>
      <el-input v-model="hostForm.address" placeholder="IP 或域名，可含端口如 192.168.1.1:22" />
    </el-form-item>
    <el-form-item label="端口">
      <el-input-number v-model="hostForm.port" :min="1" :max="65535" />
    </el-form-item>
    <el-form-item label="更多设置">
      <!-- 折叠：名称、分组、备注 -->
    </el-form-item>
  </el-form>
</FormDialog>
```

注意：`HostRecord` 的 JSON 字段已从 `host` 改为 `address`，前端提交时需要对应。

- [ ] **Step 4: 更新账号管理部分**

账号管理内嵌弹窗同样用 FormDialog 替换。账号列表用 DataTableCard（如需要分页则加分页）。

- [ ] **Step 5: 删除页面中的重复 CSS**

删除所有 `scoped` 样式中的 flex/布局代码，这些现在由 `main.css` 的 `.page-container`/`.page-card` 系列处理。保留必要的表单特定样式。

- [ ] **Step 6: 验证编译和构建**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 7: Commit**

```bash
git add web/src/views/HostsView.vue
git commit -m "refactor: HostsView 迁移到 DataTableCard/FormDialog/StatusSwitch"
```

---

### Task 12: Frontend — 迁移 DatabaseView

**Files:**
- Modify: `web/src/views/DatabaseView.vue`

**Interfaces:**
- Consumes: DataTableCard, FormDialog, StatusSwitch, 统一 API 类型
- Produces: 改造后的数据库管理页面

- [ ] **Step 1: 替换实例列表为 DataTableCard**

与 HostsView 类似模式，但列定义不同（多 protocol 列）。使用后端分页代替客户端 `instanceKeyword` 过滤。

- [ ] **Step 2: 替换账号列表为 DataTableCard**

账号管理子页面同样用 DataTableCard，加分页。

- [ ] **Step 3: 替换弹窗为 FormDialog**

创建/编辑实例弹窗、创建/编辑账号弹窗都用 FormDialog。

- [ ] **Step 4: 更新字段名适配后端变更**

- `group_name` → `group`
- `disabled` → `status`
- `upstream_username` → `username`
- `upstream_password` → `password`

- [ ] **Step 5: 删除重复 CSS，验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 6: Commit**

```bash
git add web/src/views/DatabaseView.vue
git commit -m "refactor: DatabaseView 迁移到 DataTableCard/FormDialog，适配统一字段名"
```

---

### Task 13: Frontend — 迁移 AuditView

**Files:**
- Modify: `web/src/views/AuditView.vue`

**Interfaces:**
- Consumes: DataTableCard, 统一 API 类型
- Produces: 改造后的审计页面

- [ ] **Step 1: SSH 审计标签页 — 用 DataTableCard**

替换 SSH 审计表格为 DataTableCard，搜索框由组件内部提供。删除页面自建的过滤栏（用户搜索、目标搜索、日期范围），简化为统一搜索框。

- [ ] **Step 2: DB 审计标签页 — 用 DataTableCard**

同样替换。

- [ ] **Step 3: 审计详情抽屉 — 内部表格用 DataTableCard**

命令记录、文件事件、SQL 查询的表格用 DataTableCard（showSearch=false）。

- [ ] **Step 4: 更新数据获取逻辑**

```ts
async function fetchSessions() {
  const res = await api.getSessions({ page: page.value, page_size: pageSize.value, q: keyword.value });
  sessions.value = res.items;
  total.value = res.total;
}
```

- [ ] **Step 5: 验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 6: Commit**

```bash
git add web/src/views/AuditView.vue
git commit -m "refactor: AuditView 迁移到 DataTableCard，后端分页替代前端过滤"
```

---

### Task 14: Frontend — 迁移 QuickConnectView

**Files:**
- Modify: `web/src/views/QuickConnectView.vue`

**Interfaces:**
- Consumes: DataTableCard, 统一 API 类型
- Produces: 改造后的快速连接页面

- [ ] **Step 1: SSH 标签页 — 用 DataTableCard**

替换目标列表为 DataTableCard，加分页+搜索。

- [ ] **Step 2: DB 标签页 — 用 DataTableCard**

替换数据库账号列表为 DataTableCard，加分页+搜索。

- [ ] **Step 3: 验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 4: Commit**

```bash
git add web/src/views/QuickConnectView.vue
git commit -m "refactor: QuickConnectView 迁移到 DataTableCard"
```

---

### Task 15: Frontend — 迁移 UsersView

**Files:**
- Modify: `web/src/views/UsersView.vue`

**Interfaces:**
- Consumes: DataTableCard, FormDialog, StatusSwitch, 统一 API 类型
- Produces: 改造后的用户管理页面

- [ ] **Step 1: 替换用户列表为 DataTableCard（加分页+搜索）**

- [ ] **Step 2: 替换弹窗为 FormDialog**

- [ ] **Step 3: 验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 4: Commit**

```bash
git add web/src/views/UsersView.vue
git commit -m "refactor: UsersView 迁移到 DataTableCard/FormDialog"
```

---

### Task 16: Frontend — 迁移 RolesView

**Files:**
- Modify: `web/src/views/RolesView.vue`

**Interfaces:**
- Consumes: DataTableCard, FormDialog, StatusSwitch
- Produces: 改造后的角色管理页面

- [ ] **Step 1: 替换角色列表为 DataTableCard（加分页+搜索）**

- [ ] **Step 2: 替换弹窗为 FormDialog**

- [ ] **Step 3: 验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 4: Commit**

```bash
git add web/src/views/RolesView.vue
git commit -m "refactor: RolesView 迁移到 DataTableCard/FormDialog"
```

---

### Task 17: Frontend — 迁移 RBACView

**Files:**
- Modify: `web/src/views/RBACView.vue`

**Interfaces:**
- Consumes: DataTableCard, FormDialog, StatusSwitch
- Produces: 改造后的 RBAC 权限管理页面（5 个 tab）

- [ ] **Step 1: 5 个 tab 各自用 DataTableCard + 后端分页+搜索**

5 个 tab：角色、权限、用户角色绑定、角色权限绑定、有效权限检查。前 4 个 tab 的列表都用 DataTableCard。

- [ ] **Step 2: 替换弹窗为 FormDialog**

- [ ] **Step 3: 验证编译**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 4: Commit**

```bash
git add web/src/views/RBACView.vue
git commit -m "refactor: RBACView 迁移到 DataTableCard/FormDialog"
```

---

### Task 18: 最终验证

**Files:**
- All modified files

- [ ] **Step 1: 后端全量测试**

```bash
cd C:\02-codespace\Jianmen && go build ./... && go test ./... -count=1
```

- [ ] **Step 2: 前端全量验证**

```bash
cd C:\02-codespace\Jianmen\web && npm run typecheck && npm run build
```

- [ ] **Step 3: 启动应用验证功能**

确保前端、后端正常启动，各个页面功能正常。

- [ ] **Step 4: 合并到 dev**

按照 CLAUDE.md 中的合并准则：先 `git merge dev`（拉取最新），解决冲突，验证，再合并回 dev。

---

## 任务依赖图

```
Task 1 (删死表)
  └→ Task 2 (删死字段)
       └→ Task 3 (统一Host/HostAccount命名)
            └→ Task 4 (统一DB命名)
                 └→ Task 5 (删access/static_adapter)
                      └→ Task 6 (API分页+搜索)
                           ├→ Task 10 (前端API类型)
                           └→ (后端完成)

Task 7 (CSS清理) ─┐
Task 8 (DataTableCard) ─┤
Task 9 (FormDialog等) ─┤
                        ├→ Task 11 (HostsView)
                        ├→ Task 12 (DatabaseView)
                        ├→ Task 13 (AuditView)
                        ├→ Task 14 (QuickConnectView)
                        ├→ Task 15 (UsersView)
                        ├→ Task 16 (RolesView)
                        └→ Task 17 (RBACView)
                                                  └→ Task 18 (验证)
```

### 并行化建议

- Task 1-5 必须顺序执行（后端模型清理）
- Task 7-10 可以和后端任务并行（前端基础设施）
- Task 11-17 之间无依赖，可以并行（各页面独立迁移）
