<template>
  <div class="settings-page">
    <el-card class="settings-card" shadow="never" v-loading="preferences.loading">
      <template #header>
        <div class="settings-toolbar">
          <div class="settings-toolbar__copy">
            <strong>个人设置</strong>
            <span>界面偏好和客户端配置会随账号保存；换新浏览器时可从后端加载到本地缓存。</span>
          </div>
          <div class="settings-toolbar__actions">
            <span v-if="preferences.error" class="save-error">保存失败</span>
            <el-button type="primary" :loading="preferences.saving" @click="saveAll">保存配置</el-button>
          </div>
        </div>
      </template>

      <div class="settings-tabs-shell">
        <el-tabs v-model="activeTab" class="settings-tabs">
          <!-- 界面与终端 -->
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
                    <el-input v-model="form.terminal_font_family" name="terminal_font_family" autocomplete="off" placeholder="例如 Cascadia Mono, Consolas, monospace" />
                  </el-form-item>
                  <el-form-item label="终端字号">
                    <el-input-number v-model="form.terminal_font_size" :min="10" :max="30" controls-position="right" />
                  </el-form-item>
                </div>
              </el-form>
            </section>
          </el-tab-pane>

          <!-- SSH 客户端 -->
          <el-tab-pane label="SSH 客户端" name="ssh">
            <section class="settings-section">
              <ClientSectionHeading
                title="本地 SSH 客户端"
                desc="选择快速连接时使用的本地 SSH 工具。"
                :configured="sshConfigured"
                :registered="sshRegistered"
                :load-error="preferences.error"
                @load="loadToBrowser"
              />
              <el-form label-position="top">
                <el-form-item label="默认客户端">
                  <el-select v-model="form.ssh_client" placeholder="选择本地 SSH 客户端" style="width: 100%">
                    <el-option v-for="option in SSH_CLIENT_OPTIONS" :key="option.command" :label="option.label" :value="option.command" />
                  </el-select>
                </el-form-item>
                <template v-if="form.ssh_client && form.ssh_client !== 'default'">
                  <el-form-item label="操作系统">
                    <el-segmented v-model="form.ssh_client_platform" :options="CLIENT_PLATFORM_OPTIONS" class="platform-segmented" block />
                  </el-form-item>
                  <el-form-item label="客户端路径" required :error="sshClientPathError">
                    <el-input v-model="form.ssh_client_path" name="ssh_client_path" autocomplete="off" placeholder="例如 C:\Program Files\PuTTY\putty.exe">
                      <template #append><el-button @click="pickSSHExecutable">选择文件</el-button></template>
                    </el-input>
                    <div class="field-help">程序路径必填；浏览器无法读取完整路径时，请手动粘贴。</div>
                  </el-form-item>
                  <ClientRegistrationAlert
                    v-if="sshRegistrationCommand"
                    title="请使用管理员权限在 CMD 中执行下面命令，授权打开本地 SSH 客户端"
                    :command="sshRegistrationCommand"
                    :registered="sshRegistered"
                    @copy="copyText(sshRegistrationCommand, 'SSH 协议注册命令已复制，请在管理员 CMD 中执行一次')"
                    @update:registered="sshRegistered = $event"
                  />
                </template>
              </el-form>
            </section>
          </el-tab-pane>

          <!-- 数据库客户端 -->
          <el-tab-pane label="数据库客户端" name="database">
            <section class="settings-section">
              <ClientSectionHeading
                title="本地数据库客户端"
                desc="配置数据库快速连接使用的 DBeaver 程序。"
                :configured="dbConfigured"
                :registered="dbRegistered"
                :load-error="preferences.error"
                @load="loadToBrowser"
              />
              <el-form label-position="top">
                <el-form-item label="默认客户端">
                  <el-select v-model="form.db_client" clearable placeholder="选择本地数据库客户端" style="width: 100%">
                    <el-option v-for="option in DATABASE_CLIENT_OPTIONS" :key="option.value" :label="option.label" :value="option.value" />
                  </el-select>
                </el-form-item>
                <template v-if="form.db_client">
                  <el-form-item label="操作系统">
                    <el-segmented v-model="form.db_client_platform" :options="CLIENT_PLATFORM_OPTIONS" class="platform-segmented" block />
                  </el-form-item>
                  <el-form-item label="客户端路径" required :error="dbClientPathError">
                    <el-input v-model="form.db_client_path" name="db_client_path" autocomplete="off" :placeholder="`例如 ${dbClientPathExample}`" />
                    <div class="field-help">Windows 推荐选择 dbeaverc.exe；本机路径只用于生成协议注册命令，不会上传。</div>
                  </el-form-item>
                  <el-form-item label="本地 CA 文件路径" required :error="dbCAFilePathError">
                    <el-input v-model="form.db_client_ca_file_path" name="db_client_ca_path" autocomplete="off" :placeholder="`例如 ${dbCAFilePathExample}`">
                      <template #append>
                        <el-button :loading="dbCALoading" @click="downloadDatabaseGatewayCA">下载网关 CA</el-button>
                      </template>
                    </el-input>
                    <div class="field-help">浏览器会把 CA 下载到默认下载目录；请将文件保存或移动到上方填写的绝对路径。</div>
                  </el-form-item>
                  <el-alert v-if="form.db_client_platform !== 'windows'" type="warning" :closable="false" show-icon title="当前仅 Windows 支持从浏览器直接唤起 DBeaver。" />
                  <ClientRegistrationAlert
                    v-else-if="dbRegistrationCommand"
                    title="注册 Jianmen 数据库协议"
                    :command="dbRegistrationCommand"
                    :registered="dbRegistered"
                    @copy="copyText(dbRegistrationCommand, '协议注册命令已复制；执行后请勾选[我已执行]')"
                    @update:registered="dbRegistered = $event"
                  />
                </template>
              </el-form>
            </section>
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, reactive, ref, shallowRef, watch } from 'vue';
import { ElButton, ElCheckbox, ElInput, ElMessage } from 'element-plus';
import { useRoute, useRouter } from 'vue-router';

import { apiClient } from '@/api/client';
import {
  DATABASE_CLIENT_CA_FILE_NAME,
  DATABASE_CLIENT_OPTIONS,
  buildDatabaseProtocolRegistrationCommand,
  databaseClientCAFileExample,
  databaseClientExecutableExample,
  isValidDatabaseClientCAFilePath,
  isValidDatabaseClientExecutablePath,
} from '@/config/databaseClients';
import {
  buildSSHProtocolRegistrationCommand,
  isAbsoluteExecutablePath,
  SSH_CLIENT_OPTIONS,
  CLIENT_PLATFORM_OPTIONS,
  type ClientPlatform,
} from '@/config/sshClients';
import { useDatabaseClientStore } from '@/stores/databaseClient';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';
import { hasDatabaseGatewayTLSIdentity } from '@/utils/databaseGatewayCommands';

// ── 统一标题行组件（含状态标签 + 加载到浏览器按钮） ──
const ClientSectionHeading = defineComponent({
  props: {
    title: { type: String, required: true },
    desc: { type: String, default: '' },
    configured: { type: Boolean, default: false },
    registered: { type: Boolean, default: false },
  },
  emits: ['load'],
  setup(props, { emit }) {
    const statusLabel = computed(() => {
      if (!props.configured) return '未配置';
      return props.registered ? '已就绪' : '待注册协议';
    });
    const statusType = computed(() => {
      if (!props.configured) return 'info';
      return props.registered ? 'success' : 'warning';
    });
    return () => h('div', { class: 'section-heading' }, [
      h('div', [h('h2', props.title), h('p', props.desc)]),
      h('div', { class: 'section-heading__actions' }, [
        h('el-tag', { type: statusType.value, effect: 'light' }, () => statusLabel.value),
        h(ElButton, {
          type: 'primary',
          plain: true,
          disabled: Boolean(!props.configured || !props.registered),
          onClick: () => emit('load'),
        }, () => '加载到浏览器'),
      ]),
    ]);
  },
});

// ── 统一注册命令组件（textarea + 复制按钮 + 勾选） ──
const ClientRegistrationAlert = defineComponent({
  props: {
    title: { type: String, required: true },
    command: { type: String, required: true },
    registered: { type: Boolean, default: false },
  },
  emits: ['copy', 'update:registered'],
  setup(props, { emit }) {
    return () => h('el-alert', { type: 'info', closable: false, showIcon: true, class: 'registration-alert' }, {
      title: () => props.title,
      default: () => [
        h('div', { class: 'registration-command-wrapper' }, [
          h(ElInput, {
            type: 'textarea',
            modelValue: props.command,
            readonly: true,
            rows: 4,
            class: 'registration-command-input',
          }),
        ]),
        h('div', { class: 'registration-actions' }, [
          h(ElButton, {
            type: 'primary',
            plain: true,
            onClick: () => emit('copy'),
          }, () => '复制协议注册命令'),
          h(ElCheckbox, {
            modelValue: props.registered,
            'onUpdate:modelValue': (val: string | number | boolean) => emit('update:registered', Boolean(val)),
          }, () => '我已执行以上命令'),
        ]),
      ],
    });
  },
});

const route = useRoute();
const router = useRouter();
const preferences = usePreferencesStore();
const databaseClient = useDatabaseClientStore();
const form = reactive({ ...preferences.value });
const dbCALoading = shallowRef(false);

const sshRegistered = ref(preferences.hasSSHClient);
const dbRegistered = ref(databaseClient.protocolRegistered);

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

const sshClientPathError = computed(() => executablePathError(form.ssh_client, form.ssh_client_path));
const sshConfigured = computed(() => !!(form.ssh_client && form.ssh_client !== 'default' && isAbsoluteExecutablePath(form.ssh_client_path)));
const sshRegistrationCommand = computed(() => buildSSHProtocolRegistrationCommand(form.ssh_client, form.ssh_client_path, form.ssh_client_platform as ClientPlatform));

const dbClientPathExample = computed(() => databaseClientExecutableExample(form.db_client_platform as 'windows' | 'macos' | 'linux'));
const dbCAFilePathExample = computed(() => databaseClientCAFileExample(form.db_client_platform as 'windows' | 'macos' | 'linux'));
const dbRegistrationCommand = computed(() =>
  buildDatabaseProtocolRegistrationCommand({
    client: form.db_client as 'dbeaver' | '',
    platform: form.db_client_platform as 'windows' | 'macos' | 'linux',
    executablePath: form.db_client_path,
    caFilePath: form.db_client_ca_file_path,
    protocolRegistered: dbRegistered.value,
  }),
);
const dbClientPathError = computed(() => {
  if (!form.db_client) return '';
  if (!form.db_client_path.trim()) return '请输入客户端路径';
  if (!isValidDatabaseClientExecutablePath(form.db_client_path, form.db_client_platform as 'windows' | 'macos' | 'linux')) {
    return `请输入完整路径，例如 ${dbClientPathExample.value}`;
  }
  return '';
});
const dbCAFilePathError = computed(() => {
  if (!form.db_client) return '';
  if (!form.db_client_ca_file_path.trim()) return '请输入网关 CA 在本机保存的绝对路径';
  if (!isValidDatabaseClientCAFilePath(form.db_client_ca_file_path, form.db_client_platform as 'windows' | 'macos' | 'linux')) {
    return `请输入 .pem、.crt 或 .cer 文件的完整路径，例如 ${dbCAFilePathExample.value}`;
  }
  return '';
});
const dbConfigured = computed(() => form.db_client === 'dbeaver' && !dbClientPathError.value && !dbCAFilePathError.value);

// 路径变更时重置勾选
watch(() => [form.ssh_client, form.ssh_client_platform, form.ssh_client_path] as const, (value, previous) => {
  if (previous && value.some((item, index) => item !== previous[index])) sshRegistered.value = false;
});
watch(() => [form.db_client, form.db_client_platform, form.db_client_path, form.db_client_ca_file_path] as const, (value, previous) => {
  if (previous && value.some((item, index) => item !== previous[index])) dbRegistered.value = false;
});

watch(() => form.ssh_client, (client) => {
  if (client === 'default' || !client) form.ssh_client_path = '';
});
watch(() => form.db_client, (client) => {
  if (!client) { form.db_client_path = ''; form.db_client_ca_file_path = ''; }
});

watch(activeTab, (tab) => {
  if (route.name !== 'settings' || route.query.tab === tab) return;
  void router.replace({ query: { ...route.query, tab } });
});

onMounted(async () => {
  try {
    const loaded = await preferences.fetch();
    Object.assign(form, loaded);
    dbRegistered.value = databaseClient.protocolRegistered;
  } catch { /* store 已暴露错误 */ }
});

function executablePathError(client: string, path: string): string {
  if (!client || client === 'default') return '';
  if (!path.trim()) return '请输入客户端路径';
  if (!isAbsoluteExecutablePath(path)) return '请输入完整的 Windows 绝对路径';
  return '';
}

async function saveAll() {
  const error = sshClientPathError.value || dbClientPathError.value || dbCAFilePathError.value;
  if (error) { ElMessage.warning(error); return; }

  try {
    const saved = await preferences.update({ ...form });
    Object.assign(form, saved);
    if (dbRegistered.value) databaseClient.markRegistered();
    else databaseClient.markUnregistered();
    ElMessage.success('配置已保存');
  } catch {
    ElMessage.error(preferences.error || '保存失败');
  }
}

async function loadToBrowser() {
  try {
    const loaded = await preferences.loadToBrowser();
    Object.assign(form, loaded);
    sshRegistered.value = preferences.hasSSHClient;
    dbRegistered.value = databaseClient.protocolRegistered;
    ElMessage.success('配置已加载到当前浏览器缓存');
  } catch {
    ElMessage.error(preferences.error || '加载配置失败');
  }
}

async function copyText(value: string, successMsg: string) {
  if (!value) return;
  try {
    await writeClipboardText(value);
    ElMessage.success(successMsg);
  } catch {
    ElMessage.warning('复制失败，请手动复制');
  }
}

function pickSSHExecutable() {
  const input = document.createElement('input');
  input.type = 'file'; input.accept = '.exe';
  input.onchange = event => {
    const file = (event.target as HTMLInputElement).files?.[0];
    if (!file) return;
    form.ssh_client_path = (file as File & { path?: string }).path || file.name;
  };
  input.click();
}

async function downloadDatabaseGatewayCA() {
  if (dbCALoading.value) return;
  dbCALoading.value = true;
  try {
    const results = await Promise.allSettled([apiClient.getDBGateway('mysql'), apiClient.getDBGateway('postgres')]);
    const certificates = results
      .flatMap(result => result.status === 'fulfilled' && hasDatabaseGatewayTLSIdentity(result.value) ? [result.value.tls_ca_pem.trim()] : [])
      .filter((cert, i, arr) => arr.indexOf(cert) === i);
    if (!certificates.length) {
      const rejected = results.find((r): r is PromiseRejectedResult => r.status === 'rejected');
      throw rejected?.reason instanceof Error ? rejected.reason : new Error('数据库网关 TLS 身份材料尚未就绪');
    }
    const blob = new Blob([`${certificates.join('\n')}\n`], { type: 'application/x-pem-file' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url; anchor.download = DATABASE_CLIENT_CA_FILE_NAME;
    document.body.appendChild(anchor); anchor.click(); anchor.remove();
    URL.revokeObjectURL(url);
    ElMessage.success('网关 CA 已下载，请保存或移动到上方配置的本地路径');
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '网关 CA 下载失败');
  } finally { dbCALoading.value = false; }
}
</script>

<style scoped>
.settings-page { flex: 1; min-height: 0; padding-right: 4px; overflow-y: auto; }
.settings-card { min-height: 0; border: 1px solid var(--color-border); border-radius: 18px; background: var(--color-card); }
:deep(.settings-card > .el-card__header) { position: sticky; top: 0; z-index: 4; padding: 14px 20px; background: color-mix(in srgb, var(--color-card) 96%, transparent); border-bottom-color: var(--color-border); backdrop-filter: blur(12px); }
:deep(.settings-card > .el-card__body) { display: flex; flex-direction: column; min-height: 0; padding: 0; overflow: visible; }
.settings-tabs { flex: none; min-height: 0; }
:deep(.settings-tabs > .el-tabs__header) { display: flex; align-items: center; margin: 0; padding: 0 24px; background: var(--color-card); border-bottom: 1px solid var(--color-border); }
.settings-toolbar, .settings-toolbar__actions { display: flex; align-items: center; }
.settings-toolbar { justify-content: space-between; gap: 10px; }
.settings-toolbar__copy { display: grid; min-width: 0; gap: 4px; }
.settings-toolbar__copy strong { font-size: 16px; }
.settings-toolbar__copy span { color: var(--color-text-secondary); font-size: 12px; }
.settings-toolbar__actions { flex: 0 0 auto; gap: 10px; }
:deep(.settings-tabs .el-tabs__nav-wrap::after) { display: none; }
:deep(.settings-tabs .el-tabs__item) { height: 56px; padding: 0 22px; font-weight: 700; }
:deep(.settings-tabs > .el-tabs__content) { overflow: visible; }
.settings-section { max-width: 920px; padding: 28px 32px 24px; }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 26px; }
.section-heading h2 { margin: 0; font-size: 20px; line-height: 1.3; }
.section-heading p { margin: 6px 0 0; color: var(--color-text-secondary); font-size: 13px; }
.form-pair { display: grid; grid-template-columns: minmax(0, 1fr) 150px; gap: 14px; }
:deep(.theme-segmented .el-segmented__item) { min-width: 88px; padding: 0 14px; white-space: nowrap; }
:deep(.theme-segmented .el-segmented__item-label) { overflow: visible; text-overflow: clip; }
:deep(.platform-segmented .el-segmented__item) { min-width: fit-content; }
.field-help { margin-top: 7px; color: var(--color-text-secondary); font-size: 12px; line-height: 1.5; }
.section-heading__actions { display: flex; flex: 0 0 auto; align-items: center; gap: 10px; }
.save-error { color: var(--el-color-danger); font-size: 12px; white-space: nowrap; }
.registration-alert { margin-top: 6px; }
.registration-command-wrapper { margin-top: 10px; }
.registration-command-input :deep(.el-textarea__inner) { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; line-height: 1.5; max-height: 140px; }
.registration-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-top: 10px; }

@media (max-width: 760px) {
  :deep(.settings-card > .el-card__header) { position: static; padding: 12px; }
  .settings-toolbar { align-items: stretch; flex-direction: column; }
  .settings-toolbar__actions { justify-content: space-between; }
  :deep(.settings-tabs > .el-tabs__header) { padding: 0 12px; }
  :deep(.settings-tabs .el-tabs__item) { padding: 0 12px; }
  .settings-section { padding: 22px 18px; }
  .form-pair { grid-template-columns: 1fr; }
  .section-heading { align-items: flex-start; flex-direction: column; gap: 10px; }
  .section-heading__actions { width: 100%; flex-wrap: wrap; }
  :deep(.theme-segmented .el-segmented__item) { min-width: 0; padding-inline: 8px; }
}
</style>
