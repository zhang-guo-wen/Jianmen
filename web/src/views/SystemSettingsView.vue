<template>
  <div class="system-settings-page">
    <el-card class="system-settings-card" shadow="never" v-loading="loading && !state">
      <template #header>
        <div class="card-header">
          <div>
            <div class="header-title">
              <span>系统运行策略</span>
              <el-tag v-if="state" :type="state.pending_restart ? 'warning' : 'success'" effect="light">
                {{ state.pending_restart ? '待重启生效' : '运行中' }}
              </el-tag>
            </div>
            <p>管理连接、安全与审计策略。保存后重启生效。</p>
          </div>
          <div class="header-actions">
            <el-button :loading="loading" @click="loadAll">刷新</el-button>
            <el-button disabled>重启系统</el-button>
            <el-button
              type="primary"
              :loading="saving"
              :disabled="!state || !hasUnsavedChanges"
              @click="saveSettings"
            >
              保存配置
            </el-button>
          </div>
        </div>
      </template>

      <el-alert
        v-if="loadError"
        class="load-error"
        type="error"
        :closable="false"
        show-icon
        title="系统配置加载失败"
      >
        {{ loadError }}
        <el-button link type="primary" @click="loadAll">重试</el-button>
      </el-alert>

      <template v-if="state">
        <el-tabs v-model="activeTab" class="settings-tabs">
          <el-tab-pane label="代理与审计" name="policy">
            <div class="policy-grid">
              <section class="settings-section settings-section--wide">
                <div class="section-heading">
                  <div>
                    <h2>数据库网关入口</h2>
                    <p>设置数据库客户端进入 Jianmen 的端口方式。</p>
                  </div>
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>入口模式</strong>
                    <span>统一入口只开放一个端口，MySQL 建连约增加 200ms；独立端口无额外识别延迟。</span>
                  </div>
                  <el-radio-group v-model="form.database_gateway_mode" class="gateway-mode-control">
                    <el-radio-button value="unified">统一入口（默认）</el-radio-button>
                    <el-radio-button value="independent">独立端口</el-radio-button>
                  </el-radio-group>
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>客户端 TLS 策略</strong>
                    <span>强制 TLS 拒绝未加密连接；非强制同时接受两种连接。</span>
                  </div>
                  <el-radio-group
                    v-model="form.database_gateway_client_tls_mode"
                    class="gateway-mode-control"
                  >
                    <el-radio-button value="required">强制 TLS</el-radio-button>
                    <el-radio-button value="optional">非强制（默认）</el-radio-button>
                  </el-radio-group>
                </div>
              </section>

              <section class="settings-section">
                <div class="section-heading">
                  <div>
                    <h2>Web RDP</h2>
                    <p>控制浏览器远程桌面的可用性与连接行为。</p>
                  </div>
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>启用 Web RDP</strong>
                    <span>允许用户通过浏览器建立 Windows 远程桌面会话。</span>
                  </div>
                  <el-switch v-model="form.web_rdp_enabled" />
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>连接超时</strong>
                    <span>等待 guacd 与目标桌面建立连接的最长时间。</span>
                  </div>
                  <div class="number-control">
                    <el-input-number
                      v-model="form.web_rdp_connect_timeout_seconds"
                      :min="1"
                      :max="300"
                      :step="1"
                      controls-position="right"
                    />
                    <span>秒</span>
                  </div>
                </div>

                <div class="setting-row setting-row--danger">
                  <div class="setting-copy">
                    <strong>允许未录制会话</strong>
                    <span>录制初始化失败时仍允许连接。开启会降低审计完整性，需要二次确认。</span>
                  </div>
                  <el-switch
                    v-model="form.web_rdp_allow_unrecorded"
                    inline-prompt
                    active-text="允许"
                    inactive-text="拒绝"
                    style="--el-switch-on-color: var(--el-color-danger)"
                  />
                </div>
              </section>

              <section class="settings-section">
                <div class="section-heading">
                  <div>
                    <h2>会话录制与留存</h2>
                    <p>设置录制内容、保留时间与清理方式。</p>
                  </div>
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>启用会话录制</strong>
                    <span>为支持的代理会话创建可审计的录制产物。</span>
                  </div>
                  <el-switch v-model="form.recording_enabled" />
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>记录原始输入</strong>
                    <span>可能记录口令等敏感输入，开启时需要二次确认。</span>
                  </div>
                  <el-switch v-model="form.recording_record_input" />
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>记录命令</strong>
                    <span>提取并保存可检索的命令审计事件。</span>
                  </div>
                  <el-switch v-model="form.recording_record_commands" />
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>审计保留期</strong>
                    <span>超过该期限的录制和审计产物将进入清理流程。</span>
                  </div>
                  <div class="number-control">
                    <el-input-number
                      v-model="form.recording_retention_days"
                      :min="1"
                      :max="3650"
                      :step="1"
                      controls-position="right"
                    />
                    <span>天</span>
                  </div>
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>本地回放容量上限</strong>
                    <span>限制本地回放目录占用总量；0 表示不启用容量配额。</span>
                  </div>
                  <div class="number-control">
                    <el-input-number
                      v-model="maxReplayGiB"
                      :min="0"
                      :precision="2"
                      :step="1"
                      controls-position="right"
                    />
                    <span>GiB</span>
                  </div>
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>单批清理数量</strong>
                    <span>每轮过期清理任务最多处理的审计产物数量。</span>
                  </div>
                  <div class="number-control">
                    <el-input-number
                      v-model="form.recording_cleanup_batch_size"
                      :min="1"
                      :max="1000"
                      :step="1"
                      controls-position="right"
                    />
                    <span>条</span>
                  </div>
                </div>
              </section>

              <section class="settings-section settings-section--wide">
                <div class="section-heading">
                  <div>
                    <h2>数据库与 Redis 代理</h2>
                    <p>控制代理接受的客户端请求大小；保存后需重启 Jianmen 才会生效。</p>
                  </div>
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>最大客户端 SQL / 命令报文</strong>
                    <span>
                      同时作用于 MySQL、PostgreSQL 和 Redis 客户端发送的单个 SQL、参数或命令报文；
                      超过上限的请求将被拒绝，调整时需二次确认。
                    </span>
                  </div>
                  <div class="number-control">
                    <el-input-number
                      v-model="maxClientMessageMiB"
                      :min="MIN_CLIENT_MESSAGE_MIB"
                      :max="MAX_CLIENT_MESSAGE_MIB"
                      :precision="4"
                      :step="1"
                      controls-position="right"
                    />
                    <span>MiB</span>
                    <span class="number-control__exact">
                      {{ form.database_max_client_message_bytes }} 字节
                    </span>
                  </div>
                </div>
              </section>
            </div>
          </el-tab-pane>

          <el-tab-pane label="基础设施检查" name="infrastructure">
            <div class="infrastructure-stack">
              <section class="settings-section">
                <div class="section-heading section-heading--actions">
                  <div>
                    <h2>guacd 与运行目录</h2>
                    <p>这些值来自部署配置，只读展示，不会通过管理页面修改。</p>
                  </div>
                  <el-button
                    type="primary"
                    plain
                    :loading="diagnosticLoading === 'guacd'"
                    @click="runDiagnostic('guacd')"
                  >
                    测试 guacd
                  </el-button>
                </div>

                <el-alert
                  v-if="diagnostics.guacd"
                  class="diagnostic-result"
                  :type="diagnostics.guacd.ok ? 'success' : 'error'"
                  :closable="false"
                  show-icon
                  :title="diagnosticTitle(diagnostics.guacd)"
                >
                  {{ diagnostics.guacd.message }}
                </el-alert>

                <el-descriptions :column="2" border>
                  <el-descriptions-item label="guacd 地址" :span="2">
                    <code>{{ displayValue(infrastructure.guacd.address) }}</code>
                  </el-descriptions-item>
                  <el-descriptions-item label="RDP 临时目录">
                    <code>{{ displayValue(infrastructure.directories.spool_dir) }}</code>
                  </el-descriptions-item>
                  <el-descriptions-item label="回放目录">
                    <code>{{ displayValue(infrastructure.directories.replay_dir) }}</code>
                  </el-descriptions-item>
                  <el-descriptions-item label="guacd 录制根目录">
                    <code>{{ displayValue(infrastructure.directories.guacd_recording_root) }}</code>
                  </el-descriptions-item>
                  <el-descriptions-item label="本地映射盘目录">
                    <code>{{ displayValue(infrastructure.directories.local_drive_root) }}</code>
                  </el-descriptions-item>
                  <el-descriptions-item label="guacd 映射盘目录" :span="2">
                    <code>{{ displayValue(infrastructure.directories.guacd_drive_root) }}</code>
                  </el-descriptions-item>
                </el-descriptions>
              </section>

              <section class="settings-section">
                <div class="section-heading section-heading--actions">
                  <div>
                    <h2>审计对象存储</h2>
                    <p>只显示连接元数据和凭据是否已配置，永不回显访问密钥。</p>
                  </div>
                  <el-button
                    type="primary"
                    plain
                    :loading="diagnosticLoading === 'object-storage'"
                    @click="runDiagnostic('object-storage')"
                  >
                    测试对象存储
                  </el-button>
                </div>

                <el-alert
                  v-if="diagnostics.objectStorage"
                  class="diagnostic-result"
                  :type="diagnostics.objectStorage.ok ? 'success' : 'error'"
                  :closable="false"
                  show-icon
                  :title="diagnosticTitle(diagnostics.objectStorage)"
                >
                  {{ diagnostics.objectStorage.message }}
                </el-alert>

                <el-descriptions :column="2" border>
                  <el-descriptions-item label="提供方">
                    {{ displayValue(objectStorage.provider) }}
                  </el-descriptions-item>
                  <el-descriptions-item label="本地目录">
                    <code>{{ displayValue(objectStorage.local_dir) }}</code>
                  </el-descriptions-item>
                  <el-descriptions-item label="Endpoint" :span="2">
                    <code>{{ displayValue(objectStorage.endpoint) }}</code>
                  </el-descriptions-item>
                  <el-descriptions-item label="Bucket">
                    {{ displayValue(objectStorage.bucket) }}
                  </el-descriptions-item>
                  <el-descriptions-item label="Region">
                    {{ displayValue(objectStorage.region) }}
                  </el-descriptions-item>
                  <el-descriptions-item label="对象前缀" :span="2">
                    <code>{{ displayValue(objectStorage.prefix) }}</code>
                  </el-descriptions-item>
                  <el-descriptions-item label="TLS">
                    <el-tag :type="objectStorage.secure ? 'success' : 'info'" effect="light">
                      {{ objectStorage.secure ? '已启用' : '未启用' }}
                    </el-tag>
                  </el-descriptions-item>
                  <el-descriptions-item label="Path-style">
                    {{ booleanText(objectStorage.path_style) }}
                  </el-descriptions-item>
                  <el-descriptions-item label="自动创建 Bucket">
                    {{ booleanText(objectStorage.auto_create_bucket) }}
                  </el-descriptions-item>
                  <el-descriptions-item label="凭据状态">
                    <el-tag :type="objectStorage.credentials_configured ? 'success' : 'info'" effect="light">
                      {{ objectStorage.credentials_configured ? '已配置' : '未配置或无需配置' }}
                    </el-tag>
                  </el-descriptions-item>
                  <el-descriptions-item label="Access Key ID">
                    {{ configuredText(objectStorage.access_key_id_configured) }}
                  </el-descriptions-item>
                  <el-descriptions-item label="Secret Access Key">
                    {{ configuredText(objectStorage.secret_access_key_configured) }}
                  </el-descriptions-item>
                  <el-descriptions-item label="Session Token" :span="2">
                    {{ configuredText(objectStorage.session_token_configured) }}
                  </el-descriptions-item>
                </el-descriptions>
              </section>
            </div>
          </el-tab-pane>

          <el-tab-pane label="最近变更" name="revisions">
            <section class="settings-section revisions-section">
              <div class="section-heading section-heading--actions">
                <div>
                  <h2>配置版本</h2>
                  <p>显示最近 20 次系统配置变更；具体保存操作同时进入操作审计。</p>
                </div>
                <el-button :loading="historyLoading" @click="loadRevisions">刷新记录</el-button>
              </div>

              <el-alert
                v-if="historyError"
                class="history-error"
                type="error"
                :closable="false"
                show-icon
                :title="historyError"
              />

              <el-table v-loading="historyLoading" :data="revisions" empty-text="暂无配置变更记录">
                <el-table-column label="版本" width="100">
                  <template #default="{ row }">
                    <el-tag effect="plain">#{{ row.revision }}</el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="操作者" width="160">
                  <template #default="{ row }">
                    {{ row.updated_by_username || row.actor_username || 'system' }}
                  </template>
                </el-table-column>
                <el-table-column label="变更项" min-width="320">
                  <template #default="{ row }">
                    {{ changedFieldsText(row.changed_fields) }}
                  </template>
                </el-table-column>
                <el-table-column label="时间" width="190">
                  <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
                </el-table-column>
              </el-table>
            </section>
          </el-tab-pane>
        </el-tabs>
      </template>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';

import { ApiError, apiClient } from '@/api/client';
import {
  BYTES_PER_MIB,
  DATABASE_MAX_CLIENT_MESSAGE_BYTES_DEFAULT,
  DATABASE_MAX_CLIENT_MESSAGE_BYTES_MAX,
  DATABASE_MAX_CLIENT_MESSAGE_BYTES_MIN,
  changedSystemSettingsFields,
  clientMessageBytesToMiB,
  clientMessageMiBToBytes,
  replayBytesToGiB,
  replayGiBToBytes,
  weakerProtectionReasons,
  type SystemSettingsDiagnosticResult,
  type SystemSettingsInfrastructure,
  type SystemSettingsObjectStorageInfrastructure,
  type SystemSettingsRevision,
  type SystemSettingsState,
  type SystemSettingsValues,
} from '@/api/systemSettings';

type DiagnosticKind = 'guacd' | 'object-storage';

const FIELD_LABELS: Record<keyof SystemSettingsValues, string> = {
  database_gateway_mode: '数据库网关入口模式',
  database_gateway_client_tls_mode: '数据库网关客户端 TLS 策略',
  web_rdp_enabled: 'Web RDP',
  web_rdp_connect_timeout_seconds: '连接超时',
  web_rdp_allow_unrecorded: '未录制会话策略',
  database_max_client_message_bytes: '最大客户端 SQL / 命令报文',
  recording_enabled: '会话录制',
  recording_record_input: '原始输入记录',
  recording_record_commands: '命令记录',
  recording_retention_days: '审计保留期',
  recording_max_replay_bytes: '本地回放容量上限',
  recording_cleanup_batch_size: '清理批量',
};

const activeTab = ref('policy');
const loading = ref(false);
const saving = ref(false);
const loadError = ref('');
const historyLoading = ref(false);
const historyError = ref('');
const diagnosticLoading = ref<DiagnosticKind | ''>('');
const state = ref<SystemSettingsState | null>(null);
const revisions = ref<SystemSettingsRevision[]>([]);
const diagnostics = reactive<{
  guacd?: SystemSettingsDiagnosticResult;
  objectStorage?: SystemSettingsDiagnosticResult;
}>({});
const form = reactive<SystemSettingsValues>(emptySettings());
const MIN_CLIENT_MESSAGE_MIB = DATABASE_MAX_CLIENT_MESSAGE_BYTES_MIN / BYTES_PER_MIB;
const MAX_CLIENT_MESSAGE_MIB = DATABASE_MAX_CLIENT_MESSAGE_BYTES_MAX / BYTES_PER_MIB;

const infrastructure = computed<SystemSettingsInfrastructure>(
  () => state.value?.infrastructure ?? emptyInfrastructure(),
);
const objectStorage = computed<SystemSettingsObjectStorageInfrastructure>(
  () => infrastructure.value.object_storage,
);
const maxReplayGiB = computed<number>({
  get: () => replayBytesToGiB(form.recording_max_replay_bytes),
  set: value => {
    form.recording_max_replay_bytes = replayGiBToBytes(Number(value));
  },
});
const maxClientMessageMiB = computed<number>({
  get: () => clientMessageBytesToMiB(form.database_max_client_message_bytes),
  set: value => {
    form.database_max_client_message_bytes = clientMessageMiBToBytes(Number(value));
  },
});
const hasUnsavedChanges = computed(() => {
  const desired = state.value?.desired;
  return desired
    ? changedSystemSettingsFields(desired, form).length > 0
    : false;
});

onMounted(() => {
  void loadAll();
});

function emptySettings(): SystemSettingsValues {
  return {
    database_gateway_mode: 'unified',
    database_gateway_client_tls_mode: 'optional',
    web_rdp_enabled: false,
    web_rdp_connect_timeout_seconds: 15,
    web_rdp_allow_unrecorded: false,
    database_max_client_message_bytes: DATABASE_MAX_CLIENT_MESSAGE_BYTES_DEFAULT,
    recording_enabled: true,
    recording_record_input: false,
    recording_record_commands: true,
    recording_retention_days: 30,
    recording_max_replay_bytes: 0,
    recording_cleanup_batch_size: 100,
  };
}

function emptyInfrastructure(): SystemSettingsInfrastructure {
  return {
    guacd: { address: '' },
    directories: {
      spool_dir: '',
      guacd_recording_root: '',
      local_drive_root: '',
      guacd_drive_root: '',
      replay_dir: '',
    },
    object_storage: {
      provider: '',
      local_dir: '',
      endpoint: '',
      bucket: '',
      region: '',
      prefix: '',
      secure: false,
      path_style: false,
      auto_create_bucket: false,
      access_key_id_configured: false,
      secret_access_key_configured: false,
      session_token_configured: false,
      credentials_configured: false,
    },
  };
}

async function loadAll() {
  loading.value = true;
  loadError.value = '';
  const [stateResult, historyResult] = await Promise.allSettled([
    apiClient.getSystemSettings(),
    apiClient.getSystemSettingsRevisions(20),
  ]);

  if (stateResult.status === 'fulfilled') {
    applyState(stateResult.value);
  } else {
    loadError.value = errorMessage(stateResult.reason, '无法加载系统配置');
  }
  if (historyResult.status === 'fulfilled') {
    revisions.value = historyResult.value.items ?? [];
    historyError.value = '';
  } else {
    historyError.value = errorMessage(historyResult.reason, '配置变更记录加载失败');
  }
  loading.value = false;
}

async function loadRevisions() {
  historyLoading.value = true;
  historyError.value = '';
  try {
    const history = await apiClient.getSystemSettingsRevisions(20);
    revisions.value = history.items ?? [];
  } catch (error) {
    historyError.value = errorMessage(error, '配置变更记录加载失败');
  } finally {
    historyLoading.value = false;
  }
}

function applyState(nextState: SystemSettingsState) {
  const desired = { ...emptySettings(), ...nextState.desired };
  const effective = { ...emptySettings(), ...nextState.effective };
  state.value = {
    ...nextState,
    desired,
    effective,
    infrastructure: nextState.infrastructure ?? state.value?.infrastructure ?? emptyInfrastructure(),
  };
  Object.assign(form, desired);
}

function validateSettings(): SystemSettingsValues | null {
  const next = { ...form };
  if (next.database_gateway_mode !== 'unified' && next.database_gateway_mode !== 'independent') {
    ElMessage.warning('数据库网关入口模式必须是统一入口或独立端口');
    return null;
  }
  if (
    next.database_gateway_client_tls_mode !== 'required'
    && next.database_gateway_client_tls_mode !== 'optional'
  ) {
    ElMessage.warning('数据库网关客户端 TLS 策略必须是强制或非强制');
    return null;
  }
  if (!isIntegerWithin(next.web_rdp_connect_timeout_seconds, 1, 300)) {
    ElMessage.warning('Web RDP 连接超时必须是 1-300 秒的整数');
    return null;
  }
  if (!isIntegerWithin(next.recording_retention_days, 1, 3650)) {
    ElMessage.warning('审计保留期必须是 1-3650 天的整数');
    return null;
  }
  if (
    !Number.isSafeInteger(next.database_max_client_message_bytes)
    || next.database_max_client_message_bytes < DATABASE_MAX_CLIENT_MESSAGE_BYTES_MIN
    || next.database_max_client_message_bytes > DATABASE_MAX_CLIENT_MESSAGE_BYTES_MAX
  ) {
    ElMessage.warning('最大客户端 SQL / 命令报文必须在 0.0625-16 MiB 之间');
    return null;
  }
  if (
    !Number.isSafeInteger(next.recording_max_replay_bytes)
    || next.recording_max_replay_bytes < 0
  ) {
    ElMessage.warning('本地回放容量上限必须是有效的非负数');
    return null;
  }
  if (!isIntegerWithin(next.recording_cleanup_batch_size, 1, 1000)) {
    ElMessage.warning('单批清理数量必须是 1-1000 的整数');
    return null;
  }
  return next;
}

async function saveSettings() {
  const current = state.value;
  const next = validateSettings();
  if (!current || !next) return;

  const riskReasons = weakerProtectionReasons(current.desired, next);
  let confirmRisk = false;
  if (riskReasons.length) {
    try {
      await ElMessageBox.confirm(
        `以下变更会影响审计完整性、安全边界或连接行为：${riskReasons.join('；')}。保存后仍需重启才会生效，确定继续吗？`,
        '确认高风险配置变更',
        {
          type: 'warning',
          confirmButtonText: '确认保存',
          cancelButtonText: '取消',
        },
      );
      confirmRisk = true;
    } catch {
      return;
    }
  }

  saving.value = true;
  try {
    const nextState = await apiClient.updateSystemSettings({
      settings: next,
      expected_revision: current.revision,
      confirm_risk: confirmRisk,
    });
    applyState(nextState);
    await loadRevisions();
    ElMessage.success(
      nextState.pending_restart
        ? '系统配置已保存，重启 Jianmen 后生效'
        : '系统配置已保存',
    );
  } catch (error) {
    if (error instanceof ApiError && error.statusCode === 409) {
      ElMessage.warning('配置已被其他管理员更新，正在重新加载最新版本');
      await loadAll();
    } else {
      ElMessage.error(errorMessage(error, '系统配置保存失败'));
    }
  } finally {
    saving.value = false;
  }
}

async function runDiagnostic(kind: DiagnosticKind) {
  diagnosticLoading.value = kind;
  try {
    const result = kind === 'guacd'
      ? await apiClient.testSystemSettingsGuacd()
      : await apiClient.testSystemSettingsObjectStorage();
    if (kind === 'guacd') diagnostics.guacd = result;
    else diagnostics.objectStorage = result;
    if (result.ok) ElMessage.success('连接测试通过');
    else ElMessage.warning(result.message || '连接测试未通过');
  } catch (error) {
    const result: SystemSettingsDiagnosticResult = {
      ok: false,
      message: errorMessage(error, '连接测试失败'),
      latency_ms: diagnosticErrorLatency(error),
    };
    if (kind === 'guacd') diagnostics.guacd = result;
    else diagnostics.objectStorage = result;
    ElMessage.error(result.message);
  } finally {
    diagnosticLoading.value = '';
  }
}

function diagnosticTitle(result: SystemSettingsDiagnosticResult): string {
  const latency = Number.isFinite(result.latency_ms) && result.latency_ms >= 0
    ? ` · ${result.latency_ms} ms`
    : '';
  return `${result.ok ? '检查通过' : '检查失败'}${latency}`;
}

function diagnosticErrorLatency(error: unknown): number {
  if (!(error instanceof ApiError) || !error.details || typeof error.details !== 'object') return 0;
  const latency = (error.details as Record<string, unknown>).latency_ms;
  return typeof latency === 'number' && Number.isFinite(latency) && latency >= 0 ? latency : 0;
}

function isIntegerWithin(value: number, min: number, max: number): boolean {
  return Number.isInteger(value) && value >= min && value <= max;
}

function displayValue(value?: string): string {
  return value?.trim() || '-';
}

function booleanText(value: boolean): string {
  return value ? '是' : '否';
}

function configuredText(value: boolean): string {
  return value ? '已配置' : '未配置';
}

function formatTime(value?: string): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date);
}

function changedFieldsText(fields?: string[]): string {
  if (!fields?.length) return '初始化配置';
  return fields.map(field => FIELD_LABELS[field as keyof SystemSettingsValues] || field).join('、');
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}
</script>

<style scoped>
.system-settings-page {
  flex: 1;
  min-height: 0;
  min-width: 0;
  padding-right: 4px;
  overflow-x: hidden;
  overflow-y: auto;
}

.system-settings-card {
  width: 100%;
  min-height: 100%;
  border: 1px solid var(--color-border);
  border-radius: 18px;
  background: var(--color-card);
}

:deep(.system-settings-card > .el-card__header) {
  position: sticky;
  top: 0;
  z-index: 5;
  padding: 13px 20px;
  background: color-mix(in srgb, var(--color-card) 94%, transparent);
  border-bottom-color: var(--color-border);
  backdrop-filter: blur(12px);
}

:deep(.system-settings-card > .el-card__body) {
  padding: 0;
}

.card-header,
.header-title,
.header-actions,
.section-heading--actions {
  display: flex;
  align-items: center;
}

.card-header {
  justify-content: space-between;
  gap: 20px;
}

.card-header > :first-child {
  min-width: 0;
}

.header-title {
  flex-wrap: wrap;
  gap: 10px;
  font-size: 18px;
  font-weight: 750;
}

.card-header p {
  margin: 5px 0 0;
  color: var(--color-text-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.header-actions {
  flex-shrink: 0;
  gap: 10px;
}

.header-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

.load-error {
  margin: 18px 22px 0;
}

.settings-tabs {
  margin-top: 10px;
}

:deep(.settings-tabs > .el-tabs__header) {
  margin: 0;
  padding: 0 20px;
  border-bottom: 1px solid var(--color-border);
}

:deep(.settings-tabs .el-tabs__nav-wrap::after) {
  display: none;
}

:deep(.settings-tabs .el-tabs__item) {
  height: 46px;
  font-weight: 700;
}

.policy-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
  gap: 14px;
  padding: 16px 20px 20px;
}

.settings-section--wide {
  grid-column: 1 / -1;
}

.gateway-mode-control {
  flex-shrink: 0;
  max-width: 100%;
}

.gateway-mode-control :deep(.el-radio-button) {
  flex: 1 1 auto;
  min-width: 0;
}

.gateway-mode-control :deep(.el-radio-button__inner) {
  width: 100%;
  white-space: nowrap;
}

.infrastructure-stack {
  display: grid;
  gap: 14px;
  padding: 16px 20px 20px;
}

.settings-section {
  max-width: 100%;
  min-width: 0;
  padding: 18px;
  border: 1px solid var(--color-border);
  border-radius: 12px;
  background: color-mix(in srgb, var(--color-card) 96%, var(--color-surface-muted));
}

.settings-section--wide {
  grid-column: 1 / -1;
}

.section-heading {
  margin-bottom: 12px;
}

.section-heading--actions {
  justify-content: space-between;
  gap: 18px;
}

.section-heading h2 {
  margin: 0;
  font-size: 16px;
}

.section-heading p {
  margin: 5px 0 0;
  color: var(--color-text-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.setting-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) max-content;
  align-items: center;
  gap: 22px;
  min-height: 62px;
  padding: 11px 0;
  border-top: 1px solid var(--color-border);
}

.setting-row--danger {
  margin-top: 6px;
  padding: 14px;
  border: 1px solid color-mix(in srgb, var(--el-color-danger) 35%, var(--color-border));
  border-radius: 10px;
  background: color-mix(in srgb, var(--el-color-danger) 5%, transparent);
}

.setting-copy {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.setting-copy strong {
  font-size: 14px;
}

.setting-copy span {
  color: var(--color-text-secondary);
  font-size: 12px;
  line-height: 1.55;
}

.number-control {
  display: grid;
  grid-template-columns: minmax(120px, 150px) max-content;
  justify-content: end;
  align-items: center;
  gap: 8px;
  max-width: 100%;
  min-width: 0;
}

.number-control .el-input-number {
  width: 150px;
  max-width: 100%;
}

.number-control > span {
  min-width: 24px;
  color: var(--color-text-secondary);
  font-size: 12px;
}

.number-control > .number-control__exact {
  grid-column: 1 / -1;
  justify-self: end;
  min-width: 0;
  max-width: 100%;
  text-align: right;
  overflow-wrap: anywhere;
  font-family: "Cascadia Mono", Consolas, monospace;
}

.diagnostic-result,
.history-error {
  margin-bottom: 16px;
}

:deep(.el-descriptions__label) {
  width: 160px;
}

code {
  color: var(--color-text);
  font-family: "Cascadia Mono", Consolas, monospace;
  overflow-wrap: anywhere;
}

.revisions-section {
  margin: 16px 20px 20px;
}

@media (max-width: 1240px) {
  .policy-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 1100px) {
  :deep(.system-settings-card > .el-card__header) {
    position: static;
  }

  .card-header,
  .section-heading--actions {
    align-items: flex-start;
    flex-direction: column;
  }

  .header-actions {
    width: 100%;
  }

  .header-actions .el-button {
    flex: 1;
  }

  .policy-grid,
  .infrastructure-stack {
    padding: 14px;
  }

  .settings-section {
    padding: 16px;
  }

  .setting-row {
    grid-template-columns: minmax(0, 1fr);
    align-items: flex-start;
    gap: 12px;
  }

  .number-control {
    grid-template-columns: minmax(0, 180px) max-content;
    justify-content: start;
    width: 100%;
  }

  .number-control .el-input-number {
    width: 100%;
  }

  .number-control > .number-control__exact {
    justify-self: start;
    text-align: left;
  }

  .gateway-mode-control {
    display: flex;
    width: 100%;
  }

  .revisions-section {
    margin: 14px;
  }

  :deep(.el-descriptions__body) {
    max-width: 100%;
    overflow-x: auto;
  }

  :deep(.el-descriptions__body .el-descriptions__table) {
    min-width: 640px;
  }
}

@media (max-width: 520px) {
  .header-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .header-actions .el-button {
    width: 100%;
  }

  .gateway-mode-control :deep(.el-radio-button__inner) {
    padding-right: 10px;
    padding-left: 10px;
  }

  :deep(.el-descriptions__body .el-descriptions__table) {
    min-width: 560px;
  }
}
</style>
