<template>
  <el-dialog
    v-model="visible"
    destroy-on-close
    :close-on-click-modal="false"
    :title="dialogTitle"
    width="min(700px, calc(100vw - 24px))"
    class="connection-config-dialog"
  >
    <div v-if="target" class="dialog-content">
      <section class="resource-summary">
        <div class="resource-icon">{{ resourceType === 'host' ? 'SSH' : protocolLabel }}</div>
        <div class="resource-main">
          <strong>{{ resourceName || '-' }}</strong>
        </div>
        <div class="source-meta">
          <div><span>地址</span><code>{{ sourceAddress || '-' }}</code></div>
          <div><span>账号</span><code>{{ sourceAccount || '-' }}</code></div>
        </div>
      </section>

      <el-alert v-if="connectionError" show-icon type="error" :closable="false" :title="connectionError" />

      <div v-if="!connectionError && !isRedis" class="connectivity-row">
        <span>连通性</span>
        <el-tag v-if="connectionTesting" type="info" size="small">测试中…</el-tag>
        <template v-else-if="connectionTestResult">
          <el-tag :type="connectionTestResult.ok ? 'success' : 'danger'" size="small">
            {{ connectionTestResult.ok ? '可达' : '不可达' }}
          </el-tag>
          <span v-if="connectionTestResult.latency_ms !== undefined">延迟 {{ connectionTestResult.latency_ms }}ms</span>
          <span v-if="connectionTestResult.error" class="connect-error">{{ connectionTestResult.error }}</span>
        </template>
      </div>
      <section v-if="connectionInfo && gatewayAddress" class="shared-connection-panel">
        <div class="detail-grid">
          <InfoValue label="连接地址" :value="gatewayAddress" :loading="isCopyInFlight(gatewayAddress, '连接地址')" @copy="copyValue" />
          <InfoValue label="连接账户" :value="connectionInfo.compactUser" :loading="isCopyInFlight(connectionInfo.compactUser, '连接账户')" @copy="copyValue" />
        </div>
        <div v-if="resourceType === 'database'" class="database-tls-row">
          <div>
            <strong>客户端 TLS</strong>
            <span>{{ databaseTLSRequired ? '系统已强制使用 TLS' : '默认不使用 TLS，可手动开启' }}</span>
          </div>
          <el-switch
            v-model="databaseUseTLS"
            :disabled="databaseTLSRequired"
            inline-prompt
            active-text="TLS"
            inactive-text="无 TLS"
          />
        </div>
        <el-alert
          v-if="resourceType === 'database' && databaseCommandUnavailableReason"
          type="warning"
          :closable="false"
          :title="databaseCommandUnavailableReason"
        />
        <el-alert
          v-if="resourceType === 'database' && databaseUseTLS && !secureGatewayTLS"
          type="error"
          :closable="false"
          title="TLS 身份材料不完整，无法生成安全连接命令。请联系管理员配置证书、ca_file 和 server_name。"
        />
        <CommandRows :commands="commands" :loading-for="isCopyInFlight" @copy="copyValue" />
      </section>

      <div v-if="creatingSession" class="loading-state">
        <el-icon class="is-loading" :size="30"><Loading /></el-icon>
        <p>正在生成连接配置…</p>
      </div>

      <template v-else-if="!connectionError && connectionInfo">
        <section class="connection-panel permanent-panel">
          <header>
            <strong>长期连接</strong>
            <el-tag type="primary" effect="plain">长期有效</el-tag>
          </header>
          <div class="detail-grid">
            <div class="detail-item password-tip">
              <span class="password-label">登录密码</span>
              <div class="password-hint">输入堡垒机的登录密码，不是目标{{ resourceType === 'host' ? '主机' : '数据库' }}的密码</div>
            </div>
          </div>
        </section>

        <section class="connection-panel temporary-panel">
          <header>
            <strong>临时连接</strong>
            <div class="expiry-summary"><span>密码有效期</span><strong>{{ temporaryPasswordExpiryText }}</strong></div>
          </header>
          <div class="detail-grid">
            <InfoValue label="临时密码" :value="temporaryPassword" :loading="isCopyInFlight(temporaryPassword, '临时密码')" accent @copy="copyValue" />
          </div>
        </section>
      </template>
    </div>

    <template #footer>
      <el-button data-testid="ssh-local-client" v-if="resourceType === 'host' && allowSsh" type="primary" :disabled="!connectionTestResult?.ok || sshIdentityBlocked" :loading="preferences.loading" @click="openPreferredSSHClient">本地 SSH 客户端打开</el-button>
      <el-button data-testid="ssh-browser" v-if="resourceType === 'host' && allowSsh" type="primary" :disabled="!connectionTestResult?.ok || sshIdentityBlocked" @click="openInBrowser">在浏览器中打开</el-button>
      <el-button
        v-if="resourceType === 'database' && !isRedis"
        data-testid="database-local-client"
        type="primary"
        :disabled="databaseClientLaunchBlocked"
        @click="openDatabaseClient"
      >
        本地客户端打开
      </el-button>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="initClientVisible"
    title="初始化本地 SSH 客户端"
    width="min(560px, calc(100vw - 24px))"
    class="local-client-dialog"
    destroy-on-close
  >
    <el-form label-position="top">
      <el-form-item label="客户端" required>
        <el-select v-model="initClientType" style="width: 100%">
          <el-option v-for="item in configurableClients" :key="item.command" :label="item.label" :value="item.command" />
        </el-select>
      </el-form-item>
      <el-form-item label="程序路径" required :error="initClientPathError">
        <div class="path-field">
          <el-input
            v-model="initClientPath"
            name="ssh_client_path"
            autocomplete="off"
            placeholder="例如 C:\Program Files\PuTTY\putty.exe"
          />
          <el-button @click="pickClientFile">浏览…</el-button>
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
import { computed, defineComponent, h, reactive, ref, watch, type PropType } from 'vue';
import { useRouter } from 'vue-router';
import { Loading } from '@element-plus/icons-vue';
import { ElButton, ElInput, ElMessage, ElMessageBox, ElSwitch } from 'element-plus';

import { apiClient, type DBAccountRecord, type DBGatewayConfig, type TargetRecord } from '@/api/client';
import { buildDatabaseProtocolURL } from '@/config/databaseClients';
import { buildSSHProtocolRegistrationCommand, isAbsoluteExecutablePath, SSH_CLIENT_OPTIONS } from '@/config/sshClients';
import { useDatabaseClientStore } from '@/stores/databaseClient';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';
import { databaseGatewayConnectionError } from '@/utils/databaseGatewayAvailability';
import {
  loadDatabaseConnectionResources,
} from '@/utils/databaseConnectionOrchestration';
import {
  buildDatabaseGatewayConnection,
  hasDatabaseGatewayTLSIdentity,
  resolveDatabaseGatewayClientHost,
  resolveDatabaseGatewayPort,
} from '@/utils/databaseGatewayCommands';
import {
  beginInFlightIfIdle,
  createLatestKeyedRequest,
  endInFlight,
  isInFlight,
  type InFlightCounters,
} from '@/utils/connectionRequestState';
import { buildSSHDeepLink } from '@/utils/connectionLinks';
import { buildConnectionCommands, type ConnectionCommandInput } from '@/utils/connectionConfigCommands';
import {
  parseSSHHostIdentityIssue,
  sshHostIdentityNotice,
} from '@/utils/sshHostIdentity';

interface CommandItem {
  label: string;
  value: string;
}

interface ConnectionTargetSnapshot {
  key: string;
  resourceType: 'host' | 'database';
  protocol: string;
  target: TargetRecord | DBAccountRecord;
}

type ConnectionResourceBundle = {
  session: Awaited<ReturnType<typeof apiClient.createUserSession>> | null;
  credential: Awaited<ReturnType<typeof apiClient.createConnectionPassword>> | null;
  gateway: DBGatewayConfig | null;
};

const InfoValue = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: String, required: true },
    accent: Boolean,
    loading: Boolean,
  },
  emits: ['copy'],
  setup(componentProps, { emit }) {
    return () => h('div', { class: ['detail-item', componentProps.accent ? 'accent-value' : ''] }, [
      h('span', componentProps.label),
      h('code', { class: 'detail-value' }, componentProps.value || '-'),
      h(ElButton, {
        class: 'copy-action',
        link: true,
        type: 'primary',
        size: 'small',
        loading: componentProps.loading,
        'aria-label': `复制${componentProps.label}`,
        onClick: () => emit('copy', componentProps.value, componentProps.label),
      }, () => '复制'),
    ]);
  },
});

const CommandRows = defineComponent({
  props: {
    commands: { type: Array as () => CommandItem[], required: true },
    loadingFor: { type: Function as PropType<(value: string, operation?: string) => boolean>, required: true },
  },
  emits: ['copy'],
  setup(componentProps, { emit }) {
    return () => h('div', { class: 'command-list' }, componentProps.commands.map(command => h('div', {
      class: 'command-row',
    }, [
      h('span', { class: 'command-row__heading' }, command.label),
      h(ElInput, {
        class: 'command-input',
        modelValue: command.value,
        readonly: true,
        size: 'small',
        'aria-label': command.label,
      }, {
        append: () => h(ElButton, {
          'data-testid': `connection-command-${command.label.includes('SFTP') ? 'sftp' : 'ssh'}`,
          'aria-label': `复制${command.label}`,
          loading: componentProps.loadingFor(command.value, command.label),
          onClick: () => emit('copy', command.value, command.label),
        }, () => '复制'),
      }),
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
  allowSsh?: boolean;
  allowSftp?: boolean;
}>(), {
  resourceName: '', sourceAddress: '', sourceAccount: '', protocol: 'mysql', allowSsh: true, allowSftp: false,
});

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'hostIdentityChanged', hostId: string): void
}>();
const router = useRouter();
const preferences = usePreferencesStore();
const databaseClient = useDatabaseClientStore();
const visible = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) });
const dialogTitle = computed(() => props.resourceType === 'host' ? '主机连接配置' : '数据库连接配置');
const protocolLabel = computed(() => props.protocol.toUpperCase());

const creatingSession = ref(false);
const connectionError = ref('');
const connectionTesting = ref(false);
const connectionTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null);
const sshIdentityBlocked = ref(false);
const connectionInfo = ref<{
  host: string;
  port: number;
  compactUser: string;
  tlsEnabled: boolean;
  clientTLSMode: 'required' | 'optional';
  tlsServerName: string;
  tlsCAPEM: string;
  tlsCertSHA256: string;
} | null>(null);
const temporaryPassword = ref('');
const temporaryPasswordExpiresAt = ref('');
const databaseUseTLS = ref(false);
const initializeRequest = createLatestKeyedRequest<ConnectionResourceBundle>();
const testRequest = createLatestKeyedRequest<{ ok: boolean; error?: string; latency_ms?: number }>();
const operationCounters = reactive<InFlightCounters>({});

const databaseConnectionHost = computed(() => {
  const info = connectionInfo.value;
  if (!info) return '';
  return databaseUseTLS.value && info.tlsServerName ? info.tlsServerName : info.host;
});
const gatewayAddress = computed(() => (
  connectionInfo.value
    ? `${props.resourceType === 'database' ? databaseConnectionHost.value : connectionInfo.value.host}:${connectionInfo.value.port}`
    : ''
));
const isRedis = computed(() => (
  props.resourceType === 'database' &&
  props.protocol.trim().toLowerCase() === 'redis'
));
const databaseTLSRequired = computed(() => (
  connectionInfo.value?.clientTLSMode === 'required'
));
const secureGatewayTLS = computed(() => hasDatabaseGatewayTLSIdentity({
  enabled: true,
  tls_enabled: connectionInfo.value?.tlsEnabled ?? false,
  tls_server_name: connectionInfo.value?.tlsServerName,
  tls_ca_pem: connectionInfo.value?.tlsCAPEM,
  tls_cert_sha256: connectionInfo.value?.tlsCertSHA256,
}));
const databaseConnectionPlan = computed(() => {
  if (props.resourceType !== 'database' || !connectionInfo.value) return null;
  const {
    port,
    compactUser,
    tlsEnabled,
    tlsServerName,
    tlsCAPEM,
    tlsCertSHA256,
  } = connectionInfo.value;
  const gateway = {
    enabled: true,
    host: databaseConnectionHost.value,
    client_tls_mode: connectionInfo.value.clientTLSMode,
    tls_enabled: tlsEnabled,
    tls_server_name: tlsServerName,
    tls_ca_pem: tlsCAPEM,
    tls_cert_sha256: tlsCertSHA256,
  };
  return buildDatabaseGatewayConnection({
    protocol: props.protocol,
    gateway,
    port,
    username: compactUser,
    useTLS: databaseUseTLS.value,
  });
});
const databaseCommandUnavailableReason = computed(() => {
  if (databaseConnectionPlan.value?.unavailableReason) return databaseConnectionPlan.value.unavailableReason;
  if (!databaseConnectionPlan.value) {
    return databaseUseTLS.value && !secureGatewayTLS.value
      ? 'TLS 身份材料不完整，无法生成安全连接命令。'
      : '连接参数包含不安全字符，无法生成连接命令。';
  }
  return '';
});
const commands = computed<CommandItem[]>(() => {
  const input: ConnectionCommandInput = {
    resourceType: props.resourceType,
    allowSsh: props.allowSsh,
    allowSftp: props.allowSftp,
    connectionInfo: connectionInfo.value,
    databaseConnection: databaseConnectionPlan.value,
  };
  return buildConnectionCommands(input);
});
const temporaryPasswordExpiryText = computed(() => {
  const formatted = formatExpiresAt(temporaryPasswordExpiresAt.value);
  return formatted ? `${formatted}（到期前可重复使用）` : '30 分钟内可重复使用';
});
const sshClientUrl = computed(() => {
  if (!connectionInfo.value) return '#';
  return buildSSHDeepLink({
    username: connectionInfo.value.compactUser,
    password: temporaryPassword.value,
    host: databaseConnectionHost.value,
    port: connectionInfo.value.port,
  });
});
const databaseClientLaunchBlocked = computed(() => (
  databaseClient.directLaunchReady
  && (
    !connectionInfo.value
    || (databaseUseTLS.value && !secureGatewayTLS.value)
    || !temporaryPassword.value
  )
));
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

function captureTargetSnapshot(): ConnectionTargetSnapshot | null {
  if (!props.modelValue || !props.target) return null;
  const targetID = String(props.target.id || props.target.resource_id || '');
  if (!targetID) return null;
  const resourceType = props.resourceType;
  const protocol = props.protocol.trim().toLowerCase();
  return {
    key: `${resourceType}:${targetID}:${protocol}`,
    resourceType,
    protocol,
    target: { ...props.target },
  };
}

function currentTargetSnapshotKey(): string {
  if (!props.modelValue || !props.target) return '';
  const targetID = String(props.target.id || props.target.resource_id || '');
  if (!targetID) return '';
  return `${props.resourceType}:${targetID}:${props.protocol.trim().toLowerCase()}`;
}

function operationCounterKey(operation: string): string {
  return `${currentTargetSnapshotKey()}:${operation}`;
}

function isCopyInFlight(value: string, operation = 'value'): boolean {
  return Boolean(value) && isInFlight(operationCounters, operationCounterKey(`copy:${operation}`), 'copy');
}

watch(
  () => [props.modelValue, String(props.target?.id || props.target?.resource_id || ''), props.resourceType, props.protocol] as const,
  ([isVisible, targetID]) => {
    if (!isVisible || !targetID) {
      initializeRequest.invalidate();
      testRequest.invalidate();
      creatingSession.value = false;
      connectionTesting.value = false;
      clearConnectionState();
      return;
    }
    const snapshot = captureTargetSnapshot();
    if (snapshot) void initializeConnection(snapshot);
  },
);

function clearConnectionState() {
  connectionError.value = '';
  connectionTestResult.value = null;
  sshIdentityBlocked.value = false;
  connectionInfo.value = null;
  temporaryPassword.value = '';
  temporaryPasswordExpiresAt.value = '';
  databaseUseTLS.value = false;
  for (const key of Object.keys(operationCounters)) delete operationCounters[key];
}

async function initializeConnection(snapshot: ConnectionTargetSnapshot) {
  const request = initializeRequest.begin(snapshot.key, async () => {
    const targetID = String(snapshot.target.id || snapshot.target.resource_id || '');
    if (!targetID) throw new Error('无法获取目标资源ID');
    if (snapshot.resourceType === 'database') {
      return loadDatabaseConnectionResources({
        protocol: snapshot.protocol,
        targetID,
        getGateway: protocol => apiClient.getDBGateway(protocol),
        validateGateway: gateway => {
          const message = databaseGatewayConnectionError(gateway, snapshot.protocol);
          if (message) throw new Error(message);
        },
        createSession: accountID => apiClient.createUserSession(accountID),
        createPassword: accountID => apiClient.createConnectionPassword(accountID),
      });
    }
    const [session, credential] = await Promise.all([
      apiClient.createUserSession(targetID),
      apiClient.createConnectionPassword(targetID),
    ]);
    return { session, credential, gateway: null };
  });
  const token = request.token;
  clearConnectionState();
  creatingSession.value = true;
  if (snapshot.resourceType === 'database' && snapshot.protocol.trim().toLowerCase() === 'redis') {
    testRequest.invalidate();
    connectionTesting.value = false;
  } else {
    void testConnection(snapshot);
  }
  try {
    const { session, credential, gateway } = await request.promise;
    if (!initializeRequest.isCurrent(token, currentTargetSnapshotKey())) return;
    const clientTLSMode = gateway?.client_tls_mode === 'required' ? 'required' : 'optional';
    databaseUseTLS.value = clientTLSMode === 'required';
    connectionInfo.value = {
      host: resolveDatabaseGatewayClientHost(
        gateway?.host,
        window.location.hostname,
      ),
      port: snapshot.resourceType === 'host'
        ? 47102
        : resolveDatabaseGatewayPort(snapshot.protocol, gateway),
      compactUser: session?.compact_username || '',
      tlsEnabled: gateway?.tls_enabled ?? false,
      clientTLSMode,
      tlsServerName: gateway?.tls_server_name || '',
      tlsCAPEM: gateway?.tls_ca_pem || '',
      tlsCertSHA256: gateway?.tls_cert_sha256 || '',
    };
    temporaryPassword.value = credential?.password || '';
    temporaryPasswordExpiresAt.value = credential?.expires_at || '';
  } catch (error) {
    if (!initializeRequest.isCurrent(token, currentTargetSnapshotKey())) return;
    connectionError.value = error instanceof Error ? error.message : '创建连接配置失败';
  } finally {
    if (initializeRequest.isCurrent(token, currentTargetSnapshotKey())) {
      creatingSession.value = false;
    }
  }
}

async function testConnection(snapshot: ConnectionTargetSnapshot) {
  const request = testRequest.begin(snapshot.key, async () => {
    if (snapshot.resourceType === 'database') {
      const targetID = String(snapshot.target.id || snapshot.target.resource_id || '');
      const result = await apiClient.testDBConnection(targetID);
      return { ok: result.ok, latency_ms: result.latency_ms, error: result.ok ? undefined : result.error || '连接失败' };
    }
    const target = snapshot.target as TargetRecord;
    const targetID = String(target.id || target.resource_id || '');
    if (!targetID) throw new Error('无法获取目标资源ID');
    const result = await apiClient.testTargetConnection({
      id: targetID,
    });
    return { ok: result.ok, latency_ms: result.latency_ms, error: result.ok ? undefined : result.error || result.message || '连接失败' };
  });
  const token = request.token;
  connectionTesting.value = true;
  try {
    const result = await request.promise;
    if (!testRequest.isCurrent(token, currentTargetSnapshotKey())) return;
    connectionTestResult.value = result;
  } catch (error) {
    if (!testRequest.isCurrent(token, currentTargetSnapshotKey())) return;
    const identityIssue = parseSSHHostIdentityIssue(error);
    if (identityIssue) {
      sshIdentityBlocked.value = true;
      connectionTestResult.value = { ok: false, error: 'SSH 主机身份校验未通过' };
      emit('hostIdentityChanged', identityIssue.hostId);
      const notice = sshHostIdentityNotice(identityIssue);
      await ElMessageBox.alert(notice.message, notice.title, {
        type: 'warning',
        confirmButtonText: '知道了',
      }).catch(() => undefined);
    } else {
      connectionTestResult.value = { ok: false, error: error instanceof Error ? error.message : '连接失败' };
    }
  } finally {
    if (testRequest.isCurrent(token, currentTargetSnapshotKey())) {
      connectionTesting.value = false;
    }
  }
}

async function copyValue(value: string, operation = 'value') {
  if (!value) return;
  const key = operationCounterKey(`copy:${operation}`);
  if (!beginInFlightIfIdle(operationCounters, key, 'copy')) return;
  try {
    await writeClipboardText(value);
    ElMessage.success('已复制');
  } catch {
    ElMessage.warning('复制失败，请手动复制');
  } finally {
    endInFlight(operationCounters, key, 'copy');
  }
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

function openDatabaseClientSettings() {
  const returnTo = router.currentRoute.value.fullPath;
  visible.value = false;
  void router.push({ path: '/settings', query: { tab: 'database', return_to: returnTo } });
}

function openDatabaseClient() {
  if (!databaseClient.configured) {
    ElMessage.warning('请先配置本地 DBeaver 客户端');
    openDatabaseClientSettings();
    return;
  }
  if (databaseClient.value.platform !== 'windows') {
    ElMessage.warning('当前仅 Windows 支持从浏览器直接打开 DBeaver');
    openDatabaseClientSettings();
    return;
  }
  if (!databaseClient.directLaunchReady) {
    ElMessage.warning('请先执行本地协议注册命令，并在设置中确认已完成');
    openDatabaseClientSettings();
    return;
  }
  if (databaseUseTLS.value && !databaseClient.value.caFilePath.trim()) {
    ElMessage.warning('使用 TLS 打开 DBeaver 前，请先在个人设置中配置网关 CA 文件');
    openDatabaseClientSettings();
    return;
  }
  if (
    !connectionInfo.value
    || !temporaryPassword.value
    || (databaseUseTLS.value && !secureGatewayTLS.value)
  ) {
    ElMessage.warning('数据库连接信息尚未就绪');
    return;
  }
  const launchURL = buildDatabaseProtocolURL({
    protocol: props.protocol,
    host: databaseConnectionHost.value,
    port: connectionInfo.value.port,
    username: connectionInfo.value.compactUser,
    password: temporaryPassword.value,
    databaseName: ['postgres', 'postgresql'].includes(props.protocol.toLowerCase()) ? 'postgres' : '',
    connectionName: props.resourceName || 'Jianmen 临时连接',
    tls: databaseUseTLS.value ? 'verify-full' : 'disable',
  });
  if (!launchURL) {
    ElMessage.error('连接参数不符合本地客户端安全规则');
    return;
  }
  ElMessage.success('正在打开 DBeaver 并使用临时密码建立连接');
  window.location.href = launchURL;
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
  } catch {
    ElMessage.error(preferences.error || '客户端配置保存失败');
    return;
  }

  try {
    await writeClipboardText(initRegCommand.value);
    ElMessage.success('配置已保存，注册命令已复制，请在管理员 CMD 中执行一次');
    initClientVisible.value = false;
  } catch {
    ElMessage.warning('配置已保存，但注册命令复制失败，请手动复制');
  }
}

function formatExpiresAt(value: string): string {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false });
}
</script>

<style scoped>
.dialog-content { display: flex; min-width: 0; flex-direction: column; gap: 10px; }
.resource-summary { display: grid; grid-template-columns: auto minmax(0, 1fr) minmax(260px, auto); gap: 12px; align-items: center; padding: 12px 14px; border: 1px solid var(--el-border-color-light); border-radius: 12px; background: linear-gradient(135deg, var(--el-fill-color-light), transparent); }
.resource-icon { display: grid; place-items: center; width: 44px; height: 44px; border-radius: 10px; background: var(--el-color-primary); color: white; font-size: 12px; font-weight: 800; letter-spacing: .06em; }
.resource-main { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.resource-main strong { overflow: hidden; font-size: 16px; text-overflow: ellipsis; white-space: nowrap; }
.source-meta { display: grid; gap: 6px; }
.source-meta > div { display: grid; grid-template-columns: 54px minmax(0, 1fr); gap: 8px; align-items: center; }
.source-meta span, .detail-item > span { color: var(--el-text-color-secondary); font-size: 12px; }
.source-meta code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.connectivity-row { display: flex; align-items: center; gap: 8px; color: var(--el-text-color-secondary); font-size: 13px; }
.shared-connection-panel { overflow: hidden; border: 1px solid var(--el-border-color-light); border-radius: 10px; }
.shared-connection-panel .detail-grid { border-top: 0; }
.database-tls-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 9px 14px; border-top: 1px solid var(--el-border-color-lighter); background: var(--el-fill-color-extra-light); }
.database-tls-row > div { display: grid; gap: 3px; }
.database-tls-row strong { font-size: 13px; }
.database-tls-row span { color: var(--el-text-color-secondary); font-size: 12px; }
:deep(.copy-action) { justify-self: end; }
.connect-error { color: var(--el-color-danger); }
.loading-state { padding: 30px 0; text-align: center; }
.loading-state p { margin: 10px 0 0; color: var(--el-text-color-secondary); }
.connection-panel { overflow: hidden; border: 1px solid var(--el-border-color-light); border-radius: 12px; }
.connection-panel header { display: flex; justify-content: space-between; align-items: center; gap: 16px; padding: 10px 14px; }
.connection-panel header > strong { flex: 0 0 auto; font-size: 15px; }
.permanent-panel header { background: var(--el-color-primary-light-9); }
.temporary-panel header { background: var(--el-color-warning-light-9); }
.expiry-summary { display: flex; align-items: center; justify-content: flex-end; gap: 8px; min-width: 0; font-size: 12px; }
.expiry-summary span { flex: 0 0 auto; color: var(--el-text-color-secondary); }
.expiry-summary strong { overflow: hidden; color: var(--el-color-warning-dark-2); text-overflow: ellipsis; white-space: nowrap; }
.detail-grid { display: grid; grid-template-columns: 1fr; gap: 1px; background: var(--el-border-color-lighter); border-top: 1px solid var(--el-border-color-lighter); }
.detail-item { display: grid; grid-template-columns: 72px minmax(0, 1fr) auto; align-items: center; gap: 10px; min-width: 0; padding: 9px 14px; background: var(--el-bg-color); }
.detail-item code, .detail-item strong { overflow-wrap: anywhere; font-size: 13px; }
.detail-value { min-width: 0; }
.accent-value code { color: var(--el-color-warning-dark-2); font-size: 14px; font-weight: 800; letter-spacing: .04em; }
.password-label { color: var(--el-text-color-regular) !important; font-size: 14px !important; }
.password-hint { grid-column: 2 / 4; color: var(--el-text-color-secondary); font-size: 13px; font-weight: 400; line-height: 1.5; }
.command-list { display: flex; flex-direction: column; gap: 10px; padding: 10px 14px; border-top: 1px solid var(--el-border-color-lighter); background: var(--el-fill-color-extra-light); }
.command-row { display: grid; min-width: 0; gap: 5px; }
.command-row__heading { color: var(--el-text-color-secondary); font-size: 12px; }
:deep(.command-input .el-input__inner) { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; }
.path-field { display: flex; gap: 8px; width: 100%; }
.path-help { margin-top: 5px; color: var(--el-text-color-secondary); font-size: 12px; }
.registration-command { display: flex; flex-direction: column; gap: 6px; color: var(--el-text-color-secondary); font-size: 12px; }
@media (max-width: 680px) {
  .resource-summary { grid-template-columns: auto minmax(0, 1fr); }
  .source-meta { grid-column: 1 / -1; }
  .connection-panel header { align-items: flex-start; flex-direction: column; gap: 6px; }
  .expiry-summary { align-items: flex-start; flex-direction: column; gap: 2px; }
  .detail-item { grid-template-columns: minmax(0, 1fr) auto; }
  .detail-item > span { grid-column: 1 / -1; }
  .password-hint { grid-column: 1 / -1; }
  .path-field { align-items: stretch; flex-direction: column; }
  :deep(.connection-config-dialog .el-dialog__footer),
  :deep(.local-client-dialog .el-dialog__footer) { display: flex; flex-wrap: wrap; gap: 8px; }
  :deep(.connection-config-dialog .el-dialog__footer .el-button),
  :deep(.local-client-dialog .el-dialog__footer .el-button) { flex: 1 1 180px; margin: 0; }
}
</style>
