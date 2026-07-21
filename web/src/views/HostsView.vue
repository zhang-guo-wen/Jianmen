<template>
  <div class="view-stack">
    <div class="page-container">
      <DataTableCard
        :data="hosts"
        :loading="hostsLoading"
        :total="hostTotal"
        v-model:page="hostPage"
        v-model:page-size="hostPageSize"
        search-placeholder="搜索名称、地址、分组..."
        @search="onHostSearch"
      >
        <template #toolbar-filter>
          <ResourceFilterBar
            :model-value="hostGroupFilter"
            :options="hostQuickGroupOptions"
            :preview-limit="6"
            :show-popular="false"
            @update:model-value="setHostGroupFilter"
          />
        </template>
        <template #toolbar-extra>
          <el-button :loading="hostsLoading" :icon="Refresh" @click="fetchHosts">
            刷新
          </el-button>
          <el-button v-if="permission.canDo('host:create')" type="primary" @click="openCreateHostDialog"
            >新增主机</el-button
          >
        </template>
        <el-table-column label="名称" min-width="130" show-overflow-tooltip>
          <template #default="{ row }">{{ hostName(row) }}</template>
        </el-table-column>
        <el-table-column label="地址" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">{{ hostEndpoint(row) }}</template>
        </el-table-column>
        <el-table-column label="协议" width="78" align="center">
          <template #default="{ row }">
            <el-tag :type="hostProtocol(row) === 'rdp' ? 'primary' : 'success'" size="small" effect="plain">
              {{ hostProtocol(row).toUpperCase() }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="账号管理" min-width="110" align="center">
          <template #default="{ row }">
            <el-button v-if="permission.canDo('target:view')" link type="primary" size="small" class="account-mgmt-btn" @click="openAccountsDialog(row)">
              账号管理({{ numberFrom(row.account_count, 0) }})
            </el-button>
          </template>
        </el-table-column>
        <el-table-column label="分组" width="100" show-overflow-tooltip>
          <template #default="{ row }">{{ row.group || "-" }}</template>
        </el-table-column>
        <el-table-column label="状态" width="70" align="center">
          <template #default="{ row }">
            <StatusSwitch
              v-if="row.can_manage && permission.canDo('host:update')"
              :model-value="hostEnabled(row)"
              :loading="statusUpdatingId === hostStatusKey(row)"
              :aria-label="`${hostName(row)}主机状态`"
              @update:model-value="(val: boolean) => toggleHostStatus(row, val)"
            />
            <el-tag v-else size="small" :type="hostEnabled(row) ? 'success' : 'info'">
              {{ hostEnabled(row) ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || "-" }}</template>
        </el-table-column>
        <el-table-column label="操作" width="210" align="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-button v-if="canConnectHost(row)"
                link
                type="success"
                size="small"
                @click="handleHostConnect(row)"
                >连接</el-button
              >
              <el-button v-if="row.can_manage && permission.canDo('host:update')"
                link
                type="primary"
                size="small"
                @click="openEditHostDialog(row)"
                >编辑</el-button
              >
              <el-dropdown v-if="canViewHostAudit(row) || permission.canDo('session:view') || (row.can_manage && permission.canDo('host:delete'))" trigger="click" teleported>
                <el-button link type="primary" size="small"
                  >更多<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button
                >
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item v-if="canViewHostAudit(row)" @click="handleHostAuditLog(row)">审计日志</el-dropdown-item>
                    <el-dropdown-item v-if="permission.canDo('session:view')" @click="handleHostSessions(row)">在线会话</el-dropdown-item>
                    <el-dropdown-item v-if="row.can_manage && permission.canDo('host:delete')"
                      class="danger-dropdown-item"
                      @click="confirmDeleteHost(row)"
                      >删除</el-dropdown-item
                    >
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </template>
        </el-table-column>
      </DataTableCard>

      <!-- 创建/编辑主机弹窗 -->
      <FormDialog
        v-model:visible="hostDialogVisible"
        :title="editingHostId ? '编辑主机' : '新增主机'"
        :loading="submittingHost"
        @submit="submitHost"
      >
        <el-form
          ref="hostFormRef"
          :model="hostForm"
          :rules="hostRules"
          label-position="top"
        >
          <el-form-item label="协议" prop="protocol" required>
            <el-radio-group
              v-model="hostForm.protocol"
              class="auth-method-group"
              @change="handleHostProtocolChange"
            >
              <el-radio-button label="ssh">SSH</el-radio-button>
              <el-radio-button label="rdp">RDP</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="主机地址" prop="address" required>
            <el-input
              v-model="hostForm.address"
              placeholder="IP 或域名，可含端口"
              @blur="normalizeHostAddressInput"
            />
          </el-form-item>
          <el-form-item label="端口" prop="port">
            <el-input-number
              v-model="hostForm.port"
              :min="1"
              :max="65535"
              controls-position="right"
              style="width: 100%"
            />
          </el-form-item>
          <el-collapse v-model="hostMorePanels" class="more-collapse">
            <el-collapse-item title="更多设置" name="more">
              <el-form-item label="名称">
                <el-input
                  v-model="hostForm.name"
                  placeholder="默认 = 地址:端口"
                  @input="hostNameTouched = true"
                />
              </el-form-item>
              <el-form-item label="分组">
                <el-select
                  v-model="hostForm.group"
                  allow-create
                  clearable
                  default-first-option
                  filterable
                  placeholder="选择或输入主机分组"
                >
                  <el-option
                    v-for="g in hostGroupOptions"
                    :key="g"
                    :label="g"
                    :value="g"
                  />
                </el-select>
              </el-form-item>
              <el-form-item label="备注">
                <el-input
                  v-model="hostForm.remark"
                  type="textarea"
                  :autosize="{ minRows: 3, maxRows: 5 }"
                  placeholder="备注信息"
                />
              </el-form-item>
              <template v-if="editingHostIdentity && hostForm.protocol === 'ssh'">
                <el-form-item label="主机密钥指纹">
                  <el-input
                    :model-value="editingHostIdentity.host_key_fingerprint || ''"
                    class="host-identity-value"
                    readonly
                    placeholder="尚未采集"
                  />
                </el-form-item>
                <el-form-item label="known_hosts">
                  <el-input
                    :model-value="editingHostIdentity.known_hosts || ''"
                    class="host-identity-value"
                    type="textarea"
                    :autosize="{ minRows: 2, maxRows: 4 }"
                    readonly
                    placeholder="尚未采集"
                  />
                </el-form-item>
              </template>
            </el-collapse-item>
          </el-collapse>
        </el-form>
      </FormDialog>

      <!-- 账号列表弹窗 -->
      <el-dialog
        v-model="accountsDialogVisible"
        :title="accountsDialogTitle"
        class="accounts-dialog"
        destroy-on-close
        width="min(960px, calc(100vw - 32px))"
      >
        <DataTableCard
          class="accounts-table"
          :data="accounts"
          :loading="accountsLoading"
          :total="accountTotal"
          v-model:page="accountPage"
          v-model:page-size="accountPageSize"
          :show-search="false"
          row-key="id"
        >
          <template #toolbar-extra>
            <el-button
              :loading="accountsLoading"
              @click="loadSelectedHostAccounts"
              >刷新</el-button
            >
            <el-button v-if="selectedHost?.can_manage && permission.canDo('target:create')"
              type="primary"
              :disabled="!selectedHost"
              @click="selectedHost && openCreateAccountDialog(selectedHost)"
            >
              新增账号
            </el-button>
          </template>
          <el-table-column
            label="登录账号"
            min-width="130"
            show-overflow-tooltip
          >
            <template #default="{ row }">{{ row.username || "-" }}</template>
          </el-table-column>
          <el-table-column label="验证方式" width="100">
            <template #default="{ row }">
              <el-tag
                v-for="m in targetAuthMethods(row)"
                :key="m"
                size="small"
                style="margin-right: 4px"
              >
                {{ authMethodLabel(m) }}
              </el-tag>
              <el-tag
                v-if="!targetAuthMethods(row).length"
                size="small"
                type="info"
                >无</el-tag
              >
            </template>
          </el-table-column>
          <el-table-column
            label="分组"
            min-width="110"
            show-overflow-tooltip
          >
            <template #default="{ row }">{{ row.group || "-" }}</template>
          </el-table-column>
          <el-table-column label="状态" width="80" align="center">
            <template #default="{ row }">
              <StatusSwitch
                v-if="row.can_manage && permission.canDo('target:update')"
                :model-value="targetEnabled(row)"
                :loading="statusUpdatingId === accountStatusKey(row)"
                :aria-label="`${accountDisplayName(row)}账号状态`"
                @update:model-value="
                  (val: boolean) => toggleAccountStatus(row, val)
                "
              />
              <el-tag v-else size="small" :type="targetEnabled(row) ? 'success' : 'info'">
                {{ targetEnabled(row) ? '启用' : '停用' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="过期时间"
            min-width="140"
            show-overflow-tooltip
          >
            <template #default="{ row }">{{ expiresAtText(row) }}</template>
          </el-table-column>
          <el-table-column label="备注" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">{{
              targetRemark(row) || "-"
            }}</template>
          </el-table-column>
          <el-table-column label="操作" width="200" fixed="right" align="right">
            <template #default="{ row }">
              <div class="table-actions">
                <el-button v-if="canConnectTarget(row)"
                  link
                  type="success"
                  size="small"
                  @click="openConnectionDialog(row)"
                  >连接</el-button
                >
                <el-button v-if="row.can_manage && permission.canDo('target:update')"
                  link
                  type="primary"
                  size="small"
                  @click="openEditAccountDialog(row)"
                  >编辑</el-button
                >
                <el-button v-if="row.can_manage && permission.canDo('target:delete')"
                  link
                  type="danger"
                  size="small"
                  :loading="deletingId === targetId(row)"
                  @click="confirmDeleteAccount(row)"
                >
                  删除
                </el-button>
              </div>
            </template>
          </el-table-column>
        </DataTableCard>
      </el-dialog>

      <!-- 创建/编辑账号弹窗 -->
      <FormDialog
        v-model:visible="accountFormVisible"
        :title="editingAccountId ? '编辑账号' : '新增账号'"
        :loading="submittingAccount"
        @submit="submitAccount"
      >
        <el-form
          ref="accountFormRef"
          v-loading="accountDetailLoading"
          :model="accountForm"
          :rules="accountRules"
          label-position="top"
        >
          <div class="form-section">
            <div class="form-section-title">登录与认证</div>
            <el-form-item v-if="selectedHostProtocol === 'ssh'" label="认证方式" prop="auth_method">
              <el-radio-group
                v-model="accountForm.auth_method"
                class="auth-method-group"
                @change="handleAuthMethodChange"
              >
                <el-radio-button label="password">密码</el-radio-button>
                <el-radio-button label="private_key">私钥</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="登录账号" prop="username">
              <el-input
                v-model="accountForm.username"
                :placeholder="selectedHostProtocol === 'rdp' ? 'Windows 登录用户名' : 'SSH 登录用户名'"
              />
            </el-form-item>
            <el-form-item
              v-if="accountForm.auth_method === 'password'"
              label="登录密码"
              prop="password"
            >
              <el-input
                v-model="accountForm.password"
                :placeholder="credentialPlaceholder"
                show-password
                type="password"
              />
            </el-form-item>
            <el-form-item
              v-if="selectedHostProtocol === 'ssh' && isKeyAuthMethod(accountForm.auth_method)"
              label="解锁口令"
            >
              <el-input
                v-model="accountForm.passphrase"
                :placeholder="secretPlaceholder"
                show-password
                type="password"
              />
            </el-form-item>
            <el-form-item
              v-if="selectedHostProtocol === 'ssh' && accountForm.auth_method === 'private_key'"
              label="私钥"
              prop="private_key_pem"
            >
              <div class="private-key-field">
                <div class="private-key-toolbar">
                  <el-button size="small" @click="triggerPrivateKeyFileSelect"
                    >选择文件</el-button
                  >
                  <span>{{
                    privateKeyFileName ||
                    (accountForm.private_key_pem
                      ? "已读取私钥内容"
                      : "未选择文件")
                  }}</span>
                  <input
                    ref="privateKeyFileInputRef"
                    class="private-key-file-input"
                    type="file"
                    @change="handlePrivateKeyFileChange"
                  />
                </div>
                <el-input
                  v-model="accountForm.private_key_pem"
                  :autosize="{ minRows: 4, maxRows: 8 }"
                  :placeholder="privateKeyPEMPlaceholder"
                  type="textarea"
                />
              </div>
            </el-form-item>
            <el-form-item v-if="selectedHostProtocol === 'ssh'" label="连接测试">
              <div class="test-connection-row">
                <el-button :loading="testingConnection" @click="testConnection">测试连接</el-button>
                <div v-if="accountTestResult" class="test-connection-result" aria-live="polite">
                  <el-tag :type="accountTestResult.ok ? 'success' : 'danger'" size="small">
                    {{ accountTestResult.ok ? '可达' : '不可达' }}
                  </el-tag>
                  <span v-if="accountTestResult.latency_ms !== undefined" class="test-connection-meta">
                    延迟 {{ accountTestResult.latency_ms }}ms
                  </span>
                  <span v-if="accountTestResult.error" class="test-connection-error">
                    {{ accountTestResult.error }}
                  </span>
                </div>
              </div>
            </el-form-item>
            <template v-if="selectedHostProtocol === 'rdp'">
              <el-form-item label="Windows 域">
                <el-input
                  v-model="accountForm.domain"
                  clearable
                  placeholder="可选，例如 CORP"
                />
              </el-form-item>
              <el-form-item label="安全模式">
                <el-select v-model="accountForm.rdp_security" style="width: 100%">
                  <el-option label="自动协商（推荐）" value="any" />
                  <el-option label="NLA" value="nla" />
                  <el-option label="TLS" value="tls" />
                  <el-option label="RDP 原生加密" value="rdp" />
                </el-select>
              </el-form-item>
              <el-form-item label="忽略证书">
                <el-switch v-model="accountForm.rdp_ignore_certificate" />
                <span class="inline-help">仅建议在受控测试环境中开启</span>
              </el-form-item>
              <el-form-item v-if="!accountForm.rdp_ignore_certificate" label="证书指纹">
                <el-input
                  v-model="accountForm.rdp_cert_fingerprints"
                  clearable
                  placeholder="可选；多个 SHA-256 指纹用逗号分隔"
                />
              </el-form-item>
            </template>
          </div>

          <div class="form-section">
            <div class="form-section-title">访问控制</div>
            <el-form-item label="有效期">
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
                    style="width: 100%"
                  />
                  <el-button @click="setPermanentExpiry">永久</el-button>
                </div>
              </div>
            </el-form-item>
            <template v-if="selectedHostProtocol === 'rdp'">
              <el-form-item label="通道权限">
                <div class="rdp-policy-grid">
                  <label>
                    <span>读取远端剪贴板</span>
                    <el-switch v-model="accountForm.rdp_clipboard_read" />
                  </label>
                  <label>
                    <span>写入远端剪贴板</span>
                    <el-switch v-model="accountForm.rdp_clipboard_write" />
                  </label>
                  <label>
                    <span>上传文件</span>
                    <el-switch
                      v-model="accountForm.rdp_file_upload"
                      @change="handleRDPFilePolicyChange"
                    />
                  </label>
                  <label>
                    <span>下载文件</span>
                    <el-switch
                      v-model="accountForm.rdp_file_download"
                      @change="handleRDPFilePolicyChange"
                    />
                  </label>
                  <label>
                    <span>映射堡垒机磁盘</span>
                    <el-switch
                      v-model="accountForm.rdp_drive_mapping"
                      @change="handleRDPDrivePolicyChange"
                    />
                  </label>
                </div>
                <span class="inline-help">文件上传和下载依赖磁盘映射，但仍分别校验权限</span>
              </el-form-item>
            </template>
          </div>

          <el-collapse v-model="accountMorePanels" class="more-collapse">
            <el-collapse-item title="更多设置" name="more">
              <el-form-item label="账号分组">
                <el-select
                  v-model="accountForm.group"
                  allow-create
                  clearable
                  default-first-option
                  filterable
                  placeholder="选择或输入账号分组"
                >
                  <el-option
                    v-for="g in accountGroupOptions"
                    :key="g"
                    :label="g"
                    :value="g"
                  />
                </el-select>
              </el-form-item>
              <el-form-item label="备注">
                <el-input
                  v-model="accountForm.remark"
                  :autosize="{ minRows: 3, maxRows: 5 }"
                  type="textarea"
                />
              </el-form-item>
            </el-collapse-item>
          </el-collapse>
        </el-form>
      </FormDialog>

      <ConnectionConfigDialog
        v-model="connectionDialogVisible"
        resource-type="host"
        :target="selectedConnectionTarget"
        :resource-name="connectionResourceName"
        :source-address="connectionSourceAddress"
        :source-account="connectionSourceAccount"
        :allow-ssh="permission.canDo('session:connect')"
        :allow-sftp="permission.canDo('sftp:connect')"
        @host-identity-changed="handleHostIdentityChanged"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import {
  computed,
  nextTick,
  onMounted,
  reactive,
  ref,
  shallowRef,
  watch,
} from "vue";
import {
  ElMessage,
  ElMessageBox,
  type FormInstance,
  type FormRules,
} from "element-plus";
import { ArrowDown, Refresh } from "@element-plus/icons-vue";
import { useRouter } from "vue-router";
import DataTableCard from "@/components/DataTableCard.vue";
import ResourceFilterBar from "@/components/ResourceFilterBar.vue";
import FormDialog from "@/components/FormDialog.vue";
import ConnectionConfigDialog from "@/components/ConnectionConfigDialog.vue";
import StatusSwitch from "@/components/StatusSwitch.vue";
import {
  apiClient,
  type HostPayload,
  type HostView,
  type PageResponse,
  type TargetPayload,
  type TargetRecord,
} from "@/api/client";
import { useI18n } from "@/i18n";
import { usePermissionStore } from "@/stores/permission";
import {
  parseSSHHostIdentityIssue,
  sshHostIdentityNotice,
} from "@/utils/sshHostIdentity";

type AuthMethod = "password" | "private_key";
type HostProtocol = "ssh" | "rdp";
type RDPSecurity = "any" | "nla" | "tls" | "rdp";

const UNGROUPED_HOST_FILTER = "__ungrouped__";

interface HostForm {
  id: string;
  name: string;
  group: string;
  address: string;
  port: number;
  protocol: HostProtocol;
  remark: string;
}

interface AccountForm {
  id: string;
  group: string;
  remark: string;
  disabled: boolean;
  expires_at: string;
  username: string;
  domain: string;
  auth_method: AuthMethod;
  password: string;
  private_key_pem: string;
  passphrase: string;
  rdp_security: RDPSecurity;
  rdp_ignore_certificate: boolean;
  rdp_cert_fingerprints: string;
  rdp_clipboard_read: boolean;
  rdp_clipboard_write: boolean;
  rdp_file_upload: boolean;
  rdp_file_download: boolean;
  rdp_drive_mapping: boolean;
}

const { t } = useI18n();
const permission = usePermissionStore();
const router = useRouter();

// ── Host list state ──
const hosts = ref<HostView[]>([]);
const hostTotal = ref(0);
const hostPage = ref(1);
const hostPageSize = ref(20);
const keyword = ref("");
const hostGroupFilter = shallowRef("all");
const hostsLoading = ref(false);
const hostError = ref("");
let hostRequestSequence = 0;

// ── Account list state ──
const selectedHost = ref<HostView | null>(null);
const accounts = ref<TargetRecord[]>([]);
const accountTotal = ref(0);
const accountPage = ref(1);
const accountPageSize = ref(50);
const accountsLoading = ref(false);
const accountError = ref("");
const accountsConnectableOnly = shallowRef(false);
let accountRequestSequence = 0;

// ── Dialog visibility ──
const hostDialogVisible = ref(false);
const accountFormVisible = ref(false);
const accountsDialogVisible = ref(false);
const connectionDialogVisible = ref(false);

// ── Editing state ──
const editingHostId = ref<string | null>(null);
const editingAccountId = ref<string | null>(null);
const submittingHost = ref(false);
const submittingAccount = ref(false);
const testingConnection = ref(false);
const accountTestResult = ref<{
  ok: boolean;
  error?: string;
  latency_ms?: number;
} | null>(null);
const deletingId = ref("");
const statusUpdatingId = ref("");
const hostNameTouched = ref(false);
const hostMorePanels = ref<string[]>([]);
const accountMorePanels = ref<string[]>([]);
const accountDetailLoading = ref(false);
let accountDetailRequestSequence = 0;

// ── Connection state ──
const selectedConnectionTarget = ref<TargetRecord | null>(null);
const connectionResourceName = computed(() => selectedHost.value ? hostName(selectedHost.value) : accountDisplayName(selectedConnectionTarget.value ?? {}));
const connectionSourceAddress = computed(() => selectedHost.value ? hostEndpoint(selectedHost.value) : targetHostString(selectedConnectionTarget.value ?? {}));
const connectionSourceAccount = computed(() => stringFrom(selectedConnectionTarget.value?.username));

const hostFormRef = ref<FormInstance>();
const accountFormRef = ref<FormInstance>();
const privateKeyFileInputRef = ref<HTMLInputElement>();
const privateKeyFileName = ref("");

// ── Forms ──
const hostForm = reactive<HostForm>(emptyHostForm());
const accountForm = reactive<AccountForm>(emptyAccountForm());

const accountExpiryShortcuts = [
  { text: "8小时", value: () => expiryAfter({ hours: 8 }) },
  { text: "7天", value: () => expiryAfter({ days: 7 }) },
  { text: "1年", value: () => expiryAfter({ years: 1 }) },
];

// ── Computed ──
const accountsDialogTitle = computed(() => {
  const host = selectedHost.value;
  return host ? `${hostName(host)} - 账号` : "主机账号";
});
const secretPlaceholder = computed(() =>
  editingAccountId.value
    ? t("hosts.placeholder.keepSecret")
    : t("hosts.placeholder.optional"),
);
const credentialPlaceholder = computed(() =>
  editingAccountId.value
    ? t("hosts.placeholder.keepSecret")
    : t("hosts.placeholder.required"),
);
const privateKeyPEMPlaceholder = computed(() =>
  editingAccountId.value
    ? t("hosts.placeholder.keepSecret")
    : "选择本地私钥文件自动读取，或粘贴 -----BEGIN OPENSSH PRIVATE KEY----- 开头的内容",
);
const hostGroupOptions = ref<string[]>([]);
const accountGroupOptions = ref<string[]>([]);

const hostQuickGroupOptions = computed(() => {
  const groups = new Set<string>();
  for (const group of hostGroupOptions.value) {
    const normalized = group.trim();
    if (normalized) groups.add(normalized);
  }
  for (const host of hosts.value) {
    const normalized = stringFrom(host.group).trim();
    if (normalized) groups.add(normalized);
  }
  return [
    { label: "未分组", value: UNGROUPED_HOST_FILTER },
    ...Array.from(groups)
      .sort((a, b) => a.localeCompare(b, "zh-CN"))
      .map((group) => ({ label: group, value: group })),
  ];
});
const editingHostIdentity = computed(() => {
  if (!editingHostId.value) return null;
  return hosts.value.find((host) => hostId(host) === editingHostId.value) ?? null;
});

async function loadGroupOptions() {
  const [resourceGroups, accountGroups] = await Promise.allSettled([
    apiClient.getResourceGroups({ group_type: "resource", page_size: 200 }),
    apiClient.getResourceGroups({ group_type: "account", page_size: 200 }),
  ]);
  if (resourceGroups.status === "fulfilled") {
    hostGroupOptions.value = (resourceGroups.value.items ?? [])
      .map((group) => group.name)
      .filter(Boolean);
  }
  if (accountGroups.status === "fulfilled") {
    accountGroupOptions.value = (accountGroups.value.items ?? [])
      .map((group) => group.name)
      .filter(Boolean);
  }
}
const selectedHostProtocol = computed<HostProtocol>(() =>
  selectedHost.value ? hostProtocol(selectedHost.value) : hostForm.protocol
);
const hostRules: FormRules<HostForm> = {
  protocol: [{ required: true, message: "请选择连接协议", trigger: "change" }],
  address: [{ required: true, message: "请输入主机地址", trigger: "blur" }],
  port: [
    { required: true, message: "请输入端口", trigger: "change" },
    {
      type: "number",
      min: 1,
      max: 65535,
      message: "端口范围 1-65535",
      trigger: "change",
    },
  ],
};
const accountRules: FormRules<AccountForm> = {
  username: [
    { required: true, message: t("hosts.required.username"), trigger: "blur" },
  ],
  auth_method: [
    {
      required: true,
      message: t("hosts.required.authMethod"),
      trigger: "change",
    },
  ],
  password: [{ validator: validatePassword, trigger: "blur" }],
  private_key_pem: [{ validator: validatePrivateKeyPEM, trigger: "blur" }],
};

// ════════════════════════════════════════════════════════════════
// Helpers
// ════════════════════════════════════════════════════════════════

function numberFrom(value: unknown, fallback: number): number {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
}

function stringFrom(value: unknown): string {
  return typeof value === "string" || typeof value === "number"
    ? String(value)
    : "";
}

function hasValue(value: unknown): boolean {
  return String(value ?? "").trim().length > 0;
}


function hostId(host: HostView): string {
  return stringFrom(host.id);
}

function hostEndpoint(host: HostView): string {
  const address = stringFrom(host.address);
  const port = numberFrom(host.port, defaultPort(hostProtocol(host)));
  return address ? `${address}:${port}` : "-";
}

function hostProtocol(host: HostView): HostProtocol {
  return String(host.protocol || "ssh").toLowerCase() === "rdp" ? "rdp" : "ssh";
}

function targetProtocol(target: TargetRecord): HostProtocol {
  const protocol = stringFrom(target.protocol).toLowerCase();
  if (protocol === "rdp") return "rdp";
  if (protocol === "ssh") return "ssh";
  return selectedHost.value ? hostProtocol(selectedHost.value) : "ssh";
}

function defaultPort(protocol: HostProtocol): number {
  return protocol === "rdp" ? 3389 : 22;
}

function canConnectHost(host: HostView): boolean {
  if (!hostEnabled(host)) return false;
  return hostProtocol(host) === "rdp"
    ? permission.canDo("rdp:connect")
    : permission.canDo("session:connect");
}

function canConnectTarget(target: TargetRecord): boolean {
  if (!targetEnabled(target)) return false;
  const expiresAt = stringFrom(target.expires_at).trim();
  if (expiresAt) {
    const expiry = Date.parse(expiresAt);
    if (!Number.isNaN(expiry) && expiry <= Date.now()) return false;
  }
  return targetProtocol(target) === "rdp"
    ? permission.canDo("rdp:connect")
    : permission.canDo("session:connect");
}

function canViewHostAudit(host: HostView): boolean {
  return hostProtocol(host) === "rdp"
    ? permission.canDo("rdp:recording:view")
    : permission.canDo("audit:view");
}

function hostName(host: HostView): string {
  return stringFrom(host.name).trim() || stringFrom(host.address).trim() || "-";
}

function hostStatusKey(host: HostView): string {
  return `host:${hostId(host)}`;
}

function hostEnabled(host: HostView): boolean {
  return stringFrom(host.status).trim().toLowerCase() !== "disabled";
}

function targetId(target: TargetRecord): string {
  return String(target.id ?? "");
}

function accountStatusKey(target: TargetRecord): string {
  return `account:${targetId(target)}`;
}

function targetEnabled(target: TargetRecord): boolean {
  const status = stringFrom(target.status).trim().toLowerCase();
  if (status) return status === "active" || status === "enabled";
  return target.disabled !== true;
}

function targetRemark(target: TargetRecord): string {
  return stringFrom(target.remark).trim();
}

function accountDisplayName(target: TargetRecord): string {
  return (
    stringFrom(target.username).trim() ||
    stringFrom(target.name).trim() ||
    targetId(target) ||
    "-"
  );
}

function expiresAtText(target: TargetRecord): string {
  const expiresAt = stringFrom(target.expires_at).trim();
  return expiresAt ? formatDateTime(expiresAt) : "永久";
}

function formatDateTime(value: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function expiryAfter(offset: {
  hours?: number;
  days?: number;
  years?: number;
}): Date {
  const date = new Date();
  if (offset.hours) date.setHours(date.getHours() + offset.hours);
  if (offset.days) date.setDate(date.getDate() + offset.days);
  if (offset.years) date.setFullYear(date.getFullYear() + offset.years);
  return date;
}

function setPermanentExpiry() {
  accountForm.expires_at = "";
}

function targetHostString(target: TargetRecord): string {
  const addr = stringFrom(target.address).trim();
  if (addr) return addr;
  const host = stringFrom(target.host).trim();
  if (host) return host;
  const addr2 = stringFrom(target.address).trim();
  const port = numberFrom(target.port, defaultPort(targetProtocol(target)));
  const portSuffix = `:${port}`;
  return addr2.endsWith(portSuffix)
    ? addr2.slice(0, -portSuffix.length)
    : addr2;
}


function isAuthMethod(value: unknown): value is AuthMethod {
  return value === "password" || value === "private_key";
}

function isKeyAuthMethod(method: AuthMethod): boolean {
  return method === "private_key";
}

function targetAuthMethods(target: TargetRecord): AuthMethod[] {
  const rawMethods = Array.isArray(target.auth_methods)
    ? target.auth_methods
    : [];
  const methods = new Set<AuthMethod>();
  for (const method of rawMethods) {
    if (method === "password") methods.add("password");
    else if (
      method === "private_key" ||
      method === "private_key_path" ||
      method === "private_key_pem"
    )
      methods.add("private_key");
  }
  const authType = target.auth_type;
  if (isAuthMethod(authType)) methods.add(authType);
  else if (authType === "private_key_path" || authType === "private_key_pem")
    methods.add("private_key");
  if (target.password) methods.add("password");
  if (target.private_key_path || target.private_key_pem)
    methods.add("private_key");
  return [...methods];
}

function inferAuthMethod(target: TargetRecord): AuthMethod {
  return targetAuthMethods(target)[0] ?? "password";
}

function authMethodLabel(method: AuthMethod): string {
  switch (method) {
    case "password":
      return t("hosts.auth.password");
    case "private_key":
      return t("hosts.auth.privateKey");
  }
}

function sanitizeID(value: string): string {
  const sanitized = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return sanitized || `account-${Date.now()}`;
}

function generatedAccountID(host: HostView, username: string): string {
  return sanitizeID(
    `${hostId(host) || stringFrom(host.address)}-${username || "account"}`,
  );
}

function parseAddressPort(value: string): { address: string; port?: number } {
  const trimmed = value.trim();
  if (!trimmed) return { address: "" };
  const bracketed = trimmed.match(/^\[([^\]]+)]:(\d+)$/);
  if (bracketed) {
    return { address: bracketed[1].trim(), port: validPort(bracketed[2]) };
  }
  const colonCount = (trimmed.match(/:/g) ?? []).length;
  if (colonCount !== 1) return { address: trimmed };
  const [addr, portText] = trimmed.split(":");
  const port = validPort(portText);
  return addr.trim() && port
    ? { address: addr.trim(), port }
    : { address: trimmed };
}

function validPort(value: string): number | undefined {
  const port = Number(value);
  return Number.isInteger(port) && port >= 1 && port <= 65535
    ? port
    : undefined;
}

function formatAddressPort(addr: string, port: number): string {
  const address = addr.trim();
  if (!address) return "";
  const displayHost =
    address.includes(":") && !address.startsWith("[")
      ? `[${address}]`
      : address;
  return `${displayHost}:${numberFrom(port, defaultPort(hostForm.protocol))}`;
}

function defaultHostName(): string {
  return formatAddressPort(hostForm.address, Number(hostForm.port));
}

function syncDefaultHostName() {
  if (!hostNameTouched.value) {
    hostForm.name = defaultHostName();
  }
}

function normalizeHostAddressInput() {
  const parsed = parseAddressPort(hostForm.address);
  hostForm.address = parsed.address;
  if (parsed.port) {
    hostForm.port = parsed.port;
  }
  syncDefaultHostName();
}

function handleHostProtocolChange(protocol: HostProtocol) {
  const previousDefault = protocol === "rdp" ? 22 : 3389;
  if (!hostForm.port || hostForm.port === previousDefault) {
    hostForm.port = defaultPort(protocol);
  }
  syncDefaultHostName();
}

// ════════════════════════════════════════════════════════════════
// Form factories
// ════════════════════════════════════════════════════════════════

function emptyHostForm(): HostForm {
  return {
    id: "",
    name: "",
    group: "",
    address: "",
    port: 22,
    protocol: "ssh",
    remark: "",
  };
}

function emptyAccountForm(): AccountForm {
  return {
    id: "",
    group: "",
    remark: "",
    disabled: false,
    expires_at: "",
    username: "",
    domain: "",
    auth_method: "password",
    password: "",
    private_key_pem: "",
    passphrase: "",
    rdp_security: "any",
    rdp_ignore_certificate: false,
    rdp_cert_fingerprints: "",
    rdp_clipboard_read: false,
    rdp_clipboard_write: false,
    rdp_file_upload: false,
    rdp_file_download: false,
    rdp_drive_mapping: false,
  };
}

function resetHostForm(values: HostForm = emptyHostForm()) {
  Object.assign(hostForm, values);
}

function resetAccountForm(values: AccountForm = emptyAccountForm()) {
  Object.assign(accountForm, values);
  privateKeyFileName.value = "";
  if (privateKeyFileInputRef.value) {
    privateKeyFileInputRef.value.value = "";
  }
}

function recordToHostForm(host: HostView): HostForm {
  const protocol = hostProtocol(host);
  return {
    id: hostId(host),
    name: stringFrom(host.name),
    group: stringFrom(host.group),
    address: stringFrom(host.address),
    port: numberFrom(host.port, defaultPort(protocol)),
    protocol,
    remark: stringFrom(host.remark),
  };
}

function recordToAccountForm(target: TargetRecord): AccountForm {
  return {
    id: targetId(target),
    group: stringFrom(target.group),
    remark: stringFrom(target.remark),
    disabled: !targetEnabled(target),
    expires_at: stringFrom(target.expires_at),
    username: stringFrom(target.username),
    domain: stringFrom(target.domain),
    auth_method: targetProtocol(target) === "rdp" ? "password" : inferAuthMethod(target),
    password: "",
    private_key_pem: "",
    passphrase: "",
    rdp_security: normalizeRDPSecurity(target.rdp_security),
    rdp_ignore_certificate: target.rdp_ignore_certificate === true,
    rdp_cert_fingerprints: stringFrom(target.rdp_cert_fingerprints),
    rdp_clipboard_read: target.rdp_clipboard_read === true,
    rdp_clipboard_write: target.rdp_clipboard_write === true,
    rdp_file_upload: target.rdp_file_upload === true,
    rdp_file_download: target.rdp_file_download === true,
    rdp_drive_mapping: target.rdp_drive_mapping === true,
  };
}

function normalizeRDPSecurity(value: unknown): RDPSecurity {
  const security = String(value || "").toLowerCase();
  return security === "nla" || security === "tls" || security === "rdp"
    ? security
    : "any";
}

// ════════════════════════════════════════════════════════════════
// Payload builders
// ════════════════════════════════════════════════════════════════

function buildHostPayload(): HostPayload {
  normalizeHostAddressInput();
  const defaultName = defaultHostName();
  return {
    id: hostForm.id || undefined,
    name: hostForm.name.trim() || defaultName,
    group: hostForm.group.trim() || undefined,
    address: hostForm.address.trim(),
    port: Number(hostForm.port),
    protocol: hostForm.protocol,
    remark: hostForm.remark.trim() || undefined,
  };
}

function hostPayloadFromRecord(host: HostView): HostPayload {
  const protocol = hostProtocol(host);
  return {
    id: hostId(host) || undefined,
    name: hostName(host),
    group: stringFrom(host.group).trim() || undefined,
    address: stringFrom(host.address).trim(),
    port: numberFrom(host.port, defaultPort(protocol)),
    protocol,
    remark: stringFrom(host.remark).trim() || undefined,
  };
}

function buildAccountPayload(): TargetPayload {
  const host = selectedHost.value;
  const username = accountForm.username.trim();
  const protocol = selectedHostProtocol.value;
  const payload: TargetPayload = {
    id:
      accountForm.id.trim() ||
      (host ? generatedAccountID(host, username) : sanitizeID(username)),
    host_id: host ? hostId(host) : undefined,
    name: username,
    group: accountForm.group.trim() || undefined,
    remark: accountForm.remark.trim() || undefined,
    disabled: accountForm.disabled,
    expires_at: accountForm.expires_at || undefined,
    host: stringFrom(host?.address).trim(),
    port: numberFrom(host?.port, defaultPort(protocol)),
    protocol,
    username,
    domain: protocol === "rdp" ? accountForm.domain.trim() : "",
    password: "",
    private_key_path: "",
    private_key_pem: "",
    passphrase: "",
    insecure_ignore_host_key: false,
    host_key_fingerprint: "",
    known_hosts_path: "",
    rdp_security: protocol === "rdp" ? accountForm.rdp_security : "any",
    rdp_ignore_certificate:
      protocol === "rdp" && accountForm.rdp_ignore_certificate,
    rdp_cert_fingerprints:
      protocol === "rdp" && !accountForm.rdp_ignore_certificate
        ? accountForm.rdp_cert_fingerprints.trim()
        : "",
    rdp_clipboard_read: protocol === "rdp" && accountForm.rdp_clipboard_read,
    rdp_clipboard_write: protocol === "rdp" && accountForm.rdp_clipboard_write,
    rdp_file_upload: protocol === "rdp" && accountForm.rdp_file_upload,
    rdp_file_download: protocol === "rdp" && accountForm.rdp_file_download,
    rdp_drive_mapping: protocol === "rdp" && accountForm.rdp_drive_mapping,
  };
  if (protocol === "rdp" || accountForm.auth_method === "password") {
    payload.password = accountForm.password;
  } else {
    if (hasValue(accountForm.private_key_pem)) {
      payload.private_key_pem = accountForm.private_key_pem;
      payload.passphrase = accountForm.passphrase;
    }
  }
  return payload;
}

function targetStatusPayload(
  target: TargetRecord,
  disabled: boolean,
): TargetPayload {
  const protocol = targetProtocol(target);
  const username = stringFrom(target.username).trim();
  return {
    id: targetId(target),
    host_id: stringFrom(target.host_id).trim() || undefined,
    name: username || targetId(target),
    group: stringFrom(target.group).trim() || undefined,
    remark: stringFrom(target.remark).trim() || undefined,
    disabled,
    expires_at: stringFrom(target.expires_at).trim() || undefined,
    host: targetHostString(target),
    port: numberFrom(target.port, defaultPort(protocol)),
    protocol,
    username,
    domain: stringFrom(target.domain).trim(),
    password: "",
    private_key_path: "",
    private_key_pem: "",
    passphrase: "",
    insecure_ignore_host_key: false,
    host_key_fingerprint: "",
    known_hosts_path: "",
    rdp_security: normalizeRDPSecurity(target.rdp_security),
    rdp_ignore_certificate: target.rdp_ignore_certificate === true,
    rdp_cert_fingerprints: stringFrom(target.rdp_cert_fingerprints).trim(),
    rdp_clipboard_read: target.rdp_clipboard_read === true,
    rdp_clipboard_write: target.rdp_clipboard_write === true,
    rdp_file_upload: target.rdp_file_upload === true,
    rdp_file_download: target.rdp_file_download === true,
    rdp_drive_mapping: target.rdp_drive_mapping === true,
  };
}

function selectedCredentialValue(): string {
  if (
    selectedHostProtocol.value === "rdp"
    || accountForm.auth_method === "password"
  ) return accountForm.password;
  return accountForm.private_key_pem;
}

// ════════════════════════════════════════════════════════════════
// Validators
// ════════════════════════════════════════════════════════════════

function validatePassword(
  _rule: unknown,
  value: unknown,
  callback: (error?: Error) => void,
) {
  if (
    !editingAccountId.value &&
    (selectedHostProtocol.value === "rdp" || accountForm.auth_method === "password") &&
    !hasValue(value)
  ) {
    callback(new Error(t("hosts.required.password")));
    return;
  }
  callback();
}

function validatePrivateKeyPEM(
  _rule: unknown,
  value: unknown,
  callback: (error?: Error) => void,
) {
  if (
    !editingAccountId.value &&
    selectedHostProtocol.value === "ssh" &&
    accountForm.auth_method === "private_key" &&
    !hasValue(value)
  ) {
    callback(new Error(t("hosts.required.privateKeyPem")));
    return;
  }
  callback();
}

function handleAuthMethodChange() {
  accountFormRef.value?.clearValidate([
    "password",
    "private_key_pem",
    "passphrase",
  ]);
}

function handleRDPFilePolicyChange(enabled: boolean | string | number) {
  if (enabled === true) {
    accountForm.rdp_drive_mapping = true;
  }
}

function handleRDPDrivePolicyChange(enabled: boolean | string | number) {
  if (enabled !== true) {
    accountForm.rdp_file_upload = false;
    accountForm.rdp_file_download = false;
  }
}

function triggerPrivateKeyFileSelect() {
  privateKeyFileInputRef.value?.click();
}

async function handlePrivateKeyFileChange(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  if (file.size > 1024 * 1024) {
    ElMessage.warning("私钥文件过大，请选择 1MB 以内的文本私钥文件");
    return;
  }
  try {
    const text = await file.text();
    if (!hasValue(text)) {
      ElMessage.warning("私钥文件内容为空");
      return;
    }
    accountForm.private_key_pem = text;
    privateKeyFileName.value = file.name;
    accountFormRef.value?.clearValidate(["private_key_pem"]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "读取私钥文件失败");
  }
}

// ════════════════════════════════════════════════════════════════
// Data fetching
// ════════════════════════════════════════════════════════════════

async function fetchHosts() {
  const requestSequence = ++hostRequestSequence;
  const groupFilter = hostGroupFilter.value;
  const query = keyword.value.trim() || undefined;
  hostsLoading.value = true;
  hostError.value = "";
  try {
    const response = await apiClient.getHosts({
      page: hostPage.value,
      page_size: hostPageSize.value,
      q: query,
      group:
        groupFilter !== "all" && groupFilter !== UNGROUPED_HOST_FILTER
          ? groupFilter
          : undefined,
      ungrouped:
        groupFilter === UNGROUPED_HOST_FILTER ? true : undefined,
    });
    if (requestSequence !== hostRequestSequence) return;
    hosts.value = response.items ?? [];
    hostTotal.value = response.total ?? 0;
  } catch (err) {
    if (requestSequence !== hostRequestSequence) return;
    hostError.value =
      err instanceof Error ? err.message : t("hosts.error.loadList");
  } finally {
    if (requestSequence === hostRequestSequence) {
      hostsLoading.value = false;
    }
  }
}

function refreshHostsFromFirstPage() {
  const pageChanged = hostPage.value !== 1;
  hostPage.value = 1;
  if (!pageChanged) {
    void fetchHosts();
  }
}

function onHostSearch(q: string) {
  keyword.value = q;
  refreshHostsFromFirstPage();
}

function setHostGroupFilter(value: string) {
  if (hostGroupFilter.value === value) return;
  hostGroupFilter.value = value;
  refreshHostsFromFirstPage();
}

async function loadSelectedHostAccounts() {
  const host = selectedHost.value;
  const id = host ? hostId(host) : "";
  if (!id) return;
  const requestSequence = ++accountRequestSequence;
  const requestedPage = accountPage.value;
  const requestedPageSize = accountPageSize.value;
  const requestedConnectableOnly = accountsConnectableOnly.value;
  const isCurrentRequest = () =>
    requestSequence === accountRequestSequence
    && (selectedHost.value ? hostId(selectedHost.value) : "") === id
    && accountPage.value === requestedPage
    && accountPageSize.value === requestedPageSize
    && accountsConnectableOnly.value === requestedConnectableOnly;
  accountsLoading.value = true;
  accountError.value = "";
  try {
    const res: PageResponse<TargetRecord> = await apiClient.getHostAccounts(
      id,
      {
        page: requestedPage,
        page_size: requestedPageSize,
        connectable: requestedConnectableOnly || undefined,
      },
    );
    if (!isCurrentRequest()) return;
    accounts.value = res.items ?? [];
    accountTotal.value = res.total ?? 0;
  } catch (err) {
    if (!isCurrentRequest()) return;
    accounts.value = [];
    accountError.value =
      err instanceof Error ? err.message : t("hosts.error.loadList");
  } finally {
    if (isCurrentRequest()) {
      accountsLoading.value = false;
    }
  }
}

// ════════════════════════════════════════════════════════════════
// Host CRUD
// ════════════════════════════════════════════════════════════════

function setSelectedHost(host: HostView) {
  const previousId = selectedHost.value ? hostId(selectedHost.value) : "";
  const nextId = hostId(host);
  if (previousId !== nextId) {
    accounts.value = [];
    accountError.value = "";
    accountPage.value = 1;
  }
  selectedHost.value = host;
}

async function openCreateHostDialog() {
  editingHostId.value = null;
  hostNameTouched.value = false;
  hostMorePanels.value = ["more"];
  resetHostForm();
  hostDialogVisible.value = true;
  await nextTick();
  hostFormRef.value?.clearValidate();
}

async function openEditHostDialog(host: HostView) {
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
  if (!valid) return;
  submittingHost.value = true;
  try {
    const id = editingHostId.value;
    const payload = buildHostPayload();
    if (id) {
      await apiClient.updateHost(id, payload);
      ElMessage.success("主机已更新");
    } else {
      const created = await apiClient.createHost(payload);
      if (created.status === "disabled" && created.identity_status === "unavailable") {
        ElMessage.warning("主机已保存但 SSH 身份采集失败，当前保持停用；请确认地址可达后重新启用");
      } else {
        ElMessage.success("主机已创建");
      }
    }
    hostDialogVisible.value = false;
    await Promise.all([fetchHosts(), loadGroupOptions()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t("hosts.error.save"));
  } finally {
    submittingHost.value = false;
  }
}

async function toggleHostStatus(host: HostView, active: boolean) {
  const id = hostId(host);
  if (!id) return;
  const newStatus = active ? "active" : "disabled";
  statusUpdatingId.value = hostStatusKey(host);
  try {
    await apiClient.updateHost(id, {
      ...hostPayloadFromRecord(host),
      status: newStatus,
    });
    ElMessage.success(active ? "主机已启用" : "主机已禁用");
    await fetchHosts();
  } catch (err) {
    if (!(await showSSHHostIdentityIssue(err))) {
      ElMessage.error(err instanceof Error ? err.message : t("hosts.error.save"));
    }
  } finally {
    statusUpdatingId.value = "";
  }
}

async function confirmDeleteHost(host: HostView) {
  const id = hostId(host);
  if (!id) return;
  try {
    await ElMessageBox.confirm(
      `确认删除主机"${hostName(host)}"？该主机下运行时账号也会删除。`,
      "删除主机",
      {
        cancelButtonText: "取消",
        confirmButtonText: "删除",
        type: "warning",
      },
    );
  } catch {
    return;
  }
  deletingId.value = id;
  try {
    await apiClient.deleteHost(id);
    ElMessage.success("主机已删除");
    await fetchHosts();
  } catch (err) {
    ElMessage.error(
      err instanceof Error ? err.message : t("hosts.error.delete"),
    );
  } finally {
    deletingId.value = "";
  }
}

// ════════════════════════════════════════════════════════════════
// Account CRUD
// ════════════════════════════════════════════════════════════════

async function openAccountsDialog(host: HostView) {
  accountsConnectableOnly.value = false;
  setSelectedHost(host);
  accountPage.value = 1;
  accountsDialogVisible.value = true;
  await loadSelectedHostAccounts();
}

async function openCreateAccountDialog(host: HostView) {
  accountDetailRequestSequence += 1;
  accountDetailLoading.value = false;
  setSelectedHost(host);
  editingAccountId.value = null;
  accountMorePanels.value = ["more"];
  accountTestResult.value = null;
  resetAccountForm();
  accountFormVisible.value = true;
  await nextTick();
  accountFormRef.value?.clearValidate();
}

async function openEditAccountDialog(target: TargetRecord) {
  const id = targetId(target);
  if (!id) {
    ElMessage.error(t("hosts.error.missingId"));
    return;
  }
  const requestSequence = ++accountDetailRequestSequence;
  const isCurrentRequest = () =>
    requestSequence === accountDetailRequestSequence
    && editingAccountId.value === id
    && accountFormVisible.value;
  editingAccountId.value = id;
  accountMorePanels.value = [];
  accountTestResult.value = null;
  resetAccountForm(recordToAccountForm(target));
  accountFormVisible.value = true;
  accountDetailLoading.value = true;
  await nextTick();
  if (!isCurrentRequest()) return;
  accountFormRef.value?.clearValidate();
  try {
    const detail = await apiClient.getTarget(id);
    if (!isCurrentRequest()) return;
    resetAccountForm(recordToAccountForm(detail));
  } catch (err) {
    if (!isCurrentRequest()) return;
    ElMessage.error(
      err instanceof Error ? err.message : t("hosts.error.loadDetail"),
    );
  } finally {
    if (isCurrentRequest()) {
      accountDetailLoading.value = false;
    }
  }
}

async function submitAccount() {
  const valid = await accountFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  if (!selectedHost.value) {
    ElMessage.error("请先选择主机");
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
      ElMessage.success("账号已更新");
    } else {
      await apiClient.createTarget(buildAccountPayload());
      ElMessage.success("账号已创建");
    }
    accountFormVisible.value = false;
    await Promise.all([fetchHosts(), loadSelectedHostAccounts(), loadGroupOptions()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t("hosts.error.save"));
  } finally {
    submittingAccount.value = false;
  }
}

async function testConnection() {
  if (!selectedHost.value) {
    ElMessage.error("请先选择主机");
    return;
  }
  const valid = await accountFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  const username = accountForm.username.trim();
  if (!username) {
    ElMessage.warning("请输入登录账号");
    return;
  }
  const authMethod = accountForm.auth_method;
  if (authMethod === "password" && !accountForm.password) {
    ElMessage.warning("请输入登录密码");
    return;
  }
  if (authMethod === "private_key" && !hasValue(accountForm.private_key_pem)) {
    ElMessage.warning("请提供私钥内容");
    return;
  }
  testingConnection.value = true;
  accountTestResult.value = null;
  try {
    const result = await apiClient.testTargetConnection(buildAccountPayload());
    accountTestResult.value = {
      ok: result.ok,
      latency_ms: result.latency_ms,
      error: result.ok ? undefined : (result.error || result.message || "连接失败"),
    };
  } catch (err) {
    const identityIssueHandled = await showSSHHostIdentityIssue(err);
    accountTestResult.value = {
      ok: false,
      error: identityIssueHandled
        ? "SSH 主机身份校验未通过"
        : err instanceof Error
          ? err.message
          : "连接测试失败",
    };
  } finally {
    testingConnection.value = false;
  }
}

async function showSSHHostIdentityIssue(error: unknown): Promise<boolean> {
  const issue = parseSSHHostIdentityIssue(error);
  if (!issue) return false;
  const notice = sshHostIdentityNotice(issue);
  await ElMessageBox.alert(notice.message, notice.title, {
    type: "warning",
    confirmButtonText: "知道了",
  }).catch(() => undefined);
  await fetchHosts();
  const selectedID = selectedHost.value ? hostId(selectedHost.value) : "";
  if (selectedID) {
    const refreshed = hosts.value.find((host) => hostId(host) === selectedID);
    if (refreshed) selectedHost.value = refreshed;
  }
  return true;
}

async function handleHostIdentityChanged() {
  await fetchHosts();
  const selectedID = selectedHost.value ? hostId(selectedHost.value) : "";
  if (!selectedID) return;
  const refreshed = hosts.value.find((host) => hostId(host) === selectedID);
  if (refreshed) selectedHost.value = refreshed;
}

async function toggleAccountStatus(target: TargetRecord, enabled: boolean) {
  const id = targetId(target);
  if (!id) {
    ElMessage.error(t("hosts.error.missingId"));
    return;
  }
  const disabled = !enabled;
  statusUpdatingId.value = accountStatusKey(target);
  try {
    await apiClient.updateTarget(id, targetStatusPayload(target, disabled));
    ElMessage.success(disabled ? "账号已禁用" : "账号已启用");
    await Promise.all([fetchHosts(), loadSelectedHostAccounts()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t("hosts.error.save"));
  } finally {
    statusUpdatingId.value = "";
  }
}

async function confirmDeleteAccount(target: TargetRecord) {
  const id = targetId(target);
  if (!id) {
    ElMessage.error(t("hosts.error.missingId"));
    return;
  }
  try {
    await ElMessageBox.confirm(
      `确认删除账号"${accountDisplayName(target)}"？`,
      "删除账号",
      {
        cancelButtonText: "取消",
        confirmButtonText: "删除",
        type: "warning",
      },
    );
  } catch {
    return;
  }
  deletingId.value = id;
  try {
    await apiClient.deleteTarget(id);
    ElMessage.success("账号已删除");
    await Promise.all([fetchHosts(), loadSelectedHostAccounts()]);
  } catch (err) {
    ElMessage.error(
      err instanceof Error ? err.message : t("hosts.error.delete"),
    );
  } finally {
    deletingId.value = "";
  }
}

// ════════════════════════════════════════════════════════════════
// Connection
// ════════════════════════════════════════════════════════════════

function openConnectionDialog(target: TargetRecord) {
  if (targetProtocol(target) === "rdp") {
    const id = targetId(target);
    if (!id) {
      ElMessage.error(t("hosts.error.missingId"));
      return;
    }
    void router.push({ path: "/web-rdp", query: { target_id: id } });
    return;
  }
  selectedConnectionTarget.value = target;
  connectionDialogVisible.value = true;
}

/** 从主机直接打开连接，单账号时直接弹连接窗，多账号时打开账号管理 */
async function handleHostConnect(host: HostView) {
  if (!hostEnabled(host)) {
    ElMessage.warning("该主机已停用，请重新启用并完成身份采集后再连接");
    return;
  }
  accountsConnectableOnly.value = false;
  setSelectedHost(host);
  accountPage.value = 1;
  const id = hostId(host);
  const requestSequence = ++accountRequestSequence;
  accountsLoading.value = true;
  accountError.value = "";
  try {
    const response = await apiClient.getHostAccounts(id, {
      page: 1,
      page_size: 2,
      connectable: true,
    });
    if (
      requestSequence !== accountRequestSequence
      || (selectedHost.value ? hostId(selectedHost.value) : "") !== id
    ) return;
    const previewAccounts = response.items ?? [];
    const count = response.total ?? previewAccounts.length;
    if (count === 0) {
      ElMessage.warning('该主机下无可用账号，请先新增账号');
      return;
    }
    if (count === 1) {
      const onlyAccount = previewAccounts[0];
      if (!onlyAccount) {
        ElMessage.error('可连接账号返回异常，请刷新后重试');
        return;
      }
      openConnectionDialog(onlyAccount);
      return;
    }
    accountsConnectableOnly.value = true;
    accounts.value = [];
    accountTotal.value = count;
    accountsDialogVisible.value = true;
    await loadSelectedHostAccounts();
    if (
      accountsDialogVisible.value
      && accountsConnectableOnly.value
      && (selectedHost.value ? hostId(selectedHost.value) : "") === id
    ) {
      ElMessage.info('请从账号列表中选择要连接的账号');
    }
  } catch (error) {
    if (requestSequence !== accountRequestSequence) return;
    accountError.value = error instanceof Error ? error.message : "可连接账号加载失败";
    ElMessage.error(accountError.value);
    return;
  } finally {
    if (requestSequence === accountRequestSequence) {
      accountsLoading.value = false;
    }
  }
}

/** More action - open filtered audit logs. */
function handleHostAuditLog(host: HostView) {
  void router.push({
    name: "audit",
    query: {
      scope: hostProtocol(host) === "rdp" ? "rdp" : "ssh",
      q: hostName(host),
    },
  });
}

/** More action - open filtered online sessions. */
function handleHostSessions(host: HostView) {
  void router.push({
    name: "audit",
    query: {
      scope: "online",
      resource_type: "host",
      resource_id: String(host.id ?? ""),
      q: hostName(host),
    },
  });
}

watch(
  () =>
    [
      hostForm.address,
      hostForm.port,
      hostDialogVisible.value,
      editingHostId.value,
    ] as const,
  () => {
    if (!hostDialogVisible.value || editingHostId.value) return;
    syncDefaultHostName();
  },
);

watch([hostPage, hostPageSize], () => {
  void fetchHosts();
});
watch([accountPage, accountPageSize], () => {
  if (accountsDialogVisible.value) void loadSelectedHostAccounts();
});
watch(accountsDialogVisible, (visible) => {
  if (visible) return;
  accountRequestSequence += 1;
  accountsLoading.value = false;
  accountsConnectableOnly.value = false;
});
watch(accountFormVisible, (visible) => {
  if (visible) return;
  accountDetailRequestSequence += 1;
  accountDetailLoading.value = false;
});

onMounted(() => {
  void fetchHosts();
  void loadGroupOptions();
});
</script>

<style scoped>
/* Table actions */
.table-actions {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  width: 100%;
}
.table-actions :deep(.el-button) {
  margin-left: 0;
}
.danger-dropdown-item {
  color: var(--el-color-danger);
}

/* 账号管理按钮 */
.account-mgmt-btn {
  font-size: 12px;
  padding: 0 2px;
}

.accounts-table {
  height: min(64dvh, 620px);
  min-height: 360px;
}

.test-connection-result {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

/* Form layout */
.form-section {
  margin-bottom: 16px;
}
.form-section + .form-section {
  padding-top: 16px;
  border-top: 1px solid var(--color-border);
}
.form-section-title {
  margin-bottom: 12px;
  color: var(--color-text);
  font-size: 13px;
  font-weight: 700;
  line-height: 1;
}

.host-key-note {
  margin-bottom: 18px;
}

/* Collapse */
.more-collapse {
  border-top: 1px solid var(--color-border);
  border-bottom: 0;
}
.more-collapse :deep(.el-collapse-item__header) {
  color: var(--color-text);
  font-size: 13px;
  font-weight: 700;
}
.more-collapse :deep(.el-collapse-item__wrap) {
  border-bottom: 0;
}

/* Test connection */
.test-connection-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.test-connection-meta {
  color: #667085;
  font-size: 12px;
}
.test-connection-error {
  color: var(--el-color-danger);
  font-size: 12px;
}

.inline-help {
  margin-left: 10px;
  color: var(--color-text-secondary);
  font-size: 12px;
}

.rdp-policy-grid {
  display: grid;
  width: 100%;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 14px;
}

.rdp-policy-grid label {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface-muted);
  color: var(--color-text-secondary);
  font-size: 12px;
}

/* Auth method radio */
.auth-method-group {
  display: flex;
  width: 100%;
}
.auth-method-group :deep(.el-radio-button) {
  flex: 1;
}
.auth-method-group :deep(.el-radio-button__inner) {
  width: 100%;
  padding-inline: 8px;
  white-space: nowrap;
}

/* Private key */
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
  color: var(--color-text-secondary);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.private-key-file-input {
  display: none;
}

.host-identity-value :deep(input),
.host-identity-value :deep(textarea) {
  font-family: var(--font-family-mono, "Cascadia Code", Consolas, monospace);
  font-size: 12px;
}

/* Expiry */
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
/* Connection */
/* 弹窗底部按钮间距 */
:deep(.el-dialog__footer .el-button + .el-button) {
  margin-left: 8px;
}
:deep(.el-dialog__footer .el-dropdown + .el-button) {
  margin-left: 8px;
}
:deep(.el-dialog__footer .el-button + .el-dropdown) {
  margin-left: 8px;
}
:deep(.el-dialog__footer .el-button:first-child) {
  margin-left: 0;
}

@media (max-width: 720px) {
  .accounts-table {
    height: min(66dvh, 520px);
    min-height: 280px;
  }

  .rdp-policy-grid {
    grid-template-columns: 1fr;
  }
}
</style>
