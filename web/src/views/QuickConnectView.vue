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

            <div v-if="targets.length" class="connection-card-grid">
              <article v-for="target in targets" :key="targetKey(target)" class="connection-card">
                <header class="connection-card__header">
                  <div class="protocol-mark">SSH</div>
                  <div class="connection-card__identity">
                    <div class="connection-card__title-row">
                      <h3>{{ quickHostName(target) }}</h3>
                      <el-tag
                        :type="target.status === 'disabled' ? 'info' : 'success'"
                        size="small"
                        effect="plain"
                      >
                        {{ target.status === 'disabled' ? t('common.disabled') : t('common.enabled') }}
                      </el-tag>
                    </div>
                    <p>{{ quickHostRemark(target) }}</p>
                  </div>
                </header>

                <div class="account-strip">
                  <span>账户名称</span>
                  <strong>{{ accountDisplayName(target) }}</strong>
                </div>

                <dl class="connection-details">
                  <div>
                    <dt>连接地址</dt>
                    <dd><code>{{ connectionAddress(target) }}</code></dd>
                  </div>
                  <div>
                    <dt>连接账户</dt>
                    <dd>
                      <code>{{ connectionState(target).compactUser || connectionPlaceholder(target) }}</code>
                    </dd>
                  </div>
                  <div class="password-detail">
                    <dt>连接临时密码</dt>
                    <dd>
                      <code>{{ connectionState(target).password || connectionPlaceholder(target) }}</code>
                      <small v-if="connectionState(target).expiresAt">
                        有效至 {{ formatExpiresAt(connectionState(target).expiresAt) }}
                      </small>
                    </dd>
                  </div>
                </dl>

                <div v-if="connectionState(target).error" class="connection-card__error">
                  <span>{{ connectionState(target).error }}</span>
                  <el-button link type="primary" @click="retryConnectionInfo(target)">重试</el-button>
                </div>

                <footer class="connection-card__actions">
                  <el-button
                    type="primary"
                    :loading="connectionState(target).loading"
                    @click="copyAllConnectionInfo(target)"
                  >
                    一键复制
                  </el-button>
                  <el-button
                    v-if="permission.canDo('session:connect')"
                    @click="openWebConnection(target)"
                  >
                    Web 连接
                  </el-button>
                  <el-button
                    v-if="permission.canDo('session:connect')"
                    :loading="connectionState(target).loading || preferences.loading"
                    @click="openClientConnection(target)"
                  >
                    客户端连接
                  </el-button>
                </footer>
              </article>
            </div>

            <el-empty v-else-if="!sshLoading" description="暂无可连接的主机账户" />
          </div>

          <div v-if="targetTotal > 0" class="page-card__footer">
            <el-pagination
              v-model:current-page="targetPage"
              v-model:page-size="targetPageSize"
              :page-sizes="[20, 50, 100]"
              :total="targetTotal"
              layout="total, sizes, prev, pager, next"
              size="small"
              background
            />
          </div>
        </section>
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
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { Refresh, Search } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import { useRouter } from 'vue-router';

import { apiClient, type DBAccountRecord, type HostView, type PageResponse, type TargetRecord } from '@/api/client';
import ConnectionConfigDialog from '@/components/ConnectionConfigDialog.vue';
import DataTableCard from '@/components/DataTableCard.vue';
import { useI18n } from '@/i18n';
import { usePermissionStore } from '@/stores/permission';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';

interface HostMeta {
  name: string;
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
  _protocol?: string;
  _instance_address?: string;
  _instance_port?: number;
};

const { t } = useI18n();
const permission = usePermissionStore();
const preferences = usePreferencesStore();
const router = useRouter();
const canConnectHost = computed(() => permission.canDo('session:connect') || permission.canDo('sftp:connect'));
const activeTab = ref(canConnectHost.value ? 'ssh' : 'db');

// SSH state
const sshSearchInput = ref('');
const sshKeyword = ref('');
const sshLoading = ref(false);
const sshError = ref('');
const targets = ref<TargetRecord[]>([]);
const targetTotal = ref(0);
const targetPage = ref(1);
const targetPageSize = ref(50);
const hostMeta = ref<Record<string, HostMeta>>({});
const targetConnectionStates = reactive<Record<string, SSHConnectionState>>({});
const targetConnectionRequests = new Map<string, Promise<SSHConnectionState>>();

// DB state
const dbKeyword = ref('');
const dbLoading = ref(false);
const dbError = ref('');
const dbAccounts = ref<QuickDBTarget[]>([]);
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

function connectionPlaceholder(target: TargetRecord): string {
  const state = connectionState(target);
  if (state.loading) return '生成中...';
  if (state.error) return '生成失败';
  return '等待生成';
}

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
    const items = res.items ?? [];
    items.forEach(initializeConnectionState);
    targets.value = items;
    targetTotal.value = res.total ?? 0;
    await loadHostMeta();
    void hydrateConnectionInfo(targets.value);
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

function formatExpiresAt(value: string): string {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false });
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
      const accRes = await apiClient.getDBAccounts(String(inst.id), { page: 1, page_size: 999, connectable: true });
      for (const account of accRes.items ?? []) {
        all.push({
          ...account,
          _instance_name: inst.name,
          _protocol: inst.protocol || 'mysql',
          _instance_address: inst.address,
          _instance_port: inst.port,
        });
      }
    }
    dbAccounts.value = all;
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
    [account._instance_name, account.username, account._protocol].some(value => String(value ?? '').toLowerCase().includes(query)),
  );
});

const dbTotal = computed(() => dbFiltered.value.length);

const displayedDBAccounts = computed(() => {
  const start = (dbPage.value - 1) * dbPageSize.value;
  return dbFiltered.value.slice(start, start + dbPageSize.value);
});

function onDBSearch(query: string) {
  dbKeyword.value = query;
  dbPage.value = 1;
}

function openDBConfig(account: QuickDBTarget) {
  selectedDBTarget.value = account;
  configVisible.value = true;
}

watch([targetPage, targetPageSize], () => loadTargets());

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
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 16px;
}

.connection-card {
  display: flex;
  min-width: 0;
  flex-direction: column;
  border: 1px solid var(--color-border);
  border-radius: 18px;
  background: var(--color-card);
  box-shadow: 0 8px 24px rgb(15 23 42 / 6%);
  overflow: hidden;
  transition: border-color 160ms ease, box-shadow 160ms ease, transform 160ms ease;
}

.connection-card:hover {
  border-color: rgb(14 165 233 / 36%);
  box-shadow: 0 14px 34px rgb(15 23 42 / 10%);
  transform: translateY(-2px);
}

.connection-card__header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 12px;
  align-items: center;
  padding: 16px 16px 13px;
}

.protocol-mark {
  display: grid;
  width: 46px;
  height: 46px;
  border-radius: 14px;
  place-items: center;
  background: linear-gradient(145deg, #0f766e, #0ea5a3);
  color: white;
  font-size: 12px;
  font-weight: 900;
  letter-spacing: .08em;
  box-shadow: 0 8px 18px rgb(15 118 110 / 22%);
}

.connection-card__identity {
  min-width: 0;
}

.connection-card__title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.connection-card__title-row h3 {
  margin: 0;
  overflow: hidden;
  color: var(--color-text);
  font-size: 16px;
  font-weight: 850;
  letter-spacing: -.02em;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.connection-card__identity p {
  margin: 5px 0 0;
  overflow: hidden;
  color: var(--color-text-secondary);
  font-size: 12px;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-strip {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 0 16px;
  padding: 10px 12px;
  border: 1px solid rgb(14 165 233 / 14%);
  border-radius: 12px;
  background: rgb(14 165 233 / 6%);
}

.account-strip span {
  color: var(--color-text-secondary);
  font-size: 12px;
}

.account-strip strong {
  overflow: hidden;
  color: var(--color-text);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.connection-details {
  display: grid;
  gap: 0;
  margin: 14px 16px 0;
  border: 1px solid var(--color-border);
  border-radius: 12px;
  overflow: hidden;
}

.connection-details > div {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  gap: 12px;
  align-items: center;
  min-width: 0;
  padding: 10px 12px;
  background: var(--color-card);
}

.connection-details > div + div {
  border-top: 1px solid var(--color-border);
}

.connection-details dt {
  color: var(--color-text-secondary);
  font-size: 12px;
}

.connection-details dd {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
  margin: 0;
}

.connection-details code {
  overflow-wrap: anywhere;
  color: var(--color-text);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  font-weight: 700;
}

.password-detail code {
  color: var(--el-color-warning-dark-2);
}

.connection-details small {
  color: var(--color-text-secondary);
  font-size: 10px;
}

.connection-card__error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin: 10px 16px 0;
  color: var(--el-color-danger);
  font-size: 12px;
}

.connection-card__actions {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin-top: auto;
  padding: 14px 16px 16px;
}

.connection-card__actions .el-button {
  min-width: 0;
  margin: 0;
  padding-inline: 8px;
}

@media (max-width: 780px) {
  .quick-card-body {
    padding: 12px;
  }

  .connection-card-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .connection-card__actions {
    grid-template-columns: 1fr;
  }
}
</style>