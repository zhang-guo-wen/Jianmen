<template>
  <div class="web-terminal-page">
    <!-- 顶部工具栏 -->
    <div class="terminal-toolbar">
      <div class="toolbar-left">
        <el-button link @click="goBack">
          <el-icon><ArrowLeft /></el-icon>
          <span>返回</span>
        </el-button>
        <span v-if="targetName" class="target-label">
          {{ targetName }}
        </span>
        <span v-else-if="targetId" class="target-label">
          目标: {{ targetId }}
        </span>
      </div>
      <div class="toolbar-right">
        <span class="completion-hint">Tab 补全 · 连按两次查看候选</span>
        <span class="status-badge" :class="status">
          <span class="status-dot"></span>
          {{ statusLabel }}
        </span>
      </div>
    </div>

    <!-- 终端容器 -->
    <div class="terminal-wrapper">
      <div ref="terminalRef" class="terminal-container"></div>

      <!-- 连接中遮罩 -->
      <div v-if="status === 'connecting'" class="terminal-overlay">
        <el-icon class="is-loading" :size="32"><Loading /></el-icon>
        <p>正在连接...</p>
      </div>

      <!-- 错误遮罩 -->
      <div v-if="status === 'error'" class="terminal-overlay">
        <el-icon :size="32"><WarningFilled /></el-icon>
        <p>{{ error || '连接失败' }}</p>
        <div class="overlay-actions">
          <el-button @click="goBack">返回</el-button>
          <el-button type="primary" @click="handleRetry">重试</el-button>
        </div>
      </div>

      <!-- 断开遮罩 -->
      <div v-if="status === 'disconnected'" class="terminal-overlay">
        <el-icon :size="32"><CircleClose /></el-icon>
        <p>连接已关闭</p>
        <div class="overlay-actions">
          <el-button @click="goBack">返回</el-button>
          <el-button type="primary" @click="handleRetry">重新连接</el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ArrowLeft, CircleClose, Loading, WarningFilled } from '@element-plus/icons-vue';
import { apiClient } from '@/api/client';
import { useWebTerminal, type TerminalStatus } from '@/composables/useWebTerminal';

const route = useRoute();
const router = useRouter();

const targetId = ref('');
const targetName = ref('');

const terminalRef = ref<HTMLElement | null>(null);

const {
  status,
  error,
  connect,
  disconnect,
} = useWebTerminal({ targetId });

const statusLabel = computed(() => {
  const map: Record<TerminalStatus, string> = {
    idle: '就绪',
    connecting: '连接中...',
    connected: '已连接',
    disconnected: '已断开',
    error: '错误',
  };
  return map[status.value] || status.value;
});

function goBack() {
  disconnect();
  router.back();
}

async function handleRetry() {
  if (!terminalRef.value || !targetId.value) return;
  await connect(terminalRef.value);
}

onMounted(async () => {
  const tid = String(route.query.target_id || '');
  targetId.value = tid;

  // 获取目标信息用于工具栏展示
  if (tid) {
    try {
      const target = await apiClient.getTarget(tid);
      if (target) {
        const host = target.host || target.address || '';
        const port = target.port ? `:${target.port}` : '';
        const name = target.name || target.username || tid;
        targetName.value = `${name} (${host}${port})`;
      }
    } catch {
      targetName.value = `目标: ${tid}`;
    }
  }

  if (!terminalRef.value || !tid) return;
  try {
    await connect(terminalRef.value);
  } catch {
    // 错误状态已在 composable 中设置
  }
});

onUnmounted(() => {
  disconnect();
});
</script>

<style scoped>
.web-terminal-page {
  display: flex;
  flex-direction: column;
  height: calc(100vh - 60px); /* 减去 app header 高度 */
  background: #1e1e2e;
}

.terminal-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 40px;
  padding: 0 12px;
  background: #181825;
  border-bottom: 1px solid #313244;
  flex-shrink: 0;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.toolbar-left .el-button {
  color: #cdd6f4;
}

.target-label {
  color: #a6adc8;
  font-size: 13px;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 14px;
}

.completion-hint {
  color: #6c7086;
  font-size: 12px;
}

.status-badge {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #a6adc8;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #585b70;
}

.status-badge.connected .status-dot {
  background: #a6e3a1;
}

.status-badge.error .status-dot {
  background: #f38ba8;
}

.terminal-wrapper {
  flex: 1;
  position: relative;
  overflow: hidden;
}

.terminal-container {
  width: 100%;
  height: 100%;
}

.terminal-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: rgba(30, 30, 46, 0.92);
  color: #cdd6f4;
  gap: 12px;
  z-index: 10;
}

.terminal-overlay p {
  margin: 0;
  font-size: 15px;
}

.overlay-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
</style>
