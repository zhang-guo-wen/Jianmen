# 标准化审计字段与停用标记设计

**日期**：2026-07-23
**最后更新**：2026-07-24
**状态**：已确认

## 背景

项目共有 41 个运行时数据模型。重构前，不同表的创建人、更新人和生命周期字段不统一，业务查询也没有统一的停用数据隔离规则。

本设计统一审计字段和业务对象的逻辑停用语义，同时保留审计日志、关联表和保留策略各自需要的生命周期行为。

## 目标

1. 业务表统一记录创建人、更新人、创建时间和更新时间。
2. 业务对象被删除时不立即丢失记录，而是改为非活跃状态。
3. 所有正常业务查询默认只返回活跃记录。
4. 已停用业务对象不能继续认证、建立会话、签发凭据或参与授权。
5. 软删除后允许使用相同业务键重新创建记录。
6. 本次尚未发布的审计重构只保留一个最终版本化迁移。

## 模型分类

| 类别 | 数量 | 审计字段 | 生命周期 |
|------|-----:|----------|----------|
| 业务表 | 30 | `created_at`, `created_by`, `updated_at`, `updated_by`, `active_marker` | 逻辑停用 |
| 审计日志表 | 8 | `created_at`, `created_by`，个别表保留既有运行状态字段 | 按审计保留策略处理 |
| 纯关联表 | 3 | 保留既有时间字段 | 物理删除 |

### 业务表

User, AdminSession, WebSocketTicket, SystemInitialization, SystemSetting,
SystemSettingRevision, UserPublicKey, Role, Permission, Resource,
ResourceSequence, ResourceGroup, UserGroup, UserPreference,
ConnectionPassword, AIAccessToken, ResourceGrant, Host, HostAccount,
DatabaseInstance, DatabaseAccount, Application, ContainerEndpoint, Session,
UserSession, TemporaryAccount, TemporaryCredential, TemporaryAccountGrant,
PlatformAccount, DatabaseProvisioningOperation。

### 审计日志表

AuditEvent, LoginAuditLog, AuditSession, AuditArtifact,
AuditRDPChannelEvent, AuditSSHCommand, AuditDBQuery, AuditSFTPEvent。

### 纯关联表

RolePermission, UserRole, UserGroupMember。

`DatabaseProvisioningOperation` 必须显式注册到统一模型清单，不能只依赖 GORM 通过关联关系间接发现。

## 最终字段语义

### FullAudit

业务表嵌入统一结构：

```go
type FullAudit struct {
    CreatedBy    string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
    UpdatedBy    string    `gorm:"index;size:64;not null;default:''" json:"updated_by"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
    ActiveMarker *int      `gorm:"column:active_marker;index;default:1" json:"-"`
}
```

`active_marker` 只表示记录能否参与正常业务：

- `1`：活跃。
- `NULL`：已停用或已从正常业务中移除。

它不是删除时间，因此不再使用 `deleted_at` 或 `DeletedAt` 命名。

### 删除时间

不单独保存删除时间。执行逻辑停用时：

- 将 `active_marker` 设为 `NULL`；
- 将 `updated_at` 设为本次操作时间；
- 将 `updated_by` 设为操作者；
- 对拥有标准 `status` 字段的业务对象，将 `status` 设为 `disabled`。

由于正常写路径只允许操作活跃记录，停用后的 `updated_at` 即该记录的停用时间。恢复或再次修改非活跃记录不属于普通业务流程，必须通过专用维护流程完成并留下新的审计记录。

### CreationAudit

只需要创建审计的表使用：

```go
type CreationAudit struct {
    CreatedBy string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
    CreatedAt time.Time `gorm:"index" json:"created_at"`
}
```

审计表默认只追加，但已有审计保留策略可以按配置物理清理过期会话及其子记录。保留策略不等同于业务删除接口。

## 审计操作者传递

Context key 必须由模型包统一管理，不能在不同包中声明“值相同但类型不同”的 key。

```go
ctx = model.WithAuditUserID(ctx, userID)
db.WithContext(ctx).Create(&record)
```

管理端、SSH、后台任务等入口都使用同一个辅助函数。Hook 读取不到操作者时不得覆盖模型中已经显式设置的非空 `created_by` 或 `updated_by`。

使用 `Table(...).Updates(map)` 等不会可靠执行模型 Hook 的路径，必须在更新字段中显式写入 `updated_by`。

## 查询规则

所有业务读取默认添加：

```sql
WHERE active_marker = 1
```

规则同时适用于：

- 列表、详情和存在性检查；
- 更新、删除前的目标查找；
- 用户认证、Token、公钥和浏览器会话身份查询；
- UserSession、AdminSession、WebSocketTicket；
- HostAccount 及其父 Host；
- DatabaseAccount 及其父 DatabaseInstance；
- SQL 控制台、临时授权、连接密码签发和资源授权；
- 资源同步、回填和元数据重建。

父子资源必须 fail closed：子记录或父记录任一非活跃，都视为不可用。

`active_marker` 不通过 API 返回。

## 删除规则

业务表不直接调用 GORM `Delete`。标准逻辑停用必须原子更新：

```go
updates := map[string]any{
    "active_marker": nil,
    "updated_at":    now,
    "updated_by":    actorID,
}
```

拥有标准状态字段时同时加入：

```go
updates["status"] = "disabled"
```

关联表 RolePermission、UserRole、UserGroupMember 仍采用物理删除。

资源目录、资源授权和数据库供给操作也属于业务记录；其删除路径必须遵守逻辑停用规则，不能因为它们是派生记录或内部状态而直接物理删除。

## 唯一索引

需要“停用后可重建”的唯一业务键统一使用：

```text
(business_key..., active_marker)
```

活跃记录都使用 `active_marker = 1`，因此业务键只能存在一条活跃记录。非活跃记录使用 `NULL`，SQLite、MySQL 和 PostgreSQL 的普通唯一索引都允许保留多条历史记录。

索引名称使用 `_active` 后缀，不再使用容易误解为删除时间的 `_deleted` 后缀。

迁移必须验证索引是否同时满足：

1. 是唯一索引；
2. 列顺序与最终定义一致；
3. 不再存在只覆盖业务键的旧唯一索引；
4. 不再存在引用 `deleted_at` 的旧审计重构索引。

## 迁移策略

本次审计重构尚未发布，因此源码中只保留一个最终迁移，不保留“先增加时间型 `deleted_at`、再转整数、再修复”的中间迁移链。

最终迁移负责：

1. 注册并迁移全部 41 个模型，包括 `DatabaseProvisioningOperation`；
2. 为业务表建立最终审计字段和 `active_marker`；
3. 将开发阶段数据库中可能存在的旧 `deleted_at` 数据转换为 `active_marker`：
   - 时间 sentinel 或整数 `1` 转为活跃；
   - `NULL` 或真实删除时间转为非活跃；
4. 删除旧 `deleted_at` 列和 `_deleted` 索引；
5. 创建并校验最终 `_active` 复合唯一索引；
6. 在 SQLite 表重建期间保存并恢复外键、无关索引和无关触发器；引用旧
   `deleted_at` 的索引或触发器作为过渡结构移除；
7. 将迁移内容和迁移版本记录放在同一个事务中；
8. 对 SQLite、MySQL、PostgreSQL 提供安全实现；已经是最终结构时应幂等成功，不能无条件拒绝非 SQLite。

历史开发数据库可能已经记录旧迁移版本。最终迁移使用新的单一版本号，使这些数据库仍会执行一次最终结构收敛；全新数据库只会看到这一个审计重构迁移。

## 测试要求

至少覆盖以下回归场景：

1. 用户逻辑删除后，密码、Token、公钥和已有浏览器会话均不能继续认证。
2. UserSession、DatabaseAccount 或其父资源非活跃后，不能创建连接、签发凭据或进入 SQL 控制台。
3. 管理端请求写入正确的 `created_by`、`updated_by`。
4. 逻辑删除写入 `active_marker = NULL`、`status = disabled`、`updated_at` 和 `updated_by`。
5. 同一业务键在活跃状态下不能重复，停用后可以重建。
6. 模型注册测试能发现任何嵌入 `FullAudit` 却未进入迁移清单的模型。
7. 从时间型 `deleted_at` 和整数型 `deleted_at` 两种开发阶段 schema 均可升级。
8. SQLite 表重建失败完整回滚，外键、索引和触发器得到恢复。
9. MySQL、PostgreSQL 集成测试执行最终迁移，而不是只跑普通单元测试。

## 影响范围

| 层级 | 改动 |
|------|------|
| `internal/model` | `FullAudit.ActiveMarker`、统一审计 Context API、完整模型注册 |
| `internal/store` | 活跃查询、父子 fail-closed、逻辑停用与状态停用 |
| `internal/storage` | 单一最终迁移、旧开发 schema 收敛、最终复合唯一索引 |
| `internal/server` | 管理端与 SSH 使用统一审计 Context API |
| 测试 | 认证隔离、审计操作者、迁移兼容和模型注册不变量 |

## 不改变的部分

- API 不返回内部活跃标记。
- 加密字段和密钥管理方式不变。
- 三张纯关联表保持物理删除。
- 审计数据仍受既有保留策略控制。
