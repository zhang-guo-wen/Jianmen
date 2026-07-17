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

      <!-- 创建资源授权对话框 -->
      <el-dialog
        v-model="grantDialogVisible"
        :title="t('resourceGrant.addGrant')"
        width="800px"
        top="4vh"
        class="resource-grant-dialog"
      >
        <el-form :model="grantForm" label-width="120px">
          <el-form-item :label="t('resourceGrant.principalType')" required>
            <el-radio-group v-model="grantForm.principal_type">
              <el-radio value="user">{{ t('resourceGrant.user') }}</el-radio>
              <el-radio value="user_group">{{ t('resourceGrant.userGroup') }}</el-radio>
            </el-radio-group>
          </el-form-item>

          <el-form-item :label="grantForm.principal_type === 'user' ? t('resourceGrant.selectUser') : t('resourceGrant.selectGroup')" required>
            <el-select
              v-model="grantForm.principal_id"
              filterable
              :placeholder="grantForm.principal_type === 'user' ? t('resourceGrant.searchUser') : t('resourceGrant.searchGroup')"
              style="width: 100%"
            >
              <el-option
                v-for="item in principalOptions"
                :key="item.id"
                :label="item.name"
                :value="item.id"
              />
            </el-select>
          </el-form-item>

          <el-form-item :label="t('resourceGrant.selectResource')" required>
            <div class="resource-select-inline">
              <el-tabs v-model="resourceTabType" @tab-change="handleResourceTabChange" class="resource-tabs">
                <el-tab-pane label="主机" name="host" />
                <el-tab-pane label="数据库" name="database_instance" />
                <el-tab-pane :label="t('resourceGrant.hostAccounts')" name="host_account" />
                <el-tab-pane :label="t('resourceGrant.databaseAccounts')" name="database_account" />
                <el-tab-pane label="应用" name="application" />
                <el-tab-pane :label="t('resourceGrant.resourceGroups')" name="resource_group" />
                <el-tab-pane :label="t('resourceGrant.accountGroups')" name="account_group" />
              </el-tabs>

              <el-input
                v-model="resourceSearchQuery"
                :placeholder="t('resourceGrant.searchResource')"
                clearable
                class="resource-search"
              >
                <template #prefix>
                  <el-icon><Search /></el-icon>
                </template>
              </el-input>

              <el-table
                ref="resourceTableRef"
                :data="filteredResources"
                v-loading="loadingResources"
                height="210"
                row-key="id"
                stripe
                @select="handleResourceSelect"
                @select-all="handleResourceSelectAll"
                class="resource-table"
              >
                <el-table-column type="selection" width="50" />
                <template v-if="resourceTabType === 'resource_group' || resourceTabType === 'account_group'">
                  <el-table-column :label="t('resourceGrant.groupName')" min-width="160">
                    <template #default="{ row }">
                      {{ row.name }}
                    </template>
                  </el-table-column>
                  <el-table-column :label="t('resourceGrant.memberCountLabel')" width="100">
                    <template #default="{ row }">
                      {{ row.member_count || 0 }}
                    </template>
                  </el-table-column>
                  <el-table-column :label="t('resourceGrant.description')" min-width="150" show-overflow-tooltip>
                    <template #default="{ row }">
                      {{ row.description || '' }}
                    </template>
                  </el-table-column>
                </template>
                <template v-else-if="resourceTabType === 'host'">
                  <el-table-column label="主机名称" min-width="150" show-overflow-tooltip prop="name" />
                  <el-table-column :label="t('resourceGrant.hostAddress')" min-width="150" show-overflow-tooltip>
                    <template #default="{ row }">{{ `${row.address || ''}:${row.port || 22}` }}</template>
                  </el-table-column>
                  <el-table-column label="账号数量" width="100" prop="account_count" />
                </template>
                <template v-else-if="resourceTabType === 'database_instance'">
                  <el-table-column :label="t('resourceGrant.instanceName')" min-width="150" show-overflow-tooltip prop="name" />
                  <el-table-column label="协议" width="90" prop="protocol" />
                  <el-table-column :label="t('resourceGrant.hostAddress')" min-width="150" show-overflow-tooltip>
                    <template #default="{ row }">{{ `${row.address || ''}:${row.port || ''}` }}</template>
                  </el-table-column>
                </template>
                <template v-else-if="resourceTabType === 'host_account'">
                <el-table-column :label="t('resourceGrant.accountName')" min-width="120" show-overflow-tooltip>
                  <template #default="{ row }">
                    {{ row.username }}
                  </template>
                </el-table-column>
                <el-table-column :label="t('resourceGrant.hostName')" min-width="120" show-overflow-tooltip>
                  <template #default="{ row }">
                    {{ row.host_name || '' }}
                  </template>
                </el-table-column>
                <el-table-column :label="t('resourceGrant.hostAddress')" min-width="130" show-overflow-tooltip>
                  <template #default="{ row }">
                    {{ row.host_address || '' }}
                  </template>
                </el-table-column>
              </template>
              <template v-else-if="resourceTabType === 'database_account'">
                <el-table-column :label="t('resourceGrant.accountName')" min-width="120" show-overflow-tooltip>
                  <template #default="{ row }">
                    {{ row.username || row.unique_name }}
                  </template>
                </el-table-column>
                <el-table-column :label="t('resourceGrant.instanceName')" min-width="120" show-overflow-tooltip>
                  <template #default="{ row }">
                    {{ row.instance_name || '' }}
                  </template>
                </el-table-column>
                <el-table-column :label="t('resourceGrant.hostAddress')" min-width="130" show-overflow-tooltip>
                  <template #default="{ row }">
                    {{ row.instance_address || '' }}
                  </template>
                </el-table-column>
              </template>
              <template v-else-if="resourceTabType === 'application'">
                <el-table-column label="应用名称" min-width="160" show-overflow-tooltip>
                  <template #default="{ row }">{{ row.name }}</template>
                </el-table-column>
                <el-table-column label="分组" min-width="120" show-overflow-tooltip>
                  <template #default="{ row }">{{ row.group || '' }}</template>
                </el-table-column>
                <el-table-column label="代理端口" width="100">
                  <template #default="{ row }">{{ row.listen_port }}</template>
                </el-table-column>
              </template>
              </el-table>
              <div class="resource-load-status">
                <span v-if="loadingMoreResources">正在加载...</span>
                <span v-else-if="filteredResources.length > 0 && !resourceHasMore">已加载全部 {{ resourceTotal }} 条</span>
                <span v-else-if="resourceHasMore">向下滚动加载更多</span>
              </div>

              <div v-if="selectedResources.length > 0" class="selected-resource-display">
                <el-tag
                  v-for="(r, i) in selectedResources"
                  :key="r.id"
                  type="success"
                  closable
                  @close="removeResourceSelection(i)"
                >
                  {{ r.name }}
                </el-tag>
              </div>
            </div>
          </el-form-item>

          <el-form-item :label="t('resourceGrant.effect')">
            <el-radio-group v-model="grantForm.effect">
              <el-radio value="allow">{{ t('resourceGrant.allow') }}</el-radio>
              <el-radio value="deny">{{ t('resourceGrant.deny') }}</el-radio>
            </el-radio-group>
          </el-form-item>

          <el-form-item :label="t('resourceGrant.expiresAt')">
            <div class="expires-options">
              <el-radio-group v-model="expiresOption" @change="handleExpiresOptionChange">
                <el-radio value="never">{{ t('resourceGrant.never') }}</el-radio>
                <el-radio value="8h">8 {{ t('resourceGrant.hours') }}</el-radio>
                <el-radio value="7d">7 {{ t('resourceGrant.days') }}</el-radio>
                <el-radio value="1y">1 {{ t('resourceGrant.year') }}</el-radio>
                <el-radio value="custom">{{ t('resourceGrant.custom') }}</el-radio>
              </el-radio-group>
              <el-date-picker
                v-if="expiresOption === 'custom'"
                v-model="customExpiresAt"
                type="datetime"
                :placeholder="t('resourceGrant.selectDateTime')"
                style="margin-top: 8px; width: 100%"
              />
            </div>
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="grantDialogVisible = false">{{ t('common.cancel') }}</el-button>
          <el-button type="primary" @click="saveGrant" :loading="saving">{{ t('common.save') }}</el-button>
        </template>
      </el-dialog>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, nextTick, onBeforeUnmount, onMounted, watch } from 'vue'
import { useI18n } from '@/i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import DataTableCard from '@/components/DataTableCard.vue'
import {
  apiClient,
  type ResourceGrantRecord,
  type UserRecord,
  type PlatformAccountView
} from '@/api/client'

const { t } = useI18n()

// State
const loading = ref(false)
const saving = ref(false)

// Grants state
const grants = ref<ResourceGrantRecord[]>([])
const page = ref(1)
const pageSize = ref(50)
const total = ref(0)
const keyword = ref('')

// Dialogs
const grantDialogVisible = ref(false)

// Resource selection state
type ResourceRow = Record<string, unknown> & { id: string }
type SelectedResource = { id: string; name: string; type: string }

const resourceTabType = ref('host')
const resourceSearchQuery = ref('')
const loadingResources = ref(false)
const loadingMoreResources = ref(false)
const resourceRows = ref<ResourceRow[]>([])
const resourcePage = ref(1)
const resourcePageSize = 50
const resourceTotal = ref(0)
const resourceHasMore = computed(() => resourceRows.value.length < resourceTotal.value)
const hosts = ref<Array<{ id: string; name: string; address: string; port: number; account_count: number }>>([])
const databaseInstances = ref<Array<{ id: string; name: string; protocol: string; address: string; port: number }>>([])
const hostAccounts = ref<Array<{ id: string; username: string; host_name: string; host_address: string }>>([])
const dbAccounts = ref<Array<{ id: string; unique_name: string; username: string; instance_name: string; instance_address: string }>>([])
const platformAccounts = ref<PlatformAccountView[]>([])
const applications = ref<Array<{ id: string; name: string; group: string; listen_port: number }>>([])
const resourceGroups = ref<Array<{ id: string; name: string; description: string; group_type: string; member_count: number }>>([])
const accountGroups = ref<Array<{ id: string; name: string; description: string; member_count: number }>>([])
const selectedResources = ref<SelectedResource[]>([])
const selectedResourceMap = new Map<string, SelectedResource>()
const resourceTableRef = ref<any>()
let resourceRequestSequence = 0
let resourceSearchTimer: ReturnType<typeof setTimeout> | undefined
let suppressResourceSearchReload = false
let resourceScrollElement: HTMLElement | null = null

const allUsers = ref<UserRecord[]>([])
const userGroups = ref<{ id: string; name: string }[]>([])

// Grant form
const grantForm = reactive({
  principal_type: 'user' as 'user' | 'user_group',
  principal_id: '',
  resource_type: 'host',
  resource_id: '',
  resource_ids: [] as string[],
  effect: 'allow' as 'allow' | 'deny'
})
const expiresOption = ref('never')
const customExpiresAt = ref<Date | null>(null)

// Computed
const principalOptions = computed(() => {
  if (grantForm.principal_type === 'user') {
    return allUsers.value.map(u => ({ id: u.id, name: u.username || '' }))
  }
  return userGroups.value.map(g => ({ id: g.id, name: g.name }))
})

// Resource search is executed by the backend.
const filteredResources = computed(() => resourceRows.value)

// Methods
const formatTime = (time: string) => {
  if (!time) return ''
  return new Date(time).toLocaleString()
}

const resourceTypeLabel = (type: string) => {
  switch (type) {
    case 'host': return '主机'
    case 'database_instance': return '数据库'
    case 'host_account': return t('resourceGrant.hostAccounts')
    case 'database_account': return t('resourceGrant.databaseAccounts')
    case 'platform_account': return t('resourceGrant.platformAccounts')
    case 'application': return '应用'
    case 'resource_group': return t('resourceGrant.resourceGroups')
    case 'account_group': return t('resourceGrant.accountGroups')
    default: return type
  }
}

const getPrincipalName = (grant: ResourceGrantRecord) => {
  if (grant.principal_type === 'user') {
    const user = allUsers.value.find(u => u.id === grant.principal_id)
    return user?.username || grant.principal_id
  }
  const group = userGroups.value.find(g => g.id === grant.principal_id)
  return group?.name || grant.principal_id
}

const getResourceName = (grant: ResourceGrantRecord) => {
  // 先尝试查找本地缓存的资源名
  const hostContainer = hosts.value.find(item => item.id === grant.resource_id)
  if (hostContainer) return `${hostContainer.name} (${hostContainer.address}:${hostContainer.port})`
  const databaseContainer = databaseInstances.value.find(item => item.id === grant.resource_id)
  if (databaseContainer) return `${databaseContainer.name} (${databaseContainer.address}:${databaseContainer.port})`
  const host = hostAccounts.value.find(a => a.id === grant.resource_id)
  if (host) return `${host.username}@${host.host_name || host.host_address || ''}`
  const db = dbAccounts.value.find(a => a.id === grant.resource_id)
  if (db) return `${db.username || db.unique_name} (${db.instance_name || ''})`
  const platform = platformAccounts.value.find(a => a.id === grant.resource_id)
  if (platform) return `${platform.name || platform.username} (${platform.platform_name})`
  const app = applications.value.find(a => a.id === grant.resource_id)
  if (app) return app.name
  const group = resourceGroups.value.find(g => g.id === grant.resource_id)
  if (group) return group.name
  const accGroup = accountGroups.value.find(g => g.id === grant.resource_id)
  if (accGroup) return accGroup.name
  return grant.resource_id
}

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

const ensureNamesLoaded = async () => {
  // 预加载所有关联数据用于表格中的名称显示
  const needHostContainer = grants.value.some(g => g.resource_type === 'host')
  const needDatabaseContainer = grants.value.some(g => g.resource_type === 'database_instance')
  const needHost = grants.value.some(g => g.resource_type === 'host_account')
  const needDb = grants.value.some(g => g.resource_type === 'database_account')
  const needPlatform = grants.value.some(g => g.resource_type === 'platform_account')
  const needApplication = grants.value.some(g => g.resource_type === 'application')
  const needResGroup = grants.value.some(g => g.resource_type === 'resource_group')
  const needAccGroup = grants.value.some(g => g.resource_type === 'account_group')

  if (allUsers.value.length === 0) await loadUsers()
  if (userGroups.value.length === 0) await loadUserGroups()
  if (needHostContainer && hosts.value.length === 0) await loadHosts()
  if (needDatabaseContainer && databaseInstances.value.length === 0) await loadDatabaseInstances()
  if (needHost && hostAccounts.value.length === 0) await loadHostAccounts()
  if (needDb && dbAccounts.value.length === 0) await loadDbAccounts()
  if (needPlatform && platformAccounts.value.length === 0) await loadPlatformAccounts()
  if (needApplication && applications.value.length === 0) await loadApplications()
  if (needResGroup && resourceGroups.value.length === 0) await loadResourceGroups()
  if (needAccGroup && accountGroups.value.length === 0) await loadAccountGroups()
}

const loadUsers = async () => {
  try {
    const resp = await apiClient.getUsers({ page: 1, page_size: 1000 })
    allUsers.value = resp.items || []
  } catch {
    allUsers.value = []
  }
}

const loadUserGroups = async () => {
  try {
    const gs = await apiClient.getUserGroups()
    userGroups.value = gs?.items || []
  } catch {
    userGroups.value = []
  }
}

const loadResourceGroups = async () => {
  try {
    const all = await apiClient.getResourceGroups()
    const list = (all.items || []).filter(g => g.group_type !== 'account')
    resourceGroups.value = list.map(g => ({
      id: g.id,
      name: g.name,
      description: g.description || '',
      group_type: g.group_type || 'resource',
      member_count: (g.host_count || 0) + (g.database_count || 0)
    }))
  } catch {
    resourceGroups.value = []
  }
}

const loadAccountGroups = async () => {
  try {
    const all = await apiClient.getResourceGroups()
    const list = (all.items || []).filter(g => g.group_type === 'account')
    accountGroups.value = list.map(g => ({
      id: g.id,
      name: g.name,
      description: g.description || '',
      member_count: g.account_count || 0
    }))
  } catch {
    accountGroups.value = []
  }
}

const loadHosts = async () => {
  const resp = await apiClient.getHosts({ page: 1, page_size: 1000 })
  hosts.value = (resp.items || []).map(host => ({
    id: String(host.id ?? ''),
    name: host.name || '',
    address: host.address || '',
    port: Number(host.port) || 22,
    account_count: Number(host.account_count) || 0
  }))
}

const loadDatabaseInstances = async () => {
  const resp = await apiClient.getDBInstances({ page: 1, page_size: 1000 })
  databaseInstances.value = (resp.items || []).map(instance => ({
    id: String(instance.id ?? ''),
    name: instance.name || '',
    protocol: instance.protocol || '',
    address: instance.address || '',
    port: Number(instance.port) || 0
  }))
}

const loadHostAccounts = async () => {
  const resp = await apiClient.getTargets({ page: 1, page_size: 1000 })
  hostAccounts.value = (resp.items || []).map((t: any) => ({
    id: String(t.id ?? ''),
    username: t.username || '',
    host_name: t.name || t.host || '',
    host_address: `${t.host || ''}:${t.port || ''}`
  }))
}

const loadDbAccounts = async () => {
  const instances = await apiClient.getDBInstances({ page: 1, page_size: 100 })
  const allAccounts: Array<{ id: string; unique_name: string; username: string; instance_name: string; instance_address: string }> = []
  for (const inst of (instances.items || [])) {
    if (!inst.id) continue
    try {
      const resp = await apiClient.getDBAccounts(inst.id, { page: 1, page_size: 1000 })
      for (const a of (resp.items || [])) {
        if (a.id) {
          allAccounts.push({
            id: a.id,
            unique_name: a.unique_name || '',
            username: a.username || '',
            instance_name: inst.name || '',
            instance_address: [inst.address, inst.port].filter(Boolean).join(':') || ''
          })
        }
      }
    } catch { /* ignore */ }
  }
  dbAccounts.value = allAccounts
}

const loadPlatformAccounts = async () => {
  try {
    const response = await apiClient.getPlatformAccounts({ page: 1, page_size: 200 })
    platformAccounts.value = response.items || []
  } catch {
    platformAccounts.value = []
  }
}

const loadApplications = async () => {
  const first = await apiClient.getApplications({ page: 1, page_size: 200 })
  applications.value = (first.items || []).map(app => ({
    id: String(app.id ?? ''),
    name: app.name || '',
    group: app.group || '',
    listen_port: Number(app.listen_port) || 0
  }))
}

const resetResourceSelection = () => {
  selectedResourceMap.clear()
  selectedResources.value = []
  grantForm.resource_ids = []
}

const showGrantDialog = () => {
  grantForm.principal_type = 'user'
  grantForm.principal_id = ''
  grantForm.resource_type = 'host'
  grantForm.resource_id = ''
  grantForm.resource_ids = []
  grantForm.effect = 'allow'
  expiresOption.value = 'never'
  customExpiresAt.value = null
  resetResourceSelection()
  resourceTabType.value = 'host'
  resourceSearchQuery.value = ''
  grantDialogVisible.value = true
  void loadResources(true)
}

const handleResourceTabChange = () => {
  if (resourceSearchTimer) clearTimeout(resourceSearchTimer)
  suppressResourceSearchReload = true
  resourceSearchQuery.value = ''
  suppressResourceSearchReload = false
  resetResourceSelection()
  grantForm.resource_type = resourceTabType.value
  void loadResources(true)
}

const mapResourceRows = (items: any[]): ResourceRow[] => {
  if (resourceTabType.value === 'host') {
    return items.map(host => ({ id: String(host.id ?? ''), name: host.name || '', address: host.address || '', port: Number(host.port) || 22, account_count: Number(host.account_count) || 0 }))
  }
  if (resourceTabType.value === 'database_instance') {
    return items.map(instance => ({ id: String(instance.id ?? ''), name: instance.name || '', protocol: instance.protocol || '', address: instance.address || '', port: Number(instance.port) || 0 }))
  }
  if (resourceTabType.value === 'host_account') {
    return items.map(target => ({ id: String(target.id ?? ''), username: target.username || '', host_name: target.name || target.host || '', host_address: `${target.host || ''}:${target.port || ''}` }))
  }
  if (resourceTabType.value === 'database_account') {
    return items.map(account => ({ id: String(account.id ?? ''), unique_name: account.unique_name || '', username: account.username || '', instance_name: account.instance_name || '', instance_address: account.instance_address || '' }))
  }
  if (resourceTabType.value === 'platform_account') {
    return items.map(account => ({ id: String(account.id ?? ''), name: account.name || '', platform_name: account.platform_name || '', username: account.username || '', url: account.url || '', group: account.group || '' }))
  }
  if (resourceTabType.value === 'application') {
    return items.map(app => ({ id: String(app.id ?? ''), name: app.name || '', group: app.group || '', listen_port: Number(app.listen_port) || 0 }))
  }
  if (resourceTabType.value === 'resource_group') {
    return items.map(group => ({ id: String(group.id ?? ''), name: group.name || '', description: group.description || '', group_type: group.group_type || 'resource', member_count: Number(group.host_count || 0) + Number(group.database_count || 0) }))
  }
  return items.map(group => ({ id: String(group.id ?? ''), name: group.name || '', description: group.description || '', member_count: Number(group.account_count) || 0 }))
}

const fetchResourcePage = async (pageNumber: number) => {
  const params = { page: pageNumber, page_size: resourcePageSize, q: resourceSearchQuery.value.trim() || undefined }
  if (resourceTabType.value === 'host') return apiClient.getHosts(params)
  if (resourceTabType.value === 'database_instance') return apiClient.getDBInstances(params)
  if (resourceTabType.value === 'host_account') return apiClient.getTargets(params)
  if (resourceTabType.value === 'database_account') return apiClient.getAllDBAccounts(params)
  if (resourceTabType.value === 'platform_account') return apiClient.getPlatformAccounts(params)
  if (resourceTabType.value === 'application') return apiClient.getApplications(params)
  return apiClient.getResourceGroups({ ...params, group_type: resourceTabType.value === 'account_group' ? 'account' : 'resource' })
}

const restoreResourceSelections = async () => {
  await nextTick()
  for (const row of resourceRows.value) {
    resourceTableRef.value?.toggleRowSelection(row, selectedResourceMap.has(row.id))
  }
}

const bindResourceScroll = async () => {
  await nextTick()
  const tableRoot = resourceTableRef.value?.$el as HTMLElement | undefined
  const nextElement = tableRoot?.querySelector<HTMLElement>('.el-scrollbar__wrap') || null
  if (nextElement === resourceScrollElement) return
  resourceScrollElement?.removeEventListener('scroll', handleResourceScroll)
  resourceScrollElement = nextElement
  resourceScrollElement?.addEventListener('scroll', handleResourceScroll, { passive: true })
}

const loadResources = async (reset = false) => {
  if (!reset && (!resourceHasMore.value || loadingResources.value || loadingMoreResources.value)) return
  const nextPage = reset ? 1 : resourcePage.value + 1
  const requestSequence = ++resourceRequestSequence
  if (reset) loadingResources.value = true
  else loadingMoreResources.value = true
  try {
    const response = await fetchResourcePage(nextPage)
    if (requestSequence !== resourceRequestSequence) return
    const nextRows = mapResourceRows(response.items || []).filter(row => row.id)
    if (reset) {
      resourceRows.value = nextRows
    } else {
      const existingIds = new Set(resourceRows.value.map(row => row.id))
      resourceRows.value = [...resourceRows.value, ...nextRows.filter(row => !existingIds.has(row.id))]
    }
    resourcePage.value = nextPage
    resourceTotal.value = Number(response.total) || 0
    await restoreResourceSelections()
    await bindResourceScroll()
  } catch (error) {
    if (requestSequence === resourceRequestSequence) {
      console.error('Failed to load resources:', error)
      ElMessage.error('资源加载失败')
    }
  } finally {
    if (requestSequence === resourceRequestSequence) {
      loadingResources.value = false
      loadingMoreResources.value = false
    }
  }
}

const handleResourceScroll = (event: Event) => {
  const target = event.currentTarget as HTMLElement
  if (target.scrollHeight - target.scrollTop - target.clientHeight <= 32) {
    void loadResources(false)
  }
}

const resourceSelectionName = (row: any) => {
  if (row.username) return `${row.username}@${row.host_name || row.instance_name || row.host_address || ''}`
  if (row.name) return `${row.name}${row.address ? ` (${row.address}:${row.port || ''})` : ''}`
  return row.unique_name || row.id
}

const syncSelectedResources = () => {
  selectedResources.value = Array.from(selectedResourceMap.values())
  grantForm.resource_ids = selectedResources.value.map(resource => resource.id)
  grantForm.resource_type = resourceTabType.value
}

const handleResourceSelect = (selection: ResourceRow[], row: ResourceRow) => {
  if (selection.some(item => item.id === row.id)) {
    selectedResourceMap.set(row.id, { id: row.id, name: resourceSelectionName(row), type: resourceTabType.value })
  } else {
    selectedResourceMap.delete(row.id)
  }
  syncSelectedResources()
}

const handleResourceSelectAll = (selection: ResourceRow[]) => {
  const selectedIds = new Set(selection.map(row => row.id))
  for (const row of resourceRows.value) {
    if (selectedIds.has(row.id)) {
      selectedResourceMap.set(row.id, { id: row.id, name: resourceSelectionName(row), type: resourceTabType.value })
    } else {
      selectedResourceMap.delete(row.id)
    }
  }
  syncSelectedResources()
}

const removeResourceSelection = (index: number) => {
  const resource = selectedResources.value[index]
  if (!resource) return
  selectedResourceMap.delete(resource.id)
  const visibleRow = resourceRows.value.find(row => row.id === resource.id)
  if (visibleRow) resourceTableRef.value?.toggleRowSelection(visibleRow, false)
  syncSelectedResources()
}

const handleExpiresOptionChange = (val: string) => {
  if (val !== 'custom') {
    customExpiresAt.value = null
  }
}

const saveGrant = async () => {
  const resourceIds = grantForm.resource_ids && grantForm.resource_ids.length > 0
    ? grantForm.resource_ids
    : grantForm.resource_id ? [grantForm.resource_id] : []

  if (!grantForm.principal_id || resourceIds.length === 0) {
    ElMessage.warning(t('resourceGrant.fillRequired'))
    return
  }

  let expiresAt: string | undefined
  if (expiresOption.value === '8h') {
    expiresAt = new Date(Date.now() + 8 * 3600 * 1000).toISOString()
  } else if (expiresOption.value === '7d') {
    expiresAt = new Date(Date.now() + 7 * 86400 * 1000).toISOString()
  } else if (expiresOption.value === '1y') {
    expiresAt = new Date(Date.now() + 365 * 86400 * 1000).toISOString()
  } else if (expiresOption.value === 'custom' && customExpiresAt.value) {
    expiresAt = customExpiresAt.value.toISOString()
  }

  saving.value = true
  try {
    for (const rid of resourceIds) {
      await apiClient.createResourceGrant({
        principal_type: grantForm.principal_type,
        principal_id: grantForm.principal_id,
        resource_type: grantForm.resource_type,
        resource_id: rid,
        effect: grantForm.effect,
        expires_at: expiresAt
      })
    }
    ElMessage.success(t('common.created'))
    grantDialogVisible.value = false
    await loadGrants()
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to create grant')
  } finally {
    saving.value = false
  }
}

const searching = ref(false)

const onSearch = (q: string) => {
  keyword.value = q
  searching.value = true
  page.value = 1
  loadGrants()
}

const deleteGrant = async (grant: ResourceGrantRecord) => {
  try {
    await ElMessageBox.confirm(
      t('resourceGrant.confirmDeleteGrant'),
      t('common.delete'),
      { confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel'), type: 'warning' }
    )
    await apiClient.deleteResourceGrant(grant.id)
    ElMessage.success(t('common.deleted'))
    await loadGrants()
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error(e.message || 'Failed to delete grant')
    }
  }
}

// Watch principal type change to reset selection
// 分页变化时重新加载（搜索时跳过，避免 onSearch 中已调用 loadGrants 导致双重加载）
watch([page, pageSize], () => {
  if (searching.value) { searching.value = false; return }
  loadGrants()
})

watch(resourceSearchQuery, () => {
  if (!grantDialogVisible.value || suppressResourceSearchReload) return
  if (resourceSearchTimer) clearTimeout(resourceSearchTimer)
  resourceSearchTimer = setTimeout(() => {
    void loadResources(true)
  }, 300)
}, { flush: 'sync' })

watch(() => grantForm.principal_type, () => {
  grantForm.principal_id = ''
})

onBeforeUnmount(() => {
  if (resourceSearchTimer) clearTimeout(resourceSearchTimer)
  resourceScrollElement?.removeEventListener('scroll', handleResourceScroll)
})

// Init
onMounted(async () => {
  await loadUsers()
  await loadUserGroups()
  await loadGrants()
})
</script>

<style scoped>
.resource-select-inline {
  width: 100%;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
  padding: 10px;
}

.resource-tabs {
  margin-bottom: 8px;
}

.resource-search {
  margin-bottom: 8px;
}

.resource-table {
  width: 100%;
}

.resource-load-status {
  min-height: 18px;
  padding-top: 4px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  text-align: center;
}

.selected-resource-display {
  max-height: 68px;
  margin-top: 8px;
  padding-top: 8px;
  overflow-y: auto;
  border-top: 1px solid var(--el-border-color-lighter);
}

.expires-options {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

:deep(.resource-grant-dialog) {
  margin-bottom: 4vh;
}

:deep(.resource-grant-dialog .el-dialog__body) {
  padding: 12px 20px 4px;
}

:deep(.resource-grant-dialog .el-form-item) {
  margin-bottom: 12px;
}

:deep(.resource-grant-dialog .el-dialog__footer) {
  padding-top: 12px;
}

</style>
