<template>
  <div class="view-stack">
    <div class="page-container">
      <DataTableCard
        :data="groups"
        :loading="loading"
        :total="total"
        v-model:page="page"
        v-model:page-size="pageSize"
        search-placeholder="搜索用户组名称、描述..."
        @search="onSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="showGroupDialog()">
            <el-icon><Plus /></el-icon>
            {{ t('resourceGrant.addGroup') }}
          </el-button>
        </template>

        <el-table-column :label="t('resourceGrant.groupName')" prop="name" min-width="150" />
        <el-table-column :label="t('resourceGrant.groupDescription')" prop="description" min-width="200" />
        <el-table-column :label="t('resourceGrant.memberCount')" width="120">
          <template #default="{ row }">
            <el-button link type="primary" @click="showMembers(row)">
              {{ getMemberCount(row.id) }} {{ t('resourceGrant.members') }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showGroupDialog(row)">
              {{ t('common.edit') }}
            </el-button>
            <el-button type="danger" link size="small" @click="deleteGroup(row)">
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>

      <!-- 创建/编辑用户组对话框 -->
      <el-dialog
        v-model="groupDialogVisible"
        :title="editingGroup ? t('resourceGrant.editGroup') : t('resourceGrant.addGroup')"
        width="500px"
      >
        <el-form :model="groupForm" label-width="100px">
          <el-form-item :label="t('resourceGrant.groupName')" required>
            <el-input v-model="groupForm.name" :placeholder="t('resourceGrant.groupNamePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('resourceGrant.groupDescription')">
            <el-input v-model="groupForm.description" type="textarea" :rows="3" :placeholder="t('resourceGrant.groupDescriptionPlaceholder')" />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="groupDialogVisible = false">{{ t('common.cancel') }}</el-button>
          <el-button type="primary" @click="saveGroup" :loading="saving">{{ t('common.save') }}</el-button>
        </template>
      </el-dialog>

      <!-- 用户组成员管理对话框 -->
      <el-dialog
        v-model="membersDialogVisible"
        :title="t('resourceGrant.manageMembers')"
        width="600px"
      >
        <div class="members-header">
          <el-select
            v-model="newMemberId"
            filterable
            remote
            :remote-method="searchUsers"
            :placeholder="t('resourceGrant.searchUser')"
            style="width: 300px"
          >
            <el-option
              v-for="user in availableUsers"
              :key="user.id"
              :label="user.username"
              :value="user.id"
            />
          </el-select>
          <el-button type="primary" @click="addMember" :disabled="!newMemberId">
            {{ t('resourceGrant.addMember') }}
          </el-button>
        </div>

        <el-table :data="currentMembers" v-loading="loadingMembers" stripe style="margin-top: 16px">
          <el-table-column :label="t('resourceGrant.username')" prop="user_id" min-width="200">
            <template #default="{ row }">
              {{ getUsernameById(row.user_id) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="100" fixed="right">
            <template #default="{ row }">
              <el-button type="danger" link size="small" @click="removeMember(row)">
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
import { ref, reactive, onMounted, watch } from 'vue'
import { useI18n } from '@/i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import DataTableCard from '@/components/DataTableCard.vue'
import {
  apiClient,
  type UserGroupRecord,
  type UserGroupMemberRecord,
  type UserRecord
} from '@/api/client'

const { t } = useI18n()

const groups = ref<UserGroupRecord[]>([])
const groupMembers = ref<Record<string, UserGroupMemberRecord[]>>({})
const allUsers = ref<UserRecord[]>([])
const loading = ref(false)
const saving = ref(false)

// 分页与搜索状态
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const keyword = ref('')

const groupDialogVisible = ref(false)
const editingGroup = ref<UserGroupRecord | null>(null)
const groupForm = reactive({
  name: '',
  description: ''
})

const membersDialogVisible = ref(false)
const currentGroupId = ref('')
const currentMembers = ref<UserGroupMemberRecord[]>([])
const loadingMembers = ref(false)
const newMemberId = ref('')
const availableUsers = ref<UserRecord[]>([])

const getMemberCount = (groupId: string) => {
  return groupMembers.value[groupId]?.length || 0
}

const getUsernameById = (userId: string) => {
  const user = allUsers.value.find(u => u.id === userId)
  return user?.username || userId
}

const loadGroups = async () => {
  loading.value = true
  try {
    const res = await apiClient.getUserGroups({
      page: page.value,
      page_size: pageSize.value,
      q: keyword.value || undefined,
    })
    groups.value = res.items ?? []
    total.value = res.total ?? 0
    for (const group of groups.value) {
      try {
        const members = await apiClient.getUserGroupMembers(group.id)
        groupMembers.value[group.id] = members
      } catch {
        groupMembers.value[group.id] = []
      }
    }
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load groups')
  } finally {
    loading.value = false
  }
}

const loadUsers = async () => {
  try {
    const resp = await apiClient.getUsers({ page: 1, page_size: 1000 })
    allUsers.value = resp.items || []
  } catch {
    allUsers.value = []
  }
}

const onSearch = (q: string) => {
  keyword.value = q
  page.value = 1
  loadGroups()
}

watch([page, pageSize], () => loadGroups())

const showGroupDialog = (group?: UserGroupRecord) => {
  editingGroup.value = group || null
  groupForm.name = group?.name || ''
  groupForm.description = group?.description || ''
  groupDialogVisible.value = true
}

const saveGroup = async () => {
  if (!groupForm.name) {
    ElMessage.warning(t('resourceGrant.groupNameRequired'))
    return
  }
  saving.value = true
  try {
    if (editingGroup.value) {
      await apiClient.updateUserGroup(editingGroup.value.id, groupForm)
      ElMessage.success(t('common.saved'))
    } else {
      await apiClient.createUserGroup(groupForm)
      ElMessage.success(t('common.created'))
    }
    groupDialogVisible.value = false
    await loadGroups()
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to save group')
  } finally {
    saving.value = false
  }
}

const deleteGroup = async (group: UserGroupRecord) => {
  try {
    await ElMessageBox.confirm(
      t('resourceGrant.confirmDeleteGroup').replace('{name}', group.name),
      t('common.delete'),
      { confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel'), type: 'warning' }
    )
    await apiClient.deleteUserGroup(group.id)
    ElMessage.success(t('common.deleted'))
    await loadGroups()
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error(e.message || 'Failed to delete group')
    }
  }
}

const showMembers = async (group: UserGroupRecord) => {
  currentGroupId.value = group.id
  membersDialogVisible.value = true
  loadingMembers.value = true
  try {
    currentMembers.value = await apiClient.getUserGroupMembers(group.id)
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load members')
  } finally {
    loadingMembers.value = false
  }
}

const searchUsers = (query: string) => {
  if (!query) {
    availableUsers.value = allUsers.value
    return
  }
  availableUsers.value = allUsers.value.filter(u =>
    (u.username || '').toLowerCase().includes(query.toLowerCase())
  )
}

const addMember = async () => {
  if (!newMemberId.value) return
  try {
    await apiClient.addUserGroupMember(currentGroupId.value, newMemberId.value)
    ElMessage.success(t('common.added'))
    newMemberId.value = ''
    currentMembers.value = await apiClient.getUserGroupMembers(currentGroupId.value)
    groupMembers.value[currentGroupId.value] = currentMembers.value
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to add member')
  }
}

const removeMember = async (member: UserGroupMemberRecord) => {
  try {
    await ElMessageBox.confirm(
      t('resourceGrant.confirmRemoveMember'),
      t('common.remove'),
      { confirmButtonText: t('common.remove'), cancelButtonText: t('common.cancel'), type: 'warning' }
    )
    await apiClient.removeUserGroupMember(currentGroupId.value, member.user_id)
    ElMessage.success(t('common.removed'))
    currentMembers.value = await apiClient.getUserGroupMembers(currentGroupId.value)
    groupMembers.value[currentGroupId.value] = currentMembers.value
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error(e.message || 'Failed to remove member')
    }
  }
}

onMounted(async () => {
  await loadUsers()
  await loadGroups()
})
</script>

<style scoped>
.members-header {
  display: flex;
  gap: 12px;
  align-items: center;
}
</style>
