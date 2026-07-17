<template>
  <div class="view-stack">
    <el-tabs v-model="activeTab" class="page-tabs">
      <el-tab-pane v-if="canConnectHost" label="主机" name="ssh">
        <section class="page-card quick-card-page">
          <div class="page-card__toolbar">
            <div class="page-card__search">
              <el-input
                v-model="sshSearchInput"
                placeholder="搜索主机、账号..."
                clearable
                @keyup.enter="onSSHSearch(sshSearchInput)"
                @clear="onSSHSearch('')"
              >
                <template #prefix>
                  <el-icon><Search /></el-icon>
                </template>
              </el-input>
            </div>
            <div class="quick-filter-bar">
              <div class="quick-filter-options" :class="{ 'is-expanded': sshFiltersExpanded }">
                <el-button size="small" :type="sshFilter === 'all' ? 'primary' : undefined" @click="setSSHFilter('all')">全部</el-button>
                <el-button size="small" :type="sshFilter === 'popular' ? 'primary' : undefined" @click="setSSHFilter('popular')">常用</el-button>
                <el-button
                  v-for="option in visibleSSHGroupOptions"
                  :key="option.value"
                  size="small"
                  :type="sshFilter === option.value ? 'primary' : undefined"
                  @click="setSSHFilter(option.value)"
                >
                  {{ option.label }}
                </el-button>
              </div>
              <el-button v-if="sshGroupOptions.length > filterPreviewLimit" link size="small" class="quick-filter-more" @click="sshFiltersExpanded = !sshFiltersExpanded">
                {{ sshFiltersExpanded ? '收起' : '更多' }}
              </el-button>
            </div>
            <div class="page-card__spacer"></div>
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
                  <div class="protocol-mark">SSH</div>
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

                <div v-if="connectionState(target).error" class="connection-card__error">
                  <span>{{ connectionState(target).error }}</span>
                  <el-button link type="primary" @click="retryConnectionInfo(target)">重试</el-button>
                </div>

                <footer class="connection-card__actions">
                  <el-button
                    type="primary"
                    size="small"
                    :loading="connectionState(target).loading"
                    @click="copyAllConnectionInfo(target)"
                  >
                    复制
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
                placeholder="搜索实例、账号..."
                clearable
                @keyup.enter="onDBSearch(dbSearchInput)"
                @clear="onDBSearch('')"
              >
                <template #prefix>
                  <el-icon><Search /></el-icon>
                </template>
              </el-input>
            </div>
            <div class="quick-filter-bar">
              <div class="quick-filter-options" :class="{ 'is-expanded': dbFiltersExpanded }">
                <el-button size="small" :type="dbFilter === 'all' ? 'primary' : undefined" @click="setDBFilter('all')">全部</el-button>
                <el-button size="small" :type="dbFilter === 'popular' ? 'primary' : undefined" @click="setDBFilter('popular')">常用</el-button>
                <el-button
                  v-for="option in visibleDBGroupOptions"
                  :key="option.value"
                  size="small"
                  :type="dbFilter === option.value ? 'primary' : undefined"
                  @click="setDBFilter(option.value)"
                >
                  {{ option.label }}
                </el-button>
              </div>
              <el-button v-if="dbGroupOptions.length > filterPreviewLimit" link size="small" class="quick-filter-more" @click="dbFiltersExpanded = !dbFiltersExpanded">
                {{ dbFiltersExpanded ? '收起' : '更多' }}
              </el-button>
            </div>
            <div class="page-card__spacer"></div>
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
                  <el-button
                    type="primary"
                    size="small"
                    :loading="databaseConnectionLoading(account)"
                    @click="copyDBConnectionInfo(account)"
                  >
                    复制
                  </el-button>
                  <el-button size="small" @click="openDBConfig(account)">
                    {{ t('quickConnect.action.connect') }}
                  </el-button>
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
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { Refresh, Search } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import { useRouter } from 'vue-router';

import { apiClient, type DBAccountRecord, type HostView, type PageResponse, type TargetRecord } from '@/api/client';
import ConnectionConfigDialog from '@/components/ConnectionConfigDialog.vue';
import QuickContainerConnectPanel from '@/components/QuickContainerConnectPanel.vue';
import { useI18n } from '@/i18n';
import { usePermissionStore } from '@/stores/permission';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';

interface HostMeta {
  name: string;
  group: string;
  remark: string;
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
const router = useRouter();
const canConnectHost = computed(() => permission.canDo('session:connect') || permission.canDo('sftp:connect'));
const canConnectContainer = computed(() => permission.canDo('container:connect'));
const activeTab = ref(canConnectHost.value ? 'ssh' : permission.canDo('db:connect') ? 'db' : 'container');

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
const sshFilter = ref('all');
const sshFiltersExpanded = ref(false);

// DB state
const dbSearchInput = ref('');
const dbKeyword = ref('');
const dbLoading = ref(false);
const dbError = ref('');
const dbAccounts = ref<QuickDBTarget[]>([]);
const dbConnectionStates = reactive<Record<string, { loading: boolean }>>({});
const dbUsageCounts = ref<Record<string, number>>({});
const dbFilter = ref('all');
const dbFiltersExpanded = ref(false);
const dbPage = ref(1);
const dbPageSize = ref(50);

// Dialog state
const hostConfigVisible = ref(false);
const selectedSSHTarget = ref<TargetRecord | null>(null);
const configVisible = ref(false);
const selectedDBTarget = ref<QuickDBTarget | null>(null);

function targetKey(target: TargetRecord): string {
  return String(target.id || target.resource_id || `${target.host_id || target.host}-${target.username || ''}`);
}

function targetHost(target: TargetRecord): string {
  return String(target.host || target.address || '');
}

function targetPort(target: TargetRecord): number {
  return Number(target.port) || 22;
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

const visibleSSHGroupOptions = computed(() => {
  if (sshFiltersExpanded.value) return sshGroupOptions.value;
  const options = sshGroupOptions.value.slice(0, filterPreviewLimit);
  if (sshFilter.value !== 'all' && sshFilter.value !== 'popular' && !options.some(option => option.value === sshFilter.value)) {
    const selected = sshGroupOptions.value.find(option => option.value === sshFilter.value);
    if (selected) return [selected, ...options.slice(0, filterPreviewLimit - 1)];
  }
  return options;
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

async function loadSSHUsage() {
  sshUsageCounts.value = {};
  if (!permission.canDo('audit:view')) return;
  try {
    const sessions = await fetchAllPages(page => apiClient.getSessions({ page, page_size: 200 }));
    const counts: Record<string, number> = {};
    sessions.forEach(session => {
      const key = usageKey(String(session.target_name || session.target_address || ''), String(session.account_name || session.account_username || ''));
      counts[key] = (counts[key] || 0) + 1;
    });
    sshUsageCounts.value = counts;
  } catch {
    // Audit permission or storage may be unavailable; group filters still work.
  }
}

function setSSHFilter(value: string) {
  sshFilter.value = value;
  targetPage.value = 1;
}

async function loadTargets() {
  sshLoading.value = true;
  sshError.value = '';
  try {
    const items = await fetchAllPages(page => apiClient.getTargets({
      page,
      page_size: 200,
      q: sshKeyword.value.trim() || undefined,
      connectable: true,
    }));
    items.forEach(initializeConnectionState);
    targets.value = items;
    await loadHostMeta();
    await loadSSHUsage();
    void hydrateConnectionInfo(displayedTargets.value);
  } catch (error) {
    sshError.value = error instanceof Error ? error.message : '无法加载主机账号';
    ElMessage.error(sshError.value);
  } finally {
    sshLoading.value = false;
  }
}

async function loadHostMeta() {
  try {
    const hostPage = await apiClient.getHosts({ page: 1, page_size: 999 });
    hostMeta.value = Object.fromEntries((hostPage.items ?? []).map((host: HostView) => [
      String(host.id || ''),
      {
        name: String(host.name || host.address || ''),
        group: String(host.group || ''),
        remark: String(host.remark || ''),
      },
    ]));
  } catch {
    hostMeta.value = {};
  }
}

async function hydrateConnectionInfo(items: TargetRecord[]) {
  const queue = [...items];
  const workers = Array.from({ length: Math.min(6, queue.length) }, async () => {
    while (queue.length) {
      const target = queue.shift();
      if (target) await ensureConnectionInfo(target);
    }
  });
  await Promise.all(workers);
}

function ensureConnectionInfo(target: TargetRecord, force = false): Promise<SSHConnectionState> {
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

function openWebConnection(target: TargetRecord) {
  const targetID = String(target.id || target.resource_id || '');
  if (!targetID) {
    ElMessage.error('无法获取目标资源 ID');
    return;
  }
  router.push({ path: '/web-terminal', query: { target_id: targetID } });
}

async function openClientConnection(target: TargetRecord) {
  const state = await ensureConnectionInfo(target);
  if (state.error || !state.compactUser || !state.password) {
    ElMessage.error(state.error || '连接信息尚未生成');
    return;
  }
  if (!preferences.loaded) {
    try {
      await preferences.fetch();
    } catch {
      // The connection dialog provides the client initialization flow.
    }
  }
  if (!preferences.hasSSHClient) {
    selectedSSHTarget.value = target;
    hostConfigVisible.value = true;
    ElMessage.warning('请先完成本地 SSH 客户端初始化');
    return;
  }
  const password = encodeURIComponent(state.password);
  window.location.href = `ssh://${state.compactUser}:${password}@${state.host}:${state.port}`;
}

async function loadDBAccounts() {
  dbLoading.value = true;
  dbError.value = '';
  dbPage.value = 1;
  try {
    const instRes = await apiClient.getDBInstances({ page: 1, page_size: 999 });
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

const visibleDBGroupOptions = computed(() => {
  if (dbFiltersExpanded.value) return dbGroupOptions.value;
  const options = dbGroupOptions.value.slice(0, filterPreviewLimit);
  if (dbFilter.value !== 'all' && dbFilter.value !== 'popular' && !options.some(option => option.value === dbFilter.value)) {
    const selected = dbGroupOptions.value.find(option => option.value === dbFilter.value);
    if (selected) return [selected, ...options.slice(0, filterPreviewLimit - 1)];
  }
  return options;
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

function databaseConnectionLoading(account: QuickDBTarget): boolean {
  return dbConnectionStates[databaseTargetKey(account)]?.loading ?? false;
}

function databaseProtocolLabel(protocol?: string): string {
  const labels: Record<string, string> = {
    mysql: 'MySQL',
    postgres: 'PG',
    postgresql: 'PG',
    redis: 'Redis',
  };
  return labels[String(protocol || 'mysql').toLowerCase()] || String(protocol || 'DB').toUpperCase();
}

function onDBSearch(query: string) {
  dbSearchInput.value = query;
  dbKeyword.value = query;
  dbFilter.value = 'all';
  dbPage.value = 1;
}

function databaseConnectionCommand(protocol: string, host: string, port: number, username: string): string {
  const normalized = protocol.toLowerCase();
  if (normalized === 'redis') return `redis-cli -h ${host} -p ${port} --user ${username} --askpass`;
  if (normalized === 'postgres' || normalized === 'postgresql') return `psql -h ${host} -p ${port} -U ${username}`;
  return `mysql --protocol=tcp -h ${host} -P ${port} -u ${username} -p`;
}

async function copyDBConnectionInfo(account: QuickDBTarget) {
  const targetID = String(account.id || account.resource_id || '');
  if (!targetID) {
    ElMessage.error('无法获取数据库账号 ID');
    return;
  }

  const key = databaseTargetKey(account);
  dbConnectionStates[key] = { loading: true };
  try {
    const [session, credential, gateway] = await Promise.all([
      apiClient.createUserSession(targetID),
      apiClient.createConnectionPassword(targetID),
      apiClient.getDBGateway(),
    ]);
    const host = gateway?.host || window.location.hostname || '127.0.0.1';
    const port = Number(gateway?.port) || 33060;
    const compactUser = String(session.compact_username || '');
    const password = String(credential.password || '');
    const protocol = String(account._protocol || 'mysql');
    if (!compactUser || !password) throw new Error('连接信息不完整');

    const content = [
      `数据库实例：${account._instance_name || '-'}`,
      `实例分组：${databaseGroup(account)}`,
      `实例备注：${databaseRemark(account)}`,
      `数据库账号：${account.username || '-'}`,
      `连接地址：${host}:${port}`,
      `连接账户：${compactUser}`,
      `连接临时密码：${password}`,
      `连接命令：${databaseConnectionCommand(protocol, host, port, compactUser)}`,
    ].join('\n');
    await writeClipboardText(content);
    ElMessage.success('数据库临时连接信息已复制');
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '复制数据库连接信息失败');
  } finally {
    dbConnectionStates[key].loading = false;
  }
}

function openDBConfig(account: QuickDBTarget) {
  selectedDBTarget.value = account;
  configVisible.value = true;
}

watch([targetPage, targetPageSize, sshFilter], () => {
  void hydrateConnectionInfo(displayedTargets.value);
});

watch(activeTab, tab => {
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

.quick-card-page {
  position: relative;
}

.quick-filter-bar {
  display: flex;
  align-items: center;
  flex: 1 1 auto;
  gap: 6px;
  min-width: 0;
}

.quick-filter-options {
  display: flex;
  flex: 1 1 auto;
  gap: 6px;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
}

.quick-filter-options.is-expanded {
  flex-wrap: wrap;
  overflow: visible;
  white-space: normal;
}

.quick-filter-options .el-button,
.quick-filter-more {
  flex: 0 0 auto;
  margin: 0;
}

.quick-filter-options .el-button {
  padding-inline: 9px;
}

.quick-filter-more {
  padding-inline: 4px;
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
  grid-template-columns: repeat(2, minmax(0, 1fr));
  padding-top: 8px;
}

@media (max-width: 780px) {
  .quick-filter-bar {
    width: 100%;
  }

  .quick-card-body {
    padding: 12px;
  }

  .connection-card-grid,
  .database-card-grid {
    grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
  }
}

@media (max-width: 480px) {
  .connection-card-grid,
  .database-card-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
