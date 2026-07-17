<template>
  <section class="page-card quick-card-page container-quick-page">
    <div class="page-card__toolbar container-toolbar">
      <div class="page-card__search">
        <el-input v-model="searchInput" placeholder="搜索容器、主机、IP、备注..." clearable>
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
      </div>
      <div class="quick-filter-bar">
        <div class="quick-filter-options" :class="{ 'is-expanded': filtersExpanded }">
          <el-button size="small" :type="groupFilter === 'all' ? 'primary' : undefined" @click="setGroupFilter('all')">全部</el-button>
          <el-button
            v-for="option in visibleGroupOptions"
            :key="option.value"
            size="small"
            :type="groupFilter === option.value ? 'primary' : undefined"
            @click="setGroupFilter(option.value)"
          >
            {{ option.label }}
          </el-button>
        </div>
        <el-button v-if="groupOptions.length > filterPreviewLimit" link size="small" class="quick-filter-more" @click="filtersExpanded = !filtersExpanded">
          {{ filtersExpanded ? '收起' : '更多' }}
        </el-button>
      </div>
      <div class="page-card__spacer"></div>
      <div class="page-card__actions container-toolbar__actions">
        <el-button :icon="Sort" @click="toggleSort">容器名称 {{ sortDirection === 'asc' ? 'A-Z' : 'Z-A' }}</el-button>
        <el-button :loading="scanning" :icon="Refresh" @click="loadContainerInventory(true)">{{ t('common.refresh') }}</el-button>
      </div>
    </div>

    <div class="container-workspace">
      <section v-loading="initialLoading" class="container-card-panel">
        <el-alert v-if="inventoryError" class="load-alert" type="error" show-icon :closable="false" :title="inventoryError" />
        <div class="inventory-status">
          <span>
            已发现 <strong>{{ containerCards.length }}</strong> 个容器
            <template v-if="endpointTotal">，已扫描 {{ scannedEndpointCount }}/{{ endpointTotal }} 个运行时</template>
          </span>
          <span v-if="failedEndpointCount" class="inventory-status__warning">{{ failedEndpointCount }} 个运行时读取失败</span>
          <span v-else-if="scanning" class="inventory-status__scanning">正在分批加载，页面可继续操作</span>
        </div>

        <div v-if="displayedCards.length" class="container-card-scroll">
          <div class="container-card-grid">
            <button
              v-for="card in displayedCards"
              :key="card.key"
              type="button"
              class="container-card"
              :class="{ 'is-selected': selectedCard?.key === card.key }"
              @click="selectContainer(card)"
            >
              <div class="container-card__topline">
                <span class="container-card__state" :class="card.container.state || 'unknown'"></span>
                <strong :title="containerName(card)">{{ containerName(card) }}</strong>
                <span class="container-card__group" :title="hostGroup(card)">{{ hostGroup(card) }}</span>
              </div>
              <dl class="container-card__facts">
                <div><dt>主机名称</dt><dd :title="hostName(card)">{{ hostName(card) }}</dd></div>
                <div><dt>主机 IP</dt><dd :title="hostAddress(card)">{{ hostAddress(card) }}</dd></div>
              </dl>
              <p class="container-card__remark" :title="hostRemark(card)">{{ hostRemark(card) }}</p>
            </button>
          </div>
        </div>
        <el-empty v-else-if="!initialLoading && !scanning" description="暂无可读取的容器" />
        <div v-else-if="scanning" class="container-card-skeletons" aria-label="正在加载容器">
          <span v-for="index in 4" :key="index"></span>
        </div>

        <div v-if="filteredCards.length" class="container-card-pagination">
          <el-pagination
            v-model:current-page="page"
            v-model:page-size="pageSize"
            :page-sizes="[24, 48, 96]"
            :total="filteredCards.length"
            layout="total, sizes, prev, pager, next"
            size="small"
            background
          />
        </div>
      </section>
    </div>
  </section>

  <el-drawer
    v-model="logDrawerVisible"
    class="container-log-drawer"
    title="容器日志"
    direction="rtl"
    size="min(760px, 94vw)"
    append-to-body
    :modal="true"
    :with-header="false"
    :close-on-click-modal="true"
    :close-on-press-escape="true"
    @close="stopLogPolling"
    @closed="handleLogDrawerClosed"
  >
    <section v-if="selectedCard" class="container-log-panel">
      <header class="container-log-header">
        <div class="container-log-title">
          <span>容器日志</span>
          <strong>{{ containerName(selectedCard) }}</strong>
          <small>{{ hostName(selectedCard) }} · {{ hostAddress(selectedCard) }}</small>
        </div>
        <div class="container-log-actions">
          <el-input v-model="logSearch" size="small" clearable placeholder="搜索当前日志">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
          <el-button size="small" :loading="logsLoading" :icon="Refresh" @click="refreshLogsNow">刷新日志</el-button>
          <el-button class="container-log-close" circle text :icon="Close" aria-label="关闭日志" @click="logDrawerVisible = false" />
        </div>
      </header>
      <pre ref="logViewer" v-loading="logsLoading" class="container-log-viewer">{{ filteredLogs || (logSearch ? '没有匹配的日志' : '暂无日志输出') }}</pre>
    </section>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { Close, Refresh, Search, Sort } from '@element-plus/icons-vue';

import { apiClient, type ContainerEndpointView, type ContainerRecord } from '@/api/client';
import { useI18n } from '@/i18n';

interface QuickContainerCard {
  key: string;
  endpoint: ContainerEndpointView;
  container: ContainerRecord;
}

const props = defineProps<{ active: boolean }>();
const { t } = useI18n();
const searchInput = ref('');
const keyword = ref('');
const groupFilter = ref('all');
const filtersExpanded = ref(false);
const sortDirection = ref<'asc' | 'desc'>('asc');
const page = ref(1);
const pageSize = ref(48);
const containerCards = ref<QuickContainerCard[]>([]);
const selectedCard = ref<QuickContainerCard | null>(null);
const initialLoading = ref(false);
const scanning = ref(false);
const inventoryStarted = ref(false);
const inventoryError = ref('');
const endpointTotal = ref(0);
const scannedEndpointCount = ref(0);
const failedEndpointCount = ref(0);
const inventoryControllers = new Set<AbortController>();
const inventoryCache = new Map<string, { items: ContainerRecord[]; updatedAt: number }>();
let inventoryGeneration = 0;
let searchTimer: ReturnType<typeof setTimeout> | null = null;

const logDrawerVisible = ref(false);
const logs = ref('');
const logSearch = ref('');
const logsLoading = ref(false);
const logViewer = ref<HTMLElement | null>(null);
const logCache = new Map<string, string>();
let logController: AbortController | null = null;
let logPollingTimer: ReturnType<typeof setTimeout> | null = null;
let logGeneration = 0;
const inventoryConcurrency = 6;
const inventoryCacheTTL = 60_000;
const logPollingInterval = 5_000;
const maxLogChars = 600_000;
const filterPreviewLimit = 6;

function containerName(card: QuickContainerCard): string {
  return String(card.container.name || card.container.id.slice(0, 12) || '未命名容器');
}

function hostName(card: QuickContainerCard): string {
  return String(card.endpoint.host_name || card.endpoint.name || '未命名主机');
}

function hostAddress(card: QuickContainerCard): string {
  return String(card.endpoint.host_address || card.endpoint.address || '-');
}

function hostGroup(card: QuickContainerCard): string {
  return String(card.endpoint.host_group || card.endpoint.group || '未分组');
}

function hostRemark(card: QuickContainerCard): string {
  return String(card.endpoint.host_remark || card.endpoint.remark || '暂无备注');
}

const groupOptions = computed(() => {
  const counts = new Map<string, number>();
  containerCards.value.forEach(card => counts.set(hostGroup(card), (counts.get(hostGroup(card)) || 0) + 1));
  return Array.from(counts, ([label, count]) => ({ label, value: label, count }))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label, 'zh-CN'));
});

const visibleGroupOptions = computed(() => {
  if (filtersExpanded.value) return groupOptions.value;
  const options = groupOptions.value.slice(0, filterPreviewLimit);
  if (groupFilter.value !== 'all' && !options.some(option => option.value === groupFilter.value)) {
    const selected = groupOptions.value.find(option => option.value === groupFilter.value);
    if (selected) return [selected, ...options.slice(0, filterPreviewLimit - 1)];
  }
  return options;
});

const filteredCards = computed(() => {
  const query = keyword.value.trim().toLowerCase();
  let items = containerCards.value.filter(card => {
    if (groupFilter.value !== 'all' && hostGroup(card) !== groupFilter.value) return false;
    if (!query) return true;
    return [containerName(card), hostName(card), hostAddress(card), hostGroup(card), hostRemark(card), card.container.image, card.container.state, card.container.status]
      .some(value => String(value || '').toLowerCase().includes(query));
  });
  items = [...items].sort((a, b) => {
    const compared = containerName(a).localeCompare(containerName(b), 'zh-CN', { numeric: true, sensitivity: 'base' });
    const result = compared || hostName(a).localeCompare(hostName(b), 'zh-CN', { numeric: true, sensitivity: 'base' });
    return sortDirection.value === 'asc' ? result : -result;
  });
  return items;
});

const displayedCards = computed(() => {
  const start = (page.value - 1) * pageSize.value;
  return filteredCards.value.slice(start, start + pageSize.value);
});

const filteredLogs = computed(() => {
  const query = logSearch.value.trim().toLowerCase();
  if (!query) return logs.value;
  return logs.value.split('\n').filter(line => line.toLowerCase().includes(query)).join('\n');
});

function setGroupFilter(value: string) {
  groupFilter.value = value;
  page.value = 1;
}

function toggleSort() {
  sortDirection.value = sortDirection.value === 'asc' ? 'desc' : 'asc';
  page.value = 1;
}

async function fetchActiveEndpoints(generation: number): Promise<ContainerEndpointView[]> {
  const endpoints: ContainerEndpointView[] = [];
  let currentPage = 1;
  let total = 0;
  do {
    const response = await apiClient.getContainerEndpoints({ page: currentPage, page_size: 200, status: 'active' });
    if (generation !== inventoryGeneration) return [];
    endpoints.push(...(response.items || []));
    total = response.total || endpoints.length;
    endpointTotal.value = total;
    currentPage += 1;
    if (!response.items?.length) break;
  } while (endpoints.length < total);
  return endpoints;
}

async function loadEndpointContainers(endpoint: ContainerEndpointView, generation: number, force: boolean): Promise<void> {
  const endpointID = String(endpoint.id || '');
  if (!endpointID || generation !== inventoryGeneration) return;
  const cached = inventoryCache.get(endpointID);
  if (!force && cached && Date.now() - cached.updatedAt < inventoryCacheTTL) {
    appendEndpointCards(endpoint, cached.items, generation);
    scannedEndpointCount.value += 1;
    return;
  }

  const controller = new AbortController();
  inventoryControllers.add(controller);
  let timedOut = false;
  const timeout = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, 22_000);
  try {
    const response = await apiClient.listContainers(endpointID, controller.signal);
    if (generation !== inventoryGeneration || controller.signal.aborted) return;
    const items = response.items || [];
    inventoryCache.set(endpointID, { items, updatedAt: Date.now() });
    appendEndpointCards(endpoint, items, generation);
  } catch (error: unknown) {
    if (generation === inventoryGeneration && (!controller.signal.aborted || timedOut)) failedEndpointCount.value += 1;
  } finally {
    clearTimeout(timeout);
    inventoryControllers.delete(controller);
    if (generation === inventoryGeneration) scannedEndpointCount.value += 1;
  }
}

function appendEndpointCards(endpoint: ContainerEndpointView, items: ContainerRecord[], generation: number) {
  if (generation !== inventoryGeneration) return;
  const endpointID = String(endpoint.id || '');
  const cards = items.map(container => ({
    key: `${endpointID}:${container.id}`,
    endpoint,
    container,
  }));
  containerCards.value.push(...cards);
}

async function scanEndpoints(endpoints: ContainerEndpointView[], generation: number, force: boolean) {
  const queue = [...endpoints];
  const workers = Array.from({ length: Math.min(inventoryConcurrency, queue.length) }, async () => {
    while (queue.length && generation === inventoryGeneration) {
      const endpoint = queue.shift();
      if (endpoint) await loadEndpointContainers(endpoint, generation, force);
    }
  });
  await Promise.all(workers);
}

function abortInventoryRequests() {
  inventoryGeneration += 1;
  inventoryControllers.forEach(controller => controller.abort());
  inventoryControllers.clear();
}

async function loadContainerInventory(force = false) {
  inventoryStarted.value = true;
  abortInventoryRequests();
  const generation = inventoryGeneration;
  initialLoading.value = true;
  scanning.value = true;
  inventoryError.value = '';
  endpointTotal.value = 0;
  scannedEndpointCount.value = 0;
  failedEndpointCount.value = 0;
  containerCards.value = [];
  page.value = 1;
  logDrawerVisible.value = false;
  stopLogPolling();
  selectedCard.value = null;
  logs.value = '';
  try {
    const endpoints = await fetchActiveEndpoints(generation);
    if (generation !== inventoryGeneration) return;
    initialLoading.value = false;
    await scanEndpoints(endpoints, generation, force);
  } catch (error: unknown) {
    if (generation === inventoryGeneration) {
      inventoryError.value = error instanceof Error ? error.message : '无法加载容器连接';
    }
  } finally {
    if (generation === inventoryGeneration) {
      initialLoading.value = false;
      scanning.value = false;
    }
  }
}

function logCacheKey(card: QuickContainerCard): string {
  return `${card.endpoint.id || ''}:${card.container.id}`;
}

function selectContainer(card: QuickContainerCard) {
  stopLogPolling();
  selectedCard.value = card;
  logDrawerVisible.value = true;
  logSearch.value = '';
  logs.value = logCache.get(logCacheKey(card)) || '';
  void refreshLogs(logGeneration).then(() => scheduleLogPolling(logGeneration));
}

async function refreshLogs(generation = logGeneration) {
  const card = selectedCard.value;
  const endpointID = String(card?.endpoint.id || '');
  const containerID = String(card?.container.id || '');
  if (!props.active || !logDrawerVisible.value || !card || !endpointID || !containerID || generation !== logGeneration) return;

  logController?.abort();
  const controller = new AbortController();
  logController = controller;
  const timeout = setTimeout(() => controller.abort(), 22_000);
  logsLoading.value = true;
  try {
    const response = await apiClient.getContainerLogs(endpointID, containerID, 300, controller.signal);
    if (generation !== logGeneration || controller.signal.aborted || selectedCard.value?.key !== card.key) return;
    const nextLogs = truncateLogs(response.logs || '');
    logs.value = nextLogs;
    logCache.set(logCacheKey(card), nextLogs);
    while (logCache.size > 20) {
      const oldestKey = logCache.keys().next().value;
      if (!oldestKey) break;
      logCache.delete(oldestKey);
    }
    scrollLogsToBottom();
  } catch (error: unknown) {
    if (generation === logGeneration && !controller.signal.aborted) {
      logs.value = error instanceof Error ? error.message : '读取日志失败';
    }
  } finally {
    clearTimeout(timeout);
    if (logController === controller) {
      logController = null;
      logsLoading.value = false;
    }
  }
}

function truncateLogs(value: string): string {
  if (value.length <= maxLogChars) return value;
  return `[仅展示最后 ${maxLogChars} 个字符]\n${value.slice(-maxLogChars)}`;
}

function scheduleLogPolling(generation: number) {
  if (!props.active || !logDrawerVisible.value || !selectedCard.value || generation !== logGeneration) return;
  logPollingTimer = setTimeout(async () => {
    logPollingTimer = null;
    await refreshLogs(generation);
    scheduleLogPolling(generation);
  }, logPollingInterval);
}

async function refreshLogsNow() {
  if (logPollingTimer) clearTimeout(logPollingTimer);
  logPollingTimer = null;
  const generation = logGeneration;
  await refreshLogs(generation);
  scheduleLogPolling(generation);
}

function stopLogPolling() {
  logGeneration += 1;
  if (logPollingTimer) clearTimeout(logPollingTimer);
  logPollingTimer = null;
  logController?.abort();
  logController = null;
  logsLoading.value = false;
}

function handleLogDrawerClosed() {
  selectedCard.value = null;
  logSearch.value = '';
  logs.value = '';
}

function scrollLogsToBottom() {
  if (logSearch.value) return;
  void nextTick(() => {
    if (logViewer.value) logViewer.value.scrollTop = logViewer.value.scrollHeight;
  });
}

watch(searchInput, value => {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    keyword.value = value;
    page.value = 1;
  }, 180);
});

watch([groupFilter, sortDirection, pageSize], () => { page.value = 1; });
watch(() => filteredCards.value.length, total => {
  const maxPage = Math.max(1, Math.ceil(total / pageSize.value));
  if (page.value > maxPage) page.value = maxPage;
});
watch(() => props.active, active => {
  if (!active) {
    logDrawerVisible.value = false;
    stopLogPolling();
  } else if (!inventoryStarted.value) {
    void loadContainerInventory();
  } else if (logDrawerVisible.value && selectedCard.value) {
    const generation = logGeneration;
    void refreshLogs(generation).then(() => scheduleLogPolling(generation));
  }
});

onMounted(() => {
  if (props.active) void loadContainerInventory();
});
onBeforeUnmount(() => {
  abortInventoryRequests();
  stopLogPolling();
  if (searchTimer) clearTimeout(searchTimer);
});
</script>

<style scoped>
.container-quick-page { overflow: hidden; }
.container-toolbar { flex-wrap: wrap; }
.container-toolbar__actions { display: flex; gap: 8px; }
.container-toolbar__actions .el-button { margin: 0; }
.quick-filter-bar { display: flex; min-width: 240px; flex: 1 1 360px; align-items: center; gap: 6px; }
.quick-filter-options { display: flex; min-width: 0; flex: 1 1 auto; gap: 6px; overflow: hidden; white-space: nowrap; }
.quick-filter-options.is-expanded { flex-wrap: wrap; overflow: visible; white-space: normal; }
.quick-filter-options .el-button, .quick-filter-more { flex: 0 0 auto; margin: 0; }
.quick-filter-options .el-button { padding-inline: 9px; }
.quick-filter-more { padding-inline: 4px; }
.container-workspace { display: flex; height: calc(100vh - 225px); min-height: 620px; background: var(--color-card); }
.container-card-panel { display: flex; flex: 1; min-height: 0; flex-direction: column; border-bottom: 0; background: radial-gradient(circle at 10% 0%, rgb(14 165 233 / 8%), transparent 30%), linear-gradient(180deg, var(--color-surface-muted), var(--color-card)); }
.load-alert { margin: 12px 16px 0; }
.inventory-status { display: flex; min-height: 36px; align-items: center; gap: 12px; padding: 8px 16px 4px; color: var(--color-text-secondary); font-size: 12px; }
.inventory-status strong { color: var(--color-text); }
.inventory-status__warning { color: var(--el-color-warning); }
.inventory-status__scanning { color: var(--el-color-primary); }
.container-card-scroll { min-height: 0; flex: 1; overflow: auto; padding: 8px 16px 12px; }
.container-card-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(245px, 1fr)); gap: 10px; }
.container-card { min-width: 0; border: 1px solid var(--color-border); border-radius: 12px; padding: 11px 12px; background: color-mix(in srgb, var(--color-card) 96%, var(--el-color-primary) 4%); color: inherit; cursor: pointer; text-align: left; box-shadow: 0 5px 15px rgb(15 23 42 / 4%); transition: border-color 160ms ease, box-shadow 160ms ease, transform 160ms ease; }
.container-card:hover { border-color: rgb(14 165 233 / 42%); box-shadow: 0 9px 22px rgb(15 23 42 / 9%); transform: translateY(-1px); }
.container-card.is-selected { border-color: var(--el-color-primary); box-shadow: 0 0 0 2px color-mix(in srgb, var(--el-color-primary) 18%, transparent), 0 9px 22px rgb(15 23 42 / 9%); }
.container-card__topline { display: grid; min-width: 0; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 8px; }
.container-card__topline strong { overflow: hidden; color: var(--color-text); font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }
.container-card__state { width: 9px; height: 9px; border-radius: 50%; background: #94a3b8; box-shadow: 0 0 0 3px rgb(148 163 184 / 14%); }
.container-card__state.running, .container-card__state.ready { background: #22a06b; box-shadow: 0 0 0 3px rgb(34 160 107 / 14%); }
.container-card__state.exited, .container-card__state.stopped { background: #94a3b8; }
.container-card__group { max-width: 92px; overflow: hidden; border: 1px solid var(--color-border); border-radius: 999px; padding: 2px 7px; background: var(--color-surface-muted); color: var(--color-text-secondary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.container-card__facts { display: grid; margin: 10px 0 8px; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.container-card__facts div { min-width: 0; }
.container-card__facts dt { color: var(--color-text-secondary); font-size: 10px; }
.container-card__facts dd { margin: 3px 0 0; overflow: hidden; color: var(--color-text); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.container-card__remark { margin: 0; overflow: hidden; color: var(--color-text-secondary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.container-card-pagination { display: flex; justify-content: flex-end; padding: 8px 16px 12px; }
.container-card-skeletons { display: grid; padding: 12px 16px; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
.container-card-skeletons span { height: 108px; border-radius: 12px; background: linear-gradient(90deg, var(--color-surface-muted), var(--color-border), var(--color-surface-muted)); background-size: 220% 100%; animation: container-skeleton 1.3s linear infinite; }
.container-log-panel { display: flex; width: 100%; height: 100%; min-height: 0; flex-direction: column; background: #111a16; }
:global(.container-log-drawer .el-drawer__body) { padding: 0 !important; overflow: hidden; }
.container-log-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 10px 14px; border-bottom: 1px solid rgb(255 255 255 / 8%); background: #18231e; }
.container-log-title { display: grid; min-width: 0; grid-template-columns: auto auto minmax(0, 1fr); align-items: baseline; gap: 10px; }
.container-log-title span { color: #7f9a8b; font-size: 10px; font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }
.container-log-title strong { color: #e2f1e8; font-size: 13px; }
.container-log-title small { overflow: hidden; color: #8ca397; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.container-log-actions { display: flex; width: min(390px, 48%); align-items: center; gap: 8px; }
.container-log-actions .el-input { min-width: 150px; flex: 1; }
.container-log-close { flex: 0 0 auto; color: #b9d3c2; }
.container-log-viewer { min-height: 0; flex: 1; margin: 0; overflow: auto; padding: 14px 16px; color: #d0e5d6; font: 12px/1.7 Consolas, 'SFMono-Regular', monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.container-log-empty { flex: 1; --el-empty-fill-color-2: #26362e; --el-text-color-secondary: #789083; }
@keyframes container-skeleton { to { background-position: -220% 0; } }
@media (max-width: 900px) { .container-workspace { height: calc(100vh - 285px); min-height: 560px; } .container-card-skeletons { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 680px) { .container-workspace { height: calc(100vh - 335px); min-height: 520px; } .container-toolbar__actions { width: 100%; } .container-toolbar__actions .el-button { flex: 1; } .container-card-grid { grid-template-columns: minmax(0, 1fr); } .inventory-status { align-items: flex-start; flex-direction: column; gap: 2px; } .container-log-header { align-items: stretch; flex-direction: column; } .container-log-title { grid-template-columns: auto minmax(0, 1fr); } .container-log-title small { grid-column: 1 / -1; } .container-log-actions { width: 100%; } .container-card-pagination { overflow-x: auto; justify-content: flex-start; } }
</style>
