# 标准化审计字段、逻辑删除与时间格式设计

**日期**：2026-07-23
**最后更新**：2026-07-24
**状态**：已确认

## 背景

项目共有 41 个运行时数据模型。重构前，不同表的创建人、更新人和生命周期字段不统一，业务查询也没有统一的逻辑删除数据隔离规则；数据库时间还同时存在本地时区、UTC、RFC3339 和不同小数秒精度，API 与页面也采用了多套序列化及显示方式。

本设计统一审计字段、业务对象的逻辑删除语义以及业务日期时间契约，同时保留审计日志、关联表和保留策略各自需要的生命周期行为。

## 目标

1. 业务表统一记录创建人、更新人、创建时间和更新时间。
2. 业务对象被删除时不立即丢失记录，而是写入统一的逻辑删除标记。
3. 所有业务查询默认排除已逻辑删除记录。
4. 已停用业务对象不能继续认证、建立会话、签发凭据或参与授权。
5. 逻辑删除后允许使用相同业务键重新创建记录。
6. 本次尚未发布的审计重构只保留一个最终版本化迁移。
7. 数据库日期时间按统一的 UTC 语义、无时区列和固定精度存储，API 序列化及页面显示采用同一格式。

## 模型分类

| 类别 | 数量 | 审计字段 | 生命周期 |
|------|-----:|----------|----------|
| 业务表 | 30 | `created_at`, `created_by`, `updated_at`, `updated_by`, `active_marker` | 逻辑删除 |
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
    CreatedAt    time.Time `json:"-"`
    UpdatedAt    time.Time `json:"-"`
    ActiveMarker *int32    `gorm:"column:active_marker;type:int;index;default:1" json:"-"`
}
```

持久化模型中的原始 `time.Time` 不直接参与 JSON 序列化；需要返回的 `created_at`、`updated_at` 由 API DTO 使用共享时间类型或统一格式化函数生成。

`active_marker` 是所有业务表统一的逻辑删除字段。数据库逻辑定义为可空整数 `INT NULL DEFAULT 1`，Go 使用可表达 `NULL` 且能稳定映射 SQL `INT` 的 `*int32`。不同数据库的最终物理类型为：

| 数据库 | 字段定义 |
|---|---|
| SQLite | `INTEGER NULL DEFAULT 1` |
| MySQL | `INT NULL DEFAULT 1` |
| PostgreSQL | `INTEGER NULL DEFAULT 1` |

该字段只允许两种值：

- `1`：未删除，记录仍然存在；即使业务 `status` 为 `disabled`，该值仍为 `1`。
- `NULL`：已逻辑删除，记录不得再参与正常业务。

禁止用 `0`、`false`、删除时间或其他整数表达删除状态。数据库默认值和模型创建 Hook 都必须保证正常新增记录写入 `1`；不能依赖 `DEFAULT 1` 修正调用方显式传入的 `NULL`。

支持并实际执行 `CHECK` 约束的数据库应增加 `CHECK (active_marker IS NULL OR active_marker = 1)`；由于 MySQL 5.7 不执行 `CHECK`，迁移校验、模型 Hook 和所有写路径仍必须共同保证该不变量，不能只依赖数据库约束。

`active_marker` 不是删除时间，也不是业务启停状态，因此不使用 `deleted_at`、`DeletedAt` 命名，也不能用它替代 `status`。手动停用业务对象只修改 `status`，不得将 `active_marker` 设为 `NULL`。

### 逻辑删除与删除时间

不单独保存删除时间。执行逻辑删除时：

- 将 `active_marker` 设为 `NULL`；
- 将 `updated_at` 设为本次操作时间；
- 将 `updated_by` 设为操作者；
- 对拥有标准 `status` 字段的业务对象，将 `status` 设为 `disabled`。

由于正常写路径只允许操作未删除记录，逻辑删除后的 `updated_at` 即该记录的删除时间。恢复或再次修改已删除记录不属于普通业务流程，必须通过专用维护流程完成并留下新的审计记录。

### CreationAudit

只需要创建审计的表使用：

```go
type CreationAudit struct {
    CreatedBy string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
    CreatedAt time.Time `gorm:"index" json:"-"`
}
```

审计表默认只追加，但已有审计保留策略可以按配置物理清理过期会话及其子记录。保留策略不等同于业务删除接口。

## 时间存储、序列化与显示

### 适用范围

本规范适用于业务模型、审计模型和管理 API 中表示时间点的字段，包括但不限于：

- `created_at`、`updated_at`；
- `starts_at`、`started_at`、`ended_at`；
- `expires_at`、`last_seen_at`；
- API Envelope 的 `timestamp`。

仅表示日期的 `DATE`、表示时长的 duration、数据库序列、Unix 时间戳以及 SSH、SFTP、数据库协议、录像文件等外部标准明确规定的机器格式不强制改成展示字符串。外部格式必须在适配层转换，不能直接泄漏为管理 API 或页面的时间格式。

### 数据库存储

数据库中的时间点统一按 **UTC 语义** 保存，但列和值本身不携带时区。写入前统一执行 `t.UTC().Truncate(time.Microsecond)`，读取时也必须显式按 UTC 解释，禁止依赖操作系统、数据库连接或数据库会话的本地时区。

存储保留微秒精度，避免削弱基于 `updated_at` 的乐观锁、CAS 和审计排序。最终类型为：

| 数据库 | 字段类型 | 值示例 |
|---|---|---|
| SQLite | 声明为 `DATETIME`，实际值固定写成 TEXT `yyyy-MM-dd HH:mm:ss.ffffff` | `2026-05-05 03:02:05.123456` |
| MySQL | `DATETIME(6)` | `2026-05-05 03:02:05.123456` |
| PostgreSQL | `TIMESTAMP(6) WITHOUT TIME ZONE` | `2026-05-05 03:02:05.123456` |

SQLite 没有真正的日期时间存储类型，因此字段声明本身不能保证格式。统一写路径、迁移和测试必须共同验证实际值为 TEXT、固定六位微秒且不含时区。三种数据库读取出的值都必须先归一化为 UTC 微秒精度，再参与相等比较、排序或 CAS。

禁止使用以下方式：

- MySQL `TIMESTAMP` 或 PostgreSQL `TIMESTAMPTZ` 作为统一业务时间列；
- 在 SQLite 值中保存 `Z`、`+08:00` 等时区或偏移后缀；
- 直接使用受数据库会话时区影响的 `NOW()`、`CURRENT_TIMESTAMP`、`clock_timestamp()`；确需数据库函数时，必须显式转换为 UTC、去除时区并保持微秒精度；
- 将 API 展示到秒的字符串回写为 `updated_at`，或用它参与精确并发比较。

### 业务时区

系统另有一个统一的“业务时区”，用于解析无时区的 API 输入以及生成面向用户的输出，默认值为 `Asia/Shanghai`。业务时区必须由应用配置明确加载，不能使用各服务器不一致的隐式 `time.Local`。

处理方向固定为：

1. API 输入按业务时区解析；
2. 转换为 UTC 后，以无时区形式写入数据库；
3. 从数据库读取时按 UTC 还原时间点；
4. 序列化或展示前转换到业务时区。

### API 序列化与页面显示

所有管理 API 请求、响应和 Envelope 时间统一使用：

- 格式：`yyyy-MM-dd HH:mm:ss`；
- Go layout：`2006-01-02 15:04:05`；
- 示例：`2006-05-05 11:02:05`。

输出不包含 `T`、`Z`、时区偏移或小数秒。后端必须通过共享时间类型或统一格式化函数完成序列化，禁止直接暴露 `time.Time` 的默认 JSON，禁止各 Store 自行调用 `RFC3339` / `RFC3339Nano`。

前端把后端返回值视为已经转换到业务时区的展示字符串，列表、详情和弹窗直接显示，不再使用 `new Date()`、`Date.parse()`、`toLocaleString()` 或 `Intl.DateTimeFormat` 做二次时区转换。确需倒计时、过期判断或排序时，后端应提供明确的机器时间值或由共享解析工具按业务时区处理，不能依赖浏览器对无时区字符串的默认解释。

可空时间按字段契约返回 `null` 或省略；禁止使用 `0001-01-01 00:00:00`、空字符串或其他 sentinel 表示空值。解析请求时只接受完整合法格式，不静默接受带时区、带小数秒或不完整日期的变体。

秒级 API 时间只用于输入和展示，不能充当并发版本。需要跨 API 执行乐观锁或 CAS 时，必须提供独立、无损且对调用方不透明的 `revision`，服务端再用数据库微秒时间或专用版本号完成比较。

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

这表示“未逻辑删除”，不等同于 `status = active`。管理端可以按权限查看 `active_marker = 1 AND status = disabled` 的记录；认证、连接、授权等运行时路径还必须叠加自身的启用状态、有效期和权限条件。

未删除过滤规则同时适用于：

- 列表、详情和存在性检查；
- 更新、删除前的目标查找；
- 用户认证、Token、公钥和浏览器会话身份查询；
- UserSession、AdminSession、WebSocketTicket；
- HostAccount 及其父 Host；
- DatabaseAccount 及其父 DatabaseInstance；
- SQL 控制台、临时授权、连接密码签发和资源授权；
- 资源同步、回填和元数据重建。

父子资源必须 fail closed：子记录或父记录任一已逻辑删除，或者运行时要求启用而其状态为停用，都视为不可用。

`active_marker` 不通过 API 返回。

## 删除规则

业务表不直接调用 GORM `Delete`。标准逻辑删除必须原子更新：

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

资源目录、资源授权和数据库供给操作也属于业务记录；其删除路径必须遵守逻辑删除规则，不能因为它们是派生记录或内部状态而直接物理删除。

判断逻辑删除必须使用 `active_marker IS NULL`，判断未删除必须使用 `active_marker = 1`；SQL 中不得写 `active_marker = NULL`，也不得用 `active_marker <> 1` 替代墓碑条件。

## 唯一索引

需要“停用后可重建”的唯一业务键统一使用：

```text
(business_key..., active_marker)
```

未删除记录都使用 `active_marker = 1`，因此业务键只能存在一条未删除记录。已逻辑删除记录使用 `NULL`，SQLite、MySQL 和 PostgreSQL 的普通唯一索引都允许保留多条历史记录。

索引名称使用 `_active` 后缀，不再使用容易误解为删除时间的 `_deleted` 后缀。

`active_marker` 必须是复合唯一索引的最后一列。需要唯一约束的业务键列原则上必须为 `NOT NULL`，否则业务键自身的 `NULL` 也会绕过“只能存在一条未删除记录”的约束。PostgreSQL 不得为此类索引启用 `NULLS NOT DISTINCT`。

迁移必须验证索引是否同时满足：

1. 是唯一索引；
2. 列顺序与最终定义一致；
3. 不再存在只覆盖业务键的旧唯一索引；
4. 不再存在引用 `deleted_at` 的旧审计重构索引。

## 迁移策略

本次审计重构尚未发布，因此源码中只保留一个最终迁移，不保留“先增加时间型 `deleted_at`、再转整数、再修复”的中间迁移链。

最终迁移负责：

1. 注册并迁移全部 41 个模型，包括 `DatabaseProvisioningOperation`；
2. 为业务表建立最终审计字段和可空整数 `active_marker`，默认值为 `1`；
3. 将开发阶段数据库中可能存在的旧 `deleted_at` 数据转换为 `active_marker`：
   - 时间 sentinel 或整数 `1` 转为未删除；
   - `NULL` 或真实删除时间转为已逻辑删除；
4. 校验最终字段可空、默认值为 `1`，并将可判定的旧数据收敛为 `1` 或 `NULL`；遇到语义不明的其他整数不得静默保留；
5. 删除旧 `deleted_at` 列和 `_deleted` 索引；
6. 创建并校验最终 `_active` 复合唯一索引；
7. 在 SQLite 表重建期间保存并恢复外键、无关索引和无关触发器；引用旧
   `deleted_at` 的索引或触发器作为过渡结构移除；
8. 将迁移内容和迁移版本记录放在同一个事务中；
9. 将历史时间统一为 UTC 语义、无时区列和微秒精度：
   - 带 `Z` 或偏移的值按其真实时间点转换为 UTC；
   - 旧的无时区值必须按显式指定的旧部署时区解释；只有能够确认历史环境一直使用项目旧默认时区时才允许采用 `Asia/Shanghai`，已有数据来源不明时必须要求显式配置，否则迁移失败；
   - 空值保持 `NULL`，非法值使迁移失败并回滚，不能替换成零时间；
10. 将 SQLite、MySQL、PostgreSQL 的时间列分别收敛为本设计规定的 `DATETIME`、`DATETIME(6)`、`TIMESTAMP(6) WITHOUT TIME ZONE`；
11. 对 SQLite、MySQL、PostgreSQL 提供安全实现；已经是最终结构和数据格式时应幂等成功，不能无条件拒绝非 SQLite。

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
10. 三种数据库迁移后都满足：字段为可空 `INT`、默认值为 `1`，数据只包含 `1` 或 `NULL`。
11. 新增记录默认写入 `1`；显式 `0`、其他整数和错误类型被拒绝。
12. 手动停用只修改业务 `status`，逻辑删除同时写入 `active_marker = NULL` 和 `status = disabled`。
13. 带时区、无时区和带小数秒的历史时间都能按指定旧时区迁移为 UTC 语义的无时区值，非法值完整回滚。
14. 数据库存储保留微秒；基于 `updated_at` 的 CAS 不使用展示到秒的值。
15. API 请求、响应、错误 Envelope 和页面都使用 `yyyy-MM-dd HH:mm:ss`，可空时间不输出零时间。

## 影响范围

| 层级 | 改动 |
|------|------|
| `internal/model` | `FullAudit.ActiveMarker`、统一审计 Context API、共享时间类型与格式常量、完整模型注册 |
| `internal/store` | 未删除查询、父子 fail-closed、逻辑删除、UTC 无时区持久化与时间 DTO 映射 |
| `internal/storage` | 单一最终迁移、旧开发 schema 与时间值收敛、最终复合唯一索引 |
| `internal/server` | 管理端与 SSH 使用统一审计 Context API；API 时间统一序列化 |
| `web` | 时间字符串直接展示；需要计算时只使用共享解析工具 |
| 测试 | 认证隔离、审计操作者、迁移兼容、时间契约和模型注册不变量 |

## 不改变的部分

- API 不返回内部活跃标记。
- 加密字段和密钥管理方式不变。
- 三张纯关联表保持物理删除。
- 审计数据仍受既有保留策略控制。
- 外部协议或文件格式强制要求的 RFC3339、Unix 时间戳等机器格式保持其标准，但必须在管理 API 边界转换为统一时间格式。
