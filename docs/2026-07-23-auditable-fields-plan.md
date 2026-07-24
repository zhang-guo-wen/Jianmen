# 标准化审计字段实施计划

> **历史计划（已归档）**：本文记录了重构早期的时间型 `deleted_at`
> 方案，不再作为实现依据。最终字段语义、单一迁移策略和验收标准以
> [标准化审计字段与停用标记设计](./2026-07-23-auditable-fields-design.md)
> 为准。
>
> **For agentic workers:** 使用 superpowers:subagent-driven-development 或 superpowers:executing-plans 按任务逐个实施。步骤使用 checkbox (`- [ ]`) 语法跟踪。

**目标:** 为所有数据表统一增加 created_at, created_by, updated_at, updated_by, deleted_at 标准化审计字段，实现软删除。

**架构:** 定义 FullAudit（业务表）和 CreationAudit（审计日志）两个可嵌入结构体，通过 GORM Hook 自动填充审计字段，在 Store 层统一处理软删除过滤和唯一索引迁移。

**技术栈:** Go 1.23+, GORM, MySQL

## 全局约束

- deleted_at 使用 sentinel 时间 `0001-01-01 00:00:00` 表示未删除（非 NULL）
- created_by / updated_by 存储用户 ID（64字符）
- 唯一索引必须包含 deleted_at 组成复合索引
- 审计日志表只加 created_by，不加 deleted_at
- 纯关联表（RolePermission, UserRole, UserGroupMember）不做任何改动
- 使用 git worktree 隔离开发

---

### Task 1: 创建 audit.go 基础结构体

**文件:**
- Create: `internal/model/audit.go`

**接口:**
- Produces:
  - `SentinelDeletedAt *time.Time` — 未删除标记时间常量
  - `CtxKeyUserID contextKey` — context key，值为 `"admin_user_id"`
  - `CreationAudit` 结构体（CreatedBy, CreatedAt + BeforeCreate Hook）
  - `FullAudit` 结构体（CreatedBy, UpdatedBy, CreatedAt, UpdatedAt, DeletedAt + BeforeCreate/BeforeUpdate Hooks）
  - `userIDFromContext(ctx)` 辅助函数

- [ ] **Step 1: 创建文件**

`internal/model/audit.go`:

```go
package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// SentinelDeletedAt 未删除（活跃）行的标记时间。
// 使用非 NULL 值以便与业务键组合成有效的复合唯一索引（MySQL 唯一索引中 NULL 值不参与等值比较）。
var SentinelDeletedAt = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)

// SentinelDeletedAtPtr 返回 SentinelDeletedAt 的指针。
func SentinelDeletedAtPtr() *time.Time {
	t := SentinelDeletedAt
	return &t
}

// contextKey 用于在 context 中存储/提取审计相关值。
type contextKey string

// CtxKeyUserID 与 admin server 的 ctxKeyUserID 保持一致的值。
const CtxKeyUserID contextKey = "admin_user_id"

// userIDFromContext 从 context 中提取当前用户 ID。
func userIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(CtxKeyUserID).(string); ok {
		return id
	}
	return ""
}

// CreationAudit 审计日志嵌入：仅创建信息。
// 适用于只追加不修改不删除的审计日志表。
type CreationAudit struct {
	CreatedBy string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

func (a *CreationAudit) BeforeCreate(tx *gorm.DB) error {
	a.CreatedBy = userIDFromContext(tx.Statement.Context)
	return nil
}

// FullAudit 业务表嵌入：完整的五个审计字段。
type FullAudit struct {
	CreatedBy string     `gorm:"index;size:64;not null;default:''" json:"created_by"`
	UpdatedBy string     `gorm:"index;size:64;not null;default:''" json:"updated_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index;not null;default:'0001-01-01 00:00:00'" json:"-"`
}

func (a *FullAudit) BeforeCreate(tx *gorm.DB) error {
	if a.DeletedAt == nil {
		a.DeletedAt = SentinelDeletedAtPtr()
	}
	userID := userIDFromContext(tx.Statement.Context)
	a.CreatedBy = userID
	a.UpdatedBy = userID
	return nil
}

func (a *FullAudit) BeforeUpdate(tx *gorm.DB) error {
	a.UpdatedBy = userIDFromContext(tx.Statement.Context)
	return nil
}
```

- [ ] **Step 2: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./internal/model/...
```

预期：编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/model/audit.go
git commit -m "feat: 新增 FullAudit 和 CreationAudit 审计字段基础结构体"
```

---

### Task 2: 改造核心业务模型（core.go + session.go + admin_session.go + websocket_ticket.go）

**文件:**
- Modify: `internal/model/core.go` — UserPublicKey, Role, Permission, Application（RolePermission, UserRole 不变）
- Modify: `internal/model/session.go` — User, Session
- Modify: `internal/model/admin_session.go` — AdminSession
- Modify: `internal/model/websocket_ticket.go` — WebSocketTicket

**接口:**
- Consumes: `FullAudit` from Task 1
- Produces: 改造后的模型结构体，嵌入 FullAudit，移除原有的 CreatedAt/UpdatedAt，改造唯一索引标签

**重要: BeforeCreate Hook 链式调用**

GORM 的 Hook 不会自动调用嵌入结构体的同名 Hook。如果模型嵌入 `FullAudit`（有 BeforeCreate），且模型本身也有 BeforeCreate（ensureID），GORM 只调用外层 BeforeCreate。必须显式链式调用：

```go
// 每个嵌入 FullAudit 且需要 ensureID 的模型都要用此模式
func (m *User) BeforeCreate(tx *gorm.DB) error {
	if err := m.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&m.ID)
}
```

此模式适用于**所有**后续 Task 中的业务模型 BeforeCreate hooks。

- [ ] **Step 1: 改造 core.go 中的模型**

`internal/model/core.go` 修改 `UserPublicKey`:
```go
type UserPublicKey struct {
	ID          string     `gorm:"primaryKey;size:64" json:"id"`
	UserID      string     `gorm:"index;index:idx_user_public_keys_user_revoked,priority:1;size:64;not null" json:"user_id"`
	Name        string     `gorm:"size:128" json:"name,omitempty"`
	PublicKey   string     `gorm:"type:text;not null" json:"public_key"`
	Fingerprint string     `gorm:"index;size:128" json:"fingerprint,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `gorm:"index;index:idx_user_public_keys_user_revoked,priority:2" json:"revoked_at,omitempty"`
	FullAudit
	User        User       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}
```

修改 `Role`:
```go
type Role struct {
	ID          string `gorm:"primaryKey;size:64" json:"id"`
	Name        string `gorm:"uniqueIndex:idx_roles_name_deleted,priority:1;size:128;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Builtin     bool   `json:"builtin"`
	Status      string `gorm:"size:32;not null;default:active" json:"status"`
	FullAudit
}
```

修改 `Permission`:
```go
type Permission struct {
	ID           string `gorm:"primaryKey;size:64" json:"id"`
	Name         string `gorm:"index;size:128" json:"name,omitempty"`
	Action       string `gorm:"index;index:idx_permissions_action_resource,priority:1;uniqueIndex:idx_permissions_logic_deleted,priority:1;size:128" json:"action"`
	ResourceType string `gorm:"index;index:idx_permissions_action_resource,priority:2;uniqueIndex:idx_permissions_logic_deleted,priority:2;size:64" json:"resource_type,omitempty"`
	ResourceID   string `gorm:"index;index:idx_permissions_action_resource,priority:3;uniqueIndex:idx_permissions_logic_deleted,priority:3;size:64" json:"resource_id,omitempty"`
	Effect       string `gorm:"index:idx_permissions_action_resource,priority:4;uniqueIndex:idx_permissions_logic_deleted,priority:4;size:16;not null;default:allow" json:"effect"`
	Description  string `gorm:"type:text" json:"description,omitempty"`
	FullAudit
}
```

修改 `Application`:
```go
type Application struct {
	ID             string `gorm:"primaryKey;size:64" json:"id"`
	Name           string `gorm:"size:255;not null" json:"name"`
	AppGroup       string `gorm:"size:128" json:"group"`
	ListenPort     int    `gorm:"uniqueIndex:idx_applications_listen_port_deleted,priority:1;not null" json:"listen_port"`
	Address        string `gorm:"size:2048;not null;default:''" json:"address"`
	EntryPath      string `gorm:"size:2048;not null;default:/" json:"entry_path"`
	InternalScheme string `gorm:"size:8;not null;default:http" json:"internal_scheme"`
	InternalHost   string `gorm:"size:255;not null" json:"internal_host"`
	InternalPort   int    `gorm:"not null;default:80" json:"internal_port"`
	Remark         string `gorm:"type:text" json:"remark,omitempty"`
	Status         string `gorm:"index;size:32;not null;default:active" json:"status"`
	FullAudit
}
```

移除所有不再需要的 BeforeCreate hooks（`UserPublicKey`, `Role`, `Permission`, `Application`），改为显式链式调用模式（参见本 Task 开头的重要说明）。例如：

```go
func (r *Role) BeforeCreate(tx *gorm.DB) error {
	if err := r.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&r.ID)
}
```

同样模式更新 `UserPublicKey`, `Permission`, `Application`。

- [ ] **Step 2: 改造 session.go 中的模型**

`internal/model/session.go` 修改 `User`:
```go
type User struct {
	ID              string     `gorm:"primaryKey;size:64" json:"id"`
	Username        string     `gorm:"uniqueIndex:idx_users_username_deleted,priority:1;index:idx_users_status_username,priority:2;size:128;not null" json:"username"`
	PasswordHash    string     `gorm:"size:255" json:"-"`
	MySQLNativeHash string     `gorm:"size:40" json:"-"`
	TokenHash       string     `gorm:"index;size:255" json:"-"`
	DisplayName     string     `gorm:"size:128" json:"display_name,omitempty"`
	Email           string     `gorm:"index;size:255" json:"email,omitempty"`
	Status          string     `gorm:"index:idx_users_status_username,priority:1;size:32;not null;default:active" json:"status"`
	IsSuperAdmin    bool       `gorm:"index;not null;default:false" json:"is_super_admin"`
	ExpiresAt       *time.Time `gorm:"index" json:"expires_at,omitempty"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	FullAudit

	RequestedTargetID string `gorm:"-" json:"-"`
}
```

修改 `Session`:
```go
type Session struct {
	ID              string      `gorm:"primaryKey;size:64" json:"id"`
	SID             string      `gorm:"index;size:128" json:"sid,omitempty"`
	UserID          string      `gorm:"index;index:idx_sessions_user_started,priority:1;size:64" json:"user_id,omitempty"`
	User            User        `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"-"`
	UserSessionID   string      `gorm:"index;size:64" json:"user_session_id,omitempty"`
	UserSession     UserSession `gorm:"foreignKey:UserSessionID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"-"`
	HostID          string      `gorm:"index;size:64" json:"host_id,omitempty"`
	AccountID       string      `gorm:"index;index:idx_sessions_account_started,priority:1;size:64" json:"account_id,omitempty"`
	TargetID        string      `gorm:"index;index:idx_sessions_target_started,priority:1;size:64" json:"target_id,omitempty"`
	Target          string      `gorm:"size:255" json:"target,omitempty"`
	Protocol        string      `gorm:"index;index:idx_sessions_protocol_started,priority:1;size:32" json:"protocol,omitempty"`
	ProtocolSubtype string      `gorm:"size:64" json:"protocol_subtype,omitempty"`
	UserUsername    string      `gorm:"size:128" json:"user_username,omitempty"`
	AccountUsername string      `gorm:"size:128" json:"account_username,omitempty"`
	HostIP          string      `gorm:"size:128" json:"host_ip,omitempty"`
	ConnIP          string      `gorm:"size:128" json:"conn_ip,omitempty"`
	ConnPort        int         `json:"conn_port,omitempty"`
	ClientIP        string      `gorm:"size:128" json:"client_ip,omitempty"`
	StartedAt       time.Time   `gorm:"index;index:idx_sessions_user_started,priority:2;index:idx_sessions_account_started,priority:2;index:idx_sessions_target_started,priority:2;index:idx_sessions_protocol_started,priority:2;index:idx_sessions_state_started,priority:2" json:"started_at"`
	EndedAt         *time.Time  `gorm:"index" json:"ended_at,omitempty"`
	State           string      `gorm:"index;index:idx_sessions_state_started,priority:1;size:32" json:"state,omitempty"`
	FullAudit
}
```

移除 User 和 Session 的原有 `BeforeCreate` hooks，改为显式链式调用：

```go
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if err := u.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&u.ID)
}

func (s *Session) BeforeCreate(tx *gorm.DB) error {
	if err := s.FullAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&s.ID)
}
```

- [ ] **Step 3: 改造 admin_session.go**

```go
type AdminSession struct {
	ID         string     `gorm:"primaryKey;size:64"`
	UserID     string     `gorm:"index;size:64;not null"`
	SecretHash string     `gorm:"uniqueIndex:idx_admin_sessions_secret_hash_deleted,priority:1;size:64;not null"`
	CSRFHash   string     `gorm:"size:64;not null"`
	ExpiresAt  time.Time  `gorm:"index;not null"`
	RevokedAt  *time.Time `gorm:"index"`
	FullAudit
}
```

- [ ] **Step 4: 改造 websocket_ticket.go**

```go
type WebSocketTicket struct {
	ID           string     `gorm:"primaryKey;size:64"`
	SessionID    string     `gorm:"index;size:64;not null"`
	Purpose      string     `gorm:"index;size:32;not null"`
	TargetID     string     `gorm:"index;size:64;not null"`
	ConnectionID string     `gorm:"index;size:64"`
	SecretHash   string     `gorm:"uniqueIndex:idx_websocket_tickets_secret_hash_deleted,priority:1;size:64;not null"`
	ExpiresAt    time.Time  `gorm:"index;not null"`
	ConsumedAt   *time.Time `gorm:"index"`
	FullAudit
	// 注意：原 WebSocketTicket 没有 UpdatedAt，FullAudit 会加入
}
```

- [ ] **Step 5: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./internal/model/...
```

预期：编译成功

- [ ] **Step 6: 提交**

```bash
git add internal/model/core.go internal/model/session.go internal/model/admin_session.go internal/model/websocket_ticket.go
git commit -m "feat: 核心业务模型嵌入 FullAudit 审计字段"
```

---

### Task 3: 改造资源类业务模型（resource.go, host.go, database.go, resource_grant.go, container.go）

**文件:**
- Modify: `internal/model/resource.go` — Resource, ResourceGroup
- Modify: `internal/model/host.go` — Host, HostAccount
- Modify: `internal/model/database.go` — DatabaseInstance, DatabaseAccount
- Modify: `internal/model/resource_grant.go` — ResourceGrant
- Modify: `internal/model/container.go` — ContainerEndpoint

**接口:**
- Consumes: `FullAudit` from Task 1
- Produces: 改造后的模型结构体

- [ ] **Step 1: 改造 resource.go**

`internal/model/resource.go`:

`Resource` 修改（移除 CreatedAt/UpdatedAt，嵌入 FullAudit）:
```go
type Resource struct {
	ID         string `gorm:"primaryKey;size:64" json:"id"`
	Type       string `gorm:"index;uniqueIndex:idx_resources_type_resource_id_deleted,priority:1;size:64;not null" json:"type"`
	ResourceID string `gorm:"uniqueIndex:idx_resources_type_resource_id_deleted,priority:2;size:64" json:"resource_id"`
	Name       string `gorm:"size:255" json:"name,omitempty"`
	ParentID   string `gorm:"index;size:64" json:"parent_id,omitempty"`
	FullAudit
}
```

`ResourceGroup` 修改:
```go
type ResourceGroup struct {
	ID          string `gorm:"primaryKey;size:64" json:"id"`
	Name        string `gorm:"uniqueIndex:idx_resource_groups_name_type_deleted,priority:1;size:128;not null" json:"name"`
	GroupType   string `gorm:"uniqueIndex:idx_resource_groups_name_type_deleted,priority:2;index;size:32;not null;default:resource" json:"group_type"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	FullAudit
}
```

移除 BeforeCreate hooks。

- [ ] **Step 2: 改造 host.go**

`Host` 修改:
```go
type Host struct {
	ID                 string `gorm:"primaryKey;size:64" json:"id"`
	Name               string `gorm:"size:255;not null" json:"name"`
	Address            string `gorm:"index;index:idx_hosts_address_port,priority:1;size:255;not null" json:"address"`
	Port               int    `gorm:"index:idx_hosts_address_port,priority:2;not null;default:22" json:"port"`
	Protocol           string `gorm:"index;size:16;not null;default:ssh" json:"protocol"`
	GroupName          string `gorm:"size:128" json:"group"`
	Remark             string `gorm:"type:text" json:"remark,omitempty"`
	Status             string `gorm:"index;size:32;not null;default:active" json:"status"`
	HostKeyFingerprint string `gorm:"size:128"`
	KnownHosts         string `gorm:"type:text"`
	FullAudit
}
```

`HostAccount` 修改（注意：`ResourceID` 有 uniqueIndex）:
```go
type HostAccount struct {
	ID                    string         `gorm:"primaryKey;size:64" json:"id"`
	HostID                string         `gorm:"index;index:idx_host_accounts_host_username,priority:1;index:idx_host_accounts_host_status,priority:1;size:64;not null" json:"host_id"`
	Name                  string         `gorm:"size:128;not null;default:''" json:"name"`
	Username              string         `gorm:"index:idx_host_accounts_host_username,priority:2;size:128;not null" json:"username"`
	Domain                string         `gorm:"size:255" json:"domain,omitempty"`
	AuthType              string         `gorm:"size:32" json:"auth_type,omitempty"`
	Password              EncryptedField `gorm:"type:text" json:"-"`
	PrivateKeyPEM         EncryptedField `gorm:"type:text" json:"-"`
	Passphrase            EncryptedField `gorm:"type:text" json:"-"`
	InsecureIgnoreHostKey bool           `gorm:"not null;default:false" json:"insecure_ignore_host_key"`
	HostKeyFingerprint    string         `gorm:"size:128" json:"host_key_fingerprint,omitempty"`
	KnownHostsPath        string         `gorm:"size:255" json:"known_hosts_path,omitempty"`
	RDPSecurity           string         `gorm:"size:32;not null;default:any" json:"rdp_security,omitempty"`
	RDPIgnoreCertificate  bool           `gorm:"not null;default:false" json:"rdp_ignore_certificate"`
	RDPCertFingerprints   string         `gorm:"type:text" json:"rdp_cert_fingerprints,omitempty"`
	RDPClipboardRead      bool           `gorm:"not null;default:false" json:"rdp_clipboard_read"`
	RDPClipboardWrite     bool           `gorm:"not null;default:false" json:"rdp_clipboard_write"`
	RDPFileUpload         bool           `gorm:"not null;default:false" json:"rdp_file_upload"`
	RDPFileDownload       bool           `gorm:"not null;default:false" json:"rdp_file_download"`
	RDPDriveMapping       bool           `gorm:"not null;default:false" json:"rdp_drive_mapping"`
	Status                string         `gorm:"index;index:idx_host_accounts_host_status,priority:2;index:idx_host_accounts_status_expires,priority:1;size:32;not null;default:active" json:"status"`
	ResourceSeq           int            `gorm:"index;not null;default:0" json:"resource_seq"`
	ResourceID            string         `gorm:"uniqueIndex:idx_host_accounts_resource_id_deleted,priority:1;size:4" json:"resource_id"`
	GroupName             string         `gorm:"size:128" json:"group"`
	Remark                string         `gorm:"type:text" json:"remark,omitempty"`
	ExpiresAt             *time.Time     `gorm:"index;index:idx_host_accounts_status_expires,priority:2" json:"expires_at"`
	FullAudit
	Host                  Host           `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}
```

移除 BeforeCreate hooks。

- [ ] **Step 3: 改造 database.go**

`DatabaseInstance` 修改:
```go
type DatabaseInstance struct {
	ID            string `gorm:"primaryKey;size:64" json:"id"`
	Name          string `gorm:"uniqueIndex:idx_database_instances_name_deleted,priority:1;size:255;not null" json:"name"`
	Protocol      string `gorm:"index:idx_database_instances_endpoint,priority:1;size:32;not null;default:mysql" json:"protocol"`
	Address       string `gorm:"index:idx_database_instances_endpoint,priority:2;size:255;not null" json:"address"`
	Port          int    `gorm:"index:idx_database_instances_endpoint,priority:3;not null;default:3306" json:"port"`
	TLSMode       string `gorm:"size:16;not null;default:disable" json:"tls_mode"`
	TLSServerName string `gorm:"size:255" json:"tls_server_name,omitempty"`
	TLSCAPEM      string `gorm:"column:tls_ca_pem;type:text" json:"-"`
	GroupName     string `gorm:"size:128" json:"group"`
	Remark        string `gorm:"type:text" json:"remark,omitempty"`
	Status        string `gorm:"index;size:32;not null;default:active" json:"status"`
	FullAudit
}
```

`DatabaseAccount` 修改（注意多个唯一索引）:
```go
type DatabaseAccount struct {
	ID                      string                         `gorm:"primaryKey;size:64" json:"id"`
	InstanceID              string                         `gorm:"index;index:idx_database_accounts_instance_username,priority:1;uniqueIndex:idx_dba_instance_username_deleted,priority:1;index:idx_database_accounts_instance_status,priority:1;size:64;not null" json:"instance_id"`
	UniqueName              string                         `gorm:"uniqueIndex:idx_dba_unique_name_deleted,priority:1;size:128;not null" json:"unique_name"`
	Username                string                         `gorm:"index:idx_database_accounts_instance_username,priority:2;uniqueIndex:idx_dba_instance_username_deleted,priority:2;size:128;not null" json:"username"`
	Password                EncryptedField                 `gorm:"type:text" json:"-"`
	Managed                 bool                           `gorm:"index:idx_database_accounts_managed_status,priority:1;not null;default:false;check:chk_database_accounts_managed_consistency,((managed = false AND upstream_host = '' AND provisioning_operation_id IS NULL) OR (managed = true AND upstream_host <> '' AND provisioning_operation_id IS NOT NULL))" json:"-"`
	UpstreamHost            string                         `gorm:"size:255;not null;default:''" json:"-"`
	GroupName               string                         `gorm:"size:128" json:"group"`
	Remark                  string                         `gorm:"type:text" json:"remark,omitempty"`
	ExpiresAt               *time.Time                     `gorm:"index;index:idx_database_accounts_status_expires,priority:2" json:"expires_at,omitempty"`
	Status                  string                         `gorm:"index;index:idx_database_accounts_instance_status,priority:2;index:idx_database_accounts_managed_status,priority:2;index:idx_database_accounts_status_expires,priority:1;size:32;not null;default:active" json:"status"`
	ResourceSeq             int                            `gorm:"index;not null;default:0" json:"resource_seq"`
	ResourceID              string                         `gorm:"uniqueIndex:idx_dba_resource_id_deleted,priority:1;size:4" json:"resource_id"`
	ProvisioningOperationID *string                        `gorm:"uniqueIndex:idx_dba_prov_op_deleted,priority:1;size:64" json:"-"`
	FullAudit
	Instance                DatabaseInstance               `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	ProvisioningOperation   *DatabaseProvisioningOperation `gorm:"foreignKey:ProvisioningOperationID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;" json:"-"`
}
```

移除 BeforeCreate hooks 和 BeforeDelete hook（DatabaseInstance.BeforeDelete 是与业务逻辑相关的检查函数，保留但重命名为独立方法）。

等一等 — `DatabaseInstance.BeforeDelete` 是业务逻辑（检查 pending provisioning operations），不是审计相关。它需要保留但改为在 Store 层软删除前检查。

- [ ] **Step 4: 改造 resource_grant.go**

`ResourceGrant` 已有 CreatedBy，改为嵌入 FullAudit 并移除重复字段:
```go
type ResourceGrant struct {
	ID            string     `gorm:"primaryKey;size:64" json:"id"`
	PrincipalType string     `gorm:"index;index:idx_resource_grants_principal,priority:1;uniqueIndex:idx_rg_logic_deleted,priority:1;size:32;not null" json:"principal_type"`
	PrincipalID   string     `gorm:"index;index:idx_resource_grants_principal,priority:2;uniqueIndex:idx_rg_logic_deleted,priority:2;size:64;not null" json:"principal_id"`
	ResourceType  string     `gorm:"index;index:idx_resource_grants_resource,priority:1;uniqueIndex:idx_rg_logic_deleted,priority:3;size:64;not null" json:"resource_type"`
	ResourceID    string     `gorm:"index;index:idx_resource_grants_resource,priority:2;uniqueIndex:idx_rg_logic_deleted,priority:4;size:64;not null" json:"resource_id"`
	Effect        string     `gorm:"index;uniqueIndex:idx_rg_logic_deleted,priority:5;size:16;not null;default:allow" json:"effect"`
	ExpiresAt     *time.Time `gorm:"index" json:"expires_at,omitempty"`
	FullAudit
}
```

移除 `CreatedBy string` 字段和 BeforeCreate hook（FullAudit 接管 CreatedBy）。

- [ ] **Step 5: 改造 container.go**

```go
type ContainerEndpoint struct {
	ID             string `gorm:"primaryKey;size:64" json:"id"`
	Name           string `gorm:"size:255;not null" json:"name"`
	GroupName      string `gorm:"size:128" json:"group"`
	Runtime        string `gorm:"index;size:32;not null" json:"runtime"`
	ConnectionMode string `gorm:"index;size:32;not null" json:"connection_mode"`
	Address        string `gorm:"size:2048;not null" json:"address"`
	Port           int    `gorm:"not null;default:0" json:"port"`
	HostID         string `gorm:"index;size:64" json:"host_id,omitempty"`
	HostAccountID  string `gorm:"index;size:64" json:"host_account_id,omitempty"`
	Remark         string `gorm:"type:text" json:"remark,omitempty"`
	Status         string `gorm:"index;size:32;not null;default:active" json:"status"`
	FullAudit
}
```

移除 BeforeCreate hook。

- [ ] **Step 6: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./internal/model/...
```

- [ ] **Step 7: 提交**

```bash
git add internal/model/resource.go internal/model/host.go internal/model/database.go internal/model/resource_grant.go internal/model/container.go
git commit -m "feat: 资源类业务模型嵌入 FullAudit 审计字段"
```

---

### Task 4: 改造会话与访问控制模型（user_session.go, temporary_access.go, ai_access_token.go, connection_password.go, platform_account.go）

**文件:**
- Modify: `internal/model/user_session.go` — UserSession
- Modify: `internal/model/temporary_access.go` — TemporaryAccount, TemporaryCredential, TemporaryAccountGrant
- Modify: `internal/model/ai_access_token.go` — AIAccessToken
- Modify: `internal/model/connection_password.go` — ConnectionPassword
- Modify: `internal/model/platform_account.go` — PlatformAccount

**接口:**
- Consumes: `FullAudit` from Task 1
- Produces: 改造后的模型结构体

- [ ] **Step 1: 改造 user_session.go**

```go
type UserSession struct {
	ID         string     `gorm:"primaryKey;size:64" json:"id"`
	UserID     string     `gorm:"index;index:idx_user_sessions_user_type_status,priority:1;size:64;not null" json:"user_id"`
	User       User       `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	SessionSeq int        `gorm:"not null;uniqueIndex:idx_user_sessions_session_seq_deleted,priority:1" json:"session_seq"`
	SessionID  string     `gorm:"size:5;not null;uniqueIndex:idx_user_sessions_session_id_deleted,priority:1" json:"session_id"`
	Type       string     `gorm:"size:16;not null;default:permanent;index:idx_user_sessions_user_type_status,priority:2" json:"type"`
	Status     string     `gorm:"size:16;not null;default:active;index:idx_user_sessions_user_type_status,priority:3" json:"status"`
	ExpiresAt  *time.Time `gorm:"index" json:"expires_at,omitempty"`
	FullAudit
	// 注意：原 UserSession.CreatedBy 被 FullAudit.CreatedBy 替代
}
```

移除独立的 `CreatedBy string` 字段和 `BeforeCreate` hook。

- [ ] **Step 2: 改造 temporary_access.go**

`TemporaryAccount` 修改:
```go
type TemporaryAccount struct {
	ID               string     `gorm:"primaryKey;size:64" json:"id"`
	SessionID        string     `gorm:"uniqueIndex:idx_temp_accounts_session_id_deleted,priority:1;size:128;not null" json:"session_id"`
	Type             string     `gorm:"index;size:32;not null" json:"type"`
	Username         string     `gorm:"uniqueIndex:idx_temp_accounts_username_deleted,priority:1;size:128;not null" json:"username"`
	AuthorizedUserID string     `gorm:"index;size:64" json:"authorized_user_id,omitempty"`
	Status           string     `gorm:"index;size:32;not null;default:active" json:"status"`
	StartsAt         time.Time  `gorm:"index" json:"starts_at"`
	ExpiresAt        *time.Time `gorm:"index" json:"expires_at,omitempty"`
	Remark           string     `gorm:"type:text" json:"remark,omitempty"`
	FullAudit
}
```

移除独立的 `CreatedBy string` 字段和 `BeforeCreate` hook。

`TemporaryCredential` 修改:
```go
type TemporaryCredential struct {
	ID                 string           `gorm:"primaryKey;size:64" json:"id"`
	TemporaryAccountID string           `gorm:"index;size:64;not null" json:"temporary_account_id"`
	Type               string           `gorm:"size:32;not null" json:"type"`
	PublicKey          string           `gorm:"type:text" json:"public_key,omitempty"`
	SecretHash         string           `gorm:"size:255" json:"-"`
	Fingerprint        string           `gorm:"index;size:128" json:"fingerprint,omitempty"`
	ExpiresAt          *time.Time       `gorm:"index;index:idx_temporary_credentials_validity,priority:2" json:"expires_at,omitempty"`
	RevokedAt          *time.Time       `gorm:"index;index:idx_temporary_credentials_validity,priority:1" json:"revoked_at,omitempty"`
	FullAudit
	TemporaryAccount   TemporaryAccount `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}
```

`TemporaryAccountGrant` 修改:
```go
type TemporaryAccountGrant struct {
	ID                 string           `gorm:"primaryKey;size:64" json:"id"`
	TemporaryAccountID string           `gorm:"index;size:64;not null" json:"temporary_account_id"`
	UserID             string           `gorm:"index;index:idx_temporary_grants_match,priority:1;size:64" json:"user_id,omitempty"`
	Action             string           `gorm:"index;index:idx_temporary_grants_match,priority:2;size:128" json:"action,omitempty"`
	ResourceType       string           `gorm:"index;index:idx_temporary_grants_match,priority:3;size:64" json:"resource_type,omitempty"`
	ResourceID         string           `gorm:"index;index:idx_temporary_grants_match,priority:4;size:64" json:"resource_id,omitempty"`
	StartsAt           *time.Time       `gorm:"index" json:"starts_at,omitempty"`
	ExpiresAt          *time.Time       `gorm:"index;index:idx_temporary_grants_match,priority:5" json:"expires_at,omitempty"`
	RevokedAt          *time.Time       `gorm:"index;index:idx_temporary_grants_match,priority:6" json:"revoked_at,omitempty"`
	FullAudit
	TemporaryAccount   TemporaryAccount `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}
```

移除独立的 `CreatedBy string` 字段和 `BeforeCreate` hook。

- [ ] **Step 3: 改造 ai_access_token.go**

```go
type AIAccessToken struct {
	ID                 string           `gorm:"primaryKey;size:64" json:"id"`
	UserID             string           `gorm:"index;size:64;not null" json:"user_id"`
	TemporaryAccountID string           `gorm:"index;size:64" json:"temporary_account_id,omitempty"`
	Name               string           `gorm:"size:128;not null" json:"name"`
	AccessTokenHash    string           `gorm:"uniqueIndex:idx_ai_tokens_access_hash_deleted,priority:1;size:64;not null" json:"-"`
	RefreshTokenHash   string           `gorm:"uniqueIndex:idx_ai_tokens_refresh_hash_deleted,priority:1;size:64;not null" json:"-"`
	AccessExpiresAt    time.Time        `gorm:"index;not null" json:"access_expires_at"`
	RefreshExpiresAt   time.Time        `gorm:"index;not null" json:"refresh_expires_at"`
	LastUsedAt         *time.Time       `json:"last_used_at,omitempty"`
	RevokedAt          *time.Time       `gorm:"index" json:"revoked_at,omitempty"`
	FullAudit
	User               User             `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	TemporaryAccount   TemporaryAccount `gorm:"foreignKey:TemporaryAccountID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"-"`
}
```

移除 BeforeCreate hook。

- [ ] **Step 4: 改造 connection_password.go**

```go
type ConnectionPassword struct {
	ID                 string     `gorm:"primaryKey;size:64"`
	UserID             string     `gorm:"index:idx_connection_passwords_lookup,priority:1;size:64;not null"`
	ResourceType       string     `gorm:"index:idx_connection_passwords_lookup,priority:2;size:64;not null"`
	ResourceID         string     `gorm:"index:idx_connection_passwords_lookup,priority:3;size:64;not null"`
	TemporaryAccountID string     `gorm:"index;size:64"`
	SecretHash         string     `gorm:"size:255;not null"`
	MySQLNativeHash    string     `gorm:"size:40"`
	ExpiresAt          time.Time  `gorm:"index:idx_connection_passwords_lookup,priority:4;not null"`
	RevokedAt          *time.Time `gorm:"index"`
	FullAudit
}
```

注意: 原 ConnectionPassword 没有 json 标签，保持原样。移除 BeforeCreate hook。

- [ ] **Step 5: 改造 platform_account.go**

```go
type PlatformAccount struct {
	ID           string         `gorm:"primaryKey;size:64" json:"id"`
	Name         string         `gorm:"size:255" json:"name"`
	PlatformName string         `gorm:"index;size:128;not null" json:"platform_name"`
	URL          string         `gorm:"size:512" json:"url,omitempty"`
	GroupName    string         `gorm:"size:128" json:"group,omitempty"`
	Username     string         `gorm:"size:255;not null" json:"username"`
	Password     EncryptedField `gorm:"type:text" json:"-"`
	HasPassword  bool           `gorm:"->;-:migration" json:"-"`
	Remark       string         `gorm:"type:text" json:"remark,omitempty"`
	OwnerID      string         `gorm:"index;size:64;not null" json:"owner_id"`
	Status       string         `gorm:"index;size:32;not null;default:active" json:"status"`
	ExpiresAt    *time.Time     `gorm:"index" json:"expires_at,omitempty"`
	FullAudit
	Owner        User           `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
}
```

移除 BeforeCreate hook。

- [ ] **Step 6: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./internal/model/...
```

- [ ] **Step 7: 提交**

```bash
git add internal/model/user_session.go internal/model/temporary_access.go internal/model/ai_access_token.go internal/model/connection_password.go internal/model/platform_account.go
git commit -m "feat: 会话与访问控制模型嵌入 FullAudit 审计字段"
```

---

### Task 5: 改造设置与基础设施模型（system_setting.go, system_setting_revision.go, system_initialization.go, user_preference.go, user_group.go, resource_sequence.go, database_provisioning_operation.go）

**文件:**
- Modify: `internal/model/system_setting.go` — SystemSetting
- Modify: `internal/model/system_setting_revision.go` — SystemSettingRevision
- Modify: `internal/model/system_initialization.go` — SystemInitialization
- Modify: `internal/model/user_preference.go` — UserPreference
- Modify: `internal/model/user_group.go` — UserGroup（UserGroupMember 不变：纯关联表）
- Modify: `internal/model/resource_sequence.go` — ResourceSequence
- Modify: `internal/model/database_provisioning_operation.go` — DatabaseProvisioningOperation

**接口:**
- Consumes: `FullAudit` from Task 1
- Produces: 改造后的模型结构体

- [ ] **Step 1: 改造 system_setting.go**

`SystemSetting` 已有 `UpdatedByID` / `UpdatedByUsername`，`FullAudit` 会加入标准字段。为避免冲突，移除旧的 `UpdatedByID`、`UpdatedByUsername`、`CreatedAt`、`UpdatedAt`，改用 `FullAudit`:

```go
type SystemSetting struct {
	ID                            string `gorm:"primaryKey;size:32"`
	DatabaseGatewayMode           string `gorm:"size:16;not null;default:unified;check:chk_system_settings_database_gateway_mode,database_gateway_mode IN ('unified','independent')"`
	DatabaseGatewayClientTLSMode  string `gorm:"size:16;not null;default:optional;check:chk_system_settings_database_gateway_client_tls_mode,database_gateway_client_tls_mode IN ('required','optional')"`
	WebRDPEnabled                 bool   `gorm:"not null"`
	WebRDPConnectTimeoutSeconds   int    `gorm:"not null"`
	WebRDPAllowUnrecorded         bool   `gorm:"not null"`
	RecordingEnabled              bool   `gorm:"not null"`
	RecordingRecordInput          bool   `gorm:"not null"`
	RecordingRecordCommands       bool   `gorm:"not null"`
	RecordingRetentionDays        int    `gorm:"not null"`
	RecordingMaxReplayBytes       int64  `gorm:"not null"`
	RecordingCleanupBatchSize     int    `gorm:"not null"`
	DatabaseMaxClientMessageBytes int    `gorm:"not null;default:10485760"`
	Revision                      int64  `gorm:"not null"`
	AppliedRevision               int64  `gorm:"not null;default:0"`
	AppliedAt                     *time.Time
	FullAudit
}
```

**注意**: `SystemSetting` 原来使用 `UpdatedByID` 和 `UpdatedByUsername` 作为审计字段。改造后统一为 `FullAudit` 中的 `UpdatedBy`（存用户 ID），原来的 `UpdatedByUsername` 被移除。所有引用 `UpdatedByID` 和 `UpdatedByUsername` 的代码需要更新。

检查所有引用:
```bash
cd C:/02-codespace/Jianmen && rg "UpdatedByID|UpdatedByUsername" --glob '*.go'
```

需更新的文件会在后续 store 层任务中处理。

- [ ] **Step 2: 改造 system_setting_revision.go**

```go
type SystemSettingRevision struct {
	ID                string    `gorm:"primaryKey;size:64"`
	Revision          int64     `gorm:"uniqueIndex:idx_ss_rev_revision_deleted,priority:1;not null"`
	SnapshotJSON      string    `gorm:"type:text;not null"`
	ChangedFieldsJSON string    `gorm:"type:text;not null"`
	FullAudit
}
```

移除 `UpdatedByID`/`UpdatedByUsername` 和 `BeforeCreate` hook。`CreatedAt` 的 index 标签在 Audit 字段中保留。

- [ ] **Step 3: 改造 system_initialization.go**

```go
type SystemInitialization struct {
	Key       string `gorm:"primaryKey;size:64"`
	FullAudit
}
```

移除独立的 `CreatedAt`。

- [ ] **Step 4: 改造 user_preference.go**

```go
type UserPreference struct {
	UserID             string `gorm:"primaryKey;size:64"`
	Theme              string `gorm:"size:32;not null;default:light"`
	SSHClient          string `gorm:"size:32"`
	SSHClientPath      string `gorm:"size:512"`
	SSHClientPlatform  string `gorm:"size:32;not null;default:windows"`
	DBClient           string `gorm:"size:32"`
	DBClientPlatform   string `gorm:"size:32;not null;default:windows"`
	DBClientPath       string `gorm:"size:512"`
	DBClientCAFilePath string `gorm:"size:512"`
	TerminalFontFamily string `gorm:"size:128"`
	TerminalFontSize   int    `gorm:"not null;default:14"`
	FullAudit
}
```

移除独立的 `CreatedAt`/`UpdatedAt`。

- [ ] **Step 5: 改造 user_group.go**

```go
type UserGroup struct {
	ID          string `gorm:"primaryKey;size:64" json:"id"`
	Name        string `gorm:"uniqueIndex:idx_user_groups_name_deleted,priority:1;size:128;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	FullAudit
}
```

`UserGroupMember` 不变（纯关联表）。

移除 `BeforeCreate` hook。

- [ ] **Step 6: 改造 resource_sequence.go**

```go
type ResourceSequence struct {
	Name      string `gorm:"primaryKey;size:128" json:"name"`
	NextValue int    `gorm:"not null;default:1" json:"next_value"`
	FullAudit
}
```

移除独立的 `CreatedAt`/`UpdatedAt`。

- [ ] **Step 7: 改造 database_provisioning_operation.go**

```go
type DatabaseProvisioningOperation struct {
	ID                   string            `gorm:"primaryKey;size:64" json:"-"`
	Kind                 string            `gorm:"uniqueIndex:idx_dpo_actor_kind_idem_deleted,priority:2;index:idx_database_provisioning_kind_stage,priority:1;size:32;not null;default:create;check:chk_database_provisioning_kind,kind IN ('create','deprovision')" json:"-"`
	InstanceID           string            `gorm:"index:idx_database_provisioning_instance;size:64;not null" json:"-"`
	ActorID              string            `gorm:"uniqueIndex:idx_dpo_actor_kind_idem_deleted,priority:1;size:64;not null;default:''" json:"-"`
	IdempotencyKey       *string           `gorm:"uniqueIndex:idx_dpo_actor_kind_idem_deleted,priority:3;size:128;check:chk_database_provisioning_idempotency_key,idempotency_key IS NULL OR length(trim(idempotency_key)) > 0" json:"-"`
	CanonicalRequestHash string            `gorm:"size:64;not null;default:''" json:"-"`
	AdminAccountID       string            `gorm:"index;size:64;not null" json:"-"`
	UpstreamUsername     string            `gorm:"uniqueIndex:idx_dpo_upstream_username_deleted,priority:1;size:32;not null" json:"-"`
	// ... 其余字段不变 ...
	CleanupStatus        string            `gorm:"index:idx_database_provisioning_work,priority:2;size:32;not null;default:none;check:..." json:"-"`
	TerminalAt           *time.Time        `gorm:"index" json:"-"`
	ActiveRetainedAt     *time.Time        `gorm:"index" json:"-"`
	LastError            string            `gorm:"size:64;not null;default:''" json:"-"`
	AttemptCount         int               `gorm:"not null;default:0;check:..." json:"-"`
	LastAttemptAt        *time.Time        `gorm:"index" json:"-"`
	Revision             int64             `gorm:"not null;default:1;check:..." json:"-"`
	LeaseOwner           string            `gorm:"size:64;not null;default:''" json:"-"`
	LeaseToken           string            `gorm:"size:64;not null;default:''" json:"-"`
	LeaseExpiresAt       *time.Time        `gorm:"index:idx_database_provisioning_work,priority:3" json:"-"`
	FullAudit
	Instance             *DatabaseInstance `gorm:"foreignKey:InstanceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;" json:"-"`
}
```

移除独立的 `CreatedAt`/`UpdatedAt`（FullAudit 接管）。

- [ ] **Step 8: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./internal/model/...
```

- [ ] **Step 9: 提交**

```bash
git add internal/model/system_setting.go internal/model/system_setting_revision.go internal/model/system_initialization.go internal/model/user_preference.go internal/model/user_group.go internal/model/resource_sequence.go internal/model/database_provisioning_operation.go
git commit -m "feat: 设置与基础设施模型嵌入 FullAudit 审计字段"
```

---

### Task 6: 改造审计日志模型（8 个审计表，嵌入 CreationAudit）

**文件:**
- Modify: `internal/model/audit_event.go` — AuditEvent
- Modify: `internal/model/login_audit.go` — LoginAuditLog
- Modify: `internal/model/audit_session.go` — AuditSession
- Modify: `internal/model/audit_artifact.go` — AuditArtifact
- Modify: `internal/model/audit_rdp_channel_event.go` — AuditRDPChannelEvent
- Modify: `internal/model/audit_ssh_command.go` — AuditSSHCommand
- Modify: `internal/model/audit_db_query.go` — AuditDBQuery
- Modify: `internal/model/audit_sftp_event.go` — AuditSFTPEvent

**接口:**
- Consumes: `CreationAudit` from Task 1
- Produces: 所有审计模型嵌入 `CreationAudit`，加入 `created_by`

- [ ] **Step 1: 逐个改造**

**关键规则**: 如果审计表已显式定义了 `CreatedAt` 字段，就不能嵌入 `CreationAudit`（Go 不允许同名字段）。改用显式添加 `CreatedBy` 字段并修改已有的 `BeforeCreate` Hook。

**已有 CreatedAt 的审计表（AuditEvent, LoginAuditLog, AuditSession, AuditArtifact）**：只加 `CreatedBy` 字段 + 更新 BeforeCreate。

`audit_event.go`:
```go
type AuditEvent struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	ActorID       string    `gorm:"index;size:64;not null" json:"actor_id"`
	ActorUsername string    `gorm:"index;size:128" json:"actor_username"`
	Action        string    `gorm:"index;idx_audit_events_resource,priority:1;size:64;not null" json:"action"`
	ResourceType  string    `gorm:"index;idx_audit_events_resource,priority:2;size:64;not null" json:"resource_type"`
	ResourceID    string    `gorm:"index;idx_audit_events_resource,priority:3;size:64" json:"resource_id,omitempty"`
	ResourceName  string    `gorm:"size:255" json:"resource_name,omitempty"`
	Detail        string    `gorm:"type:text" json:"detail,omitempty"`
	ClientIP      string    `gorm:"size:64" json:"client_ip,omitempty"`
	CreatedBy     string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
}

func (a *AuditEvent) BeforeCreate(tx *gorm.DB) error {
	a.CreatedBy = userIDFromContext(tx.Statement.Context)
	return ensureID(&a.ID)
}
```

`login_audit.go`:
```go
type LoginAuditLog struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	UserID    string    `gorm:"index;size:64" json:"user_id,omitempty"`
	Username  string    `gorm:"index;size:128;not null" json:"username"`
	Outcome   string    `gorm:"index;size:32;not null" json:"outcome"`
	Reason    string    `gorm:"size:128" json:"reason,omitempty"`
	ClientIP  string    `gorm:"index;size:128" json:"client_ip"`
	UserAgent string    `gorm:"size:512" json:"user_agent,omitempty"`
	CreatedBy string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

func (l *LoginAuditLog) BeforeCreate(tx *gorm.DB) error {
	l.CreatedBy = userIDFromContext(tx.Statement.Context)
	return ensureID(&l.ID)
}
```

`audit_session.go` — 已有 CreatedAt 和 UpdatedAt，加 CreatedBy:
```go
// 在原有字段基础上加:
CreatedBy string    `gorm:"index;size:64;not null;default:''" json:"created_by"`
// CreatedAt, UpdatedAt 保留原有定义
// 更新 BeforeCreate hook 增加 CreatedBy 赋值
```

`audit_artifact.go` — 同样方式添加 CreatedBy。

**无 CreatedAt 的审计表（AuditRDPChannelEvent, AuditSSHCommand, AuditDBQuery, AuditSFTPEvent）**：直接嵌入 `CreationAudit` 获得 CreatedBy + CreatedAt（含 index 标签）。同时由于嵌入会与原有的 `BeforeCreate` (ensureID) 冲突，需改为显式链式调用：

`audit_rdp_channel_event.go`:
```go
type AuditRDPChannelEvent struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string    `gorm:"index;size:64;not null" json:"audit_session_id"`
	Timestamp      time.Time `gorm:"index;not null" json:"timestamp"`
	Channel        string    `gorm:"index;size:32;not null" json:"channel"`
	Direction      string    `gorm:"size:32" json:"direction,omitempty"`
	Operation      string    `gorm:"size:32" json:"operation,omitempty"`
	Bytes          int64     `json:"bytes,omitempty"`
	Outcome        string    `gorm:"size:32;not null" json:"outcome"`
	Reason         string    `gorm:"size:255" json:"reason,omitempty"`
	CreationAudit  // 提供 CreatedBy + CreatedAt + BeforeCreate
}

// BeforeCreate 链式调用嵌入的 CreationAudit.BeforeCreate
func (e *AuditRDPChannelEvent) BeforeCreate(tx *gorm.DB) error {
	if err := e.CreationAudit.BeforeCreate(tx); err != nil {
		return err
	}
	return ensureID(&e.ID)
}
```

同样模式应用于 `AuditSSHCommand`、`AuditDBQuery`、`AuditSFTPEvent`。

- [ ] **Step 2: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./internal/model/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/model/audit_event.go internal/model/login_audit.go internal/model/audit_session.go internal/model/audit_artifact.go internal/model/audit_rdp_channel_event.go internal/model/audit_ssh_command.go internal/model/audit_db_query.go internal/model/audit_sftp_event.go
git commit -m "feat: 审计日志模型加入 created_by 审计字段"
```

---

### Task 7: 更新 AllModels() 和全局编译

**文件:**
- Modify: `internal/model/core.go` — AllModels(), BeforeCreate hooks

**接口:**
- Consumes: 所有改造后的模型 from Tasks 2-6

- [ ] **Step 1: 更新 AllModels() 中的注册**

确认 `AllModels()` 返回所有改造后的模型，无遗漏。

- [ ] **Step 2: 移除不再需要的 BeforeCreate hooks**

检查所有文件中被 FullAudit 替换的独立 BeforeCreate hooks 是否已移除：

```bash
cd C:/02-codespace/Jianmen && rg "func.*BeforeCreate.*\*gorm\.DB.*error" --glob '*.go' internal/model/
```

只应保留：
- `FullAudit.BeforeCreate`
- `CreationAudit.BeforeCreate`
- 审计日志表的 BeforeCreate（用于设置 created_by + ensureID）
- 其他带 `ensureID` 的业务表 BeforeCreate（需更新为显式调用 ensureID，因为 FullAudit 不处理 ID）

对于需要 `ensureID` 的模型（如 `AdminSession`, `WebSocketTicket` 等），原来独立的 `BeforeCreate` 被移除后，需要在结构体上添加独立的 `BeforeCreate` 只处理 ID:

```go
func (m *AdminSession) BeforeCreate(tx *gorm.DB) error {
    if m.ID == "" {
        m.ID = NewID()
    }
    return nil
}
```

FullAudit 的 BeforeCreate 会自动被 GORM 调用，但每个模型最多一个 BeforeCreate hook。Go 结构体嵌入的 hook 会被继承，所以不需要显式声明。

- [ ] **Step 3: 全项目编译**

```bash
cd C:/02-codespace/Jianmen && go build ./...
```

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "feat: 更新 AllModels 注册，清理冗余 BeforeCreate hooks"
```

---

### Task 8: 创建唯一索引迁移

**文件:**
- Create: `internal/store/migration_audit_indexes.go`
- Modify: `internal/store/dbstore.go` — 添加迁移调用

**接口:**
- Consumes: `FullAudit` from Task 1, `DBStore` from existing code
- Produces: `migrateUniqueIndexes(db *gorm.DB) error` — 幂等的索引迁移函数

- [ ] **Step 1: 编写迁移函数**

`internal/store/migration_audit_indexes.go`:

```go
package store

import (
	"fmt"

	"gorm.io/gorm"
)

// uniqueIndexMigration 定义一次索引迁移：删除旧索引，创建新复合索引。
type uniqueIndexMigration struct {
	table      string   // 表名
	indexName  string   // 新索引名
	columns    []string // 新索引列（包含 deleted_at）
}

// allIndexMigrations 列出所有需要重建的唯一索引。
func allIndexMigrations() []uniqueIndexMigration {
	return []uniqueIndexMigration{
		// core 模型
		{table: "users", indexName: "idx_users_username_deleted", columns: []string{"username", "deleted_at"}},
		{table: "roles", indexName: "idx_roles_name_deleted", columns: []string{"name", "deleted_at"}},
		{table: "permissions", indexName: "idx_permissions_logic_deleted", columns: []string{"action", "resource_type", "resource_id", "effect", "deleted_at"}},
		{table: "applications", indexName: "idx_applications_listen_port_deleted", columns: []string{"listen_port", "deleted_at"}},
		{table: "admin_sessions", indexName: "idx_admin_sessions_secret_hash_deleted", columns: []string{"secret_hash", "deleted_at"}},
		{table: "websocket_tickets", indexName: "idx_websocket_tickets_secret_hash_deleted", columns: []string{"secret_hash", "deleted_at"}},

		// 资源模型
		{table: "resources", indexName: "idx_resources_type_resource_id_deleted", columns: []string{"type", "resource_id", "deleted_at"}},
		{table: "resource_groups", indexName: "idx_resource_groups_name_type_deleted", columns: []string{"name", "group_type", "deleted_at"}},
		{table: "host_accounts", indexName: "idx_host_accounts_resource_id_deleted", columns: []string{"resource_id", "deleted_at"}},
		{table: "database_instances", indexName: "idx_database_instances_name_deleted", columns: []string{"name", "deleted_at"}},
		{table: "database_accounts", indexName: "idx_dba_instance_username_deleted", columns: []string{"instance_id", "username", "deleted_at"}},
		{table: "database_accounts", indexName: "idx_dba_unique_name_deleted", columns: []string{"unique_name", "deleted_at"}},
		{table: "database_accounts", indexName: "idx_dba_resource_id_deleted", columns: []string{"resource_id", "deleted_at"}},
		{table: "database_accounts", indexName: "idx_dba_prov_op_deleted", columns: []string{"provisioning_operation_id", "deleted_at"}},
		{table: "resource_grants", indexName: "idx_rg_logic_deleted", columns: []string{"principal_type", "principal_id", "resource_type", "resource_id", "effect", "deleted_at"}},

		// 会话/访问模型
		{table: "user_sessions", indexName: "idx_user_sessions_session_seq_deleted", columns: []string{"session_seq", "deleted_at"}},
		{table: "user_sessions", indexName: "idx_user_sessions_session_id_deleted", columns: []string{"session_id", "deleted_at"}},
		{table: "temporary_accounts", indexName: "idx_temp_accounts_session_id_deleted", columns: []string{"session_id", "deleted_at"}},
		{table: "temporary_accounts", indexName: "idx_temp_accounts_username_deleted", columns: []string{"username", "deleted_at"}},
		{table: "ai_access_tokens", indexName: "idx_ai_tokens_access_hash_deleted", columns: []string{"access_token_hash", "deleted_at"}},
		{table: "ai_access_tokens", indexName: "idx_ai_tokens_refresh_hash_deleted", columns: []string{"refresh_token_hash", "deleted_at"}},

		// 设置模型
		{table: "system_setting_revisions", indexName: "idx_ss_rev_revision_deleted", columns: []string{"revision", "deleted_at"}},
		{table: "user_groups", indexName: "idx_user_groups_name_deleted", columns: []string{"name", "deleted_at"}},

		// 数据库供给
		{table: "database_provisioning_operations", indexName: "idx_dpo_actor_kind_idem_deleted", columns: []string{"actor_id", "kind", "idempotency_key", "deleted_at"}},
		{table: "database_provisioning_operations", indexName: "idx_dpo_upstream_username_deleted", columns: []string{"upstream_username", "deleted_at"}},
	}
}

// MigrateAuditUniqueIndexes 幂等地将业务表的唯一索引重建为包含 deleted_at 的复合索引。
// 应在 AutoMigrate 之后调用。
func MigrateAuditUniqueIndexes(db *gorm.DB) error {
	migrations := allIndexMigrations()

	for _, m := range migrations {
		// 检查新索引是否已存在
		if db.Migrator().HasIndex(m.table, m.indexName) {
			continue
		}

		// 获取表中所有现有索引
		indexes, err := db.Migrator().GetIndexes(m.table)
		if err != nil {
			return fmt.Errorf("get indexes for %s: %w", m.table, err)
		}

		// 删除与新索引列前缀匹配的旧唯一索引（相同业务键但不含 deleted_at）
		for _, idx := range indexes {
			if !isUniqueIndex(idx) {
				continue
			}
			if idx.Name() == m.indexName {
				continue // 已检查过
			}
			if hasSameBusinessColumns(idx, m.columns) {
				if err := db.Migrator().DropIndex(m.table, idx.Name()); err != nil {
					return fmt.Errorf("drop old index %s on %s: %w", idx.Name(), m.table, err)
				}
			}
		}

		// 创建新索引
		cols := make([]string, len(m.columns))
		for i, c := range m.columns {
			cols[i] = db.NamingStrategy.ColumnName("", c)
		}
		if err := db.Exec(buildCreateUniqueIndexSQL(m.table, m.indexName, cols)).Error; err != nil {
			return fmt.Errorf("create index %s on %s: %w", m.indexName, m.table, err)
		}
	}

	return nil
}

func isUniqueIndex(idx gorm.Index) bool {
	// GORM 的 Index 接口没有直接返回是否 unique，通过尝试转换判断
	type unique interface{ Unique() (bool, bool) }
	if u, ok := idx.(unique); ok {
		uniq, _ := u.Unique()
		return uniq
	}
	return false
}

func hasSameBusinessColumns(idx gorm.Index, newCols []string) bool {
	idxCols := idx.Columns()
	newBizCols := newCols[:len(newCols)-1] // 去掉 deleted_at
	if len(idxCols) != len(newBizCols) {
		return false
	}
	for i, c := range idxCols {
		if c != newBizCols[i] {
			return false
		}
	}
	return true
}

func buildCreateUniqueIndexSQL(table, indexName string, columns []string) string {
	colList := ""
	for i, c := range columns {
		if i > 0 {
			colList += ", "
		}
		colList += c
	}
	return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", indexName, table, colList)
}
```

等等 — `buildCreateUniqueIndexSQL` 和 `hasSameBusinessColumns` 的逻辑需要考虑 GORM 的列名映射。GORM 默认使用 snake_case，所以列名应该是 `deleted_at` 而非 `DeletedAt`。

实际上对于迁移，我们应该直接用 SQL 字符串，跳过 GORM 的命名策略，因为 AutoMigrate 已经处理了列名。直接传入蛇形列名即可。

简化方案：所有列名已经是蛇形（在 `allIndexMigrations()` 中直接使用蛇形），因此不需要 `NamingStrategy` 转换:

```go
func buildCreateUniqueIndexSQL(table, indexName string, columns []string) string {
	colList := ""
	for i, c := range columns {
		if i > 0 {
			colList += ", "
		}
		colList += c
	}
	return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", indexName, table, colList)
}
```

同时，`hasSameBusinessColumns` 需要对比 `idx.Columns()` (返回蛇形列名) 和 `newBizCols` (也是蛇形)。

修正后：

```go
func hasSameBusinessColumns(idx gorm.Index, newCols []string) bool {
	idxCols := idx.Columns()
	newBizCols := newCols[:len(newCols)-1] // 去掉 deleted_at，只比较业务列
	if len(idxCols) != len(newBizCols) {
		return false
	}
	for i, c := range idxCols {
		if c != newBizCols[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: 在 DBStore 初始化时调用迁移**

在 `internal/store/dbstore.go` 的 `NewDBStore` 或相关初始化函数中，AutoMigrate 之后调用 `MigrateAuditUniqueIndexes`。

找到调用 `AutoMigrate` 的位置（在 `internal/storage/` 中），添加调用。

- [ ] **Step 3: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./...
```

- [ ] **Step 4: 提交**

```bash
git add internal/store/migration_audit_indexes.go
git commit -m "feat: 新增复合唯一索引迁移（业务键 + deleted_at）"
```

---

### Task 9: Store 层 — 添加软删除辅助方法

**文件:**
- Modify: `internal/store/dbstore.go` — 添加 `softDelete` 和 `activeScope` 方法

**接口:**
- Produces: `(s *DBStore) softDelete(ctx, table, id)` — 通用软删除方法
- Produces: `(s *DBStore) activeScope()` — 查询过滤 Scope

- [ ] **Step 1: 在 dbstore.go 中添加辅助方法**

```go
import (
	"time"
	"jianmen/internal/model"
)

// activeScope 返回 GORM Scope，过滤掉软删除行。
// 仅用于嵌入了 FullAudit 的业务表。
func (s *DBStore) activeScope() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("deleted_at = ?", model.SentinelDeletedAt)
	}
}

// softDelete 对指定的业务表行执行软删除（设 deleted_at = now）。
func (s *DBStore) softDelete(ctx context.Context, table string, id string) error {
	return s.db.WithContext(ctx).
		Table(table).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"deleted_at": time.Now().UTC(),
			"updated_at": time.Now().UTC(),
		}).Error
}
```

- [ ] **Step 2: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./internal/store/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/store/dbstore.go
git commit -m "feat: Store 层添加 activeScope 和 softDelete 辅助方法"
```

---

### Task 10: Store 层 — 改造删除和查询操作

**文件 (约 20 个 store 文件需要修改):**
- Modify: `internal/store/dbstore_users.go` — User Delete + activeScope
- Modify: `internal/store/dbstore_roles.go` — Role Delete + activeScope
- Modify: `internal/store/dbstore_hosts.go` — Host Delete + activeScope
- Modify: `internal/store/dbstore_databases.go` — Database Delete + activeScope
- Modify: `internal/store/dbstore_resources.go` — Resource Delete + activeScope
- Modify: `internal/store/dbstore_resource_grants.go` — ResourceGrant Delete + activeScope
- Modify: `internal/store/dbstore_user_groups.go` — UserGroup Delete + activeScope
- Modify: `internal/store/dbstore_identity.go` — Identity operations + activeScope
- Modify: `internal/store/dbstore_platform.go` — PlatformAccount Delete + activeScope
- Modify: `internal/store/dbstore_application.go` — Application Delete + activeScope
- Modify: `internal/store/dbstore_container.go` — Container Delete + activeScope
- Modify: `internal/store/dbstore_sessions.go` — Session/UserSession Delete + activeScope
- Modify: `internal/store/dbstore_temporary_access.go` — Temporary* Delete + activeScope
- Modify: `internal/store/dbstore_ai_access.go` — AIAccessToken Delete + activeScope
- Modify: `internal/store/dbstore_user_preferences.go` — UserPreference activeScope
- Modify: `internal/store/dbstore_connection_passwords.go` — ConnectionPassword activeScope
- Modify: `internal/store/dbstore_admin_auth.go` — activeScope
- Modify: `internal/store/dbstore_database_provisioning.go` — activeScope
- Modify: `internal/store/dbstore_browser_sessions.go` — activeScope
- Modify: `internal/store/system_setting.go` — activeScope
- Modify: `internal/store/dbstore_database_management.go` — activeScope
- Modify: `internal/store/dbstore_database_instance_mutations.go` — activeScope

**改造模式**: 对于每个业务表的 store 文件，做两类修改：

1. **查询方法**: 在 `s.db.WithContext(ctx).Model(&X{})` 或 `s.db.WithContext(ctx).Where(...)` 后加上 `.Scopes(s.activeScope())`
2. **删除方法**: 将 `db.Delete(&record)` 改为 `s.softDelete(ctx, tableName, id)`

注意：**审计日志表**（AuditEvent, LoginAuditLog, AuditSession, AuditArtifact 等）和**纯关联表**（RolePermission, UserRole, UserGroupMember）的查询/删除**不需要修改**。

- [ ] **Step 1: 改造 dbstore_users.go**

逐个方法检查并修改：
- 查询类方法（如 `User`, `FindUser`, `SearchUsers`）: 加上 `.Scopes(s.activeScope())`
- 删除类方法（如 `DeleteUser`）: 改为 `s.softDelete(ctx, "users", id)`
- `Authenticate` 等方法通过 `db.Model(&User{}).Where("status = ?", "active")` 查询，需加 activeScope

- [ ] **Step 2: 改造其余 store 文件**

按相同模式逐个文件修改。每个文件做完后编译验证。

- [ ] **Step 3: 注意特殊情况**

`DatabaseInstance.BeforeDelete` 包含检查逻辑（check pending provisioning operations）。该逻辑需移到 store 层的软删除方法之前执行。

`SystemSetting` 原来有 `UpdatedByID`/`UpdatedByUsername` — 引用这些字段的代码需更新为使用 `FullAudit.UpdatedBy`。需搜索所有引用并更新。

- [ ] **Step 4: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/store/
git commit -m "feat: Store 层查询加软删除过滤，Delete 改为软删除"
```

---

### Task 11: 确保 Context 传递

**文件:**
- Modify: `internal/server/admin/` — 确认所有 GORM 操作通过 `WithContext(ctx)` 传递
- Modify: `internal/server/sshserver/` — SSH 入口设置 context userID
- Modify: `internal/proxy/` — 代理层设置 context userID

- [ ] **Step 1: 检查 admin HTTP handlers**

admin server 的 `withAuthAndUser` 中间件已在 context 中设置 `ctxKeyUserID`。确认所有 handler 中的 GORM store 调用都通过 `s.db.WithContext(r.Context())` 传递了用户上下文。

- [ ] **Step 2: SSH server context 设置**

SSH 连接时（如创建 Session 记录），需要在 context 中设置用户 ID。找到 SSH session 创建代码，确认 context 包含用户 ID。

- [ ] **Step 3: 编译验证**

```bash
cd C:/02-codespace/Jianmen && go build ./...
```

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "feat: 确保 GORM context 传递用户身份"
```

---

### Task 12: 运行测试验证

- [ ] **Step 1: 运行所有单元测试**

```bash
cd C:/02-codespace/Jianmen && go test ./internal/...
```

预期：测试全部通过或只有预存在的失败

- [ ] **Step 2: 修复因模型变更导致的测试失败**

检查编译错误和测试失败：
- 模型字段变更可能导致测试中字面赋值不完整
- BeforeCreate hooks 移除可能导致 ID 生成测试失败
- UpdatedByID/UpdatedByUsername 引用变更

- [ ] **Step 3: 提交修复**

```bash
git add -A
git commit -m "test: 修复审计字段变更导致的测试"
```

- [ ] **Step 4: 再次运行测试确认通过**

```bash
cd C:/02-codespace/Jianmen && go test ./internal/...
```
