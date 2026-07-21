# Web Terminal 浏览器交互式 SSH 终端 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在浏览器中提供全屏交互式 SSH 终端，用户从快速连接或主机管理页点击即可在浏览器内直连目标主机。

**Architecture:** 新建 `WebTerminalView.vue` 全屏终端页面 + `useWebTerminal.ts` composable 封装 WebSocket/xterm 绑定逻辑。后端 `/api/web-terminal` 端点已完整实现，零改动。在 QuickConnectView 和 HostsView 的连接弹窗中增加入口按钮。

**Tech Stack:** Vue 3 + TypeScript + @xterm/xterm v6 + Element Plus (UI 组件)

## Global Constraints

- 后端代码零改动（`/api/web-terminal` WebSocket 端点已实现）
- 前端 typecheck 必须通过：`npm run typecheck`
- 前端 build 必须通过：`npm run build`
- 不在侧边栏菜单中显示终端页路由
- 使用 git worktree 进行开发（遵循 CLAUDE.md 规范）

---

### Task 1: 安装 xterm 插件依赖

**Files:**
- Modify: `web/package.json`

**Interfaces:**
- Produces: `@xterm/addon-fit`、`@xterm/addon-web-links` 可供 import

- [ ] **Step 1: 安装 addon 包**

```bash
cd web && npm install @xterm/addon-fit @xterm/addon-web-links
```

- [ ] **Step 2: 验证安装**

```bash
cd web && node -e "require('@xterm/addon-fit'); require('@xterm/addon-web-links'); console.log('OK')"
```

Expected: 输出 `OK`

- [ ] **Step 3: Commit**

```bash
git add web/package.json web/package-lock.json
git commit -m "chore: add xterm addon-fit and addon-web-links dependencies"
```

---

### Task 2: 创建 useWebTerminal.ts composable

**Files:**
- Create: `web/src/composables/useWebTerminal.ts`

**Interfaces:**
- Produces: `useWebTerminal(opts: UseWebTerminalOptions): UseWebTerminalReturn`
  - `UseWebTerminalOptions = { targetId: Ref<string>; cols?: number; rows?: number }`
  - `UseWebTerminalReturn = { terminal: Ref<Terminal | null>; status: Ref<TerminalStatus>; error: Ref<string>; connect(container: HTMLElement): Promise<void>; disconnect(): void }`
  - `TerminalStatus = 'idle' | 'connecting' | 'connected' | 'disconnected' | 'error'`
  - 注意：`targetId` 是 `Ref<string>`，composable 在 `connect()` 调用时读取 `.value`，确保获取到最新值

- [ ] **Step 1: 创建文件及全部实现**

```bash
mkdir -p web/src/composables
```

写入 `web/src/composables/useWebTerminal.ts`：

```typescript
import { ref, type Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';

export type TerminalStatus = 'idle' | 'connecting' | 'connected' | 'disconnected' | 'error';

export interface UseWebTerminalOptions {
  targetId: Ref<string>;
  cols?: number;
  rows?: number;
}

export interface UseWebTerminalReturn {
  terminal: Ref<Terminal | null>;
  status: Ref<TerminalStatus>;
  error: Ref<string>;
  connect(container: HTMLElement): Promise<void>;
  disconnect(): void;
}

function buildWsUrl(targetId: string, cols: number, rows: number): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const token = localStorage.getItem('jianmen_token') || '';
  return (
    `${protocol}//${window.location.host}/api/web-terminal` +
    `?target_id=${encodeURIComponent(targetId)}` +
    `&token=${encodeURIComponent(token)}` +
    `&cols=${cols}&rows=${rows}` +
    `&term=xterm-256color`
  );
}

function defaultTerminalOptions(cols: number, rows: number) {
  return {
    cursorBlink: true,
    fontSize: 14,
    fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
    theme: {
      background: '#1e1e2e',
      foreground: '#cdd6f4',
      cursor: '#f5e0dc',
      selectionBackground: '#585b70',
      black: '#45475a',
      red: '#f38ba8',
      green: '#a6e3a1',
      yellow: '#f9e2af',
      blue: '#89b4fa',
      magenta: '#f5c2e7',
      cyan: '#94e2d5',
      white: '#bac2de',
      brightBlack: '#585b70',
    },
    cols,
    rows,
  };
}

export function useWebTerminal(opts: UseWebTerminalOptions): UseWebTerminalReturn {
  const terminal = ref<Terminal | null>(null);
  const status = ref<TerminalStatus>('idle');
  const error = ref('');

  const cols = opts.cols || 80;
  const rows = opts.rows || 24;

  let ws: WebSocket | null = null;
  let fitAddon: FitAddon | null = null;
  let resizeObserver: ResizeObserver | null = null;

  async function connect(container: HTMLElement): Promise<void> {
    if (status.value === 'connected' || status.value === 'connecting') return;

    status.value = 'connecting';
    error.value = '';

    const term = new Terminal(defaultTerminalOptions(cols, rows));

    fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(new WebLinksAddon());

    term.open(container);
    fitAddon.fit();
    terminal.value = term;

    try {
      await new Promise<void>((resolve, reject) => {
        const url = buildWsUrl(opts.targetId.value, cols, rows);
        ws = new WebSocket(url);
        ws.binaryType = 'arraybuffer';

        ws.onopen = () => {
          status.value = 'connected';
          resolve();
        };

        ws.onerror = () => {
          // onclose will fire after this, reject there if still connecting
        };

        ws.onclose = (event) => {
          if (status.value === 'connecting') {
            const msg = event.code === 4001
              ? '认证失败，请重新登录'
              : `WebSocket 连接关闭 (code: ${event.code})`;
            error.value = msg;
            status.value = 'error';
            reject(new Error(msg));
          } else if (status.value === 'connected') {
            status.value = 'disconnected';
            if (terminal.value) {
              terminal.value.options.disableStdin = true;
            }
          }
        };

        ws.onmessage = (event) => {
          if (status.value !== 'connected' || !terminal.value) return;
          if (event.data instanceof ArrayBuffer) {
            terminal.value.write(new Uint8Array(event.data));
          } else if (typeof event.data === 'string') {
            terminal.value.write(event.data);
          }
        };

        term.onData((data) => {
          if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(data);
          }
        });
      });

      // ResizeObserver — fit terminal to container + send resize to server
      resizeObserver = new ResizeObserver(() => {
        if (!fitAddon || !terminal.value) return;
        fitAddon.fit();
        if (ws && ws.readyState === WebSocket.OPEN) {
          const t = terminal.value;
          if (t.cols > 0 && t.rows > 0) {
            ws.send(JSON.stringify({ type: 'resize', cols: t.cols, rows: t.rows }));
          }
        }
      });
      resizeObserver.observe(container);
    } catch (e) {
      // If status wasn't already set by onclose, mark as error
      if (status.value !== 'error') {
        error.value = e instanceof Error ? e.message : '连接失败';
        status.value = 'error';
      }
      throw e;
    }
  }

  function disconnect(): void {
    if (resizeObserver) {
      resizeObserver.disconnect();
      resizeObserver = null;
    }
    if (ws) {
      ws.onclose = null; // 防止 onclose 再次修改 status
      ws.close(1000, 'user disconnected');
      ws = null;
    }
    if (terminal.value) {
      terminal.value.dispose();
      terminal.value = null;
    }
    status.value = 'disconnected';
    fitAddon = null;
  }

  return { terminal, status, error, connect, disconnect };
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/composables/useWebTerminal.ts
git commit -m "feat: add useWebTerminal composable for WebSocket + xterm binding"
```

---

### Task 3: 创建 WebTerminalView.vue 终端页面

**Files:**
- Create: `web/src/views/WebTerminalView.vue`

**Interfaces:**
- Consumes: `useWebTerminal()` from `@/composables/useWebTerminal`
- Consumes: `apiClient.getTarget()` from `@/api/client`
- Consumes: `getToken()` from `@/api/client`
- Route query params: `target_id: string`

- [ ] **Step 1: 创建完整的 WebTerminalView.vue**

写入 `web/src/views/WebTerminalView.vue`：

```vue
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
import { apiClient, getToken } from '@/api/client';
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

  if (!terminalRef.value || !tid || !getToken()) return;
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
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/WebTerminalView.vue
git commit -m "feat: add WebTerminalView for browser-based interactive SSH terminal"
```

---

### Task 4: 添加 /web-terminal 路由

**Files:**
- Modify: `web/src/router/index.ts`

**Interfaces:**
- Produces: 路由 `/web-terminal` 指向 `WebTerminalView`

- [ ] **Step 1: 添加路由懒加载和路由记录**

在 `web/src/router/index.ts` 中，在已有的懒加载 import 区域（`DatabaseView` import 之后）添加：

```typescript
const WebTerminalView = () => import('@/views/WebTerminalView.vue');
```

在 routes 数组中，在 `/audit` 路由之后、`/:pathMatch(.*)*` 之前，添加：

```typescript
  {
    path: '/web-terminal',
    name: 'web-terminal',
    component: WebTerminalView,
    meta: {
      titleKey: 'quickConnect.config.webTerminal',
      descriptionKey: 'quickConnect.config.webTerminal',
    } satisfies AppRouteMeta
  },
```

- [ ] **Step 2: Commit**

```bash
git add web/src/router/index.ts
git commit -m "feat: add /web-terminal route"
```

---

### Task 5: QuickConnectView 连接弹窗增加 [在浏览器中打开] 按钮

**Files:**
- Modify: `web/src/views/QuickConnectView.vue`

**Interfaces:**
- Consumes: `useRouter` from `vue-router`

- [ ] **Step 1: 添加 import 和状态变量**

在 script setup 的 import 区域，修改 vue import：

```typescript
import { computed, onMounted, ref, watch } from 'vue';
// 改为：
import { computed, onMounted, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
```

在现有 `connectType` 和 `dialogTitle` 附近，添加：

```typescript
const router = useRouter();
const webTerminalTargetId = ref('');
```

- [ ] **Step 2: 在 openSSHConfig 中保存 targetId**

修改 `openSSHConfig` 函数，在获取 tid 后存储：

```typescript
async function openSSHConfig(target: TargetRecord) {
  connectType.value = 'ssh';
  sessionError.value = ''; creatingSession.value = true; configVisible.value = true;
  try {
    const tid = String(target.id || target.resource_id || '');
    webTerminalTargetId.value = tid;  // 新增：保存用于浏览器终端入口
    const s = await apiClient.createUserSession(tid);
    // ... 其余代码不变
```

- [ ] **Step 3: 在模板中添加按钮**

在 `config-dialog` 的 `<el-descriptions>` 区域之后、连接命令 input 之前（或之后），添加按钮。定位到 `</el-descriptions>` 闭合标签之后，`<div style="margin-top: 12px">` 之前：

```html
              <div v-if="connectInfo?.compactUser" style="margin-top: 12px; text-align: right">
                <el-button type="primary" @click="openInBrowser">
                  在浏览器中打开
                </el-button>
              </div>
```

- [ ] **Step 4: 添加 openInBrowser 方法**

在 `copyValue` 函数之后添加：

```typescript
function openInBrowser() {
  if (!webTerminalTargetId.value) return;
  configVisible.value = false;
  router.push({ path: '/web-terminal', query: { target_id: webTerminalTargetId.value } });
}
```

- [ ] **Step 5: Commit**

```bash
git add web/src/views/QuickConnectView.vue
git commit -m "feat: add browser terminal entry button to QuickConnectView"
```

---

### Task 6: HostsView 连接弹窗增加 [在浏览器中打开] 按钮

**Files:**
- Modify: `web/src/views/HostsView.vue`

**Interfaces:**
- Consumes: `useRouter` from `vue-router`

- [ ] **Step 1: 在 footer 中添加按钮**

定位到连接弹窗的 footer 区域（第 602-604 行），在关闭按钮前添加"在浏览器中打开"按钮：

```html
        <template #footer>
          <el-button type="primary" @click="openTerminalFromDialog">在浏览器中打开</el-button>
          <el-button @click="connectionDialogVisible = false">关闭</el-button>
        </template>
```

- [ ] **Step 2: 添加 import**

在 script setup 的 import 区域，修改 vue import：

```typescript
import { computed, nextTick, onMounted, reactive, ref, watch } from "vue";
// 改为：
import { computed, nextTick, onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
```

在现有 ref 声明区域（`connectionCompactUser` 附近），添加：

```typescript
const router = useRouter();
```

- [ ] **Step 3: 添加 openTerminalFromDialog 方法**

在 `copyText` 函数之后添加：

```typescript
function openTerminalFromDialog() {
  const target = selectedConnectionTarget.value;
  if (!target) return;
  const tid = String(target.id || target.resource_id || '');
  if (!tid) {
    ElMessage.warning('无法获取目标资源ID');
    return;
  }
  connectionDialogVisible.value = false;
  router.push({ path: '/web-terminal', query: { target_id: tid } });
}
```

- [ ] **Step 4: Commit**

```bash
git add web/src/views/HostsView.vue
git commit -m "feat: add browser terminal entry button to HostsView connection dialog"
```

---

### Task 7: 验证 — typecheck + build

**Files:**
- 无新文件（仅验证）

- [ ] **Step 1: 前端 typecheck**

```bash
cd web && npm run typecheck
```

Expected: 无类型错误，退出码 0。

- [ ] **Step 2: 前端 build**

```bash
cd web && npm run build
```

Expected: 构建成功，`dist/` 目录生成。

- [ ] **Step 3: 后端 go build**

```bash
go build ./...
```

Expected: 编译成功（确保未意外影响 Go 代码）。

- [ ] **Step 4: 后端 go test**

```bash
go test ./... -count=1
```

Expected: 全部通过（webterminal_test.go 已有 4 个测试用例）。

- [ ] **Step 5: Commit（如有改动）**

如有 lint/typecheck 修复，单独提交。否则验证通过即完成。
