<template>
  <div ref="pageRef" class="web-rdp-page">
    <header class="rdp-toolbar">
      <div class="toolbar-identity">
        <el-button class="toolbar-back" link @click="goBack">
          <el-icon><ArrowLeft /></el-icon>
          返回
        </el-button>
        <span class="target-name" :title="displayTargetName">
          {{ displayTargetName }}
        </span>
        <span class="status-badge" :class="status" role="status" aria-live="polite">
          <span class="status-dot"></span>
          {{ statusLabel }}
        </span>
      </div>

      <div class="toolbar-actions">
        <div class="toolbar-secondary-actions" aria-label="远程桌面辅助操作">
        <el-tooltip :content="clipboardReadTooltip" placement="bottom">
          <span class="tooltip-trigger">
            <el-button
              size="small"
              :disabled="Boolean(clipboardReadDisabledReason)"
              @click="handleCopyRemoteClipboard"
            >
              <el-icon><CopyDocument /></el-icon>
              复制到本机
            </el-button>
          </span>
        </el-tooltip>

        <el-tooltip :content="clipboardWriteTooltip" placement="bottom">
          <span class="tooltip-trigger">
            <el-button
              size="small"
              :disabled="Boolean(clipboardWriteDisabledReason)"
              @click="handlePasteLocalClipboard"
            >
              <el-icon><DocumentCopy /></el-icon>
              粘贴到远程
            </el-button>
          </span>
        </el-tooltip>

        <el-tooltip :content="uploadTooltip" placement="bottom">
          <span class="tooltip-trigger">
            <el-button
              size="small"
              :loading="uploading"
              :disabled="Boolean(uploadDisabledReason)"
              @click="chooseUploadFile"
            >
              <el-icon v-if="!uploading"><Upload /></el-icon>
              {{ uploading ? `上传 ${uploadProgress}%` : '上传文件' }}
            </el-button>
          </span>
        </el-tooltip>
        <input
          ref="fileInputRef"
          class="hidden-file-input"
          type="file"
          @change="handleUploadFile"
        />

        <el-tooltip :content="downloadTooltip" placement="bottom">
          <span class="tooltip-trigger">
            <el-button
              size="small"
              :disabled="Boolean(downloadDisabledReason)"
              @click="showDownloadHelp"
            >
              <el-icon><Download /></el-icon>
              {{ downloadLabel }}
            </el-button>
          </span>
        </el-tooltip>

        <el-tooltip :content="driveTooltip" placement="bottom">
          <span class="tooltip-trigger">
            <el-button
              size="small"
              :disabled="Boolean(driveDisabledReason)"
              @click="showDriveStatus"
            >
              <el-icon><FolderOpened /></el-icon>
              {{ driveLabel }}
            </el-button>
          </span>
        </el-tooltip>

        <span class="toolbar-divider"></span>

        <el-tooltip content="缩小远程画面" placement="bottom">
          <span class="tooltip-trigger">
            <el-button
              size="small"
              aria-label="缩小远程画面"
              :disabled="status !== 'connected' || scale <= 0.25"
              circle
              @click="setScale(scale - 0.1)"
            >
              <el-icon><Minus /></el-icon>
            </el-button>
          </span>
        </el-tooltip>
        <span class="scale-label">{{ Math.round(scale * 100) }}%</span>
        <el-tooltip content="放大远程画面" placement="bottom">
          <span class="tooltip-trigger">
            <el-button
              size="small"
              aria-label="放大远程画面"
              :disabled="status !== 'connected' || scale >= 3"
              circle
              @click="setScale(scale + 0.1)"
            >
              <el-icon><Plus /></el-icon>
            </el-button>
          </span>
        </el-tooltip>
        <el-tooltip content="按当前窗口自动缩放" placement="bottom">
          <span class="tooltip-trigger">
            <el-button
              size="small"
              :type="autoFit ? 'primary' : 'default'"
              :disabled="status !== 'connected'"
              @click="fitDisplay"
            >
              适应
            </el-button>
          </span>
        </el-tooltip>
        <el-tooltip content="按远程桌面原始尺寸显示" placement="bottom">
          <span class="tooltip-trigger">
            <el-button
              size="small"
              :disabled="status !== 'connected'"
              @click="setScale(1)"
            >
              1:1
            </el-button>
          </span>
        </el-tooltip>
        </div>
        <div class="toolbar-session-actions" aria-label="会话操作">
        <el-tooltip :content="isFullscreen ? '退出全屏' : '进入全屏'" placement="bottom">
          <el-button
            size="small"
            circle
            :aria-label="isFullscreen ? '退出全屏' : '进入全屏'"
            @click="toggleFullscreen"
          >
            <el-icon><FullScreen /></el-icon>
          </el-button>
        </el-tooltip>
        <el-button
          size="small"
          type="danger"
          plain
          :disabled="status === 'idle' || status === 'disconnected'"
          @click="handleDisconnect"
        >
          断开
        </el-button>
        </div>
      </div>
    </header>

    <main class="rdp-workspace">
      <div
        ref="displayRef"
        class="rdp-display"
        :class="{ 'is-auto-fit': autoFit }"
      ></div>

      <div
        v-if="status === 'requesting-ticket' || status === 'connecting'"
        class="rdp-overlay"
        role="status"
        aria-live="polite"
      >
        <el-icon class="is-loading overlay-icon" :size="34"><Loading /></el-icon>
        <strong>
          {{ status === 'requesting-ticket' ? '正在申请安全连接票据…' : '正在连接远程桌面…' }}
        </strong>
        <span>连接建立后，键盘和鼠标操作将发送到目标 Windows 主机</span>
      </div>

      <div v-else-if="status === 'error'" class="rdp-overlay" role="status" aria-live="polite">
        <el-icon class="overlay-icon error-icon" :size="34"><WarningFilled /></el-icon>
        <strong>连接失败</strong>
        <span>{{ error || '无法建立 Web RDP 连接' }}</span>
        <div class="overlay-actions">
          <el-button @click="goBack">返回</el-button>
          <el-button type="primary" @click="retryConnection">
            <el-icon><Refresh /></el-icon>
            重试
          </el-button>
        </div>
      </div>

      <div v-else-if="status === 'disconnected'" class="rdp-overlay" role="status" aria-live="polite">
        <el-icon class="overlay-icon" :size="34"><CircleClose /></el-icon>
        <strong>远程桌面已断开</strong>
        <span>可重新申请票据并建立新连接</span>
        <div class="overlay-actions">
          <el-button @click="goBack">返回</el-button>
          <el-button type="primary" @click="retryConnection">
            <el-icon><Refresh /></el-icon>
            重新连接
          </el-button>
        </div>
      </div>
    </main>

    <footer class="rdp-footer">
      <span v-if="remoteWidth && remoteHeight">
        远程分辨率 {{ remoteWidth }} × {{ remoteHeight }}
      </span>
      <span v-if="remoteName">会话：{{ remoteName }}</span>
      <span>
        剪贴板：读取{{ policy.clipboard_read ? '允许' : '禁止' }}
        / 写入{{ policy.clipboard_write ? '允许' : '禁止' }}
      </span>
      <span>
        文件：上传{{ policy.file_upload ? '允许' : '禁止' }}
        / 下载{{ policy.file_download ? '允许' : '禁止' }}
      </span>
      <span>虚拟盘：{{ policy.drive_mapping ? '允许' : '禁止' }}</span>
    </footer>
  </div>
</template>

<script setup lang="ts">
import {
  computed,
  onMounted,
  onUnmounted,
  ref,
} from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  ArrowLeft,
  CircleClose,
  CopyDocument,
  DocumentCopy,
  Download,
  FolderOpened,
  FullScreen,
  Loading,
  Minus,
  Plus,
  Refresh,
  Upload,
  WarningFilled,
} from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';

import { apiClient } from '@/api/client';
import { useWebRDP, type WebRDPStatus } from '@/composables/useWebRDP';

const route = useRoute();
const router = useRouter();
const pageRef = ref<HTMLElement | null>(null);
const displayRef = ref<HTMLElement | null>(null);
const fileInputRef = ref<HTMLInputElement | null>(null);
const targetId = ref('');
const targetName = ref('');
const isFullscreen = ref(false);

const {
  status,
  error,
  policy,
  remoteName,
  scale,
  autoFit,
  remoteWidth,
  remoteHeight,
  remoteClipboardAvailable,
  uploading,
  uploadProgress,
  downloadStatus,
  downloadedFilename,
  downloadBytes,
  driveStatus,
  driveName,
  connect,
  disconnect,
  resize,
  setScale,
  fitDisplay,
  copyRemoteClipboard,
  pasteLocalClipboard,
  uploadFile,
} = useWebRDP({ targetId });

const statusLabel = computed(() => {
  const labels: Record<WebRDPStatus, string> = {
    idle: '就绪',
    'requesting-ticket': '申请票据',
    connecting: '连接中',
    connected: '已连接',
    disconnected: '已断开',
    error: '连接错误',
  };
  return labels[status.value];
});

const displayTargetName = computed(() => {
  return targetName.value || (targetId.value ? `RDP 目标 ${targetId.value}` : 'Web RDP');
});

const clipboardReadDisabledReason = computed(() => {
  if (!policy.value.clipboard_read) return '管理员策略禁止从远程桌面读取剪贴板';
  if (status.value !== 'connected') return '远程桌面连接后才可复制';
  if (!remoteClipboardAvailable.value) return '远程剪贴板尚未提供文本内容';
  return '';
});

const clipboardReadTooltip = computed(() => {
  return clipboardReadDisabledReason.value || '把远程 Windows 剪贴板中的文本复制到本机';
});

const clipboardWriteDisabledReason = computed(() => {
  if (!policy.value.clipboard_write) return '管理员策略禁止向远程桌面写入剪贴板';
  if (status.value !== 'connected') return '远程桌面连接后才可粘贴';
  return '';
});

const clipboardWriteTooltip = computed(() => {
  return clipboardWriteDisabledReason.value || '读取本机剪贴板文本并发送到远程 Windows';
});

const uploadDisabledReason = computed(() => {
  if (!policy.value.file_upload) return '管理员策略禁止向远程桌面上传文件';
  if (status.value !== 'connected') return '远程桌面连接后才可上传';
  if (uploading.value) return '当前文件上传完成后才可继续';
  return '';
});

const uploadTooltip = computed(() => {
  return uploadDisabledReason.value || '选择本机文件并传送到远程 Windows';
});

const downloadDisabledReason = computed(() => {
  if (!policy.value.file_download) return '管理员策略禁止从远程桌面下载文件';
  if (status.value !== 'connected') return '远程桌面连接后才可下载';
  return '';
});

const downloadLabel = computed(() => {
  if (downloadStatus.value === 'receiving') {
    return `下载中 ${formatBytes(downloadBytes.value)}`;
  }
  if (downloadStatus.value === 'saved' && downloadedFilename.value) {
    return `已下载 ${downloadedFilename.value}`;
  }
  return '下载文件';
});

const downloadTooltip = computed(() => {
  if (downloadDisabledReason.value) return downloadDisabledReason.value;
  if (downloadStatus.value === 'receiving') {
    return `正在接收 ${downloadedFilename.value || '远程文件'}，已传输 ${formatBytes(downloadBytes.value)}`;
  }
  if (downloadStatus.value === 'saved') {
    return `最近下载：${downloadedFilename.value}（${formatBytes(downloadBytes.value)}）`;
  }
  return '请在远程 Windows 中发起下载；浏览器收到文件后会自动保存';
});

const driveDisabledReason = computed(() => {
  if (!policy.value.drive_mapping) return '管理员策略禁止虚拟盘映射';
  if (status.value !== 'connected') return '远程桌面连接后才可使用虚拟盘';
  return '';
});

const driveLabel = computed(() => {
  if (driveStatus.value === 'available') return driveName.value || '虚拟盘可用';
  if (driveStatus.value === 'waiting') return '等待虚拟盘';
  return '虚拟盘禁用';
});

const driveTooltip = computed(() => {
  if (driveDisabledReason.value) return driveDisabledReason.value;
  if (driveStatus.value === 'available') {
    return `服务端已提供虚拟盘：${driveName.value || '远程虚拟盘'}`;
  }
  return '策略已允许虚拟盘，正在等待服务端提供映射';
});

function formatBytes(bytes: number) {
  if (!bytes) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  const unitIndex = Math.min(
    units.length - 1,
    Math.floor(Math.log(bytes) / Math.log(1024)),
  );
  const value = bytes / (1024 ** unitIndex);
  return `${value >= 10 || unitIndex === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unitIndex]}`;
}

async function startConnection() {
  if (!displayRef.value) return;
  try {
    await connect(
      displayRef.value,
      displayRef.value.parentElement ?? displayRef.value,
    );
  } catch {
    // The composable exposes the user-facing error and error state.
  }
}

function retryConnection() {
  void startConnection();
}

function goBack() {
  disconnect();
  router.back();
}

function handleDisconnect() {
  disconnect();
}

async function handleCopyRemoteClipboard() {
  try {
    await copyRemoteClipboard();
    ElMessage.success('远程剪贴板文本已复制到本机');
  } catch (caught) {
    ElMessage.error(caught instanceof Error ? caught.message : '复制剪贴板失败');
  }
}

async function handlePasteLocalClipboard() {
  try {
    await pasteLocalClipboard();
    ElMessage.success('本机剪贴板文本已发送到远程桌面');
  } catch (caught) {
    ElMessage.error(caught instanceof Error ? caught.message : '粘贴剪贴板失败');
  }
}

function chooseUploadFile() {
  if (!fileInputRef.value) return;
  fileInputRef.value.value = '';
  fileInputRef.value.click();
}

async function handleUploadFile(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = '';
  if (!file) return;

  try {
    await uploadFile(file);
    ElMessage.success(`文件“${file.name}”已发送到远程桌面`);
  } catch (caught) {
    ElMessage.error(caught instanceof Error ? caught.message : '文件上传失败');
  }
}

function showDownloadHelp() {
  if (downloadStatus.value === 'saved') {
    ElMessage.success(`最近下载：${downloadedFilename.value}`);
    return;
  }
  ElMessage.info('请在远程 Windows 中发起文件下载，浏览器收到后会自动保存');
}

function showDriveStatus() {
  if (driveStatus.value === 'available') {
    ElMessage.success(`虚拟盘“${driveName.value || '远程虚拟盘'}”已由服务端提供`);
    return;
  }
  ElMessage.info('虚拟盘策略已允许，正在等待服务端提供映射');
}

async function toggleFullscreen() {
  try {
    if (document.fullscreenElement) {
      await document.exitFullscreen();
    } else {
      await pageRef.value?.requestFullscreen();
    }
  } catch {
    ElMessage.error('浏览器未能切换全屏模式');
  }
}

function handleFullscreenChange() {
  isFullscreen.value = document.fullscreenElement === pageRef.value;
  resize();
}

async function loadTargetName(id: string) {
  try {
    const target = await apiClient.getTarget(id);
    const host = typeof target.host === 'string' ? target.host : '';
    const port = target.port ? `:${target.port}` : '';
    const name = target.name || target.username || id;
    targetName.value = host ? `${name}（${host}${port}）` : String(name);
  } catch {
    targetName.value = `RDP 目标 ${id}`;
  }
}

onMounted(() => {
  targetId.value = String(route.query.target_id || '').trim();
  if (targetId.value) void loadTargetName(targetId.value);
  document.addEventListener('fullscreenchange', handleFullscreenChange);
  void startConnection();
});

onUnmounted(() => {
  document.removeEventListener('fullscreenchange', handleFullscreenChange);
  disconnect();
});
</script>

<style scoped>
.web-rdp-page {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100dvh;
  min-height: 0;
  padding:
    env(safe-area-inset-top)
    env(safe-area-inset-right)
    env(safe-area-inset-bottom)
    env(safe-area-inset-left);
  overflow: hidden;
  color: #dbe5f2;
  background: #10151d;
}

.web-rdp-page:fullscreen {
  height: 100dvh;
}

.rdp-toolbar {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: space-between;
  min-height: 46px;
  padding: 5px 10px;
  gap: 16px;
  border-bottom: 1px solid #2c3745;
  background: #17202b;
}

.toolbar-identity,
.toolbar-actions,
.toolbar-secondary-actions,
.toolbar-session-actions {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}

.toolbar-identity {
  flex: 1 1 auto;
}

.toolbar-actions {
  flex: 0 1 auto;
  overflow: hidden;
  padding-bottom: 1px;
}

.toolbar-secondary-actions {
  min-width: 0;
  overflow-x: auto;
  overscroll-behavior-inline: contain;
  scrollbar-width: thin;
}

.toolbar-session-actions {
  flex: 0 0 auto;
}

.toolbar-back {
  color: #dbe5f2;
}

.target-name {
  max-width: 260px;
  overflow: hidden;
  color: #b9c9da;
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
  color: #9cabbc;
  font-size: 12px;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #718096;
}

.status-badge.requesting-ticket .status-dot,
.status-badge.connecting .status-dot {
  background: #e6a23c;
  box-shadow: 0 0 0 3px rgb(230 162 60 / 16%);
}

.status-badge.connected .status-dot {
  background: #67c23a;
  box-shadow: 0 0 0 3px rgb(103 194 58 / 16%);
}

.status-badge.disconnected .status-dot,
.status-badge.error .status-dot {
  background: #f56c6c;
}

.tooltip-trigger {
  display: inline-flex;
}

.toolbar-divider {
  width: 1px;
  height: 24px;
  margin: 0 2px;
  background: #364354;
}

.scale-label {
  width: 42px;
  color: #a9b8c8;
  font-size: 12px;
  text-align: center;
}

.hidden-file-input {
  display: none;
}

.rdp-workspace {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
  background:
    radial-gradient(circle at center, rgb(51 65 85 / 32%) 0, transparent 55%),
    #0b0f15;
}

.rdp-display {
  display: flex;
  align-items: flex-start;
  justify-content: flex-start;
  width: 100%;
  height: 100%;
  overflow: auto;
}

.rdp-display.is-auto-fit {
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

:deep(.rdp-display > div) {
  flex: 0 0 auto;
}

:deep(.rdp-display > div:focus-visible) {
  outline: 2px solid #60a5fa;
  outline-offset: -2px;
}

.rdp-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 11px;
  padding: 24px;
  color: #dbe5f2;
  text-align: center;
  background: rgb(11 15 21 / 92%);
  backdrop-filter: blur(3px);
  z-index: 10;
}

.rdp-overlay strong {
  font-size: 16px;
}

.rdp-overlay > span {
  max-width: 620px;
  color: #91a2b6;
  font-size: 13px;
}

.overlay-icon {
  color: #7fb3ef;
}

.error-icon {
  color: #f56c6c;
}

.overlay-actions {
  display: flex;
  gap: 8px;
  margin-top: 7px;
}

.rdp-footer {
  display: flex;
  flex: 0 0 28px;
  align-items: center;
  gap: 16px;
  min-width: 0;
  padding: 0 12px;
  overflow-x: auto;
  color: #75879a;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  border-top: 1px solid #25303d;
  background: #121923;
}

@media (max-width: 1200px) {
  .rdp-toolbar {
    align-items: flex-start;
    flex-direction: column;
    gap: 5px;
  }

  .toolbar-identity,
  .toolbar-actions {
    width: 100%;
  }

  .toolbar-actions {
    justify-content: space-between;
  }

  .toolbar-secondary-actions {
    flex: 1 1 auto;
  }

  .target-name {
    max-width: 45vw;
  }
}

@media (max-width: 600px) {
  .rdp-toolbar {
    padding-inline: 8px;
  }

  .toolbar-secondary-actions {
    padding-right: 8px;
    mask-image: linear-gradient(to right, #000 calc(100% - 28px), transparent);
  }

  .target-name {
    max-width: 38vw;
  }

  .rdp-footer {
    gap: 10px;
  }
}
</style>
