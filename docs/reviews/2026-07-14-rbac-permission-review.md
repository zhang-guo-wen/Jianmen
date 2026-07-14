# 权限管理功能审查报告

日期：2026-07-14

审查范围：角色与权限目录、用户与用户角色、用户组、资源授权、资源可见性、SSH/SFTP/Web Terminal、数据库代理、应用代理、审计接口及相关前端页面。

## 总体结论

当前权限体系不能视为一套端到端可用的授权系统。超级管理员因为绕过全部检查，主要功能可以使用；普通用户通过管理页面得到的角色动作和资源授权，无法在所有资源与连接入口得到一致结果。

主要原因：

- 角色动作目录由前端硬编码，与后端动作和菜单持续漂移。
- 通用 Permission 和 ResourceGrant 两套资源授权模型并存。
- SSH、数据库、应用代理、Web Terminal 分别采用不同的授权路径。
- 部分业务 handler 只验证登录，未验证权限。
- 资源列表只按全局 view 动作控制，不按具体资源授权过滤。
- 权限分页、非事务保存和静默失败导致界面显示与实际权限不一致。

## 风险摘要

### P0

1. 普通登录用户可以调用 RBAC 管理接口给自己提权。
2. 主机、主机账号、数据库实例和数据库账号的部分更新/删除接口没有权限检查。
3. 数据库审计、终端回放和审计详情存在未授权读取路径。
4. Web Terminal 只检查登录 token，完全绕过角色动作和资源授权。

### P1

1. 角色页面漏掉应用、平台账号等 11 个后端动作。
2. 角色权限保存只加载前 20 条记录，可能造成撤权失败。
3. 用户角色管理超过 20 条后显示和分配不完整。
4. 资源组、账号组授权无法匹配组内资源。
5. 数据库 ResourceGrant 与最终数据库鉴权不相通。
6. 应用代理所需权限无法通过现有页面配置。
7. SFTP 读写权限未接入代理路径。
8. 数据库测试连接接口绕过业务权限和资源授权。
9. 主机账号有效期未在 SSH 连接路径生效。

### P2

1. `/api/me/permissions`、菜单计算和 Checker 的 deny/资源范围语义不一致。
2. 有效权限查询 API 没有对应管理页面。
3. RBAC handler 直接操作 GORM，文件超过规范行数，缺少 service/store 分层。

## 第一部分：管理接口鉴权

### P0-1：RBAC 和资源授权管理接口只验证登录

以下路由只使用 `withAuthAndUser`，没有要求 `rbac:manage`：

- `/api/rbac/roles`
- `/api/rbac/permissions`
- `/api/rbac/user-roles`
- `/api/rbac/role-permissions`
- `/api/rbac/effective`
- `/api/user-groups`
- `/api/resource-groups`
- `/api/resource-grants`

路由位置：`internal/server/admin/server.go:158-187`。

处理器内部也没有二次鉴权，例如：

- `internal/server/admin/rbac.go:58`
- `internal/server/admin/resource_grant_handlers.go:13`
- `internal/server/admin/user_group_handlers.go:12`
- `internal/server/admin/resource_group_handlers.go:12`

任意 active 用户可以创建 Permission、Role、RolePermission 和 UserRole，并将通配权限分配给自己。还可以创建通配 ResourceGrant，通过 SSH 资源检查。

整改：

- 所有 RBAC、用户组、资源组和资源授权管理接口统一要求 `rbac:manage`。
- 查询其他用户有效权限同样要求管理权限。
- 增加普通用户访问这些接口必须返回 403 的路由级集成测试。

### P0-2：资产更新和删除动作没有接入 handler

后端声明了：

- `host:update`、`host:delete`
- `target:update`、`target:delete`
- `dbproxy:update`、`dbproxy:delete`

但以下分支没有调用 `requirePermission`：

- 主机更新/删除：`internal/server/admin/host_handlers.go:94-100`
- 主机账号更新/删除：`internal/server/admin/host_handlers.go:282-288`
- 数据库实例更新/删除：`internal/server/admin/db_handlers.go:185-191`
- 数据库账号更新/删除：`internal/server/admin/db_handlers.go:269-275`
- 数据库实例下账号列表/创建：`internal/server/admin/db_handlers.go:127-145`
- 主机下账号列表：`internal/server/admin/host_handlers.go:65-80`

任意登录用户可以直接修改或删除资源，无需先利用 RBAC 管理接口提权。

整改：每个 HTTP 方法分支显式绑定对应动作，并为每个 PUT、DELETE 和账号子路由增加 403 测试。

### P0-3：审计接口鉴权不完整

- SSH 审计错误使用 `session:view`：`internal/server/admin/audit_handlers.go:15-19`。
- 数据库审计列表没有权限检查：`internal/server/admin/audit_handlers.go:39`。
- 审计详情、命令、文件事件、终端回放和 SQL 查询没有权限检查：`internal/server/admin/audit_handlers.go:63`。

整改：

- SSH/SFTP 审计要求 `audit:view`。
- 数据库审计要求 `db:audit:view`。
- artifact 接口按数据库中的真实 Session.Protocol 决定权限，不能信任 URL protocol。

## 第二部分：角色和用户体系

### 角色页面漏掉 11 个动作

角色权限清单硬编码在：

`web/src/views/RolesView.vue:148`

后端当前声明 31 个动作，前端只展示 20 个。缺失：

- `dashboard:view`
- `application:view`
- `application:create`
- `application:update`
- `application:delete`
- `app:connect`
- `platform_account:view`
- `platform_account:create`
- `platform_account:update`
- `platform_account:delete`
- `platform_account:use`

后端定义位置：`internal/rbac/resources.go:30-41`。

“应用发布”和“平台账号”已经进入主菜单，但管理员无法通过角色页面授予普通用户对应菜单权限。

角色页面还展示了一批没有真正接入业务路径的动作：

- `host:update`、`host:delete`
- `target:update`、`target:delete`
- `dbproxy:update`、`dbproxy:delete`
- `db:audit:view`
- `sftp:read`、`sftp:write`

因此当前页面同时存在“新增权限看不到”和“权限能勾选但不生效”两类问题。

### 角色权限只读取前 20 条

以下调用没有传完整分页参数：

- `getRBACRolePermissions()`：`web/src/views/RolesView.vue:257`
- `getRBACPermissions()`：`web/src/views/RolesView.vue:386`
- 保存后重新加载绑定：`web/src/views/RolesView.vue:426`

后端默认 `page_size=20`：

- Permission：`internal/server/admin/rbac.go:194`
- RolePermission：`internal/server/admin/rbac.go:389`

超过 20 条后：

- 权限数量和勾选状态错误。
- 未加载到的旧绑定不会被删除。
- 管理员看到保存成功，但原权限仍然有效。

### 角色页面混淆同 action 的不同策略

前端使用 action 作为 Permission 唯一键：

- `web/src/views/RolesView.vue:390-392`
- `web/src/views/RolesView.vue:438-441`

但 Permission 实际由以下字段共同表达：

- action
- resource_type
- resource_id
- effect

同一个 action 可能同时存在全局 allow、资源级 allow 和资源级 deny。前端会任选其中一个 Permission ID，可能把 deny 或资源级策略当作全局角色动作绑定。

保存过程逐条创建和删除，没有事务，中途失败会留下部分更新。

### 用户角色管理超过 20 条后不可用

用户页面未分页加载：

- 用户角色：`web/src/views/UsersView.vue:287`
- 角色列表：`web/src/views/UsersView.vue:294`
- 批量创建角色列表：`web/src/components/BatchCreateUsersDialog.vue:130`

结果：

- 页面只看到最新 20 条 UserRole。
- 角色下拉框只显示最新 20 个角色。
- 用户可能被错误显示为“无角色”。
- 可能重复分配已有角色并触发唯一索引错误。

UserRole 响应没有预加载 Role，且 `model.UserRole.Role` 的 JSON 标签为 `-`。前端读取 `ur.role?.name`，正常响应通常只能退化为显示 role UUID。

### 批量用户角色分配失败被静默忽略

位置：`web/src/components/BatchCreateUsersDialog.vue:246-250`。

用户创建成功、角色绑定失败时，异常被直接吞掉，整行仍显示“已创建”。管理员不会知道用户实际没有角色。

### 角色和用户功能缺口

- 角色页面没有完整的名称/描述编辑入口。
- UserRole 支持 `expires_at`，前端没有有效期设置。
- `/api/rbac/effective` 已存在，但统一权限页面没有有效权限查询入口。
- 角色权限来源、资源范围、deny 原因和过期时间无法在 UI 中解释。

## 第三部分：资源授权体系

### 资源组和账号组授权不会生效

`ResourceGrantChecker.matchesGrant` 先比较 grant.ResourceType 和请求 ResourceType：

`internal/rbac/resource_grant_checker.go:97`

授权记录为 `resource_group`，请求为 `host_account` 或 `database_account` 时会立即返回 false，后续组成员判断无法执行。

前端还会提交 `account_group`：

`web/src/views/ResourceGrantView.vue:501-542`

后端只识别 `resource_group`，并且现有组成员逻辑按主机/数据库实例的 group_name 查询，没有按账号自身 group_name 处理账号组。

### ResourceGrant 不控制资源可见性

资源列表只检查全局 view 动作，然后返回全部资源：

- 主机账号：`internal/server/admin/host_handlers.go:124-136`
- 数据库实例：`internal/server/admin/db_handlers.go:69-81`
- 数据库账号：`internal/server/admin/db_handlers.go:127-140`
- 应用：`internal/server/admin/app_handlers.go:12-24`

Quick Connect 页面直接使用这些全量接口：

- SSH：`web/src/views/QuickConnectView.vue:184-195`
- 数据库：`web/src/views/QuickConnectView.vue:263-278`

当前表现：

- 没有全局 view：看不到整个列表。
- 有全局 view、没有具体资源授权：仍能看到全部资源。
- 是否能连接由各连接入口另外判断。

建议：

- 资产管理页面可由管理 view 动作决定是否看全部。
- 快速连接页面只返回当前用户最终可连接的账号资源。
- 新增 `/api/me/connectable-host-accounts`、`/api/me/connectable-database-accounts` 等接口，并复用最终 Authorizer。

### 数据库 ResourceGrant 与最终鉴权不相通

资源授权页面创建：

```text
ResourceGrant(database_account, 数据库账号 UUID)
```

数据库网关不读取 ResourceGrant，而是调用通用 Checker：

`internal/server/dbproxy/server.go:979-990`

最终资源 ID 也不是数据库账号 UUID，而是根据 unique_name 计算：

- `internal/server/dbproxy/server.go:1082-1084`
- `internal/rbac/resources.go:44-46`

角色页面只能创建 action-only 的 `db:connect`，无法创建数据库资源级 Permission。因此普通用户在页面获得 `db:connect` 和数据库 ResourceGrant 后，最终连接仍会被拒绝。

### 应用代理权限无法通过 UI 授予

应用代理检查：

```text
app:connect + application + app.ID
```

位置：`internal/server/appproxy/server.go:151-168`。

但角色页面没有 application 动作，ResourceGrant 页面也不支持 application。非超级管理员无法通过页面得到可用的应用访问权限。

### ResourceGrant 数据完整性不足

- 创建接口没有验证主体 ID 是否存在。
- 没有验证资源 ID 和资源类型是否匹配。
- resource_type 未使用严格白名单。
- 删除用户、资源或资源组后可能留下孤立授权记录。
- ResourceGrant 检查器缺少独立测试文件。

## 第四部分：最终连接鉴权

### SSH 命令行

SSH 最终连接会检查：

1. `session:connect`：`internal/server/sshserver/server.go:182`
2. HostAccount ResourceGrant：`internal/server/sshserver/server.go:195`

直接授权具体主机账号时，这条链路基本可用。没有 grant 时，即使资源可见，最终 SSH 连接会被拒绝。

### SFTP

系统声明并展示 `sftp:read`、`sftp:write`，但 SSH 服务在通过 session/connect 和资源授权后直接代理整个 session channel：

`internal/server/sshserver/server.go:281`

没有根据 SFTP 操作执行读写权限检查。有主机连接 grant 后，SFTP 读写没有继续细分。

### Web Terminal：P0 绕过

Web Terminal 路由没有使用 `withAuthAndUser`：

`internal/server/admin/server.go:146`

内部只验证 token 是否属于 active 用户：

`internal/server/admin/webterminal.go:52-61`

认证函数只返回 bool，没有把真实 user ID 写入 context。目标解析和录像使用固定伪用户：

- `internal/server/admin/webterminal.go:126-134`
- `internal/server/admin/webterminal.go:186-193`

整个流程没有调用 Checker 或 ResourceGrantChecker。

影响：任意 active 用户只要知道主机账号 ID，就可以直接通过 Web Terminal 建立 SSH 会话，不需要 session/connect 或资源授权。审计中还无法关联真实用户。

### 数据库连接

MySQL、PostgreSQL、Redis 最终都会调用：

`internal/server/dbproxy/server.go:979`

即检查资源级 `db:connect` Permission。后端检查存在，但 UI 授权链不通，ResourceGrant 不参与。

### 数据库测试连接

`POST /api/db/accounts/test/{id}` 只经过登录认证，handler 没有权限检查：

`internal/server/admin/test_db.go:22-87`

接口会读取保存的数据库账号密码并主动认证目标。任意登录用户可以探测目标可达性和凭据有效性。

### 主机账号有效期

HostAccount 定义 ExpiresAt，但：

- `DBStore.DefaultTarget` 只按 status 查询：`internal/store/dbstore_targets.go:310-340`
- targetConfig 未带出 ExpiresAt：`internal/store/dbstore_targets.go:84-105`
- SSH 只检查 target.Disabled：`internal/server/sshserver/server.go:175-178`

所以主机账号过期后仍可能连接。数据库账号有效期已经在数据库网关检查，两个账号资源行为不一致。

## 当前可见/可连接矩阵

| 资源与入口 | 列表可见条件 | 最终连接条件 | 没有具体授权时 |
|---|---|---|---|
| SSH 主机账号 | `target:view` | `session:connect` + HostAccount ResourceGrant | 能看到全部账号，SSH 命令行拒绝 |
| Web Terminal | `target:view` 只影响页面列表 | 仅 active token | 可直接连接，资源授权被绕过 |
| SFTP | 同 SSH | 实际只检查 SSH 连接动作和 grant | 无 grant 拒绝；有 grant 后不区分读写 |
| 数据库账号 | `dbproxy:view` | 资源级 `db:connect` Permission | 能看到全部账号；ResourceGrant allow 仍无法连接 |
| 数据库测试 | API 只要求登录 | 无业务权限检查 | 可触发系统使用保存凭据测试目标 |
| 应用 | `application:view` | 资源级 `app:connect` Permission | 页面无法配置完整权限 |
| 平台账号 | `platform_account:view` | action + owner/share 规则 | 角色页面缺少整组动作 |

## `/api/me/permissions` 与实际判定不一致

接口直接汇总所有 allow Permission 的 action：

- `internal/server/admin/server.go:267-277`
- `internal/server/admin/server.go:323-333`

查询没有保留资源范围，也没有执行 Checker 中 deny 优先逻辑。

影响：资源级 allow 可能被前端当作全局权限；存在 deny 时菜单和按钮仍可能显示，但后端请求被拒绝。

## 统一整改目标

建议统一为一个 Authorizer：

```text
用户/用户组
    ↓ 角色动作（能做什么）
Authorizer.Check(user, action, resourceType, resourceID)
    ↑ 资源授权（能对哪个资源做）
host_account / database_account / application / platform_account
```

规则：

1. 角色只分配动作，不保存具体资源 ID。
2. ResourceGrant 只负责主体到资源/资源组的 allow/deny。
3. 所有最终入口执行“动作允许 AND 资源允许，deny 优先”。
4. 超级管理员可以显式 bypass，但审计必须记录真实身份。
5. 快速连接列表复用同一个 Authorizer，只返回最终可连接资源。
6. 管理列表是否显示全部，由独立的管理 view 动作决定。
7. 资源主键统一使用实体 ID，不使用 unique_name 派生 ID或紧凑用户名 ResourceID。

必须统一接入：

- SSH 命令行
- SFTP subsystem
- Web Terminal
- MySQL、PostgreSQL、Redis
- Web Application Proxy
- 数据库/SSH 测试连接
- 快速连接资源列表

## 已确认整改决策：权限目录由后端统一维护

角色“分配权限”页面不再维护前端硬编码 `PERM_GROUPS`。所有可分配动作、菜单查看动作、动作名称、说明、所属模块和适用资源类型，由后端提供唯一权威目录；前端只读取和展示。

### 后端目录

建议新增：

`internal/rbac/catalog.go`

```go
type PermissionDefinition struct {
	Action        string   `json:"action"`
	Module        string   `json:"module"`
	ModuleLabel   string   `json:"module_label"`
	Label         string   `json:"label"`
	Description   string   `json:"description"`
	MenuKey       string   `json:"menu_key,omitempty"`
	ResourceTypes []string `json:"resource_types,omitempty"`
	Assignable    bool     `json:"assignable"`
}
```

示例：

```go
var Catalog = []PermissionDefinition{
	{
		Action:      ActionHostView,
		Module:      "hosts",
		ModuleLabel: "主机管理",
		Label:       "查看主机",
		Description: "浏览主机列表与详情",
		MenuKey:     "hosts",
		Assignable:  true,
	},
	{
		Action:        ActionSessionConnect,
		Module:        "sessions",
		ModuleLabel:   "会话与传输",
		Label:         "连接 SSH",
		Description:   "通过堡垒机建立 SSH 会话",
		ResourceTypes: []string{model.ResourceTypeHostAccount},
		Assignable:    true,
	},
}
```

### 权限目录接口

```text
GET /api/rbac/catalog
```

访问条件：`rbac:manage`。目录是配置元数据，不分页，一次完整返回。

响应示例：

```json
{
  "items": [
    {
      "action": "application:view",
      "module": "applications",
      "module_label": "应用发布",
      "label": "查看应用",
      "description": "浏览应用代理列表",
      "menu_key": "applications",
      "resource_types": [],
      "assignable": true
    },
    {
      "action": "app:connect",
      "module": "applications",
      "module_label": "应用发布",
      "label": "访问应用",
      "description": "通过应用代理访问指定应用",
      "resource_types": ["application"],
      "assignable": true
    }
  ]
}
```

### 菜单权限计算

当前 `menuOrder` 同时维护 menu key 和 action，会继续与权限目录重复。

整改后：

- 菜单顺序可以保留独立配置。
- 菜单所需 action 从 Catalog 按 MenuKey 查询。
- `/api/me/menus` 不再维护另一份 `menuKey → action` 映射。
- 启动时验证一个 MenuKey 只能对应一个查看动作。
- 前端路由和导航仍维护 menuKey，但不维护权限 action 清单。

### 角色页面

删除 `RolesView.vue` 中的 `PERM_GROUPS`。

页面流程：

1. 调用 `GET /api/rbac/catalog`。
2. 过滤 `assignable=true`。
3. 按 module 分组。
4. 使用后端 label 和 description 展示。
5. 保存时只提交 action 集合。

建议新增事务化接口：

```text
PUT /api/rbac/roles/{role_id}/actions
```

请求：

```json
{
  "actions": [
    "host:view",
    "target:view",
    "session:connect",
    "application:view"
  ]
}
```

后端必须：

1. 校验 action 存在于 Catalog 且 assignable=true。
2. 在单个事务中替换角色动作集合。
3. 拒绝未知 action。
4. 不允许接口写入 resource_type、resource_id 或 deny；资源授权由 ResourceGrant 管理。

### 新增菜单标准流程

1. 在 `internal/rbac/resources.go` 定义动作常量。
2. 在 Catalog 注册动作；菜单查看动作填写 MenuKey。
3. 对对应 API handler 接入 `requirePermission`。
4. 前端增加路由和导航项，使用同一个 menuKey。
5. 增加目录、菜单和 handler 鉴权测试。

不再修改角色页面权限分组代码。

### 启动与 CI 校验

- Action 不重复。
- MenuKey 不重复。
- 每个菜单查看动作存在。
- ResourceTypes 只能使用已注册资源类型。
- 角色保存拒绝 Catalog 外 action。
- 所有 `ActionXxx` 常量都被 Catalog 注册，除非明确标记内部动作。
- Catalog 菜单动作都能在前端 routeMenuMap 找到对应 menuKey。

### 权限目录与资源授权边界

权限目录回答“用户能做什么”；ResourceGrant 回答“用户能对哪个资源做”。最终连接统一执行：

```text
角色动作允许 AND 资源授权允许 AND 没有匹配 deny
```

后端权限目录不能替代 ResourceGrant，角色页面也不能继续通过 Permission 表编辑具体资源 ID。

## 最低测试矩阵

| 场景 | 期望结果 |
|---|---|
| 普通用户创建角色、权限、绑定或资源授权 | 403 |
| 普通用户 PUT/DELETE 主机、账号、数据库实例 | 403 |
| 角色页面加载 Catalog | 所有可分配动作完整显示 |
| 新增菜单动作已注册 Catalog | 自动出现在角色分配页面 |
| 角色或绑定超过 20 条 | 仍完整显示和保存 |
| 批量创建用户角色绑定失败 | 明确显示部分失败 |
| 用户只有 host grant、没有 session/connect | SSH/Web Terminal 均拒绝 |
| 用户有 session/connect、没有 host grant | SSH/Web Terminal 均拒绝 |
| 用户有数据库 ResourceGrant 和 db/connect | MySQL/PostgreSQL/Redis 均允许 |
| 用户没有数据库 grant | 快速连接不显示，最终连接拒绝 |
| 用户没有 app grant | 应用代理返回 403 |
| 用户只有 sftp/read | 下载允许，上传/删除/重命名拒绝 |
| 主机账号已过期 | SSH、SFTP、Web Terminal 均拒绝 |
| 数据库测试已有账号且用户无 grant | 403 |
| Web Terminal 建立会话 | 审计记录真实 user ID 和 username |
| 资源级 allow 出现在 `/api/me/permissions` | 不作为全局 action 返回 |

## 审查验证记录

审查期间执行：

```text
go test ./... -count=1
npm.cmd run typecheck
npm.cmd run build
```

均通过。前端构建只有第三方 pure annotation 和 chunk size 警告。

扩展审查执行：

```text
go test ./internal/rbac ./internal/server/admin ./internal/server/sshserver ./internal/server/dbproxy ./internal/server/appproxy -count=1
npm.cmd run typecheck
```

均通过；`internal/server/appproxy` 当前没有测试文件。

现有 `internal/server/admin/rbac_test.go` 主要直接调用 handler，没有经过 `withAuthAndUser` 路由链，也没有验证 `rbac:manage`，因此无法发现管理接口只验证登录、不验证授权的问题。
