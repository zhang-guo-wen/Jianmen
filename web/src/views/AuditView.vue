<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-segmented v-model="auditScope" :options="scopeOptions" />
      <el-button :loading="currentLoading" type="primary" @click="refreshCurrent">
        {{ t('common.refresh') }}
      </el-button>
    </div>

    <el-card v-if="auditScope === 'ssh'" class="placeholder-panel" shadow="never">
      <template #header>
        <div class="toolbar">
          <span>{{ t('audit.title.sshSessions') }}</span>
          <el-tag>{{ sessions.length }}</el-tag>
        </div>
      </template>
      <el-alert v-if="sessionError" :title="sessionError" type="error" show-icon />
      <el-table v-else v-loading="sessionsLoading" :data="sessions" height="380" row-key="id">
        <el-table-column :label="t('sessions.column.id')" min-width="160">
          <template #default="{ row }">
            {{ sessionId(row) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('sessions.column.user')" min-width="150">
          <template #default="{ row }">
            {{ sessionUser(row) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('sessions.column.target')" min-width="170">
          <template #default="{ row }">
            {{ row.target || row.target_id || t('common.none') }}
          </template>
        </el-table-column>
        <el-table-column prop="client_ip" :label="t('audit.column.client')" min-width="150" />
        <el-table-column :label="t('sessions.column.started')" min-width="190">
          <template #default="{ row }">
            {{ formatTime(row.started_at ?? row.startedAt) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" fixed="right" width="310">
          <template #default="{ row }">
            <el-button link type="primary" @click="loadSessionArtifact(row, 'meta')">
              {{ t('audit.action.meta') }}
            </el-button>
            <el-button :disabled="!hasReplay(row)" link type="success" @click="loadSessionArtifact(row, 'replay')">
              {{ t('audit.action.replay') }}
            </el-button>
            <el-button link type="primary" @click="loadSessionArtifact(row, 'commands')">
              {{ t('audit.action.commands') }}
            </el-button>
            <el-button link type="primary" @click="loadSessionArtifact(row, 'files')">
              {{ t('audit.action.files') }}
            </el-button>
            <el-button link type="primary" @click="loadSessionArtifact(row, 'file-summary')">
              {{ t('audit.action.summary') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!sessionsLoading && !sessions.length && !sessionError" :description="t('sessions.empty')" />
    </el-card>

    <el-card v-if="auditScope === 'db'" class="placeholder-panel" shadow="never">
      <template #header>
        <div class="toolbar">
          <span>{{ t('audit.title.dbConnections') }}</span>
          <el-tag>{{ dbConnections.length }}</el-tag>
        </div>
      </template>
      <el-alert v-if="dbError" :title="dbError" type="warning" show-icon />
      <el-table v-else v-loading="dbLoading" :data="dbConnections" height="380" row-key="id">
        <el-table-column prop="id" :label="t('common.id')" min-width="160" />
        <el-table-column prop="name" :label="t('common.name')" min-width="150" />
        <el-table-column prop="protocol" :label="t('audit.column.protocol')" width="120" />
        <el-table-column prop="client_addr" :label="t('audit.column.client')" min-width="170" />
        <el-table-column prop="upstream_addr" :label="t('audit.column.upstream')" min-width="170" />
        <el-table-column :label="t('sessions.column.started')" min-width="190">
          <template #default="{ row }">
            {{ formatTime(row.started_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" fixed="right" width="170">
          <template #default="{ row }">
            <el-button link type="primary" @click="loadDBArtifact(row, 'meta')">
              {{ t('audit.action.meta') }}
            </el-button>
            <el-button link type="primary" @click="loadDBArtifact(row, 'queries')">
              {{ t('audit.action.queries') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!dbLoading && !dbConnections.length && !dbError" :description="t('audit.empty.dbConnections')" />
    </el-card>

    <el-drawer
      v-model="drawerVisible"
      direction="rtl"
      size="65%"
      @close="closeDetail"
    >
      <template #title>
        <div class="toolbar">
          <span>{{ detailTitle || t('audit.title.detail') }}</span>
          <el-tag v-if="detailKind">{{ detailKind }}</el-tag>
        </div>
      </template>
      <el-alert v-if="detailError" :title="detailError" type="error" show-icon />
      <div v-else v-loading="detailLoading">
        <el-descriptions v-if="isDBMeta" :column="2" border>
          <el-descriptions-item :label="t('common.id')">{{ dbMeta.id || t('common.none') }}</el-descriptions-item>
          <el-descriptions-item :label="t('common.name')">{{ dbMeta.name || t('common.none') }}</el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.protocol')">
            {{ dbMeta.protocol || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.authUser')">
            {{ dbMeta.auth_user || t('common.none') }}
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
          <div class="replay-toolbar">
            <div class="replay-meta">
              <span>{{ t('audit.replay.frames') }} {{ replayFrames.length }}</span>
              <span>{{ t('audit.replay.outputFrames') }} {{ replayOutputFrames.length }}</span>
              <span>{{ t('audit.replay.size') }} {{ formatBytes(replayRawBytes) }}</span>
              <span>{{ t('audit.replay.duration') }} {{ formatReplayDuration(replayDuration) }}</span>
            </div>
            <div class="replay-actions">
              <el-button :disabled="!replayOutputFrames.length" type="primary" @click="playReplay">
                {{ replayPlaying ? t('audit.action.restart') : t('audit.action.play') }}
              </el-button>
              <el-button :disabled="!replayPlaying" @click="stopReplay">
                {{ t('audit.action.stop') }}
              </el-button>
              <el-select
                v-model="playbackSpeed"
                size="small"
                :disabled="replayPlaying"
                style="width: 72px"
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
          <div class="replay-seek-bar">
            <span class="replay-time-label">{{ formatReplayDuration(replayCurrentTime) }}</span>
            <el-slider
              v-model="replaySeekPercent"
              :max="100"
              :show-tooltip="false"
              :disabled="!replayFrames.length"
              @change="seekReplay"
            />
            <span class="replay-time-label">{{ formatReplayDuration(replayDuration) }}</span>
          </div>
          <div class="replay-terminal-shell">
            <div ref="replayTerminalHostRef" class="replay-terminal" />
            <div v-if="replayTerminalMessage" class="replay-terminal-empty">
              {{ replayTerminalMessage }}
            </div>
          </div>
        </div>

        <el-table v-else-if="isDBQueries" :data="queryEvents" height="420">
          <el-table-column prop="seq" :label="t('audit.column.seq')" width="90" />
          <el-table-column prop="type" :label="t('audit.column.event')" min-width="150" />
          <el-table-column prop="query_kind" :label="t('audit.column.queryKind')" min-width="120" />
          <el-table-column :label="t('audit.column.result')" width="140">
            <template #default="{ row }">
              <el-tag :type="queryStatusType(row.status)">
                {{ row.status || t('dashboard.unknown') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.duration')" width="130">
            <template #default="{ row }">
              {{ formatDuration(row.duration_ms) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.time')" min-width="180">
            <template #default="{ row }">
              {{ formatTime(row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column prop="sql" :label="t('audit.column.sql')" min-width="320" show-overflow-tooltip />
          <el-table-column prop="error_message" :label="t('audit.column.error')" min-width="220" show-overflow-tooltip />
        </el-table>

        <el-table v-else-if="isCommands" :data="commandEvents" height="420">
          <el-table-column prop="seq" :label="t('audit.column.seq')" width="90" />
          <el-table-column prop="command" :label="t('audit.column.command')" min-width="260" show-overflow-tooltip />
          <el-table-column prop="preview" :label="t('audit.column.preview')" min-width="260" show-overflow-tooltip />
          <el-table-column prop="confidence" :label="t('audit.column.confidence')" width="140" />
          <el-table-column :label="t('audit.column.time')" min-width="180">
            <template #default="{ row }">
              {{ formatTime(row.started_at) }}
            </template>
          </el-table-column>
        </el-table>

        <el-table v-else-if="isFiles" :data="fileEvents" height="420">
          <el-table-column prop="seq" :label="t('audit.column.seq')" width="90" />
          <el-table-column prop="action" :label="t('audit.column.action')" min-width="140" />
          <el-table-column prop="path" :label="t('audit.column.path')" min-width="260" show-overflow-tooltip />
          <el-table-column prop="path2" :label="t('audit.column.path2')" min-width="220" show-overflow-tooltip />
          <el-table-column prop="result" :label="t('audit.column.result')" width="130" />
          <el-table-column prop="size" :label="t('audit.column.size')" width="120" />
          <el-table-column :label="t('audit.column.time')" min-width="180">
            <template #default="{ row }">
              {{ formatTime(row.started_at) }}
            </template>
          </el-table-column>
        </el-table>

        <pre v-else-if="detailData" class="json-preview">{{ JSON.stringify(detailData, null, 2) }}</pre>
        <el-empty v-else :description="t('audit.empty.detail')" />
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { ElMessage } from 'element-plus';

import {
  apiClient,
  type ApiEnvelope,
  type DBConnectionMetaRecord,
  type DBConnectionRecord,
  type DBQueryEventRecord,
  type SessionCommandRecord,
  type SessionFileEventRecord,
  type SessionRecord
} from '@/api/client';
import { useI18n } from '@/i18n';

type AuditScope = 'ssh' | 'db';
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
const auditScope = ref<AuditScope>('ssh');
const sessions = ref<SessionRecord[]>([]);
const dbConnections = ref<DBConnectionRecord[]>([]);
const sessionsLoading = ref(false);
const dbLoading = ref(false);
const detailLoading = ref(false);
const sessionError = ref('');
const dbError = ref('');
const detailError = ref('');
const detailTitle = ref('');
const detailKind = ref<DetailKind>('');
const detailData = ref<unknown>(null);
const drawerVisible = ref(false);
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

const scopeOptions = computed(() => [
  { label: t('audit.scope.ssh'), value: 'ssh' },
  { label: t('audit.scope.db'), value: 'db' }
]);
const currentLoading = computed(() =>
  auditScope.value === 'ssh' ? sessionsLoading.value : dbLoading.value
);
const isDBMeta = computed(() => detailKind.value === 'meta' && isRecord(detailData.value) && 'protocol' in detailData.value);
const isDBQueries = computed(() => detailKind.value === 'queries' && Array.isArray(detailData.value));
const isCommands = computed(() => detailKind.value === 'commands' && Array.isArray(detailData.value));
const isFiles = computed(() => detailKind.value === 'files' && Array.isArray(detailData.value));
const isReplay = computed(() => detailKind.value === 'replay' && isReplayData(detailData.value));
const dbMeta = computed(() => (isRecord(detailData.value) ? (detailData.value as DBConnectionMetaRecord) : {}));
const queryEvents = computed(() => (Array.isArray(detailData.value) ? (detailData.value as DBQueryEventRecord[]) : []));
const commandEvents = computed(() =>
  Array.isArray(detailData.value) ? (detailData.value as SessionCommandRecord[]) : []
);
const fileEvents = computed(() =>
  Array.isArray(detailData.value) ? (detailData.value as SessionFileEventRecord[]) : []
);
const replayData = computed(() => (isReplayData(detailData.value) ? detailData.value : { header: {}, frames: [], raw: '' }));
const replayFrames = computed(() => replayData.value.frames);
const replayOutputFrames = computed(() => replayFrames.value.filter((frame) => frame.stream === 'o'));
const replayDuration = computed(() => replayFrames.value.at(-1)?.time ?? 0);
const replayRawBytes = computed(() => utf8ByteLength(replayData.value.raw));
const replayFirstOutputTime = computed(() => replayOutputFrames.value[0]?.time ?? 0);
const replayTerminalCols = computed(() => replayHeaderNumber('width', 120, 20, 240));
const replayTerminalRows = computed(() => replayHeaderNumber('height', 24, 8, 80));
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

function unwrapArray<T>(payload: ApiEnvelope<T[]> | T[]): T[] {
  return Array.isArray(payload) ? payload : payload.data ?? [];
}

function unwrapObject<T>(payload: ApiEnvelope<T> | T): T {
  return (payload as ApiEnvelope<T>).data ?? (payload as T);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isReplayData(value: unknown): value is ReplayData {
  return isRecord(value) && Array.isArray(value.frames) && typeof value.raw === 'string';
}

function sessionId(session: SessionRecord): string {
  return String(session.id ?? '');
}

function sessionUser(session: SessionRecord): string {
  return String(session.user ?? session.user_username ?? session.user_id ?? t('common.none'));
}

function hasReplay(session: SessionRecord): boolean {
  return session.has_replay === true || (typeof session.replay_size === 'number' && session.replay_size > 0);
}

function formatTime(value: unknown): string {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return new Date(value).toLocaleString();
  }

  if (typeof value === 'string' && value.trim()) {
    const parsed = Date.parse(value);

    return Number.isNaN(parsed) ? value : new Date(parsed).toLocaleString();
  }

  return t('common.none');
}

function formatDuration(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? `${value} ms` : t('common.none');
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

async function loadSessions() {
  sessionsLoading.value = true;
  sessionError.value = '';

  try {
    sessions.value = unwrapArray(await apiClient.getSessions());
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
    dbConnections.value = unwrapArray(await apiClient.getDBConnections());
  } catch (err) {
    dbConnections.value = [];
    dbError.value = err instanceof Error ? err.message : t('audit.error.loadDBConnections');
  } finally {
    dbLoading.value = false;
  }
}

async function refreshCurrent() {
  if (auditScope.value === 'ssh') {
    await loadSessions();
    return;
  }

  await loadDBConnections();
}

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
      setDetail(`${t('audit.scope.ssh')} ${id}`, kind, unwrapObject(await apiClient.getSessionMeta(id)));
    } else if (kind === 'replay') {
      setDetail(`${t('audit.action.replay')} ${id}`, kind, parseReplayCast(await apiClient.getSessionReplay(id)));
      await nextTick();
      playReplay();
    } else if (kind === 'commands') {
      setDetail(`${t('audit.action.commands')} ${id}`, kind, unwrapArray(await apiClient.getSessionCommands(id)));
    } else if (kind === 'files') {
      setDetail(`${t('audit.action.files')} ${id}`, kind, unwrapArray(await apiClient.getSessionFiles(id)));
    } else {
      setDetail(`${t('audit.action.summary')} ${id}`, kind, unwrapObject(await apiClient.getSessionFileSummary(id)));
    }
  } catch (err) {
    detailError.value = err instanceof Error ? err.message : t('audit.error.loadArtifact');
  } finally {
    detailLoading.value = false;
  }
}

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
  const cols = replayTerminalCols.value;
  const rows = replayTerminalRows.value;
  if (!replayTerminal) {
    replayTerminal = new Terminal({
      cols,
      rows,
      convertEol: true,
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
    return replayTerminal;
  }
  replayTerminal.resize(cols, rows);
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
      setDetail(`${t('audit.scope.db')} ${id}`, kind, unwrapObject(await apiClient.getDBConnectionMeta(id)));
    } else {
      setDetail(`${t('audit.action.queries')} ${id}`, kind, unwrapArray(await apiClient.getDBConnectionQueries(id)));
    }
  } catch (err) {
    detailError.value = err instanceof Error ? err.message : t('audit.error.loadArtifact');
  } finally {
    detailLoading.value = false;
  }
}

onMounted(() => {
  void Promise.all([loadSessions(), loadDBConnections()]);
});

watch(isReplay, async (value) => {
  if (value) {
    await nextTick();
    ensureReplayTerminal();
    resetReplayTerminal();
  }
});

onBeforeUnmount(() => {
  stopReplay();
  destroyReplayTerminal();
});
</script>

<style scoped>
.placeholder-panel :deep(.el-segmented) {
  max-width: 100%;
}

.json-preview {
  overflow: auto;
  max-height: 460px;
  margin: 0;
  padding: 14px;
  color: #344054;
  background: #f9fafb;
  border: 1px solid #eaecf0;
  border-radius: 8px;
}

.replay-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.replay-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.replay-meta,
.replay-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.replay-meta {
  color: #667085;
  font-size: 13px;
}

.replay-terminal-shell {
  position: relative;
  height: min(520px, 70vh);
  min-height: 320px;
  background: #0b1220;
  border: 1px solid #1f2937;
  border-radius: 8px;
}

.replay-terminal {
  position: absolute;
  inset: 8px;
  box-sizing: border-box;
}

.replay-terminal :deep(.xterm) {
  width: 100%;
  height: 100%;
}

.replay-terminal :deep(.xterm-viewport) {
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: #475467 transparent;
}

.replay-terminal :deep(.xterm-viewport::-webkit-scrollbar) {
  width: 8px;
}

.replay-terminal :deep(.xterm-viewport::-webkit-scrollbar-track) {
  background: transparent;
}

.replay-terminal :deep(.xterm-viewport::-webkit-scrollbar-thumb) {
  background: #475467;
  border-radius: 4px;
}

.replay-terminal :deep(.xterm-screen) {
  max-width: 100%;
}

.replay-seek-bar {
  display: flex;
  align-items: center;
  gap: 10px;
}

.replay-seek-bar :deep(.el-slider) {
  flex: 1;
}

.replay-time-label {
  flex-shrink: 0;
  min-width: 48px;
  color: #667085;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
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
  .replay-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
}

</style>
