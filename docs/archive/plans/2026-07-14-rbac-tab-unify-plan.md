# 权限管理 Tab 样式统一 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将权限管理下用户组、资源授权、资源分组、账号分组 4 个 Tab 统一迁移到 DataTableCard 组件，增加搜索和分页功能。

**Architecture:** 后端 3 个 GET 接口增加 `q`/`page`/`page_size` 参数，返回 `{ items, total }` 分页结构；前端 4 个视图统一使用 `DataTableCard` 组件替代裸 `page-card`，API 客户端同步更新返回值类型。

**Tech Stack:** Go (GORM + net/http), Vue 3 + Element Plus + TypeScript

## Global Constraints

- 本产品未发布，不需要兼容旧数据
- 遵循 CLAUDE.md 中的 Go 后端开发规范
- 前端修改后必须通过 `npm run typecheck` 和 `npm run build`
- 后端修改后必须通过 `go build ./...` 和 `go test ./... -count=1`
- 资源授权的搜索需要跨表 JOIN（principal_id/resource_id 是 UUID）
- 资源分组和账号分组通过 `group_type` 参数区分，不再前端过滤

---

### Task 1: 后端 — 用户组列表 API 增加搜索和分页

**Files:**
- Modify: `internal/server/admin/user_group_handlers.go:95-108`

**Interfaces:**
- Consumes: `positiveIntRequestQuery(r, "page", 1)` and `positiveIntRequestQuery(r, "page_size", 20)` from `request_utils.go`
- Consumes: `pageResponse{Items, Total, Page, PageSize}` from `server.go:65-70`
- Produces: `GET /api/user-groups?q=&page=1&page_size=20` → `{ "items": [...UserGroup], "total": N, "page": 1, "page_size": 20 }`

- [ ] **Step 1: 修改 `listUserGroups` 函数，增加搜索和分页逻辑**

将 `listUserGroups` 函数替换为：

```go
func (s *Server) listUserGroups(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tx := s.db.Model(&model.UserGroup{})
	if q != "" {
		like := "%" + q + "%"
		tx = tx.Where("name LIKE ? OR description LIKE ?", like, like)
	}

	var total int64
	tx.Count(&total)

	page := positiveIntRequestQuery(r, "page", 1)
	pageSize := positiveIntRequestQuery(r, "page_size", 20)
	if pageSize > 200 {
		pageSize = 200
	}

	var groups []model.UserGroup
	if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&groups).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, pageResponse{Items: groups, Total: int(total), Page: page, PageSize: pageSize})
}
```

注意需要在文件顶部 import 中添加 `"strings"`（如果尚未添加）。

- [ ] **Step 2: 编译验证后端**

```bash
go build ./...
```

预期：编译成功。

- [ ] **Step 3: 运行后端测试**

```bash
go test ./... -count=1
```

预期：所有测试通过。

- [ ] **Step 4: 提交**

```bash
git add internal/server/admin/user_group_handlers.go
git commit -m "feat: 用户组列表API增加搜索和分页

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: 后端 — 资源分组列表 API 增加搜索和分页

**Files:**
- Modify: `internal/server/admin/resource_group_handlers.go:45-87`

**Interfaces:**
- Consumes: `positiveIntRequestQuery(r, "page", 1)`, `positiveIntRequestQuery(r, "page_size", 20)` from `request_utils.go`
- Consumes: `pageResponse{Items, Total, Page, PageSize}` from `server.go:65-70`
- Produces: `GET /api/resource-groups?group_type=resource&q=&page=1&page_size=20` → `{ "items": [...ResourceGroupWithCount], "total": N }`

- [ ] **Step 1: 修改 `listResourceGroups` 函数，增加搜索和分页逻辑**

将 `listResourceGroups` 函数（第 45-87 行）替换为：

```go
func (s *Server) listResourceGroups(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	groupType := r.URL.Query().Get("group_type")
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	tx := s.db.Model(&model.ResourceGroup{})
	if groupType != "" {
		tx = tx.Where("group_type = ?", groupType)
	}
	if q != "" {
		like := "%" + q + "%"
		tx = tx.Where("name LIKE ? OR description LIKE ?", like, like)
	}

	var total int64
	tx.Count(&total)

	page := positiveIntRequestQuery(r, "page", 1)
	pageSize := positiveIntRequestQuery(r, "page_size", 20)
	if pageSize > 200 {
		pageSize = 200
	}

	var groups []model.ResourceGroup
	if err := tx.Order("group_type, name").Offset((page - 1) * pageSize).Limit(pageSize).Find(&groups).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	type groupWithCount struct {
		model.ResourceGroup
		HostCount     int64 `json:"host_count"`
		DatabaseCount int64 `json:"database_count"`
		AccountCount  int64 `json:"account_count"`
	}

	result := make([]groupWithCount, 0, len(groups))
	for _, g := range groups {
		gwc := groupWithCount{ResourceGroup: g}
		if g.GroupType == model.ResourceGroupTypeResource {
			s.db.Model(&model.Host{}).Where("group_name = ?", g.Name).Count(&gwc.HostCount)
			s.db.Model(&model.DatabaseInstance{}).Where("group_name = ?", g.Name).Count(&gwc.DatabaseCount)
		} else {
			s.db.Model(&model.HostAccount{}).Where("group_name = ?", g.Name).Count(&gwc.AccountCount)
			var dbCount int64
			s.db.Model(&model.DatabaseAccount{}).Where("group_name = ?", g.Name).Count(&dbCount)
			gwc.AccountCount += dbCount
		}
		result = append(result, gwc)
	}

	s.writeJSON(w, r, http.StatusOK, pageResponse{Items: result, Total: int(total), Page: page, PageSize: pageSize})
}
```

- [ ] **Step 2: 编译验证后端**

```bash
go build ./...
```

预期：编译成功。

- [ ] **Step 3: 运行后端测试**

```bash
go test ./... -count=1
```

预期：所有测试通过。

- [ ] **Step 4: 提交**

```bash
git add internal/server/admin/resource_group_handlers.go
git commit -m "feat: 资源分组列表API增加搜索和分页

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: 后端 — 资源授权列表 API 增加搜索和分页，删除旧筛选参数

**Files:**
- Modify: `internal/server/admin/resource_grant_handlers.go:73-108`

**Interfaces:**
- Consumes: `positiveIntRequestQuery(r, "page", 1)`, `positiveIntRequestQuery(r, "page_size", 20)` from `request_utils.go`
- Consumes: `pageResponse{Items, Total, Page, PageSize}` from `server.go:65-70`
- Produces: `GET /api/resource-grants?q=&page=1&page_size=20` → `{ "items": [...ResourceGrant], "total": N }`
- 删除 `principal_type`, `principal_id`, `resource_type`, `resource_id` 筛选参数

- [ ] **Step 1: 修改 `listResourceGrants` 函数**

将 `listResourceGrants` 函数（第 73-108 行）替换为：

```go
func (s *Server) listResourceGrants(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "database not available")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tx := s.db.Model(&model.ResourceGrant{})

	if q != "" {
		like := "%" + q + "%"
		// 搜索匹配的主体（用户名/用户组名）
		var principalUserIDs []string
		s.db.Model(&model.User{}).Where("username LIKE ?", like).Pluck("id", &principalUserIDs)
		var principalGroupIDs []string
		s.db.Model(&model.UserGroup{}).Where("name LIKE ?", like).Pluck("id", &principalGroupIDs)
		principalIDs := append(principalUserIDs, principalGroupIDs...)

		// 搜索匹配的资源（主机账号、数据库账号、资源分组）
		var resourceHostAccountIDs []string
		s.db.Model(&model.HostAccount{}).Where("username LIKE ? OR host LIKE ?", like, like).Pluck("id", &resourceHostAccountIDs)
		var resourceDBAccountIDs []string
		s.db.Model(&model.DatabaseAccount{}).Where("unique_name LIKE ?", like).Pluck("id", &resourceDBAccountIDs)
		var resourceGroupIDs []string
		s.db.Model(&model.ResourceGroup{}).Where("name LIKE ?", like).Pluck("id", &resourceGroupIDs)
		resourceIDs := append(resourceHostAccountIDs, resourceDBAccountIDs...)
		resourceIDs = append(resourceIDs, resourceGroupIDs...)

		// 组合搜索条件
		conditions := make([]string, 0)
		args := make([]interface{}, 0)
		if len(principalIDs) > 0 {
			conditions = append(conditions, "principal_id IN ?")
			args = append(args, principalIDs)
		}
		if len(resourceIDs) > 0 {
			conditions = append(conditions, "resource_id IN ?")
			args = append(args, resourceIDs)
		}
		if len(conditions) > 0 {
			tx = tx.Where(strings.Join(conditions, " OR "), args...)
		} else {
			// 没有匹配项时返回空结果
			tx = tx.Where("1 = 0")
		}
	}

	var total int64
	tx.Count(&total)

	page := positiveIntRequestQuery(r, "page", 1)
	pageSize := positiveIntRequestQuery(r, "page_size", 20)
	if pageSize > 200 {
		pageSize = 200
	}

	var grants []model.ResourceGrant
	if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&grants).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, pageResponse{Items: grants, Total: int(total), Page: page, PageSize: pageSize})
}
```

- [ ] **Step 2: 编译验证后端**

```bash
go build ./...
```

预期：编译成功。

- [ ] **Step 3: 运行后端测试**

```bash
go test ./... -count=1
```

预期：所有测试通过。

- [ ] **Step 4: 提交**

```bash
git add internal/server/admin/resource_grant_handlers.go
git commit -m "feat: 资源授权列表API增加搜索和分页，删除旧筛选参数

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: 前端 — API 客户端类型和函数签名更新

**Files:**
- Modify: `web/src/api/client.ts:1007-1053`

**Interfaces:**
- Consumes: `UserGroupRecord`, `ResourceGrantRecord`, `ResourceGroupRecord` types (already defined)
- Consumes: `buildQS` helper (already defined)
- Produces: Updated return types from `T[]` to `{ items: T[]; total: number }`

- [ ] **Step 1: 更新 getUserGroups 函数签名和返回值类型**

将第 1008-1009 行替换为：

```typescript
  getUserGroups: (params?: { q?: string; page?: number; page_size?: number }) =>
    request<{ items: UserGroupRecord[]; total: number }>(`/api/user-groups${buildQS(params as Record<string, string | number | undefined>)}`),
```

- [ ] **Step 2: 更新 getResourceGrants 函数，移除旧筛选参数，返回值改为分页结构**

将第 1037-1038 行替换为：

```typescript
  getResourceGrants: (params?: { q?: string; page?: number; page_size?: number }) =>
    request<{ items: ResourceGrantRecord[]; total: number }>(`/api/resource-grants${buildQS(params as Record<string, string | number | undefined>)}`),
```

- [ ] **Step 3: 更新 getResourceGroups 函数，增加搜索和分页参数**

将第 1052-1053 行替换为：

```typescript
  getResourceGroups: (params?: { group_type?: string; q?: string; page?: number; page_size?: number }) =>
    request<{ items: ResourceGroupRecord[]; total: number }>(`/api/resource-groups${buildQS(params as Record<string, string | number | undefined>)}`),
```

- [ ] **Step 4: 验证前端类型检查**

```bash
npm run typecheck
```

预期：可能会有类型错误，因为调用了旧签名的地方尚未更新。记录这些错误供后续 Task 修复。

- [ ] **Step 5: 提交**

```bash
git add web/src/api/client.ts
git commit -m "feat: API客户端更新用户组/资源授权/资源分组接口为分页结构

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: 前端 — UserGroupsView 迁移到 DataTableCard

**Files:**
- Modify: `web/src/views/UserGroupsView.vue`

**Interfaces:**
- Consumes: `DataTableCard` component (search + pagination)
- Consumes: Updated `apiClient.getUserGroups()` returning `{ items, total }`
- Produces: 用户组列表支持搜索和分页

- [ ] **Step 1: 重写 UserGroupsView.vue 模板部分，使用 DataTableCard**

将 `<template>` 部分（第 1-103 行）替换为：

```vue
<template>
  <div class="view-stack">
    <div class="page-container">
      <DataTableCard
        :data="groups"
        :loading="loading"
        :total="total"
        v-model:page="page"
        v-model:page-size="pageSize"
        search-placeholder="搜索用户组名称、描述..."
        @search="onSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="showGroupDialog()">
            <el-icon><Plus /></el-icon>
            {{ t('resourceGrant.addGroup') }}
          </el-button>
        </template>

        <el-table-column :label="t('resourceGrant.groupName')" prop="name" min-width="150" />
        <el-table-column :label="t('resourceGrant.groupDescription')" prop="description" min-width="200" />
        <el-table-column :label="t('resourceGrant.memberCount')" width="120">
          <template #default="{ row }">
            <el-button link type="primary" @click="showMembers(row)">
              {{ getMemberCount(row.id) }} {{ t('resourceGrant.members') }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showGroupDialog(row)">
              {{ t('common.edit') }}
            </el-button>
            <el-button type="danger" link size="small" @click="deleteGroup(row)">
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>

      <!-- 创建/编辑用户组对话框（保持不变） -->
      <el-dialog ...>
      ...
      </el-dialog>

      <!-- 用户组成员管理对话框（保持不变） -->
      <el-dialog ...>
      ...
      </el-dialog>
    </div>
  </div>
</template>
```

- [ ] **Step 2: 更新 script 部分，增加分页和搜索状态，修改 loadGroups**

在 `<script setup>` 中：
1. 导入 `DataTableCard`：

```typescript
import DataTableCard from '@/components/DataTableCard.vue'
```

2. 增加分页和搜索相关状态（在 `const loading = ref(false)` 之后）：

```typescript
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const keyword = ref('')
```

3. 修改 `loadGroups` 函数为分页加载：

```typescript
const loadGroups = async () => {
  loading.value = true
  try {
    const res = await apiClient.getUserGroups({
      page: page.value,
      page_size: pageSize.value,
      q: keyword.value || undefined,
    })
    groups.value = res.items ?? []
    total.value = res.total ?? 0
    for (const group of groups.value) {
      try {
        const members = await apiClient.getUserGroupMembers(group.id)
        groupMembers.value[group.id] = members
      } catch {
        groupMembers.value[group.id] = []
      }
    }
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load groups')
  } finally {
    loading.value = false
  }
}
```

4. 增加搜索回调函数：

```typescript
const onSearch = (q: string) => {
  keyword.value = q
  page.value = 1
  loadGroups()
}
```

5. 增加分页变化监听：

```typescript
import { watch } from 'vue'
watch([page, pageSize], () => loadGroups())
```

- [ ] **Step 3: 删除旧的 page-card 相关样式，保留成员管理相关样式**

确保 `<style scoped>` 中删除不再需要的 `page-card` 自定义样式。

- [ ] **Step 4: 验证前端类型检查和编译**

```bash
npm run typecheck
npm run build
```

预期：无类型错误，编译成功。

- [ ] **Step 5: 提交**

```bash
git add web/src/views/UserGroupsView.vue
git commit -m "feat: 用户组列表迁移到DataTableCard，支持搜索和分页

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: 前端 — ResourceGroupsContent 迁移到 DataTableCard

**Files:**
- Modify: `web/src/components/ResourceGroupsContent.vue`

**Interfaces:**
- Consumes: `DataTableCard` component
- Consumes: Updated `apiClient.getResourceGroups({ group_type: 'resource', ... })` returning `{ items, total }`
- Produces: 资源分组列表支持搜索和分页，不再前端过滤 group_type

- [ ] **Step 1: 重写 ResourceGroupsContent.vue 模板，使用 DataTableCard**

将 `<template>` 部分替换为：

```vue
<template>
  <div class="view-stack">
    <div class="page-container">
      <DataTableCard
        :data="groups"
        :loading="loading"
        :total="total"
        v-model:page="page"
        v-model:page-size="pageSize"
        search-placeholder="搜索分组名称、描述..."
        @search="onSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="showCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ t('resourceGroups.create') }}
          </el-button>
        </template>

        <el-table-column :label="t('resourceGroups.name')" prop="name" min-width="150" />
        <el-table-column :label="t('resourceGroups.description')" prop="description" min-width="200" show-overflow-tooltip />
        <el-table-column :label="t('resourceGroups.hostCount')" width="80">
          <template #default="{ row }">
            {{ row.host_count || 0 }}
          </template>
        </el-table-column>
        <el-table-column :label="t('resourceGroups.databaseCount')" width="110">
          <template #default="{ row }">
            {{ row.database_count || 0 }}
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="showEditDialog(row)">
              {{ t('common.edit') }}
            </el-button>
            <el-button link type="danger" size="small" @click="deleteGroup(row)">
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>

      <!-- 创建/编辑对话框保持不变 -->
      <el-dialog ...>
      ...
      </el-dialog>
    </div>
  </div>
</template>
```

- [ ] **Step 2: 更新 script 部分**

在 `<script setup>` 中：
1. 导入 `DataTableCard`：

```typescript
import DataTableCard from '@/components/DataTableCard.vue'
```

2. 增加分页和搜索状态：

```typescript
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const keyword = ref('')
```

3. 修改 `loadGroups` 函数：

```typescript
const loadGroups = async () => {
  loading.value = true
  try {
    const res = await apiClient.getResourceGroups({
      group_type: 'resource',
      page: page.value,
      page_size: pageSize.value,
      q: keyword.value || undefined,
    })
    groups.value = res.items ?? []
    total.value = res.total ?? 0
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load groups')
  } finally {
    loading.value = false
  }
}
```

4. 增加搜索和分页回调：

```typescript
const onSearch = (q: string) => {
  keyword.value = q
  page.value = 1
  loadGroups()
}

import { watch, onMounted } from 'vue'
watch([page, pageSize], () => loadGroups())
```

- [ ] **Step 3: 验证前端类型检查和编译**

```bash
npm run typecheck
npm run build
```

预期：无类型错误，编译成功。

- [ ] **Step 4: 提交**

```bash
git add web/src/components/ResourceGroupsContent.vue
git commit -m "feat: 资源分组列表迁移到DataTableCard，支持搜索和分页

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: 前端 — AccountGroupsContent 迁移到 DataTableCard

**Files:**
- Modify: `web/src/components/AccountGroupsContent.vue`

**Interfaces:**
- Consumes: `DataTableCard` component
- Consumes: Updated `apiClient.getResourceGroups({ group_type: 'account', ... })` returning `{ items, total }`
- Produces: 账号分组列表支持搜索和分页，不再前端过滤 group_type

- [ ] **Step 1: 重写 AccountGroupsContent.vue 模板，使用 DataTableCard**

将 `<template>` 部分替换为：

```vue
<template>
  <div class="view-stack">
    <div class="page-container">
      <DataTableCard
        :data="groups"
        :loading="loading"
        :total="total"
        v-model:page="page"
        v-model:page-size="pageSize"
        search-placeholder="搜索分组名称、描述..."
        @search="onSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="showCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ t('resourceGroups.create') }}
          </el-button>
        </template>

        <el-table-column :label="t('resourceGroups.name')" prop="name" min-width="150" />
        <el-table-column :label="t('resourceGroups.description')" prop="description" min-width="200" show-overflow-tooltip />
        <el-table-column :label="t('resourceGroups.accountCount')" width="80">
          <template #default="{ row }">
            {{ row.account_count || 0 }}
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="showEditDialog(row)">
              {{ t('common.edit') }}
            </el-button>
            <el-button link type="danger" size="small" @click="deleteGroup(row)">
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>

      <!-- 创建/编辑对话框保持不变 -->
      <el-dialog ...>
      ...
      </el-dialog>
    </div>
  </div>
</template>
```

- [ ] **Step 2: 更新 script 部分**

与 Task 6 相同的模式，将 `group_type` 改为 `'account'`：

1. 导入 `DataTableCard`
2. 增加 `page`、`pageSize`、`total`、`keyword` 状态
3. 修改 `loadGroups`：

```typescript
const loadGroups = async () => {
  loading.value = true
  try {
    const res = await apiClient.getResourceGroups({
      group_type: 'account',
      page: page.value,
      page_size: pageSize.value,
      q: keyword.value || undefined,
    })
    groups.value = res.items ?? []
    total.value = res.total ?? 0
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load groups')
  } finally {
    loading.value = false
  }
}
```

4. 增加 `onSearch` 和 `watch([page, pageSize], ...)`：

```typescript
const onSearch = (q: string) => {
  keyword.value = q
  page.value = 1
  loadGroups()
}

import { watch, onMounted } from 'vue'
watch([page, pageSize], () => loadGroups())
```

- [ ] **Step 3: 验证前端类型检查和编译**

```bash
npm run typecheck
npm run build
```

预期：无类型错误，编译成功。

- [ ] **Step 4: 提交**

```bash
git add web/src/components/AccountGroupsContent.vue
git commit -m "feat: 账号分组列表迁移到DataTableCard，支持搜索和分页

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 8: 前端 — ResourceGrantView 迁移到 DataTableCard，去掉筛选下拉

**Files:**
- Modify: `web/src/views/ResourceGrantView.vue`

**Interfaces:**
- Consumes: `DataTableCard` component
- Consumes: Updated `apiClient.getResourceGrants({ q, page, page_size })` returning `{ items, total }`
- Produces: 资源授权列表支持搜索和分页，删除筛选下拉框

- [ ] **Step 1: 重写 ResourceGrantView.vue 模板，使用 DataTableCard**

将 `<template>` 中列表部分（第 2-75 行）替换为使用 DataTableCard，删除 `filters` 和旧搜索按钮，保留创建授权对话框（第 78-217 行）：

```vue
<template>
  <div class="view-stack">
    <div class="page-container">
      <DataTableCard
        :data="grants"
        :loading="loading"
        :total="total"
        v-model:page="page"
        v-model:page-size="pageSize"
        search-placeholder="搜索主体名称、资源名称..."
        @search="onSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="showGrantDialog()">
            <el-icon><Plus /></el-icon>
            {{ t('resourceGrant.addGrant') }}
          </el-button>
        </template>

        <el-table-column :label="t('resourceGrant.principalType')" prop="principal_type" width="100">
          <template #default="{ row }">
            <el-tag :type="row.principal_type === 'user' ? 'primary' : 'success'" size="small">
              {{ row.principal_type === 'user' ? t('resourceGrant.user') : t('resourceGrant.userGroup') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('resourceGrant.principalName')" min-width="120">
          <template #default="{ row }">
            {{ getPrincipalName(row) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('resourceGrant.resourceType')" width="120">
          <template #default="{ row }">
            <el-tag size="small">{{ resourceTypeLabel(row.resource_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('resourceGrant.resourceName')" min-width="150">
          <template #default="{ row }">
            {{ getResourceName(row) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('resourceGrant.effect')" width="80">
          <template #default="{ row }">
            <el-tag :type="row.effect === 'allow' ? 'success' : 'danger'" size="small">
              {{ row.effect === 'allow' ? t('resourceGrant.allow') : t('resourceGrant.deny') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('resourceGrant.expiresAt')" width="180">
          <template #default="{ row }">
            {{ row.expires_at ? formatTime(row.expires_at) : t('resourceGrant.never') }}
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="80" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="deleteGrant(row)">
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>

      <!-- 创建资源授权对话框（保持不变） -->
      <el-dialog
        v-model="grantDialogVisible"
        ...
      >
      ...
      </el-dialog>
    </div>
  </div>
</template>
```

- [ ] **Step 2: 更新 script 部分**

1. 导入 `DataTableCard`，删除不再需要的 `Search` 图标导入：

```typescript
import DataTableCard from '@/components/DataTableCard.vue'
// 删除: import { Search } from '@element-plus/icons-vue'
// 如果 Plus 仍在使用则保留: import { Plus } from '@element-plus/icons-vue'
```

2. 删除 `filters` reactive 对象（`principal_type`, `resource_type`），替换为分页和搜索状态：

```typescript
// 删除:
// const filters = reactive({
//   principal_type: '',
//   resource_type: ''
// })

// 新增:
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const keyword = ref('')
```

3. 修改 `loadGrants` 函数：

```typescript
const loadGrants = async () => {
  loading.value = true
  try {
    const res = await apiClient.getResourceGrants({
      page: page.value,
      page_size: pageSize.value,
      q: keyword.value || undefined,
    })
    grants.value = res.items ?? []
    total.value = res.total ?? 0
    // 预加载用户和用户组用于名称显示
    await ensureNamesLoaded()
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load grants')
  } finally {
    loading.value = false
  }
}
```

4. 增加 `ensureNamesLoaded` 函数（替代旧的 `ensureResourcesLoaded`）：

```typescript
const ensureNamesLoaded = async () => {
  if (allUsers.value.length === 0) await loadUsers()
  if (userGroups.value.length === 0) await loadUserGroups()
}
```

5. 增加搜索回调：

```typescript
const onSearch = (q: string) => {
  keyword.value = q
  page.value = 1
  loadGrants()
}
```

6. 增加分页监听：

```typescript
import { watch } from 'vue'
watch([page, pageSize], () => loadGrants())
```

- [ ] **Step 3: 清理不再需要的样式**

删除 `.filters` 样式定义（不再使用筛选下拉框）。

- [ ] **Step 4: 验证前端类型检查和编译**

```bash
npm run typecheck
npm run build
```

预期：无类型错误，编译成功。

- [ ] **Step 5: 提交**

```bash
git add web/src/views/ResourceGrantView.vue
git commit -m "feat: 资源授权列表迁移到DataTableCard，去掉筛选下拉，支持搜索和分页

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 9: 全面验证

**Files:** 无新建文件，验证所有改动。

- [ ] **Step 1: 后端编译和测试**

```bash
go build ./...
go test ./... -count=1
```

预期：编译成功，所有测试通过。

- [ ] **Step 2: 前端类型检查和编译**

```bash
npm run typecheck
npm run build
```

预期：无类型错误，编译成功。

- [ ] **Step 3: 手动功能验证清单**

启动服务后，依次进入权限管理每个 Tab：
1. 用户管理 — 搜索和分页正常（未改动，确认未破坏）
2. 角色管理 — 搜索和分页正常（未改动，确认未破坏）
3. 用户组 — 搜索框输入回车触发搜索，分页切换正常
4. 资源授权 — 搜索框输入回车触发搜索，分页切换正常，不再显示旧筛选下拉
5. 资源分组 — 搜索框输入回车触发搜索，分页切换正常
6. 账号分组 — 搜索框输入回车触发搜索，分页切换正常
