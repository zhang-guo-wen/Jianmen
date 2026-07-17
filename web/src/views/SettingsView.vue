<template>
  <div class="settings-page">
    <el-card class="settings-card" shadow="never" v-loading="preferences.loading">
      <el-tabs v-model="activeTab" class="settings-tabs">
        <template #extra>
          <span v-if="preferences.error" class="save-error">保存失败</span>
          <el-button type="primary" :loading="preferences.saving" @click="save">保存配置</el-button>
        </template>
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
                  <el-input v-model="form.terminal_font_family" placeholder="Cascadia Mono, Consolas, monospace" />
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
                <el-input v-model="form.ssh_client_path" placeholder="请输入完整绝对路径，如 C:\Program Files\PuTTY\putty.exe">
                  <template #append><el-button @click="pickExecutable('ssh')">选择文件</el-button></template>
                </el-input>
                <div class="field-help">程序路径必填，不提供默认值；浏览器无法读取完整路径时，请手动粘贴。</div>
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
                <p>配置数据库快速连接使用的本地客户端。</p>
              </div>
              <el-tag :type="form.database_client ? 'success' : 'info'" effect="light">
                {{ form.database_client ? '已配置' : '未配置' }}
              </el-tag>
            </div>
            <el-form label-position="top">
              <el-form-item label="默认客户端">
                <el-select v-model="form.database_client" placeholder="选择本地数据库客户端" clearable style="width: 100%">
                  <el-option v-for="option in DATABASE_CLIENT_OPTIONS" :key="option.command" :label="option.label" :value="option.command" />
                </el-select>
              </el-form-item>
              <el-form-item v-if="form.database_client" label="客户端路径" required :error="databaseClientPathError">
                <el-input v-model="form.database_client_path" placeholder="请输入完整绝对路径，如 C:\Program Files\DBeaver\dbeaver.exe">
                  <template #append><el-button @click="pickExecutable('database')">选择文件</el-button></template>
                </el-input>
                <div class="field-help">注册 jianmen-db:// 协议后，网页可携带临时堡垒机凭据启动 DBeaver。</div>
              </el-form-item>
              <el-alert v-if="databaseRegistrationCommand" type="info" :closable="false" show-icon>
                <template #title>请使用管理员权限在 CMD 中执行下面命令，授权打开 DBeaver</template>
                <div class="command-box"><code>{{ databaseRegistrationCommand }}</code></div>
                <el-button link type="primary" @click="copyRegistrationCommand(databaseRegistrationCommand)">复制管理员注册命令</el-button>
              </el-alert>
            </el-form>
          </section>
        </el-tab-pane>
      </el-tabs>

    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { ElMessage } from 'element-plus';

import { buildDatabaseProtocolRegistrationCommand, DATABASE_CLIENT_OPTIONS } from '@/config/databaseClients';
import { buildSSHProtocolRegistrationCommand, isAbsoluteExecutablePath, SSH_CLIENT_OPTIONS } from '@/config/sshClients';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';

const preferences = usePreferencesStore();
const form = reactive({ ...preferences.value });
const activeTab = ref('appearance');
const themeOptions = [
  { label: '跟随系统', value: 'system' },
  { label: '浅色', value: 'light' },
  { label: '深色', value: 'dark' },
];

const sshClientPathError = computed(() => executablePathError(form.ssh_client, form.ssh_client_path, 'SSH', 'C:\\Program Files\\PuTTY\\putty.exe'));
const databaseClientPathError = computed(() => executablePathError(form.database_client, form.database_client_path, '数据库', 'C:\\Program Files\\DBeaver\\dbeaver.exe'));
const sshRegistrationCommand = computed(() => buildSSHProtocolRegistrationCommand(form.ssh_client, form.ssh_client_path));
const databaseRegistrationCommand = computed(() => buildDatabaseProtocolRegistrationCommand(form.database_client, form.database_client_path));

watch(() => form.ssh_client, (client) => {
  if (client === 'default' || !client) form.ssh_client_path = '';
});
watch(() => form.database_client, (client) => {
  if (!client) form.database_client_path = '';
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
    return;
  }
  try {
    const saved = await preferences.update({ ...form });
    Object.assign(form, saved);
    ElMessage.success('用户配置已保存');
  } catch {
    ElMessage.error(preferences.error || '保存失败');
  }
}

function pickExecutable(type: 'ssh' | 'database') {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.exe';
  input.onchange = event => {
    const file = (event.target as HTMLInputElement).files?.[0];
    if (!file) return;
    const path = (file as File & { path?: string }).path || file.name;
    if (type === 'ssh') form.ssh_client_path = path;
    else form.database_client_path = path;
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
  position: sticky;
  top: 0;
  z-index: 3;
  display: flex;
  align-items: center;
  gap: 16px;
  margin: 0;
  padding: 0 20px 0 24px;
  background: var(--color-card);
  border-bottom: 1px solid var(--color-border);
}

:deep(.settings-tabs > .el-tabs__header .el-tabs__nav-wrap) {
  flex: 1;
  min-width: 0;
}

:deep(.settings-tabs > .el-tabs__header .el-tabs__extra-content) {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 10px;
  padding-bottom: 1px;
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

@media (max-width: 700px) {
  :deep(.settings-tabs > .el-tabs__header) {
    gap: 8px;
    padding: 0 12px;
  }

  :deep(.settings-tabs > .el-tabs__header .el-tabs__extra-content) {
    gap: 6px;
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
}
</style>
