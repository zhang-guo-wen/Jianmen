<template>
  <div class="view-stack">
    <div class="metric-grid">
      <el-card class="metric-card" shadow="never">
        <span>{{ t('dashboard.metric.apiHealth') }}</span>
        <div class="metric-value">{{ healthLabel }}</div>
      </el-card>
      <el-card class="metric-card" shadow="never">
        <span>{{ t('dashboard.metric.targets') }}</span>
        <div class="metric-value">--</div>
      </el-card>
      <el-card class="metric-card" shadow="never">
        <span>{{ t('dashboard.metric.activeSessions') }}</span>
        <div class="metric-value">--</div>
      </el-card>
    </div>

    <el-card class="placeholder-panel" shadow="never">
      <template #header>
        <div class="toolbar">
          <span>{{ t('dashboard.operationalOverview') }}</span>
          <el-button :loading="loading" @click="loadHealth">{{ t('common.refresh') }}</el-button>
        </div>
      </template>
      <el-alert v-if="error" :title="error" type="error" show-icon />
      <pre v-else>{{ health }}</pre>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';

import { apiClient, type HealthResponse } from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();
const health = ref<HealthResponse | null>(null);
const loading = ref(false);
const error = ref('');

const healthLabel = computed(() => health.value?.status ?? t('dashboard.unknown'));

async function loadHealth() {
  loading.value = true;
  error.value = '';

  try {
    health.value = await apiClient.getHealth();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('dashboard.loadError');
  } finally {
    loading.value = false;
  }
}

onMounted(loadHealth);
</script>
