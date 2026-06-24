<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-input
        v-model="keyword"
        clearable
        :placeholder="t('quickConnect.placeholder.search')"
        style="max-width: 340px"
      />
      <div class="endpoint-toolbar">
        <el-input v-model="bastionUser" :placeholder="t('quickConnect.placeholder.bastionUser')">
          <template #prepend>{{ t('quickConnect.field.bastionUser') }}</template>
        </el-input>
        <el-input v-model="bastionHost" :placeholder="t('quickConnect.placeholder.bastionHost')">
          <template #prepend>{{ t('quickConnect.field.bastionHost') }}</template>
        </el-input>
        <el-input-number
          v-model="bastionPort"
          :max="65535"
          :min="1"
          controls-position="right"
          :placeholder="t('quickConnect.placeholder.bastionPort')"
        />
        <el-button :loading="loading" :icon="Refresh" @click="loadTargets">
          {{ t('common.refresh') }}
        </el-button>
      </div>
    </div>

    <el-card class="placeholder-panel" shadow="never">
      <el-alert v-if="error" :title="error" type="error" show-icon />
      <el-table v-else v-loading="loading" :data="filteredTargets" height="520" :row-key="targetRowKey">
        <el-table-column prop="id" :label="t('quickConnect.column.targetId')" min-width="160" />
        <el-table-column prop="name" :label="t('common.name')" min-width="160" show-overflow-tooltip />
        <el-table-column :label="t('quickConnect.column.host')" min-width="190">
          <template #default="{ row }">
            <strong>{{ targetHost(row) || t('common.none') }}</strong>
            <span class="muted-text">:{{ targetPort(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('quickConnect.column.account')" min-width="150">
          <template #default="{ row }">
            {{ accountName(row) || t('common.none') }}
          </template>
        </el-table-column>
        <el-table-column :label="t('quickConnect.column.resource')" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="mono-text">{{ resourceIdentifier(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="130">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row)">
              {{ row.status || t('common.enabled') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" fixed="right" width="130">
          <template #default="{ row }">
            <el-button :icon="Connection" link type="primary" @click="openConfig(row)">
              {{ t('quickConnect.action.connect') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !filteredTargets.length && !error" :description="t('quickConnect.empty')" />
    </el-card>

    <el-dialog v-model="configVisible" :title="dialogTitle" class="form-dialog" destroy-on-close width="760px">
      <div v-if="selectedTarget" class="config-dialog">
        <el-alert :title="t('quickConnect.authHint')" show-icon type="info" />
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('quickConnect.column.host')">
            {{ targetHostPort(selectedTarget) || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('quickConnect.column.account')">
            {{ accountName(selectedTarget) || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('quickConnect.column.targetId')">
            <span class="mono-text">{{ targetId(selectedTarget) || t('common.none') }}</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('quickConnect.column.hostResourceId')">
            <span class="mono-text">{{ hostResourceId(selectedTarget) || t('common.none') }}</span>
          </el-descriptions-item>
        </el-descriptions>

        <div class="config-list">
          <div v-for="item in connectionItems" :key="item.key" class="config-row">
            <div class="config-label">{{ t(item.labelKey) }}</div>
            <el-input :model-value="item.value" readonly>
              <template #append>
                <el-tooltip :content="t('quickConnect.action.copy')">
                  <el-button
                    :aria-label="t('quickConnect.action.copy')"
                    :icon="CopyDocument"
                    @click="copyValue(item.value)"
                  />
                </el-tooltip>
              </template>
            </el-input>
          </div>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { Connection, CopyDocument, Refresh } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';

import { apiClient, type ApiEnvelope, type TargetRecord } from '@/api/client';
import { useI18n, type TranslationKey } from '@/i18n';

interface ConnectionItem {
  key: string;
  labelKey: TranslationKey;
  value: string;
}

const { t } = useI18n();
const keyword = ref('');
const loading = ref(false);
const error = ref('');
const targets = ref<TargetRecord[]>([]);
const selectedTarget = ref<TargetRecord | null>(null);
const configVisible = ref(false);
const bastionUser = ref('admin');
const bastionHost = ref('127.0.0.1');
const bastionPort = ref(47102);

const filteredTargets = computed(() => {
  const query = keyword.value.trim().toLowerCase();

  if (!query) {
    return targets.value;
  }

  return targets.value.filter((target) =>
    [
      targetId(target),
      target.name,
      targetHost(target),
      targetPort(target),
      targetHostPort(target),
      accountName(target),
      resourceIdentifier(target),
      hostResourceId(target),
      target.status,
      target.source
    ].some((value) => String(value ?? '').toLowerCase().includes(query))
  );
});

const dialogTitle = computed(() => {
  const target = selectedTarget.value;

  if (!target) {
    return t('quickConnect.dialog.title');
  }

  const account = accountName(target) || t('common.none');
  const host = targetHostPort(target) || t('common.none');

  return `${t('quickConnect.dialog.title')} - ${account}@${host}`;
});

const connectionItems = computed<ConnectionItem[]>(() => {
  const target = selectedTarget.value;

  if (!target) {
    return [];
  }

  const id = targetId(target);
  const loginUser = bastionUser.value.trim() || 'admin';
  const principal = `${loginUser}+${id}@${bastionHost.value.trim() || '127.0.0.1'}`;
  const port = Number(bastionPort.value) || 47102;
  const webTerminalPath = `/web-terminal?target_id=${encodeURIComponent(id)}`;

  return [
    {
      key: 'ssh',
      labelKey: 'quickConnect.config.ssh',
      value: `ssh ${principal} -p ${port}`
    },
    {
      key: 'sftp',
      labelKey: 'quickConnect.config.sftp',
      value: `sftp -P ${port} ${principal}`
    },
    {
      key: 'webTerminal',
      labelKey: 'quickConnect.config.webTerminal',
      value: webTerminalPath
    },
    {
      key: 'resource',
      labelKey: 'quickConnect.config.resource',
      value: resourceIdentifier(target)
    }
  ];
});

function unwrapTargets(payload: ApiEnvelope<TargetRecord[]> | TargetRecord[]): TargetRecord[] {
  return Array.isArray(payload) ? payload : payload.data ?? [];
}

function stringFrom(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number' ? String(value) : '';
}

function numberFrom(value: unknown, fallback: number): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value;
  }

  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function targetId(target: TargetRecord): string {
  return stringFrom(target.id) || stringFrom(target.resource_id);
}

function targetHost(target: TargetRecord): string {
  return stringFrom(target.host) || stringFrom(target.address) || stringFrom(target.hostname);
}

function targetPort(target: TargetRecord): number {
  return numberFrom(target.port, 22);
}

function targetHostPort(target: TargetRecord): string {
  const host = targetHost(target);

  return host ? `${host}:${targetPort(target)}` : '';
}

function accountName(target: TargetRecord): string {
  return stringFrom(target.username) || stringFrom(target.account) || stringFrom(target.user);
}

function hostResourceId(target: TargetRecord): string {
  return stringFrom(target.host_resource_id);
}

function resourceIdentifier(target: TargetRecord): string {
  const resourceType = stringFrom(target.resource_type) || 'host_account';
  const resourceId = stringFrom(target.resource_id) || targetId(target);

  return resourceId ? `${resourceType}:${resourceId}` : resourceType;
}

function targetRowKey(target: TargetRecord): string {
  return targetId(target) || `${targetHostPort(target)}:${accountName(target)}`;
}

function statusTagType(target: TargetRecord): 'success' | 'info' | 'warning' {
  const status = String(target.status ?? '').toLowerCase();

  if (status === 'disabled' || status === 'inactive') {
    return 'info';
  }

  return status === 'pending' ? 'warning' : 'success';
}

function openConfig(target: TargetRecord) {
  selectedTarget.value = target;
  configVisible.value = true;
}

async function copyValue(value: string) {
  try {
    if (!navigator.clipboard?.writeText) {
      throw new Error('clipboard unavailable');
    }

    await navigator.clipboard.writeText(value);
    ElMessage.success(t('quickConnect.message.copied'));
  } catch {
    ElMessage.error(t('quickConnect.error.copy'));
  }
}

async function loadTargets() {
  loading.value = true;
  error.value = '';

  try {
    targets.value = unwrapTargets(await apiClient.getTargets());
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('quickConnect.error.loadTargets');
  } finally {
    loading.value = false;
  }
}

onMounted(loadTargets);
</script>

<style scoped>
.endpoint-toolbar {
  display: flex;
  flex: 1;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 10px;
  min-width: 280px;
}

.endpoint-toolbar .el-input {
  width: min(260px, 100%);
}

.endpoint-toolbar .el-input-number {
  width: 152px;
}

.muted-text {
  color: #667085;
}

.mono-text {
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", monospace;
  font-size: 12px;
}

.config-dialog {
  display: grid;
  gap: 18px;
}

.config-list {
  display: grid;
  gap: 14px;
}

.config-row {
  display: grid;
  gap: 8px;
}

.config-label {
  color: #344054;
  font-size: 13px;
  font-weight: 650;
}

:global(.form-dialog .el-dialog__body) {
  max-height: min(66vh, 620px);
  overflow-y: auto;
  padding-right: 22px;
}

@media (max-width: 720px) {
  .endpoint-toolbar {
    justify-content: flex-start;
  }

  .endpoint-toolbar .el-input,
  .endpoint-toolbar .el-input-number {
    width: 100%;
  }
}
</style>
