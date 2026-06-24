<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-input
        v-model="keyword"
        clearable
        :placeholder="t('database.placeholder.search')"
        style="max-width: 360px"
      />
      <div class="toolbar-actions">
        <el-button :loading="loading" :icon="Refresh" @click="loadProxies">
          {{ t('common.refresh') }}
        </el-button>
        <el-button :icon="Plus" type="primary" @click="openCreateProxyDialog">新增数据库实例</el-button>
      </div>
    </div>

    <el-card class="placeholder-panel" shadow="never">
      <el-alert v-if="error" :title="error" type="error" show-icon />
      <el-table v-else v-loading="loading" :data="filteredProxies" height="520" :row-key="proxyRowKey">
        <el-table-column label="实例名称" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <strong class="primary-cell">{{ proxyName(row) }}</strong>
          </template>
        </el-table-column>
        <el-table-column label="协议" width="110">
          <template #default="{ row }">
            <el-tag size="small">{{ row.protocol || t('common.none') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="监听地址" min-width="190" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="mono-text">{{ row.listen_addr || t('common.none') }}</span>
          </template>
        </el-table-column>
        <el-table-column label="上游地址" min-width="190" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="mono-text">{{ row.upstream_addr || t('common.none') }}</span>
          </template>
        </el-table-column>
        <el-table-column label="账号数" width="120">
          <template #default="{ row }">
            <el-button link type="primary" @click="openAccountsDialog(row)">
              {{ accountCount(row) }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? t('common.enabled') : t('common.disabled') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.remark || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="操作" fixed="right" width="330">
          <template #default="{ row }">
            <el-button :icon="User" link type="success" @click="openCreateAccountDialog(row)">新增账号</el-button>
            <el-button :icon="EditIcon" link type="primary" @click="openEditProxyDialog(row)">编辑</el-button>
            <el-button
              link
              :loading="statusUpdatingKey === proxyStatusKey(row)"
              :type="row.enabled ? 'warning' : 'success'"
              @click="toggleProxyStatus(row)"
            >
              {{ row.enabled ? '禁用' : '启用' }}
            </el-button>
            <el-button
              :icon="DeleteIcon"
              :loading="deletingKey === proxyName(row)"
              link
              type="danger"
              @click="confirmDeleteProxy(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !filteredProxies.length && !error" :description="t('database.empty.resources')" />
    </el-card>

    <el-dialog
      v-model="proxyFormVisible"
      :close-on-click-modal="!submittingProxy"
      :title="editingProxyName ? '编辑数据库实例' : '新增数据库实例'"
      class="form-dialog"
      destroy-on-close
      width="min(560px, calc(100vw - 32px))"
    >
      <el-form ref="proxyFormRef" :model="proxyForm" :rules="proxyRules" label-position="top">
        <div class="form-sections">
          <section class="form-section">
            <div class="form-section-title">实例信息</div>
            <div class="form-grid">
              <el-form-item label="实例名称" prop="name">
                <el-input
                  v-model="proxyForm.name"
                  :disabled="editingProxyName !== null"
                  placeholder="例如 mysql-prod"
                />
              </el-form-item>
              <el-form-item label="协议" prop="protocol">
                <el-select v-model="proxyForm.protocol" @change="handleProxyProtocolChange">
                  <el-option label="MySQL" value="mysql" />
                  <el-option label="PostgreSQL" value="postgres" />
                  <el-option label="TCP" value="tcp" />
                </el-select>
              </el-form-item>
            </div>
          </section>

          <section class="form-section">
            <div class="form-section-title">连接信息</div>
            <div class="form-grid">
              <el-form-item label="监听地址" prop="listen_host">
                <el-input v-model="proxyForm.listen_host" placeholder="0.0.0.0 或 127.0.0.1" />
              </el-form-item>
              <el-form-item label="监听端口" prop="listen_port">
                <el-input-number v-model="proxyForm.listen_port" :max="65535" :min="1" controls-position="right" />
              </el-form-item>
              <el-form-item label="上游地址" prop="upstream_host">
                <el-input v-model="proxyForm.upstream_host" placeholder="数据库主机 IP 或域名" />
              </el-form-item>
              <el-form-item label="上游端口" prop="upstream_port">
                <el-input-number v-model="proxyForm.upstream_port" :max="65535" :min="1" controls-position="right" />
              </el-form-item>
            </div>
          </section>

          <el-collapse v-model="proxyMorePanels" class="more-collapse">
            <el-collapse-item title="更多设置" name="more">
              <div class="form-grid">
                <el-form-item class="form-full" label="备注" prop="remark">
                  <el-input v-model="proxyForm.remark" :autosize="{ minRows: 3, maxRows: 5 }" type="textarea" />
                </el-form-item>
              </div>
            </el-collapse-item>
          </el-collapse>
        </div>
      </el-form>

      <template #footer>
        <el-button :disabled="submittingProxy" @click="proxyFormVisible = false">取消</el-button>
        <el-button :loading="submittingProxy" type="primary" @click="submitProxy">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="accountsDialogVisible"
      :title="accountsDialogTitle"
      class="form-dialog"
      destroy-on-close
      width="min(980px, calc(100vw - 32px))"
    >
      <div class="dialog-stack">
        <div class="toolbar">
          <el-tag>{{ selectedProxyAccounts.length }}</el-tag>
          <div class="toolbar-actions">
            <el-button :loading="accountsLoading" :icon="Refresh" @click="loadSelectedProxyAccounts">
              {{ t('common.refresh') }}
            </el-button>
            <el-button :disabled="!selectedProxy" :icon="Plus" type="primary" @click="selectedProxy && openCreateAccountDialog(selectedProxy)">
              新增账号
            </el-button>
          </div>
        </div>
        <el-alert v-if="accountError" :title="accountError" type="error" show-icon />
        <el-table
          v-else
          v-loading="accountsLoading"
          :data="selectedProxyAccounts"
          height="360"
          :row-key="accountRowKey"
        >
          <el-table-column label="账号名称" min-width="150">
            <template #default="{ row }">
              <strong class="primary-cell">{{ accountUsername(row) || t('common.none') }}</strong>
            </template>
          </el-table-column>
          <el-table-column label="数据库" min-width="140">
            <template #default="{ row }">
              {{ databaseName(selectedProxy || {}, row) }}
            </template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag :type="row.disabled ? 'info' : 'success'">
                {{ row.disabled ? t('common.disabled') : t('common.enabled') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="资源 ID" min-width="250" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="mono-text">{{ accountResourceId(row) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="备注" min-width="180" show-overflow-tooltip>
            <template #default="{ row }">
              {{ row.remark || '-' }}
            </template>
          </el-table-column>
          <el-table-column label="操作" fixed="right" width="340">
            <template #default="{ row }">
              <el-button :icon="Connection" link type="success" @click="openConnectionDialog(row)">
                连接配置
              </el-button>
              <el-button :icon="EditIcon" link type="primary" @click="openEditAccountDialog(row)">编辑</el-button>
              <el-button
                link
                :loading="statusUpdatingKey === accountStatusKey(row)"
                :type="row.disabled ? 'success' : 'warning'"
                @click="toggleAccountStatus(row)"
              >
                {{ row.disabled ? '启用' : '禁用' }}
              </el-button>
              <el-button
                :icon="DeleteIcon"
                :loading="deletingKey === accountDeleteKey(row)"
                link
                type="danger"
                @click="confirmDeleteAccount(row)"
              >
                删除
              </el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty
          v-if="!accountsLoading && !selectedProxyAccounts.length && !accountError"
          :description="t('database.empty.accounts')"
        />
      </div>
    </el-dialog>

    <el-dialog
      v-model="accountFormVisible"
      :close-on-click-modal="!submittingAccount"
      :title="editingAccountUsername ? '编辑数据库账号' : '新增数据库账号'"
      class="form-dialog"
      destroy-on-close
      width="min(480px, calc(100vw - 32px))"
    >
      <el-form ref="accountFormRef" :model="accountForm" :rules="accountRules" label-position="top">
        <div class="form-sections">
          <section class="form-section">
            <div class="form-section-title">账号信息</div>
            <div class="form-grid">
              <el-form-item label="登录账号" prop="username">
                <el-input
                  v-model="accountForm.username"
                  :disabled="editingAccountUsername !== null"
                  placeholder="数据库登录用户名"
                />
              </el-form-item>
              <el-form-item label="默认数据库" prop="database">
                <el-input v-model="accountForm.database" placeholder="可选，例如 app 或 orders" />
              </el-form-item>
            </div>
          </section>

          <el-collapse v-model="accountMorePanels" class="more-collapse">
            <el-collapse-item title="更多设置" name="more">
              <div class="form-grid">
                <el-form-item class="form-full" label="备注" prop="remark">
                  <el-input v-model="accountForm.remark" :autosize="{ minRows: 3, maxRows: 5 }" type="textarea" />
                </el-form-item>
              </div>
            </el-collapse-item>
          </el-collapse>
        </div>
      </el-form>

      <template #footer>
        <el-button :disabled="submittingAccount" @click="accountFormVisible = false">取消</el-button>
        <el-button :loading="submittingAccount" type="primary" @click="submitAccount">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="configDialogVisible"
      :title="configDialogTitle"
      class="form-dialog"
      destroy-on-close
      width="min(760px, calc(100vw - 32px))"
    >
      <div v-if="selectedConfig" class="dialog-stack">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item :label="t('database.column.protocol')">
            {{ selectedConfig.proxy.protocol || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('database.column.listen')">
            <span class="mono-text">{{ selectedConfig.proxy.listen_addr || t('common.none') }}</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('database.column.upstream')">
            <span class="mono-text">{{ selectedConfig.proxy.upstream_addr || t('common.none') }}</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('database.column.account')">
            {{ accountUsername(selectedConfig.account) || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('database.column.resourceType')">
            <span class="mono-text">{{ accountResourceType(selectedConfig.account) }}</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('database.column.resourceId')">
            <span class="mono-text">{{ accountResourceId(selectedConfig.account) }}</span>
          </el-descriptions-item>
        </el-descriptions>

        <div class="config-list">
          <div v-for="item in connectionItems" :key="item.key" class="config-row">
            <div class="config-label">{{ item.label }}</div>
            <el-input :model-value="item.value" readonly>
              <template #append>
                <el-tooltip :content="t('quickConnect.action.copy')">
                  <el-button
                    :aria-label="t('quickConnect.action.copy')"
                    :icon="CopyDocument"
                    @click="copyText(item.value)"
                  />
                </el-tooltip>
              </template>
            </el-input>
          </div>
        </div>
      </div>

      <template #footer>
        <el-button @click="configDialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button :disabled="!selectedConfig" :icon="CopyDocument" type="primary" @click="copySelectedConfig">
          {{ t('database.action.copyAll') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue';
import {
  Connection,
  CopyDocument,
  Delete as DeleteIcon,
  Edit as EditIcon,
  Plus,
  Refresh,
  User
} from '@element-plus/icons-vue';
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus';

import {
  apiClient,
  type ApiEnvelope,
  type DBProxyAccountPayload,
  type DBProxyAccountRecord,
  type DBProxyPayload,
  type DBProxyRecord
} from '@/api/client';
import { useI18n } from '@/i18n';

type DBProxyConfigSelection = {
  proxy: DBProxyRecord;
  account: DBProxyAccountRecord;
};
type ConnectionItem = {
  key: string;
  label: string;
  value: string;
};
type HostPortParts = {
  host: string;
  port: string;
};
type EndpointFormParts = {
  host: string;
  port: number;
};
type DBProxyForm = {
  name: string;
  enabled: boolean;
  protocol: string;
  listen_host: string;
  listen_port: number;
  upstream_host: string;
  upstream_port: number;
  remark: string;
};
type DBAccountForm = {
  username: string;
  database: string;
  remark: string;
  disabled: boolean;
};

const { t } = useI18n();
const keyword = ref('');
const loading = ref(false);
const accountsLoading = ref(false);
const submittingProxy = ref(false);
const submittingAccount = ref(false);
const error = ref('');
const accountError = ref('');
const deletingKey = ref('');
const statusUpdatingKey = ref('');
const proxies = ref<DBProxyRecord[]>([]);
const accounts = ref<DBProxyAccountRecord[]>([]);
const selectedProxy = ref<DBProxyRecord | null>(null);
const selectedConfig = ref<DBProxyConfigSelection | null>(null);
const accountsDialogVisible = ref(false);
const proxyFormVisible = ref(false);
const accountFormVisible = ref(false);
const configDialogVisible = ref(false);
const editingProxyName = ref<string | null>(null);
const editingAccountUsername = ref<string | null>(null);
const proxyMorePanels = ref<string[]>([]);
const accountMorePanels = ref<string[]>([]);
const proxyFormRef = ref<FormInstance>();
const accountFormRef = ref<FormInstance>();
const proxyForm = reactive<DBProxyForm>(emptyProxyForm());
const accountForm = reactive<DBAccountForm>(emptyAccountForm());

const proxyRules: FormRules = {
  name: [{ required: true, message: '请输入实例名称', trigger: 'blur' }],
  protocol: [{ required: true, message: '请选择协议', trigger: 'change' }],
  listen_host: [{ required: true, message: '请输入监听地址', trigger: 'blur' }],
  listen_port: [{ required: true, type: 'number', message: '请输入监听端口', trigger: 'change' }],
  upstream_host: [{ required: true, message: '请输入上游地址', trigger: 'blur' }],
  upstream_port: [{ required: true, type: 'number', message: '请输入上游端口', trigger: 'change' }]
};
const accountRules: FormRules = {
  username: [{ required: true, message: '请输入登录账号', trigger: 'blur' }]
};

const filteredProxies = computed(() => {
  const query = keyword.value.trim().toLowerCase();

  if (!query) {
    return proxies.value;
  }

  return proxies.value.filter((proxy) =>
    [
      proxyName(proxy),
      proxy.protocol,
      proxy.listen_addr,
      proxy.upstream_addr,
      proxy.remark,
      proxy.enabled ? t('common.enabled') : t('common.disabled')
    ].some((value) => String(value ?? '').toLowerCase().includes(query))
  );
});
const selectedProxyAccounts = computed(() => accounts.value);
const accountsDialogTitle = computed(() => {
  const proxy = selectedProxy.value;

  return proxy ? `${proxyName(proxy)} - 数据库账号` : '数据库账号';
});
const configDialogTitle = computed(() => {
  const selection = selectedConfig.value;

  if (!selection) {
    return t('database.dialog.connectionConfig');
  }

  const account = accountUsername(selection.account) || t('common.none');

  return `${t('database.dialog.connectionConfig')} - ${account}@${proxyName(selection.proxy)}`;
});
const connectionItems = computed<ConnectionItem[]>(() =>
  selectedConfig.value ? buildConnectionItems(selectedConfig.value.proxy, selectedConfig.value.account) : []
);

function emptyProxyForm(): DBProxyForm {
  return {
    name: '',
    enabled: true,
    protocol: 'mysql',
    listen_host: '127.0.0.1',
    listen_port: 33060,
    upstream_host: '127.0.0.1',
    upstream_port: 3306,
    remark: ''
  };
}

function emptyAccountForm(): DBAccountForm {
  return {
    username: '',
    database: '',
    remark: '',
    disabled: false
  };
}

function unwrapArray<T>(payload: ApiEnvelope<T[]> | T[]): T[] {
  return Array.isArray(payload) ? payload : payload.data ?? [];
}

function stringFrom(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number' ? String(value) : '';
}

function numberFrom(value: unknown, fallback: number): number {
  const parsed = Number(value);

  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}

function defaultUpstreamPort(protocol: string): number {
  if (protocol === 'postgres') {
    return 5432;
  }

  return 3306;
}

function defaultListenPort(protocol: string): number {
  if (protocol === 'postgres') {
    return 54320;
  }

  if (protocol === 'tcp') {
    return 15432;
  }

  return 33060;
}

function normalizeProtocol(protocol: unknown): string {
  const value = stringFrom(protocol).trim().toLowerCase();

  if (value === 'postgresql' || value === 'pg') {
    return 'postgres';
  }

  return value || 'tcp';
}

function defaultPort(protocol: string): string {
  if (protocol === 'mysql') {
    return '3306';
  }

  if (protocol === 'postgres') {
    return '5432';
  }

  return '<port>';
}

function publicListenHost(host: string): string {
  const normalized = host.trim().replace(/^\[(.*)]$/, '$1');

  if (!normalized || normalized === '0.0.0.0' || normalized === '::' || normalized === '::0') {
    return '127.0.0.1';
  }

  return normalized;
}

function splitEndpointForForm(address: unknown, fallbackPort: number): EndpointFormParts {
  const value = stringFrom(address).trim();

  if (!value) {
    return { host: '', port: fallbackPort };
  }

  const bracketMatch = /^\[([^\]]+)]:(\d+)$/.exec(value);
  if (bracketMatch) {
    return { host: bracketMatch[1], port: numberFrom(bracketMatch[2], fallbackPort) };
  }

  const lastColon = value.lastIndexOf(':');
  if (lastColon > -1) {
    const host = value.slice(0, lastColon);
    const port = value.slice(lastColon + 1);

    if (host && /^\d+$/.test(port)) {
      return { host, port: numberFrom(port, fallbackPort) };
    }
  }

  return { host: value, port: fallbackPort };
}

function parseHostPort(address: unknown, protocol: string): HostPortParts {
  const value = stringFrom(address).trim();
  const fallbackPort = defaultPort(protocol);

  if (!value) {
    return { host: '127.0.0.1', port: fallbackPort };
  }

  const bracketMatch = /^\[([^\]]+)]:(\d+)$/.exec(value);
  if (bracketMatch) {
    return { host: publicListenHost(bracketMatch[1]), port: bracketMatch[2] };
  }

  const lastColon = value.lastIndexOf(':');
  if (lastColon > -1) {
    const host = value.slice(0, lastColon);
    const port = value.slice(lastColon + 1);

    if (/^\d+$/.test(port)) {
      return { host: publicListenHost(host), port };
    }
  }

  return { host: publicListenHost(value), port: fallbackPort };
}

function formatHostPort(host: string, port: string | number): string {
  const trimmedHost = host.trim();
  const safeHost = trimmedHost.includes(':') && !trimmedHost.startsWith('[') ? `[${trimmedHost}]` : trimmedHost;

  return `${safeHost}:${port}`;
}

function encodeUrlPart(value: string): string {
  return value.startsWith('<') && value.endsWith('>') ? value : encodeURIComponent(value);
}

function proxyName(proxy: DBProxyRecord): string {
  return stringFrom(proxy.name).trim() || stringFrom(proxy.upstream_addr).trim() || stringFrom(proxy.listen_addr).trim() || '-';
}

function proxyRowKey(proxy: DBProxyRecord): string {
  return proxyName(proxy);
}

function proxyStatusKey(proxy: DBProxyRecord): string {
  return `proxy:${proxyName(proxy)}`;
}

function accountCount(proxy: DBProxyRecord): number {
  const explicitCount = Number(proxy.account_count ?? proxy.accounts_count);

  return Number.isFinite(explicitCount) && explicitCount >= 0 ? explicitCount : 0;
}

function accountUsername(account: DBProxyAccountRecord): string {
  return stringFrom(account.username).trim();
}

function accountResourceType(account: DBProxyAccountRecord): string {
  return stringFrom(account.resource_type).trim() || 'database_account';
}

function accountResourceId(account: DBProxyAccountRecord): string {
  return stringFrom(account.resource_id).trim() || accountUsername(account) || '-';
}

function accountResource(account: DBProxyAccountRecord): string {
  return `${accountResourceType(account)}:${accountResourceId(account)}`;
}

function accountRowKey(account: DBProxyAccountRecord, index?: number): string {
  return `${accountResourceType(account)}:${accountResourceId(account)}:${accountUsername(account) || String(index ?? 0)}`;
}

function accountDeleteKey(account: DBProxyAccountRecord): string {
  return `${proxyName(selectedProxy.value || {})}:${accountUsername(account)}`;
}

function accountStatusKey(account: DBProxyAccountRecord): string {
  return `account:${accountDeleteKey(account)}`;
}

function databaseName(proxy: DBProxyRecord, account: DBProxyAccountRecord): string {
  return (
    stringFrom(account.database) ||
    stringFrom(account.db_name) ||
    stringFrom(account.database_name) ||
    stringFrom(proxy.database) ||
    '-'
  );
}

function connectionDatabaseName(proxy: DBProxyRecord, account: DBProxyAccountRecord): string {
  const database = databaseName(proxy, account);

  return database === '-' ? '<database>' : database;
}

function buildConnectionItems(proxy: DBProxyRecord, account: DBProxyAccountRecord): ConnectionItem[] {
  const protocol = normalizeProtocol(proxy.protocol);
  const endpoint = parseHostPort(proxy.listen_addr, protocol);
  const listenEndpoint = formatHostPort(endpoint.host, endpoint.port);
  const username = accountUsername(account) || '<username>';
  const database = connectionDatabaseName(proxy, account);
  const encodedUser = encodeUrlPart(username);
  const encodedDatabase = encodeUrlPart(database);

  if (protocol === 'mysql') {
    return [
      {
        key: 'mysql-command',
        label: t('database.config.mysqlCommand'),
        value: `mysql --protocol=tcp -h ${endpoint.host} -P ${endpoint.port} -u ${username} -p -D ${database}`
      },
      {
        key: 'mysql-url-dsn',
        label: t('database.config.mysqlUrlDsn'),
        value: `mysql://${encodedUser}:<password>@${listenEndpoint}/${encodedDatabase}`
      },
      {
        key: 'mysql-driver-dsn',
        label: t('database.config.mysqlDriverDsn'),
        value: `${username}:<password>@tcp(${listenEndpoint})/${database}`
      },
      {
        key: 'resource',
        label: t('database.config.resource'),
        value: accountResource(account)
      }
    ];
  }

  if (protocol === 'postgres') {
    return [
      {
        key: 'postgres-command',
        label: t('database.config.postgresCommand'),
        value: `psql -h ${endpoint.host} -p ${endpoint.port} -U ${username} -d ${database}`
      },
      {
        key: 'postgres-url-dsn',
        label: t('database.config.postgresUrlDsn'),
        value: `postgresql://${encodedUser}:<password>@${listenEndpoint}/${encodedDatabase}?sslmode=disable`
      },
      {
        key: 'postgres-key-value-dsn',
        label: t('database.config.postgresKeyValueDsn'),
        value: `host=${endpoint.host} port=${endpoint.port} user=${username} password=<password> dbname=${database} sslmode=disable`
      },
      {
        key: 'resource',
        label: t('database.config.resource'),
        value: accountResource(account)
      }
    ];
  }

  return [
    {
      key: 'tcp-endpoint',
      label: t('database.config.tcpEndpoint'),
      value: listenEndpoint
    },
    {
      key: 'database-account',
      label: t('database.column.account'),
      value: username
    },
    {
      key: 'resource',
      label: t('database.config.resource'),
      value: accountResource(account)
    }
  ];
}

function resetProxyForm(values: DBProxyForm = emptyProxyForm()) {
  Object.assign(proxyForm, values);
}

function resetAccountForm(values: DBAccountForm = emptyAccountForm()) {
  Object.assign(accountForm, values);
}

function proxyToForm(proxy: DBProxyRecord): DBProxyForm {
  const protocol = normalizeProtocol(proxy.protocol);
  const listen = splitEndpointForForm(proxy.listen_addr, defaultListenPort(protocol));
  const upstream = splitEndpointForForm(proxy.upstream_addr, defaultUpstreamPort(protocol));

  return {
    name: proxyName(proxy),
    enabled: proxy.enabled !== false,
    protocol,
    listen_host: listen.host,
    listen_port: listen.port,
    upstream_host: upstream.host,
    upstream_port: upstream.port,
    remark: stringFrom(proxy.remark)
  };
}

function accountToForm(account: DBProxyAccountRecord): DBAccountForm {
  return {
    username: accountUsername(account),
    database: databaseName({}, account) === '-' ? '' : databaseName({}, account),
    remark: stringFrom(account.remark),
    disabled: account.disabled === true
  };
}

function buildProxyPayload(): DBProxyPayload {
  return {
    name: proxyForm.name.trim(),
    enabled: proxyForm.enabled,
    protocol: normalizeProtocol(proxyForm.protocol),
    listen_addr: formatHostPort(proxyForm.listen_host, proxyForm.listen_port),
    upstream_addr: formatHostPort(proxyForm.upstream_host, proxyForm.upstream_port),
    remark: proxyForm.remark.trim() || undefined
  };
}

function proxyRecordPayload(proxy: DBProxyRecord, enabled: boolean): DBProxyPayload {
  return {
    name: proxyName(proxy),
    enabled,
    protocol: normalizeProtocol(proxy.protocol),
    listen_addr: stringFrom(proxy.listen_addr).trim(),
    upstream_addr: stringFrom(proxy.upstream_addr).trim(),
    remark: stringFrom(proxy.remark).trim() || undefined
  };
}

function buildAccountPayload(): DBProxyAccountPayload {
  return {
    username: accountForm.username.trim(),
    database: accountForm.database.trim() || undefined,
    remark: accountForm.remark.trim() || undefined,
    disabled: accountForm.disabled
  };
}

function accountRecordPayload(account: DBProxyAccountRecord, disabled: boolean): DBProxyAccountPayload {
  const database = databaseName({}, account);
  return {
    username: accountUsername(account),
    database: database === '-' ? undefined : database,
    remark: stringFrom(account.remark).trim() || undefined,
    disabled
  };
}

function handleProxyProtocolChange() {
  proxyForm.listen_port = defaultListenPort(proxyForm.protocol);
  proxyForm.upstream_port = defaultUpstreamPort(proxyForm.protocol);
}

async function openCreateProxyDialog() {
  editingProxyName.value = null;
  proxyMorePanels.value = [];
  resetProxyForm();
  proxyFormVisible.value = true;
  await nextTick();
  proxyFormRef.value?.clearValidate();
}

async function openEditProxyDialog(proxy: DBProxyRecord) {
  editingProxyName.value = proxyName(proxy);
  proxyMorePanels.value = [];
  resetProxyForm(proxyToForm(proxy));
  proxyFormVisible.value = true;
  await nextTick();
  proxyFormRef.value?.clearValidate();
}

async function submitProxy() {
  const valid = await proxyFormRef.value?.validate().catch(() => false);

  if (!valid) {
    return;
  }

  submittingProxy.value = true;
  try {
    const id = editingProxyName.value;
    if (id) {
      await apiClient.updateDBProxy(id, buildProxyPayload());
      ElMessage.success('数据库实例已更新');
    } else {
      await apiClient.createDBProxy(buildProxyPayload());
      ElMessage.success('数据库实例已创建');
    }
    proxyFormVisible.value = false;
    await loadProxies();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.save'));
  } finally {
    submittingProxy.value = false;
  }
}

async function toggleProxyStatus(proxy: DBProxyRecord) {
  const name = proxyName(proxy);
  const enabled = proxy.enabled !== true;
  statusUpdatingKey.value = proxyStatusKey(proxy);
  try {
    await apiClient.updateDBProxy(name, proxyRecordPayload(proxy, enabled));
    ElMessage.success(enabled ? '数据库实例已启用' : '数据库实例已禁用');
    await loadProxies();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.save'));
  } finally {
    statusUpdatingKey.value = '';
  }
}

async function confirmDeleteProxy(proxy: DBProxyRecord) {
  const name = proxyName(proxy);
  try {
    await ElMessageBox.confirm(`确认删除数据库实例“${name}”？实例下的数据库账号也会删除。`, '删除数据库实例', {
      cancelButtonText: '取消',
      confirmButtonText: '删除',
      type: 'warning'
    });
  } catch {
    return;
  }

  deletingKey.value = name;
  try {
    await apiClient.deleteDBProxy(name);
    ElMessage.success('数据库实例已删除');
    if (selectedProxy.value && proxyName(selectedProxy.value) === name) {
      selectedProxy.value = null;
      accounts.value = [];
      accountsDialogVisible.value = false;
    }
    await loadProxies();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.delete'));
  } finally {
    deletingKey.value = '';
  }
}

async function openAccountsDialog(proxy: DBProxyRecord) {
  selectedProxy.value = proxy;
  accounts.value = [];
  accountError.value = '';
  accountsDialogVisible.value = true;
  await loadSelectedProxyAccounts();
}

async function loadSelectedProxyAccounts() {
  const proxy = selectedProxy.value;
  if (!proxy) {
    return;
  }

  accountsLoading.value = true;
  accountError.value = '';

  try {
    accounts.value = unwrapArray(await apiClient.getDBProxyAccounts(proxyName(proxy)));
  } catch (err) {
    accounts.value = [];
    accountError.value = err instanceof Error ? err.message : t('database.error.loadAccounts');
  } finally {
    accountsLoading.value = false;
  }
}

async function openCreateAccountDialog(proxy: DBProxyRecord) {
  selectedProxy.value = proxy;
  editingAccountUsername.value = null;
  accountMorePanels.value = [];
  resetAccountForm();
  accountFormVisible.value = true;
  await nextTick();
  accountFormRef.value?.clearValidate();
}

async function openEditAccountDialog(account: DBProxyAccountRecord) {
  editingAccountUsername.value = accountUsername(account);
  accountMorePanels.value = [];
  resetAccountForm(accountToForm(account));
  accountFormVisible.value = true;
  await nextTick();
  accountFormRef.value?.clearValidate();
}

async function submitAccount() {
  const proxy = selectedProxy.value;
  if (!proxy) {
    ElMessage.error('请先选择数据库实例');
    return;
  }

  const valid = await accountFormRef.value?.validate().catch(() => false);
  if (!valid) {
    return;
  }

  submittingAccount.value = true;
  try {
    const username = editingAccountUsername.value;
    if (username) {
      await apiClient.updateDBProxyAccount(proxyName(proxy), username, buildAccountPayload());
      ElMessage.success('数据库账号已更新');
    } else {
      await apiClient.createDBProxyAccount(proxyName(proxy), buildAccountPayload());
      ElMessage.success('数据库账号已创建');
    }
    accountFormVisible.value = false;
    await Promise.all([loadProxies(), loadSelectedProxyAccounts()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.save'));
  } finally {
    submittingAccount.value = false;
  }
}

async function toggleAccountStatus(account: DBProxyAccountRecord) {
  const proxy = selectedProxy.value;
  const username = accountUsername(account);

  if (!proxy || !username) {
    return;
  }

  const disabled = account.disabled !== true;
  statusUpdatingKey.value = accountStatusKey(account);
  try {
    await apiClient.updateDBProxyAccount(proxyName(proxy), username, accountRecordPayload(account, disabled));
    ElMessage.success(disabled ? '数据库账号已禁用' : '数据库账号已启用');
    await Promise.all([loadProxies(), loadSelectedProxyAccounts()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.save'));
  } finally {
    statusUpdatingKey.value = '';
  }
}

async function confirmDeleteAccount(account: DBProxyAccountRecord) {
  const proxy = selectedProxy.value;
  const username = accountUsername(account);

  if (!proxy || !username) {
    return;
  }

  try {
    await ElMessageBox.confirm(`确认删除数据库账号“${username}”？`, '删除数据库账号', {
      cancelButtonText: '取消',
      confirmButtonText: '删除',
      type: 'warning'
    });
  } catch {
    return;
  }

  deletingKey.value = accountDeleteKey(account);
  try {
    await apiClient.deleteDBProxyAccount(proxyName(proxy), username);
    ElMessage.success('数据库账号已删除');
    await Promise.all([loadProxies(), loadSelectedProxyAccounts()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.delete'));
  } finally {
    deletingKey.value = '';
  }
}

function openConnectionDialog(account: DBProxyAccountRecord) {
  const proxy = selectedProxy.value;

  if (!proxy) {
    return;
  }

  selectedConfig.value = { proxy, account };
  configDialogVisible.value = true;
}

function configText(proxy: DBProxyRecord, account: DBProxyAccountRecord): string {
  return [
    `${t('database.column.protocol')}: ${proxy.protocol || t('common.none')}`,
    `${t('database.column.listen')}: ${proxy.listen_addr || t('common.none')}`,
    `${t('database.column.upstream')}: ${proxy.upstream_addr || t('common.none')}`,
    `${t('database.column.account')}: ${accountUsername(account) || t('common.none')}`,
    `${t('database.column.resourceType')}: ${accountResourceType(account)}`,
    `${t('database.column.resourceId')}: ${accountResourceId(account)}`,
    ...buildConnectionItems(proxy, account).map((item) => `${item.label}: ${item.value}`)
  ].join('\n');
}

async function writeClipboard(value: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const textarea = document.createElement('textarea');
  textarea.value = value;
  textarea.setAttribute('readonly', 'true');
  textarea.style.position = 'fixed';
  textarea.style.top = '-9999px';
  document.body.appendChild(textarea);
  textarea.select();

  try {
    if (!document.execCommand('copy')) {
      throw new Error('copy command failed');
    }
  } finally {
    document.body.removeChild(textarea);
  }
}

async function copyText(value: string) {
  if (!value.trim()) {
    ElMessage.warning(t('database.message.noCopyContent'));
    return;
  }

  try {
    await writeClipboard(value);
    ElMessage.success(t('quickConnect.message.copied'));
  } catch {
    ElMessage.warning(t('quickConnect.error.copy'));
  }
}

async function copySelectedConfig() {
  const selection = selectedConfig.value;

  if (!selection) {
    return;
  }

  await copyText(configText(selection.proxy, selection.account));
}

async function loadProxies() {
  loading.value = true;
  error.value = '';
  const selectedName = selectedProxy.value ? proxyName(selectedProxy.value) : '';

  try {
    const nextProxies = unwrapArray(await apiClient.getDBProxies());
    proxies.value = nextProxies;
    if (selectedName) {
      selectedProxy.value = nextProxies.find((proxy) => proxyName(proxy) === selectedName) ?? selectedProxy.value;
    }
  } catch (err) {
    proxies.value = [];
    error.value = err instanceof Error ? err.message : t('database.error.loadResources');
  } finally {
    loading.value = false;
  }
}

onMounted(loadProxies);
</script>

<style scoped>
.toolbar-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.primary-cell {
  color: #111827;
  font-weight: 650;
}

.mono-text {
  overflow-wrap: anywhere;
  color: #475467;
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", monospace;
  font-size: 12px;
}

.dialog-stack {
  display: flex;
  flex-direction: column;
  gap: 18px;
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

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  column-gap: 16px;
}

.form-grid .el-input-number {
  width: 100%;
}

.form-full {
  grid-column: 1 / -1;
}

.switch-field :deep(.el-form-item__content) {
  min-height: 32px;
  align-items: center;
}

.config-list {
  display: grid;
  gap: 14px;
}

.config-row {
  display: grid;
  grid-template-columns: minmax(150px, 210px) minmax(0, 1fr);
  gap: 12px;
  align-items: center;
}

.config-label {
  color: #344054;
  font-size: 13px;
  font-weight: 650;
}

:global(.form-dialog .el-dialog__body) {
  max-height: min(66vh, 620px);
  overflow-y: auto;
  padding-right: 22px;
}

@media (max-width: 720px) {
  .form-grid,
  .config-row {
    grid-template-columns: 1fr;
  }
}
</style>
