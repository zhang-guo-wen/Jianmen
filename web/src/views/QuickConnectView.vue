<template>
  <div class="view-stack">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="SSH" name="ssh" />
      <el-tab-pane label="数据库" name="db" />
    </el-tabs>

    <!-- SSH Tab -->
    <template v-if="activeTab === 'ssh'">
      <div class="toolbar">
        <el-input v-model="keyword" clearable :placeholder="t('quickConnect.placeholder.search')" style="max-width: 340px" />
        <div class="endpoint-toolbar">
          <el-input v-model="bastionUser" :placeholder="t('quickConnect.placeholder.bastionUser')">
            <template #prepend>{{ t('quickConnect.field.bastionUser') }}</template>
          </el-input>
          <el-input v-model="bastionHost" :placeholder="t('quickConnect.placeholder.bastionHost')">
            <template #prepend>{{ t('quickConnect.field.bastionHost') }}</template>
          </el-input>
          <el-input-number v-model="bastionPort" :max="65535" :min="1" controls-position="right" />
          <el-button :loading="sshLoading" :icon="Refresh" @click="loadTargets">{{ t('common.refresh') }}</el-button>
        </div>
      </div>

      <el-card class="placeholder-panel" shadow="never">
        <el-alert v-if="sshError" :title="sshError" type="error" show-icon />
        <el-table v-else v-loading="sshLoading" :data="filteredTargets" height="520" :row-key="targetRowKey">
          <el-table-column :label="t('quickConnect.column.host')" min-width="190">
            <template #default="{ row }">{{ targetHost(row) || '-' }}:{{ targetPort(row) }}</template>
          </el-table-column>
          <el-table-column :label="t('quickConnect.column.account')" min-width="150">
            <template #default="{ row }">{{ accountName(row) || '-' }}</template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="90">
            <template #default="{ row }">
              <el-tag :type="statusTagType(row)" size="small">{{ row.status || t('common.enabled') }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" fixed="right" width="100">
            <template #default="{ row }">
              <el-button link type="primary" @click="openSSHConfig(row)">{{ t('quickConnect.action.connect') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!sshLoading && !filteredTargets.length && !sshError" :description="t('quickConnect.empty')" />
      </el-card>
    </template>

    <!-- Database Tab -->
    <template v-if="activeTab === 'db'">
      <div class="toolbar">
        <el-input v-model="dbKeyword" clearable :placeholder="t('quickConnect.placeholder.search')" style="max-width: 340px" />
        <div class="endpoint-toolbar">
          <el-button :loading="dbLoading" :icon="Refresh" @click="loadDBAccounts">{{ t('common.refresh') }}</el-button>
        </div>
      </div>

      <el-card class="placeholder-panel" shadow="never">
        <el-alert v-if="dbError" :title="dbError" type="error" show-icon />
        <el-table v-else v-loading="dbLoading" :data="filteredDBAccounts" height="520" row-key="id">
          <el-table-column :label="t('audit.column.instance')" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">{{ row._instance_name || '-' }}</template>
          </el-table-column>
          <el-table-column :label="t('audit.column.account')" min-width="130" show-overflow-tooltip>
            <template #default="{ row }">{{ row.upstream_username || '-' }}</template>
          </el-table-column>
          <el-table-column :label="t('audit.column.protocol')" width="100">
            <template #default="{ row }">{{ row._protocol || 'mysql' }}</template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="90">
            <template #default="{ row }">
              <el-tag :type="row.disabled ? 'info' : 'success'" size="small">{{ row.disabled ? t('common.disabled') : t('common.enabled') }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" fixed="right" width="100">
            <template #default="{ row }">
              <el-button link type="primary" @click="openDBConfig(row)">{{ t('quickConnect.action.connect') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!dbLoading && !filteredDBAccounts.length && !dbError" :description="t('database.empty.accounts')" />
      </el-card>
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
import { computed, onMounted, ref } from 'vue';
import { Refresh } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import { apiClient, type TargetRecord, type DBAccountRecord } from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();
const activeTab = ref('ssh');

// SSH state
const keyword = ref('');
const sshLoading = ref(false);
const sshError = ref('');
const targets = ref<TargetRecord[]>([]);
const bastionUser = ref('admin');
const bastionHost = ref('127.0.0.1');
const bastionPort = ref(47102);

// DB state
const dbKeyword = ref('');
const dbLoading = ref(false);
const dbError = ref('');
const dbAccounts = ref<(DBAccountRecord & { _instance_name?: string; _protocol?: string })[]>([]);
const gatewayPort = ref(33060);

// Dialog state
const configVisible = ref(false);
const creatingSession = ref(false);
const sessionError = ref('');
const connectInfo = ref<{ host: string; port: number; compactUser: string; command: string } | null>(null);
const connectType = ref<'ssh' | 'db'>('ssh');

const dialogTitle = computed(() => connectType.value === 'ssh' ? 'SSH 连接' : '数据库连接');

// --- SSH ---
const filteredTargets = computed(() => {
  const query = keyword.value.trim().toLowerCase();
  if (!query) return targets.value;
  return targets.value.filter(t =>
    [t.id, t.name, t.host, t.username].some(v => String(v ?? '').toLowerCase().includes(query))
  );
});

function targetHost(t: TargetRecord): string { return String(t.host || t.address || ''); }
function targetPort(t: TargetRecord): number { return Number(t.port) || 22; }
function accountName(t: TargetRecord): string { return String(t.username || ''); }
function targetRowKey(t: TargetRecord): string { return String(t.id || `${targetHost(t)}:${targetPort(t)}`); }
function statusTagType(t: TargetRecord): 'success' | 'info' { return t.status === 'disabled' ? 'info' : 'success'; }

async function loadTargets() {
  sshLoading.value = true; sshError.value = '';
  try { targets.value = (await apiClient.getTargets() as any)?.data ?? await apiClient.getTargets() as any ?? []; }
  catch (e: any) { sshError.value = e.message; }
  finally { sshLoading.value = false; }
}

async function openSSHConfig(target: TargetRecord) {
  connectType.value = 'ssh';
  sessionError.value = ''; creatingSession.value = true; configVisible.value = true;
  try {
    const tid = String(target.id || target.resource_id || '');
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

// --- DB ---
const filteredDBAccounts = computed(() => {
  const q = dbKeyword.value.trim().toLowerCase();
  if (!q) return dbAccounts.value;
  return dbAccounts.value.filter(a =>
    [a._instance_name, a.upstream_username, a._protocol].some(v => String(v ?? '').toLowerCase().includes(q))
  );
});

async function loadDBAccounts() {
  dbLoading.value = true; dbError.value = '';
  try {
    const insts = (await apiClient.getDBInstances() as any)?.data ?? await apiClient.getDBInstances() as any ?? [];
    const all: any[] = [];
    for (const inst of insts) {
      if (inst.disabled) continue;
      const accs = (await apiClient.getDBAccounts(String(inst.id))) as any;
      const items = accs?.items ?? accs?.data ?? accs ?? [];
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
      const gw = await apiClient.getDBGateway() as any;
      if (gw?.port) gatewayPort.value = Number(gw.port);
    } catch {}
  } catch (e: any) { dbError.value = e.message; }
  finally { dbLoading.value = false; }
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

// --- Common ---
async function copyValue(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    ElMessage.success(t('quickConnect.message.copied'));
  } catch { ElMessage.error(t('quickConnect.error.copy')); }
}

onMounted(() => { loadTargets(); });
</script>

<style scoped>
.endpoint-toolbar { display: flex; flex: 1; flex-wrap: wrap; justify-content: flex-end; gap: 10px; }
.config-dialog { min-height: 100px; }
</style>
