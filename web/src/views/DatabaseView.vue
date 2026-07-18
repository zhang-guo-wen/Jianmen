<template>
  <div class="view-stack">
    <DataTableCard
      :data="displayedInstances"
      :loading="instancesLoading"
      :total="instanceTotal"
      v-model:page="instancePage"
      v-model:page-size="instancePageSize"
      search-placeholder="搜索实例名称、地址、协议..."
      @search="onInstanceSearch"
    >
      <template #toolbar-extra>
        <div class="instance-filter-bar">
          <div class="instance-filter-options" :class="{ 'is-expanded': instanceFiltersExpanded }">
            <el-button size="small" :type="instanceFilter === 'all' ? 'primary' : undefined" @click="setInstanceFilter('all')">全部</el-button>
            <el-button size="small" :type="instanceFilter === 'popular' ? 'primary' : undefined" @click="setInstanceFilter('popular')">常用</el-button>
            <el-button
              v-for="option in visibleInstanceQuickGroupOptions"
              :key="option.value"
              size="small"
              :type="instanceFilter === option.value ? 'primary' : undefined"
              @click="setInstanceFilter(option.value)"
            >
              {{ option.label }}
            </el-button>
          </div>
          <el-button v-if="instanceQuickGroupOptions.length > filterPreviewLimit" link size="small" class="instance-filter-more" @click="instanceFiltersExpanded = !instanceFiltersExpanded">
            {{ instanceFiltersExpanded ? '收起' : '更多' }}
          </el-button>
        </div>
        <el-button v-if="permission.canDo('dbproxy:create')" type="primary" @click="openCreateInstance">新增实例</el-button>
      </template>
      <el-table-column prop="name" label="名称" min-width="130" show-overflow-tooltip />
      <el-table-column label="地址" min-width="180" show-overflow-tooltip>
        <template #default="{ row }">{{ instanceEndpoint(row) }}</template>
      </el-table-column>
      <el-table-column label="协议" width="100" align="center">
        <template #default="{ row }">
          <el-tag class="protocol-tag" size="small" :type="row.protocol === 'mysql' ? 'success' : row.protocol === 'redis' ? 'danger' : 'primary'" effect="light">{{ row.protocol === 'mysql' ? 'MySQL' : row.protocol === 'redis' ? 'Redis' : 'PostgreSQL' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="TLS" min-width="145">
        <template #default="{ row }">
          <div class="tls-summary">
            <el-tag size="small" :type="tlsModeTagType(row.tls_mode)">{{ tlsModeLabel(row.tls_mode) }}</el-tag>
            <span v-if="row.has_tls_ca" class="tls-ca-status">CA 已配置</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="账号管理" min-width="110" align="center">
        <template #default="{ row }">
          <el-button link type="primary" size="small" class="account-mgmt-btn" @click="showAccounts(row)">账号管理({{ row.account_count ?? 0 }})</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="group" label="分组" width="100" show-overflow-tooltip />
      <el-table-column label="状态" width="70" align="center">
        <template #default="{ row }">
          <StatusSwitch v-if="row.can_manage && permission.canDo('dbproxy:update')" :model-value="row.status === 'active'" @update:model-value="toggleInstance(row)" />
        </template>
      </el-table-column>
      <el-table-column prop="remark" label="备注" min-width="120" show-overflow-tooltip />
      <el-table-column label="操作" width="210" align="right">
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
        <el-collapse v-model="instanceMorePanels">
          <el-collapse-item name="more" title="更多设置">
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
            <el-form-item label="TLS 模式">
              <el-select v-model="instanceForm.tlsMode" @change="onTLSModeChange">
                <el-option label="验证证书和主机名（推荐）" value="verify-full" />
                <el-option label="仅验证证书" value="verify-ca" />
                <el-option label="禁用 TLS（高风险）" value="disable" />
              </el-select>
              <div class="tls-mode-help">{{ tlsModeDescription(instanceForm.tlsMode) }}</div>
              <el-alert
                v-if="instanceForm.tlsMode === 'disable'"
                class="tls-risk-alert"
                type="warning"
                :closable="false"
                show-icon
              >
                不加密且不校验证书，存在凭据被窃听风险；远程 Redis 实例会被后端拒绝。
              </el-alert>
            </el-form-item>
            <el-form-item label="TLS 主机名">
              <el-input v-model="instanceForm.tlsServerName" placeholder="verify-full 时用于主机名校验，可留空使用地址" />
            </el-form-item>
            <el-form-item label="TLS CA">
              <div class="tls-ca-editor">
                <div class="tls-ca-actions">
                  <el-button size="small" :disabled="instanceForm.tlsMode === 'disable'" @click="chooseTLSCAFile">选择 PEM 文件</el-button>
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
                    @click="clearTLSCA"
                  >
                    清除 CA
                  </el-button>
                </div>
                <el-input
                  v-model="instanceForm.tlsCaPem"
                  type="textarea"
                  :rows="4"
                  :disabled="instanceForm.tlsMode === 'disable'"
                  placeholder="也可以手动粘贴 PEM 内容；编辑时留空会保留已有 CA"
                  @input="onTLSCAPEMInput"
                />
              </div>
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
          <el-button v-if="selectedInstance?.can_manage && permission.canDo('dbproxy:create')" type="primary" :disabled="!selectedInstance" @click="openCreateAccount">
            新增账号
          </el-button>
          <el-button v-if="selectedInstance?.can_manage && permission.canDo('dbproxy:create')" type="success" :disabled="!selectedInstance || selectedInstance.protocol !== 'mysql'" @click="openAutoProvision">
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
              v-if="row.can_manage && permission.canDo('dbproxy:update')"
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
            <el-button v-if="permission.canDo('db:connect')" link type="success" size="small" @click="openConnectDialog(row)">连接</el-button>
            <el-button v-if="row.can_manage && permission.canDo('dbproxy:update')" link type="primary" size="small" @click="editAccount(row)">编辑</el-button>
            <el-button v-if="row.can_manage && permission.canDo('dbproxy:delete')" link type="danger" size="small" @click="deleteAccount(row)">删除</el-button>
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
        <el-collapse v-model="accountMorePanels">
          <el-collapse-item name="more" title="更多设置">
            <el-form-item label="分组">
              <el-select
                v-model="accountForm.group"
                allow-create
                clearable
                default-first-option
                filterable
                placeholder="选择或输入分组"
              >
                <el-option
                  v-for="g in accountGroupOptions"
                  :key="g"
                  :label="g"
                  :value="g"
                />
              </el-select>
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
          <el-form-item label="主机">
            <el-input v-model="provision.host" placeholder="例如 10.0.0.8（必填，禁止通配符）" />
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
            <el-descriptions-item label="资源标识">
              <code>{{ provisionResult.account.resource_id }}</code>
            </el-descriptions-item>
            <el-descriptions-item label="主机">{{ provision.host }}</el-descriptions-item>
          </el-descriptions>
        </template>
        <el-alert v-else-if="provisionError" type="error" :title="provisionError" :closable="false" show-icon />
      </template>

      <template #footer>
        <el-button @click="autoProvisionVisible = false">取消</el-button>
        <el-button v-if="provisionStep === 1" type="primary" :disabled="!provision.adminAccountId || !provision.host.trim()" @click="goProvisionStep2">下一步</el-button>
        <el-button v-if="provisionStep === 2" :disabled="loadingDatabases" @click="provisionStep = 1">上一步</el-button>
        <el-button v-if="provisionStep === 2" type="primary" :disabled="provisioning || loadingDatabases" @click="doProvision">创建</el-button>
        <el-button v-if="provisionStep === 3 && !provisioning" type="primary" @click="closeProvisionAndRefresh">完成</el-button>
      </template>
    </el-dialog>

    <ConnectionConfigDialog
      v-model="connectDialogVisible"
      resource-type="database"
      :target="connectTarget"
      :resource-name="String(selectedInstance?.name || '')"
      :source-address="selectedInstance ? instanceEndpoint(selectedInstance) : ''"
      :source-account="String(connectTarget?.username || '')"
      :protocol="String(selectedInstance?.protocol || 'mysql')"
      :allow-ssh="false"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, Loading } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import DataTableCard from '@/components/DataTableCard.vue'
import FormDialog from '@/components/FormDialog.vue'
import ConnectionConfigDialog from '@/components/ConnectionConfigDialog.vue'
import StatusSwitch from '@/components/StatusSwitch.vue'
import * as api from '@/api/client'
import { usePermissionStore } from '@/stores/permission'
import { createProvisionIdempotencySession, type ProvisionRequest } from '@/utils/provisioningRequest'
import { createLatestKeyedRequest } from '@/utils/connectionRequestState'

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
const instanceFiltersExpanded = ref(false)
const instanceUsageCounts = ref<Record<string, number>>({})
const filterPreviewLimit = 6
const showInstanceDialog = ref(false)
const submitting = ref(false)
const editingInstance = ref<api.DatabaseInstanceView | null>(null)
const instanceMorePanels = ref<string[]>([])
const instanceGroupOptions = ref<string[]>([])
const accountGroupOptions = ref<string[]>([])
const tlsCAFileInput = ref<HTMLInputElement | null>(null)
const previousTLSMode = ref<api.DatabaseTLSMode>('verify-full')
const originalHasTLSCA = ref(false)
const instanceForm = reactive<InstanceForm>({
  name: '',
  protocol: 'mysql',
  address: '',
  port: 3306,
  group: '',
  remark: '',
  tlsMode: 'verify-full',
  tlsServerName: '',
  tlsCaPem: '',
  hasTlsCa: false,
  clearTlsCa: false
})

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

// ── Gateway config ──
const accountFormTesting = ref(false)
const accountFormTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null)
const savedCredentialTesting = ref(false)
const savedCredentialTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null)

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

const visibleInstanceQuickGroupOptions = computed(() => {
  if (instanceFiltersExpanded.value) return instanceQuickGroupOptions.value
  const options = instanceQuickGroupOptions.value.slice(0, filterPreviewLimit)
  if (instanceFilter.value !== 'all' && instanceFilter.value !== 'popular' && !options.some(option => option.value === instanceFilter.value)) {
    const selected = instanceQuickGroupOptions.value.find(option => option.value === instanceFilter.value)
    if (selected) return [selected, ...options.slice(0, filterPreviewLimit - 1)]
  }
  return options
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
      const name = String(connection.target_name || connection.upstream_addr || '').trim().toLowerCase()
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
  return value === 'disable' || value === 'verify-ca' || value === 'verify-full' ? value : 'verify-full'
}

function tlsModeLabel(value: unknown): string {
  switch (normalizeTLSMode(value)) {
    case 'disable': return '已禁用'
    case 'verify-ca': return '验证 CA'
    default: return '验证 CA + 主机名'
  }
}

function tlsModeTagType(value: unknown): 'success' | 'warning' | 'danger' {
  switch (normalizeTLSMode(value)) {
    case 'disable': return 'danger'
    case 'verify-ca': return 'warning'
    default: return 'success'
  }
}

function tlsModeDescription(value: api.DatabaseTLSMode): string {
  switch (value) {
    case 'disable': return '不加密，也不校验证书；风险最高，仅适用于受控的本机链路。'
    case 'verify-ca': return '加密并验证 CA，但不校验主机名；安全性低于 verify-full。'
    default: return '加密并验证 CA 与主机名，可防止中间人攻击，推荐使用。'
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
    ElMessage.error(err instanceof Error ? err.message : '??????')
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
  instanceMorePanels.value = ['more']
  originalHasTLSCA.value = false
  previousTLSMode.value = 'verify-full'
  Object.assign(instanceForm, {
    name: '',
    protocol: 'mysql',
    address: '',
    port: 3306,
    group: '',
    remark: '',
    tlsMode: 'verify-full',
    tlsServerName: '',
    tlsCaPem: '',
    hasTlsCa: false,
    clearTlsCa: false
  })
  showInstanceDialog.value = true
}

function editInstance(inst: api.DatabaseInstanceView) {
  editingInstance.value = inst
  instanceMorePanels.value = []
  const tlsMode = normalizeTLSMode(inst.tls_mode)
  originalHasTLSCA.value = Boolean(inst.has_tls_ca)
  previousTLSMode.value = tlsMode
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

async function onTLSModeChange(mode: api.DatabaseTLSMode) {
  if (mode !== 'disable') {
    if (previousTLSMode.value === 'disable') {
      instanceForm.hasTlsCa = originalHasTLSCA.value
      instanceForm.clearTlsCa = false
    }
    previousTLSMode.value = mode
    return
  }
  try {
    await ElMessageBox.confirm(
      '禁用 TLS 会使上游凭据通过明文链路传输，远程 Redis 也会被后端拒绝。确定继续吗？',
      '高风险设置',
      { type: 'warning', confirmButtonText: '继续禁用', cancelButtonText: '取消' }
    )
  } catch {
    instanceForm.tlsMode = previousTLSMode.value
    return
  }
  instanceForm.tlsCaPem = ''
  instanceForm.hasTlsCa = false
  instanceForm.clearTlsCa = editingInstance.value ? originalHasTLSCA.value : false
  previousTLSMode.value = mode
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
  } catch {
    ElMessage.error('读取 PEM 文件失败')
  }
}

function onTLSCAPEMInput() {
  if (instanceForm.tlsCaPem.trim()) {
    instanceForm.hasTlsCa = true
    instanceForm.clearTlsCa = false
  }
}

function clearTLSCA() {
  instanceForm.tlsCaPem = ''
  instanceForm.hasTlsCa = false
  instanceForm.clearTlsCa = Boolean(editingInstance.value && originalHasTLSCA.value)
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
      tls_mode: instanceForm.tlsMode,
      tls_server_name: instanceForm.tlsServerName.trim() || undefined,
      group: instanceForm.group.trim() || undefined,
      remark: instanceForm.remark.trim() || undefined
    }
    if (instanceForm.tlsCaPem.trim()) payload.tls_ca_pem = instanceForm.tlsCaPem.trim()
    if (instanceForm.clearTlsCa) payload.clear_tls_ca = true
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
      tls_mode: normalizeTLSMode(inst.tls_mode),
      tls_server_name: inst.tls_server_name || undefined,
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
  expiryPreset.value = ''
  accountFormTestResult.value = null
  savedCredentialTestResult.value = null
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
    instanceForm.tlsCaPem = ''
    tlsCAFileInput.value = null
  }
})

onMounted(() => {
  loadInstances()
  loadGroupOptions()
})

async function loadGroupOptions() {
  try {
    const resourceGroups = await api.apiClient.getResourceGroups({ group_type: 'resource' })
    const accountGroups = await api.apiClient.getResourceGroups({ group_type: 'account' })
    instanceGroupOptions.value = (resourceGroups.items ?? []).map(g => g.name).filter(Boolean)
    accountGroupOptions.value = (accountGroups.items ?? []).map(g => g.name).filter(Boolean)
  } catch {
    // ignore
  }
}

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
const provisionIdempotency = createProvisionIdempotencySession()

const provision = reactive({
  adminAccountId: '',
  host: '',
})

async function openAutoProvision() {
  if (!selectedInstance.value) return
  provisionIdempotency.reset()
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
  provision.host = ''
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
  if (!selectedInstance.value || provisioning.value) return
  provisioning.value = true
  provisionError.value = ''
  const payload: ProvisionRequest = {
    admin_account_id: provision.adminAccountId,
    host: provision.host,
    grants: dbGrants.value
      .filter(r => r.privilege !== '')
      .map(r => ({ database: r.database, privilege: r.privilege })),
  }
  const idempotencyKey = provisionIdempotency.keyFor(payload, selectedInstance.value.id!)
  try {
    const res = await api.apiClient.provisionDBAccount(selectedInstance.value.id!, payload, idempotencyKey)
    if (res.ok === false) throw new Error('自动供应未成功，请重试')
    provisionResult.value = res
    provisionStep.value = 3
    provisionIdempotency.markSucceeded()
  } catch (e: any) {
    provisionIdempotency.markFailed()
    provisionError.value = e.message || String(e)
  } finally {
    provisioning.value = false
  }
}

function resetAutoProvision() {
  provisionIdempotency.reset()
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
.instance-filter-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  max-width: min(620px, 100%);
}

.instance-filter-options {
  display: flex;
  gap: 6px;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
}

.instance-filter-options.is-expanded {
  flex-wrap: wrap;
  overflow: visible;
  white-space: normal;
}

.instance-filter-options .el-button,
.instance-filter-more {
  flex: 0 0 auto;
  margin: 0;
}

.instance-filter-options .el-button {
  padding-inline: 9px;
}

.instance-filter-more {
  padding-inline: 4px;
}

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

/* 协议标签统一宽度 */
.protocol-tag {
  width: 80px;
  justify-content: center;
}

.tls-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
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

.tls-risk-alert {
  margin-top: 8px;
}

.tls-ca-editor {
  width: 100%;
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
  .config-row {
    grid-template-columns: 1fr;
  }
}
</style>
