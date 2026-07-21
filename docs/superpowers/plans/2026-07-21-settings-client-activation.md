# 个人设置页客户端激活流程改版 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 重构个人设置页的客户端配置保存流程，让“界面与终端”直接保存，而 SSH/数据库客户端页签改为“先弹激活命令弹窗，点击已激活后只保存当前页签字段到后端和浏览器”。

**Architecture:** 保留 `web/src/views/SettingsView.vue` 作为页签编排层，但把页签保存动作拆开，并把命令展示从页内警告框迁移到独立弹窗组件。底层配置元数据与浏览器缓存写入逻辑放在 `web/src/config/sshClients.ts` 和 `web/src/stores/preferences.ts`，页面层只消费明确的 helper 和按页签字段集执行保存。

**Tech Stack:** Vue 3 + `<script setup lang="ts">`、Pinia、Element Plus、Vitest、tsx/node:test、Vite

## Global Constraints

- 去掉卡片头部的全局“保存配置”按钮。
- 每个页签内容区顶部右侧提供独立保存按钮。
- `界面与终端` 页签点击保存时直接保存。
- `SSH 客户端` 和 `数据库客户端` 页签点击保存时先弹出激活弹窗。
- 只有在弹窗内点击“已激活”后，才真正把当前页签相关字段保存到后端并写入当前浏览器。
- 协议注册命令、复制命令按钮、激活确认按钮全部从页签正文迁移到弹窗内。
- SSH 客户端默认使用 `Xshell`，下拉中不再提供“系统默认 SSH 协议”“系统 SSH (ssh.exe)”两个新选项。
- SSH 客户端和数据库客户端的 `macOS`、`Linux` 平台选项都改成禁用。
- 数据库客户端标题描述必须改成 `设置数据库快速连接使用的数据库客户端。`
- 数据库客户端路径帮助文案必须改成 `无法自动读取完整路径时，请手动粘贴。`
- 数据库客户端 CA 路径帮助文案必须改成 `当使用私有CA、自签证书且开启客户端TLS连接时下载网关CA到电脑，然后填写文件在电脑的路径`
- 点击客户端页签“保存配置”时，即使没有改动，也照样弹激活命令弹窗。
- 只保存当前页签字段到后端和浏览器缓存，不能覆盖其他页签未提交的本地改动。
- 旧 `ssh_client=default/system`、旧 `platform=macos/linux` 值必须可显示，但不能自动迁移。
- 注释使用中文。
- 开发新功能时使用 git worktree，不直接在主工作区改项目。

---

## File Structure

- Create: `web/src/config/sshClients.test.ts` — 覆盖设置页 SSH 客户端选项过滤、旧值回显、平台禁用规则。
- Create: `web/src/stores/preferences.test.ts` — 覆盖浏览器缓存部分写入与 `xshell` 默认值。
- Create: `web/src/components/settings/ClientSectionHeading.vue` — 统一标题/状态/右侧操作区布局。
- Create: `web/src/components/settings/ClientActivationDialog.vue` — 统一激活命令弹窗。
- Create: `web/src/components/settings/ClientActivationDialog.mount.test.ts` — 覆盖弹窗提示文案、复制/确认事件。
- Create: `web/src/views/SettingsView.mount.test.ts` — 覆盖三个页签的保存/弹窗/只保存当前字段行为。
- Modify: `web/src/config/sshClients.ts` — 新增设置页专用客户端选项、平台选项和激活支持判断 helper。
- Modify: `web/src/stores/preferences.ts` — 新增字段级浏览器缓存写入 helper，并把默认 SSH 客户端改成 `xshell`。
- Modify: `web/src/views/SettingsView.vue` — 引入新组件，拆分按页签保存流程，新增数据库路径选择按钮和更新文案。
- Modify: `web/src/utils/databaseGatewayCommands.test.ts` — 更新源码断言，验证旧命令区移除、新弹窗文案存在、脚本已纳入新测试文件。
- Modify: `web/package.json` — 把新增 test 文件接入 `test:connection-commands` / `test:connection-dialog`。

## Task 1: 收敛设置页配置元数据与浏览器缓存 helper

**Files:**
- Create: `web/src/config/sshClients.test.ts`
- Create: `web/src/stores/preferences.test.ts`
- Modify: `web/src/config/sshClients.ts`
- Modify: `web/src/stores/preferences.ts`

**Interfaces:**
- Consumes:
  - `SSH_CLIENT_OPTIONS` from `web/src/config/sshClients.ts`
  - `CLIENT_PLATFORM_OPTIONS` from `web/src/config/sshClients.ts`
  - `UserPreferences` from `web/src/api/client.ts`
  - `usePreferencesStore()` from `web/src/stores/preferences.ts`
- Produces:
  - `SETTINGS_CLIENT_PLATFORM_OPTIONS: ReadonlyArray<{ label: string; value: ClientPlatform; disabled?: boolean }>`
  - `SETTINGS_SSH_CLIENT_OPTIONS: ReadonlyArray<SSHClientSelectOption>`
  - `buildSettingsSSHClientOptions(currentCommand: string): SSHClientSelectOption[]`
  - `isSupportedSSHClientForActivation(command: string): boolean`
  - `persistPartialToBrowser(patch: Partial<UserPreferences>): void`

- [ ] **Step 1: 写失败测试，固定设置页选项过滤和浏览器缓存行为**

在 `web/src/config/sshClients.test.ts` 写入：

```ts
import assert from 'node:assert/strict';
import test from 'node:test';

import {
  SETTINGS_CLIENT_PLATFORM_OPTIONS,
  SETTINGS_SSH_CLIENT_OPTIONS,
  buildSettingsSSHClientOptions,
  isSupportedSSHClientForActivation,
} from './sshClients.ts';

test('settings SSH options exclude system clients but keep the current legacy value visible', () => {
  assert.deepEqual(
    SETTINGS_SSH_CLIENT_OPTIONS.map(option => option.command),
    ['xshell', 'putty', 'securecrt', 'mobaxterm', 'winterm'],
  );

  assert.equal(isSupportedSSHClientForActivation('xshell'), true);
  assert.equal(isSupportedSSHClientForActivation('default'), false);
  assert.equal(isSupportedSSHClientForActivation('system'), false);

  const legacyOptions = buildSettingsSSHClientOptions('default');
  assert.equal(legacyOptions[0].command, 'default');
  assert.equal(legacyOptions[0].disabled, true);
  assert.match(legacyOptions[0].label, /系统默认 SSH 协议/);
});

test('settings platform options keep windows enabled and disable macOS/linux', () => {
  assert.deepEqual(SETTINGS_CLIENT_PLATFORM_OPTIONS, [
    { label: 'Windows', value: 'windows' },
    { label: 'macOS', value: 'macos', disabled: true },
    { label: 'Linux', value: 'linux', disabled: true },
  ]);
});
```

在 `web/src/stores/preferences.test.ts` 写入：

```ts
import assert from 'node:assert/strict';
import test from 'node:test';
import { createPinia, setActivePinia } from 'pinia';

import { usePreferencesStore } from './preferences.ts';

test('preferences store defaults ssh client to xshell when nothing is saved', () => {
  const storage = createStorage();
  const localStorageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');

  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage });

  try {
    setActivePinia(createPinia());
    const store = usePreferencesStore();
    assert.equal(store.value.ssh_client, 'xshell');
  } finally {
    restoreGlobalProperty('localStorage', localStorageDescriptor);
  }
});

test('persistPartialToBrowser merges only the requested fields', () => {
  const storage = createStorage({
    jianmen_client_config: JSON.stringify({
      theme: 'dark',
      terminal_font_family: 'Cascadia Mono',
      ssh_client: 'xshell',
      ssh_client_path: 'C:\\Tools\\Xshell\\Xshell.exe',
    }),
  });
  const localStorageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');

  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage });

  try {
    setActivePinia(createPinia());
    const store = usePreferencesStore();

    store.persistPartialToBrowser({
      db_client: 'dbeaver',
      db_client_path: 'C:\\Program Files\\DBeaver\\dbeaverc.exe',
    });

    const cached = JSON.parse(storage.getItem('jianmen_client_config') || '{}');
    assert.equal(cached.theme, 'dark');
    assert.equal(cached.ssh_client, 'xshell');
    assert.equal(cached.db_client, 'dbeaver');
    assert.equal(cached.db_client_path, 'C:\\Program Files\\DBeaver\\dbeaverc.exe');
  } finally {
    restoreGlobalProperty('localStorage', localStorageDescriptor);
  }
});

function createStorage(seed: Record<string, string> = {}): Storage {
  const values = new Map(Object.entries(seed));
  return {
    get length() { return values.size; },
    clear() { values.clear(); },
    getItem(key) { return values.get(key) ?? null; },
    key(index) { return [...values.keys()][index] ?? null; },
    removeItem(key) { values.delete(key); },
    setItem(key, value) { values.set(key, value); },
  };
}

function restoreGlobalProperty(name: 'localStorage', descriptor?: PropertyDescriptor) {
  if (descriptor) Object.defineProperty(globalThis, name, descriptor);
  else Reflect.deleteProperty(globalThis, name);
}
```

- [ ] **Step 2: 运行测试，确认它们先失败**

在 `web/` 目录执行：

```bash
npm exec tsx --test src/config/sshClients.test.ts src/stores/preferences.test.ts
```

Expected: FAIL，报错包含以下任一信息即可：
- `SETTINGS_SSH_CLIENT_OPTIONS` / `buildSettingsSSHClientOptions` / `isSupportedSSHClientForActivation` 未导出
- `persistPartialToBrowser` 不存在
- `ssh_client` 默认值仍然是空字符串

- [ ] **Step 3: 写最小实现，导出设置页专用选项与缓存 helper**

在 `web/src/config/sshClients.ts` 增加设置页专用类型与 helper：

```ts
export interface SSHClientSelectOption extends SSHClientOption {
  disabled?: boolean;
}

const SETTINGS_HIDDEN_SSH_CLIENTS = new Set(['default', 'system']);

export const SETTINGS_SSH_CLIENT_OPTIONS: ReadonlyArray<SSHClientSelectOption> =
  SSH_CLIENT_OPTIONS
    .filter(option => !SETTINGS_HIDDEN_SSH_CLIENTS.has(option.command))
    .map(option => ({ ...option }));

export const SETTINGS_CLIENT_PLATFORM_OPTIONS = [
  { label: 'Windows', value: 'windows' },
  { label: 'macOS', value: 'macos', disabled: true },
  { label: 'Linux', value: 'linux', disabled: true },
] as const;

export function isSupportedSSHClientForActivation(command: string): boolean {
  return SETTINGS_SSH_CLIENT_OPTIONS.some(option => option.command === command);
}

export function buildSettingsSSHClientOptions(currentCommand: string): SSHClientSelectOption[] {
  const current = SSH_CLIENT_OPTIONS.find(option => option.command === currentCommand);
  if (current && SETTINGS_HIDDEN_SSH_CLIENTS.has(current.command)) {
    return [{ ...current, disabled: true }, ...SETTINGS_SSH_CLIENT_OPTIONS];
  }
  return [...SETTINGS_SSH_CLIENT_OPTIONS];
}
```

在 `web/src/stores/preferences.ts` 收口默认值与缓存 helper：

```ts
const defaults: UserPreferences = {
  theme: 'light',
  ssh_client: 'xshell',
  ssh_client_path: '',
  ssh_client_platform: 'windows',
  db_client: 'dbeaver',
  db_client_platform: 'windows',
  db_client_path: '',
  db_client_ca_file_path: '',
  terminal_font_family: 'Cascadia Mono, Consolas, monospace',
  terminal_font_size: 14,
};

function persistPartialToBrowser(patch: Partial<UserPreferences>) {
  localStorage.setItem(CLIENT_CACHE_KEY, JSON.stringify({
    ...JSON.parse(localStorage.getItem(CLIENT_CACHE_KEY) || '{}'),
    ...patch,
  }));
}

return {
  value,
  loaded,
  loading,
  saving,
  error,
  sshProtocolRegistered,
  hasSSHClient,
  hasDBClient,
  fetch,
  update,
  loadToBrowser,
  persistPartialToBrowser,
  markSSHProtocolRegistered,
  apply,
  reset,
};
```

如果 `cachedClientConfig()` 或 `fetch()` 的合并顺序会把 `ssh_client` 又变回空字符串，顺手把空字符串分支改成忽略空值，确保最终 `store.value.ssh_client === 'xshell'`。

- [ ] **Step 4: 重新运行测试，确认 helper 可用**

在 `web/` 目录执行：

```bash
npm exec tsx --test src/config/sshClients.test.ts src/stores/preferences.test.ts
```

Expected: PASS，输出包含 `4 tests` / `0 failed`。

- [ ] **Step 5: Commit**

```bash
git add web/src/config/sshClients.ts web/src/config/sshClients.test.ts web/src/stores/preferences.ts web/src/stores/preferences.test.ts
git commit -m "测试: 补齐个人设置客户端配置辅助逻辑"
```

### Task 2: 抽出设置页标题组件与激活弹窗组件

**Files:**
- Create: `web/src/components/settings/ClientSectionHeading.vue`
- Create: `web/src/components/settings/ClientActivationDialog.vue`
- Create: `web/src/components/settings/ClientActivationDialog.mount.test.ts`

**Interfaces:**
- Consumes:
  - `ElTag`, `ElDialog`, `ElInput`, `ElButton` from Element Plus
- Produces:
  - `ClientSectionHeading` props:
    - `title: string`
    - `desc?: string`
    - `configured?: boolean`
    - `registered?: boolean`
    - slot `actions`
  - `ClientActivationDialog` props/emits:
    - `modelValue: boolean`
    - `title: string`
    - `command: string`
    - `loading?: boolean`
    - emits: `update:modelValue`, `copy`, `confirm`

- [ ] **Step 1: 写失败测试，固定弹窗文案和事件接口**

在 `web/src/components/settings/ClientActivationDialog.mount.test.ts` 写入：

```ts
import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';

import ClientActivationDialog from './ClientActivationDialog.vue';

vi.mock('element-plus', async () => {
  const { defineComponent, h } = await import('vue');

  const ElDialog = defineComponent({
    props: { modelValue: Boolean },
    emits: ['update:modelValue'],
    setup(props, { slots }) {
      return () => props.modelValue ? h('div', { 'data-testid': 'client-activation-dialog' }, slots.default?.()) : null;
    },
  });

  const ElInput = defineComponent({
    props: { modelValue: { type: String, default: '' }, readonly: Boolean },
    setup(props) {
      return () => h('textarea', { value: props.modelValue, readonly: props.readonly, 'data-testid': 'client-activation-command' });
    },
  });

  const ElButton = defineComponent({
    props: { disabled: Boolean, loading: Boolean },
    emits: ['click'],
    setup(props, { attrs, slots, emit }) {
      return () => h('button', {
        ...attrs,
        disabled: props.disabled,
        'data-loading': props.loading ? 'true' : undefined,
        onClick: () => emit('click'),
      }, slots.default?.());
    },
  });

  return { ElDialog, ElInput, ElButton };
});

describe('ClientActivationDialog', () => {
  it('shows the activation copy and confirm actions', async () => {
    const wrapper = mount(ClientActivationDialog, {
      props: {
        modelValue: true,
        title: '激活本地 SSH 客户端',
        command: 'reg add HKCR\\ssh ...',
      },
    });

    expect(wrapper.text()).toContain('请执行协议注册命令，激活本地客户端');
    expect(wrapper.get('[data-testid="client-activation-command"]').attributes('readonly')).toBeDefined();

    await wrapper.get('[data-testid="client-activation-copy"]').trigger('click');
    await wrapper.get('[data-testid="client-activation-confirm"]').trigger('click');

    expect(wrapper.emitted('copy')).toHaveLength(1);
    expect(wrapper.emitted('confirm')).toHaveLength(1);
  });
});
```

- [ ] **Step 2: 运行测试，确认组件尚不存在**

在 `web/` 目录执行：

```bash
npm exec vitest run src/components/settings/ClientActivationDialog.mount.test.ts
```

Expected: FAIL，报错包含 `Failed to resolve import './ClientActivationDialog.vue'`。

- [ ] **Step 3: 用 SFC 抽出标题组件和激活弹窗组件**

创建 `web/src/components/settings/ClientSectionHeading.vue`：

```vue
<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
  title: string;
  desc?: string;
  configured?: boolean;
  registered?: boolean;
}>();

const statusLabel = computed(() => {
  if (!props.configured) return '未配置';
  return props.registered ? '已就绪' : '待注册协议';
});

const statusType = computed(() => {
  if (!props.configured) return 'info';
  return props.registered ? 'success' : 'warning';
});
</script>

<template>
  <div class="section-heading">
    <div>
      <h2>{{ title }}</h2>
      <p v-if="desc">{{ desc }}</p>
    </div>
    <div class="section-heading__actions">
      <el-tag :type="statusType" effect="light">{{ statusLabel }}</el-tag>
      <slot name="actions" />
    </div>
  </div>
</template>
```

创建 `web/src/components/settings/ClientActivationDialog.vue`：

```vue
<script setup lang="ts">
const props = defineProps<{
  modelValue: boolean;
  title: string;
  command: string;
  loading?: boolean;
}>();

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void;
  (event: 'copy'): void;
  (event: 'confirm'): void;
}>();
</script>

<template>
  <el-dialog
    :model-value="props.modelValue"
    :title="props.title"
    width="680px"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <p class="activation-copy">请执行协议注册命令，激活本地客户端</p>
    <el-input
      data-testid="client-activation-command"
      type="textarea"
      :model-value="props.command"
      readonly
      :rows="4"
      class="activation-command"
    />
    <template #footer>
      <el-button data-testid="client-activation-copy" @click="emit('copy')">复制命令</el-button>
      <el-button
        data-testid="client-activation-confirm"
        type="primary"
        :loading="props.loading"
        :disabled="!props.command"
        @click="emit('confirm')"
      >
        已激活
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.activation-copy {
  margin: 0 0 12px;
  color: var(--color-text-secondary);
  font-size: 13px;
}

.activation-command :deep(.el-textarea__inner) {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
}
</style>
```

- [ ] **Step 4: 重新运行弹窗测试，确认接口稳定**

在 `web/` 目录执行：

```bash
npm exec vitest run src/components/settings/ClientActivationDialog.mount.test.ts
```

Expected: PASS，输出包含 `1 passed`。

- [ ] **Step 5: Commit**

```bash
git add web/src/components/settings/ClientSectionHeading.vue web/src/components/settings/ClientActivationDialog.vue web/src/components/settings/ClientActivationDialog.mount.test.ts
git commit -m "重构: 抽出个人设置客户端激活组件"
```

### Task 3: 重写 SettingsView 为按页签保存和弹窗激活流程

**Files:**
- Modify: `web/src/views/SettingsView.vue`
- Create: `web/src/views/SettingsView.mount.test.ts`

**Interfaces:**
- Consumes:
  - `ClientSectionHeading` from `web/src/components/settings/ClientSectionHeading.vue`
  - `ClientActivationDialog` from `web/src/components/settings/ClientActivationDialog.vue`
  - `SETTINGS_CLIENT_PLATFORM_OPTIONS`, `buildSettingsSSHClientOptions`, `isSupportedSSHClientForActivation` from `web/src/config/sshClients.ts`
  - `persistPartialToBrowser(patch)` from `web/src/stores/preferences.ts`
- Produces:
  - `saveAppearanceSettings(): Promise<void>`
  - `openSSHActivationDialog(): void`
  - `confirmSSHActivation(): Promise<void>`
  - `openDatabaseActivationDialog(): void`
  - `confirmDatabaseActivation(): Promise<void>`
  - `pickDatabaseExecutable(): void`
  - local helpers:
    - `pickFormPatch<const K extends readonly PreferenceField[]>(keys: K): Pick<UserPreferences, K[number]>`
    - `applySavedFields<const K extends readonly PreferenceField[]>(keys: K, saved: UserPreferences): void`

- [ ] **Step 1: 写失败测试，先锁住按页签保存和弹窗确认行为**

在 `web/src/views/SettingsView.mount.test.ts` 写入：

```ts
import { flushPromises, mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  preferences: {
    loading: false,
    saving: false,
    loaded: true,
    error: '',
    sshProtocolRegistered: false,
    value: {
      theme: 'light',
      terminal_font_family: 'Cascadia Mono',
      terminal_font_size: 14,
      ssh_client: 'xshell',
      ssh_client_platform: 'windows',
      ssh_client_path: 'C:\\Tools\\Xshell\\Xshell.exe',
      db_client: 'dbeaver',
      db_client_platform: 'windows',
      db_client_path: 'C:\\Program Files\\DBeaver\\dbeaverc.exe',
      db_client_ca_file_path: '',
    },
    fetch: vi.fn(async () => mocks.preferences.value),
    update: vi.fn(async patch => ({ ...mocks.preferences.value, ...patch })),
    persistPartialToBrowser: vi.fn(),
    markSSHProtocolRegistered: vi.fn(),
  },
  databaseClient: {
    protocolRegistered: false,
    configured: true,
    directLaunchReady: false,
    markRegistered: vi.fn(),
    markUnregistered: vi.fn(),
  },
  writeClipboardText: vi.fn(),
  getDBGateway: vi.fn(),
  routerReplace: vi.fn(),
  route: { name: 'settings', query: { tab: 'appearance' } },
}));

vi.mock('vue-router', () => ({
  useRoute: () => mocks.route,
  useRouter: () => ({ replace: mocks.routerReplace }),
}));

vi.mock('@/stores/preferences', () => ({
  usePreferencesStore: () => mocks.preferences,
}));

vi.mock('@/stores/databaseClient', () => ({
  useDatabaseClientStore: () => mocks.databaseClient,
}));

vi.mock('@/api/client', () => ({
  apiClient: { getDBGateway: mocks.getDBGateway },
}));

vi.mock('@/utils/clipboard', () => ({
  writeClipboardText: mocks.writeClipboardText,
}));

import SettingsView from './SettingsView.vue';

describe('SettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('saves appearance fields directly without opening an activation dialog', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body });
    await flushPromises();

    await wrapper.get('[data-testid="settings-save-appearance"]').trigger('click');

    expect(mocks.preferences.update).toHaveBeenCalledWith({
      theme: 'light',
      terminal_font_family: 'Cascadia Mono',
      terminal_font_size: 14,
    });
    expect(wrapper.find('[data-testid="client-activation-dialog"]').exists()).toBe(false);
  });

  it('opens the SSH activation dialog and saves only ssh fields after confirmation', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body });
    await flushPromises();

    await wrapper.get('[data-testid="settings-save-ssh"]').trigger('click');
    expect(wrapper.get('[data-testid="client-activation-dialog"]').text()).toContain('请执行协议注册命令，激活本地客户端');
    expect(mocks.preferences.update).not.toHaveBeenCalled();

    await wrapper.get('[data-testid="client-activation-confirm"]').trigger('click');

    expect(mocks.preferences.update).toHaveBeenCalledWith({
      ssh_client: 'xshell',
      ssh_client_platform: 'windows',
      ssh_client_path: 'C:\\Tools\\Xshell\\Xshell.exe',
    });
    expect(mocks.preferences.persistPartialToBrowser).toHaveBeenCalledWith({
      ssh_client: 'xshell',
      ssh_client_platform: 'windows',
      ssh_client_path: 'C:\\Tools\\Xshell\\Xshell.exe',
    });
    expect(mocks.preferences.markSSHProtocolRegistered).toHaveBeenCalledWith(true);
  });

  it('opens the database activation dialog and saves only database fields after confirmation', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body });
    await flushPromises();

    await wrapper.get('[data-testid="settings-save-database"]').trigger('click');
    await wrapper.get('[data-testid="client-activation-confirm"]').trigger('click');

    expect(mocks.preferences.update).toHaveBeenCalledWith({
      db_client: 'dbeaver',
      db_client_platform: 'windows',
      db_client_path: 'C:\\Program Files\\DBeaver\\dbeaverc.exe',
      db_client_ca_file_path: '',
    });
    expect(mocks.preferences.persistPartialToBrowser).toHaveBeenCalledWith({
      db_client: 'dbeaver',
      db_client_platform: 'windows',
      db_client_path: 'C:\\Program Files\\DBeaver\\dbeaverc.exe',
      db_client_ca_file_path: '',
    });
    expect(mocks.databaseClient.markRegistered).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: 运行视图测试，确认当前页面还不满足新交互**

在 `web/` 目录执行：

```bash
npm exec vitest run src/views/SettingsView.mount.test.ts
```

Expected: FAIL，报错包含以下任一信息即可：
- 找不到 `[data-testid="settings-save-appearance"]` / `[data-testid="settings-save-ssh"]` / `[data-testid="settings-save-database"]`
- `preferences.update` 被过早调用（还没点“已激活”）
- 仍然渲染旧的页内命令区，而不是弹窗

- [ ] **Step 3: 用最小重构把 SettingsView 改成按页签保存**

先在 `web/src/views/SettingsView.vue` 顶部引入新组件和 helper：

```ts
import ClientActivationDialog from '@/components/settings/ClientActivationDialog.vue';
import ClientSectionHeading from '@/components/settings/ClientSectionHeading.vue';
import type { UserPreferences } from '@/api/client';
import {
  SETTINGS_CLIENT_PLATFORM_OPTIONS,
  buildSettingsSSHClientOptions,
  buildSSHProtocolRegistrationCommand,
  isAbsoluteExecutablePath,
  isSupportedSSHClientForActivation,
  type ClientPlatform,
} from '@/config/sshClients';
```

在 `setup` 区定义字段集和局部状态：

```ts
const APPEARANCE_FIELDS = ['theme', 'terminal_font_family', 'terminal_font_size'] as const;
const SSH_FIELDS = ['ssh_client', 'ssh_client_platform', 'ssh_client_path'] as const;
const DATABASE_FIELDS = ['db_client', 'db_client_platform', 'db_client_path', 'db_client_ca_file_path'] as const;

type PreferenceField = keyof UserPreferences;

const sshDialogVisible = shallowRef(false);
const databaseDialogVisible = shallowRef(false);
const appearanceSaving = shallowRef(false);

const sshClientOptions = computed(() => buildSettingsSSHClientOptions(form.ssh_client));
const settingsClientPlatformOptions = SETTINGS_CLIENT_PLATFORM_OPTIONS;

function pickFormPatch<const K extends readonly PreferenceField[]>(keys: K): Pick<UserPreferences, K[number]> {
  return Object.fromEntries(keys.map(key => [key, form[key]])) as Pick<UserPreferences, K[number]>;
}

function applySavedFields<const K extends readonly PreferenceField[]>(keys: K, saved: UserPreferences) {
  for (const key of keys) {
    form[key] = saved[key] as never;
  }
}
```

把原来的 `saveAll()`、`confirmSSHRegistration()`、`confirmDBRegistration()` 替换成按页签函数：

```ts
async function saveAppearanceSettings() {
  appearanceSaving.value = true;
  try {
    const patch = pickFormPatch(APPEARANCE_FIELDS);
    const saved = await preferences.update(patch);
    applySavedFields(APPEARANCE_FIELDS, saved);
    preferences.persistPartialToBrowser(patch);
    ElMessage.success('配置已保存');
  } catch {
    ElMessage.error(preferences.error || '保存失败');
  } finally {
    appearanceSaving.value = false;
  }
}

function openSSHActivationDialog() {
  if (!isSupportedSSHClientForActivation(form.ssh_client)) {
    ElMessage.warning('请选择受支持的 SSH 客户端');
    return;
  }
  if (form.ssh_client_platform !== 'windows') {
    ElMessage.warning('请先将操作系统改为 Windows');
    return;
  }
  if (sshClientPathError.value || !sshRegistrationCommand.value) {
    ElMessage.warning(sshClientPathError.value || '请先完善 SSH 客户端配置');
    return;
  }
  sshDialogVisible.value = true;
}

async function confirmSSHActivation() {
  if (sshClientPathError.value || !sshRegistrationCommand.value) {
    ElMessage.warning(sshClientPathError.value || '请先完善 SSH 客户端配置');
    return;
  }
  sshRegistrationSaving.value = true;
  try {
    const patch = pickFormPatch(SSH_FIELDS);
    const saved = await preferences.update(patch);
    applySavedFields(SSH_FIELDS, saved);
    preferences.persistPartialToBrowser(patch);
    preferences.markSSHProtocolRegistered(true);
    sshRegistered.value = true;
    sshDialogVisible.value = false;
    ElMessage.success('SSH 客户端配置已保存');
  } catch {
    ElMessage.error(preferences.error || '保存失败');
  } finally {
    sshRegistrationSaving.value = false;
  }
}

function openDatabaseActivationDialog() {
  const error = dbClientPathError.value || dbCAFilePathError.value;
  if (form.db_client_platform !== 'windows') {
    ElMessage.warning('请先将操作系统改为 Windows');
    return;
  }
  if (error || !dbRegistrationCommand.value) {
    ElMessage.warning(error || '请先完善数据库客户端配置');
    return;
  }
  databaseDialogVisible.value = true;
}

async function confirmDatabaseActivation() {
  const error = dbClientPathError.value || dbCAFilePathError.value;
  if (error || !dbRegistrationCommand.value) {
    ElMessage.warning(error || '请先完善数据库客户端配置');
    return;
  }
  dbRegistrationSaving.value = true;
  try {
    const patch = pickFormPatch(DATABASE_FIELDS);
    const saved = await preferences.update(patch);
    applySavedFields(DATABASE_FIELDS, saved);
    preferences.persistPartialToBrowser(patch);
    databaseClient.markRegistered();
    dbRegistered.value = true;
    databaseDialogVisible.value = false;
    ElMessage.success('数据库客户端配置已保存');
  } catch {
    ElMessage.error(preferences.error || '保存失败');
  } finally {
    dbRegistrationSaving.value = false;
  }
}
```

补上数据库路径选择按钮：

```ts
function pickDatabaseExecutable() {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.exe';
  input.onchange = event => {
    const file = (event.target as HTMLInputElement).files?.[0];
    if (!file) return;
    form.db_client_path = (file as File & { path?: string }).path || file.name;
  };
  input.click();
}
```

模板中做这些改动：

```vue
<template #header>
  <div class="settings-toolbar">
    <div class="settings-toolbar__copy">
      <strong>个人设置</strong>
      <span>管理界面偏好与本机连接工具。</span>
    </div>
    <div class="settings-toolbar__actions">
      <span v-if="preferences.error" class="save-error">保存失败</span>
    </div>
  </div>
</template>
```

```vue
<ClientSectionHeading title="界面与终端" desc="设置主题和 Web Terminal 字体。" :configured="true" :registered="true">
  <template #actions>
    <el-button data-testid="settings-save-appearance" type="primary" :loading="appearanceSaving" @click="saveAppearanceSettings">
      保存配置
    </el-button>
  </template>
</ClientSectionHeading>
```

```vue
<ClientSectionHeading
  title="本地 SSH 客户端"
  desc="设置快速连接默认使用的 SSH 工具。"
  :configured="sshConfigured"
  :registered="sshRegistered"
>
  <template #actions>
    <el-button data-testid="settings-save-ssh" type="primary" @click="openSSHActivationDialog">保存配置</el-button>
  </template>
</ClientSectionHeading>

<el-select v-model="form.ssh_client" placeholder="选择本地 SSH 客户端" style="width: 100%">
  <el-option
    v-for="option in sshClientOptions"
    :key="option.command"
    :label="option.label"
    :value="option.command"
    :disabled="option.disabled"
  />
</el-select>
<el-segmented v-model="form.ssh_client_platform" :options="settingsClientPlatformOptions" class="platform-segmented" block />
```

```vue
<ClientSectionHeading
  title="本地数据库客户端"
  desc="设置数据库快速连接使用的数据库客户端。"
  :configured="dbConfigured"
  :registered="dbRegistered"
>
  <template #actions>
    <el-button data-testid="settings-save-database" type="primary" @click="openDatabaseActivationDialog">保存配置</el-button>
  </template>
</ClientSectionHeading>

<el-segmented v-model="form.db_client_platform" :options="settingsClientPlatformOptions" class="platform-segmented" block />
<el-input v-model="form.db_client_path" name="db_client_path" autocomplete="off" :placeholder="`例如 ${dbClientPathExample}`">
  <template #append>
    <el-button @click="pickDatabaseExecutable">选择文件</el-button>
  </template>
</el-input>
<div class="field-help">无法自动读取完整路径时，请手动粘贴。</div>
<div class="field-help">当使用私有CA、自签证书且开启客户端TLS连接时下载网关CA到电脑，然后填写文件在电脑的路径</div>
```

移除整个 `ClientRegistrationAlert` 组件定义和页内 `<ClientRegistrationAlert ... />` 使用，改为页尾两个弹窗：

```vue
<ClientActivationDialog
  v-model="sshDialogVisible"
  title="激活本地 SSH 客户端"
  :command="sshRegistrationCommand"
  :loading="sshRegistrationSaving"
  @copy="copyText(sshRegistrationCommand, '协议注册命令已复制')"
  @confirm="confirmSSHActivation"
/>
<ClientActivationDialog
  v-model="databaseDialogVisible"
  title="激活本地数据库客户端"
  :command="dbRegistrationCommand"
  :loading="dbRegistrationSaving"
  @copy="copyText(dbRegistrationCommand, '协议注册命令已复制')"
  @confirm="confirmDatabaseActivation"
/>
```

最后清理样式：删掉 `.registration-alert` / `.registration-command-wrapper` / `.registration-actions`，保留 `.section-heading__actions` 的顶部按钮布局。

- [ ] **Step 4: 重新运行视图测试，确认行为符合规格**

在 `web/` 目录执行：

```bash
npm exec vitest run src/views/SettingsView.mount.test.ts
```

Expected: PASS，输出包含 `3 passed`。

- [ ] **Step 5: Commit**

```bash
git add web/src/views/SettingsView.vue web/src/views/SettingsView.mount.test.ts
git commit -m "重构: 按页签拆分个人设置保存流程"
```

### Task 4: 接入回归断言和前端测试脚本

**Files:**
- Modify: `web/src/utils/databaseGatewayCommands.test.ts`
- Modify: `web/package.json`

**Interfaces:**
- Consumes:
  - 新增测试文件：
    - `src/config/sshClients.test.ts`
    - `src/stores/preferences.test.ts`
    - `src/components/settings/ClientActivationDialog.mount.test.ts`
    - `src/views/SettingsView.mount.test.ts`
- Produces:
  - `package.json` 中更新后的 `test:connection-commands`
  - `package.json` 中更新后的 `test:connection-dialog`
  - `databaseGatewayCommands.test.ts` 中新的源码约束断言

- [ ] **Step 1: 先改回归断言，让脚本接入缺失时先失败**

在 `web/src/utils/databaseGatewayCommands.test.ts` 的 `settings exposes local-only database client registration and never stores it as an account preference` 里替换旧断言并追加脚本断言：

```ts
const settingsSource = readFileSync(new URL('../views/SettingsView.vue', import.meta.url), 'utf8');
const localStoreSource = readFileSync(new URL('../stores/databaseClient.ts', import.meta.url), 'utf8');
const packageSource = readFileSync(new URL('../../package.json', import.meta.url), 'utf8');

assert.match(settingsSource, /ClientActivationDialog/);
assert.match(settingsSource, /data-testid="settings-save-database"/);
assert.match(settingsSource, /复制命令/);
assert.match(settingsSource, /已激活/);
assert.doesNotMatch(settingsSource, /我已执行命令，保存到浏览器/);
assert.doesNotMatch(settingsSource, /复制协议注册命令/);
assert.match(settingsSource, /设置数据库快速连接使用的数据库客户端/);
assert.match(settingsSource, /无法自动读取完整路径时，请手动粘贴/);
assert.match(settingsSource, /当使用私有CA、自签证书且开启客户端TLS连接时下载网关CA到电脑，然后填写文件在电脑的路径/);
assert.match(localStoreSource, /REG_STORAGE_KEY/);
assert.match(packageSource, /src\/config\/sshClients\.test\.ts/);
assert.match(packageSource, /src\/stores\/preferences\.test\.ts/);
assert.match(packageSource, /src\/components\/settings\/ClientActivationDialog\.mount\.test\.ts/);
assert.match(packageSource, /src\/views\/SettingsView\.mount\.test\.ts/);
```

- [ ] **Step 2: 运行源码断言，确认 package 脚本尚未接入时会先失败**

在 `web/` 目录执行：

```bash
npm exec tsx --test src/utils/databaseGatewayCommands.test.ts
```

Expected: FAIL，报错包含某个新测试文件路径未出现在 `package.json` 的脚本字符串中。

- [ ] **Step 3: 接入新测试文件到 package 脚本**

把 `web/package.json` 的脚本改成下面这样：

```json
{
  "scripts": {
    "dev": "vite",
    "build": "npm run test:connection-dialog && npm run test:connection-commands && npm run test:dialog-layout && vue-tsc --noEmit && vite build",
    "preview": "vite preview",
    "typecheck": "vue-tsc --noEmit",
    "test:connection-commands": "tsx --test src/utils/databaseGatewayCommands.test.ts src/utils/databaseUpstreamTLS.test.ts src/utils/provisioningRequest.test.ts src/utils/connectionLinks.test.ts src/utils/guacamoleProtocol.test.ts src/utils/rdpReplayDisplay.test.ts src/utils/webRDPViewport.test.ts src/utils/sshHostIdentity.test.ts src/config/databaseClients.test.ts src/config/sshClients.test.ts src/stores/preferences.test.ts src/components/ConnectionConfigDialog.test.ts src/api/systemSettings.test.ts",
    "test:connection-dialog": "vitest run src/components/ConnectionConfigDialog.mount.test.ts src/components/settings/ClientActivationDialog.mount.test.ts src/components/database/DatabaseAutoProvisionDialog.mount.test.ts src/composables/useSQLConsole.test.ts src/composables/useDatabaseTLSPreflight.test.ts src/composables/useWebRDP.test.ts src/views/DatabaseView.tls.mount.test.ts src/views/SettingsView.mount.test.ts",
    "test:dialog-layout": "tsx --test src/components/FormDialog.test.ts"
  }
}
```

- [ ] **Step 4: 运行完整前端验证**

在 `web/` 目录依次执行：

```bash
npm run test:connection-commands
npm run test:connection-dialog
npm run typecheck
```

Expected:
- `test:connection-commands` PASS
- `test:connection-dialog` PASS
- `typecheck` PASS

如果三条都通过，再执行：

```bash
npm run build
```

Expected: PASS，输出包含 `vite build` 成功结束，没有新的 TypeScript 或测试失败。

- [ ] **Step 5: Commit**

```bash
git add web/src/utils/databaseGatewayCommands.test.ts web/package.json
git commit -m "测试: 接入个人设置页客户端激活回归校验"
```
