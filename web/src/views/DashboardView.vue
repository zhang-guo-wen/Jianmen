<template>
  <div class="view-stack">
    <div class="metric-grid">
      <el-card class="metric-card" shadow="never">
        <span>{{ t('dashboard.metric.apiHealth') }}</span>
        <div class="metric-value">{{ healthLabel }}</div>
      </el-card>
      <el-card class="metric-card" shadow="never">
        <span>{{ t('dashboard.metric.targets') }}</span>
        <div class="metric-value">{{ targetsLabel }}</div>
      </el-card>
      <el-card class="metric-card" shadow="never">
        <span>{{ t('dashboard.metric.activeSessions') }}</span>
        <div class="metric-value">{{ activeSessionsLabel }}</div>
      </el-card>
      <el-card class="metric-card" shadow="never">
        <span>{{ t('dashboard.metric.rbac') }}</span>
        <div class="metric-value">{{ rbacLabel }}</div>
      </el-card>
      <el-card class="metric-card" shadow="never">
        <span>{{ t('dashboard.metric.dbProxy') }}</span>
        <div class="metric-value">{{ dbInstancesLabel }}</div>
      </el-card>
      <el-card class="metric-card" shadow="never">
        <span>{{ t('dashboard.metric.audit') }}</span>
        <div class="metric-value">{{ auditLabel }}</div>
      </el-card>
    </div>

    <el-card class="placeholder-panel" shadow="never">
      <template #header>
        <div class="toolbar">
          <span>{{ t('dashboard.operationalOverview') }}</span>
          <el-button :loading="loading" @click="loadOverview">{{ t('common.refresh') }}</el-button>
        </div>
      </template>
      <el-table :data="serviceRows" height="320">
        <el-table-column prop="name" :label="t('dashboard.column.service')" min-width="160" />
        <el-table-column :label="t('common.status')" width="140">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)">
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="summary" :label="t('dashboard.column.summary')" min-width="220" />
        <el-table-column prop="detail" :label="t('dashboard.column.detail')" min-width="320" show-overflow-tooltip />
      </el-table>
    </el-card>

    <el-card class="placeholder-panel" shadow="never">
      <template #header>
        <span>{{ t('dashboard.rawHealth') }}</span>
      </template>
      <el-alert v-if="errors.health" :title="errors.health" type="error" show-icon />
      <pre v-else class="json-preview">{{ JSON.stringify(health, null, 2) }}</pre>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';

import {
  apiClient,
  type ApiEnvelope,
  type DBConnectionRecord,
  type DBInstanceRecord,
  type HealthResponse,
  type RBACPermissionRecord,
  type RBACRoleRecord,
  type SessionRecord,
  type TargetRecord
} from '@/api/client';
import { useI18n } from '@/i18n';

type ServiceStatus = 'ok' | 'warning' | 'unavailable' | 'unknown';
type ServiceRow = {
  name: string;
  status: ServiceStatus;
  summary: string;
  detail: string;
};

const { t } = useI18n();
const health = ref<HealthResponse | null>(null);
const targets = ref<TargetRecord[]>([]);
const sessions = ref<SessionRecord[]>([]);
const dbConnections = ref<DBConnectionRecord[]>([]);
const dbInstances = ref<DBInstanceRecord[]>([]);
const roles = ref<RBACRoleRecord[]>([]);
const permissions = ref<RBACPermissionRecord[]>([]);
const loading = ref(false);
const errors = reactive({
  health: '',
  targets: '',
  sessions: '',
  db: '',
  dbInstances: '',
  rbac: ''
});

const healthLabel = computed(() => health.value?.status ?? t('dashboard.unknown'));
const targetsLabel = computed(() => countOrFallback(targets.value.length, errors.targets));
const activeSessions = computed(() => sessions.value.filter(isActiveSession));
const activeSessionsLabel = computed(() => countOrFallback(activeSessions.value.length, errors.sessions));
const rbacLabel = computed(() => (errors.rbac ? t('dashboard.status.unavailable') : `${roles.value.length}/${permissions.value.length}`));
const enabledDBInstances = computed(() => dbInstances.value.filter((inst) => !inst.disabled));
const dbInstancesLabel = computed(() =>
  errors.dbInstances
    ? t('dashboard.status.unavailable')
    : `${enabledDBInstances.value.length}/${dbInstances.value.length}`
);
const auditLabel = computed(() => {
  if (errors.sessions && errors.db) {
    return t('dashboard.status.unavailable');
  }

  return String(sessions.value.length + dbConnections.value.length);
});
const serviceRows = computed<ServiceRow[]>(() => [
  {
    name: t('dashboard.service.api'),
    status: errors.health ? 'unavailable' : healthStatus.value,
    summary: health.value?.status ?? t('dashboard.unknown'),
    detail: errors.health || String(health.value?.time ?? health.value?.version ?? t('common.none'))
  },
  {
    name: t('dashboard.service.rbac'),
    status: errors.rbac ? 'unavailable' : 'ok',
    summary: errors.rbac
      ? t('dashboard.status.unavailable')
      : formatText(t('dashboard.summary.rbac'), {
          roles: String(roles.value.length),
          permissions: String(permissions.value.length)
        }),
    detail: errors.rbac || t('dashboard.detail.rbac')
  },
  {
    name: t('dashboard.service.dbProxy'),
    status: errors.dbInstances ? 'unavailable' : enabledDBInstances.value.length ? 'ok' : 'warning',
    summary: errors.dbInstances
      ? t('dashboard.status.unavailable')
      : formatText(t('dashboard.summary.dbProxy'), {
          count: String(dbConnections.value.length),
          enabled: String(enabledDBInstances.value.length),
          total: String(dbInstances.value.length)
        }),
    detail: errors.dbInstances || dbInstancesDetail.value
  },
  {
    name: t('dashboard.service.audit'),
    status: errors.sessions && errors.db ? 'unavailable' : 'ok',
    summary: formatText(t('dashboard.summary.audit'), {
      ssh: errors.sessions ? '-' : String(sessions.value.length),
      db: errors.db ? '-' : String(dbConnections.value.length)
    }),
    detail: [errors.sessions, errors.db].filter(Boolean).join('; ') || t('dashboard.detail.audit')
  }
]);
const healthStatus = computed<ServiceStatus>(() => {
  const status = String(health.value?.status ?? '').toLowerCase();

  if (!status) {
    return 'unknown';
  }

  return status === 'ok' || status === 'healthy' ? 'ok' : 'warning';
});
const dbInstancesDetail = computed(() => {
  if (!dbInstances.value.length) {
    return 'No database instances configured';
  }

  return dbInstances.value
    .map((inst) => {
      const state = inst.disabled ? t('common.disabled') : t('common.enabled');
      return `${inst.name || inst.protocol || '-'} ${state} ${inst.address || '-'}`;
    })
    .join('; ');
});

function unwrapArray<T>(payload: ApiEnvelope<T[]> | T[]): T[] {
  return Array.isArray(payload) ? payload : payload.data ?? [];
}

function countOrFallback(count: number, error: string): string {
  return error ? t('dashboard.status.unavailable') : String(count);
}

function isActiveSession(session: SessionRecord): boolean {
  const state = String(session.state ?? session.status ?? '').toLowerCase();

  if (session.ended_at) {
    return false;
  }

  return state === '' || state === 'active' || state === 'started' || state === 'open';
}

function statusTagType(status: ServiceStatus): 'success' | 'warning' | 'danger' | 'info' {
  switch (status) {
    case 'ok':
      return 'success';
    case 'warning':
      return 'warning';
    case 'unavailable':
      return 'danger';
    case 'unknown':
      return 'info';
  }
}

function statusLabel(status: ServiceStatus): string {
  switch (status) {
    case 'ok':
      return t('dashboard.status.ok');
    case 'warning':
      return t('dashboard.status.warning');
    case 'unavailable':
      return t('dashboard.status.unavailable');
    case 'unknown':
      return t('dashboard.status.unknown');
  }
}

function formatText(template: string, values: Record<string, string>): string {
  return Object.entries(values).reduce((text, [key, value]) => text.split(`{${key}}`).join(value), template);
}

async function loadHealth() {
  errors.health = '';

  try {
    health.value = await apiClient.getHealth();
  } catch (err) {
    health.value = null;
    errors.health = err instanceof Error ? err.message : t('dashboard.loadError');
  }
}

async function loadTargets() {
  errors.targets = '';

  try {
    targets.value = unwrapArray(await apiClient.getTargets());
  } catch (err) {
    targets.value = [];
    errors.targets = err instanceof Error ? err.message : t('hosts.error.loadList');
  }
}

async function loadSessions() {
  errors.sessions = '';

  try {
    sessions.value = unwrapArray(await apiClient.getSessions());
  } catch (err) {
    sessions.value = [];
    errors.sessions = err instanceof Error ? err.message : t('sessions.loadError');
  }
}

async function loadDBConnections() {
  errors.db = '';

  try {
    dbConnections.value = unwrapArray(await apiClient.getDBConnections());
  } catch (err) {
    dbConnections.value = [];
    errors.db = err instanceof Error ? err.message : t('audit.error.loadDBConnections');
  }
}

async function loadDBInstances() {
  errors.dbInstances = '';

  try {
    dbInstances.value = unwrapArray(await apiClient.getDBInstances());
  } catch (err) {
    dbInstances.value = [];
    errors.dbInstances = err instanceof Error ? err.message : 'Unable to load database instance configuration';
  }
}

async function loadRBACSummary() {
  errors.rbac = '';

  try {
    const [rolePayload, permissionPayload] = await Promise.all([
      apiClient.getRBACRoles(),
      apiClient.getRBACPermissions()
    ]);
    roles.value = unwrapArray(rolePayload);
    permissions.value = unwrapArray(permissionPayload);
  } catch (err) {
    roles.value = [];
    permissions.value = [];
    errors.rbac = err instanceof Error ? err.message : t('rbac.error.loadRoles');
  }
}

async function loadOverview() {
  loading.value = true;

  try {
    await Promise.all([
      loadHealth(),
      loadTargets(),
      loadSessions(),
      loadDBInstances(),
      loadDBConnections(),
      loadRBACSummary()
    ]);
  } finally {
    loading.value = false;
  }
}

onMounted(loadOverview);
</script>

<style scoped>
.json-preview {
  overflow: auto;
  max-height: 260px;
  margin: 0;
  padding: 14px;
  color: #344054;
  background: #f9fafb;
  border: 1px solid #eaecf0;
  border-radius: 8px;
}
</style>
