# 应用发布（Web 代理网关）设计方案

日期：2026-07-09

## 概述

堡垒机新增"应用发布"能力：用户登录堡垒机 Web 界面后，可通过代理端口直接访问内网 Web 应用（如 Nacos、Grafana、Jenkins 等），全程在浏览器内完成，无需本地安装任何软件或配置 SSH 隧道。

## 核心决策：端口模式 + 零内容重写

每个内网应用分配独立代理端口（范围 47110-47199），用户通过 `http://bastion:<port>/` 访问。由于端口独立，应用内部路径（如 `/nacos/`）在浏览器地址栏天然一致，**不需要任何 URL 重写、HTML 内容修改或 JS 垫片注入**。这是 nginx 级别的简单方案。

## 架构

```
浏览器 ──HTTPS──▶ 堡垒机 :47100（管理界面，已有）
浏览器 ──HTTP──▶  堡垒机 :47110-47199（应用代理端口，新增）

应用代理端口请求处理链：
  Request → 认证中间件（session cookie）→ RBAC 检查 → httputil.ReverseProxy → 内网目标
  Response ← 透传（不修改内容）← 内网目标返回
```

## 资源模型

新增 `Application` 实体：

```go
// internal/model/application.go
type Application struct {
    ID             uint      `gorm:"primaryKey"`
    Name           string    // 显示名称，如 "生产环境 Nacos"
    AppGroup       string    // 分组
    ListenPort     int       // 代理监听端口，如 47110
    InternalScheme string    // http 或 https
    InternalHost   string    // 内网目标 IP，如 "10.0.0.5"
    InternalPort   int       // 内网目标端口，如 8848
    Remark         string    // 备注
    Status         string    // enabled / disabled
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

**端口池管理**：`ListenPort` 从配置的端口范围（默认 47110-47199）中分配，创建时自动选空闲端口，删除后释放。同一端口只能有一个启用的应用。

## 代理服务器

新增 `internal/server/appproxy/` 包：

- `server.go`：Server 结构体，管理端口 → ReverseProxy 映射
- 启动时加载所有 enabled 应用，每个端口启动一个 HTTP server
- 应用 CRUD 时通过 API 层调用 `AddProxy`/`RemoveProxy`/`UpdateProxy` 动态管理监听
- 认证：从 Cookie 提取 session token，复用现有 session 校验逻辑
- RBAC：检查用户对应用的 `app:connect` 权限
- 安全：转发目标由数据库配置决定，不受用户输入控制（防 SSRF）

## Cookie 处理

HTTP Cookie 在同域名不同端口间共享。堡垒机 session cookie 和应用的 cookie 互不冲突（名称不同）。`Set-Cookie` 透传不修改，应用自身的 `Path` 属性已提供足够隔离。

## 前端页面

### 应用管理页 `/applications`

- 表格列：应用名称、分组、代理端口、内网地址、状态、操作
- 操作按钮：访问（新标签页打开）、编辑、启用/禁用、删除
- 新增/编辑弹窗：应用名称、内网目标地址、端口（下拉选空闲）、分组、备注

### 端口选择

端口字段下拉列出可用范围，已占用端口灰色不可选，默认"自动分配"。

## RBAC 权限

| 动作 | 说明 |
|---|---|
| `application:read` | 查看应用列表 |
| `application:create` | 创建应用 |
| `application:update` | 编辑应用 |
| `application:delete` | 删除应用 |
| `app:connect` | 通过代理访问应用 |

资源类型：`application`。复用现有 RBAC 框架，初始化时 admin 角色拥有全部权限，普通用户需手动授权。

## 文件结构

```
internal/
  model/application.go          ← GORM 实体
  store/application_store.go    ← 数据访问接口 + 实现
  server/appproxy/
    server.go                   ← Server 结构体 + 动态端口管理
    handler.go                  ← 代理 handler（认证 + RBAC + 反向代理）
  handler/application/          ← CRUD API handler
    application.go
    routes.go
  server/admin/routes.go        ← 注册 /api/applications 路由

web/src/
  views/applications/
    ApplicationList.vue         ← 应用列表页
  api/application.ts            ← API 调用封装
  router/index.ts               ← 新增 /applications 路由
```

## 验证步骤

- `npm run typecheck`
- `npm run build`
- `go build ./...`
- `go test ./... -count=1`
