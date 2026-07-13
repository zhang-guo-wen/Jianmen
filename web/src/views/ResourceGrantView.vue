<template>
  <div class="resource-grant-page">
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <!-- 资源授权列表 -->
      <el-tab-pane :label="t('resourceGrant.grants')" name="grants">
        <div class="tab-header">
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
          <el-button type="primary" @click="showGrantDialog()">
            <el-icon><Plus /></el-icon>
            {{ t('resourceGrant.addGrant') }}
          </el-button>
        </div>

        <el-table :data="grants" v-loading="loading" stripe>
          <el-table-column :label="t('resourceGrant.principalType')" prop="principal_type" width="120">
            <template #default="{ row }">
              <el-tag :type="row.principal_type === 'user' ? 'primary' : 'success'" size="small">
                {{ row.principal_type === 'user' ? t('resourceGrant.user') : t('resourceGrant.userGroup') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('resourceGrant.principalName')" min-width="150">
            <template #default="{ row }">
              {{ getPrincipalName(row) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('resourceGrant.resourceType')" prop="resource_type" width="140" />
          <el-table-column :label="t('resourceGrant.resourceName')" min-width="150">
            <template #default="{ row }">
              {{ getResourceName(row) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('resourceGrant.effect')" prop="effect" width="100">
            <template #default="{ row }">
              <el-tag :type="row.effect === 'allow' ? 'success' : 'danger'" size="small">
                {{ row.effect }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('resourceGrant.expiresAt')" prop="expires_at" width="180">
            <template #default="{ row }">
              {{ row.expires_at ? formatTime(row.expires_at) : t('resourceGrant.never') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="100" fixed="right">
            <template #default="{ row }">
              <el-button type="danger" link size="small" @click="deleteGrant(row)">
                {{ t('common.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 用户组管理 -->
      <el-tab-pane :label="t('resourceGrant.userGroups')" name="groups">
        <div class="tab-header">
          <span />
          <el-button type="primary" @click="showGroupDialog()">
            <el-icon><Plus /></el-icon>
            {{ t('resourceGrant.addGroup') }}
          </el-button>
        </div>

        <el-table :data="groups" v-loading="loading" stripe>
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
        </el-table>
      </el-tab-pane>
    </el-tabs>

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

    <!-- 创建资源授权对话框 -->
    <el-dialog
      v-model="grantDialogVisible"
      :title="t('resourceGrant.addGrant')"
      width="600px"
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

        <el-form-item :label="t('resourceGrant.resourceType')" required>
          <el-select v-model="grantForm.resource_type" style="width: 100%">
            <el-option label="Host Account" value="host_account" />
            <el-option label="Database Account" value="database_account" />
            <el-option :label="t('resourceGrant.resourceGroup')" value="resource_group" />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('resourceGrant.selectResource')" required>
          <el-select
            v-model="grantForm.resource_id"
            filterable
            remote
            :remote-method="searchResources"
            :placeholder="t('resourceGrant.searchResource')"
            style="width: 100%"
          >
            <el-option
              v-for="item in resourceOptions"
              :key="item.id"
              :label="item.name"
              :value="item.id"
            />
          </el-select>
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
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useI18n } from '@/i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import {
  apiClient,
  type UserGroupRecord,
  type UserGroupMemberRecord,
  type ResourceGrantRecord,
  type UserRecord
} from '@/api/client'

const { t } = useI18n()

// State
const activeTab = ref('grants')
const loading = ref(false)
const saving = ref(false)

// Grants state
const grants = ref<ResourceGrantRecord[]>([])
const filters = reactive({
  principal_type: '',
  resource_type: ''
})

// Groups state
const groups = ref<UserGroupRecord[]>([])
const groupMembers = ref<Record<string, UserGroupMemberRecord[]>>({})

// Dialogs
const groupDialogVisible = ref(false)
const membersDialogVisible = ref(false)
const grantDialogVisible = ref(false)

// Group form
const editingGroup = ref<UserGroupRecord | null>(null)
const groupForm = reactive({
  name: '',
  description: ''
})

// Members
const currentGroupId = ref('')
const currentMembers = ref<UserGroupMemberRecord[]>([])
const loadingMembers = ref(false)
const newMemberId = ref('')
const availableUsers = ref<UserRecord[]>([])
const allUsers = ref<UserRecord[]>([])

// Grant form
const grantForm = reactive({
  principal_type: 'user' as 'user' | 'user_group',
  principal_id: '',
  resource_type: 'host_account',
  resource_id: '',
  effect: 'allow' as 'allow' | 'deny'
})
const expiresOption = ref('never')
const customExpiresAt = ref<Date | null>(null)
const resourceOptions = ref<Array<{ id: string; name: string }>>([])

// Computed
const principalOptions = computed(() => {
  if (grantForm.principal_type === 'user') {
    return allUsers.value.map(u => ({ id: u.id, name: u.username }))
  }
  return groups.value.map(g => ({ id: g.id, name: g.name }))
})

// Methods
const formatTime = (time: string) => {
  if (!time) return ''
  return new Date(time).toLocaleString()
}

const getPrincipalName = (grant: ResourceGrantRecord) => {
  if (grant.principal_type === 'user') {
    const user = allUsers.value.find(u => u.id === grant.principal_id)
    return user?.username || grant.principal_id
  }
  const group = groups.value.find(g => g.id === grant.principal_id)
  return group?.name || grant.principal_id
}

const getResourceName = (grant: ResourceGrantRecord) => {
  const opt = resourceOptions.value.find(o => o.id === grant.resource_id)
  return opt?.name || grant.resource_id
}

const getMemberCount = (groupId: string) => {
  return groupMembers.value[groupId]?.length || 0
}

const getUsernameById = (userId: string) => {
  const user = allUsers.value.find(u => u.id === userId)
  return user?.username || userId
}

const loadGrants = async () => {
  loading.value = true
  try {
    const params: Record<string, string> = {}
    if (filters.principal_type) params.principal_type = filters.principal_type
    if (filters.resource_type) params.resource_type = filters.resource_type
    grants.value = await apiClient.getResourceGrants(params)
  } catch (e: any) {
    ElMessage.error(e.message || 'Failed to load grants')
  } finally {
    loading.value = false
  }
}

const loadGroups = async () => {
  loading.value = true
  try {
    groups.value = await apiClient.getUserGroups()
    // Load member counts
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
    // Update member count
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

const showGrantDialog = () => {
  grantForm.principal_type = 'user'
  grantForm.principal_id = ''
  grantForm.resource_type = 'host_account'
  grantForm.resource_id = ''
  grantForm.effect = 'allow'
  expiresOption.value = 'never'
  customExpiresAt.value = null
  grantDialogVisible.value = true
}

const handleExpiresOptionChange = (val: string) => {
  if (val !== 'custom') {
    customExpiresAt.value = null
  }
}

const searchResources = async (query: string) => {
  if (!query) {
    resourceOptions.value = []
    return
  }
  // Search based on resource type
  try {
    if (grantForm.resource_type === 'host_account') {
      const resp = await apiClient.getTargets({ q: query, page: 1, page_size: 50 })
      resourceOptions.value = (resp.items || []).map((t: any) => ({
        id: t.id,
        name: `${t.username || ''}@${t.host_name || t.host_address || ''}`
      }))
    } else if (grantForm.resource_type === 'database_account') {
      // Search all database accounts (need to get instances first)
      const instances = await apiClient.getDBInstances({ page: 1, page_size: 100 })
      const allAccounts: Array<{ id: string; name: string }> = []
      for (const inst of (instances.items || [])) {
        if (!inst.id) continue
        try {
          const resp = await apiClient.getDBAccounts(inst.id, { q: query, page: 1, page_size: 20 })
          for (const a of (resp.items || [])) {
            if (a.id) {
              allAccounts.push({
                id: a.id,
                name: `${a.unique_name || a.username || ''} (${inst.name || ''})`
              })
            }
          }
        } catch { /* ignore */ }
      }
      resourceOptions.value = allAccounts
    } else if (grantForm.resource_type === 'resource_group') {
      // TODO: implement resource group search
      resourceOptions.value = []
    }
  } catch {
    resourceOptions.value = []
  }
}

const saveGrant = async () => {
  if (!grantForm.principal_id || !grantForm.resource_id) {
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
    await apiClient.createResourceGrant({
      ...grantForm,
      expires_at: expiresAt
    })
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

const handleTabChange = (tab: string) => {
  if (tab === 'grants') {
    loadGrants()
  } else if (tab === 'groups') {
    loadGroups()
  }
}

// Watch principal type change to reset selection
watch(() => grantForm.principal_type, () => {
  grantForm.principal_id = ''
})

// Watch resource type change to reset selection
watch(() => grantForm.resource_type, () => {
  grantForm.resource_id = ''
  resourceOptions.value = []
})

// Init
onMounted(async () => {
  await loadUsers()
  await loadGrants()
})
</script>

<style scoped>
.resource-grant-page {
  padding: 20px;
}

.tab-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.filters {
  display: flex;
  gap: 12px;
  align-items: center;
}

.members-header {
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
