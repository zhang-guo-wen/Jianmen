<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-select v-model="status" style="max-width: 220px">
        <el-option :label="t('sessions.filter.all')" value="all" />
        <el-option :label="t('sessions.filter.active')" value="active" />
        <el-option :label="t('sessions.filter.closed')" value="closed" />
      </el-select>
      <el-button :loading="loading" type="primary" @click="loadSessions">
        {{ t('common.refresh') }}
      </el-button>
    </div>

    <el-card class="placeholder-panel" shadow="never">
      <el-alert v-if="error" :title="error" type="error" show-icon />
      <el-table v-else :data="sessions" height="360">
        <el-table-column prop="id" :label="t('sessions.column.id')" min-width="160" />
        <el-table-column prop="user" :label="t('sessions.column.user')" min-width="160" />
        <el-table-column prop="target" :label="t('sessions.column.target')" min-width="180" />
        <el-table-column prop="startedAt" :label="t('sessions.column.started')" min-width="180" />
        <el-table-column prop="status" :label="t('sessions.column.status')" width="140">
          <template #default="{ row }">
            <el-tag>{{ row.status || t('common.pending') }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !sessions.length && !error" :description="t('sessions.empty')" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';

import { apiClient, type ApiEnvelope, type SessionRecord } from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();
const status = ref('all');
const loading = ref(false);
const error = ref('');
const sessions = ref<SessionRecord[]>([]);

function unwrapSessions(payload: ApiEnvelope<SessionRecord[]> | SessionRecord[]): SessionRecord[] {
  return Array.isArray(payload) ? payload : payload.data ?? [];
}

async function loadSessions() {
  loading.value = true;
  error.value = '';

  try {
    sessions.value = unwrapSessions(await apiClient.getSessions());
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('sessions.loadError');
  } finally {
    loading.value = false;
  }
}

onMounted(loadSessions);
</script>
