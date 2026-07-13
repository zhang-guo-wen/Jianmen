# 权限管理 Tab 样式统一设计

日期：2026-07-14

## 目标

将权限管理（`UnifiedRBACView.vue`）下全部 6 个 Tab 的搜索、分页、表格容器样式统一，均使用 `DataTableCard` 组件，提供一致的用户体验。

## 现状

| Tab | 组件 | 搜索+回车 | 分页 | 表格容器 |
|-----|------|-----------|------|----------|
| 用户管理 | `UsersView.vue` | ✅ | ✅ | DataTableCard |
| 角色管理 | `RolesView.vue` | ✅ | ✅ | DataTableCard |
| 用户组 | `UserGroupsView.vue` | ❌ | ❌ | 裸 page-card |
| 资源授权 | `ResourceGrantView.vue` | ❌ | ❌ | 裸 page-card |
| 资源分组 | `ResourceGroupsContent.vue` | ❌ | ❌ | 裸 page-card |
| 账号分组 | `AccountGroupsContent.vue` | ❌ | ❌ | 裸 page-card |

`DataTableCard` 组件（`web/src/components/DataTableCard.vue`）已封装：搜索输入框（带图标、Enter 触发、清除按钮）、分页器（条数选择、上下页）、loading 状态、toolbar 插槽。

## 前端改动

### 1. UserGroupsView.vue — 改造为 DataTableCard

- 外层替换为 `DataTableCard`，搜索占位符 `"搜索用户组名称、描述..."`
- 保留现有表格列：名称、描述、成员数（点击查看成员）、操作（编辑/删除）
- 分页默认 20 条/页
- 成员管理弹窗保持不变（内部已有用户搜索）

### 2. ResourceGrantView.vue — 改造为 DataTableCard，去掉筛选下拉

- **删除** `principal_type` 和 `resource_type` 两个下拉筛选框 + 搜索按钮
- 统一用 `DataTableCard` 文本搜索，搜索占位符 `"搜索主体名称、资源名称..."`
- 保留现有表格列：主体类型、主体名、资源类型、资源名、效果、有效期、操作
- 分页默认 20 条/页
- 创建授权弹窗保持不变
- 保留 `onMounted` 中的 `loadUsers()` 和 `loadUserGroups()` 预加载（用于表格中的名称显示），主数据改为分页加载

### 3. ResourceGroupsContent.vue — 改造为 DataTableCard

- 外层替换为 `DataTableCard`，搜索占位符 `"搜索分组名称、描述..."`
- **不再在前端过滤** `group_type`，由后端 `?group_type=resource` 参数控制
- 保留现有表格列：名称、描述、主机数、数据库数、操作
- 分页默认 20 条/页

### 4. AccountGroupsContent.vue — 改造为 DataTableCard

- 外层替换为 `DataTableCard`，搜索占位符 `"搜索分组名称、描述..."`
- **不再在前端过滤** `group_type`，由后端 `?group_type=account` 参数控制
- 保留现有表格列：名称、描述、账号数、操作
- 分页默认 20 条/页

## 后端改动

3 个 GET 接口统一增加 `q`（搜索）、`page`（页码）、`page_size`（每页条数）查询参数。返回格式从 `[]T` 改为 `{ items: T[], total: number }`。

### 1. `GET /api/user-groups`

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `q` | string | "" | 模糊搜索 name、description |
| `page` | int | 1 | 页码 |
| `page_size` | int | 20 | 每页条数 |

返回：`{ "items": [...UserGroup], "total": 100 }`

### 2. `GET /api/resource-grants`

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `q` | string | "" | 模糊搜索：通过 JOIN 查找匹配用户名、用户组名、资源名 |
| `page` | int | 1 | 页码 |
| `page_size` | int | 20 | 每页条数 |

**删除** `principal_type` 和 `resource_type` 筛选参数（前端不再使用）。

**搜索实现：** 由于 principal_id 和 resource_id 是 UUID，`q` 的搜索需要通过子查询关联：
- 匹配 users.username LIKE `%q%` → 得到 user UUID 集合
- 匹配 user_groups.name LIKE `%q%` → 得到 group UUID 集合
- 匹配 host_accounts 的 username/host 字段 → 得到 host_account UUID 集合
- 匹配 database_accounts 的 unique_name 字段 → 得到 db_account UUID 集合
- 匹配 resource_groups.name LIKE `%q%` → 得到 resource_group UUID 集合
- 最终：`WHERE principal_id IN (...) OR resource_id IN (...)`

返回：`{ "items": [...ResourceGrant], "total": 100 }`

### 3. `GET /api/resource-groups`

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `group_type` | string | "" | 保留，用于区分资源分组/账号分组 |
| `q` | string | "" | 模糊搜索 name、description |
| `page` | int | 1 | 页码 |
| `page_size` | int | 20 | 每页条数 |

返回：`{ "items": [...ResourceGroupWithCount], "total": 100 }`

## API 客户端改动

`web/src/api/client.ts` 中 3 个函数的签名更新：

```typescript
// 用户组 — 返回值从数组改为分页结构
getUserGroups: (params?: { q?: string; page?: number; page_size?: number }) =>
  request<{ items: UserGroupRecord[]; total: number }>('/api/user-groups' + buildQS(...))

// 资源授权 — 移除旧筛选参数，返回值改为分页结构
getResourceGrants: (params?: { q?: string; page?: number; page_size?: number }) =>
  request<{ items: ResourceGrantRecord[]; total: number }>('/api/resource-grants' + buildQS(...))

// 资源分组 — 保留 group_type，增加搜索和分页
getResourceGroups: (params?: { group_type?: string; q?: string; page?: number; page_size?: number }) =>
  request<{ items: ResourceGroupRecord[]; total: number }>('/api/resource-groups' + buildQS(...))
```

**注意：** 调用方（View/Component）需要同步修改，从 `data` 中解构 `.items` 和 `.total`。

## 验证步骤

1. `npm run typecheck`（前端）
2. `npm run build`（前端）
3. `go build ./...`（后端）
4. `go test ./... -count=1`（后端）
5. 手动验证：每个 Tab 的搜索框输入后回车触发后端搜索，分页切换正常工作，无视觉布局差异
