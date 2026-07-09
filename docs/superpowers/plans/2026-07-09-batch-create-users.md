# 批量新建用户 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在用户管理页面新增批量新建弹窗，支持多行输入中文名 → 格式化表格（拼音账户名+随机密码）→ 逐条创建用户

**Architecture:** 新建一个 `BatchCreateUsersDialog.vue` 组件，嵌入 `UsersView.vue` 的 `#toolbar-extra` 插槽。组件内部管理格式化/编辑/逐条保存状态，复用现有的 `POST /api/users` 接口。前端使用 `pinyin-pro` 库做中文→拼音转换。

**Tech Stack:** Vue 3 Composition API + TypeScript + Element Plus + pinyin-pro

## Global Constraints

- 不修改后端代码，完全复用 `POST /api/users` 接口
- 新增依赖 `pinyin-pro`，使用 `pinyin-pro` 的 `pinyin()` 函数（无音调模式）
- 随机密码 12 位，使用 `crypto.getRandomValues()` 生成
- handler 文件不超过 500 行，因此新建独立组件而非内联到 UsersView.vue
- 中文回答用户，commit message 使用英文

---

### Task 1: 安装 pinyin-pro 依赖

**Files:**
- Modify: `web/package.json`

**Interfaces:**
- Produces: `pinyin-pro` 库可从 `pinyin-pro` 导入

- [ ] **Step 1: 运行 npm install 安装 pinyin-pro**

```bash
cd web && npm install pinyin-pro
```
Expected: 安装成功，`package.json` 和 `node_modules` 更新。

- [ ] **Step 2: 验证导入可用**

创建临时测试：在项目任意位置执行 `node -e "const { pinyin } = require('pinyin-pro'); console.log(pinyin('张三', { toneType: 'none', type: 'array' }))"` → 输出 `['zhang', 'san']`。
如果是 ESM 项目，使用 `node --input-type=module -e "import { pinyin } from 'pinyin-pro'; console.log(pinyin('张三', { toneType: 'none', type: 'array' }))"`。

- [ ] **Step 3: 提交**

```bash
git add web/package.json web/package-lock.json
git commit -m "chore: add pinyin-pro dependency for batch user creation

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 2: 新建 BatchCreateUsersDialog 组件

**Files:**
- Create: `web/src/components/BatchCreateUsersDialog.vue`

**Interfaces:**
- Produces: `BatchCreateUsersDialog` Vue 组件
  - Props: `visible: boolean`（控制弹窗显示）
  - Emits: `update:visible`（关闭弹窗），`created`（用户创建成功，通知父组件刷新列表）

- [ ] **Step 1: 创建组件骨架，包含"文本输入 → 格式化 → 表格 → 保存"交互骨架**

创建 `web/src/components/BatchCreateUsersDialog.vue`：

```vue
<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    title="批量新建用户"
    width="700px"
    :close-on-click-modal="false"
    destroy-on-close
  >
    <div class="batch-body">
      <!-- 步骤1：多行文本输入 -->
      <div v-if="!tableRows.length" class="batch-input-area">
        <el-input
          v-model="rawInput"
          type="textarea"
          :rows="10"
          placeholder="每行输入一个名字，例如：&#10;张三&#10;李四&#10;john"
        />
        <el-button type="primary" :disabled="!rawInput.trim()" style="margin-top: 12px" @click="formatRows">
          格式化
        </el-button>
      </div>

      <!-- 步骤2：可编辑表格 -->
      <div v-else class="batch-table-area">
        <div class="batch-table-toolbar">
          <el-button link type="primary" @click="backToInput">← 返回输入</el-button>
          <span class="batch-table-count">共 {{ tableRows.length }} 条</span>
        </div>
        <el-table :data="tableRows" size="small" max-height="400">
          <el-table-column label="名称" min-width="100">
            <template #default="{ row }">
              <el-input v-model="row.displayName" size="small" />
            </template>
          </el-table-column>
          <el-table-column label="账户名称" min-width="120">
            <template #default="{ row }">
              <el-input v-model="row.username" size="small" />
            </template>
          </el-table-column>
          <el-table-column label="随机密码" min-width="140">
            <template #default="{ row }">
              <el-input v-model="row.password" size="small" show-password />
            </template>
          </el-table-column>
          <el-table-column label="状态" width="100" align="center">
            <template #default="{ row }">
              <el-tag v-if="row.status === 'success'" type="success" size="small">已创建</el-tag>
              <el-tag v-else-if="row.status === 'creating'" type="warning" size="small">创建中</el-tag>
              <el-tag v-else-if="row.status === 'fail'" type="danger" size="small" class="fail-tag">
                <el-tooltip :content="row.error" placement="top">
                  <span>失败</span>
                </el-tooltip>
              </el-tag>
              <span v-else class="text-muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="60" align="center">
            <template #default="{ row, $index }">
              <el-button
                link
                type="danger"
                size="small"
                :disabled="row.status === 'creating'"
                @click="removeRow($index)"
              >
                删除
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <template #footer>
      <el-button @click="emit('update:visible', false)">取消</el-button>
      <el-button
        v-if="tableRows.length"
        type="primary"
        :loading="saving"
        :disabled="allDone"
        @click="saveAll"
      >
        保存
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { pinyin } from 'pinyin-pro'
import * as api from '@/api/client'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
  created: []
}>()

// ── 文本输入 ──
const rawInput = ref('')

// ── 表格行 ──
interface BatchRow {
  displayName: string
  username: string
  password: string
  status: 'pending' | 'creating' | 'success' | 'fail'
  error: string
}

const tableRows = ref<BatchRow[]>([])
const saving = ref(false)

const allDone = computed(() =>
  tableRows.value.length > 0 && tableRows.value.every(r => r.status !== 'pending')
)

function toPinyin(name: string): string {
  // 如果全是字母/数字，保留原样
  if (/^[a-zA-Z0-9]+$/.test(name)) return name
  // 拼音转换，无音调，合并为一个字符串
  const parts = pinyin(name, { toneType: 'none', type: 'array' })
  return parts.join('').replace(/\s+/g, '')
}

function genPassword(): string {
  const chars = 'ABCDEFGHJKMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*'
  const arr = new Uint8Array(12)
  crypto.getRandomValues(arr)
  let pwd = ''
  for (let i = 0; i < 12; i++) {
    pwd += chars[arr[i] % chars.length]
  }
  return pwd
}

function formatRows() {
  const lines = rawInput.value
    .split('\n')
    .map(l => l.trim())
    .filter(l => l)

  // 去重
  const seen = new Set<string>()
  const unique = lines.filter(l => {
    if (seen.has(l)) return false
    seen.add(l)
    return true
  })

  tableRows.value = unique.map(name => ({
    displayName: name,
    username: toPinyin(name),
    password: genPassword(),
    status: 'pending' as const,
    error: '',
  }))
}

function backToInput() {
  tableRows.value = []
}

function removeRow(index: number) {
  tableRows.value.splice(index, 1)
  if (!tableRows.value.length) {
    rawInput.value = ''
  }
}

async function saveAll() {
  saving.value = true
  for (const row of tableRows.value) {
    if (row.status === 'success') continue
    row.status = 'creating'
    row.error = ''
    try {
      await api.apiClient.createUser({
        username: row.username.trim(),
        password: row.password,
        display_name: row.displayName.trim(),
      })
      row.status = 'success'
    } catch (err) {
      row.status = 'fail'
      row.error = err instanceof Error ? err.message : '创建失败'
    }
  }
  saving.value = false
  if (tableRows.value.some(r => r.status === 'success')) {
    emit('created')
  }
}
</script>

<style scoped>
.batch-body {
  min-height: 200px;
}
.batch-input-area {
  display: flex;
  flex-direction: column;
}
.batch-table-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 10px;
}
.batch-table-count {
  font-size: 13px;
  color: #64748b;
}
.text-muted {
  font-size: 12px;
  color: #64748b;
}
.fail-tag {
  cursor: help;
}
</style>
```

- [ ] **Step 2: 运行 typecheck 验证组件无类型错误**

```bash
cd web && npm run typecheck
```
Expected: 通过，无类型错误。

- [ ] **Step 3: 提交**

```bash
git add web/src/components/BatchCreateUsersDialog.vue
git commit -m "feat: add BatchCreateUsersDialog component

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 3: 在 UsersView 中集成批量新建入口

**Files:**
- Modify: `web/src/views/UsersView.vue`

**Interfaces:**
- Consumes: `BatchCreateUsersDialog` 组件（visible prop, update:visible + created emits）
- 在现有 `#toolbar-extra` 插槽中添加"批量新建"按钮和弹窗

- [ ] **Step 1: 在 UsersView.vue 模板中添加批量新建按钮和弹窗**

在 `<template #toolbar-extra>` 内部，现有 `<el-button type="primary" @click="openCreateDialog">` 之后添加：

```vue
<el-button type="success" @click="batchDialogVisible = true">批量新建</el-button>
```

在 `</div>` (page-container 闭合标签) 之前（但仍在 template 内），角色分配弹窗 `</el-dialog>` 之后，`<FormDialog>` 之前添加：

```vue
<!-- Batch Create Dialog -->
<BatchCreateUsersDialog
  v-model:visible="batchDialogVisible"
  @created="loadUsers"
/>
```

- [ ] **Step 2: 在 script 中引入组件和添加响应式变量**

在 `<script setup>` 的 import 区域（现有 import 之后）添加：

```ts
import BatchCreateUsersDialog from '@/components/BatchCreateUsersDialog.vue'
```

在现有 `const dialogVisible = ref(false)` 附近添加：

```ts
const batchDialogVisible = ref(false)
```

- [ ] **Step 3: 运行 typecheck 验证**

```bash
cd web && npm run typecheck
```
Expected: 通过，无类型错误。

- [ ] **Step 4: 运行 build 验证**

```bash
cd web && npm run build
```
Expected: 构建成功。

- [ ] **Step 5: 提交**

```bash
git add web/src/views/UsersView.vue
git commit -m "feat: integrate batch create users dialog into UsersView

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 4: 验证与检查

**Files:** 无新文件

- [ ] **Step 1: 最终 typecheck**

```bash
cd web && npm run typecheck
```
Expected: 通过。

- [ ] **Step 2: 最终 build**

```bash
cd web && npm run build
```
Expected: 构建成功。

- [ ] **Step 3: 运行后端测试（确保无回归）**

```bash
go test ./... -count=1
```
Expected: 通过。
