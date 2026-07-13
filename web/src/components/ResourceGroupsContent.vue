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

      <!-- 创建/编辑对话框 -->
      <el-dialog
        v-model="dialogVisible"
        :title="editingGroup ? t('resourceGroups.edit') : t('resourceGroups.create')"
        width="500px"
      >
        <el-form :model="form" label-width="100px">
          <el-form-item :label="t('resourceGroups.name')" required>
            <el-input v-model="form.name" :placeholder="t('resourceGroups.namePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('resourceGroups.description')">
            <el-input v-model="form.description" type="textarea" :rows="3" />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="dialogVisible = false">{{ t('common.cancel') }}</el-button>
          <el-button type="primary" @click="saveGroup" :loading="saving">{{ t('common.save') }}</el-button>
        </template>
      </el-dialog>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch, onMounted } from 'vue'
import { useI18n } from '@/i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { apiClient, type ResourceGroupRecord } from '@/api/client'
import DataTableCard from '@/components/DataTableCard.vue'

const { t } = useI18n()

const groups = ref<ResourceGroupRecord[]>([])
const loading = ref(false)
const saving = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const keyword = ref('')

const dialogVisible = ref(false)
const editingGroup = ref<ResourceGroupRecord | null>(null)
const form = reactive({
  name: '',
  description: ''
})

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

const onSearch = (q: string) => {
  keyword.value = q
  page.value = 1
  loadGroups()
}

const showCreateDialog = () => {
  editingGroup.value = null
  form.name = ''
  form.description = ''
  dialogVisible.value = true
}

const showEditDialog = (group: ResourceGroupRecord) => {
  editingGroup.value = group
  form.name = group.name
  form.description = group.description || ''
  dialogVisible.value = true
}

const saveGroup = async () => {
  if (!form.name) {
    ElMessage.warning(t('resourceGroups.nameRequired'))
    return
  }
  saving.value = true
  try {
    if (editingGroup.value) {
      await apiClient.updateResourceGroup(editingGroup.value.id, {
        name: form.name,
        description: form.description || undefined,
      })
    } else {
      await apiClient.createResourceGroup({
        name: form.name,
        group_type: 'resource',
        description: form.description || undefined,
      })
    }
    ElMessage.success(t('common.saved'))
    dialogVisible.value = false
    await loadGroups()
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to save group')
  } finally {
    saving.value = false
  }
}

const deleteGroup = async (group: ResourceGroupRecord) => {
  try {
    await ElMessageBox.confirm(
      t('resourceGroups.confirmDelete').replace('{name}', group.name),
      t('common.delete'),
      { confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel'), type: 'warning' }
    )
    await apiClient.deleteResourceGroup(group.id)
    ElMessage.success(t('common.deleted'))
    await loadGroups()
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error(e.message || 'Failed to delete group')
    }
  }
}

watch([page, pageSize], () => loadGroups())

onMounted(() => {
  loadGroups()
})
</script>
