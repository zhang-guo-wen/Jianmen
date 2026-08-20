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
          <InfoValue :label="addressLabel" :value="gatewayAddress" :loading="isCopyInFlight(gatewayAddress, addressLabel)" @copy="copyValue" />
          <InfoValue :label="accountLabel" :value="connectionInfo.compactUser" :loading="isCopyInFlight(connectionInfo.compactUser, accountLabel)" @copy="copyValue" />
          <InfoValue :label="passwordLabel" :value="temporaryPassword" :loading="isCopyInFlight(temporaryPassword, passwordLabel)" accent @copy="copyValue" />
        </div>
        <div class="credential-hints">
          <div class="credential-hint"><span>密码有效期</span><strong>{{ temporaryPasswordExpiryText }}</strong></div>
          <div class="credential-hint">{{ longTermPasswordHint }}</div>
        </div>
      </section>

      <div v-if="creatingSession" class="loading-state">
        <el-icon class="is-loading" :size="30"><Loading /></el-icon>
        <p>正在生成连接配置…</p>
      </div>
    </div>

    <template #footer>
      <el-button
        data-testid="copy-connection-credentials"
        type="primary"
        aria-label="复制包含临时密码的连接凭据"
        title="包含临时密码，请妥善保管"
        :disabled="!connectionInfo || !temporaryPassword"
        :loading="isCopyInFlight(temporaryPassword, '复制凭据')"
        @click="copyAllConnectionInfo"
      >
        复制凭据
      </el-button>
      <el-button data-testid="ssh-local-client" v-if="resourceType === 'host' && allowSsh" type="primary" :disabled="!connectionTestResult?.ok" :loading="preferences.loading" @click="openPreferredSSHClient">本地 SSH 客户端打开</el-button>
      <el-button data-testid="ssh-browser" v-if="resourceType === 'host' && allowSsh" type="primary" :disabled="!connectionTestResult?.ok" @click="openInBrowser">在浏览器中打开</el-button>
      <el-button
        v-if="resourceType === 'database' && !isRedis && allowWebSql"
        data-testid="database-web-sql"
        type="primary"
        @click="openSQLConsole"
      >
        Web SQL 控制台
      </el-button>
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

</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { Loading } from '@element-plus/icons-vue';
import { ElButton, ElMessage } from 'element-plus';

import {
  apiClient,
  type DatabaseGatewayTLSTrustMode,
  type DBAccountRecord,
  type DBGatewayConfig,
  type HostView,
  type TargetRecord,
} from '@/api/client';
import { buildDatabaseProtocolURL } from '@/config/databaseClients';
import { useDatabaseClientStore } from '@/stores/databaseClient';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';
import { databaseGatewayConnectionError } from '@/utils/databaseGatewayAvailability';
import {
  loadDatabaseConnectionResources,
} from '@/utils/databaseConnectionOrchestration';
import { useSSHHostIdentityRecovery } from '@/composables/useSSHHostIdentityRecovery';
import {
  databaseGatewayRequiresCustomCA,
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
  type KeyedRequestToken,
} from '@/utils/connectionRequestState';
import { buildSSHDeepLink } from '@/utils/connectionLinks';

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
  allowWebSql?: boolean;
}>(), {
  resourceName: '', sourceAddress: '', sourceAccount: '', protocol: 'mysql', allowSsh: true, allowSftp: false, allowWebSql: false,
});

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'hostIdentityChanged', host: HostView): void
  (event: 'hostIdentityInvalidated', hostId: string): void
}>();
const { runWithSSHHostIdentityRecovery } = useSSHHostIdentityRecovery({
  onConfirmed: host => emit('hostIdentityChanged', host),
  onCancelledAfterDisable: issue => emit('hostIdentityInvalidated', issue.hostId),
});
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
const connectionInfo = ref<{
  host: string;
  port: number;
  compactUser: string;
  tlsEnabled: boolean;
  clientTLSMode: 'required' | 'optional';
  tlsTrustMode: DatabaseGatewayTLSTrustMode | undefined;
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
let connectionDialogActive = true;

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
const addressLabel = computed(() => props.resourceType === 'host' ? '主机地址' : '数据库地址');
const accountLabel = computed(() => props.resourceType === 'host' ? '主机账号' : '数据库账号');
const passwordLabel = computed(() => props.resourceType === 'host' ? '登录密码' : '数据库密码');
const longTermPasswordHint = computed(() => (
  props.resourceType === 'host'
    ? '长期连接可输入堡垒机的登录密码作为长期密码（不是目标主机的密码）'
    : '长期连接可输入堡垒机的登录密码作为长期密码（不是目标数据库的密码）'
));
const secureGatewayTLS = computed(() => hasDatabaseGatewayTLSIdentity({
  enabled: true,
  tls_enabled: connectionInfo.value?.tlsEnabled ?? false,
  tls_trust_mode: connectionInfo.value?.tlsTrustMode,
  tls_server_name: connectionInfo.value?.tlsServerName,
  tls_ca_pem: connectionInfo.value?.tlsCAPEM,
  tls_cert_sha256: connectionInfo.value?.tlsCertSHA256,
}));
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
      tlsTrustMode: gateway?.tls_trust_mode === 'system' || gateway?.tls_trust_mode === 'custom'
        ? gateway.tls_trust_mode
        : undefined,
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
  let token: KeyedRequestToken | null = null;
  const request = testRequest.begin(snapshot.key, async () => {
    if (snapshot.resourceType === 'database') {
      const targetID = String(snapshot.target.id || snapshot.target.resource_id || '');
      const result = await apiClient.testDBConnection(targetID);
      return { ok: result.ok, latency_ms: result.latency_ms, error: result.ok ? undefined : result.error || '连接失败' };
    }
    const target = snapshot.target as TargetRecord;
    const targetID = String(target.id || target.resource_id || '');
    if (!targetID) throw new Error('无法获取目标资源ID');
    const recovery = await runWithSSHHostIdentityRecovery(
      () => apiClient.testTargetConnection({ id: targetID }),
      {
        key: snapshot.key,
        isCurrent: () => (
          connectionDialogActive
          && token !== null
          && testRequest.isCurrent(token, currentTargetSnapshotKey())
        ),
      },
    );
    if (recovery.status !== 'success') {
      return {
        ok: false,
        error: recovery.status === 'cancelled' ? '已取消连接' : '连接状态已变化，请重试',
      };
    }
    const result = recovery.value;
    return { ok: result.ok, latency_ms: result.latency_ms, error: result.ok ? undefined : result.error || result.message || '连接失败' };
  });
  token = request.token;
  connectionTesting.value = true;
  try {
    const result = await request.promise;
    if (!testRequest.isCurrent(token, currentTargetSnapshotKey())) return;
    connectionTestResult.value = result;
  } catch (error) {
    if (!testRequest.isCurrent(token, currentTargetSnapshotKey())) return;
    connectionTestResult.value = { ok: false, error: error instanceof Error ? error.message : '连接失败' };
  } finally {
    if (testRequest.isCurrent(token, currentTargetSnapshotKey())) {
      connectionTesting.value = false;
    }
  }
}

onBeforeUnmount(() => {
  connectionDialogActive = false;
  initializeRequest.invalidate();
  testRequest.invalidate();
});

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

async function copyAllConnectionInfo() {
  if (!connectionInfo.value || !temporaryPassword.value) {
    ElMessage.error('连接信息尚未生成');
    return;
  }
  const isDatabase = props.resourceType === 'database';
  const content = [
    isDatabase
      ? `数据库实例：${props.resourceName || '-'}`
      : `资源名称：${props.resourceName || '-'}`,
  ];
  if (isDatabase) {
    content.push(`账号名称：${props.sourceAccount || '-'}`);
  } else {
    content.push(`源地址：${props.sourceAddress || '-'}`);
    content.push(`源账号：${props.sourceAccount || '-'}`);
  }
  content.push(
    `${isDatabase ? '数据库地址' : '主机地址'}：${gatewayAddress.value}`,
    `${isDatabase ? '数据库账号' : '主机账号'}：${connectionInfo.value.compactUser}`,
    `${isDatabase ? '数据库密码' : '登录密码'}：${temporaryPassword.value}`,
    `密码有效期：${temporaryPasswordExpiryText.value}`,
    longTermPasswordHint.value,
  );
  try {
    await writeClipboardText(content.join('\n'));
    ElMessage.success('临时连接信息已全部复制');
  } catch {
    ElMessage.error('复制失败，请稍后重试');
  }
}

async function openPreferredSSHClient() {
  if (!connectionInfo.value) return;
  if (!preferences.loaded) {
    try {
      await preferences.fetch();
    } catch {
      openClientSettings('ssh');
      return;
    }
  }
  if (!preferences.hasSSHClient || !preferences.sshProtocolRegistered) {
    openClientSettings('ssh');
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

function openSQLConsole() {
  const accountID = String(props.target?.id || '').trim();
  if (!accountID) {
    ElMessage.warning('缺少数据库账号，无法打开 Web SQL 控制台');
    return;
  }
  visible.value = false;
  void router.push({
    path: '/sql-console',
    query: { database_account_id: accountID },
  });
}

function openClientSettings(tab: 'ssh' | 'database') {
  const returnTo = router.currentRoute.value.fullPath;
  visible.value = false;
  void router.push({ path: '/settings', query: { tab, return_to: returnTo } });
}

function openDatabaseClient() {
  if (!databaseClient.configured) {
    ElMessage.warning('请先配置本地 DBeaver 客户端');
    openClientSettings('database');
    return;
  }
  if (databaseClient.value.platform !== 'windows') {
    ElMessage.warning('当前仅 Windows 支持从浏览器直接打开 DBeaver');
    openClientSettings('database');
    return;
  }
  if (!databaseClient.directLaunchReady) {
    ElMessage.warning('请先执行本地协议注册命令，并在设置中确认已完成');
    openClientSettings('database');
    return;
  }
  if (
    databaseUseTLS.value
    && databaseGatewayRequiresCustomCA({
      enabled: true,
      tls_enabled: connectionInfo.value?.tlsEnabled ?? false,
      tls_trust_mode: connectionInfo.value?.tlsTrustMode,
    })
    && !databaseClient.value.caFilePath.trim()
  ) {
    ElMessage.warning('使用 TLS 打开 DBeaver 前，请先在个人设置中配置网关 CA 文件');
    openClientSettings('database');
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
    ...(databaseUseTLS.value && connectionInfo.value.tlsTrustMode
      ? { tlsTrust: connectionInfo.value.tlsTrustMode }
      : {}),
  });
  if (!launchURL) {
    ElMessage.error('连接参数不符合本地客户端安全规则');
    return;
  }
  ElMessage.success('正在打开 DBeaver 并使用临时密码建立连接');
  window.location.href = launchURL;
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
:deep(.copy-action) { justify-self: end; }
.connect-error { color: var(--el-color-danger); }
.loading-state { padding: 30px 0; text-align: center; }
.loading-state p { margin: 10px 0 0; color: var(--el-text-color-secondary); }
.credential-hints { display: grid; gap: 6px; padding: 9px 14px; border-top: 1px solid var(--el-border-color-lighter); background: var(--el-fill-color-extra-light); }
.credential-hint { display: flex; align-items: center; gap: 8px; min-width: 0; color: var(--el-text-color-secondary); font-size: 12px; line-height: 1.5; }
.credential-hint strong { overflow: hidden; color: var(--el-color-warning-dark-2); text-overflow: ellipsis; white-space: nowrap; }
.detail-grid { display: grid; grid-template-columns: 1fr; gap: 1px; background: var(--el-border-color-lighter); border-top: 1px solid var(--el-border-color-lighter); }
.detail-item { display: grid; grid-template-columns: 72px minmax(0, 1fr) auto; align-items: center; gap: 10px; min-width: 0; padding: 9px 14px; background: var(--el-bg-color); }
.detail-item code, .detail-item strong { overflow-wrap: anywhere; font-size: 13px; }
.detail-value { min-width: 0; }
.accent-value code { color: var(--el-color-warning-dark-2); font-size: 14px; font-weight: 800; letter-spacing: .04em; }
@media (max-width: 680px) {
  .resource-summary { grid-template-columns: auto minmax(0, 1fr); }
  .source-meta { grid-column: 1 / -1; }
  .credential-hint { align-items: flex-start; flex-direction: column; gap: 2px; }
  .detail-item { grid-template-columns: minmax(0, 1fr) auto; }
  .detail-item > span { grid-column: 1 / -1; }
  :deep(.connection-config-dialog .el-dialog__footer) { display: flex; flex-wrap: wrap; gap: 8px; }
  :deep(.connection-config-dialog .el-dialog__footer .el-button) { flex: 1 1 180px; margin: 0; }
}
</style>
