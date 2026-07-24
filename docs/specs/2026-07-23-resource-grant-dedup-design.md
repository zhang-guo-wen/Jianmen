# 资源授权去重与批量授权 — 设计文档

> **历史设计说明**：本文保留资源授权去重功能的原始设计过程，其中
> `deleted_at` 方案已被最终的 `active_marker` 方案取代。当前生命周期
> 和索引语义以
> [标准化审计字段与停用标记设计](../2026-07-23-auditable-fields-design.md)
> 为准。

## 背景

权限管理 → 新增授权功能中，选完主体（用户/用户组）后，资源选择列表未排除该主体已有授权的资源。导致用户可能勾选已授权的资源并提交，虽然后端有唯一索引兜底，但体验很差。

## 目标

1. **前端**：选定主体后，资源列表中禁用已授权的资源行（不可勾选 + 视觉灰显）
2. **后端**：Service 层预检重复授权；对已有授权执行逻辑删除 + 重新插入（刷新授权）；新增批量授权接口，统一返回中文提示

## 设计决策

- **删除策略**：`deleted_at` DATETIME（NULL=激活，非NULL=已删除），保留删除时间用于审计
- **唯一索引**：包含 `deleted_at`，`(principal_type, principal_id, resource_type, resource_id, effect, deleted_at)`
- **批量接口**：前端改为一次提交所有选中的资源，后端事务内逐条处理
- **审计字段**：新增 `created_by`、`updated_by`

## 变更清单

### 1. Model — `internal/model/resource_grant.go`

| 字段 | 类型 | 说明 |
|------|------|------|
| `DeletedAt` | `gorm.DeletedAt` | 软删除标记，纳入唯一索引 priority 6 |
| `CreatedBy` | `string` | 创建人 ID |
| `UpdatedBy` | `string` | 最后更新人 ID |

唯一索引 `uidx_resource_grants_logic` 从 5 列扩展到 6 列，加入 `deleted_at`。

### 2. Store — `internal/store/dbstore_resource_grants.go`

新增方法：

```go
FindGrantsByPrincipal(ctx, principalType, principalID string) ([]model.ResourceGrant, error)
// 查询某主体所有未删除的授权
```

新增批量方法（事务内）：

```go
BatchUpsertGrants(ctx, grants []model.ResourceGrant, actorID string) (created int, refreshed int, error)
// 逐条处理：存在则软删+插入，不存在则直接插入
```

### 3. Service — `internal/service/resource_grant.go`

`Create` 方法保持不变（单条逻辑）。

新增：

```go
BatchCreate(ctx, actorID, bypass string, grants []model.ResourceGrant) (BatchResult, error)

type BatchResult struct {
    Created   int
    Refreshed int
}
```

逻辑：
1. 验证每条 grant 的 principal 和 resource 存在性
2. 调用 `repository.BatchUpsertGrants()` 执行批量处理
3. 返回创建/刷新计数

### 4. Handler — `internal/server/admin/resource_grant_handlers.go`

新增路由 `POST /api/resource-grants/batch`：

- 请求体：`{ "grants": [{ principal_type, principal_id, resource_type, resource_id, effect, expires_at }] }`
- 响应体：`{ "created": 2, "refreshed": 3, "message": "新增2项授权，刷新3项授权" }`
- 权限检查：`ActionRBACManage`

### 5. API Client — `web/src/api/client.ts`

```typescript
batchCreateResourceGrants(payload: { grants: ResourceGrantPayload[] }):
  Promise<{ created: number; refreshed: number; message: string }>
```

新增：

```typescript
getPrincipalGrants(principalType: string, principalId: string):
  Promise<ResourceGrantRecord[]>
// 调用 GET /api/resource-grants?principal_type=xxx&principal_id=xxx
```

### 6. 前端 — `web/src/views/ResourceGrantView.vue`

**saveGrant 改造**：
- 从循环调用 `createResourceGrant` 改为一次调用 `batchCreateResourceGrants`
- 根据响应中的 `message` 显示中文提示

**主体变更时禁用已授权行**：
- `watch(() => grantForm.principal_id)` → 拉取该主体的所有授权
- 构建 `authorizedResourceIds: Set<string>`（仅限当前 resourceTabType）
- `resourceRows` 中标记 `_disabled`
- `<el-table-column type="selection">` 添加 `:selectable` 属性
- CSS 灰显 disabled 行

## 数据流

```
用户操作                     前端                       API                       后端
─────────                   ─────                      ───                       ────
选择主体(用户/用户组)  →  watch principal_id
                        →  getPrincipalGrants()
                        →  标记已授权资源行disabled

选择资源 + 点击保存    →  batchCreateResourceGrants()  →  POST /batch
                                                          →  BatchCreate()
                                                            逐条预检:
                                                            有 → 软删+插入(refreshed++)
                                                            无 → 插入(created++)
                        ←  显示 message              ←  {created, refreshed, message}
```

## 错误处理

- 全部已授权 → 返回 `"全部资源已授权，刷新10项授权"`（created=0, refreshed=10）
- 部分已授权 → 返回 `"新增2项授权，刷新3项授权"`
- 全部新增 → 返回 `"新增5项授权"`
- principal/resource 不存在 → 返回 400 错误
- 事务失败 → 整体回滚，返回 500 错误
