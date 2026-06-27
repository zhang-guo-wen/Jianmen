<template>
  <div class="view-stack">
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <el-tab-pane :label="t('database.instance.title')" name="instances" />
      <el-tab-pane :disabled="!selectedInstance" :label="t('database.account.title')" name="accounts" />
    </el-tabs>

    <!-- Tab: Instances -->
    <template v-if="activeTab === 'instances'">
      <div class="toolbar">
        <el-input
          v-model="instanceKeyword"
          clearable
          :placeholder="t('database.instance.placeholder.search')"
          style="max-width: 360px"
          @keyup.enter="loadInstances"
        />
        <div class="toolbar-actions">
          <el-button :loading="instancesLoading" @click="loadInstances">
            {{ t('common.refresh') }}
          </el-button>
          <el-button type="primary" @click="openCreateInstanceDialog">
            {{ t('database.instance.create') }}
          </el-button>
        </div>
      </div>

      <el-card class="placeholder-panel" shadow="never">
        <el-alert v-if="instanceError" :title="instanceError" type="error" show-icon />
        <el-table v-else v-loading="instancesLoading" :data="filteredInstances" row-key="id">
          <el-table-column :label="t('database.instance.name')" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">
              <strong class="primary-cell">{{ row.name || '-' }}</strong>
            </template>
          </el-table-column>
          <el-table-column :label="t('database.instance.protocol')" width="110">
            <template #default="{ row }">
              <el-tag size="small">{{ row.protocol || t('common.none') }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('database.instance.address')" min-width="190" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="mono-text">{{ row.address || t('common.none') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('database.instance.group')" min-width="120" show-overflow-tooltip>
            <template #default="{ row }">
              {{ row.group_name || '-' }}
            </template>
          </el-table-column>
          <el-table-column :label="t('database.instance.accountCount')" width="100">
            <template #default="{ row }">
              <el-button link type="primary" @click="openAccountsTab(row)">
                {{ row.account_count ?? 0 }}
              </el-button>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="90">
            <template #default="{ row }">
              <el-tag :type="row.disabled ? 'info' : 'success'">
                {{ row.disabled ? t('common.disabled') : t('common.enabled') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('database.instance.remark')" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">
              {{ row.remark || '-' }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" fixed="right" width="290">
            <template #default="{ row }">
              <el-button link type="primary" @click="openEditInstanceDialog(row)">
                {{ t('database.instance.edit') }}
              </el-button>
              <el-button
                :loading="instanceStatusUpdatingId === row.id"
                link
                :type="row.disabled ? 'success' : 'warning'"
                @click="toggleInstanceStatus(row)"
              >
                {{ row.disabled ? t('common.enabled') : t('common.disabled') }}
              </el-button>
              <el-button
                :loading="instanceDeletingId === row.id"
                link
                type="danger"
                @click="confirmDeleteInstance(row)"
              >
                {{ t('database.instance.delete') }}
              </el-button>
              <el-button link type="success" @click="openAccountsTab(row)">
                {{ t('database.account.title') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!instancesLoading && !filteredInstances.length && !instanceError" :description="t('database.instance.empty')" />
        <div class="pagination-row">
          <el-pagination
            v-model:current-page="instancePage"
            v-model:page-size="instancePageSize"
            background
            layout="total, sizes, prev, pager, next"
            :page-sizes="[20, 50, 100]"
            :total="instanceTotal"
            @current-change="loadInstances"
            @size-change="handleInstancePageSizeChange"
          />
        </div>
      </el-card>

      <!-- Instance Create/Edit Dialog -->
      <el-dialog
        v-model="instanceDialogVisible"
        :close-on-click-modal="!submittingInstance"
        :title="editingInstanceId ? t('database.instance.edit') : t('database.instance.create')"
        class="form-dialog"
        destroy-on-close
        width="min(560px, calc(100vw - 32px))"
      >
        <el-form ref="instanceFormRef" :model="instanceForm" :rules="instanceRules" label-position="top">
          <div class="form-sections">
            <section class="form-section">
              <div class="form-section-title">连接信息</div>
              <div class="form-grid">
                <el-form-item :label="t('database.instance.name')" prop="name">
                  <el-input v-model="instanceForm.name" :placeholder="t('database.instance.placeholder.name')" />
                </el-form-item>
                <el-form-item :label="t('database.instance.protocol')" prop="protocol">
                  <el-select v-model="instanceForm.protocol">
                    <el-option label="MySQL" value="mysql" />
                    <el-option label="PostgreSQL" value="postgres" />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('database.instance.address')" prop="address" class="form-full">
                  <el-input v-model="instanceForm.address" :placeholder="t('database.instance.placeholder.address')" />
                </el-form-item>
              </div>
            </section>

            <el-collapse v-model="instanceMorePanels" class="more-collapse">
              <el-collapse-item :title="t('database.account.moreSettings')" name="more">
                <div class="form-grid">
                  <el-form-item :label="t('database.instance.group')" prop="group_name">
                    <el-select
                      v-model="instanceForm.group_name"
                      allow-create
                      clearable
                      filterable
                      placeholder="选择或输入分组"
                    >
                      <el-option
                        v-for="group in instanceGroupOptions"
                        :key="group"
                        :label="group"
                        :value="group"
                      />
                    </el-select>
                  </el-form-item>
                  <el-form-item :label="t('database.instance.remark')" prop="remark" class="form-full">
                    <el-input v-model="instanceForm.remark" :autosize="{ minRows: 3, maxRows: 5 }" type="textarea" />
                  </el-form-item>
                </div>
              </el-collapse-item>
            </el-collapse>
          </div>
        </el-form>

        <template #footer>
          <el-button :disabled="submittingInstance" @click="instanceDialogVisible = false">
            {{ t('common.cancel') }}
          </el-button>
          <el-button :loading="submittingInstance" type="primary" @click="submitInstance">
            {{ t('common.create') }}
          </el-button>
        </template>
      </el-dialog>
    </template>

    <!-- Tab: Accounts -->
    <template v-if="activeTab === 'accounts' && selectedInstance">
      <div class="toolbar">
        <div class="toolbar-breadcrumb">
          <el-button link type="primary" @click="activeTab = 'instances'">
            &larr; {{ t('database.instance.title') }}
          </el-button>
          <span class="breadcrumb-separator">/</span>
          <strong>{{ selectedInstance.name || '-' }}</strong>
        </div>
        <div class="toolbar-actions">
          <el-button :loading="accountsLoading" @click="loadAccounts">
            {{ t('common.refresh') }}
          </el-button>
          <el-button type="primary" @click="openCreateAccountDialog">
            {{ t('database.account.create') }}
          </el-button>
        </div>
      </div>

      <el-card class="placeholder-panel" shadow="never">
        <el-alert v-if="accountError" :title="accountError" type="error" show-icon />
        <el-table v-else v-loading="accountsLoading" :data="accounts" row-key="id">
          <el-table-column :label="t('database.account.uniqueName')" min-width="180" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="mono-text">{{ row.unique_name || t('common.none') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('database.account.upstreamUsername')" min-width="150" show-overflow-tooltip>
            <template #default="{ row }">
              {{ row.upstream_username || t('common.none') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('database.account.group')" min-width="120" show-overflow-tooltip>
            <template #default="{ row }">
              {{ row.group_name || '-' }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="90">
            <template #default="{ row }">
              <el-tag :type="row.disabled ? 'info' : 'success'">
                {{ row.disabled ? t('common.disabled') : t('common.enabled') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('database.account.expiresAt')" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">
              {{ formatTime(row.expires_at) || t('common.none') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" fixed="right" width="280">
            <template #default="{ row }">
              <el-button link type="success" @click="openConnectDialog(row)">
                {{ t('database.account.connect') }}
              </el-button>
              <el-button link type="primary" @click="openEditAccountDialog(row)">
                {{ t('database.account.edit') }}
              </el-button>
              <el-button
                :loading="accountStatusUpdatingId === row.id"
                link
                :type="row.disabled ? 'success' : 'warning'"
                @click="toggleAccountStatus(row)"
              >
                {{ row.disabled ? t('common.enabled') : t('common.disabled') }}
              </el-button>
              <el-button
                :loading="accountDeletingId === row.id"
                link
                type="danger"
                @click="confirmDeleteAccount(row)"
              >
                {{ t('database.account.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!accountsLoading && !accounts.length && !accountError" :description="t('database.empty.accounts')" />
        <div v-if="accountTotal > 0" class="pagination-row">
          <el-pagination
            v-model:current-page="accountPage"
            v-model:page-size="accountPageSize"
            background
            layout="total, sizes, prev, pager, next"
            :page-sizes="[20, 50, 100]"
            :total="accountTotal"
            @current-change="loadAccounts"
            @size-change="handleAccountPageSizeChange"
          />
        </div>
      </el-card>

      <!-- Account Create/Edit Dialog -->
      <el-dialog
        v-model="accountDialogVisible"
        :close-on-click-modal="!submittingAccount"
        :title="editingAccountId ? t('database.account.edit') : t('database.account.create')"
        class="form-dialog"
        destroy-on-close
        width="min(560px, calc(100vw - 32px))"
      >
        <el-form ref="accountFormRef" :model="accountForm" :rules="accountRules" label-position="top">
          <div class="form-sections">
            <section class="form-section">
              <div class="form-section-title">账号信息</div>
              <div class="form-grid">
                <el-form-item :label="t('database.account.upstreamUsername')" prop="upstream_username">
                  <el-input
                    v-model="accountForm.upstream_username"
                    :disabled="editingAccountId !== null"
                    placeholder="数据库登录用户名"
                  />
                </el-form-item>
                <el-form-item
                  :label="t('database.account.password.label')"
                  prop="upstream_password"
                >
                  <el-input
                    v-model="accountForm.upstream_password"
                    :placeholder="editingAccountId ? t('database.account.password.emptyHint') : ''"
                    show-password
                    type="password"
                  />
                </el-form-item>
              </div>
            </section>

            <section class="form-section">
              <div class="form-section-title">有效期</div>
              <div class="expire-shortcuts">
                <el-button
                  v-for="shortcut in expireShortcuts"
                  :key="shortcut.value"
                  :type="expireShortcutActive === shortcut.value ? 'primary' : ''"
                  size="small"
                  @click="applyExpireShortcut(shortcut.value)"
                >
                  {{ shortcut.label }}
                </el-button>
              </div>
              <el-date-picker
                v-model="accountForm.expires_at"
                class="expire-picker"
                placeholder="选择日期时间"
                type="datetime"
                value-format="YYYY-MM-DDTHH:mm:ss.SSS[Z]"
              />
            </section>

            <el-collapse v-model="accountMorePanels" class="more-collapse">
              <el-collapse-item :title="t('database.account.moreSettings')" name="more">
                <div class="form-grid">
                  <el-form-item :label="t('database.account.group')" prop="group_name">
                    <el-select
                      v-model="accountForm.group_name"
                      allow-create
                      clearable
                      filterable
                      placeholder="选择或输入分组"
                    >
                      <el-option
                        v-for="group in accountGroupOptions"
                        :key="group"
                        :label="group"
                        :value="group"
                      />
                    </el-select>
                  </el-form-item>
                  <el-form-item :label="t('database.account.remark')" prop="remark" class="form-full">
                    <el-input v-model="accountForm.remark" :autosize="{ minRows: 3, maxRows: 5 }" type="textarea" />
                  </el-form-item>
                </div>
              </el-collapse-item>
            </el-collapse>
          </div>
        </el-form>

        <template #footer>
          <el-button :disabled="submittingAccount" @click="accountDialogVisible = false">
            {{ t('common.cancel') }}
          </el-button>
          <el-button :loading="submittingAccount" type="primary" @click="submitAccount">
            {{ t('common.create') }}
          </el-button>
        </template>
      </el-dialog>

      <!-- Connect Dialog -->
      <el-dialog
        v-model="connectDialogVisible"
        :title="t('database.account.connection')"
        class="form-dialog"
        destroy-on-close
        width="min(560px, calc(100vw - 32px))"
      >
        <div v-if="connectTarget" class="dialog-stack">
          <div class="config-row">
            <div class="config-label">Shell</div>
            <el-input :model-value="connectCommand" readonly>
              <template #append>
                <el-tooltip :content="t('database.account.copy')">
                  <el-button :aria-label="t('database.account.copy')" @click="copyText(connectCommand)" />
                </el-tooltip>
              </template>
            </el-input>
          </div>
          <div class="config-row">
            <div class="config-label">{{ t('database.config.resource') }}</div>
            <el-input :model-value="connectResourceId" readonly>
              <template #append>
                <el-tooltip :content="t('database.account.copy')">
                  <el-button :aria-label="t('database.account.copy')" @click="copyText(connectResourceId)" />
                </el-tooltip>
              </template>
            </el-input>
          </div>
        </div>

        <template #footer>
          <el-button @click="connectDialogVisible = false">{{ t('common.cancel') }}</el-button>
          <el-button type="primary" @click="copyAllConnect">{{ t('database.account.copy') }}全部</el-button>
        </template>
      </el-dialog>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue';
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus';
import {
  apiClient,
  type ApiEnvelope,
  type DBInstanceRecord,
  type DBInstancePayload,
  type DBAccountRecord,
  type DBAccountPayload,
  type DBAccountUpdatePayload
} from '@/api/client';
import { useI18n } from '@/i18n';

interface InstanceForm {
  name: string;
  protocol: string;
  address: string;
  group_name: string;
  remark: string;
}

interface AccountForm {
  upstream_username: string;
  upstream_password: string;
  group_name: string;
  remark: string;
  expires_at: string;
}

const { t } = useI18n();

// Tab state
const activeTab = ref('instances');

// Instance state
const instanceKeyword = ref('');
const instances = ref<DBInstanceRecord[]>([]);
const instancesLoading = ref(false);
const instanceError = ref('');
const instanceDeletingId = ref('');
const instanceStatusUpdatingId = ref('');
const instancePage = ref(1);
const instancePageSize = ref(20);
const instanceTotal = ref(0);
const instanceDialogVisible = ref(false);
const submittingInstance = ref(false);
const editingInstanceId = ref<string | null>(null);
const instanceMorePanels = ref<string[]>([]);
const instanceFormRef = ref<FormInstance>();
const instanceGroupOptions = ref<string[]>([]);

const instanceForm = reactive<InstanceForm>({
  name: '',
  protocol: 'mysql',
  address: '',
  group_name: '',
  remark: ''
});

const instanceRules: FormRules = {
  name: [{ required: true, message: '请输入实例名称', trigger: 'blur' }],
  protocol: [{ required: true, message: '请选择协议', trigger: 'change' }],
  address: [{ required: true, message: '请输入上游地址', trigger: 'blur' }]
};

// Account state
const selectedInstance = ref<DBInstanceRecord | null>(null);
const accounts = ref<DBAccountRecord[]>([]);
const accountsLoading = ref(false);
const accountError = ref('');
const accountDeletingId = ref('');
const accountStatusUpdatingId = ref('');
const accountDialogVisible = ref(false);
const submittingAccount = ref(false);
const editingAccountId = ref<string | null>(null);
const accountMorePanels = ref<string[]>([]);
const accountFormRef = ref<FormInstance>();
const expireShortcutActive = ref('');
const accountGroupOptions = ref<string[]>([]);
const accountPage = ref(1);
const accountPageSize = ref(20);
const accountTotal = ref(0);

const accountForm = reactive<AccountForm>({
  upstream_username: '',
  upstream_password: '',
  group_name: '',
  remark: '',
  expires_at: ''
});

const accountRules: FormRules = {
  upstream_username: [{ required: true, message: '请输入目标用户名', trigger: 'blur' }],
  upstream_password: [{ required: true, message: '请输入目标密码', trigger: 'blur' }]
};

// Connect dialog state
const connectDialogVisible = ref(false);
const connectTarget = ref<DBAccountRecord | null>(null);
const userSessionId = ref('00001');

// Computed
const filteredInstances = computed(() => {
  const query = instanceKeyword.value.trim().toLowerCase();
  if (!query) return instances.value;
  return instances.value.filter((inst) =>
    [inst.name, inst.protocol, inst.address, inst.group_name, inst.remark]
      .some((v) => String(v ?? '').toLowerCase().includes(query))
  );
});

const expireShortcuts = computed(() => [
  { label: t('database.account.expireShortcuts.hours8'), value: '8h' },
  { label: t('database.account.expireShortcuts.days7'), value: '7d' },
  { label: t('database.account.expireShortcuts.year1'), value: '1y' },
  { label: t('database.account.expireShortcuts.permanent'), value: 'permanent' }
]);

const connectCommand = computed(() => {
  if (!connectTarget.value || !selectedInstance.value) return '';
  const inst = selectedInstance.value;
  const protocol = inst.protocol || 'mysql';
  const resourceId = connectTarget.value.resource_id || '0000';
  const sessionId = userSessionId.value || '00001';
  const compactUser = `D${resourceId}${sessionId}`;
  // Parse host and port from address
  const address = inst.address || '127.0.0.1:3306';
  const lastColon = address.lastIndexOf(':');
  let host = address;
  let port = '3306';
  if (lastColon > -1) {
    const portPart = address.slice(lastColon + 1);
    if (/^\d+$/.test(portPart)) {
      host = address.slice(0, lastColon);
      port = portPart;
    }
  }
  // Use proxy port convention: default port + 30000
  const proxyPort = Number(port) + 30000;
  if (protocol === 'mysql') {
    return `mysql --protocol=tcp -h ${host} -P ${proxyPort} -u ${compactUser} -p`;
  }
  return `psql -h ${host} -p ${proxyPort} -U ${compactUser}`;
});

const connectResourceId = computed(() => {
  if (!connectTarget.value || !selectedInstance.value) return '';
  const resourceId = connectTarget.value.resource_id || '0000';
  return `database_account:D${resourceId}`;
});

// Helpers
function unwrapArray<T>(payload: ApiEnvelope<T[]> | T[]): T[] {
  return Array.isArray(payload) ? payload : payload.data ?? [];
}

function formatTime(value: unknown): string {
  if (typeof value === 'string' && value.trim()) {
    const d = new Date(value);
    return Number.isNaN(d.getTime()) ? value : d.toLocaleString();
  }
  return '';
}

function computeExpiry(value: string): string {
  if (!value || value === 'permanent') return '';
  const now = new Date();
  const match = /^(\d+)([hdmy])$/.exec(value);
  if (!match) return '';
  const num = Number(match[1]);
  const unit = match[2];
  switch (unit) {
    case 'h': now.setHours(now.getHours() + num); break;
    case 'd': now.setDate(now.getDate() + num); break;
    case 'm': now.setMonth(now.getMonth() + num); break;
    case 'y': now.setFullYear(now.getFullYear() + num); break;
  }
  return now.toISOString().replace('Z', '') + 'Z';
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
    if (!document.execCommand('copy')) throw new Error('copy command failed');
  } finally {
    document.body.removeChild(textarea);
  }
}

async function copyText(value: string) {
  if (!value.trim()) {
    ElMessage.warning('没有可复制的内容');
    return;
  }
  try {
    await writeClipboard(value);
    ElMessage.success(t('database.account.copied'));
  } catch {
    ElMessage.warning('复制失败');
  }
}

function copyAllConnect() {
  if (!connectTarget.value) return;
  const all = `Shell: ${connectCommand.value}\n${t('database.config.resource')}: ${connectResourceId.value}`;
  copyText(all);
}

// Instance methods
async function loadInstances() {
  instancesLoading.value = true;
  instanceError.value = '';
  try {
    const data = unwrapArray(await apiClient.getDBInstances());
    // Collect unique groups
    const groups = new Set<string>();
    for (const inst of data) {
      if (inst.group_name) groups.add(inst.group_name);
    }
    instanceGroupOptions.value = Array.from(groups).sort();
    instances.value = data;
    instanceTotal.value = data.length;
  } catch (err) {
    instances.value = [];
    instanceError.value = err instanceof Error ? err.message : t('database.error.loadResources');
  } finally {
    instancesLoading.value = false;
  }
}

async function openCreateInstanceDialog() {
  editingInstanceId.value = null;
  instanceMorePanels.value = [];
  Object.assign(instanceForm, {
    name: '',
    protocol: 'mysql',
    address: '',
    group_name: '',
    remark: ''
  });
  instanceDialogVisible.value = true;
  await nextTick();
  instanceFormRef.value?.clearValidate();
}

async function openEditInstanceDialog(inst: DBInstanceRecord) {
  editingInstanceId.value = inst.id ?? null;
  instanceMorePanels.value = [];
  Object.assign(instanceForm, {
    name: inst.name || '',
    protocol: inst.protocol || 'mysql',
    address: inst.address || '',
    group_name: inst.group_name || '',
    remark: inst.remark || ''
  });
  instanceDialogVisible.value = true;
  await nextTick();
  instanceFormRef.value?.clearValidate();
}

async function submitInstance() {
  const valid = await instanceFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  submittingInstance.value = true;
  try {
    const payload: DBInstancePayload = {
      name: instanceForm.name.trim(),
      protocol: instanceForm.protocol,
      address: instanceForm.address.trim(),
      group_name: instanceForm.group_name.trim() || undefined,
      remark: instanceForm.remark.trim() || undefined
    };
    if (editingInstanceId.value) {
      await apiClient.updateDBInstance(editingInstanceId.value, { ...payload, disabled: undefined });
      ElMessage.success('数据库实例已更新');
    } else {
      await apiClient.createDBInstance(payload);
      ElMessage.success('数据库实例已创建');
    }
    instanceDialogVisible.value = false;
    await loadInstances();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '保存失败');
  } finally {
    submittingInstance.value = false;
  }
}

async function toggleInstanceStatus(inst: DBInstanceRecord) {
  const id = inst.id;
  if (!id) return;
  const newDisabled = !inst.disabled;
  instanceStatusUpdatingId.value = id;
  try {
    await apiClient.updateDBInstance(id, {
      name: inst.name || '',
      protocol: inst.protocol || 'mysql',
      address: inst.address || '',
      group_name: inst.group_name || undefined,
      remark: inst.remark || undefined,
      disabled: newDisabled
    });
    ElMessage.success(newDisabled ? '数据库实例已禁用' : '数据库实例已启用');
    await loadInstances();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '保存失败');
  } finally {
    instanceStatusUpdatingId.value = '';
  }
}

async function confirmDeleteInstance(inst: DBInstanceRecord) {
  const id = inst.id;
  if (!id) return;
  try {
    await ElMessageBox.confirm(
      t('database.instance.deleteConfirm'),
      t('database.instance.delete'),
      { cancelButtonText: t('common.cancel'), confirmButtonText: t('common.delete'), type: 'warning' }
    );
  } catch {
    return;
  }
  instanceDeletingId.value = id;
  try {
    await apiClient.deleteDBInstance(id);
    ElMessage.success('数据库实例已删除');
    if (selectedInstance.value?.id === id) {
      selectedInstance.value = null;
      accounts.value = [];
      activeTab.value = 'instances';
    }
    await loadInstances();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.delete'));
  } finally {
    instanceDeletingId.value = '';
  }
}

function handleInstancePageSizeChange() {
  instancePage.value = 1;
  loadInstances();
}

function handleAccountPageSizeChange() {
  accountPage.value = 1;
  loadAccounts();
}

// Account methods
function openAccountsTab(inst: DBInstanceRecord) {
  selectedInstance.value = inst;
  accounts.value = [];
  accountError.value = '';
  activeTab.value = 'accounts';
  loadAccounts();
  // Collect group options
  loadAccountGroups();
}

async function loadAccounts() {
  const inst = selectedInstance.value;
  if (!inst?.id) return;
  accountsLoading.value = true;
  accountError.value = '';
  try {
    const data = await apiClient.getDBAccounts(inst.id, {
      page: accountPage.value,
      size: accountPageSize.value
    });
    if (Array.isArray(data)) {
      accounts.value = data;
      accountTotal.value = data.length;
    } else if (data && typeof data === 'object' && 'items' in data) {
      accounts.value = (data as any).items ?? [];
      accountTotal.value = (data as any).total ?? 0;
    } else {
      const unwrapped = unwrapArray(data as any);
      accounts.value = unwrapped as DBAccountRecord[];
      accountTotal.value = unwrapped.length;
    }
  } catch (err) {
    accounts.value = [];
    accountError.value = err instanceof Error ? err.message : t('database.error.loadAccounts');
  } finally {
    accountsLoading.value = false;
  }
}

async function loadAccountGroups() {
  const inst = selectedInstance.value;
  if (!inst?.id) return;
  try {
    const data = await apiClient.getDBAccounts(inst.id, { page: 1, size: 1000 });
    const records = Array.isArray(data) ? data : (data as any)?.items ?? [];
    const groups = new Set<string>();
    for (const acc of records) {
      if (acc.group_name) groups.add(acc.group_name);
    }
    accountGroupOptions.value = Array.from(groups).sort();
  } catch {
    // ignore
  }
}

async function openCreateAccountDialog() {
  editingAccountId.value = null;
  accountMorePanels.value = [];
  expireShortcutActive.value = '';
  Object.assign(accountForm, {
    upstream_username: '',
    upstream_password: '',
    group_name: '',
    remark: '',
    expires_at: ''
  });
  accountDialogVisible.value = true;
  await nextTick();
  accountFormRef.value?.clearValidate();
  // Dynamic rule: password required only for create
  accountRules.upstream_password = [{ required: true, message: '请输入目标密码', trigger: 'blur' }];
}

async function openEditAccountDialog(acc: DBAccountRecord) {
  editingAccountId.value = acc.id ?? null;
  accountMorePanels.value = [];
  expireShortcutActive.value = '';
  Object.assign(accountForm, {
    upstream_username: acc.upstream_username || '',
    upstream_password: '',
    group_name: acc.group_name || '',
    remark: acc.remark || '',
    expires_at: acc.expires_at || ''
  });
  accountDialogVisible.value = true;
  await nextTick();
  accountFormRef.value?.clearValidate();
  // Dynamic rule: password optional for edit
  accountRules.upstream_password = [];
}

function applyExpireShortcut(value: string) {
  expireShortcutActive.value = value;
  if (value === 'permanent') {
    accountForm.expires_at = '';
  } else {
    accountForm.expires_at = computeExpiry(value);
  }
}

async function submitAccount() {
  const inst = selectedInstance.value;
  if (!inst?.id) {
    ElMessage.error('请先选择数据库实例');
    return;
  }
  const valid = await accountFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  submittingAccount.value = true;
  try {
    const basePayload: DBAccountPayload = {
      upstream_username: accountForm.upstream_username.trim(),
      upstream_password: accountForm.upstream_password,
      group_name: accountForm.group_name.trim() || undefined,
      remark: accountForm.remark.trim() || undefined,
      expires_at: accountForm.expires_at || undefined
    };
    if (editingAccountId.value) {
      const updatePayload: DBAccountUpdatePayload = {
        ...basePayload,
        upstream_password: accountForm.upstream_password || undefined
      };
      await apiClient.updateDBAccount(editingAccountId.value, updatePayload);
      ElMessage.success('数据库账号已更新');
    } else {
      await apiClient.createDBAccount(inst.id, basePayload);
      ElMessage.success('数据库账号已创建');
    }
    accountDialogVisible.value = false;
    await Promise.all([loadInstances(), loadAccounts(), loadAccountGroups()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '保存失败');
  } finally {
    submittingAccount.value = false;
  }
}

async function toggleAccountStatus(acc: DBAccountRecord) {
  const id = acc.id;
  if (!id) return;
  const newDisabled = !acc.disabled;
  accountStatusUpdatingId.value = id;
  try {
    await apiClient.updateDBAccount(id, {
      upstream_username: acc.upstream_username || '',
      disabled: newDisabled
    });
    ElMessage.success(newDisabled ? '数据库账号已禁用' : '数据库账号已启用');
    await loadAccounts();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '保存失败');
  } finally {
    accountStatusUpdatingId.value = '';
  }
}

async function confirmDeleteAccount(acc: DBAccountRecord) {
  const id = acc.id;
  if (!id) return;
  try {
    await ElMessageBox.confirm(
      t('database.account.deleteConfirm'),
      t('database.account.delete'),
      { cancelButtonText: t('common.cancel'), confirmButtonText: t('common.delete'), type: 'warning' }
    );
  } catch {
    return;
  }
  accountDeletingId.value = id;
  try {
    await apiClient.deleteDBAccount(id);
    ElMessage.success('数据库账号已删除');
    await Promise.all([loadInstances(), loadAccounts()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('hosts.error.delete'));
  } finally {
    accountDeletingId.value = '';
  }
}

// Connect dialog
function openConnectDialog(acc: DBAccountRecord) {
  connectTarget.value = acc;
  connectDialogVisible.value = true;
}

// Tab change
function handleTabChange(tabName: string | number) {
  if (tabName === 'instances') {
    loadInstances();
  }
}

// Watch for instance selection changes to reload
watch(selectedInstance, (inst) => {
  if (inst && activeTab.value === 'accounts') {
    loadAccounts();
    loadAccountGroups();
  }
});

onMounted(() => {
  loadInstances();
});
</script>

<style scoped>
.toolbar-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.toolbar-breadcrumb {
  display: flex;
  align-items: center;
  gap: 6px;
}

.breadcrumb-separator {
  color: #98a2b3;
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

.pagination-row {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
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

.expire-shortcuts {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}

.expire-picker {
  width: 100%;
}

.config-row {
  display: grid;
  grid-template-columns: minmax(100px, 140px) minmax(0, 1fr);
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
