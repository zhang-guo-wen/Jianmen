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
      <el-table-column prop="address" label="地址" min-width="130" show-overflow-tooltip />
      <el-table-column prop="port" label="端口" width="70" />
      <el-table-column label="协议" width="80" align="center">
        <template #default="{ row }">
          <el-tag size="small" :type="row.protocol === 'mysql' ? 'success' : row.protocol === 'redis' ? 'danger' : 'primary'" effect="plain">{{ row.protocol === 'mysql' ? 'MySQL' : row.protocol === 'redis' ? 'Redis' : 'PG' }}</el-tag>
        </template>
      </el-table-column>
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
      <el-table-column label="操作" width="220">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="editInstance(row)">编辑</el-button>
          <el-button link type="primary" size="small" @click="openCreateAccountForInstance(row)">新建账号</el-button>
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
          <el-select v-model="instanceForm.protocol" @change="onProtocolChange">
            <el-option label="MySQL" value="mysql" />
            <el-option label="PostgreSQL" value="postgres" />
            <el-option label="Redis" value="redis" />
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
          <el-button type="success" :disabled="!selectedInstance || selectedInstance.protocol !== 'mysql'" @click="openAutoProvision">
            自动创建
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
          <template #default="{ row }">{{ formatTime(row.expires_at) || '永久' }}</template>
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
        <el-form-item :label="selectedInstance?.protocol === 'redis' ? '目标用户名' : '目标用户名'" :required="selectedInstance?.protocol !== 'redis'">
          <el-input v-model="accountForm.username" :placeholder="selectedInstance?.protocol === 'redis' ? 'Redis ACL 用户名（可选，留空则使用单一密码认证）' : '数据库登录用户名'" />
        </el-form-item>
        <el-form-item label="目标密码">
          <el-input v-model="accountForm.password" type="password" show-password
            :placeholder="editingAccount ? '留空则保留原密码' : '数据库登录密码'" />
        </el-form-item>
        <el-form-item label="连接测试">
          <div class="test-connection-row">
            <el-button :loading="accountFormTesting" @click="testAccountFormConnection">测试连接</el-button>
            <template v-if="accountFormTestResult">
              <el-tag :type="accountFormTestResult.ok ? 'success' : 'danger'" size="small">
                {{ accountFormTestResult.ok ? '可达' : '不可达' }}
              </el-tag>
              <span v-if="accountFormTestResult.latency_ms !== undefined" class="test-connection-meta">
                延迟 {{ accountFormTestResult.latency_ms }}ms
              </span>
              <span v-if="accountFormTestResult.error" class="test-connection-error">
                {{ accountFormTestResult.error }}
              </span>
            </template>
          </div>
          <div v-if="editingAccount" class="test-connection-hint">
            点击测试连接时必须重新输入数据库密码；保存时密码留空仍会保留原密码。
          </div>
          <div v-if="editingAccount" class="test-connection-row saved-credential-row">
            <span class="test-connection-meta">已保存凭据：</span>
            <el-tag v-if="savedCredentialTesting" type="info" size="small">测试中...</el-tag>
            <template v-else-if="savedCredentialTestResult">
              <el-tag :type="savedCredentialTestResult.ok ? 'success' : 'danger'" size="small">
                {{ savedCredentialTestResult.ok ? '可达' : '不可达' }}
              </el-tag>
              <span v-if="savedCredentialTestResult.latency_ms !== undefined" class="test-connection-meta">
                延迟 {{ savedCredentialTestResult.latency_ms }}ms
              </span>
              <span v-if="savedCredentialTestResult.error" class="test-connection-error">
                {{ savedCredentialTestResult.error }}
              </span>
            </template>
          </div>
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

    <!-- 自动创建账号弹窗 -->
    <el-dialog
      v-model="autoProvisionVisible"
      title="自动创建 MySQL 账号"
      width="min(720px, calc(100vw - 32px))"
      destroy-on-close
      @closed="resetAutoProvision"
    >
      <template v-if="provisionStep === 1">
        <el-form label-width="100px">
          <el-form-item label="管理员凭据">
            <el-select v-model="provision.adminAccountId" placeholder="选择用于创建账号的凭据" style="width:100%">
              <el-option
                v-for="acc in adminAccounts"
                :key="acc.id"
                :label="`${acc.username} (${acc.unique_name})`"
                :value="acc.id"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="新用户名">
            <el-input v-model="provision.newUsername" placeholder="留空自动生成" />
          </el-form-item>
          <el-form-item label="主机">
            <el-input v-model="provision.host" placeholder="%" />
          </el-form-item>
        </el-form>
      </template>

      <template v-else-if="provisionStep === 2">
        <div v-if="loadingDatabases" style="text-align:center;padding:30px 0">
          <el-icon class="is-loading" :size="24"><Loading /></el-icon>
          <p style="margin-top:8px;color:#999">正在获取数据库列表...</p>
        </div>
        <template v-else>
          <div style="margin-bottom:8px;display:flex;gap:8px">
            <el-button size="small" @click="setAllDBGrants('readwrite')">全部读写</el-button>
            <el-button size="small" @click="setAllDBGrants('read')">全部只读</el-button>
            <el-button size="small" @click="setAllDBGrants('')">全部无</el-button>
          </div>
          <el-table :data="dbGrants" size="small" max-height="340">
            <el-table-column prop="database" label="数据库" />
            <el-table-column label="权限" width="180" align="center">
              <template #default="{ row }">
                <el-radio-group v-model="row.privilege" size="small">
                  <el-radio-button value="">无</el-radio-button>
                  <el-radio-button value="read">读</el-radio-button>
                  <el-radio-button value="readwrite">读写</el-radio-button>
                </el-radio-group>
              </template>
            </el-table-column>
          </el-table>
        </template>
      </template>

      <template v-else-if="provisionStep === 3">
        <div v-if="provisioning" style="text-align:center;padding:30px 0">
          <el-icon class="is-loading" :size="28"><Loading /></el-icon>
          <p style="margin-top:10px;color:#667085">正在目标 MySQL 上创建账号...</p>
        </div>
        <template v-else-if="provisionResult">
          <el-alert type="success" title="账号创建成功" :closable="false" show-icon />
          <el-descriptions :column="1" border size="small" style="margin-top:12px">
            <el-descriptions-item label="用户名">
              <code>{{ provisionResult.account.username }}</code>
            </el-descriptions-item>
            <el-descriptions-item label="密码">
              <code>{{ provisionResult.generated_password }}</code>
              <el-button link type="primary" size="small" style="margin-left:8px" @click="copyText(provisionResult.generated_password)">复制</el-button>
            </el-descriptions-item>
            <el-descriptions-item label="主机">{{ provision.host || '%' }}</el-descriptions-item>
          </el-descriptions>
          <el-alert type="warning" :closable="false" style="margin-top:8px" title="密码仅显示一次，请立即复制保存" />
        </template>
        <el-alert v-else-if="provisionError" type="error" :title="provisionError" :closable="false" show-icon />
      </template>

      <template #footer>
        <el-button @click="autoProvisionVisible = false">取消</el-button>
        <el-button v-if="provisionStep === 1" type="primary" :disabled="!provision.adminAccountId" @click="goProvisionStep2">下一步</el-button>
        <el-button v-if="provisionStep === 2" :disabled="loadingDatabases" @click="provisionStep = 1">上一步</el-button>
        <el-button v-if="provisionStep === 2" type="primary" :disabled="provisioning || loadingDatabases" @click="doProvision">创建</el-button>
        <el-button v-if="provisionStep === 3 && !provisioning" type="primary" @click="closeProvisionAndRefresh">完成</el-button>
      </template>
    </el-dialog>

    <!-- 连接弹窗 -->
    <el-dialog
      v-model="connectDialogVisible"
      destroy-on-close
      title="连接数据库账号"
      width="min(720px, calc(100vw - 32px))"
      @opened="onConnectDialogOpened"
    >
      <div v-if="connectTarget" class="connection-dialog">
        <el-alert show-icon type="info" :closable="false"
          title="输入堡垒机的登录密码，不是目标数据库的密码" />

        <div style="margin-bottom: 8px; display: flex; align-items: center; gap: 8px;" v-if="!creatingSession">
          <span style="font-size: 13px; color: #667085;">连通性：</span>
          <el-tag v-if="connectionTesting" type="info" size="small">测试中...</el-tag>
          <template v-else-if="connectionTestResult !== null">
            <el-tag :type="connectionTestResult.ok ? 'success' : 'danger'" size="small">
              {{ connectionTestResult.ok ? '可达' : '不可达' }}
            </el-tag>
            <span v-if="connectionTestResult.latency_ms !== undefined" style="font-size: 12px; color: #667085;">
              延迟 {{ connectionTestResult.latency_ms }}ms
            </span>
            <span v-if="connectionTestResult.error" style="font-size: 12px; color: var(--el-color-danger);">
              {{ connectionTestResult.error }}
            </span>
          </template>
        </div>

        <div v-if="creatingSession" style="text-align: center; padding: 30px 0;">
          <el-icon class="is-loading" :size="28"><Loading /></el-icon>
          <p style="margin-top: 10px; color: #667085;">正在创建连接会话...</p>
        </div>

        <template v-else-if="!connectionError && compactUser">
          <el-descriptions :column="1" border size="small" style="margin-top: 12px">
            <el-descriptions-item label="连接地址">
              <code>{{ gatewayHost }}:{{ gatewayPort }}</code>
              <el-button link type="primary" size="small" style="margin-left: 8px" @click="copyText(`${gatewayHost}:${gatewayPort}`)">复制</el-button>
            </el-descriptions-item>
            <el-descriptions-item label="用户名">
              <code>{{ compactUser }}</code>
              <el-button link type="primary" size="small" style="margin-left: 8px" @click="copyText(compactUser)">复制</el-button>
            </el-descriptions-item>
            <el-descriptions-item label="密码">堡垒机登录密码</el-descriptions-item>
          </el-descriptions>

          <div style="margin-top: 12px">
            <el-input :model-value="connectCommand" readonly size="small">
              <template #append>
                <el-button @click="copyText(connectCommand)">复制命令</el-button>
              </template>
            </el-input>
          </div>
        </template>
      </div>
      <template #footer>
        <el-button @click="connectDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch, computed, onMounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
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

const creatingSession = ref(false)
const connectionError = ref('')
const connectionTesting = ref(false)
const connectionTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null)
const accountFormTesting = ref(false)
const accountFormTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null)
const savedCredentialTesting = ref(false)
const savedCredentialTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null)

const compactUser = computed(() => {
  if (!connectTarget.value) return ''
  const resourceId = connectTarget.value.resource_id || '0000'
  const sessionId = userSessionId.value
  if (!sessionId) return ''
  const inst = selectedInstance.value
  const prefix = inst?.protocol === 'redis' ? 'R' : 'D'
  return prefix + resourceId + sessionId
})
const gatewayHost = computed(() => gatewayConfig.value.host)
const gatewayPort = computed(() => gatewayConfig.value.port)

const connectCommand = computed(() => {
  if (!connectTarget.value || !selectedInstance.value) return ''
  const inst = selectedInstance.value
  const protocol = inst.protocol || 'mysql'
  const resourceId = connectTarget.value.resource_id || '0000'
  const sessionId = userSessionId.value || '00001'
  const prefix = protocol === 'redis' ? 'R' : 'D'
  const compactUser = `${prefix}${resourceId}${sessionId}`
  const host = gatewayConfig.value.host
  const proxyPort = gatewayConfig.value.port
  if (protocol === 'mysql') {
    return `mysql --protocol=tcp -h ${host} -P ${proxyPort} -u ${compactUser} -p`
  }
  if (protocol === 'redis') {
    return `redis-cli -h ${host} -p ${proxyPort} -a ${compactUser} --user ${connectTarget.value.username || 'default'}`
  }
  return `psql -h ${host} -p ${proxyPort} -U ${compactUser}`
})

// ── Helpers ──
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

async function openCreateAccountForInstance(inst: api.DatabaseInstanceView) {
  showAccounts(inst)
  await nextTick()
  openCreateAccount()
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

function onProtocolChange(protocol: string) {
  if (!editingInstance.value) {
    switch (protocol) {
      case 'redis': instanceForm.port = 6379; break
      case 'postgres': instanceForm.port = 5432; break
      default: instanceForm.port = 3306; break
    }
  }
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
  accountFormTestResult.value = null
  savedCredentialTestResult.value = null
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
  accountFormTestResult.value = null
  savedCredentialTestResult.value = null
  accountDialogVisible.value = true
  testSavedAccountConnection(row)
}

function setExpiry(opt: { label: string; hours: number }) {
  expiryPreset.value = opt.label
  if (opt.hours === -1) {
    accountForm.expiresAt = null
  } else {
    accountForm.expiresAt = new Date(Date.now() + opt.hours * 3600 * 1000)
  }
}

async function testSavedAccountConnection(row: api.DBAccountRecord) {
  const id = row.id || row.resource_id || ''
  if (!id) return
  savedCredentialTesting.value = true
  savedCredentialTestResult.value = null
  try {
    const result = await api.apiClient.testDBConnection(String(id))
    savedCredentialTestResult.value = { ok: result.ok, latency_ms: result.latency_ms, error: result.ok ? undefined : (result.error || '连接失败') }
  } catch (err) {
    savedCredentialTestResult.value = { ok: false, error: err instanceof Error ? err.message : '连接失败' }
  } finally {
    savedCredentialTesting.value = false
  }
}

async function testAccountFormConnection() {
  if (!selectedInstance.value?.id) {
    ElMessage.warning('请先选择数据库实例')
    return
  }
  const isRedis = selectedInstance.value.protocol === 'redis'
  if (!isRedis && !accountForm.username.trim()) {
    ElMessage.warning('请输入目标用户名')
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
  if (!isRedis && !accountForm.username.trim()) { ElMessage.warning('请输入目标用户名'); return }
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
  connectTarget.value = acc
  selectedInstance.value = instances.value.find(i => i.id === acc.instance_id) || selectedInstance.value
  userSessionId.value = ''
  connectionError.value = ''
  connectionTestResult.value = null
  connectDialogVisible.value = true
}

async function onConnectDialogOpened() {
  if (!connectTarget.value) return
  creatingSession.value = true
  connectionError.value = ''
  connectionTestResult.value = null
  testDBConnectionForTarget()
  try {
    const targetId = connectTarget.value.id || connectTarget.value.resource_id || ''
    if (targetId) {
      const session = await api.apiClient.createUserSession(String(targetId))
      userSessionId.value = session?.session_id || ''
    }
  } catch (err) {
    connectionError.value = err instanceof Error ? err.message : '创建会话失败'
  } finally {
    creatingSession.value = false
  }
}

async function testDBConnectionForTarget() {
  if (!connectTarget.value) return
  connectionTesting.value = true
  connectionTestResult.value = null
  try {
    const id = connectTarget.value.id || connectTarget.value.resource_id || ''
    if (!id) return
    const result = await api.apiClient.testDBConnection(String(id))
    connectionTestResult.value = { ok: result.ok, latency_ms: result.latency_ms, error: result.ok ? undefined : (result.error || '连接失败') }
  } catch (err) {
    connectionTestResult.value = { ok: false, error: err instanceof Error ? err.message : '连接失败' }
  } finally {
    connectionTesting.value = false
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

// ── 自动创建 ──
interface DBGrantRow {
  database: string
  privilege: '' | 'read' | 'readwrite'
}

const autoProvisionVisible = ref(false)
const provisionStep = ref(1)
const provisioning = ref(false)
const loadingDatabases = ref(false)
const provisionError = ref('')
const provisionResult = ref<any>(null)
const dbGrants = ref<DBGrantRow[]>([])
const adminAccounts = ref<any[]>([])

const provision = reactive({
  adminAccountId: '',
  newUsername: '',
  host: '%',
})

async function openAutoProvision() {
  if (!selectedInstance.value) return
  const instId = selectedInstance.value.id!
  try {
    const res = await api.apiClient.getDBAccounts(instId, { page_size: 200 })
    const items = res.items ?? []
    adminAccounts.value = items.filter((a: any) => a.status === 'active')
  } catch {
    adminAccounts.value = []
  }
  if (adminAccounts.value.length > 0) {
    provision.adminAccountId = adminAccounts.value[0].id
  } else {
    provision.adminAccountId = ''
  }
  provision.newUsername = ''
  provision.host = '%'
  provisionStep.value = 1
  provisionError.value = ''
  provisionResult.value = null
  autoProvisionVisible.value = true
}

async function goProvisionStep2() {
  if (!provision.adminAccountId || !selectedInstance.value) return
  loadingDatabases.value = true
  try {
    const res = await api.apiClient.listDBDatabases(selectedInstance.value.id!, provision.adminAccountId)
    const dbs: string[] = res.databases ?? []
    dbGrants.value = dbs.map(db => ({ database: db, privilege: '' as const }))
    provisionStep.value = 2
  } catch (e: any) {
    ElMessage.error('获取数据库列表失败: ' + (e.message || e))
  } finally {
    loadingDatabases.value = false
  }
}

function setAllDBGrants(p: '' | 'read' | 'readwrite') {
  dbGrants.value.forEach(row => { row.privilege = p })
}

async function doProvision() {
  if (!selectedInstance.value) return
  provisioning.value = true
  provisionError.value = ''
  try {
    const grants = dbGrants.value
      .filter(r => r.privilege !== '')
      .map(r => ({ database: r.database, privilege: r.privilege }))
    const res = await api.apiClient.provisionDBAccount(selectedInstance.value.id!, {
      admin_account_id: provision.adminAccountId,
      new_username: provision.newUsername || undefined,
      host: provision.host || '%',
      grants,
    })
    provisionResult.value = res
    provisionStep.value = 3
  } catch (e: any) {
    provisionError.value = e.message || String(e)
  } finally {
    provisioning.value = false
  }
}

function resetAutoProvision() {
  provisionStep.value = 1
  provisionError.value = ''
  provisionResult.value = null
  dbGrants.value = []
}

function closeProvisionAndRefresh() {
  autoProvisionVisible.value = false
  loadSelectedInstanceAccounts()
}
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

.test-connection-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.saved-credential-row {
  margin-top: 8px;
}

.test-connection-meta {
  color: #667085;
  font-size: 12px;
}

.test-connection-error {
  color: var(--el-color-danger);
  font-size: 12px;
}

.test-connection-hint {
  color: #667085;
  font-size: 12px;
  line-height: 1.5;
  margin-top: 6px;
  width: 100%;
}

@media (max-width: 720px) {
  .config-row {
    grid-template-columns: 1fr;
  }
}
</style>
