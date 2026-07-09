<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    title="批量新建用户"
    width="800px"
    :close-on-click-modal="false"
    destroy-on-close
  >
    <div class="batch-body">
      <!-- 步骤1：多行文本输入 -->
      <div v-if="!tableRows.length" class="batch-input-area">
        <div style="margin-bottom:12px">
          <span style="font-size:13px;color:#374151;font-weight:600;margin-right:8px">角色（可选）</span>
          <el-select v-model="defaultRoleId" placeholder="选择角色" size="small" clearable style="width:200px">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="String(r.id ?? '')" />
          </el-select>
        </div>
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
          <div style="display:flex;align-items:center;gap:8px">
            <el-button link type="primary" size="small" @click="copyAll">复制全部</el-button>
            <span class="batch-table-count">共 {{ tableRows.length }} 条</span>
          </div>
        </div>
        <el-table :data="tableRows" size="small" max-height="400">
          <el-table-column label="名称" min-width="90">
            <template #default="{ row }">
              <el-input v-model="row.displayName" size="small" />
            </template>
          </el-table-column>
          <el-table-column label="账户名称" min-width="110">
            <template #default="{ row }">
              <el-input v-model="row.username" size="small" />
            </template>
          </el-table-column>
          <el-table-column label="随机密码" min-width="130">
            <template #default="{ row }">
              <el-input v-model="row.password" size="small" show-password />
            </template>
          </el-table-column>
          <el-table-column label="角色" min-width="130">
            <template #default="{ row }">
              <el-select v-model="row.roleId" placeholder="选择" size="small" clearable style="width:100%">
                <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="String(r.id ?? '')" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90" align="center">
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
                :disabled="saving || row.status === 'creating'"
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
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { pinyin } from 'pinyin-pro'
import * as api from '@/api/client'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
  created: []
}>()

// ── 角色列表 ──
const roles = ref<api.RBACRoleRecord[]>([])
const defaultRoleId = ref('')

watch(() => props.visible, async (v) => {
  if (v) {
    rawInput.value = ''
    tableRows.value = []
    saving.value = false
    defaultRoleId.value = ''
    // 加载角色列表
    try {
      const res = await api.apiClient.getRBACRoles()
      roles.value = (res as api.PageResponse<api.RBACRoleRecord>).items ?? []
    } catch { /* 非关键 */ }
  }
})

// ── 文本输入 ──
const rawInput = ref('')

// ── 表格行 ──
interface BatchRow {
  displayName: string
  username: string
  password: string
  roleId: string
  status: 'pending' | 'creating' | 'success' | 'fail'
  error: string
}

const tableRows = ref<BatchRow[]>([])
const saving = ref(false)

const allDone = computed(() =>
  tableRows.value.length > 0 && tableRows.value.every(r => r.status !== 'pending')
)

function toPinyin(name: string): string {
  if (/^[a-zA-Z0-9]+$/.test(name)) return name
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
    roleId: defaultRoleId.value,
    status: 'pending' as const,
    error: '',
  }))
}

function copyAll() {
  const text = tableRows.value
    .map(r => {
      const roleName = roles.value.find(rr => String(rr.id) === r.roleId)?.name || ''
      const part = `名称:${r.displayName}  账户:${r.username}  密码:${r.password}`
      return roleName ? `${part}  角色:${roleName}` : part
    })
    .join('\n')
  navigator.clipboard.writeText(text).then(
    () => ElMessage.success('已复制'),
    () => ElMessage.error('复制失败')
  )
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
    let userId = ''
    try {
      const res = await api.apiClient.createUser({
        username: row.username.trim(),
        password: row.password,
        display_name: row.displayName.trim(),
      })
      // 获取新用户 ID
      const created = (res as any)?.user
      userId = String(created?.id ?? '')
      row.status = 'success'
    } catch (err) {
      row.status = 'fail'
      row.error = err instanceof Error ? err.message : '创建失败'
      continue
    }
    // 分配角色
    if (userId && row.roleId) {
      try {
        await api.apiClient.createRBACUserRole({
          user_id: userId,
          role_id: row.roleId,
        })
      } catch { /* 角色分配失败不影响创建成功 */ }
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
