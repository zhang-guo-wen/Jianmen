<template>
  <div class="view-stack">
    <DataTableCard
      :data="instances"
      :loading="instancesLoading"
      :total="instanceTotal"
      v-model:page="instancePage"
      v-model:page-size="instancePageSize"
      search-placeholder="搜索实例名称、地址、协议..."
      @search="onInstanceSearch"
    >
      <template #toolbar-extra>
        <el-button type="primary" @click="openCreateInstance">新增实例</el-button>
      </template>
      <el-table-column prop="name" label="名称" min-width="130" show-overflow-tooltip />
      <el-table-column label="协议" width="80" align="center">
        <template #default="{ row }">
          <el-tag size="small" :type="row.protocol === 'mysql' ? 'success' : 'primary'" effect="plain">{{ row.protocol === 'mysql' ? 'MySQL' : 'PG' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="address" label="地址" min-width="130" show-overflow-tooltip />
      <el-table-column prop="port" label="端口" width="70" />
      <el-table-column label="账号数" width="80" align="center">
        <template #default="{ row }">
          <el-button link type="primary" @click="showAccounts(row)">{{ row.account_count ?? 0 }}</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="group" label="分组" width="100" show-overflow-tooltip />
      <el-table-column label="状态" width="70" align="center">
        <template #default="{ row }">
          <StatusSwitch :model-value="row.status === 'active'" @update:model-value="toggleInstance(row)" />
        </template>
      </el-table-column>
      <el-table-column prop="remark" label="备注" min-width="120" show-overflow-tooltip />
      <el-table-column label="操作" width="150">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="editInstance(row)">编辑</el-button>
          <el-button link type="danger" size="small" @click="deleteInstance(row)">删除</el-button>
        </template>
      </el-table-column>
    </DataTableCard>

    <!-- 创建/编辑实例弹窗 -->
    <FormDialog v-model:visible="showInstanceDialog" :title="editingInstance ? '编辑实例' : '新增实例'" width="640px" :loading="submitting" @submit="submitInstance">
      <el-form :model="instanceForm" label-width="80px">
        <el-form-item label="名称" required>
          <el-input v-model="instanceForm.name" />
        </el-form-item>
        <el-form-item label="协议" required>
          <el-select v-model="instanceForm.protocol">
            <el-option label="MySQL" value="mysql" />
            <el-option label="PostgreSQL" value="postgres" />
          </el-select>
        </el-form-item>
        <el-form-item label="上游地址" required>
          <el-input v-model="instanceForm.address" placeholder="host:port 或 IP" />
        </el-form-item>
        <el-form-item label="端口">
          <el-input-number v-model="instanceForm.port" :min="1" :max="65535" />
        </el-form-item>
        <el-collapse>
          <el-collapse-item title="更多设置">
            <el-form-item label="分组">
              <el-input v-model="instanceForm.group" />
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="instanceForm.remark" type="textarea" />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
    </FormDialog>

    <!-- 账号列表弹窗 -->
    <el-dialog
      v-model="accountsDialogVisible"
      :title="accountsDialogTitle"
      destroy-on-close
      width="min(960px, calc(100vw - 32px))"
    >
      <DataTableCard
        :data="accounts"
        :loading="accountsLoading"
        :total="accountTotal"
        v-model:page="accountPage"
        v-model:page-size="accountPageSize"
        :show-search="false"
        row-key="id"
      >
        <template #toolbar-extra>
          <el-button :loading="accountsLoading" @click="loadSelectedInstanceAccounts">刷新</el-button>
          <el-button type="primary" :disabled="!selectedInstance" @click="openCreateAccount">
            新增账号
          </el-button>
        </template>
        <el-table-column label="连接账号" min-width="130">
          <template #default="{ row }">{{ row.username || '-' }}</template>
        </el-table-column>
        <el-table-column label="分组" width="110">
          <template #default="{ row }">{{ row.group || '-' }}</template>
        </el-table-column>
        <el-table-column label="状态" width="80" align="center">
          <template #default="{ row }">
            <StatusSwitch
              :model-value="row.status === 'active'"
              :loading="statusUpdatingId === row.id"
              @update:model-value="(val: boolean) => toggleAccountStatus(row, val)"
            />
          </template>
        </el-table-column>
        <el-table-column label="过期时间" min-width="140">
          <template #default="{ row }">{{ row.expires_at ? new Date(row.expires_at).toLocaleString() : '永久' }}</template>
        </el-table-column>
        <el-table-column label="备注" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button link type="success" size="small" @click="openConnectDialog(row)">连接</el-button>
            <el-button link type="primary" size="small" @click="editAccount(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="deleteAccount(row)">删除</el-button>
          </template>
        </el-table-column>
      </DataTableCard>
    </el-dialog>

    <!-- 创建/编辑账号弹窗 -->
    <FormDialog
      v-model:visible="accountDialogVisible"
      :title="editingAccount ? '编辑账号' : '新增账号'"
      width="620px"
      :loading="accountSubmitting"
      @submit="submitAccount"
    >
      <el-form :model="accountForm" label-width="100px">
        <el-form-item label="目标用户名" required>
          <el-input v-model="accountForm.username" placeholder="数据库登录用户名" />
        </el-form-item>
        <el-form-item label="目标密码">
          <el-input v-model="accountForm.password" type="password" show-password
            :placeholder="editingAccount ? '留空则保留原密码' : '数据库登录密码'" />
        </el-form-item>
        <el-form-item label="有效期">
          <div style="display:flex;gap:8px;flex-wrap:wrap">
            <el-button v-for="opt in expiryOptions" :key="opt.label" size="small"
              :type="expiryPreset === opt.label ? 'primary' : ''"
              @click="setExpiry(opt)">{{ opt.label }}</el-button>
          </div>
          <el-date-picker v-model="accountForm.expiresAt" type="datetime"
            placeholder="自定义时间" style="margin-top:8px;width:100%" />
        </el-form-item>
        <el-collapse>
          <el-collapse-item title="更多设置">
            <el-form-item label="分组">
              <el-input v-model="accountForm.group" placeholder="输入或选择分组" />
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="accountForm.remark" type="textarea" placeholder="备注信息" />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
    </FormDialog>

    <!-- 连接弹窗 -->
    <el-dialog
      v-model="connectDialogVisible"
      title="数据库连接"
      class="form-dialog"
      destroy-on-close
      width="min(560px, calc(100vw - 32px))"
      @opened="onConnectDialogOpened"
    >
      <div v-if="connectTarget" class="dialog-stack">
        <section class="connect-section">
          <div class="connect-section-title">连接状态</div>
          <div class="connect-status-card" v-loading="connectTesting">
            <template v-if="connectTestResult !== null">
              <el-tag :type="connectTestResult.ok ? 'success' : 'danger'" size="small">
                {{ connectTestResult.ok ? '可达' : '不可达' }}
              </el-tag>
              <span class="connect-latency" v-if="connectTestResult.latency_ms !== undefined" style="margin-left: 8px;">
                延迟: {{ connectTestResult.latency_ms }}ms
              </span>
              <div class="connect-error" v-if="connectTestResult.error" style="margin-top: 4px; color: var(--el-color-danger); font-size: 12px;">
                {{ connectTestResult.error }}
              </div>
            </template>
          </div>
          <div class="connect-status-tags" style="margin-top: 8px;">
            <el-tag :type="connectTarget.status === 'active' ? 'success' : 'info'" size="small">
              {{ connectTarget.status === 'active' ? '启用' : '禁用' }}
            </el-tag>
            <el-tag v-if="isExpired(connectTarget.expires_at)" type="danger" size="small" style="margin-left: 4px;">
              已过期
            </el-tag>
            <el-tag v-else-if="connectTarget.expires_at" type="warning" size="small" style="margin-left: 4px;">
              过期: {{ formatTime(connectTarget.expires_at) }}
            </el-tag>
          </div>
        </section>

        <section class="connect-section">
          <div class="connect-section-title">连接参数</div>
          <div class="config-row" v-for="param in connectParams" :key="param.label">
            <div class="config-label">{{ param.label }}</div>
            <el-input :model-value="param.value" readonly>
              <template #append>
                <el-tooltip content="复制">
                  <el-button aria-label="复制" @click="copyText(param.value)" />
                </el-tooltip>
              </template>
            </el-input>
          </div>
        </section>

        <section class="connect-section">
          <div class="connect-section-title">连接命令</div>
          <div class="config-row">
            <div class="config-label">Shell</div>
            <el-input :model-value="connectCommand" readonly>
              <template #append>
                <el-tooltip content="复制">
                  <el-button aria-label="复制" @click="copyText(connectCommand)" />
                </el-tooltip>
              </template>
            </el-input>
          </div>
        </section>
      </div>

      <template #footer>
        <el-button @click="connectDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="copyAllConnect">复制全部</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import DataTableCard from '@/components/DataTableCard.vue'
import FormDialog from '@/components/FormDialog.vue'
import StatusSwitch from '@/components/StatusSwitch.vue'
import * as api from '@/api/client'

interface InstanceForm {
  name: string
  protocol: string
  address: string
  port: number
  group: string
  remark: string
}

interface AccountFormState {
  username: string
  password: string
  group: string
  remark: string
  expiresAt: Date | null
}

// ── Instance state ──
const instances = ref<api.DatabaseInstanceView[]>([])
const instancesLoading = ref(false)
const instancePage = ref(1)
const instancePageSize = ref(20)
const instanceTotal = ref(0)
const instanceSearchKeyword = ref('')
const showInstanceDialog = ref(false)
const submitting = ref(false)
const editingInstance = ref<api.DatabaseInstanceView | null>(null)
const instanceForm = reactive<InstanceForm>({
  name: '',
  protocol: 'mysql',
  address: '',
  port: 3306,
  group: '',
  remark: ''
})

// ── Account dialog state ──
const accountsDialogVisible = ref(false)
const selectedInstance = ref<api.DatabaseInstanceView | null>(null)
const accountsDialogTitle = computed(() => selectedInstance.value ? `${selectedInstance.value.name} - 账号` : '数据库账号')

const accounts = ref<api.DBAccountRecord[]>([])
const accountsLoading = ref(false)
const accountTotal = ref(0)
const accountPage = ref(1)
const accountPageSize = ref(20)

const accountDialogVisible = ref(false)
const editingAccount = ref<api.DBAccountRecord | null>(null)
const accountSubmitting = ref(false)
const statusUpdatingId = ref('')

const expiryPreset = ref('')
const expiryOptions = [
  { label: '8小时', hours: 8 },
  { label: '7天', hours: 7 * 24 },
  { label: '1年', hours: 365 * 24 },
  { label: '永久', hours: -1 },
]

const accountForm = reactive<AccountFormState>({
  username: '',
  password: '',
  group: '',
  remark: '',
  expiresAt: null,
})

// ── Connect dialog state ──
const connectDialogVisible = ref(false)
const connectTarget = ref<api.DBAccountRecord | null>(null)
const userSessionId = ref('')
const connectTesting = ref(false)
const connectTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null)

// ── Gateway config ──
const gatewayConfig = ref<{ host: string; port: number; enabled: boolean }>({
  host: '127.0.0.1',
  port: 33060,
  enabled: false,
})

async function loadGatewayConfig() {
  try {
    const cfg = await api.apiClient.getDBGateway()
    if (cfg && typeof cfg === 'object') {
      gatewayConfig.value = {
        host: String(cfg.host || '127.0.0.1'),
        port: Number(cfg.port) || 33060,
        enabled: Boolean(cfg.enabled),
      }
    }
  } catch {
    // 使用默认值
  }
}

const connectParams = computed(() => {
  if (!connectTarget.value || !selectedInstance.value) return []
  const host = gatewayConfig.value.host
  const proxyPort = gatewayConfig.value.port
  const resourceId = connectTarget.value.resource_id || '0000'
  const sessionId = userSessionId.value
  const compactUser = sessionId ? 'D' + resourceId + sessionId : ''
  return [
    { label: '主机', value: host },
    { label: '端口', value: String(proxyPort) },
    { label: '用户名', value: compactUser },
  ]
})

const connectCommand = computed(() => {
  if (!connectTarget.value || !selectedInstance.value) return ''
  const inst = selectedInstance.value
  const protocol = inst.protocol || 'mysql'
  const resourceId = connectTarget.value.resource_id || '0000'
  const sessionId = userSessionId.value || '00001'
  const compactUser = `D${resourceId}${sessionId}`
  const host = gatewayConfig.value.host
  const proxyPort = gatewayConfig.value.port
  if (protocol === 'mysql') {
    return `mysql --protocol=tcp -h ${host} -P ${proxyPort} -u ${compactUser} -p`
  }
  return `psql -h ${host} -p ${proxyPort} -U ${compactUser}`
})

// ── Helpers ──
function formatTime(value: unknown): string {
  if (typeof value === 'string' && value.trim()) {
    const d = new Date(value)
    return Number.isNaN(d.getTime()) ? value : d.toLocaleString()
  }
  return ''
}

function isExpired(expiresAt: unknown): boolean {
  if (typeof expiresAt !== 'string' || !expiresAt.trim()) return false
  const d = new Date(expiresAt)
  if (Number.isNaN(d.getTime())) return false
  return d.getTime() < Date.now()
}

async function writeClipboard(value: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value)
    return
  }
  const textarea = document.createElement('textarea')
  textarea.value = value
  textarea.setAttribute('readonly', 'true')
  textarea.style.position = 'fixed'
  textarea.style.top = '-9999px'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    if (!document.execCommand('copy')) throw new Error('copy command failed')
  } finally {
    document.body.removeChild(textarea)
  }
}

async function copyText(value: string) {
  if (!value.trim()) {
    ElMessage.warning('没有可复制的内容')
    return
  }
  try {
    await writeClipboard(value)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败')
  }
}

function copyAllConnect() {
  if (!connectTarget.value) return
  const lines = connectParams.value.map(p => `${p.label}: ${p.value}`)
  lines.push(`Shell: ${connectCommand.value}`)
  copyText(lines.join('\n'))
}

// ── Instance methods ──
async function loadInstances() {
  instancesLoading.value = true
  try {
    const res = await api.apiClient.getDBInstances({
      page: instancePage.value,
      page_size: instancePageSize.value,
      q: instanceSearchKeyword.value || undefined
    })
    instances.value = res.items
    instanceTotal.value = res.total
  } catch (err) {
    instances.value = []
    ElMessage.error(err instanceof Error ? err.message : '加载实例失败')
  } finally {
    instancesLoading.value = false
  }
}

function onInstanceSearch(keyword: string) {
  instanceSearchKeyword.value = keyword
  instancePage.value = 1
  loadInstances()
}

function openCreateInstance() {
  editingInstance.value = null
  Object.assign(instanceForm, {
    name: '',
    protocol: 'mysql',
    address: '',
    port: 3306,
    group: '',
    remark: ''
  })
  showInstanceDialog.value = true
}

function editInstance(inst: api.DatabaseInstanceView) {
  editingInstance.value = inst
  Object.assign(instanceForm, {
    name: inst.name || '',
    protocol: inst.protocol || 'mysql',
    address: inst.address || '',
    port: inst.port || 3306,
    group: inst.group || '',
    remark: inst.remark || ''
  })
  showInstanceDialog.value = true
}

async function submitInstance() {
  if (!instanceForm.name.trim() || !instanceForm.address.trim()) {
    ElMessage.warning('请填写必填字段')
    return
  }
  submitting.value = true
  try {
    const payload: api.DBInstancePayload = {
      name: instanceForm.name.trim(),
      protocol: instanceForm.protocol,
      address: instanceForm.address.trim(),
      port: instanceForm.port,
      group: instanceForm.group.trim() || undefined,
      remark: instanceForm.remark.trim() || undefined
    }
    if (editingInstance.value?.id) {
      await api.apiClient.updateDBInstance(editingInstance.value.id, payload)
      ElMessage.success('数据库实例已更新')
    } else {
      await api.apiClient.createDBInstance(payload)
      ElMessage.success('数据库实例已创建')
    }
    showInstanceDialog.value = false
    await loadInstances()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '保存失败')
  } finally {
    submitting.value = false
  }
}

async function toggleInstance(inst: api.DatabaseInstanceView) {
  const id = inst.id
  if (!id) return
  const newStatus = inst.status === 'active' ? 'disabled' : 'active'
  try {
    await api.apiClient.updateDBInstance(id, {
      name: inst.name || '',
      protocol: inst.protocol || 'mysql',
      address: inst.address || '',
      port: inst.port,
      group: inst.group || undefined,
      remark: inst.remark || undefined,
      status: newStatus
    })
    ElMessage.success(newStatus === 'active' ? '数据库实例已启用' : '数据库实例已禁用')
    await loadInstances()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '状态切换失败')
  }
}

async function deleteInstance(inst: api.DatabaseInstanceView) {
  const id = inst.id
  if (!id) return
  try {
    await ElMessageBox.confirm(
      `确定要删除数据库实例「${inst.name || id}」吗？`,
      '删除实例',
      { cancelButtonText: '取消', confirmButtonText: '删除', type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await api.apiClient.deleteDBInstance(id)
    ElMessage.success('数据库实例已删除')
    await loadInstances()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '删除失败')
  }
}

// ── Account methods ──
function showAccounts(inst: api.DatabaseInstanceView) {
  selectedInstance.value = inst
  accountPage.value = 1
  accountsDialogVisible.value = true
  loadSelectedInstanceAccounts()
}

async function loadSelectedInstanceAccounts() {
  if (!selectedInstance.value) return
  accountsLoading.value = true
  try {
    const res = await api.apiClient.getDBAccounts(selectedInstance.value.id!, {
      page: accountPage.value,
      page_size: accountPageSize.value,
    })
    accounts.value = res.items
    accountTotal.value = res.total
  } finally {
    accountsLoading.value = false
  }
}

function openCreateAccount() {
  editingAccount.value = null
  accountForm.username = ''
  accountForm.password = ''
  accountForm.group = ''
  accountForm.remark = ''
  accountForm.expiresAt = null
  expiryPreset.value = ''
  accountDialogVisible.value = true
}

function editAccount(row: api.DBAccountRecord) {
  editingAccount.value = row
  accountForm.username = row.username || ''
  accountForm.password = ''
  accountForm.group = row.group || ''
  accountForm.remark = row.remark || ''
  accountForm.expiresAt = row.expires_at ? new Date(row.expires_at) : null
  expiryPreset.value = ''
  accountDialogVisible.value = true
}

function setExpiry(opt: { label: string; hours: number }) {
  expiryPreset.value = opt.label
  if (opt.hours === -1) {
    accountForm.expiresAt = null
  } else {
    accountForm.expiresAt = new Date(Date.now() + opt.hours * 3600 * 1000)
  }
}

async function submitAccount() {
  if (!accountForm.username.trim()) { ElMessage.warning('请输入目标用户名'); return }
  accountSubmitting.value = true
  try {
    if (editingAccount.value) {
      await api.apiClient.updateDBAccount(editingAccount.value.id!, {
        username: accountForm.username,
        password: accountForm.password || undefined,
        group: accountForm.group,
        remark: accountForm.remark,
        status: editingAccount.value.status,
        expires_at: accountForm.expiresAt?.toISOString(),
      })
      ElMessage.success('账号已更新')
    } else {
      await api.apiClient.createDBAccount(selectedInstance.value!.id!, {
        username: accountForm.username,
        password: accountForm.password,
        group: accountForm.group,
        remark: accountForm.remark,
        expires_at: accountForm.expiresAt?.toISOString(),
      })
      ElMessage.success('账号已创建')
    }
    accountDialogVisible.value = false
    loadSelectedInstanceAccounts()
    loadInstances()
  } finally {
    accountSubmitting.value = false
  }
}

async function toggleAccountStatus(account: api.DBAccountRecord, active: boolean) {
  statusUpdatingId.value = account.id!
  try {
    await api.apiClient.updateDBAccount(account.id!, {
      username: account.username || '',
      group: account.group || '',
      remark: account.remark || '',
      status: active ? 'active' : 'disabled',
    })
    ElMessage.success(active ? '账号已启用' : '账号已禁用')
    loadSelectedInstanceAccounts()
  } finally {
    statusUpdatingId.value = ''
  }
}

async function deleteAccount(account: api.DBAccountRecord) {
  await ElMessageBox.confirm(`确认删除账号"${account.username}"？`, '删除账号', { type: 'warning' })
  await api.apiClient.deleteDBAccount(account.id!)
  ElMessage.success('账号已删除')
  loadSelectedInstanceAccounts()
  loadInstances()
}

// ── Connect dialog ──
async function openConnectDialog(acc: api.DBAccountRecord) {
  connectTestResult.value = null
  connectTarget.value = acc
  userSessionId.value = ''
  connectDialogVisible.value = true

  try {
    const targetId = acc.id || acc.resource_id || ''
    if (!targetId) return
    const session = await api.apiClient.createUserSession(String(targetId))
    userSessionId.value = session?.session_id || ''
  } catch {
    // 静默失败
  }
}

async function onConnectDialogOpened() {
  if (!connectTarget.value?.id) return
  connectTesting.value = true
  connectTestResult.value = null
  try {
    const result = await api.apiClient.testDBConnection(connectTarget.value.id)
    const data = ('data' in result ? (result as api.ApiEnvelope<{ ok: boolean; error?: string; latency_ms?: number }>).data : result) as { ok: boolean; error?: string; latency_ms?: number }
    connectTestResult.value = data ?? null
  } catch (err) {
    connectTestResult.value = { ok: false, error: err instanceof Error ? err.message : 'test failed' }
  } finally {
    connectTesting.value = false
  }
}

// ── Watchers ──
watch([accountPage, accountPageSize], () => {
  if (accountsDialogVisible.value) loadSelectedInstanceAccounts()
})

onMounted(() => {
  loadGatewayConfig()
  loadInstances()
})
</script>

<style scoped>
.dialog-stack {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.connect-section + .connect-section {
  margin-top: 20px;
}

.connect-section-title {
  color: #374151;
  font-size: 13px;
  font-weight: 700;
  margin-bottom: 10px;
}

.connect-status-card {
  min-height: 20px;
}

.config-row {
  display: grid;
  grid-template-columns: minmax(80px, 120px) minmax(0, 1fr);
  gap: 12px;
  align-items: center;
}

.config-label {
  color: #344054;
  font-size: 13px;
  font-weight: 650;
}

@media (max-width: 720px) {
  .config-row {
    grid-template-columns: 1fr;
  }
}
</style>
