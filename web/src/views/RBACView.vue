<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-radio-group v-model="mode">
        <el-radio-button label="users">{{ t('rbac.mode.users') }}</el-radio-button>
        <el-radio-button label="roles">{{ t('rbac.mode.roles') }}</el-radio-button>
        <el-radio-button label="policies">{{ t('rbac.mode.policies') }}</el-radio-button>
      </el-radio-group>
      <el-button :loading="loading" type="primary" @click="loadUsers">
        {{ t('common.refresh') }}
      </el-button>
    </div>

    <el-card class="placeholder-panel" shadow="never">
      <el-alert v-if="error" :title="error" type="error" show-icon />
      <el-table v-else :data="users" height="360">
        <el-table-column prop="username" :label="t('rbac.column.username')" min-width="160" />
        <el-table-column prop="name" :label="t('rbac.column.name')" min-width="160" />
        <el-table-column prop="role" :label="t('rbac.column.role')" min-width="160" />
        <el-table-column prop="status" :label="t('rbac.column.status')" width="140">
          <template #default="{ row }">
            <el-tag>{{ row.status || t('common.pending') }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !users.length && !error" :description="t('rbac.empty')" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';

import { apiClient, type ApiEnvelope, type UserRecord } from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();
const mode = ref('users');
const loading = ref(false);
const error = ref('');
const users = ref<UserRecord[]>([]);

function unwrapUsers(payload: ApiEnvelope<UserRecord[]> | UserRecord[]): UserRecord[] {
  return Array.isArray(payload) ? payload : payload.data ?? [];
}

async function loadUsers() {
  loading.value = true;
  error.value = '';

  try {
    users.value = unwrapUsers(await apiClient.getUsers());
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('rbac.loadError');
  } finally {
    loading.value = false;
  }
}

onMounted(loadUsers);
</script>
