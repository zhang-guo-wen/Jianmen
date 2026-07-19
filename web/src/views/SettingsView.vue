<template>
  <div class="settings-page">
    <el-card class="settings-card" shadow="never" v-loading="preferences.loading">
      <template #header>
        <div class="settings-toolbar">
          <div class="settings-toolbar__copy">
            <strong>个人设置</strong>
            <span>界面偏好会随账号保存；本地客户端路径由当前浏览器单独保存。</span>
          </div>
          <div class="settings-toolbar__actions">
            <span v-if="preferences.error" class="save-error">保存失败</span>
            <el-button type="primary" :loading="preferences.saving" @click="save">保存配置</el-button>
          </div>
        </div>
      </template>

      <div class="settings-tabs-shell">
        <el-tabs v-model="activeTab" class="settings-tabs">
          <el-tab-pane label="界面与终端" name="appearance">
            <section class="settings-section">
              <div class="section-heading">
                <div>
                  <h2>界面与终端</h2>
                  <p>调整系统主题以及 Web Terminal 的字体显示。</p>
                </div>
              </div>
              <el-form label-position="top">
                <el-form-item label="主题">
                  <el-segmented v-model="form.theme" :options="themeOptions" class="theme-segmented" block />
                </el-form-item>
                <div class="form-pair">
                  <el-form-item label="终端字体">
                    <el-input
                      v-model="form.terminal_font_family"
                      name="terminal_font_family"
                      autocomplete="off"
                      placeholder="例如 Cascadia Mono, Consolas, monospace"
                    />
                  </el-form-item>
                  <el-form-item label="终端字号">
                    <el-input-number v-model="form.terminal_font_size" :min="10" :max="30" controls-position="right" />
                  </el-form-item>
                </div>
              </el-form>
            </section>
          </el-tab-pane>

          <el-tab-pane label="SSH 客户端" name="ssh">
            <section class="settings-section">
              <div class="section-heading">
                <div>
                  <h2>本地 SSH 客户端</h2>
                  <p>选择快速连接时使用的本地 SSH 工具。</p>
                </div>
                <el-tag :type="form.ssh_client ? 'success' : 'info'" effect="light">
                  {{ form.ssh_client ? '已配置' : '未配置' }}
                </el-tag>
              </div>
              <el-form label-position="top">
                <el-form-item label="默认客户端">
                  <el-select v-model="form.ssh_client" placeholder="选择本地 SSH 客户端" style="width: 100%">
                    <el-option v-for="option in SSH_CLIENT_OPTIONS" :key="option.command" :label="option.label" :value="option.command" />
                  </el-select>
                </el-form-item>
                <el-form-item v-if="form.ssh_client && form.ssh_client !== 'default'" label="客户端路径" required :error="sshClientPathError">
                  <el-input
                    v-model="form.ssh_client_path"
                    name="ssh_client_path"
                    autocomplete="off"
                    placeholder="例如 C:\Program Files\PuTTY\putty.exe"
                  >
                    <template #append><el-button @click="pickExecutable">选择文件</el-button></template>
                  </el-input>
                  <div class="field-help">程序路径必填；浏览器无法读取完整路径时，请手动粘贴。</div>
                </el-form-item>
                <el-alert v-if="sshRegistrationCommand" type="info" :closable="false" show-icon>
                  <template #title>请使用管理员权限在 CMD 中执行下面命令，授权打开本地 SSH 客户端</template>
                  <div class="command-box"><code>{{ sshRegistrationCommand }}</code></div>
                  <el-button link type="primary" @click="copyRegistrationCommand(sshRegistrationCommand)">复制管理员注册命令</el-button>
                </el-alert>
              </el-form>
            </section>
          </el-tab-pane>

          <el-tab-pane label="数据库客户端" name="database">
            <section class="settings-section">
              <div class="section-heading">
                <div>
                  <h2>本地数据库客户端</h2>
                  <p>配置数据库快速连接使用的 DBeaver 程序。该配置只保存在当前浏览器。</p>
                </div>
                <div class="section-heading__actions">
                  <el-tag :type="databaseClient.configured ? 'success' : 'info'" effect="light">
                    {{ databaseClient.configured ? '已配置' : '未配置' }}
                  </el-tag>
                  <el-button type="primary" @click="saveDatabaseClient">
                    {{ databaseReturnPath ? '保存并返回' : '保存本地配置' }}
                  </el-button>
                </div>
              </div>

              <el-alert
                class="local-only-alert"
                type="info"
                :closable="false"
                show-icon
                title="本机程序路径不会上传到 Jianmen；同一浏览器中的登录账号会共用这项本机配置。"
              />

              <el-form label-position="top">
                <el-form-item label="默认客户端">
                  <el-select v-model="databaseForm.client" clearable placeholder="选择本地数据库客户端" style="width: 100%">
                    <el-option
                      v-for="option in DATABASE_CLIENT_OPTIONS"
                      :key="option.value"
                      :label="option.label"
                      :value="option.value"
                    />
                  </el-select>
                </el-form-item>

                <template v-if="databaseForm.client">
                  <el-form-item label="本机系统">
                    <el-segmented
                      v-model="databaseForm.platform"
                      :options="DATABASE_CLIENT_PLATFORM_OPTIONS"
                      class="platform-segmented"
                      block
                    />
                  </el-form-item>
                  <el-form-item label="DBeaver 命令行程序" required :error="databaseClientPathError">
                    <el-input
                      v-model="databaseForm.executablePath"
                      name="database_client_path"
                      autocomplete="off"
                      :placeholder="`例如 ${databaseClientPathExample}`"
                    />
                    <div class="field-help">
                      Windows 推荐选择 dbeaverc.exe；快速连接会生成不含密码的基础配置命令。
                    </div>
                  </el-form-item>
                </template>
              </el-form>

              <div class="database-client-flow">
                <strong>数据库快速连接流程</strong>
                <ol>
                  <li>在“快速连接 → 数据库”中打开“连接配置”。</li>
                  <li>下载网关 CA，并复制 DBeaver 基础配置命令。</li>
                  <li>执行命令创建未保存、不会自动连接的连接草稿。</li>
                  <li>在 DBeaver 的 SSL 页选择 CA 并启用严格校验，再粘贴弹窗中的临时密码。</li>
                </ol>
              </div>
            </section>
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, shallowRef, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { useRoute, useRouter } from 'vue-router';

import {
  DATABASE_CLIENT_OPTIONS,
  DATABASE_CLIENT_PLATFORM_OPTIONS,
  databaseClientExecutableExample,
  isValidDatabaseClientExecutablePath,
  type DatabaseClientSettings,
} from '@/config/databaseClients';
import { buildSSHProtocolRegistrationCommand, isAbsoluteExecutablePath, SSH_CLIENT_OPTIONS } from '@/config/sshClients';
import { useDatabaseClientStore } from '@/stores/databaseClient';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';

const route = useRoute();
const router = useRouter();
const preferences = usePreferencesStore();
const databaseClient = useDatabaseClientStore();
const form = reactive({ ...preferences.value });
const databaseForm = reactive<DatabaseClientSettings>({ ...databaseClient.value });
const settingsTabs = ['appearance', 'ssh', 'database'] as const;
const requestedTab = typeof route.query.tab === 'string' && settingsTabs.includes(route.query.tab as typeof settingsTabs[number])
  ? route.query.tab
  : 'appearance';
const activeTab = shallowRef(requestedTab);
const themeOptions = [
  { label: '跟随系统', value: 'system' },
  { label: '浅色', value: 'light' },
  { label: '深色', value: 'dark' },
];

const sshClientPathError = computed(() => executablePathError(form.ssh_client, form.ssh_client_path, 'SSH', 'C:\\Program Files\\PuTTY\\putty.exe'));
const sshRegistrationCommand = computed(() => buildSSHProtocolRegistrationCommand(form.ssh_client, form.ssh_client_path));
const databaseClientPathExample = computed(() => databaseClientExecutableExample(databaseForm.platform));
const databaseReturnPath = computed(() => {
  const value = typeof route.query.return_to === 'string' ? route.query.return_to : '';
  return value.startsWith('/') && !value.startsWith('//') ? value : '';
});
const databaseClientPathError = computed(() => {
  if (!databaseForm.client) return '';
  if (!databaseForm.executablePath.trim()) return '请输入本地 DBeaver 命令行程序路径';
  if (!isValidDatabaseClientExecutablePath(databaseForm.executablePath, databaseForm.platform)) {
    return `请输入完整路径，例如 ${databaseClientPathExample.value}`;
  }
  return '';
});

watch(() => form.ssh_client, (client) => {
  if (client === 'default' || !client) form.ssh_client_path = '';
});
watch(() => databaseForm.client, (client) => {
  if (!client) databaseForm.executablePath = '';
});
watch(activeTab, (tab) => {
  if (route.name !== 'settings' || route.query.tab === tab) return;
  void router.replace({ query: { ...route.query, tab } });
});
onMounted(async () => {
  try {
    const loaded = await preferences.fetch();
    Object.assign(form, loaded);
  } catch {
    // The store exposes the request error in the page.
  }
});

function executablePathError(client: string, path: string, label: string, example: string): string {
  if (!client || client === 'default') return '';
  if (!path.trim()) return `请输入本地${label}客户端的程序路径`;
  if (!isAbsoluteExecutablePath(path)) return `请输入完整的 Windows 绝对路径，例如 ${example}`;
  return '';
}

async function save() {
  const error = sshClientPathError.value || databaseClientPathError.value;
  if (error) {
    ElMessage.warning(error);
    activeTab.value = sshClientPathError.value ? 'ssh' : 'database';
    return;
  }

  let localSaveError = '';
  try {
    databaseClient.update({ ...databaseForm });
  } catch {
    localSaveError = '本地数据库客户端配置无法写入当前浏览器';
  }

  try {
    const saved = await preferences.update({ ...form });
    Object.assign(form, saved);
    if (localSaveError) {
      ElMessage.warning(`账号配置已保存；${localSaveError}`);
    } else {
      ElMessage.success('用户配置已保存');
    }
  } catch {
    if (!localSaveError) {
      ElMessage.warning(`本地客户端配置已保存；${preferences.error || '账号配置保存失败'}`);
    } else {
      ElMessage.error(preferences.error || '保存失败');
    }
  }
}

function saveDatabaseClient() {
  if (databaseClientPathError.value) {
    ElMessage.warning(databaseClientPathError.value);
    return;
  }
  try {
    databaseClient.update({ ...databaseForm });
    ElMessage.success('本地数据库客户端配置已保存');
    if (databaseReturnPath.value) void router.push(databaseReturnPath.value);
  } catch {
    ElMessage.error('当前浏览器无法保存本地客户端配置');
  }
}

function pickExecutable() {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.exe';
  input.onchange = event => {
    const file = (event.target as HTMLInputElement).files?.[0];
    if (!file) return;
    const path = (file as File & { path?: string }).path || file.name;
    form.ssh_client_path = path;
  };
  input.click();
}

async function copyRegistrationCommand(command: string) {
  if (!command) return;
  try {
    await writeClipboardText(command);
    ElMessage.success('注册命令已复制，请在管理员 CMD 中执行一次');
  } catch {
    ElMessage.warning('复制失败，请手动复制命令');
  }
}
</script>

<style scoped>
.settings-page {
  flex: 1;
  min-height: 0;
  padding-right: 4px;
  overflow-y: auto;
}

.settings-card {
  min-height: 0;
  border: 1px solid var(--color-border);
  border-radius: 18px;
  background: var(--color-card);
}

:deep(.settings-card > .el-card__header) {
  position: sticky;
  top: 0;
  z-index: 4;
  padding: 14px 20px;
  background: color-mix(in srgb, var(--color-card) 96%, transparent);
  border-bottom-color: var(--color-border);
  backdrop-filter: blur(12px);
}

:deep(.settings-card > .el-card__body) {
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 0;
  overflow: visible;
}

.settings-tabs {
  flex: none;
  min-height: 0;
}

:deep(.settings-tabs > .el-tabs__header) {
  display: flex;
  align-items: center;
  margin: 0;
  padding: 0 24px;
  background: var(--color-card);
  border-bottom: 1px solid var(--color-border);
}

.settings-toolbar,
.settings-toolbar__actions {
  display: flex;
  align-items: center;
}

.settings-toolbar {
  justify-content: space-between;
  gap: 10px;
}

.settings-toolbar__copy {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.settings-toolbar__copy strong {
  font-size: 16px;
}

.settings-toolbar__copy span {
  color: var(--color-text-secondary);
  font-size: 12px;
}

.settings-toolbar__actions {
  flex: 0 0 auto;
  gap: 10px;
}

:deep(.settings-tabs .el-tabs__nav-wrap::after) {
  display: none;
}

:deep(.settings-tabs .el-tabs__item) {
  height: 56px;
  padding: 0 22px;
  font-weight: 700;
}

:deep(.settings-tabs > .el-tabs__content) {
  overflow: visible;
}

.settings-section {
  max-width: 920px;
  padding: 28px 32px 24px;
}

.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 26px;
}

.section-heading h2 {
  margin: 0;
  font-size: 20px;
  line-height: 1.3;
}

.section-heading p {
  margin: 6px 0 0;
  color: var(--color-text-secondary);
  font-size: 13px;
}

.form-pair {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 150px;
  gap: 14px;
}

:deep(.theme-segmented .el-segmented__item) {
  min-width: 88px;
  padding: 0 14px;
  white-space: nowrap;
}

:deep(.theme-segmented .el-segmented__item-label) {
  overflow: visible;
  text-overflow: clip;
}

.field-help {
  margin-top: 7px;
  color: var(--color-text-secondary);
  font-size: 12px;
  line-height: 1.5;
}

.section-heading__actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
}

.command-box {
  margin: 10px 0 4px;
  padding: 10px;
  border-radius: 10px;
  background: var(--color-surface-muted);
}

.command-box code {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.save-error {
  color: var(--el-color-danger);
  font-size: 12px;
  white-space: nowrap;
}

.local-only-alert {
  margin-bottom: 20px;
}

.database-client-flow {
  margin-top: 8px;
  padding: 16px;
  border: 1px solid var(--color-border);
  border-radius: 12px;
  background: var(--color-surface-muted);
}

.database-client-flow strong {
  font-size: 14px;
}

.database-client-flow ol {
  margin: 10px 0 0;
  padding-left: 20px;
  color: var(--color-text-secondary);
  font-size: 13px;
  line-height: 1.8;
}

@media (max-width: 760px) {
  :deep(.settings-card > .el-card__header) {
    position: static;
    padding: 12px;
  }

  .settings-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .settings-toolbar__actions {
    justify-content: space-between;
  }

  :deep(.settings-tabs > .el-tabs__header) {
    padding: 0 12px;
  }

  :deep(.settings-tabs .el-tabs__item) {
    padding: 0 12px;
  }

  .settings-section {
    padding: 22px 18px;
  }

  .form-pair {
    grid-template-columns: 1fr;
  }

  .section-heading {
    align-items: flex-start;
    flex-direction: column;
    gap: 10px;
  }

  .section-heading__actions {
    width: 100%;
    flex-wrap: wrap;
  }

  :deep(.theme-segmented .el-segmented__group),
  :deep(.platform-segmented .el-segmented__group) {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    width: 100%;
  }

  :deep(.theme-segmented .el-segmented__item),
  :deep(.platform-segmented .el-segmented__item) {
    min-width: 0;
    padding-inline: 8px;
  }
}
</style>
