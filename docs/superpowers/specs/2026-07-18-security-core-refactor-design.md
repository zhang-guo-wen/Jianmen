# Jianmen 安全内核与核心稳定化设计

日期：2026-07-18

## 1. 背景

Jianmen 当前已经覆盖管理 API、SSH/SFTP、Web Terminal、MySQL/PostgreSQL/Redis
网关、应用代理、容器入口、临时授权和 AI 访问。账号级资源模型已经基本建立，但身份、
授权、会话、凭据和审计规则仍分散在各协议 Server 中。

本设计将后续重构分为两个阶段：

1. **第一阶段：发布阻断项关闭**——统一安全决策，消除可直接利用的凭据、授权和传输风险。
2. **第二阶段：核心稳定化**——完成分层、事务、资源关系、协议边界、审计治理和迁移治理。

这两个阶段只重构模块化单体，不拆分微服务，不引入多租户和高可用控制面。

## 2. 目标

### 2.1 第一阶段目标

- 所有入口使用同一身份状态和授权语义。
- 超级管理员只有一个数据库事实来源，可审计并即时生效。
- 动作权限、资源授权、deny、有效期和禁用状态在连接前统一决策。
- 浏览器不再把长期管理令牌放入 URL 或 `localStorage`。
- AI 令牌和刷新令牌只在签发或轮换时显示一次。
- 容器、应用、SSH、SFTP、Web Terminal、数据库连接均不能绕过资源授权。
- 默认拒绝不安全的 SSH 主机密钥、远程 Docker API 和生产 HTTP 配置。
- CI 在发布前强制执行前后端构建和测试。

### 2.2 第二阶段目标

- HTTP handler 只负责请求解析和响应序列化。
- 业务规则进入 service，SQL 和事务进入 store。
- Store 接口在使用方按资源定义，所有 I/O 接口第一个参数是 `context.Context`。
- 临时授权的创建、延期、禁用和撤销全部原子化。
- 资源组使用稳定 ID 关系，不再依靠名称字符串维持授权继承。
- 数据库协议适配器解耦，协议监听和 TLS 能独立配置与测试。
- 审计具备保留期限、脱敏、配额和一致性清理策略。
- 版本迁移成为生产唯一模式，并有空库和升级路径测试。

## 3. 非目标

以下内容不属于这两阶段：

- 微服务拆分。
- 多租户。
- OIDC、LDAP、WebAuthn 和审批流。
- 分布式在线会话、对象存储和多节点高可用。
- 完整密码轮换平台。
- 新增资源类型或新的代理协议。

重构期间冻结应用代理、容器、平台账号和 AI 访问的新功能扩张。

## 4. 方案比较

### 4.1 方案 A：直接修补每个入口

在 Web Terminal、容器、应用和数据库网关中分别补检查。

优点是改动小；缺点是继续保留多套身份和授权逻辑，下一次新增入口仍会产生绕过。
该方案不采用。

### 4.2 方案 B：一次性重写全部认证与协议层

同时替换浏览器会话、RBAC、资源授权、SSH 和数据库代理。

优点是最终结构整齐；缺点是变更面过大，难以建立可信回归证据，也不利于多人并行。
该方案不采用。

### 4.3 方案 C：建立统一安全内核，逐入口迁移

先建立 `IdentityService`、`AuthorizationService` 和小型 Repository 接口，再逐一迁移
Admin、Web Terminal、SSH/SFTP、数据库、应用和容器入口。迁移期间旧 Checker 仅作为
Repository 适配器存在，所有新路径必须经过统一 Service。

该方案可按垂直切片提交、测试和回滚，是本设计采用的方案。

## 5. 目标架构

```text
cmd/bastion-core
  └─ 组装依赖、启动和关闭

internal/server/*
  └─ HTTP、WebSocket、SSH、数据库、应用等协议适配

internal/handler/admin/*
  └─ 管理 API 请求解析、DTO 和响应

internal/service/*
  ├─ IdentityService
  ├─ AuthorizationService
  ├─ BrowserSessionService
  ├─ TemporaryAccessService
  ├─ UserService / UserGroupService / RoleService
  └─ AuditPolicyService

internal/store/*
  ├─ 各 Service 使用方定义的小接口
  ├─ DBStore 作为组合实现
  └─ 事务和持久化

internal/model/*
  └─ 纯 GORM 实体
```

所有连接入口执行相同流程：

```text
authenticate subject
  → validate subject/session status and expiry
  → authorize action
  → authorize concrete resource
  → apply deny precedence
  → validate parent/resource/account status and expiry
  → establish upstream connection
  → register online session and audit
```

## 6. 身份与超级管理员

### 6.1 唯一事实来源

`model.User` 增加 `IsSuperAdmin bool`。超级管理员不再来自：

- `config.users` 的隐式约定。
- `.super_admin_ids` 文件。
- 各 Server 自己加载的内存 Map。

初始化向导创建的首个用户在同一数据库事务中写入 `is_super_admin=true`。配置文件用户
只有明确设置 `super_admin=true` 才能成为超级管理员。

产品尚未发布，不保留 `.super_admin_ids` 兼容导入。启动时不得读取、创建或重命名该文件；
配置用户的显式 `super_admin` 仅在数据库没有有效超级管理员时作为首次写库或恢复种子；
已有有效数据库管理员时，配置不得提升其他用户。数据库存在用户但没有有效超级管理员时，
启动失败关闭，避免进入无法管理但表面可用的状态。

### 6.2 身份状态

`IdentityService` 返回经过验证的 `Subject`：

```go
type Subject struct {
	ID           string
	Username     string
	SuperAdmin   bool
	SessionID    string
	SessionType  string
	SessionUntil *time.Time
}
```

状态验证统一覆盖：

- 用户存在且为 `active`。
- 非超级管理员用户未过期。
- 用户会话未禁用、未撤销、未过期。
- 浏览器会话或 AI Token 未撤销且未过期。

## 7. 统一授权

Service 对外提供：

```go
type AuthorizationRequest struct {
	UserID       string
	Actions      []string
	ResourceType string
	ResourceID   string
}

type AuthorizationDecision struct {
	Allowed bool
	Reason  string
}

func (s *AuthorizationService) Authorize(
	ctx context.Context,
	request AuthorizationRequest,
) (AuthorizationDecision, error)
```

决策规则：

1. 超级管理员在身份状态有效时允许。
2. 任一请求动作必须被角色允许。
3. 指定资源时必须存在有效 ResourceGrant。
4. deny 始终覆盖 allow。
5. 用户组和临时授权与直接授权合并。
6. 资源组授权通过稳定成员关系展开。
7. 数据库错误返回 error，不得转换为普通拒绝或静默允许。

列表接口使用批量可见性查询，不能对每个资源执行完整 N+1 授权查询。

## 8. 浏览器会话与 WebSocket

管理登录改为服务端会话：

- 数据库只保存随机会话令牌的 SHA-256 Hash。
- 浏览器只接收 `HttpOnly` Cookie。
- 生产环境 Cookie 必须 `Secure`。
- Cookie 使用 `SameSite=Lax`，路径为 `/`。
- 管理 API 的状态变更请求需要 `X-CSRF-Token`。
- CSRF Token 不是身份凭据，可保存在内存或 `sessionStorage`。
- 前端不再将长期身份凭据保存在 `localStorage`。

Web Terminal 连接流程：

1. 已登录页面调用受保护 API 申请一次性票据。
2. 票据绑定用户、目标账号和到期时间，最长有效 30 秒。
3. WebSocket URL 只携带一次性票据。
4. 票据首次成功使用后立即失效。
5. 代理和日志中不得出现长期会话令牌。

## 9. 凭据与 AI Token

- AI Access Token、Refresh Token 只保存 Hash。
- 创建和刷新响应是唯一返回明文的时机。
- Token 详情接口只返回元数据和 `has_secret=false`。
- 提示词和文档不得拼接真实 Token。
- Refresh Token 轮换后旧值立即失效。
- 临时授权禁用时，关联 AI Token 在同一事务中撤销。
- 平台账号密码查看继续保留为实验功能，但必须经过动作权限、资源授权和操作审计；
  第二阶段完成前不得把它宣传为凭据托管能力。

## 10. 传输安全

### 10.1 管理面

生产配置必须满足以下任一条件：

- Admin Server 直接配置证书和私钥并启用 TLS。
- Admin 只监听 loopback，由受信任反向代理终止 HTTPS，且 `public_url` 是 `https://`。

非 loopback 的明文 HTTP 仅在显式 `allow_insecure_http=true` 时允许，启动日志必须输出高风险警告。

### 10.2 数据库网关

MySQL、PostgreSQL、Redis 改用独立监听地址。每个监听器独立配置 TLS：

- PostgreSQL 不得在拒绝 SSL 后请求明文堡垒机密码。
- Redis 密码认证在非 TLS 监听器上默认禁用。
- MySQL 完整认证不得向非 TLS 上游发送明文密码。
- 上游数据库连接支持独立的 TLS 和证书校验配置。

### 10.3 SSH 与 Docker

- 新建主机账号默认要求指纹确认。
- 连接测试不得自动设置 `InsecureIgnoreHostKey`。
- 远程 Docker API 只允许 HTTPS；HTTP 仅允许 loopback。
- Docker over SSH 继续复用主机账号凭据和主机密钥校验。

## 11. 临时授权事务

以下操作必须原子提交：

- 创建临时 UserSession、TemporaryAccount、Grant 和 ConnectionPassword。
- 延长 TemporaryAccount、Grant、Session 和 AI Token 的有效期。
- 禁用 TemporaryAccount，同时撤销 Grant、Session、ConnectionPassword 和 AI Token。

Service 不直接持有 GORM。Store 提供面向业务操作的事务方法，不暴露 `*gorm.DB`。

## 12. 资源组关系

新增：

```text
resource_group_members
- id
- group_id
- resource_type
- resource_id
- created_at
```

唯一约束为 `(group_id, resource_type, resource_id)`。

业务实体仍保留自己的类型表；`resources` 是统一注册表；分组成员表是授权继承的唯一关系来源。
旧 `group_name` 字段在第二阶段迁移完成后删除，不保留双写。

## 13. 审计策略

新增配置：

- `retention_days`
- `max_replay_bytes`
- `record_input`
- `redact_patterns`

规则：

- 默认不记录终端原始输入。
- SQL、命令和路径在写入前执行脱敏。
- 数据库审计记录和 Replay 文件必须一致删除。
- 清理任务失败只告警，不删除另一侧数据。
- 审计写入失败策略按事件类型显式定义：连接元数据失败时拒绝建立新会话；
  会话中增量事件失败时记录高优先级告警并继续连接。

## 14. 数据迁移

- 生产配置默认 `auto_migrate=false`。
- 版本化 Migration 是唯一生产迁移入口。
- 历史 Migration 不再修改。
- 每个新实体和索引都有独立版本号。
- 测试同时覆盖空库迁移和上一版本升级。
- Migration 失败时不得写入 `schema_migrations`。

## 15. 测试策略

### 15.1 单元测试

- IdentityService：禁用、过期、会话撤销、超级管理员。
- AuthorizationService：action、grant、deny、用户组、临时授权和错误传播。
- BrowserSessionService：Hash、过期、撤销、CSRF 和一次性 WS 票据。
- TemporaryAccessService：事务成功和任一步失败回滚。
- Audit redactor：密码、Token、私钥片段和 SQL 字面值。

### 15.2 路由级测试

权限测试必须通过真实 Router 和 Middleware，禁止只直接调用 Handler 证明鉴权成立。

### 15.3 协议测试

SSH、SFTP、Web Terminal、MySQL、PostgreSQL、Redis、应用代理和容器分别验证：

- 缺少动作权限。
- 缺少资源授权。
- deny 覆盖 allow。
- 用户、会话、资源或账号禁用和过期。
- 后端存储故障。

协议解析器增加 fuzz 测试；真实数据库测试使用 integration build tag。

### 15.4 发布门禁

```text
npm run typecheck
npm run build
go build ./...
go test ./... -count=1
go vet ./...
go test -tags=integration ./internal/integration
.\build.ps1
```

Docker 集成测试不可用时，CI 必须明确失败或由独立 required job 报告，不允许静默跳过。

## 16. 完成定义

### 第一阶段完成

- 所有发布阻断项都有失败测试、修复提交和回归证据。
- 所有连接入口经过统一 AuthorizationService。
- 浏览器和 AI 不再回取或传播长期明文 Token。
- 非安全传输和 SSH/Docker 不安全默认被阻止。
- CI 门禁在 PR 和发布流程中生效。

### 第二阶段完成

- 临时授权、用户、用户组和 RBAC 不再由 Handler 直接操作 GORM。
- `store.Store` 被使用方小接口替代，I/O 方法全部传播 Context。
- 分组授权使用稳定成员关系。
- 数据库协议适配、审计治理和版本迁移有自动化验证。
- 全量测试、构建、打包和主目录运行冒烟全部通过。
