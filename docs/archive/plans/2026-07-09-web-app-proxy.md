# 应用发布（Web 代理网关）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 堡垒机新增"应用发布"能力，通过端口模式 HTTP 反向代理让用户无需本地工具即可通过浏览器访问内网 Web 应用（如 Nacos）。

**Architecture:** 每个内网应用分配独立代理端口（47110-47199），独立端口消除 URL 路径前缀不匹配问题，零内容重写。堡垒机 admin server 端口（47100）的 session cookie 在同域名不同端口间共享，代理端口校验 cookie 实现认证和 RBAC。

**Tech Stack:** Go 标准库 `net/http/httputil.ReverseProxy`，Vue 3 + Element Plus，GORM

## Global Constraints

- 产品未发布，不考虑向后兼容性
- 零配置启动：端口范围不配置时使用默认值 47110-47199
- 代理端口通过数据库加载后动态监听，应用增删时动态启停
- 代理路径前后一致，不需要 URL 重写或 JS 垫片注入
- 必须支持 RBAC 控制访问（`app:connect` 动作，`application` 资源类型）
- 应用管理页面 CRUD，和现有主机/数据库管理一致
- 前端：`npm run typecheck` + `npm run build` 必须通过
- 后端：`go build ./...` + `go test ./... -count=1` 必须通过
- 遵循 CLAUDE.md 的分层规范和 Go 开发规范

---

### Task 1: Application 模型（GORM 实体）

**Files:**
- Modify: `internal/model/core.go`

**Interfaces:**
- Produces: `model.Application` struct、`ResourceTypeApplication` 常量、`BeforeCreate` 钩子、`AllModels()` 条目

- [ ] **Step 1: 添加 `ResourceTypeApplication` 常量和 `Application` 结构体**

在 `internal/model/core.go` 的 `const` 块中添加：

```go
const (
    // ... 已有常量 ...
    ResourceTypeApplication = "application"
)
```

在 `DatabaseAccount` 结构体后添加 `Application` 结构体：

```go
type Application struct {
    ID             string    `gorm:"primaryKey;size:64" json:"id"`
    Name           string    `gorm:"size:255;not null" json:"name"`
    AppGroup       string    `gorm:"size:128" json:"group"`
    ListenPort     int       `gorm:"uniqueIndex;not null" json:"listen_port"`
    InternalScheme string    `gorm:"size:8;not null;default:http" json:"internal_scheme"`
    InternalHost   string    `gorm:"size:255;not null" json:"internal_host"`
    InternalPort   int       `gorm:"not null;default:80" json:"internal_port"`
    Remark         string    `gorm:"type:text" json:"remark,omitempty"`
    Status         string    `gorm:"index;size:32;not null;default:active" json:"status"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

- [ ] **Step 2: 添加 `BeforeCreate` 钩子**

在文件底部的 `BeforeCreate` 注册区域添加：

```go
func (m *Application) BeforeCreate(_ *gorm.DB) error { return ensureID(&m.ID) }
```

- [ ] **Step 3: 将 `Application` 添加到 `AllModels()`**

在 `AllModels()` 返回的切片中添加：

```go
&Application{},
```

- [ ] **Step 4: 验证编译**

```powershell
go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/model/core.go
git commit -m "feat: add Application model entity"
```

---

### Task 2: Store 接口与哨兵错误

**Files:**
- Modify: `internal/store/store.go`

**Interfaces:**
- Consumes: `model.Application`（来自 Task 1）
- Produces: `ApplicationView` 类型、`ErrApplicationNotFound`、Store 接口方法签名

- [ ] **Step 1: 添加 `ApplicationView` 类型**

在 `store.go` 的 View 类型区域（`DatabaseAccountView` 之后）添加：

```go
type ApplicationView struct {
    ID             string `json:"id"`
    Name           string `json:"name"`
    AppGroup       string `json:"group"`
    ListenPort     int    `json:"listen_port"`
    InternalScheme string `json:"internal_scheme"`
    InternalHost   string `json:"internal_host"`
    InternalPort   int    `json:"internal_port"`
    Remark         string `json:"remark,omitempty"`
    Status         string `json:"status"`
    CreatedAt      string `json:"created_at"`
    UpdatedAt      string `json:"updated_at"`
}
```

- [ ] **Step 2: 添加 `ErrApplicationNotFound` 哨兵错误**

在哨兵错误区域添加：

```go
ErrApplicationNotFound = errSentinel("application not found")
```

- [ ] **Step 3: 在 `Store` 接口中添加应用方法签名**

在 `Store` 接口中添加：

```go
// Application CRUD
Applications() []ApplicationView
Application(id string) (ApplicationView, error)
AddApplication(name, scheme, host string, port, listenPort int, group, remark string) (ApplicationView, error)
UpdateApplication(id, name, scheme, host string, port, listenPort int, group, remark, status string) (ApplicationView, error)
DeleteApplication(id string) error
```

- [ ] **Step 4: 验证编译**

```powershell
go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/store/store.go
git commit -m "feat: add ApplicationView type and Store interface methods"
```

---

### Task 3: Store 实现（GORM CRUD）

**Files:**
- Create: `internal/store/dbstore_application.go`

**Interfaces:**
- Consumes: `ApplicationView`、Store 接口（来自 Task 2）；`model.Application`、`ResourceTypeApplication`（来自 Task 1）；`*DBStore`
- Produces: `Applications()`、`Application()`、`AddApplication()`、`UpdateApplication()`、`DeleteApplication()` 的 GORM 实现

- [ ] **Step 1: 创建 `dbstore_application.go`**

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

// -- application CRUD (DB-backed) --

func (s *DBStore) Applications() []ApplicationView {
    var apps []model.Application
    if err := s.db.Order("name ASC").Find(&apps).Error; err != nil {
        return nil
    }
    views := make([]ApplicationView, 0, len(apps))
    for _, app := range apps {
        views = append(views, applicationView(app))
    }
    return views
}

func (s *DBStore) Application(id string) (ApplicationView, error) {
    id = strings.TrimSpace(id)
    var app model.Application
    if err := s.db.First(&app, "id = ?", id).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return ApplicationView{}, fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
        }
        return ApplicationView{}, err
    }
    return applicationView(app), nil
}

func applicationView(app model.Application) ApplicationView {
    return ApplicationView{
        ID:             app.ID,
        Name:           app.Name,
        AppGroup:       app.AppGroup,
        ListenPort:     app.ListenPort,
        InternalScheme: app.InternalScheme,
        InternalHost:   app.InternalHost,
        InternalPort:   app.InternalPort,
        Remark:         app.Remark,
        Status:         app.Status,
        CreatedAt:      app.CreatedAt.Format(time.RFC3339),
        UpdatedAt:      app.UpdatedAt.Format(time.RFC3339),
    }
}

func (s *DBStore) AddApplication(name, scheme, host string, port, listenPort int, group, remark string) (ApplicationView, error) {
    scheme = strings.ToLower(strings.TrimSpace(scheme))
    if scheme != "http" && scheme != "https" {
        scheme = "http"
    }
    host = strings.TrimSpace(host)
    if host == "" {
        return ApplicationView{}, fmt.Errorf("internal host is required")
    }
    if listenPort <= 0 || listenPort > 65535 {
        return ApplicationView{}, fmt.Errorf("listen port must be 1-65535")
    }
    if port <= 0 {
        port = 80
        if scheme == "https" {
            port = 443
        }
    }
    app := model.Application{
        Name:           strings.TrimSpace(name),
        InternalScheme: scheme,
        InternalHost:   host,
        InternalPort:   port,
        ListenPort:     listenPort,
        AppGroup:       strings.TrimSpace(group),
        Remark:         strings.TrimSpace(remark),
    }
    if app.Name == "" {
        app.Name = fmt.Sprintf("%s://%s:%d", scheme, host, port)
    }
    if err := s.db.Transaction(func(tx *gorm.DB) error {
        if err := tx.Create(&app).Error; err != nil {
            return err
        }
        return s.syncResourceTx(tx, model.ResourceTypeApplication, app.ID, app.Name, "")
    }); err != nil {
        return ApplicationView{}, err
    }
    return applicationView(app), nil
}

func (s *DBStore) UpdateApplication(id, name, scheme, host string, port, listenPort int, group, remark, status string) (ApplicationView, error) {
    id = strings.TrimSpace(id)
    var app model.Application
    if err := s.db.First(&app, "id = ?", id).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return ApplicationView{}, fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
        }
        return ApplicationView{}, err
    }
    scheme = strings.ToLower(strings.TrimSpace(scheme))
    if scheme != "http" && scheme != "https" {
        scheme = app.InternalScheme
    }
    host = strings.TrimSpace(host)
    if host != "" {
        app.InternalHost = host
    }
    if port > 0 {
        app.InternalPort = port
    }
    if listenPort > 0 {
        app.ListenPort = listenPort
    }
    app.Name = strings.TrimSpace(name)
    app.InternalScheme = scheme
    app.AppGroup = strings.TrimSpace(group)
    app.Remark = strings.TrimSpace(remark)
    if status != "" {
        app.Status = status
    }
    if app.Name == "" {
        app.Name = fmt.Sprintf("%s://%s:%d", app.InternalScheme, app.InternalHost, app.InternalPort)
    }
    if err := s.db.Transaction(func(tx *gorm.DB) error {
        if err := tx.Save(&app).Error; err != nil {
            return err
        }
        return s.syncResourceTx(tx, model.ResourceTypeApplication, app.ID, app.Name, "")
    }); err != nil {
        return ApplicationView{}, err
    }
    return applicationView(app), nil
}

func (s *DBStore) DeleteApplication(id string) error {
    id = strings.TrimSpace(id)
    return s.db.Transaction(func(tx *gorm.DB) error {
        var app model.Application
        if err := tx.First(&app, "id = ?", id).Error; err != nil {
            if errors.Is(err, gorm.ErrRecordNotFound) {
                return fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
            }
            return err
        }
        if err := s.deleteResourceTx(tx, model.ResourceTypeApplication, app.ID); err != nil {
            return err
        }
        return tx.Delete(&app).Error
    })
}
```

- [ ] **Step 2: 验证编译**

```powershell
go build ./...
```

- [ ] **Step 3: 提交**

```bash
git add internal/store/dbstore_application.go
git commit -m "feat: add Application store CRUD implementation"
```

---

### Task 4: RBAC 动作常量

**Files:**
- Modify: `internal/rbac/resources.go`

**Interfaces:**
- Consumes: 无
- Produces: `ActionAppCreate`、`ActionAppUpdate`、`ActionAppDelete`、`ActionAppView`、`ActionAppConnect` 常量

- [ ] **Step 1: 添加应用动作常量**

在 `internal/rbac/resources.go` 的 `const` 块中添加：

```go
const (
    // ... 已有常量 ...
    ActionAppCreate  = "application:create"
    ActionAppUpdate  = "application:update"
    ActionAppDelete  = "application:delete"
    ActionAppView    = "application:view"
    ActionAppConnect = "app:connect"
)
```

- [ ] **Step 2: 验证编译**

```powershell
go build ./...
```

- [ ] **Step 3: 提交**

```bash
git add internal/rbac/resources.go
git commit -m "feat: add application RBAC action constants"
```

---

### Task 5: Config — 应用代理网关配置

**Files:**
- Modify: `internal/config/config.go`

**Interfaces:**
- Consumes: 无
- Produces: `ApplicationGatewayConfig` 结构体、默认值、验证逻辑

- [ ] **Step 1: 在 `Config` 结构体中添加 `ApplicationGateway` 字段**

```go
type Config struct {
    // ... 已有字段 ...
    ApplicationGateway ApplicationGatewayConfig `json:"application_gateway"`
}
```

- [ ] **Step 2: 添加 `ApplicationGatewayConfig` 结构体**

```go
type ApplicationGatewayConfig struct {
    Enabled    bool   `json:"enabled"`
    PortStart  int    `json:"port_start"`
    PortEnd    int    `json:"port_end"`
}
```

- [ ] **Step 3: 在 `applyDefaults()` 中添加默认值**

在 `applyDefaults()` 函数中，加到 `databaseGateway` 默认值附近：

```go
if !c.ApplicationGateway.Enabled && c.ApplicationGateway.PortStart == 0 && c.ApplicationGateway.PortEnd == 0 {
    c.ApplicationGateway.Enabled = true
    c.ApplicationGateway.PortStart = 47110
    c.ApplicationGateway.PortEnd = 47199
}
```

- [ ] **Step 4: 在 `Validate()` 中添加验证**

在 `Validate()` 函数中（`databaseGateway` 验证之后）添加：

```go
if c.ApplicationGateway.Enabled {
    if c.ApplicationGateway.PortStart <= 0 || c.ApplicationGateway.PortStart > 65535 {
        return fmt.Errorf("invalid application_gateway.port_start %d", c.ApplicationGateway.PortStart)
    }
    if c.ApplicationGateway.PortEnd <= 0 || c.ApplicationGateway.PortEnd > 65535 {
        return fmt.Errorf("invalid application_gateway.port_end %d", c.ApplicationGateway.PortEnd)
    }
    if c.ApplicationGateway.PortStart > c.ApplicationGateway.PortEnd {
        return fmt.Errorf("application_gateway.port_start (%d) > port_end (%d)", c.ApplicationGateway.PortStart, c.ApplicationGateway.PortEnd)
    }
}
```

- [ ] **Step 5: 验证编译**

```powershell
go build ./...
```

- [ ] **Step 6: 提交**

```bash
git add internal/config/config.go
git commit -m "feat: add ApplicationGatewayConfig to config"
```

---

### Task 6: Admin Handler — 应用 CRUD API

**Files:**
- Create: `internal/server/admin/app_handlers.go`
- Modify: `internal/server/admin/request_utils.go`（添加路径解析）
- Modify: `internal/server/admin/server.go`（添加路由注册）

**Interfaces:**
- Consumes: 所有前面的接口（Store、RBAC、ApplicationView）
- Produces: `handleApplications`、`handleApplication` HTTP handler 方法

- [ ] **Step 1: 添加路径解析函数**

在 `internal/server/admin/request_utils.go` 中添加：

```go
func appPathParts(path string) (id, child string, ok bool) {
    trimmed := strings.Trim(strings.TrimPrefix(path, "/api/applications/"), "/")
    if trimmed == "" {
        return "", "", false
    }
    parts := strings.Split(trimmed, "/")
    if len(parts) == 1 {
        return parts[0], "", true
    }
    return "", "", false
}

func writeApplicationStoreError(w http.ResponseWriter, err error) {
    switch {
    case errors.Is(err, store.ErrApplicationNotFound):
        writeError(w, http.StatusNotFound, err)
    default:
        writeError(w, http.StatusBadRequest, err)
    }
}
```

- [ ] **Step 2: 创建 `app_handlers.go`**

```go
package admin

import (
    "encoding/json"
    "jianmen/internal/rbac"
    "jianmen/internal/store"
    "net/http"
    "strings"
)

func (s *Server) handleApplications(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        if !s.requirePermission(r, rbac.ActionAppView) {
            s.forbidden(w)
            return
        }
        apps := s.store.Applications()
        resp := paginateSlice(apps, r, func(v store.ApplicationView, q string) bool {
            return strings.Contains(strings.ToLower(v.Name), q) ||
                strings.Contains(strings.ToLower(v.InternalHost), q) ||
                strings.Contains(strings.ToLower(v.AppGroup), q) ||
                strings.Contains(strings.ToLower(v.Remark), q)
        })
        writeJSON(w, http.StatusOK, resp)
    case http.MethodPost:
        if !s.requirePermission(r, rbac.ActionAppCreate) {
            s.forbidden(w)
            return
        }
        s.handleCreateApplication(w, r)
    default:
        w.Header().Set("Allow", "GET, POST")
        writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
    }
}

func (s *Server) handleCreateApplication(w http.ResponseWriter, r *http.Request) {
    defer r.Body.Close()
    r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
    var payload struct {
        Name       string `json:"name"`
        Scheme     string `json:"scheme"`
        Host       string `json:"host"`
        Port       int    `json:"port"`
        ListenPort int    `json:"listen_port"`
        Group      string `json:"group"`
        Remark     string `json:"remark"`
    }
    if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
        writeError(w, http.StatusBadRequest, err)
        return
    }
    view, err := s.store.AddApplication(payload.Name, payload.Scheme, payload.Host, payload.Port, payload.ListenPort, payload.Group, payload.Remark)
    if err != nil {
        writeError(w, http.StatusBadRequest, err)
        return
    }
    // 通知代理服务器启动监听（Task 8 中完成集成后生效）
    if s.appProxy != nil {
        app := model.Application{ID: view.ID, Name: view.Name, ListenPort: view.ListenPort, InternalScheme: view.InternalScheme, InternalHost: view.InternalHost, InternalPort: view.InternalPort, Status: view.Status}
        if err := s.appProxy.AddProxy(app); err != nil {
            s.logger.Warn("failed to start app proxy", "name", app.Name, "error", err)
        }
    }
    writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleApplication(w http.ResponseWriter, r *http.Request) {
    id, child, ok := appPathParts(r.URL.Path)
    if !ok {
        writeErrorText(w, http.StatusNotFound, "not found")
        return
    }
    if child != "" {
        writeErrorText(w, http.StatusNotFound, "not found")
        return
    }

    switch r.Method {
    case http.MethodGet:
        if !s.requirePermission(r, rbac.ActionAppView) {
            s.forbidden(w)
            return
        }
        view, err := s.store.Application(id)
        if err != nil {
            writeApplicationStoreError(w, err)
            return
        }
        writeJSON(w, http.StatusOK, view)
    case http.MethodPut:
        s.handleUpdateApplication(w, r, id)
    case http.MethodDelete:
        if err := s.store.DeleteApplication(id); err != nil {
            writeApplicationStoreError(w, err)
            return
        }
        // TODO: Task 9 — notify app proxy server to stop listening
        w.WriteHeader(http.StatusNoContent)
    default:
        w.Header().Set("Allow", "GET, PUT, DELETE")
        writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
    }
}

func (s *Server) handleUpdateApplication(w http.ResponseWriter, r *http.Request, id string) {
    defer r.Body.Close()
    r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
    var payload struct {
        Name       string `json:"name"`
        Scheme     string `json:"scheme"`
        Host       string `json:"host"`
        Port       int    `json:"port"`
        ListenPort int    `json:"listen_port"`
        Group      string `json:"group"`
        Remark     string `json:"remark"`
        Status     string `json:"status"`
    }
    if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
        writeError(w, http.StatusBadRequest, err)
        return
    }
    view, err := s.store.UpdateApplication(id, payload.Name, payload.Scheme, payload.Host, payload.Port, payload.ListenPort, payload.Group, payload.Remark, payload.Status)
    if err != nil {
        writeApplicationStoreError(w, err)
        return
    }
    // TODO: Task 9 — notify app proxy server to update listener
    writeJSON(w, http.StatusOK, view)
}
```

- [ ] **Step 3: 注册路由**

在 `internal/server/admin/server.go` 的 `ListenAndServe` 方法中，在 `mux.HandleFunc("/api/me/menus"...` 之前添加：

```go
mux.HandleFunc("/api/applications", s.withAuthAndUser(s.handleApplications))
mux.HandleFunc("/api/applications/", s.withAuthAndUser(s.handleApplication))
```

- [ ] **Step 4: 添加菜单项**

在 `server.go` 的 `menuOrder` 中添加：

```go
{"applications", "application:view"},
```

- [ ] **Step 5: 验证编译**

```powershell
go build ./...
```

- [ ] **Step 6: 提交**

```bash
git add internal/server/admin/app_handlers.go internal/server/admin/request_utils.go internal/server/admin/server.go
git commit -m "feat: add application CRUD API handlers and routes"
```

---

### Task 7: 应用代理服务器（端口模式 HTTP 反向代理）

**Files:**
- Create: `internal/server/appproxy/server.go`

**Interfaces:**
- Consumes: `*gorm.DB`（查询应用和认证）、`rbac.Checker`、`config.ApplicationGatewayConfig`、`*slog.Logger`
- Produces: `Server` 结构体、`New()`、`ListenAndServe()`、动态 `AddProxy()`/`RemoveProxy()`/`UpdateProxy()`

- [ ] **Step 1: 创建 `internal/server/appproxy/server.go`**

```go
package appproxy

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "log/slog"
    "net/http"
    "net/http/httputil"
    "net/url"
    "strings"
    "sync"

    "gorm.io/gorm"

    "jianmen/internal/config"
    "jianmen/internal/model"
    rbaccheck "jianmen/internal/rbac"
)

type Server struct {
    cfg           config.ApplicationGatewayConfig
    db            *gorm.DB
    checker       *rbaccheck.Checker
    superAdminIDs map[string]bool
    logger        *slog.Logger

    mu       sync.Mutex
    proxies  map[int]*proxyEntry
}

type proxyEntry struct {
    app     model.Application
    server  *http.Server
    proxy   *httputil.ReverseProxy
}

func New(cfg config.ApplicationGatewayConfig, db *gorm.DB, superAdminIDs map[string]bool, logger *slog.Logger) *Server {
    if logger == nil {
        logger = slog.Default()
    }
    return &Server{
        cfg:           cfg,
        db:            db,
        checker:       rbaccheck.NewChecker(db),
        superAdminIDs: superAdminIDs,
        logger:        logger,
        proxies:       make(map[int]*proxyEntry),
    }
}

func (s *Server) ListenAndServe(ctx context.Context) error {
    if !s.cfg.Enabled {
        s.logger.Info("application gateway disabled")
        return nil
    }

    var apps []model.Application
    if err := s.db.Where("status = ?", "active").Find(&apps).Error; err != nil {
        return fmt.Errorf("load applications: %w", err)
    }

    for _, app := range apps {
        if err := s.startProxy(app); err != nil {
            s.logger.Error("failed to start app proxy", "name", app.Name, "port", app.ListenPort, "error", err)
        }
    }

    s.logger.Info("application gateway started", "port_range", fmt.Sprintf("%d-%d", s.cfg.PortStart, s.cfg.PortEnd))
    <-ctx.Done()
    s.shutdown()
    return nil
}

func (s *Server) AddProxy(app model.Application) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if _, exists := s.proxies[app.ListenPort]; exists {
        return fmt.Errorf("port %d already in use", app.ListenPort)
    }
    return s.startProxy(app)
}

func (s *Server) RemoveProxy(listenPort int) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if entry, ok := s.proxies[listenPort]; ok {
        _ = entry.server.Close()
        delete(s.proxies, listenPort)
        s.logger.Info("stopped app proxy", "port", listenPort)
    }
}

func (s *Server) UpdateProxy(app model.Application) error {
    s.RemoveProxy(app.ListenPort)
    return s.AddProxy(app)
}

func (s *Server) startProxy(app model.Application) error {
    target := fmt.Sprintf("%s://%s:%d", app.InternalScheme, app.InternalHost, app.InternalPort)
    targetURL, err := url.Parse(target)
    if err != nil {
        return fmt.Errorf("parse target %q: %w", target, err)
    }

    rp := httputil.NewSingleHostReverseProxy(targetURL)
    handler := s.authMiddleware(s.rbacMiddleware(app, rp))

    addr := fmt.Sprintf(":%d", app.ListenPort)
    srv := &http.Server{
        Addr:              addr,
        Handler:           handler,
        ReadHeaderTimeout: 10 * 1e9,
    }

    go func() {
        s.logger.Info("starting app proxy", "name", app.Name, "port", app.ListenPort, "target", target)
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            s.logger.Error("app proxy stopped", "name", app.Name, "port", app.ListenPort, "error", err)
        }
    }()

    s.proxies[app.ListenPort] = &proxyEntry{app: app, server: srv, proxy: rp}
    return nil
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Extract token from Cookie
        cookie, err := r.Cookie("jianmen_token")
        if err != nil {
            // Fallback to Authorization header
            auth := r.Header.Get("Authorization")
            token := strings.TrimPrefix(auth, "Bearer ")
            if token != "" && token != auth {
                if !s.validateToken(token) {
                    http.Error(w, "unauthorized", http.StatusUnauthorized)
                    return
                }
            } else {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
            }
        } else if !s.validateToken(cookie.Value) {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}

func (s *Server) rbacMiddleware(app model.Application, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Get user ID from cookie token
        userID := s.getUserID(r)
        if userID == "" {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        // Super admin bypasses RBAC
        if s.superAdminIDs[userID] {
            next.ServeHTTP(w, r)
            return
        }
        // Check app:connect permission
        allowed, err := s.checker.HasPermission(userID, rbaccheck.ActionAppConnect, model.ResourceTypeApplication, app.ID)
        if err != nil || !allowed {
            http.Error(w, "forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
}

func (s *Server) validateToken(token string) bool {
    var user model.User
    hash := sha256.Sum256([]byte(token))
    tokenHash := hex.EncodeToString(hash[:])
    err := s.db.Where("token_hash = ? AND status = ?", tokenHash, "active").First(&user).Error
    return err == nil
}

func (s *Server) getUserID(r *http.Request) string {
    cookie, err := r.Cookie("jianmen_token")
    if err != nil {
        auth := r.Header.Get("Authorization")
        token := strings.TrimPrefix(auth, "Bearer ")
        if token == "" || token == auth {
            return ""
        }
        return s.userIDForToken(token)
    }
    return s.userIDForToken(cookie.Value)
}

func (s *Server) userIDForToken(token string) string {
    var user model.User
    hash := sha256.Sum256([]byte(token))
    tokenHash := hex.EncodeToString(hash[:])
    if err := s.db.Where("token_hash = ? AND status = ?", tokenHash, "active").First(&user).Error; err != nil {
        return ""
    }
    return user.ID
}

func (s *Server) shutdown() {
    s.mu.Lock()
    defer s.mu.Unlock()
    for port, entry := range s.proxies {
        _ = entry.server.Close()
        delete(s.proxies, port)
    }
}
```

**注意**：`ReadHeaderTimeout` 用 `10 * time.Second`，但因为文件头没有 import `time`，在文件顶部添加：

```go
import (
    // ... 其他 ...
    "time"
)
```

把 `10 * 1e9` 改为 `10 * time.Second`。

- [ ] **Step 2: 验证编译**

```powershell
go build ./...
```

- [ ] **Step 3: 提交**

```bash
git add internal/server/appproxy/server.go
git commit -m "feat: add application proxy server with cookie auth and RBAC"
```

---

### Task 8: 代理服务器与 Admin server 集成 + main.go 组装

**Files:**
- Modify: `internal/server/admin/server.go`（添加 appProxy 字段）
- Modify: `cmd/bastion-core/main.go`（创建和启动 appProxy）

**Interfaces:**
- Consumes: `appproxy.Server`（来自 Task 7）、Config（来自 Task 5）
- Produces: 完整的应用程序启动管道

- [ ] **Step 1: 在 admin Server 中持有 appProxy 引用**

在 `internal/server/admin/server.go` 中，给 `Server` 结构体添加字段：

```go
type Server struct {
    // ... 已有字段 ...
    appProxy *appproxy.Server
}
```

在文件头部添加 import：

```go
import (
    // ... 已有 imports ...
    "jianmen/internal/server/appproxy"
)
```

修改 `New()` 函数，添加 `appProxy` 参数：

```go
func New(cfg *config.Config, store store.Store, logger *slog.Logger, dataDir string, appProxy *appproxy.Server, dbs ...*gorm.DB) *Server {
    // ... 已有逻辑 ...
    return &Server{..., appProxy: appProxy, ...}
}
```

- [ ] **Step 2: 在 handler 中调用代理服务器动态管理**

在 `app_handlers.go` 中，将创建/更新/删除 handler 中的 TODO 替换为实际调用。例如在 `handleCreateApplication` 的 `writeJSON` 之前添加：

```go
if s.appProxy != nil && view.Status == "active" {
    app := model.Application{
        ID:             view.ID,
        Name:           view.Name,
        ListenPort:     view.ListenPort,
        InternalScheme: view.InternalScheme,
        InternalHost:   view.InternalHost,
        InternalPort:   view.InternalPort,
        Status:         view.Status,
    }
    if err := s.appProxy.AddProxy(app); err != nil {
        s.logger.Warn("failed to start app proxy", "name", app.Name, "error", err)
    }
}
```

同样在更新和删除 handler 中添加相应的 `UpdateProxy` 和 `RemoveProxy` 调用。

- [ ] **Step 3: 在 main.go 中创建并启动 appProxy**

在 `cmd/bastion-core/main.go` 中添加 import：

```go
import (
    // ... 已有 ...
    "jianmen/internal/server/appproxy"
)
```

在 `main()` 中，在 `dbGateway` 行之前添加：

```go
appProxy := appproxy.New(cfg.ApplicationGateway, metadataDB, admin.LoadSuperAdminIDs(cfg, dataDir), logger)
go func() {
    errCh <- appProxy.ListenAndServe(ctx)
}()
```

修改 admin server 构造以传入 appProxy：

```go
adminSrv := admin.New(cfg, appStore, logger, dataDir, appProxy, metadataDB)
```

- [ ] **Step 4: 验证编译**

```powershell
go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/server/admin/server.go internal/server/admin/app_handlers.go cmd/bastion-core/main.go
git commit -m "feat: integrate app proxy server with admin server and main entry"
```

---

### Task 9: 前端 — TypeScript 类型与 API 客户端

**Files:**
- Modify: `web/src/api/client.ts`

**Interfaces:**
- Consumes: API 客户端模式（已有）
- Produces: `ApplicationView`、`ApplicationPayload` 接口 + CRUD API 方法

- [ ] **Step 1: 添加 TypeScript 接口**

在 `client.ts` 的 Database 区域（`DBConnectionMetaRecord` 之前）添加：

```typescript
// ── Application (Web App Proxy) ─────────────────────────────────────────

export interface ApplicationView {
  id?: string;
  name: string;
  group?: string;
  listen_port: number;
  internal_scheme: string;
  internal_host: string;
  internal_port: number;
  remark?: string;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface ApplicationPayload {
  name: string;
  scheme?: string;
  host: string;
  port?: number;
  listen_port?: number;
  group?: string;
  remark?: string;
}
```

- [ ] **Step 2: 添加 API 客户端方法**

在 `apiClient` 对象中，在 `// rbac` 注释之前添加：

```typescript
// applications (web app proxy)
getApplications: (params?: { page?: number; page_size?: number; q?: string }) =>
  request<PageResponse<ApplicationView>>(`/api/applications${buildQS(params as Record<string, string | number | undefined>)}`),
createApplication: (payload: ApplicationPayload) =>
  request<ApiEnvelope<ApplicationView> | ApplicationView>('/api/applications', {
    method: 'POST',
    body: JSON.stringify(payload)
  }),
updateApplication: (id: string, payload: ApplicationPayload & { status?: string }) =>
  request<ApiEnvelope<ApplicationView> | ApplicationView>(`/api/applications/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(payload)
  }),
deleteApplication: (id: string) =>
  request<ApiEnvelope<unknown> | unknown>(`/api/applications/${encodeURIComponent(id)}`, {
    method: 'DELETE'
  }),
```

- [ ] **Step 3: 验证编译**

```powershell
cd web; npm run typecheck
```

- [ ] **Step 4: 提交**

```bash
git add web/src/api/client.ts
git commit -m "feat: add Application type definitions and API client methods"
```

---

### Task 10: 前端 — 国际化（i18n）

**Files:**
- Modify: `web/src/i18n/index.ts`

**Interfaces:**
- Consumes: i18n 框架（已有）
- Produces: 应用管理的 `zh_CN` 和 `en_US` 翻译键

- [ ] **Step 1: 在 `zhCN` 和 `enUS` 对象中添加翻译键**

在 `zhCN` 对象中，在 `nav.databases` 区域附近添加：

```typescript
'nav.applications': '应用发布',
'route.applications.description': '管理内网 Web 应用代理发布与访问控制',
'route.applications.title': '应用发布',
'application.title': '应用发布',
'application.create': '新增应用',
'application.edit': '编辑应用',
'application.delete': '删除应用',
'application.deleteConfirm': '确定删除应用 "{name}"？',
'application.empty': '暂无应用数据',
'application.error.load': '加载应用列表失败',
'application.error.save': '保存应用失败',
'application.error.delete': '删除应用失败',
'application.field.name': '应用名称',
'application.field.scheme': '协议',
'application.field.host': '内网地址',
'application.field.port': '内网端口',
'application.field.listenPort': '代理端口',
'application.field.group': '分组',
'application.field.remark': '备注',
'application.action.visit': '访问',
'application.placeholder.name': '如 生产环境 Nacos',
'application.placeholder.host': '如 10.0.0.5',
'application.placeholder.search': '搜索应用名称、地址...',
'application.column.name': '应用名称',
'application.column.group': '分组',
'application.column.listenPort': '代理端口',
'application.column.target': '内网地址',
'application.column.status': '状态',
'application.column.actions': '操作',
```

在 `enUS` 对象中添加：

```typescript
'nav.applications': 'Application Publishing',
'route.applications.description': 'Manage internal web app proxy publishing and access control',
'route.applications.title': 'Application Publishing',
'application.title': 'Application Publishing',
'application.create': 'New Application',
'application.edit': 'Edit Application',
'application.delete': 'Delete Application',
'application.deleteConfirm': 'Delete application "{name}"?',
'application.empty': 'No application data',
'application.error.load': 'Failed to load applications',
'application.error.save': 'Failed to save application',
'application.error.delete': 'Failed to delete application',
'application.field.name': 'Application Name',
'application.field.scheme': 'Scheme',
'application.field.host': 'Internal Host',
'application.field.port': 'Internal Port',
'application.field.listenPort': 'Proxy Port',
'application.field.group': 'Group',
'application.field.remark': 'Remark',
'application.action.visit': 'Visit',
'application.placeholder.name': 'e.g. Production Nacos',
'application.placeholder.host': 'e.g. 10.0.0.5',
'application.placeholder.search': 'Search application name, address...',
'application.column.name': 'Application Name',
'application.column.group': 'Group',
'application.column.listenPort': 'Proxy Port',
'application.column.target': 'Internal Address',
'application.column.status': 'Status',
'application.column.actions': 'Actions',
```

**注意**：添加新键后检查 `enUS` 对象中的新键是否在 `zhCN` 中都有对应项（`satisfies Record<TranslationKey, string>` 会验证）。

- [ ] **Step 2: 验证编译**

```powershell
cd web; npm run typecheck
```

- [ ] **Step 3: 提交**

```bash
git add web/src/i18n/index.ts
git commit -m "feat: add application publishing i18n keys"
```

---

### Task 11: 前端 — 路由注册

**Files:**
- Modify: `web/src/router/index.ts`

**Interfaces:**
- Consumes: Vue Router（已有）
- Produces: `/applications` 路由 + `routeMenuMap` 映射 + 导入

- [ ] **Step 1: 添加路由**

在 `web/src/router/index.ts` 中：

导入语句区域添加：

```typescript
const ApplicationsView = () => import('@/views/ApplicationsView.vue');
```

在 `routeMenuMap` 中添加：

```typescript
'/applications': 'applications',
```

在 `routes` 数组中，在 `databases` 路由后添加：

```typescript
{
  path: '/applications',
  name: 'applications',
  component: ApplicationsView,
  meta: {
    titleKey: 'route.applications.title',
    descriptionKey: 'route.applications.description'
  } satisfies AppRouteMeta
},
```

- [ ] **Step 2: 验证编译**

```powershell
cd web; npm run typecheck
```

- [ ] **Step 3: 提交**

```bash
git add web/src/router/index.ts
git commit -m "feat: add /applications route"
```

---

### Task 12: 前端 — 应用管理页面视图

**Files:**
- Create: `web/src/views/ApplicationsView.vue`

**Interfaces:**
- Consumes: `apiClient`、`useI18n`、`ApplicationView`/`ApplicationPayload`、`DataTableCard`/`FormDialog`（来自 Element Plus）
- Produces: 完整的应用管理 SFC 页面（列表 + CRUD 弹窗）

- [ ] **Step 1: 创建 `ApplicationsView.vue`**

参考 `DatabaseView.vue` 的结构（列表 + 弹窗），创建一个应用管理页面。核心功能：

- 列表展示：应用名称、分组、代理端口、内网地址（`scheme://host:port`）、状态
- 操作按钮：访问（`window.open` 新标签打开代理端口地址）、编辑、删除
- 新增/编辑弹窗：应用名称（必填）、协议（http/https 下拉）、内网地址（必填）、内网端口、代理端口（下拉可选，默认自动分配）、分组、备注
- 搜索：名称、地址、分组、备注
- 分页：默认 20 条

结构如下：

```vue
<template>
  <div class="applications-page">
    <DataTableCard
      :data="apps"
      :loading="loading"
      :total="total"
      v-model:page="page"
      v-model:page-size="pageSize"
      search-placeholder="搜索应用名称、地址..."
      @search="onSearch"
    >
      <template #toolbar-actions>
        <el-button type="primary" @click="openCreate">{{ t('application.create') }}</el-button>
      </template>

      <el-table-column prop="name" :label="t('application.column.name')" show-overflow-tooltip />
      <el-table-column prop="group" :label="t('application.column.group')" show-overflow-tooltip />
      <el-table-column :label="t('application.column.listenPort')" width="100">
        <template #default="{ row }">
          <el-tag size="small">{{ row.listen_port }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('application.column.target')" show-overflow-tooltip>
        <template #default="{ row }">{{ row.internal_scheme }}://{{ row.internal_host }}:{{ row.internal_port }}</template>
      </el-table-column>
      <el-table-column :label="t('application.column.status')" width="80">
        <template #default="{ row }">
          <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
            {{ row.status === 'active' ? t('common.enabled') : t('common.disabled') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('application.column.actions')" width="160" align="right">
        <template #default="{ row }">
          <el-button link size="small" @click="visitApp(row)">{{ t('application.action.visit') }}</el-button>
          <el-button link size="small" @click="openEdit(row)">{{ t('common.edit') }}</el-button>
          <el-button link size="small" type="danger" @click="deleteApp(row)">{{ t('common.delete') }}</el-button>
        </template>
      </el-table-column>
    </DataTableCard>

    <FormDialog
      v-model:visible="dialogVisible"
      :title="editingId ? t('application.edit') : t('application.create')"
      width="560px"
      :loading="submitting"
      @submit="submitApp"
    >
      <el-form-item :label="t('application.field.name')" :rules="[{ required: true }]">
        <el-input v-model="form.name" :placeholder="t('application.placeholder.name')" />
      </el-form-item>
      <el-form-item :label="t('application.field.host')" :rules="[{ required: true }]">
        <el-input v-model="form.host" :placeholder="t('application.placeholder.host')" />
      </el-form-item>
      <el-row :gutter="12">
        <el-col :span="12">
          <el-form-item :label="t('application.field.scheme')">
            <el-select v-model="form.scheme">
              <el-option label="http" value="http" />
              <el-option label="https" value="https" />
            </el-select>
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item :label="t('application.field.port')">
            <el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" />
          </el-form-item>
        </el-col>
      </el-row>
      <el-form-item :label="t('application.field.listenPort')">
        <el-select v-model="form.listen_port" placeholder="自动分配" clearable>
          <el-option
            v-for="p in availablePorts"
            :key="p"
            :label="String(p)"
            :value="p"
          />
        </el-select>
      </el-form-item>
      <el-collapse>
        <el-collapse-item title="更多设置">
          <el-form-item :label="t('application.field.group')">
            <el-input v-model="form.group" />
          </el-form-item>
          <el-form-item :label="t('application.field.remark')">
            <el-input v-model="form.remark" type="textarea" />
          </el-form-item>
        </el-collapse-item>
      </el-collapse>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { apiClient, type ApplicationView, type ApplicationPayload } from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();

const PORT_START = 47110;
const PORT_END = 47199;

const apps = ref<ApplicationView[]>([]);
const loading = ref(false);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const search = ref('');

const dialogVisible = ref(false);
const editingId = ref('');
const submitting = ref(false);
const form = reactive({ name: '', scheme: 'http', host: '', port: 80, listen_port: 0, group: '', remark: '' });

const usedPorts = computed(() => apps.value.map(a => a.listen_port));

const availablePorts = computed(() => {
  const ports: number[] = [];
  for (let p = PORT_START; p <= PORT_END; p++) {
    if (!usedPorts.value.includes(p)) ports.push(p);
    if (ports.length >= 100) break;
  }
  return ports;
});

async function fetchApps() {
  loading.value = true;
  try {
    const res = await apiClient.getApplications({ page: page.value, page_size: pageSize.value, q: search.value || undefined });
    apps.value = res.items;
    total.value = res.total;
  } catch (e: any) {
    ElMessage.error(e.message || t('application.error.load'));
  } finally {
    loading.value = false;
  }
}

function onSearch(val: string) {
  search.value = val;
  page.value = 1;
  fetchApps();
}

watch([page, pageSize], () => fetchApps());
onMounted(() => fetchApps());

function visitApp(app: ApplicationView) {
  window.open(`http://${window.location.hostname}:${app.listen_port}`, '_blank');
}

function openCreate() {
  editingId.value = '';
  form.name = '';
  form.scheme = 'http';
  form.host = '';
  form.port = 80;
  form.listen_port = 0;
  form.group = '';
  form.remark = '';
  dialogVisible.value = true;
}

function openEdit(app: ApplicationView) {
  editingId.value = app.id!;
  form.name = app.name;
  form.scheme = app.internal_scheme;
  form.host = app.internal_host;
  form.port = app.internal_port;
  form.listen_port = app.listen_port;
  form.group = app.group || '';
  form.remark = app.remark || '';
  dialogVisible.value = true;
}

async function submitApp() {
  submitting.value = true;
  try {
    const payload: ApplicationPayload = {
      name: form.name,
      scheme: form.scheme,
      host: form.host,
      port: form.port,
      listen_port: form.listen_port || 0,
      group: form.group,
      remark: form.remark,
    };
    if (editingId.value) {
      await apiClient.updateApplication(editingId.value, payload);
    } else {
      await apiClient.createApplication(payload);
    }
    dialogVisible.value = false;
    fetchApps();
  } catch (e: any) {
    ElMessage.error(e.message || t('application.error.save'));
  } finally {
    submitting.value = false;
  }
}

async function deleteApp(app: ApplicationView) {
  try {
    await ElMessageBox.confirm(t('application.deleteConfirm', { name: app.name }), t('application.delete'));
  } catch {
    return;
  }
  try {
    await apiClient.deleteApplication(app.id!);
    fetchApps();
  } catch (e: any) {
    ElMessage.error(e.message || t('application.error.delete'));
  }
}
</script>
```

**注意**：`ElMessageBox.confirm` 的第二个参数 `{ name: app.name }` 是插值参数，需要确认 Element Plus 的 API 是 `confirm(message, title, options)`。调整写法为：

```typescript
await ElMessageBox.confirm(
  t('application.deleteConfirm').replace('{name}', app.name),
  t('application.delete')
);
```

- [ ] **Step 2: 验证编译**

```powershell
cd web; npm run typecheck; npm run build
```

- [ ] **Step 3: 提交**

```bash
git add web/src/views/ApplicationsView.vue
git commit -m "feat: add ApplicationsView page with CRUD and visit action"
```

---

### Task 13: 端到端验证与集成测试

**Files:**
- None (验证步骤)

- [ ] **Step 1: 后端编译与测试**

```powershell
go build ./...
go test ./... -count=1
```

预期：编译成功，所有测试通过（允许预先存在的测试失败，但不允许新增失败）

- [ ] **Step 2: 前端类型检查与构建**

```powershell
cd web; npm run typecheck; npm run build
```

预期：类型检查通过，构建成功

- [ ] **Step 3: 功能验证（手动）**

1. 启动堡垒机服务
2. 登录管理界面，确认菜单栏显示"应用发布"
3. 创建应用：填写名称和内网地址、端口，测试端口分配
4. 编辑应用：修改名称、端口
5. 访问应用：点击"访问"按钮，确认新标签页打开代理端口
6. 删除应用：确认删除后列表更新
7. 验证未登录时访问代理端口返回 401
8. 验证无权限用户访问代理端口返回 403

- [ ] **Step 4: 提交最终状态**

```bash
git status
```

确认仅包含预期的文件变更，无意外修改。

---

## 实现顺序说明

任务依赖关系：
```
Task 1 (Model) ──> Task 2 (Store 接口) ──> Task 3 (Store 实现)
                                            ──> Task 6 (Handler)
Task 4 (RBAC) ──────────────────────────────> Task 6 (Handler)
Task 5 (Config) ────────────────────────────> Task 7 (Proxy Server) ──> Task 8 (集成)
                                                                        Task 9 (API Client)
                                                                        Task 10 (i18n)
                                                                        Task 11 (Router)
                                                                        Task 12 (View)
                                                                        Task 13 (验证)
```

可并行执行：Tasks 1-5 之间无依赖，可同步开发。Tasks 9-12 之间有顺序依赖且可和 Tasks 6-8 并行进行。
