# 平台账号密码管理 设计文档

日期：2026-07-12
状态：设计完成，待评审

## 概述

新增可导入、可关联用户的平台账号密码管理功能，用于管理 Jenkins、GitLab、Jira 等内部 Web 服务的登录凭证。支持用户私有和多人共享两种模式。

## 一、数据模型

### 1.1 PlatformAccount 实体

```go
type PlatformAccount struct {
    ID           string         `gorm:"primaryKey;size:64" json:"id"`
    Name         string         `gorm:"size:255" json:"name"`                   // 账号名称，默认=用户名
    PlatformName string         `gorm:"index;size:128;not null" json:"platform_name"` // Jenkins / GitLab / Jira...
    URL          string         `gorm:"size:512" json:"url,omitempty"`          // 平台地址
    Category     string         `gorm:"index;size:64" json:"category,omitempty"`// CI/CD / 代码仓库 / 项目管理...
    GroupName    string         `gorm:"size:128" json:"group,omitempty"`        // 分组
    Username     string         `gorm:"size:255;not null" json:"username"`      // 登录用户名
    Password     EncryptedField `gorm:"type:text" json:"-"`                     // 密码（API 不返回）
    TOTPSecret   EncryptedField `gorm:"type:text" json:"-"`                     // TOTP 密钥（加密，可选）
    Remark       string         `gorm:"type:text" json:"remark,omitempty"`
    OwnerID      string         `gorm:"index;index:idx_platform_accounts_owner_visibility,priority:1;size:64;not null" json:"owner_id"`
    Visibility   string         `gorm:"index;index:idx_platform_accounts_owner_visibility,priority:2;size:32;not null;default:private" json:"visibility"` // private / shared
    Status       string         `gorm:"index;size:32;not null;default:active" json:"status"`
    ExpiresAt    *time.Time     `gorm:"index" json:"expires_at,omitempty"`
    CreatedAt    time.Time      `json:"created_at"`
    UpdatedAt    time.Time      `json:"updated_at"`
    Owner        User           `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}
```

### 1.2 PlatformAccountShare 实体

```go
type PlatformAccountShare struct {
    ID                string         `gorm:"primaryKey;size:64" json:"id"`
    PlatformAccountID string         `gorm:"index;index:idx_platform_shares_account,priority:1;size:64;not null" json:"platform_account_id"`
    UserID            string         `gorm:"index;index:idx_platform_shares_user_role,priority:1;size:64" json:"user_id,omitempty"`
    RoleID            string         `gorm:"index;index:idx_platform_shares_user_role,priority:2;size:64" json:"role_id,omitempty"`
    AccessLevel       string         `gorm:"size:32;not null;default:view" json:"access_level"` // view / use
    ExpiresAt         *time.Time     `gorm:"index" json:"expires_at,omitempty"`
    CreatedAt         time.Time      `json:"created_at"`
    PlatformAccount   PlatformAccount `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}
```

- `UserID` 和 `RoleID` 至少填一个
- `AccessLevel`: `view` 表示可见不可用，`use` 表示可查看/使用密码

### 1.3 AuditEvent 实体（通用操作审计）

```go
type AuditEvent struct {
    ID           string    `gorm:"primaryKey;size:64" json:"id"`
    ActorID      string    `gorm:"index;size:64;not null" json:"actor_id"`
    ActorUsername string   `gorm:"index;size:128" json:"actor_username"`
    Action       string    `gorm:"index;index:idx_audit_events_resource,priority:1;size:64;not null" json:"action"`
    ResourceType string    `gorm:"index;index:idx_audit_events_resource,priority:2;size:64;not null" json:"resource_type"`
    ResourceID   string    `gorm:"index;index:idx_audit_events_resource,priority:3;size:64" json:"resource_id,omitempty"`
    ResourceName string    `gorm:"size:255" json:"resource_name,omitempty"`
    Detail       string    `gorm:"type:text" json:"detail,omitempty"`       // JSON 详情
    ClientIP     string    `gorm:"size:64" json:"client_ip,omitempty"`
    CreatedAt    time.Time `gorm:"index" json:"created_at"`
}
```

### 1.4 新增常量

```go
// model/core.go
ResourceTypePlatformAccount = "platform_account"

// rbac/resources.go
ActionPlatformAccountCreate = "platform_account:create"
ActionPlatformAccountUpdate = "platform_account:update"
ActionPlatformAccountDelete = "platform_account:delete"
ActionPlatformAccountView   = "platform_account:view"
ActionPlatformAccountUse    = "platform_account:use"
```

### 1.5 与现有模型的关系

- `PlatformAccount.OwnerID` → `User.ID`（级联删除，所有者删除后账号随之删除）
- 纳入 `AllModels()` 注册表
- 创建/删除时通过 `syncResourceTx` / `deleteResourceTx` 同步到 `resources` 表

## 二、API 设计

### 2.1 端点

| 方法 | 路径 | 说明 | 权限 |
|------|------|------|------|
| GET | `/api/platform-accounts` | 分页列表 | `platform_account:view` |
| POST | `/api/platform-accounts` | 新增 | `platform_account:create` |
| GET | `/api/platform-accounts/{id}` | 详情（无密码） | 本人/已共享/管理员 |
| PUT | `/api/platform-accounts/{id}` | 编辑 | 本人/管理员 |
| DELETE | `/api/platform-accounts/{id}` | 删除 | 本人/管理员 |
| GET | `/api/platform-accounts/{id}/password` | 获取密码 | `platform_account:use` + 本人/已共享 |
| GET | `/api/platform-accounts/{id}/shares` | 列出共享 | 本人/管理员 |
| POST | `/api/platform-accounts/{id}/shares` | 添加共享 | 本人/管理员 |
| DELETE | `/api/platform-accounts/{id}/shares/{sid}` | 撤销共享 | 本人/管理员 |

### 2.2 列表查询参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `q` | string | 模糊搜索名称、平台、用户名 |
| `owner_id` | string | 按所有者过滤 |
| `visibility` | string | private / shared |
| `platform` | string | 按平台名称过滤 |
| `category` | string | 按分类过滤 |
| `page` | int | 页码，默认 1 |
| `page_size` | int | 每页条数，默认 20，最大 200 |

### 2.3 数据可见性规则

- 普通用户列表：`(owner_id = 自己)` OR `(存在 share 记录指向自己或自己所属角色)`
- 管理员：可看全域（与现有 `host:view` 模式一致）
- 密码接口额外校验 `platform_account:use` 权限 + 有效共享记录

### 2.4 编辑时密码处理

与 `HostAccount` 一致：提交时 `password` 为空字符串表示保留原密码；非空表示更新。

### 2.5 Store 接口扩展

```go
// Store 接口新增方法
PlatformAccounts(params PlatformAccountListParams) ([]PlatformAccountView, int64, error)
PlatformAccount(id string) (PlatformAccountView, error)
AddPlatformAccount(record PlatformAccountRecord) (PlatformAccountView, error)
UpdatePlatformAccount(id string, record PlatformAccountRecord) (PlatformAccountView, error)
DeletePlatformAccount(id string) error
GetPlatformAccountPassword(id string) (string, error)

PlatformAccountShares(accountID string) ([]PlatformAccountShareView, error)
AddPlatformAccountShare(accountID, userID, roleID, accessLevel string, expiresAt *time.Time) (PlatformAccountShareView, error)
DeletePlatformAccountShare(accountID, shareID string) error
GetPlatformAccountSharesForUser(userID string, roleIDs []string) ([]PlatformAccountShareView, error)
```

## 三、前端设计

### 3.1 路由与菜单

- 路由：`/platform-accounts`
- 菜单显示名："平台账号"
- 菜单键：`platformAccounts`
- 权限控制：需要 `platform_account:view`

### 3.2 列表页布局

参照 `HostsView.vue` 模式：顶部筛选栏 + 表格 + CRUD 弹窗。

**筛选栏：**
- 搜索框（名称/平台/用户名模糊搜索）
- 可见范围下拉：全部 / 我的私有 / 共享给我的
- 平台下拉选择器
- 分类下拉选择器

**表格列（按顺序）：**

| 列 | 宽度 | 说明 |
|---|---|---|
| 平台 | 100px | `&lt;el-tag&gt;` 显示平台名 |
| 账号名称 | 140px | 可点击查看详情 |
| 用户名 | 120px | 登录名 |
| 分组 | 100px | 分组标签 |
| 分类 | 100px | 分类标签 |
| 可见范围 | 80px | 🔒私有 / 👥共享 图标 |
| 状态 | 80px | 启用/禁用开关 |
| 有效期 | 120px | 过期高亮红色 |
| 操作 | 靠右 | 查看密码 · 编辑 · 共享 · 删除 |

### 3.3 新增/编辑弹窗

- 宽度：520px（与 HostAccount 弹窗一致）
- 表单布局与 HostAccount 一致

**主要区域：**
- 平台名称（`el-select` + 允许输入新值，参照分组下拉）
- 登录用户名
- 密码（`el-input` type=password，带显示/隐藏切换）
- TOTP 密钥（可选，checkbox 展开）

**"更多设置"折叠（`el-collapse`）：**
- 账号名称（默认 = 用户名）
- URL（平台地址，带链接图标）
- 分类
- 分组
- 备注
- 可见范围：私有 / 共享（radio）
- 有效期（快捷按钮：8小时 / 7天 / 1年 / 永久 + `el-date-picker`）

### 3.4 共享管理弹窗

- 宽度：480px
- 已共享列表表格：用户/角色 + 权限级别 + 有效期 + 撤销按钮
- 新增共享表单：选择用户或角色（`el-select`） + 仅查看/可使用（radio） + 有效期

### 3.5 查看密码交互

- 弹出确认对话框（"查看密码操作将被审计记录"）
- 确认后显示密码明文 + 一键复制按钮（`el-input` readonly + `el-button` 复制）
- 30秒倒计时后自动隐藏密码

## 四、安全与审计

### 4.1 密码安全

| 措施 | 实现 |
|------|------|
| 存储加密 | `EncryptedField`（AES-256-GCM），与 HostAccount 一致 |
| API 不返回密码 | 列表/详情接口 `json:"-"` 排除 |
| 密码查看鉴权 | 需 `use` 权限或有效共享记录 |
| 传输层保护 | HTTPS（部署层确保） |
| TOTP 加密 | 同样使用 EncryptedField |

### 4.2 审计记录

每次操作通过 `AuditEvent` 表记录：

| 操作 | action 值 |
|------|-----------|
| 创建平台账号 | `platform_account.create` |
| 编辑平台账号 | `platform_account.update` |
| 删除平台账号 | `platform_account.delete` |
| 查看密码 | `platform_account.view_password` |
| 添加共享 | `platform_account.share` |
| 撤销共享 | `platform_account.unshare` |

### 4.3 导入安全（后续迭代）

- 批量导入时 HTTPS 保护传输
- 逐条加密落库
- 导入日志记录条数，不记录密码

## 五、实现阶段

### 阶段一：后端模型与 Store

1. 新增 `model/platform_account.go` — 三个实体定义
2. 注册到 `AllModels()` + 新增资源类型常量 + RBAC 操作常量
3. 扩展 `Store` 接口 — 新增 View 类型和方法
4. 新建 `store/dbstore_platform.go` — DBStore 实现
5. 添加数据库迁移 — `storage/migrations.go`
6. 编写 Store 层测试

### 阶段二：后端 Handler 与路由

1. 新增 `server/admin/platform_handlers.go`
2. 注册路由到 `server.go`
3. 编写 handler 测试

### 阶段三：前端页面

1. 新增 API 类型和函数 — `api/client.ts`
2. 新建 `views/PlatformAccountsView.vue`
3. 注册路由 / 菜单 / 权限
4. 补充国际化文案

### 阶段四：端到端验证

1. 创建测试种子数据
2. 全流程验证：创建 → 共享 → 查看密码 → 审计记录
3. 运行 `npm run typecheck && npm run build && go build ./... && go test ./... -count=1`

## 六、设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 权限范围 | 两者都支持（私有 + 共享） | 精准匹配 A 类平台账号"大部分个人 + 少数团队共用"的现实 |
| 密码存储 | EncryptedField 复用 | 与 HostAccount/DatabaseAccount 一致，无需新建加密体系 |
| 审计方式 | 新建 AuditEvent 表 | 现有审计表偏向会话记录，不适用于管理操作审计 |
| 共享粒度 | 用户级 + 角色级 | 支持灵活授权，与现有 RBAC 体系自然整合 |
| 导入功能 | 后续迭代 | MVP 先覆盖核心 CRUD + 共享，导入为独立迭代 |
