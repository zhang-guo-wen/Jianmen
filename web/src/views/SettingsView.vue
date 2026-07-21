<template>
  <div class="settings-page">
    <el-card class="settings-card" shadow="never" v-loading="preferences.loading">
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

      <div class="settings-tabs-shell">
        <el-tabs v-model="activeTab" class="settings-tabs">
          <el-tab-pane label="界面与终端" name="appearance">
            <section class="settings-section">
              <ClientSectionHeading title="界面与终端" desc="设置主题和 Web Terminal 字体。" :configured="true" :registered="true">
                <template #actions>
                  <el-button data-testid="settings-save-appearance" type="primary" :loading="appearanceSaving" @click="saveAppearanceSettings">
                    保存配置
                  </el-button>
                </template>
              </ClientSectionHeading>
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

          <el-tab-pane label="SSH 客户端" name="ssh">
            <section class="settings-section">
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
              <el-form label-position="top">
                <el-form-item label="默认客户端">
                  <el-select v-model="form.ssh_client" placeholder="选择本地 SSH 客户端" style="width: 100%">
                    <el-option
                      v-for="option in sshClientOptions"
                      :key="option.command"
                      :label="option.label"
                      :value="option.command"
                      :disabled="option.disabled"
                    />
                  </el-select>
                </el-form-item>
                <template v-if="form.ssh_client">
                  <el-form-item label="操作系统">
                    <el-segmented v-model="form.ssh_client_platform" :options="settingsClientPlatformOptions" class="platform-segmented" block />
                  </el-form-item>
                  <el-form-item label="客户端路径" required :error="sshClientPathError">
                    <el-input v-model="form.ssh_client_path" name="ssh_client_path" autocomplete="off" placeholder="例如 C:\Program Files\Xshell\Xshell.exe">
                      <template #append>
                        <el-button @click="pickSSHExecutable">选择文件</el-button>
                      </template>
                    </el-input>
                    <div class="field-help">无法自动读取完整路径时，请手动粘贴。</div>
                  </el-form-item>
                </template>
              </el-form>
            </section>
          </el-tab-pane>

          <el-tab-pane label="数据库客户端" name="database">
            <section class="settings-section">
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
              <el-form label-position="top">
                <el-form-item label="默认客户端">
                  <el-select v-model="form.db_client" placeholder="选择本地数据库客户端" style="width: 100%">
                    <el-option v-for="option in DATABASE_CLIENT_OPTIONS" :key="option.value" :label="option.label" :value="option.value" />
                  </el-select>
                </el-form-item>
                <template v-if="form.db_client">
                  <el-form-item label="操作系统">
                    <el-segmented v-model="form.db_client_platform" :options="settingsClientPlatformOptions" class="platform-segmented" block />
                  </el-form-item>
                  <el-form-item label="客户端路径" required :error="dbClientPathError">
                    <el-input v-model="form.db_client_path" name="db_client_path" autocomplete="off" :placeholder="`例如 ${dbClientPathExample}`">
                      <template #append>
                        <el-button @click="pickDatabaseExecutable">选择文件</el-button>
                      </template>
                    </el-input>
                    <div class="field-help">无法自动读取完整路径时，请手动粘贴。</div>
                  </el-form-item>
                  <el-form-item label="本地 CA 文件路径（私有/自签证书）" :error="dbCAFilePathError">
                    <el-input v-model="form.db_client_ca_file_path" name="db_client_ca_path" autocomplete="off" :placeholder="`例如 ${dbCAFilePathExample}`">
                      <template #append>
                        <el-button :loading="dbCALoading" @click="downloadDatabaseGatewayCA">下载网关 CA</el-button>
                      </template>
                    </el-input>
                    <div class="field-help">当使用私有CA、自签证书且开启客户端TLS连接时下载网关CA到电脑，然后填写文件在电脑的路径</div>
                  </el-form-item>
                </template>
              </el-form>
            </section>
          </el-tab-pane>
        </el-tabs>
      </div>

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
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, shallowRef, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { useRoute, useRouter } from 'vue-router';

import { apiClient, type UserPreferences } from '@/api/client';
import ClientActivationDialog from '@/components/settings/ClientActivationDialog.vue';
import ClientSectionHeading from '@/components/settings/ClientSectionHeading.vue';
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
  buildSettingsSSHClientOptions,
  buildSSHProtocolRegistrationCommand,
  isAbsoluteExecutablePath,
  isSupportedSSHClientForActivation,
  SETTINGS_CLIENT_PLATFORM_OPTIONS,
  type ClientPlatform,
} from '@/config/sshClients';
import { useDatabaseClientStore } from '@/stores/databaseClient';
import { usePreferencesStore } from '@/stores/preferences';
import { writeClipboardText } from '@/utils/clipboard';
import { hasDatabaseGatewayTLSIdentity } from '@/utils/databaseGatewayCommands';

const APPEARANCE_FIELDS = ['theme', 'terminal_font_family', 'terminal_font_size'] as const;
const SSH_FIELDS = ['ssh_client', 'ssh_client_platform', 'ssh_client_path'] as const;
const DATABASE_FIELDS = ['db_client', 'db_client_platform', 'db_client_path', 'db_client_ca_file_path'] as const;

type PreferenceField = keyof UserPreferences;

const route = useRoute();
const router = useRouter();
const preferences = usePreferencesStore();
const databaseClient = useDatabaseClientStore();
const form = reactive<UserPreferences>({ ...normalizePreferences(preferences.value) });
const dbCALoading = shallowRef(false);
const appearanceSaving = shallowRef(false);
const sshRegistrationSaving = shallowRef(false);
const dbRegistrationSaving = shallowRef(false);
const sshDialogVisible = shallowRef(false);
const databaseDialogVisible = shallowRef(false);

const sshRegistered = ref(preferences.sshProtocolRegistered);
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
const sshClientOptions = computed(() => buildSettingsSSHClientOptions(form.ssh_client));
const settingsClientPlatformOptions = SETTINGS_CLIENT_PLATFORM_OPTIONS;

const sshClientPathError = computed(() => {
  if (!form.ssh_client || !isSupportedSSHClientForActivation(form.ssh_client)) return '';
  if (!form.ssh_client_path.trim()) return '请输入客户端路径';
  if (!isAbsoluteExecutablePath(form.ssh_client_path)) return '请输入完整的 Windows 绝对路径';
  return '';
});
const sshConfigured = computed(() => isSupportedSSHClientForActivation(form.ssh_client) && isAbsoluteExecutablePath(form.ssh_client_path));
const sshRegistrationCommand = computed(() => {
  if (!isSupportedSSHClientForActivation(form.ssh_client)) return '';
  return buildSSHProtocolRegistrationCommand(form.ssh_client, form.ssh_client_path, form.ssh_client_platform as ClientPlatform);
});

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
  if (!form.db_client_ca_file_path.trim()) return '';
  if (!isValidDatabaseClientCAFilePath(form.db_client_ca_file_path, form.db_client_platform as 'windows' | 'macos' | 'linux')) {
    return `请输入 .pem、.crt 或 .cer 文件的完整路径，例如 ${dbCAFilePathExample.value}`;
  }
  return '';
});
const dbConfigured = computed(() => form.db_client === 'dbeaver' && form.db_client_platform === 'windows' && !dbClientPathError.value && !dbCAFilePathError.value);

watch(() => [form.ssh_client, form.ssh_client_platform, form.ssh_client_path] as const, (value, previous) => {
  if (previous && value.some((item, index) => item !== previous[index])) {
    sshRegistered.value = false;
    preferences.markSSHProtocolRegistered(false);
  }
});
watch(() => [form.db_client, form.db_client_platform, form.db_client_path, form.db_client_ca_file_path] as const, (value, previous) => {
  if (previous && value.some((item, index) => item !== previous[index])) {
    dbRegistered.value = false;
    databaseClient.markUnregistered();
  }
});
watch(() => form.db_client, (client) => {
  if (!client) {
    form.db_client_path = '';
    form.db_client_ca_file_path = '';
  }
});
watch(activeTab, (tab) => {
  if (route.name !== 'settings' || route.query.tab === tab) return;
  void router.replace({ query: { ...route.query, tab } });
});

onMounted(async () => {
  try {
    const loaded = await preferences.fetch();
    Object.assign(form, normalizePreferences(loaded));
    sshRegistered.value = preferences.sshProtocolRegistered;
    dbRegistered.value = databaseClient.protocolRegistered;
  } catch {
    /* store 已暴露错误 */
  }
});

function normalizePreferences(value: UserPreferences): UserPreferences {
  return {
    ...value,
    ssh_client: value.ssh_client?.trim() ? value.ssh_client : 'xshell',
  };
}

function pickFormPatch<K extends PreferenceField>(keys: readonly K[]): Pick<UserPreferences, K> {
  const patch = {} as Pick<UserPreferences, K>;
  for (const key of keys) {
    patch[key] = form[key] as Pick<UserPreferences, K>[K];
  }
  return patch;
}

function applySavedFields<K extends PreferenceField>(keys: readonly K[], saved: UserPreferences) {
  for (const key of keys) {
    form[key] = saved[key] as UserPreferences[K];
  }
}

async function saveAppearanceSettings() {
  appearanceSaving.value = true;
  try {
    const patch = pickFormPatch(APPEARANCE_FIELDS);
    const saved = normalizePreferences(await preferences.update(patch));
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
    const saved = normalizePreferences(await preferences.update(patch));
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
    const saved = normalizePreferences(await preferences.update(patch));
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
  input.type = 'file';
  input.accept = '.exe';
  input.onchange = event => {
    const file = (event.target as HTMLInputElement).files?.[0];
    if (!file) return;
    form.ssh_client_path = (file as File & { path?: string }).path || file.name;
  };
  input.click();
}

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

async function downloadDatabaseGatewayCA() {
  if (dbCALoading.value) return;
  dbCALoading.value = true;
  try {
    const results = await Promise.allSettled([
      apiClient.getDBGateway('mysql'),
      apiClient.getDBGateway('postgres'),
      apiClient.getDBGateway('redis'),
    ]);
    const certificates = results
      .flatMap(result => {
        if (result.status !== 'fulfilled' || !hasDatabaseGatewayTLSIdentity(result.value)) return [];
        const certificate = result.value.tls_ca_pem?.trim();
        return certificate ? [certificate] : [];
      })
      .filter((cert, index, arr) => arr.indexOf(cert) === index);
    if (!certificates.length) {
      const rejected = results.find((result): result is PromiseRejectedResult => result.status === 'rejected');
      throw rejected?.reason instanceof Error ? rejected.reason : new Error('数据库网关 TLS 身份材料尚未就绪');
    }
    const blob = new Blob([`${certificates.join('\n')}\n`], { type: 'application/x-pem-file' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = DATABASE_CLIENT_CA_FILE_NAME;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
    ElMessage.success('网关 CA 已下载，请保存或移动到上方配置的本地路径');
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '网关 CA 下载失败');
  } finally {
    dbCALoading.value = false;
  }
}
</script>

<style scoped>
.settings-page { flex: 1; min-height: 0; padding-right: 4px; overflow-y: auto; }
.settings-card { min-height: 0; border: 1px solid var(--color-border); border-radius: 18px; background: var(--color-card); }
:deep(.settings-card > .el-card__header) { position: sticky; top: 0; z-index: 4; padding: 14px 20px; background: color-mix(in srgb, var(--color-card) 96%, transparent); border-bottom-color: var(--color-border); backdrop-filter: blur(12px); }
:deep(.settings-card > .el-card__body) { display: flex; flex-direction: column; min-height: 0; padding: 0; overflow: visible; }
.settings-tabs { min-height: 420px; }
:deep(.settings-tabs > .el-tabs__header) { width: auto; margin: 0; padding: 0 20px; background: color-mix(in srgb, var(--color-surface-muted) 48%, var(--color-card)); border-bottom: 1px solid var(--color-border); }
.settings-toolbar, .settings-toolbar__actions { display: flex; align-items: center; }
.settings-toolbar { justify-content: space-between; gap: 10px; }
.settings-toolbar__copy { display: grid; min-width: 0; gap: 4px; }
.settings-toolbar__copy strong { font-size: 16px; }
.settings-toolbar__copy span { color: var(--color-text-secondary); font-size: 12px; }
.settings-toolbar__actions { flex: 0 0 auto; gap: 10px; }
:deep(.settings-tabs .el-tabs__nav-wrap::after) { background-color: transparent; }
:deep(.settings-tabs .el-tabs__item) { height: 46px; padding: 0 16px; color: var(--color-text-secondary); font-weight: 650; transition: color .18s ease, background-color .18s ease; }
:deep(.settings-tabs .el-tabs__item:hover) { color: var(--el-color-primary); background: color-mix(in srgb, var(--el-color-primary) 7%, transparent); }
:deep(.settings-tabs .el-tabs__item.is-active) { color: var(--el-color-primary); }
:deep(.settings-tabs > .el-tabs__content) { min-width: 0; overflow: visible; }
.settings-section { width: min(100%, 1040px); padding: 24px 28px; }
.form-pair { display: grid; grid-template-columns: minmax(0, 1fr) 150px; gap: 14px; }
:deep(.theme-segmented .el-segmented__item) { min-width: 88px; padding: 0 14px; white-space: nowrap; }
:deep(.theme-segmented .el-segmented__item-label) { overflow: visible; text-overflow: clip; }
:deep(.platform-segmented .el-segmented__item) { min-width: fit-content; }
.field-help { margin-top: 7px; color: var(--color-text-secondary); font-size: 12px; line-height: 1.5; }
.save-error { color: var(--el-color-danger); font-size: 12px; white-space: nowrap; }

@media (max-width: 760px) {
  :deep(.settings-card > .el-card__header) { position: static; padding: 12px; }
  .settings-toolbar { align-items: stretch; flex-direction: column; }
  .settings-toolbar__actions { justify-content: space-between; }
  .settings-tabs { min-height: 0; }
  :deep(.settings-tabs > .el-tabs__header) { width: 100%; padding: 0 10px; }
  :deep(.settings-tabs .el-tabs__item) { padding: 0 10px; }
  .settings-section { padding: 22px 18px; }
  .form-pair { grid-template-columns: 1fr; }
  :deep(.theme-segmented .el-segmented__item) { min-width: 0; padding-inline: 8px; }
}
</style>
