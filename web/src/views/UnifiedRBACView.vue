<template>
  <div class="view-stack">
    <el-tabs v-model="activeTab" class="page-tabs" @tab-change="handleTabChange">
      <!-- 用户管理 Tab -->
      <el-tab-pane :label="t('nav.users')" name="users">
        <UsersView v-if="activeTab === 'users'" />
      </el-tab-pane>

      <!-- 角色管理 Tab -->
      <el-tab-pane :label="t('nav.roles')" name="roles">
        <RolesView v-if="activeTab === 'roles'" />
      </el-tab-pane>

      <!-- 用户组 Tab -->
      <el-tab-pane :label="t('nav.userGroups')" name="userGroups">
        <UserGroupsView v-if="activeTab === 'userGroups'" />
      </el-tab-pane>

      <!-- 资源授权 Tab -->
      <el-tab-pane :label="t('nav.resourceGrant')" name="resourceGrant">
        <ResourceGrantView v-if="activeTab === 'resourceGrant'" />
      </el-tab-pane>

      <!-- 资源分组 Tab -->
      <el-tab-pane :label="t('nav.resourceGroups')" name="resourceGroups">
        <ResourceGroupsContent v-if="activeTab === 'resourceGroups'" />
      </el-tab-pane>

      <!-- 账号分组 Tab -->
      <el-tab-pane :label="t('nav.accountGroups')" name="accountGroups">
        <AccountGroupsContent v-if="activeTab === 'accountGroups'" />
      </el-tab-pane>

      <el-tab-pane label="&#x4E34;&#x65F6;&#x7528;&#x6237;" name="temporaryAccounts">
        <TemporaryAccountsView v-if="activeTab === 'temporaryAccounts'" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from '@/i18n'
import UsersView from '@/views/UsersView.vue'
import RolesView from '@/views/RolesView.vue'
import UserGroupsView from '@/views/UserGroupsView.vue'
import ResourceGrantView from '@/views/ResourceGrantView.vue'
import ResourceGroupsContent from '@/components/ResourceGroupsContent.vue'
import AccountGroupsContent from '@/components/AccountGroupsContent.vue'
import TemporaryAccountsView from '@/views/TemporaryAccountsView.vue'

const { t } = useI18n()

const activeTab = ref('users')

const handleTabChange = (_tab: string) => {
  // Tab 切换时的逻辑（如果需要）
}
</script>

<style scoped>
/* 保留 tab header 默认间距 (page-tabs 会清零) */
.page-tabs :deep(.el-tabs__header) {
  margin-bottom: 15px;
  padding: 0;
}
</style>
