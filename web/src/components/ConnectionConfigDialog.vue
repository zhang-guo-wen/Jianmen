<template>
  <el-dialog v-model="visible" destroy-on-close :title="dialogTitle" width="min(700px, calc(100vw - 24px))" class="connection-config-dialog">
    <div v-if="target" class="dialog-content">
      <section class="resource-summary">
        <div class="resource-icon">{{ resourceType === 'host' ? 'SSH' : protocolLabel }}</div>
        <div class="resource-main">
          <strong>{{ resourceName || '-' }}</strong>
        </div>
        <div class="source-meta">
          <div><span>源地址</span><code>{{ sourceAddress || '-' }}</code></div>
          <div><span>原账号</span><code>{{ sourceAccount || '-' }}</code></div>
        </div>
      </section>

      <el-alert v-if="connectionError" show-icon type="error" :closable="false" :title="connectionError" />

      <div v-if="!connectionError" class="connectivity-row">
        <span>连通性</span>
        <el-tag v-if="connectionTesting" type="info" size="small">测试中...</el-tag>
        <template v-else-if="connectionTestResult">
          <el-tag :type="connectionTestResult.ok ? 'success' : 'danger'" size="small">
            {{ connectionTestResult.ok ? '可达' : '不可达' }}
          </el-tag>
          <span v-if="connectionTestResult.latency_ms !== undefined">延迟 {{ connectionTestResult.latency_ms }}ms</span>
          <span v-if="connectionTestResult.error" class="connect-error">{{ connectionTestResult.error }}</span>
        </template>
      </div>

      <div v-if="creatingSession" class="loading-state">
        <el-icon class="is-loading" :size="30"><Loading /></el-icon>
        <p>正在生成连接配置...</p>
      </div>

      <template v-else-if="!connectionError && connectionInfo">
        <section class="connection-panel permanent-panel">
          <header>
            <strong>长期连接</strong>
            <el-tag type="primary" effect="plain">长期有效</el-tag>
          </header>
          <div class="detail-grid">
            <InfoValue label="连接地址" :value="gatewayAddress" @copy="copyValue" />
            <InfoValue label="连接账号" :value="connectionInfo.compactUser" @copy="copyValue" />
            <div class="detail-item password-tip">
              <span>登录密码</span>
              <strong>输入堡垒机的登录密码，不是目标{{ resourceType === 'host' ? '主机' : '数据库' }}的密码</strong>
            </div>
          </div>
          <CommandRows :commands="commands" @copy="copyValue" />
        </section>

        <section class="connection-panel temporary-panel">
          <header>
            <strong>临时连接</strong>
            <el-tag type="warning" effect="dark">一次性</el-tag>
          </header>
          <div class="detail-grid">
            <InfoValue label="连接地址" :value="gatewayAddress" @copy="copyValue" />
            <InfoValue label="连接账号" :value="connectionInfo.compactUser" @copy="copyValue" />
            <InfoValue label="临时密码" :value="temporaryPassword" accent @copy="copyValue" />
            <div class="detail-item expiry-item">
              <span>有效期</span>
              <strong>{{ temporaryPasswordExpiryText }}</strong>
            </div>
          </div>
          <CommandRows :commands="commands" temporary @copy="copyValue" />
        </section>
      </template>
    </div>

    <template #footer>
      <el-button v-if="resourceType === 'host' && allowSSH" type="primary" :loading="preferences.loading" @click="openPreferredSSHClient">本地 SSH 客户端打开</el-button>
      <el-button v-if="resourceType === 'host' && allowSSH" type="primary" @click="openInBrowser">在浏览器中打开</el-button>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="initClientVisible" title="初始化本地 SSH 客户端" width="560px" destroy-on-close>
    <el-form label-position="top">
      <el-form-item label="客户端" required>
        <el-select v-model="initClientType" style="width: 100%">
          <el-option v-for="item in configurableClients" :key="item.command" :label="item.label" :value="item.command" />
        </el-select>
      </el-form-item>
      <el-form-item label="程序路径" required :error="initClientPathError">
        <div class="path-field">
          <el-input v-model="initClientPath" placeholder="请输入完整绝对路径，如 C:\Program Files\PuTTY\putty.exe" />
          <el-button @click="pickClientFile">浏览...</el-button>
        </div>
        <div class="path-help">程序路径必填，不提供默认值，且必须是包含盘符的完整绝对路径。</div>
      </el-form-item>
    </el-form>
    <div v-if="initRegCommand" class="registration-command">
      <div>复制以下命令，以<strong>管理员身份</strong>在 CMD 中执行：</div>
      <el-input :model-value="initRegCommand" readonly type="textarea" :rows="3" />
    </div>
    <template #footer>
      <el-button type="primary" :disabled="!canSaveClient" @click="saveClientAndCopyCommand">保存并复制注册命令</el-button>
      <el-button @click="initClientVisible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { Loading } from '@element-plus/icons-vue';
import { ElButton, ElMessage } from 'element-plus';

import { apiClient, type DBAccountRecord, type TargetRecord } from '@/api/client';
import { buildSSHProtocolRegistrationCommand, isAbsoluteExecutablePath, SSH_CLIENT_OPTIONS } from '@/config/sshClients';
import { usePreferencesStore } from '@/stores/preferences';

interface CommandItem {
  label: string;
  value: string;
}

const InfoValue = defineComponent({
  props: { label: { type: String, required: true }, value: { type: String, required: true }, accent: Boolean },
  emits: ['copy'],
  setup(componentProps, { emit }) {
    return () => h('div', { class: ['detail-item', componentProps.accent ? 'accent-value' : ''] }, [
      h('span', componentProps.label),
      h('div', { class: 'value-line' }, [
        h('code', componentProps.value || '-'),
        h(ElButton, { link: true, type: 'primary', size: 'small', onClick: () => emit('copy', componentProps.value) }, () => '复制'),
      ]),
    ]);
  },
});

const CommandRows = defineComponent({
  props: { commands: { type: Array as () => CommandItem[], required: true }, temporary: Boolean },
  emits: ['copy'],
  setup(componentProps, { emit }) {
    return () => h('div', { class: 'command-list' }, componentProps.commands.map(command => h('div', { class: 'command-row' }, [
      h('div', [h('span', componentProps.temporary ? `临时${command.label}` : command.label), h('code', command.value)]),
      h(ElButton, { type: 'primary', plain: true, size: 'small', onClick: () => emit('copy', command.value) }, () => `复制${command.label}`),
    ])));
  },
});

const props = withDefaults(defineProps<{
  modelValue: boolean;
  resourceType: 'host' | 'database';
  target: TargetRecord | DBAccountRecord | null;
  resourceName?: string;
  sourceAddress?: string;
  sourceAccount?: string;
  protocol?: string;
  allowSSH?: boolean;
  allowSFTP?: boolean;
}>(), {
  resourceName: '', sourceAddress: '', sourceAccount: '', protocol: 'mysql', allowSSH: true, allowSFTP: false,
});

const emit = defineEmits<{ (event: 'update:modelValue', value: boolean): void }>();
const router = useRouter();
const preferences = usePreferencesStore();
const visible = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) });
const dialogTitle = computed(() => props.resourceType === 'host' ? '主机连接配置' : '数据库连接配置');
const protocolLabel = computed(() => props.protocol.toUpperCase());

const creatingSession = ref(false);
const connectionError = ref('');
const connectionTesting = ref(false);
const connectionTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null);
const connectionInfo = ref<{ host: string; port: number; compactUser: string } | null>(null);
const temporaryPassword = ref('');
const temporaryPasswordExpiresAt = ref('');

const gatewayAddress = computed(() => connectionInfo.value ? `${connectionInfo.value.host}:${connectionInfo.value.port}` : '');
const commands = computed<CommandItem[]>(() => {
  if (!connectionInfo.value) return [];
  const { host, port, compactUser } = connectionInfo.value;
  if (props.resourceType === 'host') {
    const values: CommandItem[] = [];
    if (props.allowSSH) values.push({ label: 'SSH 命令', value: `ssh ${compactUser}@${host} -p ${port}` });
    if (props.allowSFTP) values.push({ label: 'XFTP/SFTP 命令', value: `sftp -P ${port} ${compactUser}@${host}` });
    return values;
  }
  const protocol = props.protocol.toLowerCase();
  if (protocol === 'redis') return [{ label: '连接命令', value: `redis-cli -h ${host} -p ${port} --user ${compactUser} --askpass` }];
  if (protocol === 'postgres' || protocol === 'postgresql') return [{ label: '连接命令', value: `psql -h ${host} -p ${port} -U ${compactUser}` }];
  return [{ label: '连接命令', value: `mysql --protocol=tcp -h ${host} -P ${port} -u ${compactUser} -p` }];
});
const temporaryPasswordExpiryText = computed(() => {
  const formatted = formatExpiresAt(temporaryPasswordExpiresAt.value);
  return formatted ? `${formatted}（使用一次后失效）` : '使用一次后失效';
});
const sshClientUrl = computed(() => {
  if (!connectionInfo.value) return '#';
  const password = temporaryPassword.value ? `:${encodeURIComponent(temporaryPassword.value)}` : '';
  return `ssh://${connectionInfo.value.compactUser}${password}@${connectionInfo.value.host}:${connectionInfo.value.port}`;
});

const initClientVisible = ref(false);
const initClientType = ref('xshell');
const initClientPath = ref('');
const configurableClients = SSH_CLIENT_OPTIONS.filter(item => item.command !== 'default');
const initClientPathError = computed(() => {
  if (!initClientPath.value.trim()) return '请输入本地 SSH 客户端的程序路径';
  if (!isAbsoluteExecutablePath(initClientPath.value)) return '请输入完整的 Windows 绝对路径，例如 C:\\Program Files\\PuTTY\\putty.exe';
  return '';
});
const initRegCommand = computed(() => buildSSHProtocolRegistrationCommand(initClientType.value, initClientPath.value));
const canSaveClient = computed(() => Boolean(initClientType.value && !initClientPathError.value && initRegCommand.value));

watch(
  () => [props.modelValue, String(props.target?.id || props.target?.resource_id || ''), props.resourceType] as const,
  ([isVisible, targetID]) => { if (isVisible && targetID) initializeConnection(); },
);

async function initializeConnection() {
  if (!props.target) return;
  connectionError.value = '';
  connectionTestResult.value = null;
  connectionInfo.value = null;
  temporaryPassword.value = '';
  temporaryPasswordExpiresAt.value = '';
  creatingSession.value = true;
  testConnection();
  try {
    const targetID = String(props.target.id || props.target.resource_id || '');
    if (!targetID) throw new Error('无法获取目标资源ID');
    const requests: [ReturnType<typeof apiClient.createUserSession>, ReturnType<typeof apiClient.createConnectionPassword>, Promise<{ host?: string; port?: number } | null>] = [
      apiClient.createUserSession(targetID),
      apiClient.createConnectionPassword(targetID),
      props.resourceType === 'database' ? apiClient.getDBGateway() : Promise.resolve(null),
    ];
    const [session, credential, gateway] = await Promise.all(requests);
    connectionInfo.value = {
      host: gateway?.host || window.location.hostname,
      port: Number(gateway?.port) || (props.resourceType === 'host' ? 47102 : 33060),
      compactUser: session?.compact_username || '',
    };
    temporaryPassword.value = credential.password;
    temporaryPasswordExpiresAt.value = credential.expires_at;
  } catch (error) {
    connectionError.value = error instanceof Error ? error.message : '创建连接配置失败';
  } finally {
    creatingSession.value = false;
  }
}

async function testConnection() {
  if (!props.target) return;
  connectionTesting.value = true;
  try {
    if (props.resourceType === 'database') {
      const targetID = String(props.target.id || props.target.resource_id || '');
      const result = await apiClient.testDBConnection(targetID);
      connectionTestResult.value = { ok: result.ok, latency_ms: result.latency_ms, error: result.ok ? undefined : result.error || '连接失败' };
      return;
    }
    const target = props.target as TargetRecord;
    const username = String(target.username || 'unknown');
    const result = await apiClient.testTargetConnection({
      id: String(target.id || target.resource_id || username), name: String(target.name || username), username,
      password: '', private_key_path: '', private_key_pem: '', passphrase: '', address: String(target.host || target.address || ''),
      port: Number(target.port) || 22, insecure_ignore_host_key: true, host_key_fingerprint: '', known_hosts_path: '',
    });
    connectionTestResult.value = { ok: result.ok, latency_ms: result.latency_ms, error: result.ok ? undefined : result.error || result.message || '连接失败' };
  } catch (error) {
    connectionTestResult.value = { ok: false, error: error instanceof Error ? error.message : '连接失败' };
  } finally {
    connectionTesting.value = false;
  }
}

async function copyValue(value: string) {
  if (!value) return;
  try { await navigator.clipboard.writeText(value); ElMessage.success('已复制'); }
  catch { ElMessage.warning('复制失败，请手动复制'); }
}

async function openPreferredSSHClient() {
  if (!connectionInfo.value) return;
  if (!preferences.loaded) { try { await preferences.fetch(); } catch { /* initialization remains available */ } }
  if (!preferences.hasSSHClient) {
    initClientType.value = preferences.value.ssh_client && preferences.value.ssh_client !== 'default' ? preferences.value.ssh_client : 'xshell';
    initClientPath.value = preferences.value.ssh_client_path || '';
    initClientVisible.value = true;
    return;
  }
  window.location.href = sshClientUrl.value;
}

function openInBrowser() {
  const targetID = String(props.target?.id || props.target?.resource_id || '');
  if (!targetID) return;
  visible.value = false;
  router.push({ path: '/web-terminal', query: { target_id: targetID } });
}

function pickClientFile() {
  const input = document.createElement('input');
  input.type = 'file'; input.accept = '.exe';
  input.onchange = event => {
    const file = (event.target as HTMLInputElement).files?.[0] as (File & { path?: string }) | undefined;
    if (file) initClientPath.value = file.path || file.name;
  };
  input.click();
}

async function saveClientAndCopyCommand() {
  if (!canSaveClient.value) { ElMessage.warning(initClientPathError.value || '请完善客户端配置'); return; }
  try {
    await preferences.update({ ssh_client: initClientType.value, ssh_client_path: initClientPath.value.trim() });
    await navigator.clipboard.writeText(initRegCommand.value);
    ElMessage.success('配置已保存，注册命令已复制，请在管理员 CMD 中执行一次');
    initClientVisible.value = false;
  } catch { ElMessage.error(preferences.error || '客户端配置保存失败'); }
}

function formatExpiresAt(value: string): string {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false });
}
</script>

<style scoped>
.dialog-content { display: flex; flex-direction: column; gap: 10px; }
.resource-summary { display: grid; grid-template-columns: auto minmax(0, 1fr) minmax(260px, auto); gap: 12px; align-items: center; padding: 12px 14px; border: 1px solid var(--el-border-color-light); border-radius: 12px; background: linear-gradient(135deg, var(--el-fill-color-light), transparent); }
.resource-icon { display: grid; place-items: center; width: 44px; height: 44px; border-radius: 10px; background: var(--el-color-primary); color: white; font-size: 12px; font-weight: 800; letter-spacing: .06em; }
.resource-main { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.resource-main strong { overflow: hidden; font-size: 16px; text-overflow: ellipsis; white-space: nowrap; }
.source-meta { display: grid; gap: 6px; }
.source-meta > div { display: grid; grid-template-columns: 54px minmax(0, 1fr); gap: 8px; align-items: center; }
.source-meta span, .detail-item > span { color: var(--el-text-color-secondary); font-size: 12px; }
.source-meta code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.connectivity-row { display: flex; align-items: center; gap: 8px; color: var(--el-text-color-secondary); font-size: 13px; }
.connect-error { color: var(--el-color-danger); }
.loading-state { padding: 30px 0; text-align: center; }
.loading-state p { margin: 10px 0 0; color: var(--el-text-color-secondary); }
.connection-panel { overflow: hidden; border: 1px solid var(--el-border-color-light); border-radius: 12px; }
.connection-panel header { display: flex; justify-content: space-between; align-items: center; padding: 10px 14px; }
.connection-panel header strong { font-size: 15px; }
.permanent-panel header { background: var(--el-color-primary-light-9); }
.temporary-panel header { background: var(--el-color-warning-light-9); }
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1px; background: var(--el-border-color-lighter); border-top: 1px solid var(--el-border-color-lighter); }
.detail-item { display: flex; flex-direction: column; gap: 5px; min-width: 0; padding: 9px 14px; background: var(--el-bg-color); }
.detail-item code, .detail-item strong { overflow-wrap: anywhere; font-size: 13px; }
.value-line { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.accent-value code { color: var(--el-color-warning-dark-2); font-size: 14px; font-weight: 800; letter-spacing: .04em; }
.password-tip { grid-column: 1 / -1; }
.password-tip strong { color: var(--el-color-primary); }
.expiry-item strong { color: var(--el-color-warning-dark-2); }
.command-list { display: flex; flex-direction: column; gap: 6px; padding: 9px 14px 10px; border-top: 1px solid var(--el-border-color-lighter); background: var(--el-fill-color-extra-light); }
.command-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; }
.command-row > div { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.command-row span { color: var(--el-text-color-secondary); font-size: 11px; }
.command-row code { overflow-x: auto; white-space: nowrap; font-size: 12px; }
.path-field { display: flex; gap: 8px; width: 100%; }
.path-help { margin-top: 5px; color: var(--el-text-color-secondary); font-size: 12px; }
.registration-command { display: flex; flex-direction: column; gap: 6px; color: var(--el-text-color-secondary); font-size: 12px; }
@media (max-width: 680px) {
  .resource-summary { grid-template-columns: auto minmax(0, 1fr); }
  .source-meta { grid-column: 1 / -1; }
  .detail-grid { grid-template-columns: 1fr; }
  .password-tip { grid-column: auto; }
  .command-row { align-items: stretch; flex-direction: column; }
}
</style>
