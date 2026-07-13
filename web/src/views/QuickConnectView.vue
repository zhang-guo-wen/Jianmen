<template>
  <div class="view-stack">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="SSH" name="ssh" />
      <el-tab-pane label="数据库" name="db" />
    </el-tabs>

    <!-- SSH Tab -->
    <template v-if="activeTab === 'ssh'">
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
    </template>

    <!-- Database Tab -->
    <template v-if="activeTab === 'db'">
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
    </template>

    <!-- Connect Dialog -->
    <el-dialog v-model="configVisible" :title="dialogTitle" class="form-dialog" destroy-on-close width="480px">
      <div v-if="connectInfo" class="config-dialog">
        <el-alert v-if="sessionError" show-icon type="error" :closable="false" :title="sessionError" />
        <el-alert v-else show-icon type="info" :closable="false" title="输入堡垒机的登录密码，不是目标主机的密码" />

        <div v-if="creatingSession" style="text-align: center; padding: 30px 0;">
          <el-icon class="is-loading" :size="28"><Loading /></el-icon>
          <p style="margin-top: 10px; color: #667085;">{{ t('quickConnect.label.creatingSession') }}</p>
        </div>

        <template v-else-if="!sessionError && connectInfo.compactUser">
          <el-descriptions :column="1" border size="small" style="margin-top: 12px">
            <el-descriptions-item label="连接地址">
              <code>{{ connectInfo.host }}:{{ connectInfo.port }}</code>
              <el-button link type="primary" size="small" style="margin-left: 8px" @click="copyValue(`${connectInfo.host}:${connectInfo.port}`)">复制</el-button>
            </el-descriptions-item>
            <el-descriptions-item label="用户名">
              <code>{{ connectInfo.compactUser }}</code>
              <el-button link type="primary" size="small" style="margin-left: 8px" @click="copyValue(connectInfo.compactUser)">复制</el-button>
            </el-descriptions-item>
            <el-descriptions-item label="密码">堡垒机登录密码</el-descriptions-item>
          </el-descriptions>

          <div v-if="connectInfo?.compactUser" style="margin-top: 12px; text-align: right">
            <el-button type="primary" @click="openInBrowser">
              在浏览器中打开
            </el-button>
          </div>

          <div style="margin-top: 12px">
            <el-input :model-value="connectInfo.command" readonly size="small">
              <template #append>
                <el-button @click="copyValue(connectInfo.command)">复制</el-button>
              </template>
            </el-input>
          </div>
        </template>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { Refresh } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import DataTableCard from '@/components/DataTableCard.vue';
import { apiClient, type PageResponse, type TargetRecord, type DBAccountRecord } from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();
const router = useRouter();
const activeTab = ref('ssh');
const webTerminalTargetId = ref('');

// ── SSH state ──
const sshKeyword = ref('');
const sshLoading = ref(false);
const sshError = ref('');
const targets = ref<TargetRecord[]>([]);
const targetTotal = ref(0);
const targetPage = ref(1);
const targetPageSize = ref(20);
const bastionHost = ref('127.0.0.1');
const bastionPort = ref(47102);

// ── DB state ──
const dbKeyword = ref('');
const dbLoading = ref(false);
const dbError = ref('');
const dbAccounts = ref<(DBAccountRecord & { _instance_name?: string; _protocol?: string })[]>([]);
const dbPage = ref(1);
const dbPageSize = ref(20);
const gatewayPort = ref(33060);

// ── Dialog state ──
const configVisible = ref(false);
const creatingSession = ref(false);
const sessionError = ref('');
const connectInfo = ref<{ host: string; port: number; compactUser: string; command: string } | null>(null);
const connectType = ref<'ssh' | 'db'>('ssh');

const dialogTitle = computed(() => connectType.value === 'ssh' ? 'SSH 连接' : '数据库连接');

// ── SSH ──
function targetHost(t: TargetRecord): string { return String(t.host || t.address || ''); }
function targetPort(t: TargetRecord): number { return Number(t.port) || 22; }
function accountName(t: TargetRecord): string { return String(t.username || ''); }
async function loadTargets() {
  sshLoading.value = true;
  sshError.value = '';
  try {
    const res: PageResponse<TargetRecord> = await apiClient.getTargets({
      page: targetPage.value,
      page_size: targetPageSize.value,
      q: sshKeyword.value.trim() || undefined,
    });
    targets.value = res.items ?? [];
    targetTotal.value = res.total ?? 0;
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

async function openSSHConfig(target: TargetRecord) {
  connectType.value = 'ssh';
  sessionError.value = ''; creatingSession.value = true; configVisible.value = true;
  try {
    const tid = String(target.id || target.resource_id || '');
    webTerminalTargetId.value = tid;
    const s = await apiClient.createUserSession(tid);
    const cu = s?.compact_username || '';
    connectInfo.value = {
      host: bastionHost.value || '127.0.0.1',
      port: bastionPort.value || 47102,
      compactUser: cu,
      command: `ssh ${cu}@${bastionHost.value || '127.0.0.1'} -p ${bastionPort.value || 47102}`,
    };
  } catch (e: any) { sessionError.value = e.message; }
  finally { creatingSession.value = false; }
}

// ── DB ──
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
      const accRes = await apiClient.getDBAccounts(String(inst.id), { page: 1, page_size: 999 });
      const items = accRes.items ?? [];
      for (const a of items) {
        a._instance_name = inst.name;
        a._protocol = inst.protocol || 'mysql';
        a._instance_address = inst.address;
        all.push(a);
      }
    }
    dbAccounts.value = all;
    // Load gateway config
    try {
      const gw = await apiClient.getDBGateway();
      if (gw?.port) gatewayPort.value = Number(gw.port);
    } catch { /* ignore */ }
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

async function openDBConfig(acc: any) {
  connectType.value = 'db';
  sessionError.value = ''; creatingSession.value = true; configVisible.value = true;
  try {
    const s = await apiClient.createUserSession(String(acc.id));
    const cu = s?.compact_username || '';
    const proto = acc._protocol || 'mysql';
    const host = bastionHost.value || '127.0.0.1';
    const port = gatewayPort.value || 33060;
    const cmd = proto === 'mysql'
      ? `mysql --protocol=tcp -h ${host} -P ${port} -u ${cu} -p`
      : `psql -h ${host} -p ${port} -U ${cu}`;
    connectInfo.value = { host, port, compactUser: cu, command: cmd };
  } catch (e: any) { sessionError.value = e.message; }
  finally { creatingSession.value = false; }
}

// ── Common ──
async function copyValue(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    ElMessage.success(t('quickConnect.message.copied'));
  } catch { ElMessage.error(t('quickConnect.error.copy')); }
}

function openInBrowser() {
  if (!webTerminalTargetId.value) return;
  configVisible.value = false;
  router.push({ path: '/web-terminal', query: { target_id: webTerminalTargetId.value } });
}

// ── Watchers ──
watch([targetPage, targetPageSize], () => loadTargets());

// 切换到 DB tab 时自动加载数据
watch(activeTab, (tab) => {
  if (tab === 'db' && dbAccounts.value.length === 0) {
    loadDBAccounts();
  }
});

onMounted(() => { loadTargets(); });
</script>

<style scoped>
.config-dialog { min-height: 100px; }
</style>
