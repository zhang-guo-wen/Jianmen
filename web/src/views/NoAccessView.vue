<template>
  <div class="no-access-page">
    <el-empty :description="t('route.noAccess.description')">
      <el-button v-if="permission.error" type="primary" :loading="permission.loading" @click="retry">
        {{ t('common.retry') }}
      </el-button>
    </el-empty>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from '@/i18n';
import { usePermissionStore } from '@/stores/permission';

const { t } = useI18n();
const permission = usePermissionStore();

async function retry() {
  await permission.fetch({ force: true });
  const path = permission.firstAccessiblePath();
  if (path) window.location.assign(path);
}
</script>

<style scoped>
.no-access-page {
  min-height: 55vh;
  display: grid;
  place-items: center;
}
</style>
