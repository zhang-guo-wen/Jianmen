<template>
  <div class="settings-grid">
    <el-card class="settings-card" shadow="never" v-loading="preferences.loading">
      <template #header><strong>界面与终端</strong></template>
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
    </el-card>

    <el-card class="settings-card client-card" shadow="never">
      <template #header>
        <div class="card-heading">
          <strong>本地 SSH 客户端</strong>
          <el-tag :type="form.ssh_client ? 'success' : 'info'" effect="light">
            {{ form.ssh_client ? '已配置' : '未配置' }}
          </el-tag>
        </div>
      </template>
      <el-form label-position="top">
        <el-form-item label="默认客户端">
          <el-select v-model="form.ssh_client" placeholder="选择本地 SSH 客户端" style="width: 100%">
            <el-option v-for="option in SSH_CLIENT_OPTIONS" :key="option.command" :label="option.label" :value="option.command" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.ssh_client && form.ssh_client !== 'default'" label="客户端路径" required :error="clientPathError">
          <el-input v-model="form.ssh_client_path" placeholder="请输入完整绝对路径，如 C:\Program Files\PuTTY\putty.exe">
            <template #append><el-button @click="pickClientFile">选择文件</el-button></template>
          </el-input>
          <div class="field-help">程序路径必填，不提供默认值；浏览器无法读取完整路径时，请手动粘贴。</div>
        </el-form-item>
        <el-alert v-if="registrationCommand" type="info" :closable="false" show-icon>
          <template #title>请使用管理员权限执行下面命令，授权打开本地SSH客户端</template>
          <div class="command-box"><code>{{ registrationCommand }}</code></div>
          <el-button link type="primary" @click="copyRegistrationCommand">复制管理员注册命令</el-button>
        </el-alert>
      </el-form>
    </el-card>

    <div class="settings-actions">
      <span v-if="preferences.error" class="save-error">{{ preferences.error }}</span>
      <el-button type="primary" size="large" :loading="preferences.saving" @click="save">保存用户配置</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, watch } from 'vue';
import { ElMessage } from 'element-plus';

import { buildSSHProtocolRegistrationCommand, isAbsoluteExecutablePath, SSH_CLIENT_OPTIONS } from '@/config/sshClients';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';

const preferences = usePreferencesStore();
const form = reactive({ ...preferences.value });
const themeOptions = [
  { label: '跟随系统', value: 'system' },
  { label: '浅色', value: 'light' },
  { label: '深色', value: 'dark' },
];

const clientPathError = computed(() => {
  if (!form.ssh_client || form.ssh_client === 'default') return '';
  if (!form.ssh_client_path.trim()) return '请输入本地 SSH 客户端的程序路径';
  if (!isAbsoluteExecutablePath(form.ssh_client_path)) return '请输入完整的 Windows 绝对路径，例如 C:\\Program Files\\PuTTY\\putty.exe';
  return '';
});
const registrationCommand = computed(() => buildSSHProtocolRegistrationCommand(form.ssh_client, form.ssh_client_path));

watch(() => form.ssh_client, (client) => {
  if (client === 'default') form.ssh_client_path = '';
});

onMounted(async () => {
  try {
    const loaded = await preferences.fetch();
    Object.assign(form, loaded);
  } catch {
    // The store exposes the request error in the page.
  }
});

async function save() {
  if (clientPathError.value) {
    ElMessage.warning(clientPathError.value);
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

function pickClientFile() {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.exe';
  input.onchange = event => {
    const file = (event.target as HTMLInputElement).files?.[0];
    if (!file) return;
    form.ssh_client_path = (file as File & { path?: string }).path || file.name;
  };
  input.click();
}

async function copyRegistrationCommand() {
  if (!registrationCommand.value) return;
  try {
    await writeClipboardText(registrationCommand.value);
    ElMessage.success('注册命令已复制，请在管理员 CMD 中执行一次');
  } catch {
    ElMessage.warning('复制失败，请手动复制命令');
  }
}
</script>

<style scoped>
.settings-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, .9fr); gap: 16px; overflow: auto; padding-bottom: 4px; }
.settings-card { border: 1px solid var(--color-border); border-radius: 18px; background: var(--color-card); }
.form-pair { display: grid; grid-template-columns: minmax(0, 1fr) 150px; gap: 14px; }
:deep(.theme-segmented .el-segmented__item) { min-width: 88px; padding: 0 14px; white-space: nowrap; }
:deep(.theme-segmented .el-segmented__item-label) { overflow: visible; text-overflow: clip; }
.card-heading { display: flex; align-items: center; justify-content: space-between; }
.field-help { margin-top: 7px; color: var(--color-text-secondary); font-size: 12px; }
.command-box { max-height: 92px; margin: 10px 0 4px; padding: 10px; overflow: auto; border-radius: 10px; background: var(--color-surface-muted); }
.command-box code { white-space: pre-wrap; word-break: break-all; }
.settings-actions { grid-column: 1 / -1; display: flex; align-items: center; justify-content: flex-end; gap: 14px; }
.save-error { color: var(--el-color-danger); font-size: 13px; }
@media (max-width: 900px) { .settings-grid { grid-template-columns: 1fr; } .form-pair { grid-template-columns: 1fr; } }
</style>
