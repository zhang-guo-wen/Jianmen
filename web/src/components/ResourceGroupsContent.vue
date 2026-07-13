<template>
  <div class="view-stack">
    <div class="page-container">
      <div class="page-card">
        <div class="page-card__toolbar">
          <div class="page-card__actions" style="display:flex;justify-content:space-between;width:100%">
            <el-radio-group v-model="filterType" @change="loadGroups">
              <el-radio-button value="resource">{{ t('resourceGroups.resourceTypeTab') }}</el-radio-button>
              <el-radio-button value="account">{{ t('resourceGroups.accountTypeTab') }}</el-radio-button>
            </el-radio-group>
            <el-button type="primary" @click="showCreateDialog">
              <el-icon><Plus /></el-icon>
              {{ t('resourceGroups.create') }}
            </el-button>
          </div>
        </div>
        <div class="page-card__body">
          <el-table :data="groups" v-loading="loading" stripe>
            <el-table-column :label="t('resourceGroups.name')" prop="name" min-width="150" />
            <el-table-column :label="t('resourceGroups.type')" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="row.group_type === 'account' ? 'warning' : 'primary'">
                  {{ row.group_type === 'account' ? t('resourceGroups.typeAccount') : t('resourceGroups.typeResource') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('resourceGroups.description')" prop="description" min-width="200" show-overflow-tooltip />
            <el-table-column v-if="filterType === 'resource'" :label="t('resourceGroups.hostCount')" width="80">
              <template #default="{ row }">
                {{ row.host_count || 0 }}
              </template>
            </el-table-column>
            <el-table-column v-if="filterType === 'resource'" :label="t('resourceGroups.databaseCount')" width="110">
              <template #default="{ row }">
                {{ row.database_count || 0 }}
              </template>
            </el-table-column>
            <el-table-column v-if="filterType === 'account'" :label="t('resourceGroups.accountCount')" width="80">
              <template #default="{ row }">
                {{ row.account_count || 0 }}
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
          </el-table>
        </div>
      </div>

      <el-dialog
        v-model="dialogVisible"
        :title="editingGroup ? t('resourceGroups.edit') : t('resourceGroups.create')"
        width="500px"
      >
        <el-form :model="form" label-width="100px">
          <el-form-item :label="t('resourceGroups.type')" required v-if="!editingGroup">
            <el-radio-group v-model="form.group_type">
              <el-radio value="resource">{{ t('resourceGroups.typeResource') }}</el-radio>
              <el-radio value="account">{{ t('resourceGroups.typeAccount') }}</el-radio>
            </el-radio-group>
          </el-form-item>
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
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from '@/i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { apiClient, type ResourceGroupRecord } from '@/api/client'

const { t } = useI18n()

const filterType = ref('resource')
const groups = ref<ResourceGroupRecord[]>([])
const loading = ref(false)
const saving = ref(false)

const dialogVisible = ref(false)
const editingGroup = ref<ResourceGroupRecord | null>(null)
const form = reactive({
  name: '',
  group_type: 'resource' as string,
  description: ''
})

const loadGroups = async () => {
  loading.value = true
  try {
    const all = await apiClient.getResourceGroups()
    groups.value = all.filter(g => g.group_type === filterType.value)
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load groups')
  } finally {
    loading.value = false
  }
}

const showCreateDialog = () => {
  editingGroup.value = null
  form.name = ''
  form.group_type = filterType.value
  form.description = ''
  dialogVisible.value = true
}

const showEditDialog = (group: ResourceGroupRecord) => {
  editingGroup.value = group
  form.name = group.name
  form.group_type = group.group_type || 'resource'
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
        group_type: form.group_type,
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

onMounted(() => {
  loadGroups()
})
</script>
