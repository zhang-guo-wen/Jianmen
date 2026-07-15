<template>
  <div class="view-stack">
    <el-tabs v-model="activeTab" class="page-tabs">
      <el-tab-pane v-if="canConnectHost" label="主机" name="ssh">
        <DataTableCard
          :data="targets"
          :loading="sshLoading"
          :total="targetTotal"
          v-model:page="targetPage"
          v-model:page-size="targetPageSize"
          search-placeholder="搜索主机、账号..."
          @search="onSSHSearch"
        >
          <template #toolbar-extra>
            <el-button :loading="sshLoading" :icon="Refresh" @click="loadTargets">{{ t('common.refresh') }}</el-button>
          </template>
          <el-table-column :label="t('quickConnect.column.host')" min-width="190">
            <template #default="{ row }">{{ targetHost(row) || '-' }}:{{ targetPort(row) }}</template>
          </el-table-column>
          <el-table-column :label="t('quickConnect.column.account')" min-width="150">
            <template #default="{ row }">{{ accountName(row) || '-' }}</template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="90">
            <template #default="{ row }">
              <el-tag :type="row.status === 'disabled' ? 'info' : 'success'" size="small">{{ row.status === 'disabled' ? t('common.disabled') : t('common.enabled') }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" fixed="right" width="100">
            <template #default="{ row }">
              <el-button link type="primary" @click="openSSHConfig(row)">{{ t('quickConnect.action.connect') }}</el-button>
            </template>
          </el-table-column>
        </DataTableCard>
      </el-tab-pane>
      <el-tab-pane v-if="permission.canDo('db:connect')" label="数据库" name="db">
        <DataTableCard
          :data="displayedDBAccounts"
          :loading="dbLoading"
          :total="dbTotal"
          v-model:page="dbPage"
          v-model:page-size="dbPageSize"
          search-placeholder="搜索实例、账号..."
          @search="onDBSearch"
        >
          <template #toolbar-extra>
            <el-button :loading="dbLoading" :icon="Refresh" @click="loadDBAccounts">{{ t('common.refresh') }}</el-button>
          </template>
          <el-table-column :label="t('audit.column.instance')" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">{{ row._instance_name || '-' }}</template>
          </el-table-column>
          <el-table-column :label="t('audit.column.account')" min-width="130" show-overflow-tooltip>
            <template #default="{ row }">{{ row.username || '-' }}</template>
          </el-table-column>
          <el-table-column :label="t('audit.column.protocol')" width="100">
            <template #default="{ row }">{{ row._protocol || 'mysql' }}</template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="90">
            <template #default="{ row }">
              <el-tag :type="row.status === 'disabled' ? 'info' : 'success'" size="small">{{ row.status === 'disabled' ? t('common.disabled') : t('common.enabled') }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" fixed="right" width="100">
            <template #default="{ row }">
              <el-button link type="primary" @click="openDBConfig(row)">{{ t('quickConnect.action.connect') }}</el-button>
            </template>
          </el-table-column>
        </DataTableCard>
      </el-tab-pane>
    </el-tabs>

      <ConnectionConfigDialog
        v-model="hostConfigVisible"
        resource-type="host"
        :target="selectedSSHTarget"
        :resource-name="selectedSSHTarget ? quickHostName(selectedSSHTarget) : ''"
        :source-address="selectedSSHTarget ? `${targetHost(selectedSSHTarget)}:${targetPort(selectedSSHTarget)}` : ''"
        :source-account="String(selectedSSHTarget?.username || '')"
        :allow-ssh="permission.canDo('session:connect')"
        :allow-sftp="permission.canDo('sftp:connect')"
      />

      <ConnectionConfigDialog
        v-model="configVisible"
        resource-type="database"
        :target="selectedDBTarget"
        :resource-name="String(selectedDBTarget?._instance_name || '')"
        :source-address="selectedDBTarget ? `${selectedDBTarget._instance_address || ''}:${selectedDBTarget._instance_port || 3306}` : ''"
        :source-account="String(selectedDBTarget?.username || '')"
        :protocol="String(selectedDBTarget?._protocol || 'mysql')"
        :allow-ssh="false"
      />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { Refresh } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import DataTableCard from '@/components/DataTableCard.vue';
import ConnectionConfigDialog from '@/components/ConnectionConfigDialog.vue';
import { apiClient, type PageResponse, type TargetRecord, type DBAccountRecord } from '@/api/client';
import { useI18n } from '@/i18n';
import { usePermissionStore } from '@/stores/permission';

const { t } = useI18n();
const permission = usePermissionStore();
const canConnectHost = computed(() => permission.canDo('session:connect') || permission.canDo('sftp:connect'));
const activeTab = ref(canConnectHost.value ? 'ssh' : 'db');

// ── SSH state ──
const sshKeyword = ref('');
const sshLoading = ref(false);
const sshError = ref('');
const targets = ref<TargetRecord[]>([]);
const targetTotal = ref(0);
const targetPage = ref(1);
const targetPageSize = ref(20);

// ── DB state ──
const dbKeyword = ref('');
const dbLoading = ref(false);
const dbError = ref('');
const dbAccounts = ref<QuickDBTarget[]>([]);
const dbPage = ref(1);
const dbPageSize = ref(20);

// ── Dialog state ──
const hostConfigVisible = ref(false);
const selectedSSHTarget = ref<TargetRecord | null>(null);
const hostNames = ref<Record<string, string>>({});
const configVisible = ref(false);
type QuickDBTarget = DBAccountRecord & { _instance_name?: string; _protocol?: string; _instance_address?: string; _instance_port?: number };
const selectedDBTarget = ref<QuickDBTarget | null>(null);

function targetHost(t: TargetRecord): string { return String(t.host || t.address || ''); }
function targetPort(t: TargetRecord): number { return Number(t.port) || 22; }
function accountName(t: TargetRecord): string { return String(t.username || ''); }
function quickHostName(t: TargetRecord): string { return hostNames.value[String(t.host_id || '')] || targetHost(t) || '-'; }
async function loadTargets() {
  sshLoading.value = true;
  sshError.value = '';
  try {
    const res: PageResponse<TargetRecord> = await apiClient.getTargets({
      page: targetPage.value,
      page_size: targetPageSize.value,
      q: sshKeyword.value.trim() || undefined,
      connectable: true,
    });
    targets.value = res.items ?? [];
    targetTotal.value = res.total ?? 0;
    try {
      const hostPage = await apiClient.getHosts({ page: 1, page_size: 999 });
      hostNames.value = Object.fromEntries((hostPage.items ?? []).map(host => [String(host.id || ''), String(host.name || host.address || '')]));
    } catch {
      hostNames.value = {};
    }
  } catch (e: any) {
    sshError.value = e.message;
    ElMessage.error(e.message);
  } finally {
    sshLoading.value = false;
  }
}

function onSSHSearch(q: string) {
  sshKeyword.value = q;
  targetPage.value = 1;
  loadTargets();
}

function openSSHConfig(target: TargetRecord) {
  selectedSSHTarget.value = target;
  hostConfigVisible.value = true;
}

async function loadDBAccounts() {
  dbLoading.value = true;
  dbError.value = '';
  dbPage.value = 1;
  try {
    const instRes = await apiClient.getDBInstances({ page: 1, page_size: 999 });
    const insts = instRes.items ?? [];
    const all: any[] = [];
    for (const inst of insts) {
      if (inst.status === 'disabled') continue;
      const accRes = await apiClient.getDBAccounts(String(inst.id), { page: 1, page_size: 999, connectable: true });
      const items = accRes.items ?? [];
      for (const a of items) {
        a._instance_name = inst.name;
        a._protocol = inst.protocol || 'mysql';
        a._instance_address = inst.address;
        a._instance_port = inst.port;
        all.push(a);
      }
    }
    dbAccounts.value = all;
  } catch (e: any) {
    dbError.value = e.message;
    ElMessage.error(e.message);
  } finally {
    dbLoading.value = false;
  }
}

const dbFiltered = computed(() => {
  const q = dbKeyword.value.trim().toLowerCase();
  if (!q) return dbAccounts.value;
  return dbAccounts.value.filter(a =>
    [a._instance_name, a.username, a._protocol].some(v => String(v ?? '').toLowerCase().includes(q))
  );
});

const dbTotal = computed(() => dbFiltered.value.length);

const displayedDBAccounts = computed(() => {
  const start = (dbPage.value - 1) * dbPageSize.value;
  return dbFiltered.value.slice(start, start + dbPageSize.value);
});

function onDBSearch(q: string) {
  dbKeyword.value = q;
  dbPage.value = 1;
}

function openDBConfig(acc: QuickDBTarget) {
  selectedDBTarget.value = acc;
  configVisible.value = true;
}

watch([targetPage, targetPageSize], () => loadTargets());

watch(activeTab, (tab) => {
  if (tab === 'db' && permission.canDo('db:connect') && dbAccounts.value.length === 0) {
    loadDBAccounts();
  }
});

onMounted(() => {
  if (canConnectHost.value) loadTargets();
  else if (permission.canDo('db:connect')) loadDBAccounts();
});
</script>

<style scoped>
.page-tabs :deep(.el-tabs__header) {
  margin-bottom: 15px;
  padding: 0;
}

</style>
