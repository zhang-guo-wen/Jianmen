# 标准化审计字段设计

**日期**: 2026-07-23  
**状态**: 已确认

## 背景

当前项目的 34 个数据模型中，字段规范不统一：
- 0/34 有软删除（`deleted_at`）
- 4/34 有创建人（`created_by`）
- 2/34 有更新人（`updated_by`，且字段名不一致）
- 4 个审计子表甚至没有 `created_at`

## 目标

为所有数据库表统一增加标准化审计字段，统一软删除机制，确保数据可追溯。

## 设计决策

### 决策 1: 分类处理

按表的性质分为三类，分别处理：

| 类别 | 说明 | 表数 | 审计字段 | 软删除 |
|------|------|------|---------|--------|
| **业务表** | 核心业务实体 | ~22 | created_at + created_by + updated_at + updated_by + deleted_at | ✅ |
| **审计日志表** | 只追加不删除 | 8 | created_at + created_by | ❌ |
| **纯关联表** | M:N 关系表 | 3 | 无 | 物理删除 |

**业务表清单** (22张):
User, AdminSession, WebSocketTicket, SystemInitialization, SystemSetting, SystemSettingRevision, UserPublicKey, Role, Permission, Resource, ResourceSequence, ResourceGroup, UserGroup, UserPreference, ConnectionPassword, AIAccessToken, ResourceGrant, Host, HostAccount, DatabaseInstance, DatabaseAccount, Application, ContainerEndpoint, Session, UserSession, TemporaryAccount, TemporaryCredential, TemporaryAccountGrant, PlatformAccount, DatabaseProvisioningOperation

**审计日志表清单** (8张):
AuditEvent, LoginAuditLog, AuditSession, AuditArtifact, AuditRDPChannelEvent, AuditSSHCommand, AuditDBQuery, AuditSFTPEvent

**纯关联表清单** (3张):
RolePermission, UserRole, UserGroupMember

### 决策 2: 软删除使用 sentinel 时间而非 NULL

由于 MySQL 唯一索引中 NULL 值被视为"不相等"（允许多条 NULL），不能使用 `deleted_at IS NULL` 表示未删除。采用 sentinel 时间方案：

- **未删除行**: `deleted_at = '0001-01-01 00:00:00'`
- **已删除行**: `deleted_at = 删除操作时间`
- 不使用 `gorm.DeletedAt`（它硬编码了 NULL 表示未删除），手动管理 `*time.Time` 字段

### 决策 3: GORM Hook 自动填充审计字段

通过 `BeforeCreate`/`BeforeUpdate`/`BeforeDelete` Hook 从 `db.Statement.Context` 中自动获取当前用户 ID，减少手动设置和遗漏风险。

### 决策 4: 唯一索引包含 deleted_at

所有唯一索引改为 `(业务键, deleted_at)` 复合索引，确保软删除后可以用相同业务键重新创建。

## 技术设计

### 基础结构体

新增文件 `internal/model/audit.go`:

```go
package model

import (
    "context"
    "time"
    "gorm.io/gorm"
)

// SentinelDeletedAt 未删除（活跃）行的标记时间
var SentinelDeletedAt = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)

// Context key（与 admin server 的 ctxKeyUserID 保持一致的值）
type contextKey string
const CtxKeyUserID contextKey = "admin_user_id"

// userIDFromContext 从 GORM 操作的 context 中提取当前用户 ID
func userIDFromContext(ctx context.Context) string {
    if ctx == nil {
        return ""
    }
    if id, ok := ctx.Value(CtxKeyUserID).(string); ok {
        return id
    }
    return ""
}

// CreationAudit 审计日志嵌入：创建信息
type CreationAudit struct {
    CreatedBy string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
    CreatedAt time.Time `json:"created_at"`
}

// FullAudit 业务表嵌入：完整的五个审计字段
type FullAudit struct {
    CreatedBy string     `gorm:"index;size:64;not null;default:''" json:"created_by"`
    UpdatedBy string     `gorm:"index;size:64;not null;default:''" json:"updated_by"`
    CreatedAt time.Time  `json:"created_at"`
    UpdatedAt time.Time  `json:"updated_at"`
    DeletedAt *time.Time `gorm:"index;not null;default:'0001-01-01 00:00:00'" json:"-"`
}

// Hook 实现

func (a *CreationAudit) BeforeCreate(tx *gorm.DB) error {
    a.CreatedBy = userIDFromContext(tx.Statement.Context)
    return nil
}

func (a *FullAudit) BeforeCreate(tx *gorm.DB) error {
    t := SentinelDeletedAt
    a.DeletedAt = &t
    userID := userIDFromContext(tx.Statement.Context)
    a.CreatedBy = userID
    a.UpdatedBy = userID
    return nil
}

func (a *FullAudit) BeforeUpdate(tx *gorm.DB) error {
    a.UpdatedBy = userIDFromContext(tx.Statement.Context)
    return nil
}

// 注意：不在 BeforeDelete Hook 中做软删除。
// BeforeDelete 中调用 tx.Updates() 会触发 BeforeUpdate Hook 且无法可靠阻止后续 DELETE。
// 软删除逻辑放在 Store 层：统一使用 tx.Updates() 设置 deleted_at = now，
// 永远不调用 tx.Delete() 删除业务表记录。
```

### 模型改造示例

#### 业务表 (FullAudit 嵌入)

```go
// 改造前
type User struct {
    ID           string     `gorm:"primaryKey;size:64"`
    Username     string     `gorm:"uniqueIndex;size:128;not null"`
    // ...
    CreatedAt    time.Time  `json:"created_at"`
    UpdatedAt    time.Time  `json:"updated_at"`
}

// 改造后
type User struct {
    ID       string `gorm:"primaryKey;size:64"`
    Username string `gorm:"uniqueIndex:idx_users_username_deleted,priority:1;size:128;not null"`
    // ...
    FullAudit  // 嵌入：CreatedAt, UpdatedAt, DeletedAt (含 uniqueIndex priority:2), CreatedBy, UpdatedBy
}
```

其中 `DeletedAt` 的 GORM 标签需要改为：
```go
DeletedAt *time.Time `gorm:"uniqueIndex:idx_users_username_deleted,priority:2;index;not null;default:'0001-01-01 00:00:00'" json:"-"`
```

### 唯一索引改造方案

**问题**：FullAudit 中的 `DeletedAt` 标签是通用的，不能包含每张表特定的 `uniqueIndex` 名称。且 GORM 嵌入字段的标签无法在父结构体中覆盖。

**方案**：分两步走

1. **Model 层（GORM tag）**：FullAudit 的 `DeletedAt` 只保留基本标签（`index`, `not null`, `default`），不声明 uniqueIndex。业务键字段的 `uniqueIndex` 名称统一规划为 `idx_{table}_{business_key}_deleted`，在 `priority:1` 定义业务键列。例如：

```go
type User struct {
    ID       string `gorm:"primaryKey;size:64"`
    Username string `gorm:"uniqueIndex:idx_users_username_deleted,priority:1;size:128;not null"`
    FullAudit  // DeletedAt 为 priority:2（不写在 tag 中，由 SQL 迁移补充）
}
```

2. **Store 层（SQL 迁移）**：在 AutoMigrate 之后，执行一个迁移函数。该函数：
   - 删除旧的唯一索引（不含 deleted_at）
   - 创建新的复合唯一索引（业务键列 + deleted_at）
   - 幂等：检查索引是否存在，存在则跳过

```go
func migrateUniqueIndexes(db *gorm.DB) error {
    indexes := []struct {
        table   string
        oldName string        // 旧索引名（可能为空表示 GORM auto-name）
        columns []string      // 新索引列
    }{
        {table: "users", oldName: "idx_users_username", columns: []string{"username", "deleted_at"}},
        // ... 其余表
    }
    // 删除旧索引 → 创建新索引
}
```

### Store 层改造

#### 查询过滤

所有涉及业务表的查询，统一加上 `WHERE deleted_at = '0001-01-01 00:00:00'` 条件：

```go
// 在 DBStore 中增加辅助方法
func (s *DBStore) activeScope() func(db *gorm.DB) *gorm.DB {
    return func(db *gorm.DB) *gorm.DB {
        return db.Where("deleted_at = ?", model.SentinelDeletedAt)
    }
}
```

#### 删除操作

Store 层**永远不调用 `db.Delete()` 删除业务表**。改为：

```go
func (s *DBStore) SoftDeleteUser(ctx context.Context, id string) error {
    return s.db.WithContext(ctx).
        Model(&model.User{}).
        Where("id = ?", id).
        Updates(map[string]interface{}{
            "deleted_at": time.Now().UTC(),
            "updated_at": time.Now().UTC(),
            "updated_by": userIDFromContext(ctx),
        }).Error
}
```

`BeforeUpdate` Hook 会自动设置 `updated_by`，所以 `updated_by` 无需在 map 中显式设置（但显式设置更安全）。

### 审计日志表改造

审计日志表只需加 `created_by`，不需要 `updated_by` 和 `deleted_at`。

**注意**：部分审计表已有的 `CreatedAt` 字段如果带有特殊标签（如 `gorm:"index"`），需要在嵌入 `CreationAudit` 后覆盖该字段以保留特定标签：

```go
// 改造前
type AuditEvent struct {
    ID        string    `gorm:"primaryKey;size:64"`
    ActorID   string    `gorm:"index;size:64;not null"`
    // ...
    CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// 改造后 — 显式覆盖 CreatedAt 以保留 index 标签
type AuditEvent struct {
    ID      string `gorm:"primaryKey;size:64"`
    ActorID string `gorm:"index;size:64;not null"`
    // ...
    CreatedBy     string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
    CreatedAt     time.Time `gorm:"index" json:"created_at"`
}
```

或者额外声明一个带 `index` 标签的 `CreatedAt` 字段覆盖嵌入的字段。对于无特殊标签的审计表（如 `AuditRDPChannelEvent` 根本没有 `CreatedAt`），直接嵌入 `CreationAudit` 即可。

### 纯关联表

RolePermission, UserRole, UserGroupMember 不变，保持物理删除。

### Context 传递

admin server 的 `withAuthAndUser` 中间件已经在 context 中设置了用户 ID（key = `ctxKeyUserID` = `"admin_user_id"`）。GORM 操作需要在带有该 context 的 db session 上执行：

```go
db := s.gormDB.WithContext(ctx)  // ctx 包含 userID
db.Create(&model)  // BeforeCreate Hook 从中取出 userID
```

SSH server 等非 admin 入口创建资源时（如 Session 创建），需要在 context 中设置对应的用户 ID。

## 迁移策略

由于使用 GORM AutoMigrate：
1. 新增字段（`created_by`, `updated_by`, `deleted_at`）：AutoMigrate 自动添加列，默认值兼容现有数据
2. 已有 `created_at`/`updated_at` 字段：保留不变
3. 唯一索引重建：需要手动删除旧索引并创建包含 `deleted_at` 的新复合索引（不能靠 AutoMigrate 自动重命名）
4. 现有数据的 `deleted_at`：设默认值 `'0001-01-01 00:00:00'`，已物理删除的数据不需要处理

## 影响范围

| 层级 | 改动 |
|------|------|
| `internal/model/audit.go` | **新建**：FullAudit, CreationAudit, sentinel 常量, context key |
| `internal/model/*.go` | **修改**：~30 个模型文件，嵌入审计结构体，改造唯一索引标签 |
| `internal/store/*.go` | **修改**：所有 CRUD 查询增加 deleted_at 过滤，Delete 改为软删除 |
| `internal/server/admin/*.go` | **确认**：所有 GORM 调用通过 WithContext(ctx) 传递用户身份 |

## 不改变的部分

- 审计日志类表保持只追加，不支持删除
- 纯关联表保持物理删除
- EncryptionField 类型不变
- API 接口不变（deleted 记录默认不返回）
