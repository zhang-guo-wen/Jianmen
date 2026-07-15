<template>
  <div class="view-stack audit-view">
    <el-tabs v-model="auditScope" class="page-tabs">
      <el-tab-pane v-if="permission.canDo('audit:view')" :label="t('audit.scope.ssh')" name="ssh">
        <el-alert v-if="sessionError" :title="sessionError" type="error" show-icon style="margin-bottom: 12px" />
        <div class="page-container">
          <DataTableCard
            :data="sessions"
            :loading="sessionsLoading"
            :total="sessionTotal"
            v-model:page="sessionPage"
            v-model:page-size="sessionPageSize"
            v-model:search="sessionKeyword"
            search-placeholder="搜索会话..."
            @search="onSessionSearch"
          >
            <template #toolbar-extra>
              <el-button :loading="sessionsLoading" :icon="Refresh" @click="loadSessions">{{ t('common.refresh') }}</el-button>
            </template>
            <el-table-column :label="t('audit.column.instance')" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">
                {{ sessionInstance(row) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.account')" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">
                {{ sessionAccount(row) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.operator')" min-width="120">
              <template #default="{ row }">
                {{ sessionUser(row) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.protocol')" width="90">
              <template #default="{ row }">
                <el-tag :type="sessionProtocolTag(row)" size="small" effect="plain">{{ sessionProtocol(row) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('sessions.column.started')" width="170" show-overflow-tooltip class-name="col-time">
              <template #default="{ row }">
                {{ formatTime(row.started_at ?? row.startedAt) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.duration')" width="90">
              <template #default="{ row }">
                {{ formatDurationSeconds(computeDuration(row.started_at, row.ended_at)) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.logCount')" width="90" align="center">
              <template #default="{ row }">{{ row.log_count ?? 0 }}</template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" fixed="right" width="180">
              <template #default="{ row }">
                <el-button :disabled="!hasReplay(row)" link type="success" @click="loadSessionArtifact(row, 'replay')">
                  {{ t('audit.action.replay') }}
                </el-button>
                <el-button link type="primary" @click="loadSessionLog(row)">
                  {{ t('audit.action.log') }}
                </el-button>
              </template>
            </el-table-column>
          </DataTableCard>
        </div>
      </el-tab-pane>
      <el-tab-pane v-if="permission.canDo('db:audit:view')" :label="t('audit.scope.db')" name="db">
        <el-alert v-if="dbError" :title="dbError" type="warning" show-icon style="margin-bottom: 12px" />
        <div class="page-container">
          <DataTableCard
            :data="dbConnections"
            :loading="dbLoading"
            :total="dbTotal"
            v-model:page="dbPage"
            v-model:page-size="dbPageSize"
            v-model:search="dbKeyword"
            search-placeholder="搜索数据库连接..."
            @search="onDBSearch"
          >
            <template #toolbar-extra>
              <el-button :loading="dbLoading" :icon="Refresh" @click="loadDBConnections">{{ t('common.refresh') }}</el-button>
            </template>
            <el-table-column :label="t('audit.column.instance')" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ row.target_name || row.upstream_addr || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.account')" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">{{ row.account_name || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.operator')" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">{{ row.username || row.name || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.protocol')" width="110">
              <template #default="{ row }">
                <el-tag :type="databaseProtocolTag(row.protocol)" size="small" effect="plain">
                  {{ formatDatabaseProtocol(row.protocol) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('sessions.column.started')" min-width="170" show-overflow-tooltip class-name="col-time">
              <template #default="{ row }">
                {{ formatTime(row.started_at) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.duration')" width="100">
              <template #default="{ row }">
                {{ formatDuration(row.duration_ms ?? computeDurationMs(row.started_at, row.ended_at)) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.logCount')" width="90" align="center">
              <template #default="{ row }">{{ row.log_count ?? 0 }}</template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" fixed="right" width="90">
              <template #default="{ row }">
                <el-button link type="primary" @click="loadDBArtifact(row, 'queries')">
                  {{ t('audit.action.queries') }}
                </el-button>
              </template>
            </el-table-column>
          </DataTableCard>
        </div>
      </el-tab-pane>
      <el-tab-pane v-if="permission.canDo('session:view')" :label="t('audit.scope.online')" name="online">
        <el-alert v-if="onlineError" :title="onlineError" type="warning" show-icon style="margin-bottom: 12px" />
        <div class="page-container">
          <DataTableCard
            :data="onlineSessions"
            :loading="onlineLoading"
            :total="onlineTotal"
            v-model:page="onlinePage"
            v-model:page-size="onlinePageSize"
            v-model:search="onlineKeyword"
            :search-placeholder="t('audit.search.online')"
            @search="onOnlineSearch"
          >
            <template #toolbar-extra>
              <el-button :loading="onlineLoading" :icon="Refresh" @click="loadOnlineSessions">{{ t('common.refresh') }}</el-button>
            </template>
            <el-table-column :label="t('audit.column.instance')" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">{{ row.instance || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.protocol')" width="110">
              <template #default="{ row }">
                <el-tag :type="onlineProtocolTag(row)" size="small" effect="plain">
                  {{ onlineProtocol(row) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.account')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ row.account || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.operator')" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">{{ row.operator || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('sessions.column.started')" width="170" show-overflow-tooltip class-name="col-time">
              <template #default="{ row }">{{ formatTime(row.started_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" fixed="right" width="210">
              <template #default="{ row }">
                <el-button :disabled="!row.has_replay" link type="success" @click="loadOnlineReplay(row)">
                  {{ t('audit.action.replay') }}
                </el-button>
                <el-button link type="primary" @click="loadOnlineLog(row)">
                  {{ t('audit.action.log') }}
                </el-button>
                <el-button
                  v-if="permission.canDo('session:disconnect')"
                  :loading="disconnectingSessionID === row.id"
                  link
                  type="danger"
                  @click="disconnectOnlineSession(row)"
                >
                  {{ t('audit.action.disconnect') }}
                </el-button>
              </template>
            </el-table-column>
          </DataTableCard>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-drawer
      v-model="drawerVisible"
      direction="rtl"
      size="65%"
      @close="closeDetail"
    >
      <template #title>
        <div class="toolbar">
          <span>{{ detailTitle || t('audit.title.detail') }}</span>
          <el-tag v-if="detailKind">{{ detailKind === 'queries' ? t('audit.action.queries') : detailKind }}</el-tag>
        </div>
      </template>
      <el-alert v-if="detailError" :title="detailError" type="error" show-icon />
      <div v-else v-loading="detailLoading" class="drawer-content">
        <el-descriptions v-if="isDBMeta" :column="2" border>
          <el-descriptions-item :label="t('common.id')">{{ dbMeta.id || t('common.none') }}</el-descriptions-item>
          <el-descriptions-item :label="t('common.name')">{{ dbMeta.name || t('common.none') }}</el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.protocol')">
            {{ dbMeta.protocol || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.authUser')">
            {{ dbMeta.username || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.database')">
            {{ dbMeta.database || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.application')">
            {{ dbMeta.application_name || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.client')">
            {{ dbMeta.client_addr || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.upstream')">
            {{ dbMeta.upstream_addr || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.allowedUsers')" :span="2">
            <el-tag :type="dbMeta.allowed_users_enforced ? 'success' : 'info'">
              {{ dbMeta.allowed_users_enforced ? t('common.enabled') : t('common.disabled') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.observation')" :span="2">
            {{ dbMeta.auth_observation || t('common.none') }}
          </el-descriptions-item>
        </el-descriptions>

        <div v-else-if="isReplay" class="replay-panel">
          <div class="replay-controls">
            <div class="replay-meta">
              <span>{{ formatReplayDuration(replayCurrentTime) }}</span>
              <el-slider
                v-model="replaySeekPercent"
                :max="100"
                :show-tooltip="false"
                :disabled="!replayFrames.length"
                size="small"
                @change="seekReplay"
              />
              <span>{{ formatReplayDuration(replayDuration) }}</span>
            </div>
            <div class="replay-actions">
              <el-button :disabled="!replayOutputFrames.length" type="primary" size="small" @click="playReplay">
                {{ replayPlaying ? t('audit.action.restart') : t('audit.action.play') }}
              </el-button>
              <el-button :disabled="!replayPlaying" size="small" @click="stopReplay">
                {{ t('audit.action.stop') }}
              </el-button>
              <el-select
                v-model="playbackSpeed"
                size="small"
                :disabled="replayPlaying"
                style="width: 64px"
              >
                <el-option
                  v-for="s in speedOptions"
                  :key="s"
                  :label="`${s}x`"
                  :value="s"
                />
              </el-select>
            </div>
          </div>
          <div class="replay-meta-secondary">
            <span>{{ t('audit.replay.frames') }} {{ replayFrames.length }}</span>
            <span>{{ t('audit.replay.outputFrames') }} {{ replayOutputFrames.length }}</span>
            <span>{{ t('audit.replay.size') }} {{ formatBytes(replayRawBytes) }}</span>
          </div>
          <div class="replay-terminal-shell">
            <div ref="replayTerminalHostRef" class="replay-terminal" />
            <div v-if="replayTerminalMessage" class="replay-terminal-empty">
              {{ replayTerminalMessage }}
            </div>
          </div>
        </div>

        <DataTableCard
          v-else-if="isDBQueries"
          :key="`queries-${logSearchVersion}`"
          :data="pagedQueryEvents"
          :total="filteredQueryEvents.length"
          :search-placeholder="t('audit.search.sqlLog')"
          v-model:page="logPage"
          v-model:page-size="logPageSize"
          @search="onLogSearch"
        >
          <el-table-column :label="t('audit.column.time')" width="170" show-overflow-tooltip class-name="col-time">
            <template #default="{ row }">
              {{ formatTime(row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column prop="sql" :label="t('audit.column.sql')" min-width="420" show-overflow-tooltip />
          <el-table-column :label="t('audit.column.duration')" width="100">
            <template #default="{ row }">
              {{ formatDuration(row.duration_ms) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.result')" width="110">
            <template #default="{ row }">
              <el-tag :type="queryStatusType(row.status)" size="small" effect="plain">
                {{ queryStatusLabel(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
        </DataTableCard>

        <DataTableCard
          v-else-if="isCommands"
          :key="`commands-${logSearchVersion}`"
          :data="pagedCommandEvents"
          :total="filteredCommandEvents.length"
          :search-placeholder="t('audit.search.commandLog')"
          v-model:page="logPage"
          v-model:page-size="logPageSize"
          @search="onLogSearch"
        >
          <el-table-column :label="t('audit.column.time')" width="175" show-overflow-tooltip class-name="col-time">
            <template #default="{ row }">
              {{ formatTime(row.timestamp ?? row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column prop="command" :label="t('audit.column.command')" min-width="280" show-overflow-tooltip />
          <el-table-column prop="output" :label="t('audit.column.output')" min-width="280" show-overflow-tooltip />
        </DataTableCard>

        <DataTableCard
          v-else-if="isFiles"
          :data="pagedFileEvents"
          :total="fileEvents.length"
          :show-search="false"
          v-model:page="logPage"
          v-model:page-size="logPageSize"
        >
          <el-table-column :label="t('audit.column.time')" width="175" show-overflow-tooltip class-name="col-time">
            <template #default="{ row }">
              {{ formatTime(row.timestamp ?? row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.action')" width="80">
            <template #default="{ row }">
              {{ formatFileAction(row.action) }}
            </template>
          </el-table-column>
          <el-table-column prop="path" :label="t('audit.column.path')" min-width="420" show-overflow-tooltip />
          <el-table-column :label="t('audit.column.result')" width="75">
            <template #default="{ row }">
              <el-tag :type="row.result === 'success' ? 'success' : 'danger'" size="small">
                {{ row.result === 'success' ? t('audit.result.success') : t('audit.result.failure') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.size')" width="75">
            <template #default="{ row }">
              <template v-if="row.size > 0">{{ formatBytes(row.size) }}</template>
            </template>
          </el-table-column>
        </DataTableCard>

        <el-empty v-else :description="t('audit.empty.detail')" />
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { Refresh } from '@element-plus/icons-vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { useRoute } from 'vue-router';

import DataTableCard from '@/components/DataTableCard.vue';
import {
  apiClient,
  type DBConnectionMetaRecord,
  type DBConnectionRecord,
  type DBQueryEventRecord,
  type OnlineSessionRecord,
  type SessionCommandRecord,
  type SessionFileEventRecord,
  type SessionRecord
} from '@/api/client';
import { useI18n } from '@/i18n';
import { usePermissionStore } from '@/stores/permission';

type AuditScope = 'ssh' | 'db' | 'online';
type DetailKind = '' | 'meta' | 'commands' | 'files' | 'file-summary' | 'queries' | 'replay';
type ReplayFrame = {
  time: number;
  stream: string;
  data: string;
};
type ReplayData = {
  header: Record<string, unknown>;
  frames: ReplayFrame[];
  raw: string;
};

const { t } = useI18n();
const permission = usePermissionStore();
const route = useRoute();

function routeQueryValue(value: unknown): string {
  if (Array.isArray(value)) return routeQueryValue(value[0]);
  return typeof value === 'string' ? value.trim() : '';
}

function permittedAuditScope(value: unknown): AuditScope {
  const requested = routeQueryValue(value);
  if (requested === 'online' && permission.canDo('session:view')) return 'online';
  if (requested === 'db' && permission.canDo('db:audit:view')) return 'db';
  if (requested === 'ssh' && permission.canDo('audit:view')) return 'ssh';
  if (permission.canDo('audit:view')) return 'ssh';
  if (permission.canDo('db:audit:view')) return 'db';
  return 'online';
}

const initialAuditScope = permittedAuditScope(route.query.scope);
const initialAuditKeyword = routeQueryValue(route.query.q);
const auditScope = ref<AuditScope>(initialAuditScope);
const initialOnlineResourceType = initialAuditScope === 'online' ? routeQueryValue(route.query.resource_type) : '';
const initialOnlineResourceID = initialAuditScope === 'online' ? routeQueryValue(route.query.resource_id) : '';

// ── SSH session list state ──
const sessions = ref<SessionRecord[]>([]);
const sessionTotal = ref(0);
const sessionPage = ref(1);
const sessionPageSize = ref(20);
const sessionKeyword = ref(initialAuditScope === 'ssh' ? initialAuditKeyword : '');
const sessionsLoading = ref(false);
const sessionError = ref('');

// ── DB connection list state ──
const dbConnections = ref<DBConnectionRecord[]>([]);
const dbTotal = ref(0);
const dbPage = ref(1);
const dbPageSize = ref(20);
const dbKeyword = ref(initialAuditScope === 'db' ? initialAuditKeyword : '');
const dbLoading = ref(false);
const dbError = ref('');

// Online session list state
const onlineSessions = ref<OnlineSessionRecord[]>([]);
const onlineTotal = ref(0);
const onlinePage = ref(1);
const onlinePageSize = ref(20);
const onlineKeyword = ref(initialAuditScope === 'online' ? initialAuditKeyword : '');
const onlineResourceType = ref(initialOnlineResourceType);
const onlineResourceID = ref(initialOnlineResourceID);
const onlineLoading = ref(false);
const onlineError = ref('');
const disconnectingSessionID = ref('');
let onlineRefreshTimer: number | undefined;

// ── Drawer state ──
const detailLoading = ref(false);
const detailError = ref('');
const detailTitle = ref('');
const detailKind = ref<DetailKind>('');
const detailData = ref<unknown>(null);
const drawerVisible = ref(false);
const logPage = ref(1);
const logPageSize = ref(30);
const logKeyword = ref('');
const logSearchVersion = ref(0);

// ── Replay state ──
const playbackSpeed = ref(1);
const speedOptions = [1, 2, 4, 8];
const replayPlaying = ref(false);
const replayProgress = ref(0);
const replaySeekPercent = ref(0);
const replayCurrentTime = ref(0);
const replayRenderedOutput = ref(false);
const replayTerminalHostRef = ref<HTMLElement>();
let replayTerminal: Terminal | undefined;
let replayTimer: number | undefined;
let replayStartedAt = 0;
let replayStartOffset = 0;
let replayFrameIndex = 0;

// ── Computed ──
const isDBMeta = computed(() => detailKind.value === 'meta' && isRecord(detailData.value) && 'protocol' in detailData.value);
function hasItems(data: unknown): boolean {
  if (Array.isArray(data)) return true;
  if (data && typeof data === 'object' && 'items' in data && Array.isArray((data as Record<string, unknown>).items)) return true;
  return false;
}
const isDBQueries = computed(() => detailKind.value === 'queries' && hasItems(detailData.value));
const isCommands = computed(() => detailKind.value === 'commands' && hasItems(detailData.value));
const isFiles = computed(() => detailKind.value === 'files' && hasItems(detailData.value));
const isReplay = computed(() => detailKind.value === 'replay' && isReplayData(detailData.value));
const dbMeta = computed(() => (isRecord(detailData.value) ? (detailData.value as DBConnectionMetaRecord) : {}));
const queryEvents = computed(() => extractItems<DBQueryEventRecord>(detailData.value));

// 合并 start/finish 事件为一行
interface MergedQueryEvent {
  seq: number;
  sql: string;
  comment: string;
  query_kind: string;
  status: string;
  duration_ms: number;
  started_at: number;
  error_code?: string;
  error_message?: string;
}
function splitSQLComment(sql: string): { comment: string; sql: string } {
  // 提取 MySQL /++ ... +/ 风格注释，作为来源信息
  const m = /^\/\*\s*(.+?)\s*\*\/\s*/.exec(sql);
  if (m) {
    return { comment: m[1], sql: sql.slice(m[0].length) };
  }
  return { comment: '', sql };
}
const mergedQueryEvents = computed<MergedQueryEvent[]>(() => {
  const map = new Map<number, MergedQueryEvent>();
  for (const ev of queryEvents.value) {
    const seq = ev.seq ?? 0;
    const cur = map.get(seq) ?? { seq, sql: '', comment: '', query_kind: ev.query_kind ?? '', status: 'unknown', duration_ms: 0, started_at: ev.started_at ?? 0 };
    if (ev.type === 'query_started') {
      const parsed = splitSQLComment(ev.sql || cur.sql);
      cur.sql = parsed.sql || cur.sql;
      cur.comment = parsed.comment || cur.comment;
      cur.query_kind = ev.query_kind || cur.query_kind;
      cur.started_at = ev.started_at ?? cur.started_at;
    } else {
      cur.status = ev.status ?? cur.status;
      cur.duration_ms = ev.duration_ms ?? cur.duration_ms;
      cur.error_code = ev.error_code;
      cur.error_message = ev.error_message;
      // 如果没有 duration_ms，用 started_at 和 completed_at 计算
      if (!cur.duration_ms && cur.started_at && ev.completed_at) {
        cur.duration_ms = ev.completed_at - cur.started_at;
      }
    }
    map.set(seq, cur);
  }
  return Array.from(map.values()).sort((a, b) => a.seq - b.seq);
});
function extractItems<T>(data: unknown): T[] {
  if (Array.isArray(data)) return data as T[];
  if (data && typeof data === 'object' && 'items' in data && Array.isArray((data as Record<string, unknown>).items)) {
    return (data as Record<string, unknown>).items as T[];
  }
  return [];
}
const commandEvents = computed(() => extractItems<SessionCommandRecord>(detailData.value));
const fileEvents = computed(() => extractItems<SessionFileEventRecord>(detailData.value));
const normalizedLogKeyword = computed(() => logKeyword.value.trim().toLowerCase());
const filteredQueryEvents = computed(() => {
  if (!normalizedLogKeyword.value) return mergedQueryEvents.value;
  return mergedQueryEvents.value.filter((event) => event.sql.toLowerCase().includes(normalizedLogKeyword.value));
});
const filteredCommandEvents = computed(() => {
  if (!normalizedLogKeyword.value) return commandEvents.value;
  return commandEvents.value.filter((event) => String(event.command ?? '').toLowerCase().includes(normalizedLogKeyword.value));
});

// Client-side pagination for drawer sub-tables
const pagedQueryEvents = computed(() => {
  const start = (logPage.value - 1) * logPageSize.value;
  return filteredQueryEvents.value.slice(start, start + logPageSize.value);
});
const pagedCommandEvents = computed(() => {
  const start = (logPage.value - 1) * logPageSize.value;
  return filteredCommandEvents.value.slice(start, start + logPageSize.value);
});
const pagedFileEvents = computed(() => {
  const start = (logPage.value - 1) * logPageSize.value;
  return fileEvents.value.slice(start, start + logPageSize.value);
});

const replayData = computed(() => (isReplayData(detailData.value) ? detailData.value : { header: {}, frames: [], raw: '' }));
const replayFrames = computed(() => replayData.value.frames);
const replayOutputFrames = computed(() => replayFrames.value.filter((frame) => frame.stream === 'o'));
const replayDuration = computed(() => replayFrames.value.at(-1)?.time ?? 0);
const replayRawBytes = computed(() => utf8ByteLength(replayData.value.raw));
const replayFirstOutputTime = computed(() => replayOutputFrames.value[0]?.time ?? 0);
const replayTerminalCols = computed(() => replayHeaderNumber('width', 120, 20, 240));
const replayTerminalRows = computed(() => replayHeaderNumber('height', 24, 8, 60));
const replayTerminalMessage = computed(() => {
  if (!isReplay.value) {
    return '';
  }
  if (!replayFrames.value.length) {
    return t('audit.empty.replay');
  }
  if (!replayOutputFrames.value.length) {
    return t('audit.empty.replayNoOutput');
  }
  if (replayPlaying.value && !replayRenderedOutput.value) {
    return t('audit.empty.replayWaiting');
  }
  return '';
});

// ── Helpers ──

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isReplayData(value: unknown): value is ReplayData {
  return isRecord(value) && Array.isArray(value.frames) && typeof value.raw === 'string';
}

function sessionId(session: SessionRecord): string {
  return String(session.id ?? '');
}

function displayAuditIdentity(actualValue: unknown, displayNameValue: unknown): string {
  const actual = String(actualValue ?? '').trim();
  const displayName = String(displayNameValue ?? '').trim();
  if (!actual) return displayName || t('common.none');
  if (!displayName || displayName === actual) return actual;
  return `${actual}（${displayName}）`;
}

function sessionInstance(session: SessionRecord): string {
  return displayAuditIdentity(session.target_address ?? session.target_id, session.target_name);
}

function sessionAccount(session: SessionRecord): string {
  return displayAuditIdentity(session.account_username, session.account_name);
}

function sessionUser(session: SessionRecord): string {
  return String(session.username ?? session.user_username ?? session.user_id ?? t('common.none'));
}

function hasReplay(session: SessionRecord): boolean {
  return typeof session.replay_dir === 'string' && session.replay_dir.length > 0;
}

function formatTime(value: unknown): string {
  let d: Date | null = null
  if (typeof value === 'number' && Number.isFinite(value)) {
    d = new Date(value)
  } else if (typeof value === 'string' && value.trim()) {
    const parsed = Date.parse(value)
    if (!Number.isNaN(parsed)) d = new Date(parsed)
  }
  if (!d || Number.isNaN(d.getTime())) return t('common.none')
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function formatDuration(value: unknown): string {
  if (value === undefined || value === null) return t('common.none');
  const n = Number(value);
  if (!Number.isFinite(n)) return t('common.none');
  // n is milliseconds
  if (n < 1000) return `${Math.round(n)}ms`;
  const totalSeconds = n / 1000;
  if (totalSeconds < 60) return `${Math.round(totalSeconds * 10) / 10}s`;
  const mins = Math.floor(totalSeconds / 60);
  const secs = Math.round(totalSeconds % 60);
  return `${mins}m ${secs}s`;
}

function formatDurationSeconds(value: unknown): string {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return t('common.none');
  }
  if (value < 60) {
    return `${Math.round(value)}s`;
  }
  const mins = Math.floor(value / 60);
  const secs = Math.round(value % 60);
  return `${mins}m ${secs}s`;
}

function computeDuration(started_at: unknown, ended_at: unknown): number {
  const s = toTimestamp(started_at);
  const e = toTimestamp(ended_at);
  if (s && e && e > s) return (e - s) / 1000;
  return 0;
}

function computeDurationMs(started_at: unknown, ended_at: unknown): number {
  const s = toTimestamp(started_at);
  const e = toTimestamp(ended_at);
  if (s && e && e > s) return e - s;
  return 0;
}

function toTimestamp(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const parsed = Date.parse(value);
    if (!Number.isNaN(parsed)) return parsed;
  }
  return null;
}

function sessionProtocol(row: SessionRecord): string {
  const subtype = row.protocol_subtype || '';
  if (subtype === 'web-terminal') return 'Web';
  if (subtype === 'sftp') return 'SFTP';
  if (subtype === 'scp') return 'SCP';
  if (!subtype && !hasReplay(row)) return 'SFTP';
  return 'SSH';
}

function sessionProtocolTag(row: SessionRecord): 'success' | 'warning' | 'info' | 'danger' | '' {
  const subtype = row.protocol_subtype || '';
  if (subtype === 'web-terminal') return 'info';
  if (subtype === 'sftp') return 'warning';
  if (subtype === 'scp') return 'warning';
  if (!subtype && !hasReplay(row)) return 'warning';
  return 'success';
}

function formatDatabaseProtocol(protocol: unknown): string {
  switch (String(protocol ?? '').toLowerCase()) {
    case 'mysql':
      return 'MySQL';
    case 'postgres':
    case 'postgresql':
      return 'PostgreSQL';
    case 'redis':
      return 'Redis';
    default:
      return String(protocol || '-');
  }
}

function databaseProtocolTag(protocol: unknown): 'primary' | 'success' | 'warning' | 'info' | 'danger' | '' {
  switch (String(protocol ?? '').toLowerCase()) {
    case 'mysql':
      return 'warning';
    case 'postgres':
    case 'postgresql':
      return 'primary';
    case 'redis':
      return 'danger';
    default:
      return 'info';
  }
}

function formatReplayDuration(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0s';
  }
  if (value < 60) {
    return `${value.toFixed(1)}s`;
  }
  const minutes = Math.floor(value / 60);
  const seconds = Math.round(value % 60);
  return `${minutes}m ${seconds}s`;
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0 B';
  }
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

function queryStatusLabel(status: unknown): string {
  switch (String(status ?? '').toLowerCase()) {
    case 'success':
      return '成功';
    case 'error':
      return '失败';
    case 'policy_denied':
      return '拒绝';
    default:
      return '未知';
  }
}

function queryStatusType(status: unknown): 'success' | 'warning' | 'danger' | 'info' {
  switch (String(status ?? '').toLowerCase()) {
    case 'success':
      return 'success';
    case 'error':
    case 'policy_denied':
      return 'danger';
    case 'unknown':
      return 'warning';
    default:
      return 'info';
  }
}

function setDetail(title: string, kind: DetailKind, data: unknown) {
  stopReplay();
  detailTitle.value = title;
  detailKind.value = kind;
  detailData.value = data;
  drawerVisible.value = true;
  playbackSpeed.value = 1;
  logPage.value = 1;
  logKeyword.value = '';
  logSearchVersion.value++;
  replayProgress.value = 0;
  replaySeekPercent.value = 0;
  replayCurrentTime.value = 0;
  replayRenderedOutput.value = false;
  resetReplayTerminal();
}

function closeDetail() {
  stopReplay();
  drawerVisible.value = false;
  playbackSpeed.value = 1;
}

// ── Data fetching ──

async function loadOnlineSessions() {
  onlineLoading.value = true;
  onlineError.value = '';
  try {
    const res = await apiClient.getOnlineSessions({
      page: onlinePage.value,
      page_size: onlinePageSize.value,
      q: onlineKeyword.value || undefined,
      resource_type: onlineResourceType.value || undefined,
      resource_id: onlineResourceID.value || undefined,
    });
    onlineSessions.value = res.items ?? [];
    onlineTotal.value = res.total ?? 0;
  } catch (err) {
    onlineSessions.value = [];
    onlineError.value = err instanceof Error ? err.message : t('audit.error.loadOnline');
  } finally {
    onlineLoading.value = false;
  }
}

async function loadSessions() {
  sessionsLoading.value = true;
  sessionError.value = '';

  try {
    const res = await apiClient.getSessions({
      page: sessionPage.value,
      page_size: sessionPageSize.value,
      q: sessionKeyword.value || undefined,
    });
    sessions.value = res.items ?? [];
    sessionTotal.value = res.total ?? 0;
  } catch (err) {
    sessionError.value = err instanceof Error ? err.message : t('sessions.loadError');
  } finally {
    sessionsLoading.value = false;
  }
}

async function loadDBConnections() {
  dbLoading.value = true;
  dbError.value = '';

  try {
    const res = await apiClient.getDBConnections({
      page: dbPage.value,
      page_size: dbPageSize.value,
      q: dbKeyword.value || undefined,
    });
    dbConnections.value = res.items ?? [];
    dbTotal.value = res.total ?? 0;
  } catch (err) {
    dbConnections.value = [];
    dbError.value = err instanceof Error ? err.message : t('audit.error.loadDBConnections');
  } finally {
    dbLoading.value = false;
  }
}

function onLogSearch(q: string) {
  logKeyword.value = q;
  logPage.value = 1;
}

function onOnlineSearch(q: string) {
  onlineKeyword.value = q;
  onlinePage.value = 1;
  void loadOnlineSessions();
}

function onSessionSearch(q: string) {
  sessionKeyword.value = q;
  sessionPage.value = 1;
  loadSessions();
}

function onDBSearch(q: string) {
  dbKeyword.value = q;
  dbPage.value = 1;
  loadDBConnections();
}

// ── Session artifacts ──

async function loadSessionArtifact(session: SessionRecord, kind: Exclude<DetailKind, '' | 'queries'>) {
  const id = sessionId(session);

  if (!id) {
    ElMessage.error(t('audit.error.missingSession'));
    return;
  }

  detailLoading.value = true;
  detailError.value = '';

  try {
    if (kind === 'meta') {
      setDetail(`${t('audit.scope.ssh')} ${id}`, kind, await apiClient.getSessionMeta(id));
    } else if (kind === 'replay') {
      setDetail(`${t('audit.action.replay')} ${id}`, kind, parseReplayCast(await apiClient.getSessionReplay(id)));
      await nextTick();
      playReplay();
    } else if (kind === 'commands') {
      setDetail(`${t('audit.action.commands')} ${id}`, kind, await apiClient.getSessionCommands(id));
    } else if (kind === 'files') {
      setDetail(`${t('audit.action.files')} ${id}`, kind, await apiClient.getSessionFiles(id));
    } else {
      setDetail(`${t('audit.action.summary')} ${id}`, kind, await apiClient.getSessionFileSummary(id));
    }
  } catch (err) {
    detailError.value = err instanceof Error ? err.message : t('audit.error.loadArtifact');
  } finally {
    detailLoading.value = false;
  }
}

function formatFileAction(action: string): string {
  const map: Record<string, string> = {
    realpath: '解析路径',
    list: '列目录',
    open_read: '打开读取',
    open_write: '打开写入',
    read: '读取',
    write: '写入',
    close: '关闭',
    remove: '删除',
    rename: '重命名',
    mkdir: '创建目录',
    rmdir: '删除目录',
    stat: '查看属性',
    setstat: '设属性',
    fstat: '文件属性',
    fsetstat: '设文件属性',
    opendir: '打开目录',
    readdir: '读目录',
    readlink: '读链接',
    symlink: '创建链接',
  };
  return map[action] || action;
}

function isSFTP(row: SessionRecord): boolean {
  if (row.protocol_subtype === 'sftp') return true;
  if (!row.protocol_subtype && !hasReplay(row)) return true;
  return false;
}

function loadSessionLog(session: SessionRecord) {
  if (isSFTP(session)) {
    void loadSessionArtifact(session, 'files');
  } else {
    void loadSessionArtifact(session, 'commands');
  }
}

// ── Replay ──

function parseReplayCast(raw: string): ReplayData {
  const lines = raw.split(/\r?\n/).filter((line) => line.trim().length > 0);
  const header = parseReplayHeader(lines[0]);
  const frames: ReplayFrame[] = [];

  for (const line of lines.slice(1)) {
    try {
      const row = JSON.parse(line) as unknown[];
      const time = typeof row[0] === 'number' ? row[0] : Number(row[0]);
      const stream = typeof row[1] === 'string' ? row[1] : '';
      const data = typeof row[2] === 'string' ? row[2] : '';
      if (Number.isFinite(time) && data) {
        frames.push({ time: Math.max(0, time), stream, data });
      }
    } catch {
      // Skip malformed rows so one bad line does not break playback.
    }
  }

  frames.sort((a, b) => a.time - b.time);
  return { header, frames, raw };
}

function parseReplayHeader(line: string | undefined): Record<string, unknown> {
  if (!line) {
    return {};
  }
  try {
    const value = JSON.parse(line);
    return isRecord(value) ? value : {};
  } catch {
    return {};
  }
}

function playReplay() {
  const frames = replayFrames.value;
  stopReplay();
  const terminal = ensureReplayTerminal();
  terminal?.reset();
  replayProgress.value = 0;
  replaySeekPercent.value = 0;
  replayCurrentTime.value = 0;
  replayRenderedOutput.value = false;
  replayStartOffset = replayFirstOutputTime.value > 0 ? Math.max(0, replayFirstOutputTime.value - 0.2) : 0;
  replayFrameIndex = Math.max(
    0,
    frames.findIndex((frame) => frame.time >= replayStartOffset)
  );

  if (!frames.length || !replayOutputFrames.value.length) {
    return;
  }

  replayPlaying.value = true;
  replayStartedAt = performance.now();
  tickReplay();
}

function stopReplay() {
  if (replayTimer !== undefined) {
    window.clearTimeout(replayTimer);
    replayTimer = undefined;
  }
  cancelAutoScroll();
  replayPlaying.value = false;
}

let scrollRafId: number | undefined;

function autoScrollTerminal() {
  // Cancel any pending scroll — only the latest position matters
  if (scrollRafId !== undefined) {
    cancelAnimationFrame(scrollRafId);
  }
  scrollRafId = requestAnimationFrame(() => {
    const host = replayTerminalHostRef.value;
    if (!host) return;
    const viewport = host.querySelector('.xterm-viewport') as HTMLElement | null;
    if (!viewport) return;
    viewport.scrollTop = viewport.scrollHeight;
    // xterm may batch DOM updates — confirm on the next frame
    scrollRafId = requestAnimationFrame(() => {
      scrollRafId = undefined;
      viewport.scrollTop = viewport.scrollHeight;
    });
  });
}

function cancelAutoScroll() {
  if (scrollRafId !== undefined) {
    cancelAnimationFrame(scrollRafId);
    scrollRafId = undefined;
  }
}

function seekReplay(percent: number) {
  const frames = replayFrames.value;
  const duration = Math.max(replayDuration.value, 0.1);
  const targetTime = (percent / 100) * duration;

  // Find target frame index
  const targetIndex = frames.findIndex((f) => f.time >= targetTime);
  const idx = targetIndex >= 0 ? targetIndex : frames.length;

  const wasPlaying = replayPlaying.value;
  stopReplay();

  // Reset terminal and fast-forward
  const terminal = ensureReplayTerminal();
  terminal?.reset();
  replayRenderedOutput.value = false;

  for (let i = 0; i < idx; i++) {
    if (frames[i].stream === 'o') {
      terminal?.write(frames[i].data);
      replayRenderedOutput.value = true;
    }
  }

  autoScrollTerminal();

  // Update state
  replayFrameIndex = idx;
  replayProgress.value = percent;
  replaySeekPercent.value = percent;
  replayCurrentTime.value = targetTime;
  replayStartOffset = targetTime;

  if (wasPlaying) {
    replayPlaying.value = true;
    replayStartedAt = performance.now();
    tickReplay();
  }
}

function tickReplay() {
  if (!replayPlaying.value) {
    return;
  }

  const frames = replayFrames.value;
  const speed = playbackSpeed.value;
  const elapsed = ((performance.now() - replayStartedAt) / 1000) * speed + replayStartOffset;
  while (replayFrameIndex < frames.length && frames[replayFrameIndex].time <= elapsed) {
    appendReplayOutput(frames[replayFrameIndex]);
    replayFrameIndex++;
  }

  // Keep viewport at bottom so new output is always visible
  autoScrollTerminal();

  const duration = Math.max(replayDuration.value, 0.1);
  const pct =
    replayFrameIndex >= frames.length ? 100 : Math.min(99, Math.round((elapsed / duration) * 100));
  replayProgress.value = pct;
  replaySeekPercent.value = pct;
  replayCurrentTime.value = Math.min(elapsed, duration);

  if (replayFrameIndex >= frames.length) {
    replayPlaying.value = false;
    replayTimer = undefined;
    return;
  }

  replayTimer = window.setTimeout(tickReplay, 33);
}

function appendReplayOutput(frame: ReplayFrame) {
  if (frame.stream !== 'o') {
    return;
  }
  const terminal = ensureReplayTerminal();
  if (!terminal) {
    return;
  }
  replayRenderedOutput.value = true;
  terminal.write(frame.data);
}

function ensureReplayTerminal(): Terminal | undefined {
  const host = replayTerminalHostRef.value;
  if (!host) {
    return undefined;
  }

  if (!replayTerminal) {
    // Match original session cols/rows so vim/TUI escape sequences align
    // and content doesn't leave stale lines in rows outside the session height.
    replayTerminal = new Terminal({
      cols: replayTerminalCols.value,
      rows: replayTerminalRows.value,
      convertEol: false,
      cursorBlink: false,
      disableStdin: true,
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.2,
      scrollback: 5000,
      theme: {
        background: '#0b1220',
        foreground: '#d0d5dd',
        cursor: '#98a2b3',
        selectionBackground: '#344054'
      }
    });
    replayTerminal.open(host);
  }

  return replayTerminal;
}

function resetReplayTerminal() {
  replayTerminal?.reset();
}

function destroyReplayTerminal() {
  replayTerminal?.dispose();
  replayTerminal = undefined;
}

function replayHeaderNumber(key: string, fallback: number, min: number, max: number): number {
  const value = Number(replayData.value.header[key]);
  if (!Number.isFinite(value)) {
    return fallback;
  }
  return Math.min(max, Math.max(min, Math.round(value)));
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

// ── DB artifacts ──

function onlineProtocol(row: OnlineSessionRecord): string {
  if (row.resource_type === 'database_instance') return formatDatabaseProtocol(row.protocol);
  if (row.protocol_subtype === 'sftp') return 'SFTP';
  if (row.protocol_subtype === 'web-terminal') return 'Web';
  return 'SSH';
}

function onlineProtocolTag(row: OnlineSessionRecord): 'primary' | 'success' | 'warning' | 'info' | 'danger' | '' {
  if (row.resource_type === 'database_instance') return databaseProtocolTag(row.protocol);
  if (row.protocol_subtype === 'sftp') return 'warning';
  if (row.protocol_subtype === 'web-terminal') return 'success';
  return 'info';
}

function loadOnlineReplay(row: OnlineSessionRecord) {
  void loadSessionArtifact({ id: row.audit_session_id, replay_dir: row.has_replay ? 'online' : '' }, 'replay');
}

function loadOnlineLog(row: OnlineSessionRecord) {
  if (row.resource_type === 'database_instance') {
    void loadDBArtifact({ id: row.audit_session_id }, 'queries');
    return;
  }
  const kind = row.protocol_subtype === 'sftp' ? 'files' : 'commands';
  void loadSessionArtifact({
    id: row.audit_session_id,
    protocol: row.protocol,
    protocol_subtype: row.protocol_subtype,
    replay_dir: row.has_replay ? 'online' : '',
  }, kind);
}

async function disconnectOnlineSession(row: OnlineSessionRecord) {
  try {
    await ElMessageBox.confirm(
      t('audit.confirm.disconnectMessage'),
      t('audit.confirm.disconnectTitle'),
      { type: 'warning' },
    );
  } catch {
    return;
  }

  disconnectingSessionID.value = row.id;
  try {
    await apiClient.disconnectOnlineSession(row.id);
    ElMessage.success(t('audit.success.disconnected'));
    await loadOnlineSessions();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('audit.error.disconnect'));
  } finally {
    disconnectingSessionID.value = '';
  }
}

async function loadDBArtifact(connection: DBConnectionRecord, kind: 'meta' | 'queries') {
  const id = String(connection.id ?? '');

  if (!id) {
    ElMessage.error(t('audit.error.missingConnection'));
    return;
  }

  detailLoading.value = true;
  detailError.value = '';

  try {
    if (kind === 'meta') {
      setDetail(`${t('audit.scope.db')} ${id}`, kind, await apiClient.getDBConnectionMeta(id));
    } else {
      setDetail(`${t('audit.action.queries')} ${id}`, kind, await apiClient.getDBConnectionQueries(id));
    }
  } catch (err) {
    detailError.value = err instanceof Error ? err.message : t('audit.error.loadArtifact');
  } finally {
    detailLoading.value = false;
  }
}

// ── Lifecycle & watchers ──

function applyRouteAuditFilter() {
  const scope = permittedAuditScope(route.query.scope);
  const keyword = routeQueryValue(route.query.q);
  auditScope.value = scope;
  if (scope === 'online') {
    onlineKeyword.value = keyword;
    onlineResourceType.value = routeQueryValue(route.query.resource_type);
    onlineResourceID.value = routeQueryValue(route.query.resource_id);
    if (onlinePage.value === 1) void loadOnlineSessions();
    else onlinePage.value = 1;
    return;
  }
  onlineKeyword.value = '';
  onlineResourceType.value = '';
  onlineResourceID.value = '';
  if (scope === 'ssh') {
    sessionKeyword.value = keyword;
    if (sessionPage.value === 1) void loadSessions();
    else sessionPage.value = 1;
  } else {
    dbKeyword.value = keyword;
    if (dbPage.value === 1) void loadDBConnections();
    else dbPage.value = 1;
  }
}

onMounted(() => {
  if (permission.canDo('audit:view')) void loadSessions();
  if (permission.canDo('db:audit:view')) void loadDBConnections();
  if (permission.canDo('session:view')) {
    void loadOnlineSessions();
    onlineRefreshTimer = window.setInterval(() => {
      if (auditScope.value === 'online' && !onlineLoading.value) void loadOnlineSessions();
    }, 5000);
  }
});

watch(
  () => route.fullPath,
  () => {
    if (route.name === 'audit') applyRouteAuditFilter();
  },
);

watch(isReplay, async (value) => {
  if (value) {
    await nextTick();
    ensureReplayTerminal();
    resetReplayTerminal();
  }
});

// Watch pagination changes for main lists
watch([sessionPage, sessionPageSize], () => {
  if (auditScope.value === 'ssh') loadSessions();
});
watch([dbPage, dbPageSize], () => {
  if (auditScope.value === 'db') loadDBConnections();
});
watch([onlinePage, onlinePageSize], () => {
  if (auditScope.value === 'online') loadOnlineSessions();
});

onBeforeUnmount(() => {
  if (onlineRefreshTimer !== undefined) window.clearInterval(onlineRefreshTimer);
  stopReplay();
  destroyReplayTerminal();
});
</script>

<style scoped>
/* 保留 tab header 默认间距 (page-tabs 会清零) */
.page-tabs :deep(.el-tabs__header) {
  margin-bottom: 15px;
  padding: 0;
}

.audit-view {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.placeholder-panel :deep(.el-segmented) {
  max-width: 100%;
}

:deep(.col-time) {
  white-space: nowrap;
}

/* Make drawer body a flex column so terminal can fill remaining space */
:deep(.el-drawer__body) {
  display: flex;
  flex-direction: column;
  padding: 12px 16px;
}

.drawer-content {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}

.replay-panel {
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex: 1;
  min-height: 0;
}

.replay-controls {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.replay-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  color: #667085;
  font-size: 12px;
}

.replay-meta :deep(.el-slider) {
  flex: 1;
  min-width: 80px;
}

.replay-meta-secondary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  color: #667085;
  font-size: 11px;
}

.replay-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.replay-terminal-shell {
  position: relative;
  flex: 1;
  min-height: 200px;
  overflow: hidden;
  background: #0b1220;
  border-radius: 8px;
}

.replay-terminal {
  position: absolute;
  inset: 0;
}

/* let xterm control its own dimensions based on cols/rows */

.replay-terminal :deep(.xterm-viewport) {
  overflow-y: auto !important;
  scrollbar-width: thin;
  scrollbar-color: #475467 transparent;
}

.replay-terminal :deep(.xterm-viewport::-webkit-scrollbar) {
  width: 6px;
}

.replay-terminal :deep(.xterm-viewport::-webkit-scrollbar-track) {
  background: transparent;
}

.replay-terminal :deep(.xterm-viewport::-webkit-scrollbar-thumb) {
  background: #475467;
  border-radius: 3px;
}

.replay-terminal :deep(.xterm-screen) {
  max-width: 100%;
}

.replay-terminal-empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 16px;
  color: #98a2b3;
  font-size: 13px;
  text-align: center;
  pointer-events: none;
}

@media (max-width: 720px) {
  .replay-controls {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
