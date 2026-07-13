<template>
  <div class="unified-rbac-page">
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
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

const { t } = useI18n()

const activeTab = ref('users')

const handleTabChange = (_tab: string) => {
  // Tab 切换时的逻辑（如果需要）
}
</script>

<style scoped>
.unified-rbac-page {
  padding: 20px;
}
</style>
