<template>
  <div class="view-stack">
    <DataTableCard
      :data="displayedInstances"
      :loading="instancesLoading"
      :total="instanceTotal"
      v-model:page="instancePage"
      v-model:page-size="instancePageSize"
      search-placeholder="搜索实例名称、地址、协议…"
      @search="onInstanceSearch"
    >
      <template #toolbar-filter>
        <ResourceFilterBar
          :model-value="instanceFilter"
          :options="instanceQuickGroupOptions"
          :preview-limit="filterPreviewLimit"
          @update:model-value="setInstanceFilter"
        />
      </template>
      <template #toolbar-extra>
        <el-button :loading="instancesLoading" :icon="Refresh" @click="loadInstances">
          刷新
        </el-button>
        <el-button v-if="permission.canDo('dbproxy:create')" type="primary" @click="openCreateInstance">新增实例</el-button>
      </template>
      <el-table-column prop="name" label="名称" min-width="130" show-overflow-tooltip />
      <el-table-column label="地址" v-bind="TABLE_COLUMNS.address">
        <template #default="{ row }">{{ instanceEndpoint(row) }}</template>
      </el-table-column>
      <el-table-column label="协议" width="100" align="center">
        <template #default="{ row }">
          <el-tag class="protocol-tag" size="small" :type="row.protocol === 'mysql' ? 'success' : row.protocol === 'redis' ? 'danger' : 'primary'" effect="light">{{ row.protocol === 'mysql' ? 'MySQL' : row.protocol === 'redis' ? 'Redis' : 'PostgreSQL' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="账号管理" min-width="110" align="center">
        <template #default="{ row }">
          <el-button link type="primary" size="small" class="account-mgmt-btn" @click="showAccounts(row)">账号管理({{ row.account_count ?? 0 }})</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="group" label="分组" v-bind="TABLE_COLUMNS.group" />
      <el-table-column label="状态" v-bind="TABLE_COLUMNS.status">
        <template #default="{ row }">
          <StatusSwitch v-if="row.can_manage && permission.canDo('dbproxy:update')" :model-value="row.status === 'active'" @update:model-value="toggleInstance(row)" />
        </template>
      </el-table-column>
      <el-table-column prop="remark" label="备注" v-bind="TABLE_COLUMNS.note" />
      <el-table-column label="操作" v-bind="TABLE_COLUMNS.actionsWide">
        <template #default="{ row }">
          <div class="table-actions">
            <el-button v-if="permission.canDo('db:connect')" link type="success" size="small" @click="handleDBConnect(row)">连接</el-button>
            <el-button v-if="row.can_manage && permission.canDo('dbproxy:update')" link type="primary" size="small" @click="editInstance(row)">编辑</el-button>
            <el-dropdown v-if="permission.canDo('db:audit:view') || permission.canDo('session:view') || (row.can_manage && permission.canDo('dbproxy:delete'))" trigger="click" teleported>
              <el-button link type="primary" size="small"
                >更多<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button
              >
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-if="permission.canDo('db:audit:view')" @click="handleDBAuditLog(row)">审计日志</el-dropdown-item>
                  <el-dropdown-item v-if="permission.canDo('session:view')" @click="handleDBSessions(row)">在线会话</el-dropdown-item>
                  <el-dropdown-item v-if="row.can_manage && permission.canDo('dbproxy:delete')" class="danger-dropdown-item" @click="deleteInstance(row)">删除</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </template>
      </el-table-column>
    </DataTableCard>

    <!-- 创建/编辑实例弹窗 -->
    <FormDialog v-model:visible="showInstanceDialog" :title="editingInstance ? '编辑实例' : '新增实例'" :loading="submitting || tlsPreflightChecking" @submit="submitInstance">
      <el-form :model="instanceForm" class="database-resource-form" label-position="top">
        <el-form-item label="协议" required>
          <el-select v-model="instanceForm.protocol" @change="onProtocolChange">
            <el-option label="MySQL" value="mysql" />
            <el-option label="PostgreSQL" value="postgres" />
            <el-option label="Redis" value="redis" />
          </el-select>
        </el-form-item>
        <el-form-item label="上游地址" required>
          <el-input
            v-model="instanceForm.address"
            placeholder="host:port 或 IP"
            @input="onInstanceAddressInput"
          />
        </el-form-item>
        <el-form-item label="端口">
          <el-input-number v-model="instanceForm.port" :min="1" :max="65535" @change="invalidateTLSPreflightConfiguration" />
        </el-form-item>
        <el-collapse v-model="instanceMorePanels">
          <el-collapse-item name="more" title="更多设置">
            <el-form-item label="名称">
              <el-input
                v-model="instanceForm.name"
                placeholder="默认 = 上游地址"
                @input="instanceNameTouched = true"
              />
            </el-form-item>
            <el-form-item label="分组">
              <el-select
                v-model="instanceForm.group"
                allow-create
                clearable
                default-first-option
                filterable
                placeholder="选择或输入分组"
              >
                <el-option
                  v-for="g in instanceGroupOptions"
                  :key="g"
                  :label="g"
                  :value="g"
                />
              </el-select>
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="instanceForm.remark" type="textarea" />
            </el-form-item>
            <el-form-item label="启用 TLS">
              <el-select :model-value="displayedTLSMode" :disabled="tlsPreflightChecking" @change="onTLSModeChange">
                <el-option label="不启用（默认）" value="disable" />
                <el-option label="验证证书和主机名（远程需启用 TLS，推荐）" value="verify-full" />
                <el-option label="仅验证证书" value="verify-ca" />
              </el-select>
              <div class="tls-mode-help">{{ tlsModeDescription(displayedTLSMode) }}</div>
            </el-form-item>
            <template v-if="displayedTLSMode !== 'disable'">
              <el-form-item label="主机名">
                <el-input
                  v-model="instanceForm.tlsServerName"
                  :disabled="displayedTLSMode !== 'verify-full' || tlsPreflightChecking"
                  placeholder="根据上游地址自动推导，也可手动修改"
                  @input="invalidateTLSPreflightConfiguration"
                />
              </el-form-item>
              <el-form-item label="自定义 CA">
                <div class="tls-ca-editor">
                  <div class="tls-ca-help">
                    只有使用企业私有 CA、自签名证书时，才需要提供自定义 CA。
                  </div>
                  <div class="tls-ca-actions">
                    <el-button size="small" :disabled="tlsPreflightChecking" @click="chooseTLSCAFile">选择 PEM 文件</el-button>
                    <input
                      ref="tlsCAFileInput"
                      class="tls-ca-file-input"
                      type="file"
                      accept=".pem,.crt,.cer,text/plain"
                      @change="handleTLSCAFileChange"
                    />
                    <span v-if="instanceForm.tlsCaPem" class="tls-ca-status">已填写新的 CA</span>
                    <span v-else-if="instanceForm.hasTlsCa" class="tls-ca-status">已配置（内容不回显）</span>
                    <el-button
                      v-if="instanceForm.hasTlsCa || instanceForm.tlsCaPem"
                      link
                      type="danger"
                      size="small"
                      :disabled="tlsPreflightChecking"
                      @click="clearTLSCA"
                    >
                      清除 CA
                    </el-button>
                  </div>
                  <el-input
                    v-model="instanceForm.tlsCaPem"
                    type="textarea"
                    :rows="4"
                    :disabled="tlsPreflightChecking"
                    placeholder="也可以手动粘贴 PEM 内容；编辑时留空会保留已有 CA"
                    @input="onTLSCAPEMInput"
                  />
                </div>
              </el-form-item>
              <div class="tls-preflight-status">
                <el-alert
                  v-if="tlsPreflightChecking"
                  title="正在检测远程数据库 TLS，请稍候…"
                  type="info"
                  :closable="false"
                  show-icon
                />
                <el-alert
                  v-else-if="tlsPreflightResult?.ok"
                  title="TLS 检测通过，可以保存"
                  type="success"
                  :closable="false"
                  show-icon
                />
                <el-alert
                  v-else-if="tlsPreflightResult && tlsPreflightResult.code !== 'cancelled'"
                  :title="tlsPreflightResult.error || tlsPreflightResult.message || 'TLS 检测失败，TLS 尚未开启'"
                  type="error"
                  :closable="false"
                  show-icon
                />
                <el-alert
                  v-else-if="tlsSetupMode"
                  title="TLS 尚未开启；配置调整后请重新检测"
                  type="warning"
                  :closable="false"
                  show-icon
                />
                <div v-if="tlsSetupMode && !tlsPreflightChecking" class="tls-preflight-actions">
                  <el-button size="small" type="primary" plain @click="retryTLSPreflight">
                    重新检测并开启
                  </el-button>
                  <el-button size="small" @click="cancelTLSSetup">
                    {{ instanceForm.tlsMode === 'disable' ? '取消开启' : '保留当前模式' }}
                  </el-button>
                </div>
              </div>
            </template>
          </el-collapse-item>
        </el-collapse>
      </el-form>
    </FormDialog>

    <!-- 账号列表弹窗 -->
    <el-dialog
      v-model="accountsDialogVisible"
      :title="accountsDialogTitle"
      class="accounts-dialog"
      destroy-on-close
      width="min(960px, calc(100vw - 32px))"
    >
      <DataTableCard
        class="accounts-table"
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
          <el-button v-if="selectedInstance?.can_manage && permission.canDo('dbproxy:create')" type="primary" :disabled="!selectedInstance" @click="openCreateAccount">
            新增账号
          </el-button>
          <el-button v-if="selectedInstance?.can_manage && permission.canDo('dbproxy:create')" type="success" :disabled="!selectedInstance || selectedInstance.protocol !== 'mysql'" @click="openAutoProvision">
            自动创建
          </el-button>
        </template>
        <el-table-column label="登录账号" min-width="130">
          <template #default="{ row }">{{ row.username || '-' }}</template>
        </el-table-column>
        <el-table-column label="分组" v-bind="TABLE_COLUMNS.group">
          <template #default="{ row }">{{ row.group || '-' }}</template>
        </el-table-column>
        <el-table-column label="状态" v-bind="TABLE_COLUMNS.status">
          <template #default="{ row }">
            <StatusSwitch
              v-if="row.can_manage && permission.canDo('dbproxy:update')"
              :model-value="row.status === 'active'"
              :loading="statusUpdatingId === row.id"
              :aria-label="`${row.username || '未命名账号'}账号状态：${row.status === 'active' ? '启用' : '停用'}`"
              @update:model-value="(val: boolean) => toggleAccountStatus(row, val)"
            />
            <el-tag v-else size="small" :type="row.status === 'active' ? 'success' : 'info'">
              {{ row.status === 'active' ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="过期时间" v-bind="TABLE_COLUMNS.time">
          <template #default="{ row }">{{ formatTime(row.expires_at) || '永久' }}</template>
        </el-table-column>
        <el-table-column label="备注" v-bind="TABLE_COLUMNS.note">
          <template #default="{ row }">{{ row.remark || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" v-bind="TABLE_COLUMNS.actionsWide">
          <template #default="{ row }">
            <div class="table-actions">
              <el-button v-if="permission.canDo('db:connect')" link type="success" size="small" @click="openConnectDialog(row)">连接</el-button>
              <el-button v-if="row.can_manage && permission.canDo('dbproxy:update')" link type="primary" size="small" @click="editAccount(row)">编辑</el-button>
              <el-button v-if="row.can_manage && permission.canDo('dbproxy:delete')" link type="danger" size="small" @click="deleteAccount(row)">删除</el-button>
            </div>
          </template>
        </el-table-column>
      </DataTableCard>
    </el-dialog>

    <DatabaseAccountFormDialog
      v-model:visible="accountDialogVisible"
      v-model:username="accountForm.username"
      v-model:password="accountForm.password"
      v-model:group="accountForm.group"
      v-model:remark="accountForm.remark"
      v-model:expires-at="accountForm.expiresAt"
      v-model:more-panels="accountMorePanels"
      :editing="Boolean(editingAccount)"
      :protocol="selectedInstance?.protocol"
      :loading="accountSubmitting"
      :testing="accountFormTesting"
      :test-result="accountFormTestResult"
      :group-options="accountGroupOptions"
      @test="testAccountFormConnection"
      @submit="submitAccount"
    />

    <DatabaseAutoProvisionDialog
      v-model="autoProvisionVisible"
      :instance="selectedInstance"
      @created="handleProvisionedAccountCreated"
    />

    <ConnectionConfigDialog
      v-model="connectDialogVisible"
      resource-type="database"
      :target="connectTarget"
      :resource-name="String(selectedInstance?.name || '')"
      :source-address="selectedInstance ? instanceEndpoint(selectedInstance) : ''"
      :source-account="String(connectTarget?.username || '')"
      :protocol="String(selectedInstance?.protocol || 'mysql')"
      :allow-ssh="false"
      :allow-web-sql="permission.canDo('db:query') || permission.canDo('db:execute')"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, shallowRef, reactive, watch, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, Refresh } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import DataTableCard from '@/components/DataTableCard.vue'
import FormDialog from '@/components/FormDialog.vue'
import ConnectionConfigDialog from '@/components/ConnectionConfigDialog.vue'
import DatabaseAccountFormDialog from '@/components/database/DatabaseAccountFormDialog.vue'
import DatabaseAutoProvisionDialog from '@/components/database/DatabaseAutoProvisionDialog.vue'
import ResourceFilterBar from '@/components/ResourceFilterBar.vue'
import StatusSwitch from '@/components/StatusSwitch.vue'
import { TABLE_COLUMNS } from '@/config/tableColumns'
import * as api from '@/api/client'
import { usePermissionStore } from '@/stores/permission'
import { createLatestKeyedRequest } from '@/utils/connectionRequestState'
import {
  databaseTLSPreflightFingerprint,
  useDatabaseTLSPreflight,
} from '@/composables/useDatabaseTLSPreflight'
import {
  buildDatabaseUpstreamTLSPayload,
  DEFAULT_DATABASE_UPSTREAM_TLS_MODE,
  normalizeDatabaseUpstreamTLSMode,
} from '@/utils/databaseUpstreamTLS'

interface InstanceForm {
  name: string
  protocol: string
  address: string
  port: number
  group: string
  remark: string
  tlsMode: api.DatabaseTLSMode
  tlsServerName: string
  tlsCaPem: string
  hasTlsCa: boolean
  clearTlsCa: boolean
}

interface AccountFormState {
  username: string
  password: string
  group: string
  remark: string
  expiresAt: Date | null
}

// ── Instance state ──
const permission = usePermissionStore()
const router = useRouter()

const instances = ref<api.DatabaseInstanceView[]>([])
const instancesLoading = ref(false)
const instanceRequests = createLatestKeyedRequest<api.DatabaseInstanceView[]>()
const instancePage = ref(1)
const instancePageSize = ref(50)
const instanceSearchKeyword = ref('')
const instanceFilter = ref('all')
const instanceUsageCounts = ref<Record<string, number>>({})
const filterPreviewLimit = 6
const showInstanceDialog = ref(false)
const submitting = ref(false)
const editingInstance = ref<api.DatabaseInstanceView | null>(null)
const instanceMorePanels = ref<string[]>([])
const instanceGroupOptions = ref<string[]>([])
const accountGroupOptions = ref<string[]>([])
const tlsCAFileInput = ref<HTMLInputElement | null>(null)
const tlsSetupMode = shallowRef<api.EnabledDatabaseTLSMode | null>(null)
const tlsBaselineFingerprint = shallowRef('')
const originalHasTLSCA = ref(false)
const lastAutoTLSHostName = ref('')
const instanceNameTouched = ref(false)
const instanceForm = reactive<InstanceForm>({
  name: '',
  protocol: 'mysql',
  address: '',
  port: 3306,
  group: '',
  remark: '',
  tlsMode: DEFAULT_DATABASE_UPSTREAM_TLS_MODE,
  tlsServerName: '',
  tlsCaPem: '',
  hasTlsCa: false,
  clearTlsCa: false
})
const {
  checking: tlsPreflightChecking,
  result: tlsPreflightResult,
  accept: acceptTLSPreflight,
  invalidate: invalidateTLSPreflight,
  isVerified: isTLSPreflightVerified,
  reset: resetTLSPreflight,
  verify: verifyTLSPreflight,
} = useDatabaseTLSPreflight()
const displayedTLSMode = computed<api.DatabaseTLSMode>(() => tlsSetupMode.value ?? instanceForm.tlsMode)

// ── Account dialog state ──
const accountsDialogVisible = ref(false)
const selectedInstance = ref<api.DatabaseInstanceView | null>(null)
const accountsDialogTitle = computed(() => selectedInstance.value ? `${selectedInstance.value.name} - 账号` : '数据库账号')

const accounts = ref<api.DBAccountRecord[]>([])
const accountsLoading = ref(false)
const accountRequests = createLatestKeyedRequest<{ items: api.DBAccountRecord[]; total: number }>()
const accountTotal = ref(0)
const accountPage = ref(1)
const accountPageSize = ref(50)

const accountDialogVisible = ref(false)
const editingAccount = ref<api.DBAccountRecord | null>(null)
const accountMorePanels = ref<string[]>([])
const accountSubmitting = ref(false)
const statusUpdatingId = ref('')

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

// ── Gateway config ──
const accountFormTesting = ref(false)
const accountFormTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null)

function instanceUsageCount(instance: api.DatabaseInstanceView): number {
  return instanceUsageCounts.value[String(instance.name || '').trim().toLowerCase()] || 0
}

const instanceQuickGroupOptions = computed(() => {
  const groups = new Map<string, number>()
  instances.value.forEach(instance => {
    const group = String(instance.group || '未分组')
    groups.set(group, (groups.get(group) || 0) + instanceUsageCount(instance))
  })
  return Array.from(groups, ([label, count]) => ({ label, value: label, count }))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label, 'zh-CN'))
})

const filteredInstances = computed(() => {
  let items = instances.value
  if (instanceFilter.value !== 'all' && instanceFilter.value !== 'popular') {
    items = items.filter(instance => String(instance.group || '未分组') === instanceFilter.value)
  }
  if (instanceFilter.value === 'popular') {
    items = [...items].sort((a, b) => instanceUsageCount(b) - instanceUsageCount(a) || String(a.name || '').localeCompare(String(b.name || ''), 'zh-CN'))
  }
  return items
})

const instanceTotal = computed(() => filteredInstances.value.length)
const displayedInstances = computed(() => {
  const start = (instancePage.value - 1) * instancePageSize.value
  return filteredInstances.value.slice(start, start + instancePageSize.value)
})

async function fetchAllPages<T>(fetchPage: (page: number, pageSize: number) => Promise<api.PageResponse<T>>): Promise<T[]> {
  const pageSize = 200
  const items: T[] = []
  let page = 1
  let total = 0
  do {
    const response = await fetchPage(page, pageSize)
    items.push(...(response.items ?? []))
    total = response.total ?? items.length
    page += 1
    if (!response.items?.length) break
  } while (items.length < total)
  return items
}

async function loadInstanceUsage() {
  instanceUsageCounts.value = {}
  if (!permission.canDo('db:audit:view')) return
  try {
    const connections = await fetchAllPages(page => api.apiClient.getDBConnections({ page, page_size: 200 }))
    const counts: Record<string, number> = {}
    connections.forEach(connection => {
      const name = String(connection.target_name || connection.target_address || '').trim().toLowerCase()
      if (name) counts[name] = (counts[name] || 0) + 1
    })
    instanceUsageCounts.value = counts
  } catch {
    // Audit permission or storage may be unavailable; group filters still work.
  }
}

function setInstanceFilter(value: string) {
  instanceFilter.value = value
  instancePage.value = 1
}

function instanceEndpoint(inst: api.DatabaseInstanceView): string {
  const address = (inst.address || '').trim()
  const port = inst.port
  if (!address) return '-'
  return port ? `${address}:${port}` : address
}

function normalizeTLSMode(value: unknown): api.DatabaseTLSMode {
  return normalizeDatabaseUpstreamTLSMode(value)
}

function tlsModeDescription(value: api.DatabaseTLSMode): string {
  switch (value) {
    case 'disable': return 'Jianmen 到上游数据库不加密；PostgreSQL 上游要求明文密码认证时，密码也会以明文传输。仅用于可信内网。'
    case 'verify-ca': return '要求远程数据库已启用 SSL/TLS。Jianmen 将加密连接并验证 CA，但不校验主机名；安全性低于验证证书和主机名。'
    default: return '要求远程数据库已启用 SSL/TLS。Jianmen 将加密连接并验证 CA 与主机名，可防止中间人攻击；远程未启用 TLS 时连接将失败。'
  }
}

function formatTime(value: unknown): string {
  let d: Date | null = null
  if (typeof value === 'number' && Number.isFinite(value)) {
    d = new Date(value)
  } else if (typeof value === 'string' && value.trim()) {
    const parsed = Date.parse(value)
    if (!Number.isNaN(parsed)) d = new Date(parsed)
  }
  if (!d || Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// ── Instance methods ──
async function loadInstances() {
  const key = instanceSearchKeyword.value.trim()
  const request = instanceRequests.begin(key, async () => {
    const loaded = await fetchAllPages(page => api.apiClient.getDBInstances({
      page,
      page_size: 200,
      q: key || undefined
    }))
    await loadInstanceUsage()
    return loaded
  })
  instancesLoading.value = instanceRequests.isLoading()
  try {
    const loaded = await request.promise
    if (!instanceRequests.isCurrent(request.token, key)) return
    instances.value = loaded
  } catch (err) {
    if (!instanceRequests.isCurrent(request.token, key)) return
    instances.value = []
    ElMessage.error(err instanceof Error ? err.message : '数据库实例加载失败')
  } finally {
    instancesLoading.value = instanceRequests.isLoading()
  }
}

function onInstanceSearch(keyword: string) {
  instanceSearchKeyword.value = keyword
  instanceFilter.value = 'all'
  instancePage.value = 1
  loadInstances()
}


function openCreateInstance() {
  editingInstance.value = null
  resetTLSPreflight()
  tlsSetupMode.value = null
  tlsBaselineFingerprint.value = ''
  instanceNameTouched.value = false
  instanceMorePanels.value = ['more']
  originalHasTLSCA.value = false
  lastAutoTLSHostName.value = ''
  Object.assign(instanceForm, {
    name: '',
    protocol: 'mysql',
    address: '',
    port: 3306,
    group: '',
    remark: '',
    tlsMode: DEFAULT_DATABASE_UPSTREAM_TLS_MODE,
    tlsServerName: '',
    tlsCaPem: '',
    hasTlsCa: false,
    clearTlsCa: false
  })
  showInstanceDialog.value = true
}

function inferTLSHostName(address: string): string {
  const value = address.trim()
  if (!value) return ''
  if (value.startsWith('[')) {
    const bracketEnd = value.indexOf(']')
    return bracketEnd > 1 ? value.slice(1, bracketEnd) : ''
  }
  if ((value.match(/:/g) || []).length > 1) return value
  const separator = value.lastIndexOf(':')
  if (separator > 0 && /^\d+$/.test(value.slice(separator + 1))) {
    return value.slice(0, separator)
  }
  return value
}

function onInstanceAddressInput(address: string) {
  invalidateTLSPreflightConfiguration()
  const inferred = inferTLSHostName(address)
  if (
    !instanceForm.tlsServerName.trim()
    || instanceForm.tlsServerName === lastAutoTLSHostName.value
  ) {
    instanceForm.tlsServerName = inferred
  }
  lastAutoTLSHostName.value = inferred
  syncDefaultInstanceName()
}

function editInstance(inst: api.DatabaseInstanceView) {
  editingInstance.value = inst
  resetTLSPreflight()
  tlsSetupMode.value = null
  tlsBaselineFingerprint.value = ''
  instanceNameTouched.value = true
  instanceMorePanels.value = []
  const tlsMode = normalizeTLSMode(inst.tls_mode)
  originalHasTLSCA.value = Boolean(inst.has_tls_ca)
  lastAutoTLSHostName.value = inferTLSHostName(inst.address || '')
  Object.assign(instanceForm, {
    name: inst.name || '',
    protocol: inst.protocol || 'mysql',
    address: inst.address || '',
    port: inst.port || 3306,
    group: inst.group || '',
    remark: inst.remark || '',
    tlsMode,
    tlsServerName: inst.tls_server_name || '',
    tlsCaPem: '',
    hasTlsCa: originalHasTLSCA.value,
    clearTlsCa: false
  })
  if (tlsMode !== 'disable') {
    const payload = buildTLSPreflightPayload(tlsMode)
    if (payload) {
      tlsBaselineFingerprint.value = databaseTLSPreflightFingerprint(payload)
      acceptTLSPreflight(payload)
    }
  }
  showInstanceDialog.value = true
}

function onProtocolChange(protocol: string) {
  if (!editingInstance.value) {
    switch (protocol) {
      case 'redis': instanceForm.port = 6379; break
      case 'postgres': instanceForm.port = 5432; break
      default: instanceForm.port = 3306; break
    }
  }
  invalidateTLSPreflightConfiguration()
}

function defaultInstanceName(): string {
  return instanceForm.address.trim()
}

function syncDefaultInstanceName() {
  if (!instanceNameTouched.value) {
    instanceForm.name = defaultInstanceName()
  }
}

async function onTLSModeChange(mode: api.DatabaseTLSMode) {
  if (tlsPreflightChecking.value) return
  if (tlsSetupMode.value && mode === instanceForm.tlsMode) {
    cancelTLSSetup()
    return
  }
  if (mode === 'disable') {
    if (instanceForm.tlsMode !== 'disable') {
      try {
        await ElMessageBox.confirm(
          '关闭后，上游数据库链路将不再加密；PostgreSQL 上游要求明文密码认证时，密码也会以明文传输。确定关闭吗？',
          '关闭上游 TLS',
          { type: 'warning', confirmButtonText: '确认关闭', cancelButtonText: '取消' }
        )
      } catch {
        return
      }
    }
    resetTLSPreflight()
    tlsSetupMode.value = null
    instanceForm.tlsMode = 'disable'
    return
  }
  tlsSetupMode.value = mode
  if (mode === 'verify-full' && !instanceForm.tlsServerName.trim()) {
    instanceForm.tlsServerName = inferTLSHostName(instanceForm.address)
    lastAutoTLSHostName.value = instanceForm.tlsServerName
  }
  await runTLSPreflight(mode)
}

function buildTLSPreflightPayload(mode: api.EnabledDatabaseTLSMode): api.DBTLSPreflightPayload | null {
  const address = instanceForm.address.trim()
  if (!address || !Number.isInteger(instanceForm.port) || instanceForm.port < 1 || instanceForm.port > 65535) {
    return null
  }
  const payload: api.DBTLSPreflightPayload = {
    protocol: instanceForm.protocol,
    address,
    port: instanceForm.port,
    tls_mode: mode,
  }
  if (editingInstance.value?.id) payload.instance_id = editingInstance.value.id
  if (mode === 'verify-full' && instanceForm.tlsServerName.trim()) {
    payload.tls_server_name = instanceForm.tlsServerName.trim()
  }
  if (instanceForm.tlsCaPem.trim()) payload.tls_ca_pem = instanceForm.tlsCaPem.trim()
  if (instanceForm.clearTlsCa) payload.clear_tls_ca = true
  return payload
}

async function runTLSPreflight(mode: api.EnabledDatabaseTLSMode, announce = true): Promise<boolean> {
  const payload = buildTLSPreflightPayload(mode)
  if (!payload) {
    if (announce) ElMessage.warning('请先填写有效的上游地址和端口')
    return false
  }
  const response = await verifyTLSPreflight(payload)
  if (response.code === 'cancelled') return false
  if (!response.ok) {
    if (announce) ElMessage.error(response.error || response.message || 'TLS 检测失败，TLS 尚未开启')
    return false
  }
  instanceForm.tlsMode = mode
  tlsSetupMode.value = null
  if (announce) ElMessage.success('TLS 检测通过，已允许开启')
  return true
}

async function retryTLSPreflight() {
  if (tlsSetupMode.value) await runTLSPreflight(tlsSetupMode.value)
}

function cancelTLSSetup() {
  resetTLSPreflight()
  tlsSetupMode.value = null
  if (instanceForm.tlsMode !== 'disable') {
    const payload = buildTLSPreflightPayload(instanceForm.tlsMode)
    if (payload && databaseTLSPreflightFingerprint(payload) === tlsBaselineFingerprint.value) {
      acceptTLSPreflight(payload)
    }
  }
}

function invalidateTLSPreflightConfiguration() {
  const mode = displayedTLSMode.value
  if (mode !== 'disable') {
    tlsSetupMode.value = mode
  }
  invalidateTLSPreflight()
}

function chooseTLSCAFile() {
  tlsCAFileInput.value?.click()
}

async function handleTLSCAFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  try {
    const pem = (await file.text()).trim()
    if (!pem) {
      ElMessage.warning('PEM 文件内容为空')
      return
    }
    instanceForm.tlsCaPem = pem
    instanceForm.hasTlsCa = true
    instanceForm.clearTlsCa = false
    invalidateTLSPreflightConfiguration()
  } catch {
    ElMessage.error('读取 PEM 文件失败')
  }
}

function onTLSCAPEMInput() {
  if (instanceForm.tlsCaPem.trim()) {
    instanceForm.hasTlsCa = true
    instanceForm.clearTlsCa = false
  }
  invalidateTLSPreflightConfiguration()
}

function clearTLSCA() {
  instanceForm.tlsCaPem = ''
  instanceForm.hasTlsCa = false
  instanceForm.clearTlsCa = Boolean(editingInstance.value && originalHasTLSCA.value)
  invalidateTLSPreflightConfiguration()
}

async function submitInstance() {
  if (submitting.value || tlsPreflightChecking.value) return
  if (!instanceForm.address.trim()) {
    ElMessage.warning('请填写上游地址')
    return
  }
  submitting.value = true
  try {
    const desiredTLSMode = displayedTLSMode.value
    if (desiredTLSMode !== 'disable') {
      const preflightPayload = buildTLSPreflightPayload(desiredTLSMode)
      if (!preflightPayload) {
        ElMessage.warning('请先填写有效的上游地址和端口')
        return
      }
      if (!isTLSPreflightVerified(preflightPayload)) {
        const passed = await runTLSPreflight(desiredTLSMode, false)
        if (!passed) {
          const failure = tlsPreflightResult.value
          ElMessage.error(failure?.error || failure?.message || 'TLS 检测失败，无法保存')
          return
        }
      }
    }
    const payload: api.DBInstancePayload = {
      ...buildDatabaseUpstreamTLSPayload(
        instanceForm.tlsMode,
        instanceForm.tlsServerName,
        instanceForm.tlsCaPem,
      ),
      name: instanceForm.name.trim() || defaultInstanceName(),
      protocol: instanceForm.protocol,
      address: instanceForm.address.trim(),
      port: instanceForm.port,
      group: instanceForm.group.trim() || undefined,
      remark: instanceForm.remark.trim() || undefined
    }
    const clearStoredTLSCA = Boolean(
      editingInstance.value
      && (
        instanceForm.clearTlsCa
        || (instanceForm.tlsMode === 'disable' && originalHasTLSCA.value)
      )
    )
    if (clearStoredTLSCA) payload.clear_tls_ca = true
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
  const tlsPayload = buildDatabaseUpstreamTLSPayload(inst.tls_mode, inst.tls_server_name)
  try {
    await api.apiClient.updateDBInstance(id, {
      ...tlsPayload,
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
  const instanceID = selectedInstance.value.id
  if (!instanceID) return
  const page = accountPage.value
  const pageSize = accountPageSize.value
  const key = `${instanceID}:${page}:${pageSize}`
  const request = accountRequests.begin(key, () => api.apiClient.getDBAccounts(instanceID, {
    page,
    page_size: pageSize,
  }))
  accountsLoading.value = accountRequests.isLoading()
  try {
    const res = await request.promise
    if (!accountRequests.isCurrent(request.token, key)) return
    accounts.value = res.items
    accountTotal.value = res.total
  } catch (err) {
    if (!accountRequests.isCurrent(request.token, key)) return
    ElMessage.error(err instanceof Error ? err.message : '加载账号失败')
  } finally {
    accountsLoading.value = accountRequests.isLoading()
  }
}

function openCreateAccount() {
  editingAccount.value = null
  accountMorePanels.value = ['more']
  accountForm.username = ''
  accountForm.password = ''
  accountForm.group = ''
  accountForm.remark = ''
  accountForm.expiresAt = null
  accountFormTestResult.value = null
  accountDialogVisible.value = true
}

function editAccount(row: api.DBAccountRecord) {
  editingAccount.value = row
  accountMorePanels.value = []
  accountForm.username = row.username || ''
  accountForm.password = ''
  accountForm.group = row.group || ''
  accountForm.remark = row.remark || ''
  accountForm.expiresAt = row.expires_at ? new Date(row.expires_at) : null
  accountFormTestResult.value = null
  accountDialogVisible.value = true
}

async function testAccountFormConnection() {
  if (!selectedInstance.value?.id) {
    ElMessage.warning('请先选择数据库实例')
    return
  }
  const isRedis = selectedInstance.value.protocol === 'redis'
  if (!isRedis && !accountForm.username.trim()) {
    ElMessage.warning('请输入登录账号')
    return
  }
  if (!accountForm.password) {
    ElMessage.warning('请先输入数据库密码再测试连接')
    return
  }
  accountFormTesting.value = true
  accountFormTestResult.value = null
  try {
    const result = await api.apiClient.testDBConnectionPayload({
      instance_id: selectedInstance.value.id,
      username: accountForm.username.trim(),
      password: accountForm.password,
    })
    accountFormTestResult.value = { ok: result.ok, latency_ms: result.latency_ms, error: result.ok ? undefined : (result.error || '连接失败') }
  } catch (err) {
    accountFormTestResult.value = { ok: false, error: err instanceof Error ? err.message : '连接失败' }
  } finally {
    accountFormTesting.value = false
  }
}

async function submitAccount() {
  const isRedis = selectedInstance.value?.protocol === 'redis'
  if (!isRedis && !accountForm.username.trim()) { ElMessage.warning('请输入登录账号'); return }
  accountSubmitting.value = true
  try {
    if (editingAccount.value) {
      await api.apiClient.updateDBAccount(editingAccount.value.id!, {
        username: accountForm.username,
        password: accountForm.password || undefined,
        group: accountForm.group,
        remark: accountForm.remark,
        status: editingAccount.value.status,
        expires_at: accountForm.expiresAt?.toISOString() ?? null,
      })
      ElMessage.success('账号已更新')
    } else {
      await api.apiClient.createDBAccount(selectedInstance.value!.id!, {
        username: accountForm.username,
        password: accountForm.password,
        group: accountForm.group,
        remark: accountForm.remark,
        expires_at: accountForm.expiresAt?.toISOString() ?? null,
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
      expires_at: account.expires_at || null,
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

// ── Instance-level connect ──
/** 从实例直接打开连接，单账号时直接弹连接窗，多账号时打开账号管理 */
async function handleDBConnect(inst: api.DatabaseInstanceView) {
  selectedInstance.value = inst;
  accountPage.value = 1;
  await loadSelectedInstanceAccounts();
  const count = accounts.value.length;
  if (count === 0) {
    ElMessage.warning('该实例下无可用账号，请先新增账号');
  } else if (count === 1) {
    openConnectDialog(accounts.value[0]);
  } else {
    accountsDialogVisible.value = true;
    ElMessage.info('请从账号列表中选择要连接的账号');
  }
}

/** More action - open filtered audit logs. */
function handleDBAuditLog(inst: api.DatabaseInstanceView) {
  void router.push({
    name: 'audit',
    query: { scope: 'db', q: String(inst.name || instanceEndpoint(inst)) },
  })
}

/** More action - open filtered online sessions. */
function handleDBSessions(inst: api.DatabaseInstanceView) {
  void router.push({
    name: 'audit',
    query: {
      scope: 'online',
      resource_type: 'database_instance',
      resource_id: String(inst.id ?? ''),
      q: String(inst.name || instanceEndpoint(inst)),
    },
  })
}

// ── Connect dialog ──
function openConnectDialog(acc: api.DBAccountRecord) {
  connectTarget.value = acc
  selectedInstance.value = instances.value.find(i => i.id === acc.instance_id) || selectedInstance.value
  connectDialogVisible.value = true
}

watch([accountPage, accountPageSize], () => {
  if (accountsDialogVisible.value) loadSelectedInstanceAccounts()
})

watch(showInstanceDialog, visible => {
  if (!visible) {
    resetTLSPreflight()
    tlsSetupMode.value = null
    tlsBaselineFingerprint.value = ''
    instanceForm.tlsCaPem = ''
    tlsCAFileInput.value = null
  }
})

onMounted(() => {
  loadInstances()
  loadGroupOptions()
})

async function loadGroupOptions() {
  const [resourceGroups, accountGroups] = await Promise.allSettled([
    api.apiClient.getResourceGroups({ group_type: 'resource', page_size: 200 }),
    api.apiClient.getResourceGroups({ group_type: 'account', page_size: 200 }),
  ])
  instanceGroupOptions.value = resourceGroups.status === 'fulfilled'
    ? (resourceGroups.value.items ?? []).map(group => group.name).filter(Boolean)
    : []
  accountGroupOptions.value = accountGroups.status === 'fulfilled'
    ? (accountGroups.value.items ?? []).map(group => group.name).filter(Boolean)
    : []
}

const autoProvisionVisible = ref(false)

function openAutoProvision() {
  if (!selectedInstance.value?.id) return
  autoProvisionVisible.value = true
}

function handleProvisionedAccountCreated() {
  void Promise.all([
    loadSelectedInstanceAccounts(),
    loadInstances(),
  ])
}

</script>

<style scoped>
.accounts-table {
  height: min(64dvh, 620px);
  min-height: 360px;
}

.database-resource-form :deep(.el-select),
.database-resource-form :deep(.el-input-number),
.database-resource-form :deep(.el-date-editor) {
  width: 100%;
}

.tls-preflight-status {
  display: grid;
  gap: 8px;
  margin-bottom: 18px;
}

.tls-preflight-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tls-preflight-actions :deep(.el-button) {
  margin: 0;
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

/* 协议标签统一宽度 */
.protocol-tag {
  width: 80px;
  justify-content: center;
}

.tls-ca-status {
  color: #667085;
  font-size: 12px;
}

.tls-mode-help {
  color: #667085;
  font-size: 12px;
  line-height: 1.5;
  margin-top: 6px;
}

.tls-ca-editor {
  width: 100%;
}

.tls-ca-help {
  margin-bottom: 8px;
  color: #667085;
  font-size: 12px;
  line-height: 1.5;
}

.tls-ca-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 8px;
}

.tls-ca-file-input {
  display: none;
}

/* 账号管理按钮 */
.account-mgmt-btn {
  font-size: 12px;
  padding: 0 2px;
}

/* Table actions */
.table-actions {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  width: 100%;
}
.table-actions :deep(.el-button) {
  margin-left: 0;
}
.danger-dropdown-item {
  color: var(--el-color-danger);
}

@media (max-width: 720px) {
  .accounts-table {
    height: min(66dvh, 520px);
    min-height: 280px;
  }

  .config-row {
    grid-template-columns: 1fr;
  }
}
</style>
