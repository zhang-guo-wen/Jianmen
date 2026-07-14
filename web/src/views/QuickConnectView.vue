<template>
  <div class="view-stack">
    <el-tabs v-model="activeTab" class="page-tabs">
      <el-tab-pane label="SSH" name="ssh">
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
      <el-tab-pane label="数据库" name="db">
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

    <!-- SSH 连接弹窗 -->
    <el-dialog v-model="configVisible" :title="dialogTitle" class="form-dialog" destroy-on-close width="min(720px, calc(100vw - 32px))">
      <div v-if="connectInfo" class="connection-dialog">
        <el-alert
          v-if="sessionError"
          show-icon type="error" :closable="false" :title="sessionError"
        />
        <el-alert
          v-else
          show-icon type="info" :closable="false"
          :title="connectType === 'ssh' ? '输入堡垒机的登录密码，不是目标主机的密码' : '输入堡垒机的登录密码，不是目标数据库的密码'"
        />

        <div v-if="!sessionError" style="margin-bottom: 8px; display: flex; align-items: center; gap: 8px;">
          <span style="font-size: 13px; color: #667085;">连通性：</span>
          <el-tag v-if="connectionTesting" type="info" size="small">测试中...</el-tag>
          <template v-else-if="connectionTestResult !== null">
            <el-tag :type="connectionTestResult.ok ? 'success' : 'danger'" size="small">
              {{ connectionTestResult.ok ? '可达' : '不可达' }}
            </el-tag>
            <span v-if="connectionTestResult.latency_ms !== undefined" style="font-size: 12px; color: #667085;">
              延迟 {{ connectionTestResult.latency_ms }}ms
            </span>
            <span v-if="connectionTestResult.error" style="font-size: 12px; color: var(--el-color-danger);">
              {{ connectionTestResult.error }}
            </span>
          </template>
        </div>

        <div v-if="creatingSession" style="text-align: center; padding: 30px 0;">
          <el-icon class="is-loading" :size="28"><Loading /></el-icon>
          <p style="margin-top: 10px; color: #667085;">正在创建连接会话...</p>
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
          </el-descriptions>

          <div style="margin-top: 12px">
            <el-input
              v-model="bastionPassword"
              type="password"
              show-password
              placeholder="输入堡垒机登录密码（自动携带到客户端）"
              size="small"
            />
          </div>

          <div style="margin-top: 12px">
            <el-input :model-value="sshCommandText" readonly size="small">
              <template #append>
                <el-button @click="copyValue(sshCommandText)">复制{{ connectType === 'ssh' ? ' SSH ' : ' ' }}命令</el-button>
              </template>
            </el-input>
          </div>
        </template>
      </div>
      <template #footer>
        <el-dropdown v-if="connectType === 'ssh'" trigger="click" style="margin-right:8px">
          <el-button type="primary">
            本地 SSH 客户端打开<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu v-if="connectInfo" @click.prevent.stop>
              <a
                v-for="item in SSH_CLIENT_LIST"
                :key="item.command"
                :href="sshClientUrl"
                target="_self"
                style="display:block;padding:5px 16px;color:#303133;text-decoration:none;font-size:13px"
                @mouseenter="(e: MouseEvent) => (e.target as HTMLElement).style.backgroundColor = '#f5f7fa'"
                @mouseleave="(e: MouseEvent) => (e.target as HTMLElement).style.backgroundColor = ''"
                @click.stop="onSSHClientClick"
              >
                {{ item.label }}
              </a>
              <div style="height:1px;background:#e4e7ed;margin:4px 0"></div>
              <span
                style="display:block;padding:5px 16px;color:#409eff;text-decoration:none;font-size:13px;cursor:pointer"
                @mouseenter="(e: MouseEvent) => (e.target as HTMLElement).style.backgroundColor = '#f5f7fa'"
                @mouseleave="(e: MouseEvent) => (e.target as HTMLElement).style.backgroundColor = ''"
                @click="openInitClientDialog"
              >
                初始化客户端...
              </span>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button v-if="connectType === 'ssh'" type="primary" @click="openInBrowser">在浏览器中打开</el-button>
        <el-button style="margin-left:8px" @click="configVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 初始化客户端弹窗 -->
    <el-dialog v-model="initClientVisible" title="初始化本地 SSH 客户端" width="560px" destroy-on-close>
      <el-form label-width="80px">
        <el-form-item label="客户端">
          <el-select v-model="initClientType" style="width:100%">
            <el-option
              v-for="item in CLIENT_INIT_OPTIONS"
              :key="item.command"
              :label="item.label"
              :value="item.command"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="程序路径">
          <div style="display:flex;gap:8px;width:100%">
            <el-input v-model="initClientPath" :placeholder="initClientPlaceholder" style="flex:1" />
            <el-button @click="pickClientFile">浏览...</el-button>
          </div>
          <div v-if="initClientPathHint" style="font-size:11px;color:#909399;margin-top:4px">{{ initClientPathHint }}</div>
        </el-form-item>
      </el-form>
      <div v-if="initRegCommand" style="margin-top:12px">
        <div style="font-size:12px;color:#909399;margin-bottom:4px">
          复制以下命令，以<strong>管理员身份</strong>在 cmd 中执行：
        </div>
        <el-input v-model="initRegCommand" readonly type="textarea" :rows="3" style="font-family:monospace;font-size:12px" />
      </div>
      <template #footer>
        <el-button type="primary" @click="copyInitCommand">复制注册命令</el-button>
        <el-button @click="initClientVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { ArrowDown, Refresh } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import DataTableCard from '@/components/DataTableCard.vue';
import { apiClient, type PageResponse, type TargetRecord, type DBAccountRecord } from '@/api/client';
import { useI18n } from '@/i18n';

const SSH_CLIENT_LIST = [
  { command: 'default', label: '系统默认 (ssh://)' },
  { command: 'xshell', label: 'Xshell' },
  { command: 'putty', label: 'PuTTY' },
  { command: 'securecrt', label: 'SecureCRT' },
  { command: 'mobaxterm', label: 'MobaXterm' },
  { command: 'winterm', label: 'Windows Terminal' },
] as const;

// ── 初始化客户端弹窗 ──
interface ClientInitOption { command: string; label: string; defaultPath: string }
const CLIENT_INIT_OPTIONS: ClientInitOption[] = [
  { command: 'xshell', label: 'Xshell', defaultPath: 'C:\\Program Files (x86)\\NetSarang\\Xshell 7\\Xshell.exe' },
  { command: 'putty', label: 'PuTTY', defaultPath: 'C:\\Program Files\\PuTTY\\putty.exe' },
  { command: 'securecrt', label: 'SecureCRT', defaultPath: 'C:\\Program Files\\VanDyke Software\\SecureCRT\\SecureCRT.exe' },
  { command: 'mobaxterm', label: 'MobaXterm', defaultPath: 'C:\\Program Files (x86)\\Mobatek\\MobaXterm\\MobaXterm.exe' },
  { command: 'winterm', label: 'Windows Terminal', defaultPath: 'wt.exe' },
  { command: 'system', label: '系统 SSH (ssh.exe)', defaultPath: 'ssh.exe' },
];
const initClientVisible = ref(false);
const initClientType = ref('xshell');
const initClientPath = ref('');
const initClientPathHint = ref('');
const initRegCommand = ref('');

const initClientPlaceholder = computed(() => {
  const opt = CLIENT_INIT_OPTIONS.find(o => o.command === initClientType.value);
  return opt ? `默认: ${opt.defaultPath}` : '';
});

function openInitClientDialog() {
  initClientType.value = 'xshell';
  initClientPath.value = '';
  initRegCommand.value = '';
  initClientVisible.value = true;
}

watch([initClientType, initClientPath], () => {
  const opt = CLIENT_INIT_OPTIONS.find(o => o.command === initClientType.value);
  const exePath = initClientPath.value || opt?.defaultPath || '';
  if (!exePath) { initRegCommand.value = ''; return; }
  const escaped = exePath.replace(/\\/g, '\\\\');
  initRegCommand.value = `reg add "HKCR\\ssh" /ve /d "URL:SSH Protocol" /f && reg add "HKCR\\ssh" /v "URL Protocol" /d "" /f && reg add "HKCR\\ssh\\shell\\open\\command" /ve /d "\\"${escaped}\\" -url \\"%1\\"" /f`;
});

async function copyInitCommand() {
  const cmd = initRegCommand.value;
  if (!cmd) { ElMessage.warning('请先选择客户端'); return; }
  try {
    await navigator.clipboard.writeText(cmd);
    ElMessage.success('命令已复制，请在管理员 cmd 中粘贴执行');
  } catch {
    ElMessage.warning('复制失败，请手动复制');
  }
}

function pickClientFile() {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.exe';
  input.onchange = (e) => {
    const file = (e.target as HTMLInputElement).files?.[0];
    if (file) {
      const path = (file as any).path || file.name;
      initClientPath.value = path;
      if ((file as any).path) {
        initClientPathHint.value = '';
      } else {
        initClientPathHint.value = '浏览器不支持获取完整路径，请手动输入或复制路径';
      }
    }
  };
  input.click();
}

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
const bastionHost = ref(window.location.hostname);
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
const connectionTesting = ref(false);
const connectionTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null);
const bastionPassword = ref('');

const dialogTitle = computed(() => connectType.value === 'ssh' ? 'SSH 连接' : '数据库连接');

/** 对密码做 URL 编码，特殊字符需要转义 */
function encodePassword(pwd: string): string {
  return encodeURIComponent(pwd);
}

/** 当前 SSH 连接的 ssh:// 协议 URL（含密码） */
const sshClientUrl = computed(() => {
  const info = connectInfo.value;
  if (!info) return '#';
  const pwd = bastionPassword.value;
  if (pwd) {
    return `ssh://${info.compactUser}:${encodePassword(pwd)}@${info.host}:${info.port}`;
  }
  return `ssh://${info.compactUser}@${info.host}:${info.port}`;
});

/** 当前 SSH 命令行文本（含 sshpass） */
const sshCommandText = computed(() => {
  const info = connectInfo.value;
  if (!info || !info.compactUser) return '';
  const pwd = bastionPassword.value;
  if (pwd) {
    // Windows: sshpass 需要单独安装；提示用 ssh:// 方式
    return `ssh ${info.compactUser}@${info.host} -p ${info.port}`;
  }
  return `ssh ${info.compactUser}@${info.host} -p ${info.port}`;
});

/** 点击协议链接时：浏览器触发 ssh:// 协议打开本地客户端，同时复制命令行到剪贴板 */
function onSSHClientClick() {
  const info = connectInfo.value;
  if (!info) return;
  const pwd = bastionPassword.value;
  const command = pwd
    ? `sshpass -p '${pwd}' ssh ${info.compactUser}@${info.host} -p ${info.port}`
    : `ssh ${info.compactUser}@${info.host} -p ${info.port}`;
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(command).then(() => {
      ElMessage.success(t('quickConnect.message.copied'));
    }).catch(() => {});
  }
}

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
  sessionError.value = ''; connectionTestResult.value = null;
  bastionPassword.value = '';
  creatingSession.value = true; configVisible.value = true;
  testSSHConnection(target);
  try {
    const tid = String(target.id || target.resource_id || '');
    webTerminalTargetId.value = tid;
    const s = await apiClient.createUserSession(tid);
    const cu = s?.compact_username || '';
    connectInfo.value = {
      host: bastionHost.value || window.location.hostname,
      port: bastionPort.value || 47102,
      compactUser: cu,
      command: `ssh ${cu}@${bastionHost.value || window.location.hostname} -p ${bastionPort.value || 47102}`,
    };
  } catch (e: any) { sessionError.value = e.message; }
  finally { creatingSession.value = false; }
}

async function testSSHConnection(target: TargetRecord) {
  connectionTesting.value = true;
  connectionTestResult.value = null;
  try {
    const username = target.username || 'unknown';
    const result = await apiClient.testTargetConnection({
      id: String(target.id || target.resource_id || username),
      name: target.name || username,
      username,
      password: '',
      private_key_path: '',
      private_key_pem: '',
      passphrase: '',
      address: String(target.host || target.address || ''),
      port: Number(target.port) || 22,
      insecure_ignore_host_key: true,
      host_key_fingerprint: '',
      known_hosts_path: '',
    });
    connectionTestResult.value = {
      ok: result.ok,
      latency_ms: result.latency_ms,
      error: result.ok ? undefined : (result.error || result.message || '连接失败'),
    };
  } catch (err) {
    connectionTestResult.value = { ok: false, error: err instanceof Error ? err.message : '连接失败' };
  } finally {
    connectionTesting.value = false;
  }
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
  sessionError.value = ''; connectionTestResult.value = null;
  creatingSession.value = true; configVisible.value = true;
  testDBConnection(acc);
  try {
    const s = await apiClient.createUserSession(String(acc.id));
    const cu = s?.compact_username || '';
    const proto = acc._protocol || 'mysql';
    const host = bastionHost.value || window.location.hostname;
    const port = gatewayPort.value || 33060;
    const cmd = proto === 'mysql'
      ? `mysql --protocol=tcp -h ${host} -P ${port} -u ${cu} -p`
      : proto === 'redis'
        ? `redis-cli -h ${host} -p ${port} -a ${cu}`
        : `psql -h ${host} -p ${port} -U ${cu}`;
    connectInfo.value = { host, port, compactUser: cu, command: cmd };
  } catch (e: any) { sessionError.value = e.message; }
  finally { creatingSession.value = false; }
}

async function testDBConnection(acc: any) {
  connectionTesting.value = true;
  connectionTestResult.value = null;
  try {
    const id = String(acc.id || acc.resource_id || '');
    if (!id) return;
    const result = await apiClient.testDBConnection(id);
    connectionTestResult.value = {
      ok: result.ok,
      latency_ms: result.latency_ms,
      error: result.ok ? undefined : (result.error || '连接失败'),
    };
  } catch (err) {
    connectionTestResult.value = { ok: false, error: err instanceof Error ? err.message : '连接失败' };
  } finally {
    connectionTesting.value = false;
  }
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

watch(activeTab, (tab) => {
  if (tab === 'db' && dbAccounts.value.length === 0) {
    loadDBAccounts();
  }
});

onMounted(() => { loadTargets(); });
</script>

<style scoped>
.page-tabs :deep(.el-tabs__header) {
  margin-bottom: 15px;
  padding: 0;
}

.connection-dialog {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.form-dialog :deep(.el-dialog__footer .el-button + .el-button) {
  margin-left: 8px;
}
.form-dialog :deep(.el-dialog__footer .el-dropdown + .el-button) {
  margin-left: 8px;
}
.form-dialog :deep(.el-dialog__footer .el-button + .el-dropdown) {
  margin-left: 8px;
}
.form-dialog :deep(.el-dialog__footer .el-button:first-child) {
  margin-left: 0;
}
</style>
