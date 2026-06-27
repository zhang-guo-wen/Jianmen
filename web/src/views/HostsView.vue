<template>
  <div class="view-stack host-view">
    <div class="toolbar">
      <el-input
        v-model="keyword"
        clearable
        placeholder="搜索主机名称、IP、主机分组"
        style="max-width: 320px"
        @keyup.enter="searchHosts"
      />
      <div class="toolbar-actions">
        <el-button :loading="hostsLoading" @click="loadHosts">{{ t('common.refresh') }}</el-button>
        <el-button type="primary" @click="openCreateHostDialog">新增主机</el-button>
      </div>
    </div>

    <el-card class="placeholder-panel host-table-panel" shadow="never">
      <el-alert v-if="hostError" :title="hostError" type="error" show-icon />
      <el-table v-else v-loading="hostsLoading" class="host-table" :data="hosts" :height="hostTableHeight" row-key="id">
        <el-table-column label="主机名称" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">
            <div class="host-address">{{ hostName(row) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="IP 端口" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">
            {{ hostAddress(row) }}
          </template>
        </el-table-column>
        <el-table-column label="账号" width="110">
          <template #default="{ row }">
            <el-button link type="primary" @click="openAccountsDialog(row)">
              <el-icon style="margin-right: 2px"><User /></el-icon>
              {{ numberFrom(row.account_count, 0) }} 个
            </el-button>
          </template>
        </el-table-column>
        <el-table-column align="center" label="启用状态" width="80">
          <template #default="{ row }">
            <el-switch
              :loading="statusUpdatingId === hostStatusKey(row)"
              :model-value="!row.disabled"
              size="small"
              @change="toggleHostStatus(row)"
            />
          </template>
        </el-table-column>
        <el-table-column label="主机分组" min-width="110" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.group || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="130" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.remark || '-' }}
          </template>
        </el-table-column>
        <el-table-column align="right" header-align="right" label="操作" fixed="right" width="170">
          <template #default="{ row }">
            <div class="table-actions host-row-actions">
              <el-button link type="primary" @click="openEditHostDialog(row)">编辑</el-button>
              <el-button link type="danger" @click="confirmDeleteHost(row)">删除</el-button>
              <el-button link type="success" @click="openCreateAccountDialog(row)">新建账号</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!hostsLoading && !hosts.length && !hostError" description="暂无主机数据" />
      <div class="pagination-row">
        <el-pagination
          v-model:current-page="hostPage"
          v-model:page-size="hostPageSize"
          background
          layout="total, sizes, prev, pager, next"
          :page-sizes="[20, 50, 100]"
          :total="hostTotal"
          @current-change="loadHosts"
          @size-change="handleHostPageSizeChange"
        />
      </div>
    </el-card>

    <el-dialog
      v-model="hostDialogVisible"
      :close-on-click-modal="!submittingHost"
      :title="editingHostId ? '编辑主机' : '新增主机'"
      class="form-dialog resource-edit-dialog"
      destroy-on-close
      width="min(680px, calc(100vw - 32px))"
    >
      <el-form ref="hostFormRef" :model="hostForm" :rules="hostRules" label-position="top">
        <div class="form-sections">
          <section class="form-section">
            <div class="form-section-title">连接信息</div>
            <div class="host-form-grid">
              <el-form-item label="主机地址" prop="host">
                <el-input v-model="hostForm.host" placeholder="IP、域名或 host:port" @blur="normalizeHostAddressInput" />
              </el-form-item>
              <el-form-item label="端口" prop="port">
                <el-input-number v-model="hostForm.port" :max="65535" :min="1" controls-position="right" />
              </el-form-item>
            </div>
          </section>

          <el-collapse v-model="hostMorePanels" class="more-collapse">
            <el-collapse-item title="更多设置" name="more">
              <div class="host-form-grid">
              <el-form-item label="主机名称" prop="name">
                <el-input v-model="hostForm.name" placeholder="默认等于 主机地址:端口" @input="hostNameTouched = true" />
              </el-form-item>
              <el-form-item label="主机分组" prop="group">
                <el-select
                  v-model="hostForm.group"
                  allow-create
                  clearable
                  default-first-option
                  filterable
                  placeholder="选择或输入主机分组"
                >
                  <el-option v-for="group in hostGroupOptions" :key="group" :label="group" :value="group" />
                </el-select>
              </el-form-item>
              <el-form-item class="host-form-full" label="备注" prop="remark">
                <el-input v-model="hostForm.remark" :autosize="{ minRows: 3, maxRows: 5 }" type="textarea" />
              </el-form-item>
              </div>
            </el-collapse-item>
          </el-collapse>
        </div>
      </el-form>
      <template #footer>
        <el-button :disabled="submittingHost" @click="hostDialogVisible = false">取消</el-button>
        <el-button :loading="submittingHost" type="primary" @click="submitHost">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="accountsDialogVisible"
      :title="accountsDialogTitle"
      class="form-dialog"
      destroy-on-close
      width="min(900px, calc(100vw - 32px))"
    >
      <div class="account-dialog-stack">
        <div class="toolbar">
          <el-button :loading="accountsLoading" @click="loadSelectedHostAccounts">{{ t('common.refresh') }}</el-button>
          <el-button :disabled="!selectedHost" type="primary" @click="selectedHost && openCreateAccountDialog(selectedHost)">
            新增账号
          </el-button>
        </div>
        <el-alert v-if="accountError" :title="accountError" type="error" show-icon />
        <el-table v-else v-loading="accountsLoading" :data="accounts" height="360" row-key="id">
          <el-table-column label="登录账号" min-width="130" show-overflow-tooltip>
            <template #default="{ row }">
              <div class="account-name">{{ row.username || '-' }}</div>
            </template>
          </el-table-column>
          <el-table-column label="验证方式" width="96">
            <template #default="{ row }">
              <el-space wrap :size="4">
                <el-tag v-for="method in targetAuthMethods(row)" :key="method" size="small">
                  {{ authMethodLabel(method) }}
                </el-tag>
                <el-tag v-if="!targetAuthMethods(row).length" size="small" type="info">
                  {{ t('hosts.auth.none') }}
                </el-tag>
              </el-space>
            </template>
          </el-table-column>
          <el-table-column align="center" label="启用状态" width="80">
            <template #default="{ row }">
              <el-switch
                :loading="statusUpdatingId === accountStatusKey(row)"
                :model-value="!row.disabled"
                size="small"
                @change="toggleAccountStatus(row)"
              />
            </template>
          </el-table-column>
          <el-table-column label="过期时间" min-width="140" show-overflow-tooltip>
            <template #default="{ row }">
              {{ expiresAtText(row) }}
            </template>
          </el-table-column>
          <el-table-column label="备注" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">
              {{ targetRemark(row) || '-' }}
            </template>
          </el-table-column>
          <el-table-column label="账号名称" min-width="130" show-overflow-tooltip>
            <template #default="{ row }">
              {{ accountDisplayName(row) }}
            </template>
          </el-table-column>
          <el-table-column label="账号分组" min-width="110" show-overflow-tooltip>
            <template #default="{ row }">
              {{ row.group || '-' }}
            </template>
          </el-table-column>
          <el-table-column align="right" header-align="right" label="操作" fixed="right" width="180">
            <template #default="{ row }">
              <div class="table-actions">
                <el-button link type="success" @click="openConnectionDialog(row)">连接</el-button>
                <el-button link type="primary" @click="openEditAccountDialog(row)">编辑</el-button>
                <el-button
                  :loading="deletingId === targetId(row)"
                  link
                  type="danger"
                  @click="confirmDeleteAccount(row)"
                >
                  删除
                </el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!accountsLoading && !accounts.length && !accountError" description="暂无账号数据" />
      </div>
    </el-dialog>

    <el-dialog
      v-model="accountFormVisible"
      :close-on-click-modal="!submittingAccount"
      :title="editingAccountId ? '编辑账号' : '新增账号'"
      class="form-dialog resource-edit-dialog"
      destroy-on-close
      width="min(680px, calc(100vw - 32px))"
    >
      <el-form
        ref="accountFormRef"
        v-loading="accountDetailLoading"
        :model="accountForm"
        :rules="accountRules"
        label-position="top"
        @submit.prevent="submitAccount"
      >
        <div class="form-sections account-form-grid">
          <section class="form-section">
            <div class="form-section-title">登录与认证</div>
            <div class="host-form-grid account-auth-grid">
              <el-form-item class="host-form-full" label="认证方式" prop="auth_method">
                <el-radio-group class="auth-method-group" v-model="accountForm.auth_method" @change="handleAuthMethodChange">
                  <el-radio-button label="password">{{ t('hosts.auth.password') }}</el-radio-button>
                  <el-radio-button label="private_key">{{ t('hosts.auth.privateKey') }}</el-radio-button>
                </el-radio-group>
              </el-form-item>
              <el-form-item label="登录账号" prop="username">
                <el-input v-model="accountForm.username" placeholder="SSH 登录用户名" />
              </el-form-item>
              <el-form-item
                v-if="isKeyAuthMethod(accountForm.auth_method)"
                label="解锁口令（可选）"
                prop="passphrase"
              >
                <el-input v-model="accountForm.passphrase" :placeholder="secretPlaceholder" show-password type="password" />
              </el-form-item>
              <el-form-item v-if="accountForm.auth_method === 'password'" label="登录密码" prop="password">
                <el-input
                  v-model="accountForm.password"
                  :placeholder="credentialPlaceholder"
                  show-password
                  type="password"
                />
              </el-form-item>
              <el-form-item
                v-if="accountForm.auth_method === 'private_key'"
                class="host-form-full"
                label="私钥"
                prop="private_key_pem"
              >
                <div class="private-key-field">
                  <div class="private-key-toolbar">
                    <el-button @click="triggerPrivateKeyFileSelect">选择文件</el-button>
                    <span>{{ privateKeyFileName || (accountForm.private_key_pem ? '已读取私钥内容' : '未选择文件') }}</span>
                    <input ref="privateKeyFileInputRef" class="private-key-file-input" type="file" @change="handlePrivateKeyFileChange" />
                  </div>
                  <el-input
                    v-model="accountForm.private_key_pem"
                    :autosize="{ minRows: 4, maxRows: 8 }"
                    :placeholder="privateKeyPEMPlaceholder"
                    type="textarea"
                  />
                </div>
              </el-form-item>
            </div>
          </section>

          <section class="form-section">
            <div class="form-section-title">访问控制</div>
            <div class="host-form-grid">
              <el-form-item class="host-form-full" label="有效期" prop="expires_at">
                <div class="expiry-control">
                  <div class="expiry-picker-row">
                    <el-date-picker
                      v-model="accountForm.expires_at"
                      clearable
                      format="YYYY-MM-DD HH:mm"
                      placeholder="永久有效"
                      :shortcuts="accountExpiryShortcuts"
                      type="datetime"
                      value-format="YYYY-MM-DDTHH:mm:ss.SSSZ"
                    />
                    <el-button @click="setPermanentExpiry">永久</el-button>
                  </div>
                  <span class="expiry-text">{{ accountExpiryText }}</span>
                </div>
              </el-form-item>
            </div>
          </section>

          <el-collapse v-model="accountMorePanels" class="more-collapse">
            <el-collapse-item title="更多设置" name="more">
              <div class="host-form-grid">
                <el-form-item label="账号名称" prop="name">
                  <el-input v-model="accountForm.name" placeholder="默认等于登录账号" @input="accountNameTouched = true" />
                </el-form-item>
                <el-form-item label="账号分组" prop="group">
                  <el-select
                    v-model="accountForm.group"
                    allow-create
                    clearable
                    default-first-option
                    filterable
                    placeholder="选择或输入账号分组"
                  >
                    <el-option v-for="group in accountGroupOptions" :key="group" :label="group" :value="group" />
                  </el-select>
                </el-form-item>
                <el-form-item class="host-form-full" label="备注" prop="remark">
                  <el-input v-model="accountForm.remark" :autosize="{ minRows: 3, maxRows: 5 }" type="textarea" />
                </el-form-item>
              </div>
            </el-collapse-item>
          </el-collapse>
        </div>
      </el-form>
      <template #footer>
        <el-button :disabled="submittingAccount" @click="accountFormVisible = false">取消</el-button>
        <el-button :loading="testingConnection" @click="testConnection">测试连接</el-button>
        <el-button :loading="submittingAccount" type="primary" @click="submitAccount">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="connectionDialogVisible"
      class="form-dialog"
      destroy-on-close
      title="连接主机账号"
      width="min(720px, calc(100vw - 32px))"
    >
      <div v-if="selectedConnectionTarget" class="connection-dialog">
        <el-alert show-icon type="info" :closable="false"
          title="堡垒机密码是 admin，不是目标主机密码" />

        <el-descriptions :column="1" border size="small" style="margin-top: 12px">
          <el-descriptions-item label="连接地址">
            <code>{{ bastionHost || '127.0.0.1' }}:{{ bastionPort || 47102 }}</code>
            <el-button link type="primary" size="small" style="margin-left: 8px" @click="copyText(`${bastionHost || '127.0.0.1'}:${bastionPort || 47102}`)">复制</el-button>
          </el-descriptions-item>
          <el-descriptions-item label="用户名">
            <code>{{ connectionCompactUser }}</code>
            <el-button link type="primary" size="small" style="margin-left: 8px" @click="copyText(connectionCompactUser)">复制</el-button>
          </el-descriptions-item>
          <el-descriptions-item label="密码">
            堡垒机用户密码（默认 admin）
          </el-descriptions-item>
        </el-descriptions>

        <div style="margin-top: 12px">
          <el-input :model-value="`ssh ${connectionCompactUser}@${bastionHost || '127.0.0.1'} -p ${bastionPort || 47102}`" readonly size="small">
            <template #append>
              <el-button @click="copyText(`ssh ${connectionCompactUser}@${bastionHost || '127.0.0.1'} -p ${bastionPort || 47102}`)">复制 SSH 命令</el-button>
            </template>
          </el-input>
        </div>
      </div>

      <template #footer>
        <el-button @click="connectionDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus';
import { User } from '@element-plus/icons-vue';

import {
  apiClient,
  type ApiEnvelope,
  type HostPayload,
  type HostRecord,
  type PagedHostRecord,
  type TargetPayload,
  type TargetRecord
} from '@/api/client';
import { useI18n } from '@/i18n';

type AuthMethod = 'password' | 'private_key';
type HostKeyMode = 'ignore' | 'fingerprint' | 'known_hosts';
type HostForm = {
  id: string;
  name: string;
  group: string;
  host: string;
  port: number;
  remark: string;
  disabled: boolean;
};
type AccountForm = {
  id: string;
  name: string;
  group: string;
  remark: string;
  disabled: boolean;
  expires_at: string;
  username: string;
  auth_method: AuthMethod;
  password: string;
  private_key_pem: string;
  passphrase: string;
  host_key_mode: HostKeyMode;
  insecure_ignore_host_key: boolean;
  host_key_fingerprint: string;
  known_hosts_path: string;
};


const { t } = useI18n();
const keyword = ref('');
const hostsLoading = ref(false);
const hostError = ref('');
const hosts = ref<HostRecord[]>([]);
const hostPage = ref(1);
const hostPageSize = ref(20);
const hostTotal = ref(0);
const hostTableHeight = ref(420);
const selectedHost = ref<HostRecord | null>(null);
const accounts = ref<TargetRecord[]>([]);
const accountsLoading = ref(false);
const accountError = ref('');
const hostDialogVisible = ref(false);
const accountFormVisible = ref(false);
const accountsDialogVisible = ref(false);
const connectionDialogVisible = ref(false);
const selectedConnectionTarget = ref<TargetRecord | null>(null);
const editingHostId = ref<string | null>(null);
const editingAccountId = ref<string | null>(null);
const accountDetailLoading = ref(false);
const submittingHost = ref(false);
const submittingAccount = ref(false);
const testingConnection = ref(false);
const deletingId = ref('');
const statusUpdatingId = ref('');
const hostNameTouched = ref(false);
const accountNameTouched = ref(false);
const hostMorePanels = ref<string[]>([]);
const accountMorePanels = ref<string[]>([]);

const bastionHost = ref('127.0.0.1');
const bastionPort = ref(47102);
const userSessionId = ref('00001');
const hostFormRef = ref<FormInstance>();
const accountFormRef = ref<FormInstance>();
const privateKeyFileInputRef = ref<HTMLInputElement>();
const privateKeyFileName = ref('');
const hostForm = reactive<HostForm>(emptyHostForm());
const accountForm = reactive<AccountForm>(emptyAccountForm());
const accountExpiryShortcuts = [
  { text: '8小时', value: () => expiryAfter({ hours: 8 }) },
  { text: '7天', value: () => expiryAfter({ days: 7 }) },
  { text: '1年', value: () => expiryAfter({ years: 1 }) }
];

const accountsDialogTitle = computed(() => {
  const host = selectedHost.value;
  return host ? `${hostName(host)} - 账号` : '主机账号';
});
const secretPlaceholder = computed(() => (editingAccountId.value ? t('hosts.placeholder.keepSecret') : t('hosts.placeholder.optional')));
const credentialPlaceholder = computed(() => (editingAccountId.value ? t('hosts.placeholder.keepSecret') : t('hosts.placeholder.required')));
const privateKeyPEMPlaceholder = computed(() =>
  editingAccountId.value ? t('hosts.placeholder.keepSecret') : '选择本地私钥文件自动读取，或粘贴 -----BEGIN OPENSSH PRIVATE KEY----- 开头的内容'
);
const hostGroupOptions = computed(() =>
  uniqueTextValues([...hosts.value.map((host) => stringFrom(host.group)), hostForm.group])
);
const accountGroupOptions = computed(() =>
  uniqueTextValues([...accounts.value.map((account) => stringFrom(account.group)), accountForm.group])
);
const accountExpiryText = computed(() => {
  if (!accountForm.expires_at) {
    return '永久有效';
  }
  return formatDateTime(accountForm.expires_at);
});
const connectionCompactUser = computed(() => {
  const target = selectedConnectionTarget.value;
  if (!target) return 'H000000001';
  const resId = target.resource_id || targetId(target) || resourceId(target) || '0000';
  const sessionId = userSessionId.value || '00001';
  return `H${resId}${sessionId}`;
});

const hostRules: FormRules<HostForm> = {
  host: [{ required: true, message: '请输入主机地址', trigger: 'blur' }],
  port: [
    { required: true, message: '请输入端口', trigger: 'change' },
    { type: 'number', min: 1, max: 65535, message: '端口范围 1-65535', trigger: 'change' }
  ]
};
const accountRules: FormRules<AccountForm> = {
  username: [{ required: true, message: t('hosts.required.username'), trigger: 'blur' }],
  auth_method: [{ required: true, message: t('hosts.required.authMethod'), trigger: 'change' }],
  password: [{ validator: validatePassword, trigger: 'blur' }],
  private_key_pem: [{ validator: validatePrivateKeyPEM, trigger: 'blur' }]
};

function unwrapArray<T>(payload: ApiEnvelope<T[]> | T[]): T[] {
  return Array.isArray(payload) ? payload : payload.data ?? [];
}

function unwrapObject<T>(payload: ApiEnvelope<T> | T): T {
  return (payload as ApiEnvelope<T>).data ?? (payload as T);
}

function unwrapHostsPage(payload: PagedHostRecord | ApiEnvelope<HostRecord[]> | HostRecord[]): PagedHostRecord {
  if (Array.isArray(payload)) {
    return {
      data: payload,
      page: 1,
      page_size: payload.length || hostPageSize.value,
      total: payload.length
    };
  }

  if ('data' in payload && Array.isArray(payload.data) && !('total' in payload)) {
    return {
      data: payload.data,
      page: 1,
      page_size: payload.data.length || hostPageSize.value,
      total: payload.data.length
    };
  }

  return payload as PagedHostRecord;
}

function emptyHostForm(): HostForm {
  return {
    id: '',
    name: '',
    group: '',
    host: '',
    port: 22,
    remark: '',
    disabled: false
  };
}

function emptyAccountForm(): AccountForm {
  return {
    id: '',
    name: '',
    group: '',
    remark: '',
    disabled: false,
    expires_at: '',
    username: '',
    auth_method: 'password',
    password: '',
    private_key_pem: '',
    passphrase: '',
    host_key_mode: 'ignore',
    insecure_ignore_host_key: true,
    host_key_fingerprint: '',
    known_hosts_path: ''
  };
}

function resetHostForm(values: HostForm = emptyHostForm()) {
  Object.assign(hostForm, values);
}

function resetAccountForm(values: AccountForm = emptyAccountForm()) {
  Object.assign(accountForm, values);
  privateKeyFileName.value = '';
  if (privateKeyFileInputRef.value) {
    privateKeyFileInputRef.value.value = '';
  }
}

function syncDefaultAccountName() {
  if (!accountNameTouched.value) {
    accountForm.name = accountForm.username.trim();
  }
}

function numberFrom(value: unknown, fallback: number): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value;
  }

  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
}

function stringFrom(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number' ? String(value) : '';
}

function hasValue(value: unknown): boolean {
  return String(value ?? '').trim().length > 0;
}

function updateHostTableHeight() {
  const reserved = window.innerWidth <= 780 ? 318 : 292;
  hostTableHeight.value = Math.max(300, window.innerHeight - reserved);
}

function uniqueTextValues(values: string[]): string[] {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b));
}

function hostId(host: HostRecord): string {
  return stringFrom(host.id);
}

function hostName(host: HostRecord): string {
  return stringFrom(host.name).trim() || stringFrom(host.host).trim() || '-';
}

function hostAddress(host: HostRecord): string {
  const address = stringFrom(host.host).trim();
  const port = numberFrom(host.port, 22);
  return address ? formatAddressPort(address, port) : '-';
}

function targetId(target: TargetRecord): string {
  return String(target.id ?? '');
}

function hostStatusKey(host: HostRecord): string {
  return `host:${hostId(host)}`;
}

function accountStatusKey(target: TargetRecord): string {
  return `account:${targetId(target)}`;
}

function targetRemark(target: TargetRecord): string {
  return stringFrom(target.remark).trim();
}

function accountDisplayName(target: TargetRecord): string {
  return stringFrom(target.name).trim() || stringFrom(target.username).trim() || targetId(target) || '-';
}


function expiresAtText(target: TargetRecord): string {
  const expiresAt = stringFrom(target.expires_at).trim();
  return expiresAt ? formatDateTime(expiresAt) : '永久';
}

function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function expiryAfter(offset: { hours?: number; days?: number; years?: number }): Date {
  const date = new Date();
  if (offset.hours) {
    date.setHours(date.getHours() + offset.hours);
  }
  if (offset.days) {
    date.setDate(date.getDate() + offset.days);
  }
  if (offset.years) {
    date.setFullYear(date.getFullYear() + offset.years);
  }
  return date;
}

function setPermanentExpiry() {
  accountForm.expires_at = '';
}

function targetHost(target: TargetRecord): string {
  const host = stringFrom(target.host).trim();

  if (host) {
    return host;
  }

  const address = stringFrom(target.address).trim();
  const port = numberFrom(target.port, 22);
  const portSuffix = `:${port}`;

  return address.endsWith(portSuffix) ? address.slice(0, -portSuffix.length) : address;
}

function resourceId(target: TargetRecord): string {
  return stringFrom(target.resource_id).trim() || targetId(target);
}

function isAuthMethod(value: unknown): value is AuthMethod {
  return value === 'password' || value === 'private_key';
}

function isKeyAuthMethod(method: AuthMethod): boolean {
  return method === 'private_key';
}

function targetAuthMethods(target: TargetRecord): AuthMethod[] {
  const rawMethods = Array.isArray(target.auth_methods) ? target.auth_methods : [];
  const methods = new Set<AuthMethod>();
  for (const method of rawMethods) {
    if (method === 'password') {
      methods.add('password');
    } else if (method === 'private_key' || method === 'private_key_path' || method === 'private_key_pem') {
      methods.add('private_key');
    }
  }

  const authType = target.auth_type;
  if (isAuthMethod(authType)) {
    methods.add(authType);
  } else if (authType === 'private_key_path' || authType === 'private_key_pem') {
    methods.add('private_key');
  }

  if (target.password) {
    methods.add('password');
  }
  if (target.private_key_path || target.private_key_pem) {
    methods.add('private_key');
  }

  return [...methods];
}

function inferAuthMethod(target: TargetRecord): AuthMethod {
  return targetAuthMethods(target)[0] ?? 'password';
}

function authMethodLabel(method: AuthMethod): string {
  switch (method) {
    case 'password':
      return t('hosts.auth.password');
    case 'private_key':
      return t('hosts.auth.privateKey');
  }
}

function sanitizeID(value: string): string {
  const sanitized = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return sanitized || `account-${Date.now()}`;
}

function generatedAccountID(host: HostRecord, username: string): string {
  return sanitizeID(`${hostId(host) || stringFrom(host.host)}-${username || 'account'}`);
}

function parseAddressPort(value: string): { host: string; port?: number } {
  const trimmed = value.trim();

  if (!trimmed) {
    return { host: '' };
  }

  const bracketed = trimmed.match(/^\[([^\]]+)]:(\d+)$/);
  if (bracketed) {
    return {
      host: bracketed[1].trim(),
      port: validPort(bracketed[2])
    };
  }

  const colonCount = (trimmed.match(/:/g) ?? []).length;
  if (colonCount !== 1) {
    return { host: trimmed };
  }

  const [host, portText] = trimmed.split(':');
  const port = validPort(portText);
  return host.trim() && port ? { host: host.trim(), port } : { host: trimmed };
}

function validPort(value: string): number | undefined {
  const port = Number(value);
  return Number.isInteger(port) && port >= 1 && port <= 65535 ? port : undefined;
}

function formatAddressPort(host: string, port: number): string {
  const address = host.trim();
  if (!address) {
    return '';
  }
  const displayHost = address.includes(':') && !address.startsWith('[') ? `[${address}]` : address;
  return `${displayHost}:${numberFrom(port, 22)}`;
}

function defaultHostName(): string {
  return formatAddressPort(hostForm.host, Number(hostForm.port));
}

function syncDefaultHostName() {
  if (!hostNameTouched.value) {
    hostForm.name = defaultHostName();
  }
}

function normalizeHostAddressInput() {
  const parsed = parseAddressPort(hostForm.host);
  hostForm.host = parsed.host;
  if (parsed.port) {
    hostForm.port = parsed.port;
  }
  syncDefaultHostName();
}

function setSelectedHost(host: HostRecord) {
  const previousId = selectedHost.value ? hostId(selectedHost.value) : '';
  const nextId = hostId(host);

  if (previousId !== nextId) {
    accounts.value = [];
    accountError.value = '';
  }

  selectedHost.value = host;
}

function recordToHostForm(host: HostRecord): HostForm {
  return {
    id: hostId(host),
    name: stringFrom(host.name),
    group: stringFrom(host.group),
    host: stringFrom(host.host),
    port: numberFrom(host.port, 22),
    remark: stringFrom(host.remark),
    disabled: host.disabled === true
  };
}

function recordToAccountForm(target: TargetRecord): AccountForm {
  const hostKeyMode = hostKeyModeForTarget(target);
  return {
    id: targetId(target),
    name: stringFrom(target.name),
    group: stringFrom(target.group),
    remark: stringFrom(target.remark),
    disabled: target.disabled === true,
    expires_at: stringFrom(target.expires_at),
    username: stringFrom(target.username),
    auth_method: inferAuthMethod(target),
    password: '',
    private_key_pem: '',
    passphrase: '',
    host_key_mode: hostKeyMode,
    insecure_ignore_host_key: typeof target.insecure_ignore_host_key === 'boolean' ? target.insecure_ignore_host_key : true,
    host_key_fingerprint: stringFrom(target.host_key_fingerprint),
    known_hosts_path: stringFrom(target.known_hosts_path)
  };
}

function hostKeyModeForTarget(target: TargetRecord): HostKeyMode {
  if (target.insecure_ignore_host_key === false) {
    if (hasValue(target.host_key_fingerprint)) {
      return 'fingerprint';
    }

    if (hasValue(target.known_hosts_path)) {
      return 'known_hosts';
    }
  }

  return 'ignore';
}

function buildHostPayload(): HostPayload {
  normalizeHostAddressInput();
  const defaultName = defaultHostName();

  return {
    id: hostForm.id || undefined,
    name: hostForm.name.trim() || defaultName,
    group: hostForm.group.trim() || undefined,
    host: hostForm.host.trim(),
    port: Number(hostForm.port),
    remark: hostForm.remark.trim() || undefined,
    disabled: hostForm.disabled
  };
}

function hostRecordPayload(host: HostRecord, disabled: boolean): HostPayload {
  return {
    id: hostId(host) || undefined,
    name: hostName(host),
    group: stringFrom(host.group).trim() || undefined,
    host: stringFrom(host.host).trim(),
    port: numberFrom(host.port, 22),
    remark: stringFrom(host.remark).trim() || undefined,
    disabled
  };
}

function buildAccountPayload(): TargetPayload {
  const host = selectedHost.value;
  const username = accountForm.username.trim();
  const payload: TargetPayload = {
    id: accountForm.id.trim() || (host ? generatedAccountID(host, username) : sanitizeID(username)),
    host_id: host ? hostId(host) : undefined,
    name: accountForm.name.trim() || username,
    group: accountForm.group.trim() || undefined,
    remark: accountForm.remark.trim() || undefined,
    disabled: accountForm.disabled,
    expires_at: accountForm.expires_at || undefined,
    host: stringFrom(host?.host).trim(),
    port: numberFrom(host?.port, 22),
    username,
    password: '',
    private_key_path: '',
    private_key_pem: '',
    passphrase: '',
    insecure_ignore_host_key: accountForm.insecure_ignore_host_key,
    host_key_fingerprint: accountForm.host_key_fingerprint,
    known_hosts_path: accountForm.known_hosts_path
  };

  if (accountForm.auth_method === 'password') {
    payload.password = accountForm.password;
  } else {
    if (hasValue(accountForm.private_key_pem)) {
      payload.private_key_pem = accountForm.private_key_pem;
      payload.passphrase = accountForm.passphrase;
    }
  }

  return payload;
}

function targetStatusPayload(target: TargetRecord, disabled: boolean): TargetPayload {
  return {
    id: targetId(target),
    host_id: stringFrom(target.host_id).trim() || undefined,
    name: stringFrom(target.name).trim() || stringFrom(target.username).trim() || targetId(target),
    group: stringFrom(target.group).trim() || undefined,
    remark: stringFrom(target.remark).trim() || undefined,
    disabled,
    expires_at: stringFrom(target.expires_at).trim() || undefined,
    host: targetHost(target),
    port: numberFrom(target.port, 22),
    username: stringFrom(target.username).trim(),
    password: '',
    private_key_path: '',
    private_key_pem: '',
    passphrase: '',
    insecure_ignore_host_key: typeof target.insecure_ignore_host_key === 'boolean' ? target.insecure_ignore_host_key : true,
    host_key_fingerprint: stringFrom(target.host_key_fingerprint).trim(),
    known_hosts_path: stringFrom(target.known_hosts_path).trim()
  };
}

function selectedCredentialValue(): string {
  if (accountForm.auth_method === 'password') {
    return accountForm.password;
  }

  return accountForm.private_key_pem;
}

function validatePassword(_rule: unknown, value: unknown, callback: (error?: Error) => void) {
  if (!editingAccountId.value && accountForm.auth_method === 'password' && !hasValue(value)) {
    callback(new Error(t('hosts.required.password')));
    return;
  }

  callback();
}

function validatePrivateKeyPEM(_rule: unknown, value: unknown, callback: (error?: Error) => void) {
  if (!editingAccountId.value && accountForm.auth_method === 'private_key' && !hasValue(value)) {
    callback(new Error(t('hosts.required.privateKeyPem')));
    return;
  }

  callback();
}

function handleAuthMethodChange() {
  accountFormRef.value?.clearValidate(['password', 'private_key_pem', 'passphrase']);
}

function triggerPrivateKeyFileSelect() {
  privateKeyFileInputRef.value?.click();
}

async function handlePrivateKeyFileChange(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = '';

  if (!file) {
    return;
  }

  if (file.size > 1024 * 1024) {
    ElMessage.warning('私钥文件过大，请选择 1MB 以内的文本私钥文件');
    return;
  }

  try {
    const text = await file.text();
    if (!hasValue(text)) {
      ElMessage.warning('私钥文件内容为空');
      return;
    }
    accountForm.private_key_pem = text;
    privateKeyFileName.value = file.name;
    accountFormRef.value?.clearValidate(['private_key_pem']);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '读取私钥文件失败');
  }
}

async function loadHosts() {
  hostsLoading.value = true;
  hostError.value = '';

  try {
    const page = unwrapHostsPage(
      await apiClient.getHosts({
        page: hostPage.value,
        page_size: hostPageSize.value,
        q: keyword.value.trim()
      })
    );
    hosts.value = page.data ?? [];
    hostPage.value = page.page ?? hostPage.value;
    hostPageSize.value = page.page_size ?? hostPageSize.value;
    hostTotal.value = page.total ?? hosts.value.length;
  } catch (err) {
    hostError.value = err instanceof Error ? err.message : t('hosts.error.loadList');
  } finally {
    hostsLoading.value = false;
  }
}

async function searchHosts() {
  hostPage.value = 1;
  await loadHosts();
}

async function handleHostPageSizeChange() {
  hostPage.value = 1;
  await loadHosts();
}

async function loadSelectedHostAccounts() {
  const host = selectedHost.value;
  const id = host ? hostId(host) : '';

  if (!id) {
    return;
  }

  accountsLoading.value = true;
  accountError.value = '';

  try {
    accounts.value = unwrapArray(await apiClient.getHostAccounts(id));
  } catch (err) {
    accounts.value = [];
    accountError.value = err instanceof Error ? err.message : t('hosts.error.loadList');
  } finally {
    accountsLoading.value = false;
  }
}

async function openAccountsDialog(host: HostRecord) {
  setSelectedHost(host);
  accountsDialogVisible.value = true;
  await loadSelectedHostAccounts();
}

async function openCreateHostDialog() {
  editingHostId.value = null;
  hostNameTouched.value = false;
  hostMorePanels.value = [];
  resetHostForm();
  hostDialogVisible.value = true;
  await nextTick();
  hostFormRef.value?.clearValidate();
}

async function openEditHostDialog(host: HostRecord) {
  editingHostId.value = hostId(host);
  hostNameTouched.value = true;
  hostMorePanels.value = [];
  resetHostForm(recordToHostForm(host));
  hostDialogVisible.value = true;
  await nextTick();
  hostFormRef.value?.clearValidate();
}

async function submitHost() {
  normalizeHostAddressInput();
  const valid = await hostFormRef.value?.validate().catch(() => false);

  if (!valid) {
    return;
  }

  submittingHost.value = true;
  try {
    const id = editingHostId.value;
    if (id) {
      await apiClient.updateHost(id, buildHostPayload());
      ElMessage.success('主机已更新');
    } else {
      await apiClient.createHost(buildHostPayload());
      ElMessage.success('主机已创建');
    }
    hostDialogVisible.value = false;
    await loadHosts();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.save'));
  } finally {
    submittingHost.value = false;
  }
}

async function toggleHostStatus(host: HostRecord) {
  const id = hostId(host);
  if (!id) {
    return;
  }

  const disabled = host.disabled !== true;
  statusUpdatingId.value = hostStatusKey(host);
  try {
    await apiClient.updateHost(id, hostRecordPayload(host, disabled));
    ElMessage.success(disabled ? '主机已禁用' : '主机已启用');
    await loadHosts();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.save'));
  } finally {
    statusUpdatingId.value = '';
  }
}

async function openCreateAccountDialog(host: HostRecord) {
  setSelectedHost(host);
  editingAccountId.value = null;
  accountNameTouched.value = false;
  accountMorePanels.value = [];
  resetAccountForm();
  accountFormVisible.value = true;
  await nextTick();
  accountFormRef.value?.clearValidate();
}

async function openEditAccountDialog(target: TargetRecord) {
  const id = targetId(target);

  if (!id) {
    ElMessage.error(t('hosts.error.missingId'));
    return;
  }

  editingAccountId.value = id;
  accountNameTouched.value = true;
  accountMorePanels.value = [];
  resetAccountForm(recordToAccountForm(target));
  accountFormVisible.value = true;
  accountDetailLoading.value = true;
  await nextTick();
  accountFormRef.value?.clearValidate();

  try {
    resetAccountForm(recordToAccountForm(unwrapObject(await apiClient.getTarget(id))));
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.loadDetail'));
  } finally {
    accountDetailLoading.value = false;
  }
}

async function submitAccount() {
  const valid = await accountFormRef.value?.validate().catch(() => false);

  if (!valid) {
    return;
  }

  if (!selectedHost.value) {
    ElMessage.error('请先选择主机');
    return;
  }

  if (!editingAccountId.value && !hasValue(selectedCredentialValue())) {
    ElMessage.warning(`请输入${authMethodLabel(accountForm.auth_method)}`);
    return;
  }

  submittingAccount.value = true;
  try {
    const id = editingAccountId.value;
    if (id) {
      await apiClient.updateTarget(id, buildAccountPayload());
      ElMessage.success('账号已更新');
    } else {
      await apiClient.createTarget(buildAccountPayload());
      ElMessage.success('账号已创建');
    }
    accountFormVisible.value = false;
    await Promise.all([loadHosts(), loadSelectedHostAccounts()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.save'));
  } finally {
    submittingAccount.value = false;
  }
}

async function testConnection() {
  if (!selectedHost.value) {
    ElMessage.error('请先选择主机');
    return;
  }

  const username = accountForm.username.trim();
  if (!username) {
    ElMessage.warning('请输入登录账号');
    return;
  }

  const authMethod = accountForm.auth_method;
  if (authMethod === 'password' && !accountForm.password) {
    ElMessage.warning('请输入登录密码');
    return;
  }
  if (authMethod === 'private_key' && !hasValue(accountForm.private_key_pem)) {
    ElMessage.warning('请提供私钥内容');
    return;
  }

  testingConnection.value = true;
  try {
    const result = await apiClient.testTargetConnection(buildAccountPayload());
    if (result.ok) {
      ElMessage.success(result.message || '连接成功');
    } else {
      ElMessage.error(result.message || '连接失败');
    }
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '连接测试失败');
  } finally {
    testingConnection.value = false;
  }
}

async function toggleAccountStatus(target: TargetRecord) {
  const id = targetId(target);
  if (!id) {
    ElMessage.error(t('hosts.error.missingId'));
    return;
  }

  const disabled = target.disabled !== true;
  statusUpdatingId.value = accountStatusKey(target);
  try {
    await apiClient.updateTarget(id, targetStatusPayload(target, disabled));
    ElMessage.success(disabled ? '账号已禁用' : '账号已启用');
    await Promise.all([loadHosts(), loadSelectedHostAccounts()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.save'));
  } finally {
    statusUpdatingId.value = '';
  }
}

async function confirmDeleteHost(host: HostRecord) {
  const id = hostId(host);
  if (!id) {
    return;
  }

  try {
    await ElMessageBox.confirm(`确认删除主机“${hostName(host)}”？该主机下运行时账号也会删除。`, '删除主机', {
      cancelButtonText: '取消',
      confirmButtonText: '删除',
      type: 'warning'
    });
  } catch {
    return;
  }

  deletingId.value = id;
  try {
    await apiClient.deleteHost(id);
    ElMessage.success('主机已删除');
    await loadHosts();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.delete'));
  } finally {
    deletingId.value = '';
  }
}

async function confirmDeleteAccount(target: TargetRecord) {
  const id = targetId(target);

  if (!id) {
    ElMessage.error(t('hosts.error.missingId'));
    return;
  }

  try {
    await ElMessageBox.confirm(`确认删除账号“${accountDisplayName(target)}”？`, '删除账号', {
      cancelButtonText: '取消',
      confirmButtonText: '删除',
      type: 'warning'
    });
  } catch {
    return;
  }

  deletingId.value = id;
  try {
    await apiClient.deleteTarget(id);
    ElMessage.success('账号已删除');
    await Promise.all([loadHosts(), loadSelectedHostAccounts()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.delete'));
  } finally {
    deletingId.value = '';
  }
}

function openConnectionDialog(target: TargetRecord) {
  selectedConnectionTarget.value = target;
  connectionDialogVisible.value = true;
}

async function copyText(value: string) {
  if (!hasValue(value)) {
    ElMessage.warning('没有可复制的内容');
    return;
  }

  try {
    if (!navigator.clipboard?.writeText) {
      throw new Error('clipboard unavailable');
    }

    await navigator.clipboard.writeText(value);
    ElMessage.success('已复制');
  } catch {
    ElMessage.warning('复制失败，请手动选择文本复制');
  }
}

watch(
  () => accountForm.username,
  () => {
    syncDefaultAccountName();
  }
);

watch(
  () => [hostForm.host, hostForm.port, hostDialogVisible.value, editingHostId.value] as const,
  () => {
    if (!hostDialogVisible.value || editingHostId.value) {
      return;
    }
    syncDefaultHostName();
  }
);

onMounted(() => {
  updateHostTableHeight();
  window.addEventListener('resize', updateHostTableHeight);
  void loadHosts();
});

onBeforeUnmount(() => {
  window.removeEventListener('resize', updateHostTableHeight);
});
</script>

<style scoped>
.toolbar-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.host-view {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.host-table-panel {
  min-height: 0;
}

.host-table-panel :deep(.el-card__body) {
  display: flex;
  flex-direction: column;
  gap: 0;
  min-height: 0;
}

.host-table {
  min-width: 0;
}

.pagination-row {
  display: flex;
  flex: 0 0 auto;
  justify-content: flex-end;
  padding-top: 14px;
}

.account-dialog-stack,
.connection-dialog {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.host-address,
.account-name {
  color: #111827;
  font-weight: 600;
}

.table-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  white-space: nowrap;
}

.table-actions :deep(.el-button) {
  margin-left: 0;
}

.host-row-actions {
  gap: 8px;
}

.gateway-form {
  padding: 14px 14px 0;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  background: #f9fafb;
}

.connection-config-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.connection-config-row {
  display: grid;
  grid-template-columns: minmax(150px, 190px) minmax(0, 1fr);
  gap: 12px;
  align-items: center;
}

.connection-config-label {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.connection-config-label span {
  color: #6b7280;
  font-size: 12px;
}

.connection-auth-alert {
  margin-bottom: 2px;
}

.form-sections {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.form-section {
  min-width: 0;
}

.form-section + .form-section {
  padding-top: 16px;
  border-top: 1px solid #eef2f7;
}

.form-section-title {
  margin-bottom: 12px;
  color: #374151;
  font-size: 13px;
  font-weight: 700;
  line-height: 1;
}

.more-collapse {
  border-top: 1px solid #eef2f7;
  border-bottom: 0;
}

.more-collapse :deep(.el-collapse-item__header) {
  color: #374151;
  font-size: 13px;
  font-weight: 700;
}

.more-collapse :deep(.el-collapse-item__wrap) {
  border-bottom: 0;
}

.host-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  column-gap: 16px;
}

.host-form-grid .el-input-number {
  width: 100%;
}

.host-form-full {
  grid-column: 1 / -1;
}

.switch-field :deep(.el-form-item__content) {
  min-height: 32px;
  align-items: center;
}

.account-form-grid .auth-method-group {
  display: flex;
  width: 100%;
}

.account-form-grid .auth-method-group :deep(.el-radio-button) {
  flex: 1;
}

.account-form-grid .auth-method-group :deep(.el-radio-button__inner) {
  width: 100%;
  padding-inline: 8px;
  white-space: nowrap;
}

.account-auth-grid {
  align-items: start;
}

.private-key-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}

.private-key-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.private-key-toolbar span {
  min-width: 0;
  overflow: hidden;
  color: #6b7280;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.private-key-file-input {
  display: none;
}

.expiry-control {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.expiry-picker-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  width: 100%;
  align-items: center;
}

.expiry-picker-row :deep(.el-date-editor.el-input) {
  width: 100%;
}

.expiry-text {
  color: #6b7280;
  font-size: 12px;
  line-height: 1.4;
}

:global(.form-dialog .el-dialog__body) {
  max-height: min(66vh, 620px);
  overflow-y: auto;
  padding-right: 22px;
}

:global(.resource-edit-dialog .el-dialog__body) {
  min-height: 360px;
}

@media (max-width: 720px) {
  .host-form-grid,
  .connection-config-row {
    grid-template-columns: 1fr;
  }

  .expiry-picker-row {
    grid-template-columns: 1fr;
  }
}
</style>
