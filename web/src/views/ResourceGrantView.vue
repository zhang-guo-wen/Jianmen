<template>
  <div class="view-stack">
    <div class="page-container">
      <div class="page-card">
        <div class="page-card__toolbar">
          <div class="page-card__actions" style="flex:1; justify-content:space-between;">
            <div class="filters">
              <el-select v-model="filters.principal_type" :placeholder="t('resourceGrant.principalType')" clearable style="width: 140px">
                <el-option :label="t('resourceGrant.user')" value="user" />
                <el-option :label="t('resourceGrant.userGroup')" value="user_group" />
              </el-select>
              <el-select v-model="filters.resource_type" :placeholder="t('resourceGrant.resourceType')" clearable style="width: 140px">
                <el-option label="Host Account" value="host_account" />
                <el-option label="Database Account" value="database_account" />
                <el-option label="Resource Group" value="resource_group" />
              </el-select>
              <el-button type="primary" @click="loadGrants">
                <el-icon><Search /></el-icon>
                {{ t('common.search') }}
              </el-button>
            </div>
            <div>
              <el-button type="primary" @click="showGrantDialog()">
                <el-icon><Plus /></el-icon>
                {{ t('resourceGrant.addGrant') }}
              </el-button>
            </div>
          </div>
        </div>
        <div class="page-card__body">
          <el-table :data="grants" v-loading="loading" stripe>
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
          </el-table>
        </div>
      </div>

      <!-- 创建资源授权对话框 -->
      <el-dialog
        v-model="grantDialogVisible"
        :title="t('resourceGrant.addGrant')"
        width="800px"
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
                <el-tab-pane :label="t('resourceGrant.hostAccounts')" name="host_account" />
                <el-tab-pane :label="t('resourceGrant.databaseAccounts')" name="database_account" />
                <el-tab-pane :label="t('resourceGrant.resourceGroups')" name="resource_group" />
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
                height="250"
                stripe
                @selection-change="handleResourceSelectionChange"
                class="resource-table"
              >
                <el-table-column type="selection" width="50" />
                <el-table-column :label="t('resourceGrant.accountName')" min-width="130">
                  <template #default="{ row }">
                    {{ row.username || row.unique_name || row.name }}
                  </template>
                </el-table-column>
                <el-table-column :label="t('resourceGrant.hostName')" min-width="130">
                  <template #default="{ row }">
                    {{ row.host_name || row.instance_name || '' }}
                  </template>
                </el-table-column>
                <el-table-column :label="t('resourceGrant.hostAddress')" min-width="130">
                  <template #default="{ row }">
                    {{ row.host_address || '' }}
                  </template>
                </el-table-column>
              </el-table>

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
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useI18n } from '@/i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import {
  apiClient,
  type ResourceGrantRecord,
  type UserRecord
} from '@/api/client'

const { t } = useI18n()

// State
const loading = ref(false)
const saving = ref(false)

// Grants state
const grants = ref<ResourceGrantRecord[]>([])
const filters = reactive({
  principal_type: '',
  resource_type: ''
})

// Dialogs
const grantDialogVisible = ref(false)

// Resource selection state
const resourceTabType = ref('host_account')
const resourceSearchQuery = ref('')
const loadingResources = ref(false)
const hostAccounts = ref<Array<{ id: string; username: string; host_name: string; host_address: string }>>([])
const dbAccounts = ref<Array<{ id: string; unique_name: string; instance_name: string }>>([])
const resourceGroups = ref<Array<{ id: string; name: string; description: string }>>([])
const selectedResources = ref<Array<{ id: string; name: string; type: string }>>([])

const allUsers = ref<UserRecord[]>([])
const userGroups = ref<{ id: string; name: string }[]>([])

// Grant form
const grantForm = reactive({
  principal_type: 'user' as 'user' | 'user_group',
  principal_id: '',
  resource_type: 'host_account',
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

// Filtered resources based on tab type and search query
const filteredResources = computed(() => {
  const query = resourceSearchQuery.value.toLowerCase()
  if (resourceTabType.value === 'host_account') {
    if (!query) return hostAccounts.value
    return hostAccounts.value.filter(a =>
      (a.username || '').toLowerCase().includes(query) ||
      (a.host_name || '').toLowerCase().includes(query) ||
      (a.host_address || '').toLowerCase().includes(query)
    )
  } else if (resourceTabType.value === 'database_account') {
    if (!query) return dbAccounts.value
    return dbAccounts.value.filter(a =>
      (a.unique_name || '').toLowerCase().includes(query) ||
      (a.instance_name || '').toLowerCase().includes(query)
    )
  } else {
    if (!query) return resourceGroups.value
    return resourceGroups.value.filter(g =>
      (g.name || '').toLowerCase().includes(query) ||
      (g.description || '').toLowerCase().includes(query)
    )
  }
})

// Methods
const formatTime = (time: string) => {
  if (!time) return ''
  return new Date(time).toLocaleString()
}

const resourceTypeLabel = (type: string) => {
  switch (type) {
    case 'host_account': return t('resourceGrant.hostAccounts')
    case 'database_account': return t('resourceGrant.databaseAccounts')
    case 'resource_group': return t('resourceGrant.resourceGroups')
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
  const host = hostAccounts.value.find(a => a.id === grant.resource_id)
  if (host) return `${host.username}@${host.host_name || host.host_address || ''}`
  const db = dbAccounts.value.find(a => a.id === grant.resource_id)
  if (db) return `${db.unique_name} (${db.instance_name || ''})`
  const group = resourceGroups.value.find(g => g.id === grant.resource_id)
  if (group) return group.name
  return grant.resource_id
}

const loadGrants = async () => {
  loading.value = true
  try {
    const params: Record<string, string> = {}
    if (filters.principal_type) params.principal_type = filters.principal_type
    if (filters.resource_type) params.resource_type = filters.resource_type
    grants.value = await apiClient.getResourceGrants(params)
    // 加载时预拉资源数据用于名称显示
    await ensureResourcesLoaded()
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load grants')
  } finally {
    loading.value = false
  }
}

const ensureResourcesLoaded = async () => {
  // 根据 grants 中的 resource_type 按需加载对应资源缓存
  const needHost = grants.value.some(g => g.resource_type === 'host_account')
  const needDb = grants.value.some(g => g.resource_type === 'database_account')
  if (needHost && hostAccounts.value.length === 0) await loadHostAccounts()
  if (needDb && dbAccounts.value.length === 0) await loadDbAccounts()
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
    userGroups.value = gs || []
  } catch {
    userGroups.value = []
  }
}

const showGrantDialog = () => {
  grantForm.principal_type = 'user'
  grantForm.principal_id = ''
  grantForm.resource_type = 'host_account'
  grantForm.resource_id = ''
  grantForm.resource_ids = []
  grantForm.effect = 'allow'
  expiresOption.value = 'never'
  customExpiresAt.value = null
  selectedResources.value = []
  resourceTabType.value = 'host_account'
  resourceSearchQuery.value = ''
  grantDialogVisible.value = true
  loadResources()
}

const handleResourceTabChange = () => {
  resourceSearchQuery.value = ''
  loadResources()
}

const loadResources = async () => {
  loadingResources.value = true
  try {
    if (resourceTabType.value === 'host_account') {
      await loadHostAccounts()
    } else if (resourceTabType.value === 'database_account') {
      await loadDbAccounts()
    } else if (resourceTabType.value === 'resource_group') {
      resourceGroups.value = []
    }
  } catch (e) {
    console.error('Failed to load resources:', e)
  } finally {
    loadingResources.value = false
  }
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
  const allAccounts: Array<{ id: string; unique_name: string; instance_name: string }> = []
  for (const inst of (instances.items || [])) {
    if (!inst.id) continue
    try {
      const resp = await apiClient.getDBAccounts(inst.id, { page: 1, page_size: 1000 })
      for (const a of (resp.items || [])) {
        if (a.id) {
          allAccounts.push({
            id: a.id,
            unique_name: a.unique_name || a.username || '',
            instance_name: inst.name || ''
          })
        }
      }
    } catch { /* ignore */ }
  }
  dbAccounts.value = allAccounts
}

const handleResourceSelectionChange = (rows: any[]) => {
  selectedResources.value = rows.map(row => {
    const name = row.username ? `${row.username}@${row.host_name || row.host_address || ''}` :
                 row.unique_name ? `${row.unique_name} (${row.instance_name || ''})` :
                 row.name || ''
    return { id: row.id, name, type: resourceTabType.value }
  })
  grantForm.resource_ids = selectedResources.value.map(r => r.id)
  grantForm.resource_type = resourceTabType.value
}

const removeResourceSelection = (index: number) => {
  selectedResources.value.splice(index, 1)
  grantForm.resource_ids = selectedResources.value.map(r => r.id)
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
watch(() => grantForm.principal_type, () => {
  grantForm.principal_id = ''
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
  padding: 12px;
}

.resource-tabs {
  margin-bottom: 12px;
}

.resource-search {
  margin-bottom: 12px;
}

.resource-table {
  width: 100%;
}

.selected-resource-display {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.filters {
  display: flex;
  gap: 12px;
  align-items: center;
}

.expires-options {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
