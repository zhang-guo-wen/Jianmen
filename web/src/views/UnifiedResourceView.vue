<template>
  <div class="unified-resource-page">
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <!-- 主机管理 Tab -->
      <el-tab-pane :label="t('nav.hosts')" name="hosts">
        <HostsView v-if="activeTab === 'hosts'" />
      </el-tab-pane>

      <!-- 数据库管理 Tab -->
      <el-tab-pane :label="t('nav.databases')" name="databases">
        <DatabaseView v-if="activeTab === 'databases'" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from '@/i18n'
import HostsView from '@/views/HostsView.vue'
import DatabaseView from '@/views/DatabaseView.vue'

const { t } = useI18n()

const activeTab = ref('hosts')

const handleTabChange = (_tab: string) => {
  // Tab 切换时的逻辑（如果需要）
}
</script>

<style scoped>
.unified-resource-page {
  padding: 20px;
}
</style>
