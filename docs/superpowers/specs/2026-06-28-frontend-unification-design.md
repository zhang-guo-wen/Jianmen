# 前端页面统一优化设计

日期：2026-06-28

## 目标

1. 每个页面支持后端分页，分页组件位于右下角，布局占满屏幕
2. 统一搜索框进行过滤搜索
3. SSH 和数据库统一建模（独立表 + 统一 API JSON 层 + 前端统一组件）
4. 保持代码简单干净，大胆清理无用字段/代码/注释，不考虑兼容性

## 设计决策

| 决策 | 选择 |
|---|---|
| 统一建模深度 | 独立表 + 统一 API JSON 层 + 前端统一组件 |
| 分页策略 | 全部后端分页（`page`/`page_size`/`q`） |
| 搜索过滤 | 每页顶部统一搜索框，后端 `q` 参数模糊搜索 |
| 分页位置 | 内嵌表格卡片底部 footer，右下角 |
| 实施方案 | 先建地基（布局+组件+后端），再一次性替换页面 |
| 代码风格 | 简单干净，删无用字段/代码/注释，不考虑兼容性 |

---

## 一、全局布局与设计系统

### 1.1 布局结构

保持现有 App.vue 骨架（侧边栏 + Header + 主内容区），统一页面容器：

```
┌──────────┬──────────────────────────────────────┐
│ Sidebar  │ Header（页面标题 + 描述）              │
│ 236px    ├──────────────────────────────────────┤
│          │ PageContainer（flex:1, overflow:hidden）│
│          │  ┌────────────────────────────────┐  │
│          │  │ Toolbar（搜索框 + 操作按钮）     │  │
│          │  ├────────────────────────────────┤  │
│          │  │ Table Area（flex:1, 可滚动）    │  │
│          │  ├────────────────────────────────┤  │
│          │  │ Footer（分页，右对齐）           │  │
│          │  └────────────────────────────────┘  │
└──────────┴──────────────────────────────────────┘
```

### 1.2 核心原则

- 页面容器 `height: calc(100vh - header高度)`，不撑出外层滚动条
- 表格卡片 `display: flex; flex-direction: column; height: 100%`
- 表格区域 `flex: 1; overflow: auto`
- 弹窗统一宽度：简单表单 480px，复杂表单 640px
- 弹窗内表单 `max-height: 60vh; overflow-y: auto`

### 1.3 CSS 变量

```css
--gap-xs: 4px;
--gap-sm: 8px;
--gap-md: 16px;
--gap-lg: 24px;
--radius-sm: 4px;
--radius-md: 8px;
```

### 1.4 全局样式清理

- 删除不再使用的 class：`.view-stack`、`.metric-grid`、`.metric-card`、`.terminal-frame`（如未使用）
- 删除侧边栏响应式断点 `<=780px` 逻辑（管理后台不需要移动端适配）
- 表格统一 `size="small"`

---

## 二、核心统一组件

### 2.1 DataTableCard — 统一数据表格卡片

所有列表页面的标准容器。

**Props:**
```ts
{
  columns: Column[]             // 列定义
  data: any[]                   // 当前页数据
  loading: boolean              // 加载状态
  total: number                 // 总条数
  page: number                  // 当前页码
  pageSize: number              // 每页条数
  pageSizes?: number[]          // 可选每页条数选项，默认 [20, 50, 100]
  searchPlaceholder?: string    // 搜索框占位文字
  showSearch?: boolean          // 是否显示搜索框，默认 true
  rowKey?: string               // 行 key，默认 'id'
}
```

**Emits:** `update:page`, `update:pageSize`, `search`, `row-click`

**Slot:** `toolbar-extra`（搜索框右边的操作按钮区）

**结构：**
```
┌─────────────────────────────────────────┐
│ [搜索框 🔍________________] [+ 新增按钮] │
├─────────────────────────────────────────┤
│ 表格（el-table, small, stripe）          │
│  ...数据行...                            │
├─────────────────────────────────────────┤
│              共 N 条  [< 1 2 3 >]  20/页│
└─────────────────────────────────────────┘
```

### 2.2 FormDialog — 统一表单弹窗

**Props:**
```ts
{
  visible: boolean
  title: string
  width?: string              // 默认 '480px'
  loading?: boolean
  submitText?: string         // 默认 '保存'
  modelValue: object
}
```

**Emits:** `update:visible`, `update:modelValue`, `submit`

**结构：** 固定宽度，表单区 `max-height: 60vh; overflow-y: auto`，底部取消+确定按钮。

### 2.3 StatusSwitch — 统一启用禁用开关

**Props:** `modelValue: boolean`, `loading?: boolean`
**Emits:** `update:modelValue`

### 2.4 删除的旧组件

- `PaginationBar.vue` → 功能合并到 DataTableCard

---

## 三、后端模型清理与统一

### 3.1 删除的死表（4 张）

| 模型 | 表 | 原因 |
|---|---|---|
| `SessionCommand` | `session_commands` | 从未创建或查询任何记录 |
| `SessionFileEvent` | `session_file_events` | 从未创建或查询任何记录 |
| `Recording` | `recordings` | 录制走文件系统，不依赖数据库 |
| `AuditLog` | `audit_logs` | 从未写入或查询 |

操作：删除 struct 定义、从 `AllModels()` 移除、删除 `BeforeCreate` 钩子。

### 3.2 删除的未使用字段

| 结构体 | 字段 | 原因 |
|---|---|---|
| `HostAccount` | `CredentialRef` | 零引用 |
| `HostAccount` | `IsPrivileged` | 零引用 |
| `Resource` | `Attributes` | 零引用 |
| `Host` | `Protocol` | 写入但从未读取，Host 必然是 SSH |

### 3.3 字段统一重命名

**Host ↔ DatabaseInstance：**

| 概念 | 当前 Host | 当前 DBInstance | 统一后 |
|---|---|---|---|
| 分组 | `Labels` → JSON `"labels"` | `GroupName` → JSON `"group_name"` | `GroupName` → JSON `"group"` |
| 端口 | `Port` 有 | 无 | DBInstance 加 `Port` 字段 |
| 状态 | `Status` string | `Disabled` bool | 统一为 `Status` string |
| 视图地址字段 | `HostView.Host` | `DBInstanceView.Address` | 统一为 `address` |

**HostAccount ↔ DatabaseAccount：**

| 概念 | 当前 HostAccount | 当前 DBAccount | 统一后 |
|---|---|---|---|
| 登录用户名 | `Username` | `UpstreamUsername` | 统一为 `Username` |
| 密码 | `Password` | `UpstreamPassword` | 统一为 `Password` |
| 分组 | `Labels` | `GroupName` | 统一为 `GroupName` → JSON `"group"` |
| 状态 | `Status` string | `Disabled` bool | 统一为 `Status` string |
| 有效期 | 无 | `ExpiresAt` | HostAccount 加 `ExpiresAt` |

### 3.4 HostView 冗余字段清理

- `Static` — DB 模式下永远是 `false`，删除
- `Disabled` — 和 `Status` 表达相同含义，只保留 `Status`
- 删除所有 `omitempty` tag

### 3.5 删除 access/static_adapter

- 删除 `internal/access/static.go`
- 删除 `internal/store/static_adapter.go`
- 清理所有相关的适配层代码
- 只保留 DB 存储路径（`dbstore.go`）

---

## 四、API 层统一

### 4.1 后端 API 统一分页参数

所有列表接口加：`?page=1&page_size=20&q=关键词`

需要改的接口：
- `GET /api/hosts` — 已有分页，确认 `q` 搜索范围
- `GET /api/hosts/:id/accounts` — 加分页+搜索
- `GET /api/db/instances` — 加分页+搜索
- `GET /api/db/instances/:id/accounts` — 加分页+搜索
- `GET /api/sessions` — 加分页+搜索
- `GET /api/db/connections` — 加分页+搜索
- `GET /api/users` — 加分页+搜索
- `GET /api/rbac/roles` — 加分页+搜索
- `GET /api/rbac/permissions` — 加分页+搜索
- `GET /api/targets` — 加分页+搜索

### 4.2 统一响应格式

```json
{
  "items": [...],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

### 4.3 前端 API 类型统一

**Endpoint（统一端点）：**
```ts
interface Endpoint {
  id: string
  name: string
  type: 'ssh' | 'database'
  protocol: string
  address: string
  port: number
  group: string
  remark: string
  status: string
  accountCount: number
  createdAt: string
}
```

**EndpointAccount（统一账号）：**
```ts
interface EndpointAccount {
  id: string
  endpointId: string
  endpointType: 'ssh' | 'database'
  username: string
  authType: string             // SSH: 'password'|'private_key', DB: 无
  group: string
  remark: string
  status: string
  expiresAt?: string
  resourceId: string
}
```

**AuditRecord（统一审计）：**
```ts
interface AuditRecord {
  id: string
  type: 'ssh' | 'database'
  instanceName: string
  accountName: string
  operator: string
  protocol: string
  startedAt: string
  durationMs: number
}
```

---

## 五、页面级改造

### 5.1 改造清单

所有列表页面统一使用 DataTableCard 组件：

| 页面 | 当前 | 改造 |
|---|---|---|
| 主机管理 | 1676行，已有分页+搜索 | DataTableCard + FormDialog + StatusSwitch |
| 数据库管理 | 1225行，客户端过滤 | 后端分页+搜索，DataTableCard |
| 审计 | 1218行，前端分页 | 后端分页+搜索，DataTableCard |
| 快速连接 | 262行，无分页 | 加分页+搜索，DataTableCard |
| 用户管理 | 428行，无分页 | 加分页+搜索，DataTableCard |
| 角色管理 | 483行，无分页 | 加分页+搜索，DataTableCard |
| RBAC | 1527行，客户端过滤 | 5个tab各加分页+搜索 |

### 5.2 弹窗统一

所有弹窗使用 FormDialog，宽度统一：
- 简单表单（用户创建、角色创建）：480px
- 复杂表单（主机创建、账号创建）：640px

### 5.3 前端代码清理

每个页面删除：
- 自建的 `el-pagination` 配置
- 自建的搜索框 + `computed` 过滤
- 自建的 `el-dialog` + 表单布局
- 自建的启用/禁用开关逻辑
- 重复的 CSS（flex 布局、表格高度等）
- `omitempty` 相关的条件渲染
