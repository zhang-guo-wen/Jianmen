<template>
  <div class="view-stack">
    <el-tabs v-model="activeTab" class="page-tabs">
      <el-tab-pane v-if="canConnectHost" label="主机" name="ssh">
        <section class="page-card quick-card-page">
          <div class="page-card__toolbar">
            <div class="page-card__search">
              <el-input
                v-model="sshSearchInput"
                name="quick_connect_host_search"
                autocomplete="off"
                aria-label="搜索主机和账号"
                placeholder="搜索主机、账号…"
                clearable
                @keyup.enter="onSSHSearch(sshSearchInput)"
                @clear="onSSHSearch('')"
              >
                <template #prefix>
                  <el-icon><Search /></el-icon>
                </template>
              </el-input>
            </div>
            <ResourceFilterBar
              :model-value="sshFilter"
              :options="sshGroupOptions"
              :preview-limit="filterPreviewLimit"
              @update:model-value="setSSHFilter"
            />
            <div class="page-card__actions">
              <el-button :loading="sshLoading" :icon="Refresh" @click="loadTargets">
                {{ t('common.refresh') }}
              </el-button>
            </div>
          </div>

          <div v-loading="sshLoading" class="page-card__body quick-card-body">
            <el-alert
              v-if="sshError"
              class="load-alert"
              type="error"
              show-icon
              :closable="false"
              :title="sshError"
            />

            <div v-if="displayedTargets.length" class="connection-card-grid">
              <article v-for="target in displayedTargets" :key="targetKey(target)" class="connection-card host-connection-card">
                <div class="connection-card__summary">
                  <div class="protocol-mark" :class="{ 'protocol-mark--rdp': isRDPTarget(target) }">
                    {{ targetProtocolLabel(target) }}
                  </div>
                  <div class="connection-card__identity">
                    <h3>{{ quickHostName(target) }}</h3>
                    <p>{{ accountDisplayName(target) }}</p>
                  </div>
                  <span class="connection-card__group" :title="quickHostGroup(target)">
                    {{ quickHostGroup(target) }}
                  </span>
                </div>
                <div class="connection-card__remark" :title="quickHostRemark(target)">
                  {{ quickHostRemark(target) }}
                </div>

                <div v-if="!isRDPTarget(target) && connectionState(target).error" class="connection-card__error">
                  <span>{{ connectionState(target).error }}</span>
                  <el-button link type="primary" @click="retryConnectionInfo(target)">重试</el-button>
                </div>

                <footer class="connection-card__actions">
                  <template v-if="isRDPTarget(target)">
                    <el-button
                      v-if="permission.canDo('rdp:connect')"
                      type="primary"
                      size="small"
                      @click="openWebConnection(target)"
                    >
                      Web RDP
                    </el-button>
                  </template>
                  <template v-else>
                    <el-button
                      type="primary"
                      size="small"
                      aria-label="复制包含临时密码的主机连接信息"
                      title="包含临时密码，请妥善保管"
                      :loading="connectionState(target).loading"
                      @click="copyAllConnectionInfo(target)"
                    >
                      复制凭据
                    </el-button>
                    <el-button
                      v-if="permission.canDo('session:connect')"
                      size="small"
                      @click="openWebConnection(target)"
                    >
                      Web
                    </el-button>
                    <el-button
                      v-if="permission.canDo('session:connect')"
                      size="small"
                      :loading="connectionState(target).loading || preferences.loading"
                      @click="openClientConnection(target)"
                    >
                      客户端
                    </el-button>
                  </template>
                </footer>
              </article>
            </div>

            <el-empty v-else-if="!sshLoading" description="暂无可连接的主机账户" />
          </div>

          <div v-if="sshFilteredTotal > 0" class="page-card__footer">
            <el-pagination
              v-model:current-page="targetPage"
              v-model:page-size="targetPageSize"
              :page-sizes="[20, 50, 100]"
              :total="sshFilteredTotal"
              layout="total, sizes, prev, pager, next"
              :pager-count="5"
              size="small"
              background
            />
          </div>
        </section>
      </el-tab-pane>

      <el-tab-pane v-if="permission.canDo('db:connect')" label="数据库" name="db">
        <section class="page-card quick-card-page">
          <div class="page-card__toolbar">
            <div class="page-card__search">
              <el-input
                v-model="dbSearchInput"
                name="quick_connect_database_search"
                autocomplete="off"
                aria-label="搜索数据库实例和账号"
                placeholder="搜索实例、账号…"
                clearable
                @keyup.enter="onDBSearch(dbSearchInput)"
                @clear="onDBSearch('')"
              >
                <template #prefix>
                  <el-icon><Search /></el-icon>
                </template>
              </el-input>
            </div>
            <ResourceFilterBar
              :model-value="dbFilter"
              :options="dbGroupOptions"
              :preview-limit="filterPreviewLimit"
              @update:model-value="setDBFilter"
            />
            <div class="page-card__actions">
              <el-button :loading="dbLoading" :icon="Refresh" @click="loadDBAccounts">
                {{ t('common.refresh') }}
              </el-button>
            </div>
          </div>

          <div v-loading="dbLoading" class="page-card__body quick-card-body">
            <el-alert
              v-if="dbError"
              class="load-alert"
              type="error"
              show-icon
              :closable="false"
              :title="dbError"
            />

            <div v-if="displayedDBAccounts.length" class="connection-card-grid database-card-grid">
              <article v-for="account in displayedDBAccounts" :key="databaseTargetKey(account)" class="connection-card database-connection-card">
                <div class="connection-card__summary">
                  <div class="protocol-mark" :class="`protocol-mark--${account._protocol || 'mysql'}`">
                    {{ databaseProtocolLabel(account._protocol) }}
                  </div>
                  <div class="connection-card__identity">
                    <h3>{{ account._instance_name || '-' }}</h3>
                    <p>{{ account.username || '-' }}</p>
                  </div>
                  <span class="connection-card__group" :title="databaseGroup(account)">
                    {{ databaseGroup(account) }}
                  </span>
                </div>
                <div class="connection-card__remark" :title="databaseRemark(account)">
                  {{ databaseRemark(account) }}
                </div>
                <footer class="connection-card__actions database-card__actions">
                  <el-tooltip
                    :content="databaseCredentialUnavailableReason(account._protocol)"
                    :disabled="!databaseCredentialUnavailableReason(account._protocol)"
                    placement="top"
                  >
                    <span
                      class="database-action-tooltip"
                      :tabindex="databaseCredentialUnavailableReason(account._protocol) ? 0 : undefined"
                      :role="databaseCredentialUnavailableReason(account._protocol) ? 'button' : undefined"
                      :aria-disabled="databaseCredentialUnavailableReason(account._protocol) ? 'true' : undefined"
                      :aria-label="databaseCredentialUnavailableReason(account._protocol) || undefined"
                    >
                      <el-button
                        type="primary"
                        size="small"
                        aria-label="复制包含临时密码的数据库连接信息"
                        title="包含临时密码，请妥善保管"
                        :disabled="Boolean(databaseCredentialUnavailableReason(account._protocol))"
                        :loading="databaseCredentialLoading(account)"
                        @click="copyDatabaseConnectionInfo(account)"
                      >
                        复制凭据
                      </el-button>
                    </span>
                  </el-tooltip>
                  <el-tooltip content="Web 数据库连接暂未开放" placement="top">
                    <span
                      class="database-action-tooltip"
                      tabindex="0"
                      role="button"
                      aria-disabled="true"
                      aria-label="Web 数据库连接暂未开放"
                    >
                      <el-button size="small" disabled>
                        Web
                      </el-button>
                    </span>
                  </el-tooltip>
                  <el-tooltip
                    :content="databaseClientUnavailableReason(account._protocol)"
                    :disabled="!databaseClientUnavailableReason(account._protocol)"
                    placement="top"
                  >
                    <span
                      class="database-action-tooltip"
                      :tabindex="databaseClientUnavailableReason(account._protocol) ? 0 : undefined"
                      :role="databaseClientUnavailableReason(account._protocol) ? 'button' : undefined"
                      :aria-disabled="databaseClientUnavailableReason(account._protocol) ? 'true' : undefined"
                      :aria-label="databaseClientUnavailableReason(account._protocol) || undefined"
                    >
                      <el-button
                        size="small"
                        :disabled="Boolean(databaseClientUnavailableReason(account._protocol))"
                        :loading="databaseClientLoading(account)"
                        @click="openDatabaseClient(account)"
                      >
                        客户端
                      </el-button>
                    </span>
                  </el-tooltip>
                </footer>
              </article>
            </div>

            <el-empty v-else-if="!dbLoading" description="暂无可连接的数据库账号" />
          </div>

          <div v-if="dbTotal > 0" class="page-card__footer">
            <el-pagination
              v-model:current-page="dbPage"
              v-model:page-size="dbPageSize"
              :page-sizes="[20, 50, 100]"
              :total="dbTotal"
              layout="total, sizes, prev, pager, next"
              :pager-count="5"
              size="small"
              background
            />
          </div>
        </section>
      </el-tab-pane>

      <el-tab-pane v-if="canConnectContainer" label="容器" name="container">
        <QuickContainerConnectPanel :active="activeTab === 'container'" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { Refresh, Search } from '@element-plus/icons-vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { useRoute, useRouter } from 'vue-router';

import {
  apiClient,
  type DBAccountRecord,
  type DBGatewayConfig,
  type HostView,
  type PageResponse,
  type TargetRecord,
} from '@/api/client';
import QuickContainerConnectPanel from '@/components/QuickContainerConnectPanel.vue';
import ResourceFilterBar from '@/components/ResourceFilterBar.vue';
import { buildDatabaseProtocolURL } from '@/config/databaseClients';
import { useI18n } from '@/i18n';
import { useDatabaseClientStore } from '@/stores/databaseClient';
import { usePermissionStore } from '@/stores/permission';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';
import {
  databaseGatewayConnectionError,
  databaseProtocolLabel,
} from '@/utils/databaseGatewayAvailability';
import { loadDatabaseConnectionResources } from '@/utils/databaseConnectionOrchestration';
import {
  databaseGatewayCAFileName,
  hasDatabaseGatewayTLSIdentity,
  resolveDatabaseGatewayPort,
} from '@/utils/databaseGatewayCommands';
import {
  createSingleFlight,
  createLatestKeyedRequest,
} from '@/utils/connectionRequestState';
import { buildSSHDeepLink } from '@/utils/connectionLinks';
import {
  parseSSHHostIdentityIssue,
  sshHostIdentityNotice,
} from '@/utils/sshHostIdentity';

interface HostMeta {
  name: string;
  group: string;
  remark: string;
  protocol: string;
}

interface SSHConnectionState {
  loading: boolean;
  error: string;
  host: string;
  port: number;
  compactUser: string;
  password: string;
  expiresAt: string;
}

interface DatabaseConnectionState {
  loading: boolean;
  host: string;
  port: number;
  compactUser: string;
  password: string;
  expiresAt: string;
}

class DatabaseGatewayConfigurationRedirect extends Error {}

type QuickDBTarget = DBAccountRecord & {
  _instance_name?: string;
  _instance_group?: string;
  _instance_remark?: string;
  _protocol?: string;
  _instance_address?: string;
  _instance_port?: number;
};

const { t } = useI18n();
const permission = usePermissionStore();
const preferences = usePreferencesStore();
const databaseClient = useDatabaseClientStore();
const route = useRoute();
const router = useRouter();
const canConnectHost = computed(() =>
  permission.canDo('session:connect')
  || permission.canDo('sftp:connect')
  || permission.canDo('rdp:connect')
);
const canConnectContainer = computed(() => permission.canDo('container:connect'));
const defaultQuickConnectTab = canConnectHost.value ? 'ssh' : permission.canDo('db:connect') ? 'db' : 'container';
const requestedQuickConnectTab = route.query.tab === 'db'
  ? 'db'
  : route.query.tab === 'container'
    ? 'container'
    : 'ssh';
const activeTab = ref(
  requestedQuickConnectTab === 'db' && permission.canDo('db:connect')
    ? 'db'
    : requestedQuickConnectTab === 'container' && canConnectContainer.value
      ? 'container'
      : defaultQuickConnectTab,
);

// SSH state
const sshSearchInput = ref('');
const sshKeyword = ref('');
const sshLoading = ref(false);
const sshError = ref('');
const targets = ref<TargetRecord[]>([]);
const targetPage = ref(1);
const targetPageSize = ref(50);
const hostMeta = ref<Record<string, HostMeta>>({});
const targetConnectionStates = reactive<Record<string, SSHConnectionState>>({});
const targetConnectionRequests = new Map<string, Promise<SSHConnectionState>>();
const sshUsageCounts = ref<Record<string, number>>({});
const sshRequests = createLatestKeyedRequest<{
  items: TargetRecord[];
  hostMeta: Record<string, HostMeta>;
  usageCounts: Record<string, number>;
}>();
const sshFilter = ref('all');

// DB state
const dbSearchInput = ref('');
const dbKeyword = ref('');
const dbLoading = ref(false);
const dbError = ref('');
const dbAccounts = ref<QuickDBTarget[]>([]);
const dbClientLaunching = reactive<Record<string, boolean>>({});
const dbConnectionStates = reactive<Record<string, DatabaseConnectionState>>({});
const dbConnectionRequests = new Map<string, Promise<DatabaseConnectionState>>();
const dbAccountsFlight = createSingleFlight<void>();
const dbUsageCounts = ref<Record<string, number>>({});
const dbFilter = ref('all');
const dbPage = ref(1);
const dbPageSize = ref(50);

function targetKey(target: TargetRecord): string {
  return String(target.id || target.resource_id || `${target.host_id || target.host}-${target.username || ''}`);
}

function targetHost(target: TargetRecord): string {
  return String(target.host || target.address || '');
}

function targetProtocol(target: TargetRecord): 'ssh' | 'rdp' {
  const protocol = String(
    target.protocol || hostMeta.value[String(target.host_id || '')]?.protocol || 'ssh'
  ).toLowerCase();
  return protocol === 'rdp' ? 'rdp' : 'ssh';
}

function isRDPTarget(target: TargetRecord): boolean {
  return targetProtocol(target) === 'rdp';
}

function targetProtocolLabel(target: TargetRecord): string {
  return isRDPTarget(target) ? 'RDP' : 'SSH';
}

function accountDisplayName(target: TargetRecord): string {
  return String(target.name || target.username || '-');
}

function quickHostName(target: TargetRecord): string {
  return hostMeta.value[String(target.host_id || '')]?.name || targetHost(target) || '-';
}

function quickHostGroup(target: TargetRecord): string {
  return hostMeta.value[String(target.host_id || '')]?.group || '未分组';
}

function quickHostRemark(target: TargetRecord): string {
  return hostMeta.value[String(target.host_id || '')]?.remark || '暂无备注';
}

function initializeConnectionState(target: TargetRecord): SSHConnectionState {
  const key = targetKey(target);
  if (!targetConnectionStates[key]) {
    targetConnectionStates[key] = {
      loading: false,
      error: '',
      host: window.location.hostname || '127.0.0.1',
      port: 47102,
      compactUser: '',
      password: '',
      expiresAt: '',
    };
  }
  return targetConnectionStates[key];
}

function connectionState(target: TargetRecord): SSHConnectionState {
  return targetConnectionStates[targetKey(target)] || initializeConnectionState(target);
}

function connectionAddress(target: TargetRecord): string {
  const state = connectionState(target);
  return `${state.host}:${state.port}`;
}

const filterPreviewLimit = 6;

async function fetchAllPages<T>(fetchPage: (page: number, pageSize: number) => Promise<PageResponse<T>>): Promise<T[]> {
  const pageSize = 200;
  const items: T[] = [];
  let page = 1;
  let total = 0;
  do {
    const response = await fetchPage(page, pageSize);
    items.push(...(response.items ?? []));
    total = response.total ?? items.length;
    page += 1;
    if (!response.items?.length) break;
  } while (items.length < total);
  return items;
}

function usageKey(targetName: string, accountName: string): string {
  return `${String(targetName || '').trim().toLowerCase()}\u0000${String(accountName || '').trim().toLowerCase()}`;
}

function sshUsageCount(target: TargetRecord): number {
  return sshUsageCounts.value[usageKey(quickHostName(target), accountDisplayName(target))] || 0;
}

function dbUsageCount(account: QuickDBTarget): number {
  return dbUsageCounts.value[usageKey(account._instance_name || '', account.username || '')] || 0;
}

const sshGroupOptions = computed(() => {
  const groups = new Map<string, number>();
  targets.value.forEach(target => {
    const group = quickHostGroup(target);
    groups.set(group, (groups.get(group) || 0) + sshUsageCount(target));
  });
  return Array.from(groups, ([label, count]) => ({ label, value: label, count }))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label, 'zh-CN'));
});

const sshFilteredTargets = computed(() => {
  let items = targets.value;
  if (sshFilter.value !== 'all' && sshFilter.value !== 'popular') {
    items = items.filter(target => quickHostGroup(target) === sshFilter.value);
  }
  if (sshFilter.value === 'popular') {
    items = [...items].sort((a, b) => sshUsageCount(b) - sshUsageCount(a) || quickHostName(a).localeCompare(quickHostName(b), 'zh-CN'));
  }
  return items;
});

const sshFilteredTotal = computed(() => sshFilteredTargets.value.length);
const displayedTargets = computed(() => {
  const start = (targetPage.value - 1) * targetPageSize.value;
  return sshFilteredTargets.value.slice(start, start + targetPageSize.value);
});

async function loadSSHUsage(): Promise<Record<string, number>> {
  if (!permission.canDo('audit:view')) return {};
  try {
    const sessions = await fetchAllPages(page => apiClient.getSessions({ page, page_size: 200 }));
    const counts: Record<string, number> = {};
    sessions.forEach(session => {
      const key = usageKey(String(session.target_name || session.target_address || ''), String(session.account_name || session.account_username || ''));
      counts[key] = (counts[key] || 0) + 1;
    });
    return counts;
  } catch {
    // Audit permission or storage may be unavailable; group filters still work.
    return {};
  }
}

function setSSHFilter(value: string) {
  sshFilter.value = value;
  targetPage.value = 1;
}

async function loadTargets() {
  const keyword = sshKeyword.value.trim();
  const request = sshRequests.begin(keyword, async () => {
    const items = await fetchAllPages(page => apiClient.getTargets({
      page,
      page_size: 200,
      q: keyword || undefined,
      connectable: true,
    }));
    const [nextHostMeta, nextUsageCounts] = await Promise.all([loadHostMeta(), loadSSHUsage()]);
    return { items, hostMeta: nextHostMeta, usageCounts: nextUsageCounts };
  });
  sshLoading.value = sshRequests.isLoading();
  sshError.value = '';
  try {
    const result = await request.promise;
    if (!sshRequests.isCurrent(request.token, keyword)) return;
    hostMeta.value = result.hostMeta;
    result.items.filter(target => !isRDPTarget(target)).forEach(initializeConnectionState);
    targets.value = result.items;
    sshUsageCounts.value = result.usageCounts;
    void hydrateConnectionInfo(displayedTargets.value);
  } catch (error) {
    if (!sshRequests.isCurrent(request.token, keyword)) return;
    sshError.value = error instanceof Error ? error.message : '无法加载主机账号';
    ElMessage.error(sshError.value);
  } finally {
    sshLoading.value = sshRequests.isLoading();
  }
}

async function loadHostMeta(): Promise<Record<string, HostMeta>> {
  try {
    const hostPage = await apiClient.getHosts({ page: 1, page_size: 999 });
    return Object.fromEntries((hostPage.items ?? []).map((host: HostView) => [
      String(host.id || ''),
      {
        name: String(host.name || host.address || ''),
        group: String(host.group || ''),
        remark: String(host.remark || ''),
        protocol: String(host.protocol || 'ssh').toLowerCase(),
      },
    ]));
  } catch {
    return {};
  }
}

async function hydrateConnectionInfo(items: TargetRecord[]) {
  const queue = items.filter(target => !isRDPTarget(target));
  const workers = Array.from({ length: Math.min(6, queue.length) }, async () => {
    while (queue.length) {
      const target = queue.shift();
      if (target) await ensureConnectionInfo(target);
    }
  });
  await Promise.all(workers);
}

function ensureConnectionInfo(target: TargetRecord, force = false): Promise<SSHConnectionState> {
  if (isRDPTarget(target)) {
    return Promise.resolve(initializeConnectionState(target));
  }
  const key = targetKey(target);
  const state = connectionState(target);
  const expiryTime = Date.parse(state.expiresAt);
  const credentialUsable = state.compactUser && state.password && (!state.expiresAt || Number.isNaN(expiryTime) || expiryTime > Date.now());
  if (!force && credentialUsable) return Promise.resolve(state);
  const currentRequest = targetConnectionRequests.get(key);
  if (!currentRequest) {
    state.compactUser = '';
    state.password = '';
    state.expiresAt = '';
  }
  if (currentRequest) return currentRequest;

  const request = (async () => {
    state.loading = true;
    state.error = '';
    try {
      const targetID = String(target.id || target.resource_id || '');
      if (!targetID) throw new Error('无法获取目标资源 ID');
      const [session, credential] = await Promise.all([
        apiClient.createUserSession(targetID),
        apiClient.createConnectionPassword(targetID),
      ]);
      state.host = window.location.hostname || '127.0.0.1';
      state.port = 47102;
      state.compactUser = String(session.compact_username || '');
      state.password = credential.password;
      state.expiresAt = credential.expires_at;
      if (!state.compactUser || !state.password) throw new Error('连接信息不完整');
    } catch (error) {
      state.error = error instanceof Error ? error.message : '生成连接信息失败';
    } finally {
      state.loading = false;
      targetConnectionRequests.delete(key);
    }
    return state;
  })();

  targetConnectionRequests.set(key, request);
  return request;
}

async function retryConnectionInfo(target: TargetRecord) {
  const state = connectionState(target);
  state.compactUser = '';
  state.password = '';
  state.expiresAt = '';
  await ensureConnectionInfo(target, true);
}

function onSSHSearch(query: string) {
  sshSearchInput.value = query;
  sshKeyword.value = query;
  sshFilter.value = 'all';
  targetPage.value = 1;
  loadTargets();
}

async function copyAllConnectionInfo(target: TargetRecord) {
  const state = await ensureConnectionInfo(target);
  if (state.error || !state.compactUser || !state.password) {
    ElMessage.error(state.error || '连接信息尚未生成');
    return;
  }
  const content = [
    `主机名称：${quickHostName(target)}`,
    `主机分组：${quickHostGroup(target)}`,
    `主机备注：${quickHostRemark(target)}`,
    `账户名称：${accountDisplayName(target)}`,
    `连接地址：${connectionAddress(target)}`,
    `连接账户：${state.compactUser}`,
    `连接临时密码：${state.password}`,
  ].join('\n');
  try {
    await writeClipboardText(content);
    ElMessage.success('临时连接信息已全部复制');
  } catch {
    ElMessage.error('复制失败，请稍后重试');
  }
}

async function openWebConnection(target: TargetRecord) {
  const targetID = String(target.id || target.resource_id || '');
  if (!targetID) {
    ElMessage.error('无法获取目标资源 ID');
    return;
  }
  if (!isRDPTarget(target) && !(await preflightSSHConnection(target))) {
    return;
  }
  router.push({
    path: isRDPTarget(target) ? '/web-rdp' : '/web-terminal',
    query: { target_id: targetID },
  });
}

async function openClientConnection(target: TargetRecord) {
  if (isRDPTarget(target)) {
    await openWebConnection(target);
    return;
  }
  if (!preferences.loaded) {
    try {
      await preferences.fetch();
    } catch (error) {
      ElMessage.error(error instanceof Error ? error.message : '无法加载本地 SSH 客户端配置');
      return;
    }
  }
  if (!preferences.hasSSHClient) {
    ElMessage.warning('请先配置本地 SSH 客户端');
    openClientSettings('ssh');
    return;
  }
  if (!(await preflightSSHConnection(target))) {
    return;
  }
  const state = await ensureConnectionInfo(target);
  if (state.error || !state.compactUser || !state.password) {
    ElMessage.error(state.error || '连接信息尚未生成');
    return;
  }
  window.location.href = buildSSHDeepLink({
    username: state.compactUser,
    password: state.password,
    host: state.host,
    port: state.port,
  });
}

function loadDBAccounts(): Promise<void> {
  return dbAccountsFlight.run(performLoadDBAccounts);
}

async function performLoadDBAccounts() {
  dbLoading.value = true;
  dbError.value = '';
  dbPage.value = 1;
  try {
    const instRes = await apiClient.getDBInstances({ page: 1, page_size: 999, connectable: true });
    const insts = instRes.items ?? [];
    const all: QuickDBTarget[] = [];
    for (const inst of insts) {
      if (inst.status === 'disabled') continue;
      const accounts = await fetchAllPages(page => apiClient.getDBAccounts(String(inst.id), { page, page_size: 200, connectable: true }));
      for (const account of accounts) {
        all.push({
          ...account,
          _instance_name: inst.name,
          _instance_group: inst.group,
          _instance_remark: inst.remark,
          _protocol: inst.protocol || 'mysql',
          _instance_address: inst.address,
          _instance_port: inst.port,
        });
      }
    }
    dbAccounts.value = all;
    await loadDBUsage();
  } catch (error) {
    dbError.value = error instanceof Error ? error.message : '无法加载数据库账号';
    ElMessage.error(dbError.value);
  } finally {
    dbLoading.value = false;
  }
}

const dbFiltered = computed(() => {
  const query = dbKeyword.value.trim().toLowerCase();
  if (!query) return dbAccounts.value;
  return dbAccounts.value.filter(account =>
    [account._instance_name, account._instance_group, account._instance_remark, account.username, account._protocol].some(value =>
      String(value ?? '').toLowerCase().includes(query),
    ),
  );
});

const dbGroupOptions = computed(() => {
  const groups = new Map<string, number>();
  dbAccounts.value.forEach(account => {
    const group = databaseGroup(account);
    groups.set(group, (groups.get(group) || 0) + dbUsageCount(account));
  });
  return Array.from(groups, ([label, count]) => ({ label, value: label, count }))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label, 'zh-CN'));
});

const dbQuickFiltered = computed(() => {
  let items = dbFiltered.value;
  if (dbFilter.value !== 'all' && dbFilter.value !== 'popular') {
    items = items.filter(account => databaseGroup(account) === dbFilter.value);
  }
  if (dbFilter.value === 'popular') {
    items = [...items].sort((a, b) => dbUsageCount(b) - dbUsageCount(a) || String(a._instance_name || '').localeCompare(String(b._instance_name || ''), 'zh-CN'));
  }
  return items;
});

const dbTotal = computed(() => dbQuickFiltered.value.length);

const displayedDBAccounts = computed(() => {
  const start = (dbPage.value - 1) * dbPageSize.value;
  return dbQuickFiltered.value.slice(start, start + dbPageSize.value);
});

async function loadDBUsage() {
  dbUsageCounts.value = {};
  if (!permission.canDo('db:audit:view')) return;
  try {
    const connections = await fetchAllPages(page => apiClient.getDBConnections({ page, page_size: 200 }));
    const counts: Record<string, number> = {};
    connections.forEach(connection => {
      const key = usageKey(String(connection.target_name || connection.upstream_addr || ''), String(connection.account_name || connection.username || ''));
      counts[key] = (counts[key] || 0) + 1;
    });
    dbUsageCounts.value = counts;
  } catch {
    // Audit permission or storage may be unavailable; group filters still work.
  }
}

function setDBFilter(value: string) {
  dbFilter.value = value;
  dbPage.value = 1;
}

function databaseTargetKey(account: QuickDBTarget): string {
  return String(account.id || `${account._instance_name || ''}-${account.username || ''}`);
}

function databaseGroup(account: QuickDBTarget): string {
  return String(account._instance_group || '未分组');
}

function databaseRemark(account: QuickDBTarget): string {
  return String(account._instance_remark || '暂无备注');
}

function databaseClientLoading(account: QuickDBTarget): boolean {
  return dbClientLaunching[databaseTargetKey(account)] ?? false;
}

function databaseCredentialLoading(account: QuickDBTarget): boolean {
  return dbConnectionStates[databaseTargetKey(account)]?.loading ?? false;
}

function databaseCredentialUnavailableReason(protocol?: string): string {
  return String(protocol || '').toLowerCase() === 'redis'
    ? 'Redis 暂不支持复制临时连接凭据'
    : '';
}

function databaseClientUnavailableReason(protocol?: string): string {
  return String(protocol || '').toLowerCase() === 'redis'
    ? 'Redis 暂不支持通过 DBeaver 本地客户端打开'
    : '';
}

function onDBSearch(query: string) {
  dbSearchInput.value = query;
  dbKeyword.value = query;
  dbFilter.value = 'all';
  dbPage.value = 1;
}

function requireConnectableDatabaseGateway(
  gateway: DBGatewayConfig | null | undefined,
  protocol: string,
): DBGatewayConfig {
  const availabilityError = databaseGatewayConnectionError(gateway, protocol);
  if (!availabilityError && gateway) return gateway;
  const message = availabilityError || `${databaseProtocolLabel(protocol)} 数据库网关暂不可用`;
  if (gateway?.enabled) throw new Error(message);

  if (!permission.canAccessMenu('systemSettings')) {
    throw new Error(`${message}，请联系超级管理员完成系统配置`);
  }

  ElMessage.warning(`${message}，正在前往系统设置`);
  void router.push({
    path: '/system-settings',
    query: { return_to: router.currentRoute.value.fullPath },
  });
  throw new DatabaseGatewayConfigurationRedirect(message);
}

function ensureDatabaseConnectionInfo(account: QuickDBTarget): Promise<DatabaseConnectionState> {
  const key = databaseTargetKey(account);
  const currentState = dbConnectionStates[key];
  const expiryTime = Date.parse(currentState?.expiresAt || '');
  const credentialUsable = currentState?.compactUser
    && currentState.password
    && (!currentState.expiresAt || Number.isNaN(expiryTime) || expiryTime > Date.now());
  if (credentialUsable) return Promise.resolve(currentState);

  const currentRequest = dbConnectionRequests.get(key);
  if (currentRequest) return currentRequest;

  const state: DatabaseConnectionState = {
    loading: true,
    host: '',
    port: 0,
    compactUser: '',
    password: '',
    expiresAt: '',
  };
  dbConnectionStates[key] = state;

  const request = (async () => {
    try {
      const targetID = String(account.id || account.resource_id || '');
      if (!targetID) throw new Error('无法获取数据库账号 ID');
      const protocol = String(account._protocol || 'mysql');
      const { session, credential, gateway } = await loadDatabaseConnectionResources({
        protocol,
        targetID,
        getGateway: value => apiClient.getDBGateway(value),
        validateGateway: value => {
          requireConnectableDatabaseGateway(value, protocol);
        },
        createSession: accountID => apiClient.createUserSession(accountID),
        createPassword: accountID => apiClient.createConnectionPassword(accountID),
      });
      const connectableGateway = gateway;
      state.host = String(connectableGateway.tls_server_name || connectableGateway.host || window.location.hostname || '127.0.0.1');
      state.port = resolveDatabaseGatewayPort(protocol, connectableGateway);
      state.compactUser = String(session?.compact_username || '');
      state.password = String(credential?.password || '');
      state.expiresAt = String(credential?.expires_at || '');
      if (!state.host || !state.port || !state.compactUser || !state.password) {
        throw new Error('数据库连接信息不完整');
      }
      return state;
    } finally {
      state.loading = false;
      dbConnectionRequests.delete(key);
    }
  })();

  dbConnectionRequests.set(key, request);
  return request;
}

async function copyDatabaseConnectionInfo(account: QuickDBTarget) {
  try {
    const state = await ensureDatabaseConnectionInfo(account);
    const content = [
      `数据库实例：${account._instance_name || '-'}`,
      `数据库分组：${databaseGroup(account)}`,
      `数据库备注：${databaseRemark(account)}`,
      `账号名称：${account.username || '-'}`,
      `连接地址：${state.host}:${state.port}`,
      `连接账户：${state.compactUser}`,
      `连接临时密码：${state.password}`,
    ].join('\n');
    await writeClipboardText(content);
    ElMessage.success('数据库临时连接信息已全部复制');
  } catch (error) {
    if (error instanceof DatabaseGatewayConfigurationRedirect) return;
    ElMessage.error(error instanceof Error ? error.message : '复制失败，请稍后重试');
  }
}

async function openDatabaseClient(account: QuickDBTarget) {
  const targetID = String(account.id || account.resource_id || '');
  if (!targetID) {
    ElMessage.error('无法获取数据库账号 ID');
    return;
  }
  const protocol = String(account._protocol || 'mysql');
  const unavailableReason = databaseClientUnavailableReason(protocol);
  if (unavailableReason) {
    ElMessage.warning(unavailableReason);
    return;
  }
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

  const key = databaseTargetKey(account);
  if (dbClientLaunching[key]) return;
  dbClientLaunching[key] = true;
  try {
    const gateway = await apiClient.getDBGateway(protocol);
    const connectableGateway = requireConnectableDatabaseGateway(gateway, protocol);
    const session = await apiClient.createUserSession(targetID);
    if (!hasDatabaseGatewayTLSIdentity(connectableGateway)) {
      throw new Error('数据库网关 TLS 身份材料不完整，已阻止本地客户端打开');
    }
    const host = connectableGateway.tls_server_name;
    const port = resolveDatabaseGatewayPort(protocol, connectableGateway);
    const compactUser = String(session.compact_username || '');
    if (!compactUser) throw new Error('连接账号生成失败');
    const launchURL = buildDatabaseProtocolURL({
      protocol,
      host,
      port,
      username: compactUser,
      databaseName: 'postgres',
      connectionName: `${account._instance_name || 'Jianmen'} / ${account.username || '数据库账号'}`,
    });
    if (!launchURL) throw new Error('连接参数不符合本地客户端安全规则');
    downloadDatabaseGatewayCA(protocol, connectableGateway.tls_ca_pem);
    ElMessage.success('已下载网关 CA，正在打开 DBeaver 连接草稿；请在 SSL 配置中选择该 CA 后连接');
    window.location.href = launchURL;
  } catch (error) {
    if (error instanceof DatabaseGatewayConfigurationRedirect) return;
    ElMessage.error(error instanceof Error ? error.message : '无法打开本地数据库客户端');
  } finally {
    delete dbClientLaunching[key];
  }
}

function downloadDatabaseGatewayCA(protocol: string, pem: string) {
  const blob = new Blob([pem], { type: 'application/x-pem-file' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = databaseGatewayCAFileName(protocol);
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

function openClientSettings(tab: 'ssh' | 'database') {
  void router.push({
    path: '/settings',
    query: { tab, return_to: router.currentRoute.value.fullPath },
  });
}

async function preflightSSHConnection(target: TargetRecord): Promise<boolean> {
  const targetID = String(target.id || target.resource_id || '');
  if (!targetID) {
    ElMessage.error('无法获取目标资源 ID');
    return false;
  }
  try {
    const result = await apiClient.testTargetConnection({ id: targetID });
    if (result.ok) return true;
    ElMessage.error(result.error || result.message || '主机连接测试失败');
    return false;
  } catch (error) {
    const issue = parseSSHHostIdentityIssue(error);
    if (!issue) {
      ElMessage.error(error instanceof Error ? error.message : '主机连接测试失败');
      return false;
    }
    const notice = sshHostIdentityNotice(issue);
    await ElMessageBox.alert(notice.message, notice.title, {
      type: 'warning',
      confirmButtonText: '知道了',
    }).catch(() => undefined);
    await loadTargets();
    return false;
  }
}

watch([targetPage, targetPageSize, sshFilter], () => {
  void hydrateConnectionInfo(displayedTargets.value);
});

watch(activeTab, tab => {
  if (tab === 'db' && permission.canDo('db:connect') && dbAccounts.value.length === 0) {
    loadDBAccounts();
  }
  const queryTab = tab === 'ssh' ? 'host' : tab;
  if (route.name === 'quick-connect' && route.query.tab !== queryTab) {
    void router.replace({ query: { ...route.query, tab: queryTab } });
  }
});

watch(() => route.query.tab, tab => {
  const requested = tab === 'db' ? 'db' : tab === 'container' ? 'container' : 'ssh';
  const allowed = requested === 'db'
    ? permission.canDo('db:connect')
    : requested === 'container'
      ? canConnectContainer.value
      : canConnectHost.value;
  if (allowed && activeTab.value !== requested) activeTab.value = requested;
});

onMounted(() => {
  if (activeTab.value === 'db') loadDBAccounts();
  else if (activeTab.value === 'container') return;
  else if (canConnectHost.value) loadTargets();
});
</script>

<style scoped>
.page-tabs :deep(.el-tabs__header) {
  margin-bottom: 15px;
  padding: 0;
}

.quick-card-page {
  position: relative;
}

.quick-card-body {
  padding: 18px;
  background:
    radial-gradient(circle at 10% 0%, rgb(14 165 233 / 8%), transparent 30%),
    linear-gradient(180deg, var(--color-surface-muted), var(--color-card));
}

.load-alert {
  margin-bottom: 14px;
}

.connection-card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(238px, 1fr));
  gap: 10px;
}

.connection-card {
  display: flex;
  min-width: 0;
  flex-direction: column;
  border: 1px solid var(--color-border);
  border-radius: 12px;
  background: var(--color-card);
  box-shadow: 0 5px 16px rgb(15 23 42 / 5%);
  overflow: hidden;
  transition: border-color 160ms ease, box-shadow 160ms ease, transform 160ms ease;
}

.connection-card:hover {
  border-color: rgb(14 165 233 / 36%);
  box-shadow: 0 9px 22px rgb(15 23 42 / 9%);
  transform: translateY(-1px);
}

.connection-card__summary {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 9px;
  align-items: center;
  min-width: 0;
  padding: 12px 12px 8px;
}

.protocol-mark {
  display: grid;
  width: 38px;
  height: 38px;
  border-radius: 10px;
  place-items: center;
  background: linear-gradient(145deg, #0f766e, #0ea5a3);
  color: white;
  font-size: 10px;
  font-weight: 900;
  letter-spacing: .05em;
  box-shadow: 0 6px 14px rgb(15 118 110 / 18%);
}

.protocol-mark--mysql {
  background: linear-gradient(145deg, #2563eb, #0ea5e9);
}

.protocol-mark--rdp {
  background: linear-gradient(145deg, #2563eb, #0ea5e9);
  box-shadow: 0 6px 14px rgb(37 99 235 / 18%);
}

.protocol-mark--postgres,
.protocol-mark--postgresql {
  background: linear-gradient(145deg, #475569, #2563eb);
}

.protocol-mark--redis {
  background: linear-gradient(145deg, #dc2626, #f97316);
}

.connection-card__identity {
  min-width: 0;
}

.connection-card__identity h3 {
  margin: 0;
  overflow: hidden;
  color: var(--color-text);
  font-size: 14px;
  font-weight: 800;
  letter-spacing: -.015em;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.connection-card__identity p {
  margin: 3px 0 0;
  overflow: hidden;
  color: var(--color-text-secondary);
  font-size: 12px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.connection-card__group {
  align-self: start;
  max-width: 92px;
  overflow: hidden;
  padding: 2px 7px;
  border: 1px solid var(--color-border);
  border-radius: 999px;
  background: var(--color-surface-muted);
  color: var(--color-text-secondary);
  font-size: 10px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.connection-card__remark {
  min-width: 0;
  margin: 0 12px;
  overflow: hidden;
  color: var(--color-text-secondary);
  font-size: 11px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.connection-card__error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  margin: 7px 12px 0;
  color: var(--el-color-danger);
  font-size: 11px;
}

.connection-card__error span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.connection-card__actions {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
  margin-top: auto;
  padding: 10px 12px 12px;
}

.connection-card__actions .el-button {
  min-width: 0;
  margin: 0;
  padding: 5px 6px;
  font-size: 12px;
}

.database-card-grid {
  grid-template-columns: repeat(auto-fill, minmax(238px, 1fr));
}

.database-card__actions {
  grid-template-columns: repeat(3, minmax(0, 1fr));
  padding-top: 8px;
}

.database-action-tooltip {
  display: flex;
  min-width: 0;
}

.database-action-tooltip :deep(.el-button) {
  width: 100%;
}

@media (prefers-reduced-motion: reduce) {
  .connection-card {
    transition: none;
  }

  .connection-card:hover {
    transform: none;
  }
}

@media (max-width: 780px) {
  .quick-card-body {
    padding: 12px;
  }

  .connection-card-grid,
  .database-card-grid {
    grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
  }

  .quick-card-page > .page-card__footer {
    min-width: 0;
    justify-content: center;
    overflow-x: auto;
    overscroll-behavior-inline: contain;
  }

  .quick-card-page > .page-card__footer :deep(.el-pagination) {
    flex: 0 0 auto;
  }
}

@media (max-width: 620px) {
  .quick-card-page > .page-card__footer :deep(.el-pagination__total),
  .quick-card-page > .page-card__footer :deep(.el-pagination__sizes) {
    display: none;
  }
}

@media (max-width: 480px) {
  .connection-card-grid,
  .database-card-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
