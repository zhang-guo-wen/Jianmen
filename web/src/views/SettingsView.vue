<template>
  <div class="settings-grid">
    <section class="settings-hero">
      <div>
        <span class="eyebrow">PERSONAL WORKSPACE</span>
        <h2>把连接习惯留在 Jianmen</h2>
        <p>主题、本地 SSH 客户端与终端显示配置会跟随当前用户保存。</p>
      </div>
      <div class="hero-orbit" aria-hidden="true"><span>SSH</span></div>
    </section>

    <el-card class="settings-card" shadow="never" v-loading="preferences.loading">
      <template #header><strong>界面与终端</strong></template>
      <el-form label-position="top">
        <el-form-item label="主题">
          <el-segmented v-model="form.theme" :options="themeOptions" block />
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
        <el-form-item v-if="form.ssh_client && form.ssh_client !== 'default'" label="客户端路径">
          <el-input v-model="form.ssh_client_path" :placeholder="clientPathPlaceholder">
            <template #append><el-button @click="pickClientFile">选择文件</el-button></template>
          </el-input>
          <div class="field-help">浏览器无法读取完整本地路径时，请手动粘贴可执行文件路径。</div>
        </el-form-item>
        <el-alert v-if="registrationCommand" type="info" :closable="false" show-icon>
          <template #title>首次使用需要注册系统 ssh:// 协议</template>
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

import { buildSSHProtocolRegistrationCommand, SSH_CLIENT_OPTIONS, sshClientOption } from '@/config/sshClients';
import { usePreferencesStore } from '@/stores/preferences';

const preferences = usePreferencesStore();
const form = reactive({ ...preferences.value });
const themeOptions = [
  { label: '跟随系统', value: 'system' },
  { label: '浅色', value: 'light' },
  { label: '深色', value: 'dark' },
];

const clientPathPlaceholder = computed(() => sshClientOption(form.ssh_client)?.defaultPath || '请输入可执行文件路径');
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
    await navigator.clipboard.writeText(registrationCommand.value);
    ElMessage.success('注册命令已复制，请在管理员 CMD 中执行一次');
  } catch {
    ElMessage.warning('复制失败，请手动复制命令');
  }
}
</script>

<style scoped>
.settings-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, .9fr); gap: 16px; overflow: auto; padding-bottom: 4px; }
.settings-hero { grid-column: 1 / -1; display: flex; align-items: center; justify-content: space-between; min-height: 170px; padding: 28px 34px; color: #f8fafc; overflow: hidden; position: relative; border-radius: 22px; background: radial-gradient(circle at 78% 28%, rgb(56 189 248 / 35%), transparent 24%), linear-gradient(125deg, #0f172a, #164e63 58%, #0e7490); box-shadow: var(--shadow-elevated); }
.settings-hero h2 { margin: 8px 0 6px; font-size: clamp(26px, 4vw, 42px); letter-spacing: -.045em; }
.settings-hero p { margin: 0; color: #bae6fd; }
.eyebrow { color: #67e8f9; font-size: 11px; font-weight: 800; letter-spacing: .18em; }
.hero-orbit { display: grid; place-items: center; width: 108px; height: 108px; margin-right: 28px; border: 1px solid rgb(255 255 255 / 26%); border-radius: 50%; box-shadow: inset 0 0 0 18px rgb(255 255 255 / 4%), 0 0 60px rgb(34 211 238 / 22%); }
.hero-orbit span { font-size: 22px; font-weight: 900; letter-spacing: .08em; }
.settings-card { border: 1px solid var(--color-border); border-radius: 18px; background: var(--color-card); }
.form-pair { display: grid; grid-template-columns: minmax(0, 1fr) 150px; gap: 14px; }
.card-heading { display: flex; align-items: center; justify-content: space-between; }
.field-help { margin-top: 7px; color: var(--color-text-secondary); font-size: 12px; }
.command-box { max-height: 92px; margin: 10px 0 4px; padding: 10px; overflow: auto; border-radius: 10px; background: var(--color-surface-muted); }
.command-box code { white-space: pre-wrap; word-break: break-all; }
.settings-actions { grid-column: 1 / -1; display: flex; align-items: center; justify-content: flex-end; gap: 14px; }
.save-error { color: var(--el-color-danger); font-size: 13px; }
@media (max-width: 900px) { .settings-grid { grid-template-columns: 1fr; } .hero-orbit { display: none; } .form-pair { grid-template-columns: 1fr; } }
</style>
