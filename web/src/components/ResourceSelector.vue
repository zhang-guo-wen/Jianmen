<template>
  <el-dialog
    v-model="visible"
    :title="t('resourceGrant.selectResource')"
    width="700px"
    :close-on-click-modal="false"
  >
    <div class="resource-selector">
      <!-- 资源类型切换 -->
      <el-tabs v-model="activeType" @tab-change="handleTypeChange">
        <el-tab-pane :label="t('resourceGrant.hostAccounts')" name="host_account">
          <div class="resource-list">
            <el-input
              v-model="searchQuery"
              :placeholder="t('resourceGrant.searchResource')"
              clearable
              class="search-input"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <el-table
              :data="filteredHostAccounts"
              v-loading="loading"
              height="350"
              stripe
              @selection-change="handleSelectionChange"
              ref="tableRef"
            >
              <el-table-column type="selection" width="50" />
              <el-table-column :label="t('resourceGrant.accountName')" prop="username" min-width="120" />
              <el-table-column :label="t('resourceGrant.hostName')" prop="host_name" min-width="150" />
              <el-table-column :label="t('resourceGrant.hostAddress')" prop="host_address" min-width="150" />
            </el-table>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="t('resourceGrant.databaseAccounts')" name="database_account">
          <div class="resource-list">
            <el-input
              v-model="searchQuery"
              :placeholder="t('resourceGrant.searchResource')"
              clearable
              class="search-input"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <el-table
              :data="filteredDBAccounts"
              v-loading="loading"
              height="350"
              stripe
              @selection-change="handleSelectionChange"
            >
              <el-table-column type="selection" width="50" />
              <el-table-column :label="t('resourceGrant.accountName')" prop="unique_name" min-width="150" />
              <el-table-column :label="t('resourceGrant.instanceName')" prop="instance_name" min-width="150" />
            </el-table>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="t('resourceGrant.resourceGroups')" name="resource_group">
          <div class="resource-list">
            <el-input
              v-model="searchQuery"
              :placeholder="t('resourceGrant.searchResource')"
              clearable
              class="search-input"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <el-table
              :data="filteredResourceGroups"
              v-loading="loading"
              height="350"
              stripe
              @selection-change="handleSelectionChange"
            >
              <el-table-column type="selection" width="50" />
              <el-table-column :label="t('resourceGrant.groupName')" prop="name" min-width="150" />
              <el-table-column :label="t('resourceGrant.description')" prop="description" min-width="200" />
            </el-table>
          </div>
        </el-tab-pane>
      </el-tabs>

      <!-- 已选资源 -->
      <div class="selected-resources" v-if="selectedResources.length > 0">
        <div class="selected-header">
          <span>{{ t('resourceGrant.selectedResources') }} ({{ selectedResources.length }})</span>
          <el-button link type="primary" size="small" @click="clearSelection">
            {{ t('resourceGrant.clearSelection') }}
          </el-button>
        </div>
        <div class="selected-tags">
          <el-tag
            v-for="resource in selectedResources"
            :key="resource.id"
            closable
            @close="removeResource(resource)"
            class="resource-tag"
          >
            {{ resource.name }}
          </el-tag>
        </div>
      </div>
    </div>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" @click="confirmSelection" :disabled="selectedResources.length === 0">
        {{ t('common.confirm') }} ({{ selectedResources.length }})
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from '@/i18n'
import { Search } from '@element-plus/icons-vue'
import { apiClient } from '@/api/client'

interface ResourceItem {
  id: string
  name: string
  type: string
}

const { t } = useI18n()

const visible = defineModel<boolean>({ default: false })
const props = defineProps<{
  resourceType?: string
  multiple?: boolean
}>()

const emit = defineEmits<{
  confirm: [resources: ResourceItem[]]
}>()

// State
const activeType = ref('host_account')
const searchQuery = ref('')
const loading = ref(false)
const selectedResources = ref<ResourceItem[]>([])

// Data
const hostAccounts = ref<Array<{ id: string; username: string; host_name: string; host_address: string }>>([])
const dbAccounts = ref<Array<{ id: string; unique_name: string; instance_name: string }>>([])
const resourceGroups = ref<Array<{ id: string; name: string; description: string }>>([])

// Computed
const filteredHostAccounts = computed(() => {
  if (!searchQuery.value) return hostAccounts.value
  const query = searchQuery.value.toLowerCase()
  return hostAccounts.value.filter(a =>
    (a.username || '').toLowerCase().includes(query) ||
    (a.host_name || '').toLowerCase().includes(query) ||
    (a.host_address || '').toLowerCase().includes(query)
  )
})

const filteredDBAccounts = computed(() => {
  if (!searchQuery.value) return dbAccounts.value
  const query = searchQuery.value.toLowerCase()
  return dbAccounts.value.filter(a =>
    (a.unique_name || '').toLowerCase().includes(query) ||
    (a.instance_name || '').toLowerCase().includes(query)
  )
})

const filteredResourceGroups = computed(() => {
  if (!searchQuery.value) return resourceGroups.value
  const query = searchQuery.value.toLowerCase()
  return resourceGroups.value.filter(g =>
    (g.name || '').toLowerCase().includes(query) ||
    (g.description || '').toLowerCase().includes(query)
  )
})

// Methods
const loadHostAccounts = async () => {
  loading.value = true
  try {
    const resp = await apiClient.getTargets({ page: 1, page_size: 1000 })
    hostAccounts.value = (resp.items || []).map((t: any) => ({
      id: t.id,
      username: t.username || '',
      host_name: t.host_name || t.host?.name || '',
      host_address: t.host_address || t.host?.address || ''
    }))
  } catch (e) {
    console.error('Failed to load host accounts:', e)
  } finally {
    loading.value = false
  }
}

const loadDBAccounts = async () => {
  loading.value = true
  try {
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
  } catch (e) {
    console.error('Failed to load DB accounts:', e)
  } finally {
    loading.value = false
  }
}

const loadResourceGroups = async () => {
  loading.value = true
  try {
    // TODO: 实现资源组列表 API
    resourceGroups.value = []
  } catch (e) {
    console.error('Failed to load resource groups:', e)
  } finally {
    loading.value = false
  }
}

const handleTypeChange = () => {
  searchQuery.value = ''
  selectedResources.value = []
  loadData()
}

const loadData = () => {
  if (activeType.value === 'host_account') {
    loadHostAccounts()
  } else if (activeType.value === 'database_account') {
    loadDBAccounts()
  } else if (activeType.value === 'resource_group') {
    loadResourceGroups()
  }
}

const handleSelectionChange = (selection: any[]) => {
  const newResources = selection.map(item => ({
    id: item.id,
    name: item.username || item.unique_name || item.name || '',
    type: activeType.value
  }))
  // 合并不同类型的选中资源
  const otherTypeResources = selectedResources.value.filter(r => r.type !== activeType.value)
  selectedResources.value = [...otherTypeResources, ...newResources]
}

const removeResource = (resource: ResourceItem) => {
  selectedResources.value = selectedResources.value.filter(r => r.id !== resource.id)
}

const clearSelection = () => {
  selectedResources.value = []
}

const confirmSelection = () => {
  emit('confirm', selectedResources.value)
  visible.value = false
  selectedResources.value = []
}

// Watch for dialog open
watch(visible, (val) => {
  if (val) {
    if (props.resourceType) {
      activeType.value = props.resourceType
    }
    loadData()
  }
})

// Init
onMounted(() => {
  if (visible.value) {
    loadData()
  }
})
</script>

<style scoped>
.resource-selector {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.resource-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.search-input {
  width: 100%;
}

.selected-resources {
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 12px;
}

.selected-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
  font-size: 14px;
  color: var(--el-text-color-regular);
}

.selected-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.resource-tag {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
