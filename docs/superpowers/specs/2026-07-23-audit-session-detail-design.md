# 审计连接授权会话 ID 与授权详情设计

## 背景

当前 SSH、RDP 与数据库审计列表可关联用户会话的 5 位 `session_id`；在线会话也可通过 `audit_session_id` 关联。界面统一将该字段显示为“授权会话 ID”，并允许点击查看授权来源。

审计人员需要从连接记录快速追溯该连接使用的授权身份，辨别普通用户、临时授权或 AI 授权，并查看授权对象、有效期、开始时间、备注和状态。

项目中的三个会话概念职责不同：

- `AdminSession`：浏览器后台登录认证，承载 Cookie、CSRF 和后台登录有效期。
- `UserSession`：资源访问授权身份，承载 5 位 SessionID、授权类型、状态和有效期。
- `AuditSession`：一次具体的 SSH、数据库或其他资源连接，使用 `UserSessionID` 关联授权身份。

本设计只增强资源访问审计，不把后台 `AdminSession` 作为资源授权会话使用。

## 目标

### 必须实现

1. SSH、RDP、数据库审计和在线会话列表统一增加独立的“授权会话 ID”列。
2. 删除 SSH 和数据库审计“操作者”旁原有的 `(session_id)` 内联显示。
3. 授权会话 ID 使用统一的可点击链接样式；点击后展示 UserSession 授权详情。
4. 在线会话后端响应补充对应 UserSession 的 5 位 SessionID。
5. 详情能够区分普通用户、临时授权和 AI 授权。
6. 详情展示授权用户、授权创建人、授权有效期、开始时间、备注和状态。
7. 临时授权与 AI 授权的备注复用现有 `TemporaryAccount.Remark`，不在 `UserSession` 重复新增字段。
8. 四个列表共用同一套详情 API 和前端弹窗组件。

### 明确不做

1. 本文不改变登录审计和操作审计的授权详情语义。
2. 不建立 `AdminSession` 与 `UserSession` 的新关联。
3. 不修改 `UserSession` 表结构，也不新增备注字段。
4. 不把完整授权详情冗余写入 SSH、数据库或在线会话列表响应。
5. 不迁移或重写历史审计数据。
6. 不调整现有回放、命令、文件、查询、刷新和断开功能。

## 现有数据关系

SSH 和数据库审计已经具备以下链路：

```text
AuditSession.UserSessionID
  -> UserSession.ID
  -> UserSession.SessionID（5 位展示 ID）
```

在线会话已经返回 `audit_session_id`，可沿以下链路得到 SessionID：

```text
OnlineSession.AuditSessionID
  -> AuditSession.ID
  -> AuditSession.UserSessionID
  -> UserSession.ID
  -> UserSession.SessionID
```

授权类型需要结合两类数据判断：

```text
UserSession.Type = permanent
  -> 普通用户

UserSession.Type = temporary
  + TemporaryAccount.SessionID = UserSession.ID
  + TemporaryAccount.Type = temporary_user
  -> 临时授权

UserSession.Type = temporary
  + TemporaryAccount.SessionID = UserSession.ID
  + TemporaryAccount.Type = ai_user
  -> AI 授权
```

## 方案选择

### 采用方案：统一详情 API

新增按 5 位 SessionID 查询授权详情的只读 API。SSH、数据库和在线会话列表只返回展示所需的 SessionID；用户点击后再请求完整详情。

优点：

- 授权类型判定和状态计算集中在后端，不在多个列表重复。
- 列表响应保持轻量，不携带大量重复授权信息。
- 四个列表可以复用同一组件和接口。
- 后续其他审计页面若需要，也可以复用详情能力。

代价是首次点击会多发起一次请求，但详情按需加载，整体成本可控。

### 未采用方案：列表直接返回完整详情

该方案需要在四个列表查询中重复关联和组装授权信息，增加响应体积和维护成本，因此不采用。

### 未采用方案：前端组合多个现有接口

当前没有完整的 UserSession 详情读取接口。让前端分别查询用户、临时账号并自行判定类型，会造成请求和权限逻辑分散，因此不采用。

## 后端设计

### UserSession 授权详情视图

在 store 或 service 层新增专用详情视图，建议字段如下：

```go
type UserSessionDetail struct {
    ID                     string
    SessionID              string
    SessionType            string
    AuthorizationType      string
    UserID                 string
    Username               string
    AuthorizedBy           string
    StartsAt               time.Time
    ExpiresAt              *time.Time
    Remark                 string
    Status                 string
    EffectiveStatus        string
}
```

API JSON 响应采用 snake_case：

```json
{
  "id": "user-session-primary-key",
  "session_id": "A1b2C",
  "session_type": "temporary",
  "authorization_type": "ai",
  "user_id": "user-id",
  "username": "authorized-user",
  "authorized_by": "admin-user",
  "starts_at": "2026-07-23T10:00:00Z",
  "expires_at": "2026-07-23T18:00:00Z",
  "remark": "生产故障排查",
  "status": "active",
  "effective_status": "active"
}
```

`authorization_type` 使用稳定的机器值：

- `normal`：普通用户。
- `temporary`：临时授权。
- `ai`：AI 授权。
- `unknown`：临时 UserSession 缺少或无法识别关联授权记录。

展示文案由前端 i18n 根据机器值映射，不由后端返回中文标签。

### 字段来源

- `id`：`UserSession.ID`。
- `session_id`：`UserSession.SessionID`。
- `session_type`：`UserSession.Type`。
- `authorization_type`：结合 `UserSession.Type` 与 `TemporaryAccount.Type` 判定。
- `user_id`、`username`：`UserSession.UserID` 及关联的 `User`。
- `authorized_by`：`UserSession.CreatedBy`；普通永久会话为空时返回空字符串。
- `starts_at`：`UserSession.CreatedAt`。
- `expires_at`：`UserSession.ExpiresAt`。
- `remark`：临时或 AI 授权读取 `TemporaryAccount.Remark`；普通用户返回空字符串。
- `status`：`UserSession.Status` 原始值。
- `effective_status`：综合原始状态和有效期计算。

### 有效状态计算

有效状态使用以下优先级：

1. `UserSession.Status` 不是 `active`：返回 `disabled`。
2. `ExpiresAt` 非空且不晚于当前时间：返回 `expired`。
3. 其他情况：返回 `active`。

原始 `status` 与计算后的 `effective_status` 同时返回，以便审计人员区分“数据记录仍为 active 但授权已到期”的情况。

### 查询方法

新增只读查询方法，通过 5 位 `UserSession.SessionID` 查询详情：

1. 校验 SessionID 格式。
2. 查询 `UserSession` 并关联用户信息。
3. 当 `UserSession.Type = temporary` 时，以 `TemporaryAccount.SessionID = UserSession.ID` 查询临时账号。
4. 根据临时账号类型填充授权类型和备注。
5. 即使临时账号关联缺失，也返回 UserSession 基本详情，并将授权类型设为 `unknown`。

查询不依赖审计记录 ID，因此 SSH、数据库和在线会话共用同一入口。

### 详情 API

新增路由：

```text
GET /api/user-sessions/by-session-id/{sessionID}
```

权限策略：调用者拥有以下任一权限即可读取：

- `audit:view`
- `db:audit:view`
- `session:view`

这与三个入口页面的现有查看权限对应。接口仍须经过后台认证，不能因为知道 5 位 SessionID 就匿名读取授权信息。

响应规则：

- 成功：`200` 和详情对象。
- SessionID 格式非法：`400`。
- SessionID 不存在：`404`。
- 无读取权限：`403`。
- 数据库错误：`500`，不返回内部查询信息。

### 在线会话列表

在线会话响应类型增加：

```go
UserSessionID string `json:"user_session_id,omitempty"`
SessionID     string `json:"session_id,omitempty"`
```

其中：

- `user_session_id` 是内部主键，保留给前后端关联和排障，不作为页面主展示内容。
- `session_id` 是 5 位短 ID，用于列表展示和详情请求。
- `audit_session_id` 继续表示当前具体连接的审计记录 ID，语义不变。

在线会话查询应在后端批量或联表补齐 UserSession，避免对每行执行单独查询。无法关联 UserSession 时返回空字段，不影响在线会话刷新、回放和断开操作。

### SSH 与数据库审计列表

SSH 与数据库列表已有 SessionID 返回能力，保持现有数据链路，不修改审计表结构。实现时需要确认两类列表均从 `AuditSession.UserSessionID` 关联 `UserSession.SessionID`，并在无关联时返回空字符串。

## 前端设计

### API 类型与客户端

新增 `UserSessionDetail` 类型和详情请求方法。类型字段与后端响应一致：

```typescript
export interface UserSessionDetail {
  id: string;
  session_id: string;
  session_type: string;
  authorization_type: 'normal' | 'temporary' | 'ai' | 'unknown';
  user_id: string;
  username: string;
  authorized_by?: string;
  starts_at: string;
  expires_at?: string;
  remark?: string;
  status: string;
  effective_status: 'active' | 'expired' | 'disabled' | string;
}
```

`OnlineSessionRecord` 增加可选的 `user_session_id` 和 `session_id` 字段。

### 独立详情组件

新增独立组件 `UserSessionDetailDialog.vue`，避免继续扩大已经较大的 `AuditView.vue`。组件职责：

1. 接收要查看的 `sessionID` 和显隐状态。
2. 在打开时请求详情。
3. 处理加载、成功、错误和关闭状态。
4. 使用描述列表展示详情。
5. 对授权类型和有效状态使用统一标签。
6. 关闭时清空旧数据，避免下一次打开短暂显示上一条详情。

组件不负责列表查询，也不感知入口来自 SSH、数据库还是在线会话。

### 统一授权会话 ID 列

四个连接类列表均使用独立的“授权会话 ID”列。该字段对应
`UserSession.SessionID`，不能与 `AuditSession.ID`（审计会话 ID）混称：

- SSH：开始时间、操作者、授权会话 ID、目标主机、主机账号、来源 IP、协议、结果、时长、审计事件数、操作。
- RDP：开始时间、操作者、授权会话 ID、目标主机、主机账号、来源 IP、结果、时长、录制状态、操作。
- 数据库：开始时间、操作者、授权会话 ID、数据库实例、数据库账号、来源 IP、协议、结果、时长、SQL 记录数、操作。
- 在线会话：开始时间、操作者、授权会话 ID、目标资源、协议、登录账号、操作。

具体要求：

1. SSH 和数据库“操作者”列只显示用户名，删除旁边原有的 `(session_id)`。
2. 授权会话 ID 存在时使用链接样式按钮显示 5 位短 ID。
3. 点击链接打开统一详情弹窗。
4. 授权会话 ID 缺失时显示 `-`，且不可点击。
5. 四个表格使用相同列宽、对齐方式和交互样式。
6. 点击授权会话 ID 不触发表格行的其他操作。

### 详情展示

弹窗展示：

- SessionID
- 授权类型
- 授权用户
- 授权创建人
- 开始时间
- 授权有效期
- 备注
- 当前状态

展示规则：

- 普通用户没有到期时间时显示“永久有效”。
- 授权创建人或备注为空时显示 `-`。
- 时间按现有审计页面的本地化格式化方式显示。
- `normal` 映射为“普通用户”。
- `temporary` 映射为“临时授权”。
- `ai` 映射为“AI 授权”。
- `unknown` 映射为“未知授权”。
- `active` 映射为“生效中”。
- `expired` 映射为“已过期”。
- `disabled` 映射为“已禁用”。

建议标签颜色：

- 普通用户：蓝色。
- 临时授权：橙色。
- AI 授权：青色。
- 未知授权：灰色。
- 生效中：绿色。
- 已过期：灰色。
- 已禁用：红色。

所有新增文案同时补充中文和英文 i18n，不在模板中散落硬编码文案。

## 数据流

### SSH 和数据库审计

```text
列表接口返回 session_id
  -> 用户点击 SessionID
  -> GET /api/user-sessions/by-session-id/{sessionID}
  -> 后端查询 UserSession + User + TemporaryAccount
  -> 前端弹窗展示授权详情
```

### 在线会话

```text
在线连接
  -> audit_session_id
  -> AuditSession.UserSessionID
  -> UserSession.SessionID
  -> 在线列表返回 session_id
  -> 用户点击 SessionID
  -> 统一详情 API
  -> 统一详情弹窗
```

## 异常与兼容处理

1. 历史 AuditSession 保留有效 `UserSessionID` 时，可正常查看详情。
2. 历史记录的 UserSession 已删除或无法关联时，列表 SessionID 显示 `-`。
3. 临时 UserSession 存在但 TemporaryAccount 缺失时，详情仍返回基础信息，类型显示“未知授权”。
4. 点击后记录恰好被删除时，前端提示“会话授权信息不存在”，关闭或保留空态弹窗，不影响列表。
5. 网络或服务端错误时显示统一错误提示，不展示上一条缓存详情。
6. 在线会话关联失败不影响定时刷新、回放和断开。
7. API 不返回临时账号密码、AI Token、Cookie、密钥或其他敏感凭据。

## 测试设计

### 后端单元与集成测试

1. 普通永久 UserSession 返回 `authorization_type = normal`。
2. 临时授权返回 `authorization_type = temporary`，并正确返回备注和有效期。
3. AI 授权返回 `authorization_type = ai`，并正确返回备注和有效期。
4. 临时 UserSession 缺少 TemporaryAccount 时返回 `unknown`，基础字段仍完整。
5. 已到期且原始状态为 active 时，`effective_status = expired`。
6. 非 active 状态优先返回 `disabled`。
7. SessionID 不存在返回 `404`。
8. SessionID 格式非法返回 `400`。
9. 无权限访问返回 `403`。
10. 拥有 `audit:view`、`db:audit:view` 或 `session:view` 任一权限时可访问。
11. 在线会话正确返回 `user_session_id` 和 5 位 `session_id`。
12. 在线会话无 UserSession 关联时正常返回其他字段。
13. 在线会话列表补齐 SessionID 不产生逐行 N+1 查询。

### 前端验证

1. SSH、RDP、数据库、在线会话均出现独立的授权会话 ID 列。
2. SSH 和数据库操作者旁不再显示 `(session_id)`。
3. 有授权会话 ID 时可点击，无授权会话 ID 时显示 `-`。
4. 四个列表点击后打开同一个详情组件。
5. 普通、临时、AI 和未知授权类型显示正确。
6. 永久有效、有效期、备注和状态显示正确。
7. 加载期间不显示旧详情。
8. `404`、`403` 和通用请求错误均有明确提示。
9. 关闭后重新打开另一条 SessionID 时重新加载正确数据。
10. SSH/数据库筛选、回放、命令、文件和查询功能不受影响。
11. 在线会话自动刷新、回放和断开功能不受影响。
12. 执行 `npm run typecheck` 和 `npm run build`。

### Go 回归验证

执行受影响包测试和完整 Go 测试；至少覆盖 store、admin handler、在线会话查询以及现有审计查询。

## 风险与处理

### 风险一：临时授权关联键误用

`TemporaryAccount.SessionID` 关联的是 `UserSession.ID`，不是 5 位 `UserSession.SessionID`。查询实现必须显式使用内部主键关联，并通过测试锁定该关系。

### 风险二：在线会话出现 N+1 查询

若逐条通过 AuditSession 和 UserSession 查询，会放大在线会话的定时刷新负载。应使用现有 AuditSession 数据、批量查询或联表一次性补齐。

### 风险三：短 SessionID 可枚举

5 位 SessionID 不是访问凭据。详情接口必须要求后台认证和审计相关权限，且不得返回任何凭据类敏感字段。

### 风险四：原始状态和实际有效性不一致

临时授权到期后，数据库状态可能仍为 active。接口同时返回原始状态和计算状态，前端主展示计算状态，从而避免误导。

## 验收标准

满足以下条件即可认为功能完成：

1. SSH、RDP、数据库和在线会话分别有独立的授权会话 ID 列。
2. SSH、数据库操作者旁不再内联显示授权会话 ID。
3. 四个列表中的有效授权会话 ID 都可点击并打开统一详情弹窗。
4. 在线会话能返回并显示对应 UserSession 的 5 位 SessionID。
5. 弹窗正确区分普通用户、临时授权和 AI 授权。
6. 弹窗展示授权用户、授权创建人、有效期、开始时间、备注和状态。
7. 备注来自现有 `TemporaryAccount.Remark`，未修改 UserSession 表结构。
8. 缺失关联和历史数据不会导致列表加载失败。
9. 权限检查阻止未授权用户读取详情。
10. SSH、RDP、数据库和在线会话原有操作均无回归。
11. 后端测试、前端类型检查和生产构建通过。
