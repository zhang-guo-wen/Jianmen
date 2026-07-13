<template>
  <div class="view-stack">
    <div class="page-container">
      <div class="page-card">
        <div class="page-card__toolbar">
          <div class="page-card__spacer"></div>
          <div class="page-card__actions">
            <el-button type="primary" @click="showCreateDialog">
              <el-icon><Plus /></el-icon>
              {{ t('resourceGroups.create') }}
            </el-button>
          </div>
        </div>
        <div class="page-card__body">
          <el-table :data="groups" v-loading="loading" stripe>
            <el-table-column :label="t('resourceGroups.name')" prop="name" min-width="150" />
            <el-table-column :label="t('resourceGroups.resourceType')" prop="resource_type" min-width="120">
              <template #default="{ row }">
                <el-tag size="small">{{ getResourceTypeLabel(row.resource_type) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('resourceGroups.description')" prop="description" min-width="200" show-overflow-tooltip />
            <el-table-column :label="t('resourceGroups.memberCount')" width="100">
              <template #default="{ row }">
                <el-button link type="primary" @click="showMembers(row)">
                  {{ row.member_count || 0 }}
                </el-button>
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
          <el-form-item :label="t('resourceGroups.resourceType')" required>
            <el-select v-model="form.resource_type" style="width: 100%">
              <el-option :label="t('resourceGroups.hostAccount')" value="host_account" />
              <el-option :label="t('resourceGroups.databaseAccount')" value="database_account" />
            </el-select>
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

      <!-- 成员管理对话框 -->
      <el-dialog
        v-model="membersDialogVisible"
        :title="t('resourceGroups.manageMembers')"
        width="700px"
      >
        <div class="members-toolbar">
          <el-select
            v-model="newMemberId"
            filterable
            :placeholder="t('resourceGroups.selectResource')"
            style="width: 300px"
          >
            <el-option
              v-for="item in availableResources"
              :key="item.id"
              :label="item.name"
              :value="item.id"
            />
          </el-select>
          <el-button type="primary" @click="addMember" :disabled="!newMemberId">
            {{ t('resourceGroups.addMember') }}
          </el-button>
        </div>

        <el-table :data="members" v-loading="loadingMembers" stripe style="margin-top: 16px">
          <el-table-column :label="t('resourceGroups.resourceName')" min-width="200">
            <template #default="{ row }">
              {{ row.resource_name || row.resource_id }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="100" fixed="right">
            <template #default="{ row }">
              <el-button link type="danger" size="small" @click="removeMember(row)">
                {{ t('common.remove') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-dialog>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from '@/i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { apiClient } from '@/api/client'

const { t } = useI18n()

interface ResourceGroup {
  id: string
  name: string
  resource_type: string
  description?: string
  member_count?: number
}

interface GroupMember {
  id: string
  group_id: string
  resource_type: string
  resource_id: string
  resource_name?: string
}

const groups = ref<ResourceGroup[]>([])
const loading = ref(false)
const saving = ref(false)

const dialogVisible = ref(false)
const editingGroup = ref<ResourceGroup | null>(null)
const form = reactive({
  name: '',
  resource_type: 'host_account',
  description: ''
})

const membersDialogVisible = ref(false)
const currentGroupId = ref('')
const members = ref<GroupMember[]>([])
const loadingMembers = ref(false)
const newMemberId = ref('')
const availableResources = ref<Array<{ id: string; name: string }>>([])

const getResourceTypeLabel = (type: string) => {
  switch (type) {
    case 'host_account': return t('resourceGroups.hostAccount')
    case 'database_account': return t('resourceGroups.databaseAccount')
    default: return type
  }
}

const loadGroups = async () => {
  loading.value = true
  try {
    // TODO: 实现资源组列表 API
    groups.value = []
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load groups')
  } finally {
    loading.value = false
  }
}

const showCreateDialog = () => {
  editingGroup.value = null
  form.name = ''
  form.resource_type = 'host_account'
  form.description = ''
  dialogVisible.value = true
}

const showEditDialog = (group: ResourceGroup) => {
  editingGroup.value = group
  form.name = group.name
  form.resource_type = group.resource_type
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
    // TODO: 实现资源组保存 API
    ElMessage.success(t('common.saved'))
    dialogVisible.value = false
    await loadGroups()
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to save group')
  } finally {
    saving.value = false
  }
}

const deleteGroup = async (group: ResourceGroup) => {
  try {
    await ElMessageBox.confirm(
      t('resourceGroups.confirmDelete').replace('{name}', group.name),
      t('common.delete'),
      { confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel'), type: 'warning' }
    )
    // TODO: 实现资源组删除 API
    ElMessage.success(t('common.deleted'))
    await loadGroups()
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error(e.message || 'Failed to delete group')
    }
  }
}

const showMembers = async (group: ResourceGroup) => {
  currentGroupId.value = group.id
  membersDialogVisible.value = true
  loadingMembers.value = true
  try {
    // TODO: 实现资源组成员列表 API
    members.value = []
    // 加载可用资源
    if (group.resource_type === 'host_account') {
      const resp = await apiClient.getTargets({ page: 1, page_size: 1000 })
      availableResources.value = (resp.items || []).map((t: any) => ({
        id: t.id,
        name: `${t.username || ''}@${t.host_name || t.host_address || ''}`
      }))
    }
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load members')
  } finally {
    loadingMembers.value = false
  }
}

const addMember = async () => {
  if (!newMemberId.value) return
  try {
    // TODO: 实现添加资源组成员 API
    ElMessage.success(t('common.added'))
    newMemberId.value = ''
    // 重新加载成员列表
    await showMembers({ id: currentGroupId.value } as ResourceGroup)
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to add member')
  }
}

const removeMember = async (_member: GroupMember) => {
  try {
    await ElMessageBox.confirm(
      t('resourceGroups.confirmRemoveMember'),
      t('common.remove'),
      { confirmButtonText: t('common.remove'), cancelButtonText: t('common.cancel'), type: 'warning' }
    )
    // TODO: 实现删除资源组成员 API
    ElMessage.success(t('common.removed'))
    // 重新加载成员列表
    await showMembers({ id: currentGroupId.value } as ResourceGroup)
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error(e.message || 'Failed to remove member')
    }
  }
}

onMounted(() => {
  loadGroups()
})
</script>

<style scoped>
.members-toolbar {
  display: flex;
  gap: 12px;
  align-items: center;
}
</style>
