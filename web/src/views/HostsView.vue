<template>
  <div class="view-stack">
    <div class="page-container">
      <DataTableCard
        :data="hosts"
        :loading="hostsLoading"
        :total="hostTotal"
        v-model:page="hostPage"
        v-model:page-size="hostPageSize"
        search-placeholder="搜索主机名称、地址、分组..."
        @search="onHostSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="openCreateHostDialog"
            >新增主机</el-button
          >
        </template>
        <el-table-column label="主机名称" min-width="130" show-overflow-tooltip>
          <template #default="{ row }">{{ hostName(row) }}</template>
        </el-table-column>
        <el-table-column label="地址" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">{{ hostEndpoint(row) }}</template>
        </el-table-column>
        <el-table-column label="账号管理" min-width="110" align="center">
          <template #default="{ row }">
            <el-button link type="primary" size="small" class="account-mgmt-btn" @click="openAccountsDialog(row)">
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
              :model-value="row.status === 'active'"
              :loading="statusUpdatingId === hostStatusKey(row)"
              @update:model-value="(val: boolean) => toggleHostStatus(row, val)"
            />
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || "-" }}</template>
        </el-table-column>
        <el-table-column label="操作" width="210" align="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-button
                link
                type="success"
                size="small"
                @click="handleHostConnect(row)"
                >连接</el-button
              >
              <el-button
                link
                type="primary"
                size="small"
                @click="openEditHostDialog(row)"
                >编辑</el-button
              >
              <el-dropdown trigger="click" teleported>
                <el-button link type="primary" size="small"
                  >更多<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button
                >
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item @click="handleHostAuditLog(row)">审计日志</el-dropdown-item>
                    <el-dropdown-item @click="handleHostSessions(row)">在线会话</el-dropdown-item>
                    <el-dropdown-item @click="handleHostPermissions(row)">权限管理</el-dropdown-item>
                    <el-dropdown-item
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
        width="680px"
        :loading="submittingHost"
        @submit="submitHost"
      >
        <el-form
          ref="hostFormRef"
          :model="hostForm"
          :rules="hostRules"
          label-width="96px"
        >
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
              <el-form-item label="主机名称">
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
            </el-collapse-item>
          </el-collapse>
        </el-form>
      </FormDialog>

      <!-- 账号列表弹窗 -->
      <el-dialog
        v-model="accountsDialogVisible"
        :title="accountsDialogTitle"
        destroy-on-close
        width="min(960px, calc(100vw - 32px))"
      >
        <DataTableCard
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
            <el-button
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
          <el-table-column label="启用状态" width="80" align="center">
            <template #default="{ row }">
              <StatusSwitch
                :model-value="!row.disabled"
                :loading="statusUpdatingId === accountStatusKey(row)"
                @update:model-value="
                  (val: boolean) => toggleAccountStatus(row, val)
                "
              />
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
          <el-table-column
            label="账号名称"
            min-width="130"
            show-overflow-tooltip
          >
            <template #default="{ row }">{{
              accountDisplayName(row)
            }}</template>
          </el-table-column>
          <el-table-column
            label="账号分组"
            min-width="110"
            show-overflow-tooltip
          >
            <template #default="{ row }">{{ row.group || "-" }}</template>
          </el-table-column>
          <el-table-column label="操作" width="200" fixed="right">
            <template #default="{ row }">
              <el-button
                link
                type="success"
                size="small"
                @click="openConnectionDialog(row)"
                >连接</el-button
              >
              <el-button
                link
                type="primary"
                size="small"
                @click="openEditAccountDialog(row)"
                >编辑</el-button
              >
              <el-button
                link
                type="danger"
                size="small"
                :loading="deletingId === targetId(row)"
                @click="confirmDeleteAccount(row)"
              >
                删除
              </el-button>
            </template>
          </el-table-column>
        </DataTableCard>
      </el-dialog>

      <!-- 创建/编辑账号弹窗 -->
      <FormDialog
        v-model:visible="accountFormVisible"
        :title="editingAccountId ? '编辑账号' : '新增账号'"
        width="680px"
        :loading="submittingAccount"
        @submit="submitAccount"
      >
        <el-form
          ref="accountFormRef"
          v-loading="accountDetailLoading"
          :model="accountForm"
          :rules="accountRules"
          label-width="96px"
        >
          <div class="form-section">
            <div class="form-section-title">登录与认证</div>
            <el-form-item label="认证方式" prop="auth_method">
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
                placeholder="SSH 登录用户名"
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
              v-if="isKeyAuthMethod(accountForm.auth_method)"
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
              v-if="accountForm.auth_method === 'private_key'"
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
                <span class="expiry-text">{{ accountExpiryText }}</span>
              </div>
            </el-form-item>
          </div>

          <el-form-item label="连接测试">
            <div class="test-connection-row">
              <el-button :loading="testingConnection" @click="testConnection">测试连接</el-button>
              <template v-if="accountTestResult">
                <el-tag :type="accountTestResult.ok ? 'success' : 'danger'" size="small">
                  {{ accountTestResult.ok ? '可达' : '不可达' }}
                </el-tag>
                <span v-if="accountTestResult.latency_ms !== undefined" class="test-connection-meta">
                  延迟 {{ accountTestResult.latency_ms }}ms
                </span>
                <span v-if="accountTestResult.error" class="test-connection-error">
                  {{ accountTestResult.error }}
                </span>
              </template>
            </div>
          </el-form-item>

          <el-collapse v-model="accountMorePanels" class="more-collapse">
            <el-collapse-item title="更多设置" name="more">
              <el-form-item label="账号名称">
                <el-input
                  v-model="accountForm.name"
                  placeholder="默认等于登录账号"
                  @input="accountNameTouched = true"
                />
              </el-form-item>
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
              <el-form-item label="主机密钥" prop="host_key_mode">
                <div class="host-key-field">
                  <el-radio-group
                    v-model="accountForm.host_key_mode"
                    @change="handleHostKeyModeChange"
                  >
                    <el-radio-button label="fingerprint">指纹</el-radio-button>
                    <el-radio-button label="known_hosts"
                      >known_hosts</el-radio-button
                    >
                    <el-radio-button label="ignore">忽略校验</el-radio-button>
                  </el-radio-group>
                  <el-alert
                    v-if="accountForm.host_key_mode === 'ignore'"
                    class="host-key-alert"
                    show-icon
                    type="warning"
                    :closable="false"
                    title="仅在受控测试环境中使用，忽略校验会降低 SSH 主机身份保护。"
                  />
                </div>
              </el-form-item>
              <el-form-item
                v-if="accountForm.host_key_mode === 'fingerprint'"
                label="主机指纹"
                prop="host_key_fingerprint"
              >
                <el-input
                  v-model="accountForm.host_key_fingerprint"
                  placeholder="SHA256:..."
                  clearable
                />
              </el-form-item>
              <el-form-item
                v-if="accountForm.host_key_mode === 'known_hosts'"
                label="known_hosts"
                prop="known_hosts_path"
              >
                <el-input
                  v-model="accountForm.known_hosts_path"
                  placeholder="/home/app/.ssh/known_hosts"
                  clearable
                />
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

      <!-- 连接弹窗 -->
      <el-dialog
        v-model="connectionDialogVisible"
        destroy-on-close
        title="连接主机账号"
        width="min(720px, calc(100vw - 32px))"
      >
        <div v-if="selectedConnectionTarget" class="connection-dialog">
          <el-alert
            v-if="connectionError"
            show-icon
            type="error"
            :closable="false"
            :title="connectionError"
          />
          <el-alert
            v-else
            show-icon
            type="info"
            :closable="false"
            title="输入堡垒机的登录密码，不是目标主机的密码"
          />

          <div
            style="
              margin-bottom: 8px;
              display: flex;
              align-items: center;
              gap: 8px;
            "
            v-if="!creatingSession"
          >
            <span style="font-size: 13px; color: #667085">连通性：</span>
            <el-tag v-if="connectionTesting" type="info" size="small"
              >测试中...</el-tag
            >
            <template v-else-if="connectionTestResult !== null">
              <el-tag
                :type="connectionTestResult.ok ? 'success' : 'danger'"
                size="small"
              >
                {{ connectionTestResult.ok ? "可达" : "不可达" }}
              </el-tag>
              <span
                v-if="connectionTestResult.latency_ms !== undefined"
                style="font-size: 12px; color: #667085"
              >
                延迟 {{ connectionTestResult.latency_ms }}ms
              </span>
              <span
                v-if="connectionTestResult.error"
                style="font-size: 12px; color: var(--el-color-danger)"
              >
                {{ connectionTestResult.error }}
              </span>
            </template>
          </div>

          <div
            v-if="creatingSession"
            style="text-align: center; padding: 30px 0"
          >
            <el-icon class="is-loading" :size="28"><Loading /></el-icon>
            <p style="margin-top: 10px; color: #667085">正在创建连接会话...</p>
          </div>

          <template v-else-if="!connectionError && connectionCompactUser">
            <el-descriptions
              :column="1"
              border
              size="small"
              style="margin-top: 12px"
            >
              <el-descriptions-item label="连接地址">
                <code
                  >{{ bastionHost }}:{{
                    bastionPort || 47102
                  }}</code
                >
                <el-button
                  link
                  type="primary"
                  size="small"
                  style="margin-left: 8px"
                  @click="
                    copyText(
                      `${bastionHost}:${bastionPort || 47102}`,
                    )
                  "
                  >复制</el-button
                >
              </el-descriptions-item>
              <el-descriptions-item label="用户名">
                <code>{{ connectionCompactUser }}</code>
                <el-button
                  link
                  type="primary"
                  size="small"
                  style="margin-left: 8px"
                  @click="copyText(connectionCompactUser)"
                  >复制</el-button
                >
              </el-descriptions-item>
              <el-descriptions-item label="密码"
                >堡垒机登录密码</el-descriptions-item
              >
            </el-descriptions>

            <div style="margin-top: 12px">
              <el-input
                :model-value="`ssh ${connectionCompactUser}@${bastionHost} -p ${bastionPort || 47102}`"
                readonly
                size="small"
              >
                <template #append>
                  <el-button
                    @click="
                      copyText(
                        `ssh ${connectionCompactUser}@${bastionHost} -p ${bastionPort || 47102}`,
                      )
                    "
                    >复制 SSH 命令</el-button
                  >
                </template>
              </el-input>
            </div>
          </template>
        </div>
        <template #footer>
          <el-button type="primary" @click="openTerminalFromDialog">在浏览器中打开</el-button>
          <el-button @click="connectionDialogVisible = false">关闭</el-button>
        </template>
      </el-dialog>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import {
  ElMessage,
  ElMessageBox,
  type FormInstance,
  type FormRules,
} from "element-plus";
import { ArrowDown, Loading } from "@element-plus/icons-vue";
import DataTableCard from "@/components/DataTableCard.vue";
import FormDialog from "@/components/FormDialog.vue";
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

type AuthMethod = "password" | "private_key";
type HostKeyMode = "ignore" | "fingerprint" | "known_hosts";

interface HostForm {
  id: string;
  name: string;
  group: string;
  address: string;
  port: number;
  remark: string;
}

interface AccountForm {
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
}

const { t } = useI18n();

// ── Host list state ──
const hosts = ref<HostView[]>([]);
const hostTotal = ref(0);
const hostPage = ref(1);
const hostPageSize = ref(20);
const keyword = ref("");
const hostsLoading = ref(false);
const hostError = ref("");

// ── Account list state ──
const selectedHost = ref<HostView | null>(null);
const accounts = ref<TargetRecord[]>([]);
const accountTotal = ref(0);
const accountPage = ref(1);
const accountPageSize = ref(20);
const accountsLoading = ref(false);
const accountError = ref("");

// ── Dialog visibility ──
const hostDialogVisible = ref(false);
const accountFormVisible = ref(false);
const accountsDialogVisible = ref(false);
const connectionDialogVisible = ref(false);
const router = useRouter();

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
const accountNameTouched = ref(false);
const hostMorePanels = ref<string[]>([]);
const accountMorePanels = ref<string[]>([]);
const accountDetailLoading = ref(false);

// ── Connection state ──
const selectedConnectionTarget = ref<TargetRecord | null>(null);
const bastionHost = ref(window.location.hostname);
const bastionPort = ref(47102);
const userSessionId = ref("");
const creatingSession = ref(false);
const connectionError = ref("");
const connectionTesting = ref(false);
const connectionTestResult = ref<{
  ok: boolean;
  error?: string;
  latency_ms?: number;
} | null>(null);

// ── Refs ──
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

async function loadGroupOptions() {
  try {
    const resourceGroups = await apiClient.getResourceGroups({ group_type: 'resource' });
    const accountGroups = await apiClient.getResourceGroups({ group_type: 'account' });
    hostGroupOptions.value = (resourceGroups.items ?? []).map(g => g.name).filter(Boolean);
    accountGroupOptions.value = (accountGroups.items ?? []).map(g => g.name).filter(Boolean);
  } catch {
    // 加载失败时保持空列表
  }
}
const accountExpiryText = computed(() => {
  if (!accountForm.expires_at) return "永久有效";
  return formatDateTime(accountForm.expires_at);
});
const connectionCompactUser = computed(() => {
  const target = selectedConnectionTarget.value;
  if (!target) return "";
  const resId =
    target.resource_id || targetId(target) || resourceId(target) || "0000";
  const sessionId = userSessionId.value;
  return sessionId ? `H${resId}${sessionId}` : "";
});

// ── Form rules ──
const hostRules: FormRules<HostForm> = {
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
  host_key_mode: [
    { required: true, message: "请选择主机密钥校验方式", trigger: "change" },
  ],
  password: [{ validator: validatePassword, trigger: "blur" }],
  private_key_pem: [{ validator: validatePrivateKeyPEM, trigger: "blur" }],
  host_key_fingerprint: [
    { validator: validateHostKeyFingerprint, trigger: "blur" },
  ],
  known_hosts_path: [{ validator: validateKnownHostsPath, trigger: "blur" }],
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
  const port = numberFrom(host.port, 22);
  return address ? `${address}:${port}` : "-";
}

function hostName(host: HostView): string {
  return stringFrom(host.name).trim() || stringFrom(host.address).trim() || "-";
}

function hostStatusKey(host: HostView): string {
  return `host:${hostId(host)}`;
}

function targetId(target: TargetRecord): string {
  return String(target.id ?? "");
}

function accountStatusKey(target: TargetRecord): string {
  return `account:${targetId(target)}`;
}

function targetRemark(target: TargetRecord): string {
  return stringFrom(target.remark).trim();
}

function accountDisplayName(target: TargetRecord): string {
  return (
    stringFrom(target.name).trim() ||
    stringFrom(target.username).trim() ||
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
  const port = numberFrom(target.port, 22);
  const portSuffix = `:${port}`;
  return addr2.endsWith(portSuffix)
    ? addr2.slice(0, -portSuffix.length)
    : addr2;
}

function resourceId(target: TargetRecord): string {
  return stringFrom(target.resource_id).trim() || targetId(target);
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
  return `${displayHost}:${numberFrom(port, 22)}`;
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

// ════════════════════════════════════════════════════════════════
// Form factories
// ════════════════════════════════════════════════════════════════

function emptyHostForm(): HostForm {
  return { id: "", name: "", group: "", address: "", port: 22, remark: "" };
}

function emptyAccountForm(): AccountForm {
  return {
    id: "",
    name: "",
    group: "",
    remark: "",
    disabled: false,
    expires_at: "",
    username: "",
    auth_method: "password",
    password: "",
    private_key_pem: "",
    passphrase: "",
    host_key_mode: "ignore",
    insecure_ignore_host_key: false,
    host_key_fingerprint: "",
    known_hosts_path: "",
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

function syncDefaultAccountName() {
  if (!accountNameTouched.value) {
    accountForm.name = accountForm.username.trim();
  }
}

function recordToHostForm(host: HostView): HostForm {
  return {
    id: hostId(host),
    name: stringFrom(host.name),
    group: stringFrom(host.group),
    address: stringFrom(host.address),
    port: numberFrom(host.port, 22),
    remark: stringFrom(host.remark),
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
    password: "",
    private_key_pem: "",
    passphrase: "",
    host_key_mode: hostKeyMode,
    insecure_ignore_host_key: target.insecure_ignore_host_key === true,
    host_key_fingerprint: stringFrom(target.host_key_fingerprint),
    known_hosts_path: stringFrom(target.known_hosts_path),
  };
}

function hostKeyModeForTarget(target: TargetRecord): HostKeyMode {
  if (target.insecure_ignore_host_key === true) return "ignore";
  if (hasValue(target.host_key_fingerprint)) return "fingerprint";
  if (hasValue(target.known_hosts_path)) return "known_hosts";
  return "fingerprint";
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
    remark: hostForm.remark.trim() || undefined,
  };
}

function hostPayloadFromRecord(host: HostView): HostPayload {
  return {
    id: hostId(host) || undefined,
    name: hostName(host),
    group: stringFrom(host.group).trim() || undefined,
    address: stringFrom(host.address).trim(),
    port: numberFrom(host.port, 22),
    remark: stringFrom(host.remark).trim() || undefined,
  };
}

function buildAccountPayload(): TargetPayload {
  const host = selectedHost.value;
  const username = accountForm.username.trim();
  const hostKey = accountHostKeyPayload();
  const payload: TargetPayload = {
    id:
      accountForm.id.trim() ||
      (host ? generatedAccountID(host, username) : sanitizeID(username)),
    host_id: host ? hostId(host) : undefined,
    name: accountForm.name.trim() || username,
    group: accountForm.group.trim() || undefined,
    remark: accountForm.remark.trim() || undefined,
    disabled: accountForm.disabled,
    expires_at: accountForm.expires_at || undefined,
    address: stringFrom(host?.address).trim(),
    port: numberFrom(host?.port, 22),
    username,
    password: "",
    private_key_path: "",
    private_key_pem: "",
    passphrase: "",
    insecure_ignore_host_key: hostKey.insecure_ignore_host_key,
    host_key_fingerprint: hostKey.host_key_fingerprint,
    known_hosts_path: hostKey.known_hosts_path,
  };
  if (accountForm.auth_method === "password") {
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
  const mode = hostKeyModeForTarget(target);
  return {
    id: targetId(target),
    host_id: stringFrom(target.host_id).trim() || undefined,
    name:
      stringFrom(target.name).trim() ||
      stringFrom(target.username).trim() ||
      targetId(target),
    group: stringFrom(target.group).trim() || undefined,
    remark: stringFrom(target.remark).trim() || undefined,
    disabled,
    expires_at: stringFrom(target.expires_at).trim() || undefined,
    address: targetHostString(target),
    port: numberFrom(target.port, 22),
    username: stringFrom(target.username).trim(),
    password: "",
    private_key_path: "",
    private_key_pem: "",
    passphrase: "",
    insecure_ignore_host_key: mode === "ignore",
    host_key_fingerprint:
      mode === "fingerprint"
        ? stringFrom(target.host_key_fingerprint).trim()
        : "",
    known_hosts_path:
      mode === "known_hosts" ? stringFrom(target.known_hosts_path).trim() : "",
  };
}

function accountHostKeyPayload() {
  const mode = accountForm.host_key_mode;
  return {
    insecure_ignore_host_key: mode === "ignore",
    host_key_fingerprint:
      mode === "fingerprint" ? accountForm.host_key_fingerprint.trim() : "",
    known_hosts_path:
      mode === "known_hosts" ? accountForm.known_hosts_path.trim() : "",
  };
}

function selectedCredentialValue(): string {
  if (accountForm.auth_method === "password") return accountForm.password;
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
    accountForm.auth_method === "password" &&
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
    accountForm.auth_method === "private_key" &&
    !hasValue(value)
  ) {
    callback(new Error(t("hosts.required.privateKeyPem")));
    return;
  }
  callback();
}

function validateHostKeyFingerprint(
  _rule: unknown,
  value: unknown,
  callback: (error?: Error) => void,
) {
  if (accountForm.host_key_mode === "fingerprint" && !hasValue(value)) {
    callback(new Error("请输入 SSH 主机密钥指纹"));
    return;
  }
  callback();
}

function validateKnownHostsPath(
  _rule: unknown,
  value: unknown,
  callback: (error?: Error) => void,
) {
  if (accountForm.host_key_mode === "known_hosts" && !hasValue(value)) {
    callback(new Error("请输入 known_hosts 文件路径"));
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

function handleHostKeyModeChange() {
  accountForm.insecure_ignore_host_key = accountForm.host_key_mode === "ignore";
  if (accountForm.host_key_mode !== "fingerprint") {
    accountForm.host_key_fingerprint = "";
  }
  if (accountForm.host_key_mode !== "known_hosts") {
    accountForm.known_hosts_path = "";
  }
  accountFormRef.value?.clearValidate([
    "host_key_fingerprint",
    "known_hosts_path",
  ]);
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
  hostsLoading.value = true;
  hostError.value = "";
  try {
    const res: PageResponse<HostView> = await apiClient.getHosts({
      page: hostPage.value,
      page_size: hostPageSize.value,
      q: keyword.value.trim() || undefined,
    });
    hosts.value = res.items ?? [];
    hostTotal.value = res.total ?? 0;
  } catch (err) {
    hostError.value =
      err instanceof Error ? err.message : t("hosts.error.loadList");
  } finally {
    hostsLoading.value = false;
  }
}

function onHostSearch(q: string) {
  keyword.value = q;
  hostPage.value = 1;
  fetchHosts();
}

async function loadSelectedHostAccounts() {
  const host = selectedHost.value;
  const id = host ? hostId(host) : "";
  if (!id) return;
  accountsLoading.value = true;
  accountError.value = "";
  try {
    const res: PageResponse<TargetRecord> = await apiClient.getHostAccounts(
      id,
      {
        page: accountPage.value,
        page_size: accountPageSize.value,
      },
    );
    accounts.value = res.items ?? [];
    accountTotal.value = res.total ?? 0;
  } catch (err) {
    accounts.value = [];
    accountError.value =
      err instanceof Error ? err.message : t("hosts.error.loadList");
  } finally {
    accountsLoading.value = false;
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
  hostMorePanels.value = [];
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
      await apiClient.createHost(payload);
      ElMessage.success("主机已创建");
    }
    hostDialogVisible.value = false;
    await fetchHosts();
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
    // HostPayload doesn't have a status field, so we pass it as an extra property
    await apiClient.updateHost(id, {
      ...hostPayloadFromRecord(host),
      status: newStatus,
    } as HostPayload & { status: string });
    ElMessage.success(active ? "主机已启用" : "主机已禁用");
    await fetchHosts();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t("hosts.error.save"));
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
  setSelectedHost(host);
  accountPage.value = 1;
  accountsDialogVisible.value = true;
  await loadSelectedHostAccounts();
}

async function openCreateAccountDialog(host: HostView) {
  setSelectedHost(host);
  editingAccountId.value = null;
  accountNameTouched.value = false;
  accountMorePanels.value = [];
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
  editingAccountId.value = id;
  accountNameTouched.value = true;
  accountMorePanels.value = [];
  accountTestResult.value = null;
  resetAccountForm(recordToAccountForm(target));
  accountFormVisible.value = true;
  accountDetailLoading.value = true;
  await nextTick();
  accountFormRef.value?.clearValidate();
  try {
    const detail = await apiClient.getTarget(id);
    resetAccountForm(recordToAccountForm(detail));
  } catch (err) {
    ElMessage.error(
      err instanceof Error ? err.message : t("hosts.error.loadDetail"),
    );
  } finally {
    accountDetailLoading.value = false;
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
    await Promise.all([fetchHosts(), loadSelectedHostAccounts()]);
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
    accountTestResult.value = {
      ok: false,
      error: err instanceof Error ? err.message : "连接测试失败",
    };
  } finally {
    testingConnection.value = false;
  }
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

async function openConnectionDialog(target: TargetRecord) {
  selectedConnectionTarget.value = target;
  userSessionId.value = "";
  connectionError.value = "";
  connectionTestResult.value = null;
  creatingSession.value = true;
  connectionDialogVisible.value = true;
  // Run connectivity test in parallel
  testHostConnection();
  try {
    const targetIdStr = String(target.id || target.resource_id || "");
    if (!targetIdStr) {
      connectionError.value = "无法获取目标资源ID";
      return;
    }
    const session = await apiClient.createUserSession(targetIdStr);
    userSessionId.value = session?.session_id || "";
  } catch (err) {
    connectionError.value =
      err instanceof Error ? err.message : "创建连接会话失败";
  } finally {
    creatingSession.value = false;
  }
}

async function testHostConnection() {
  if (!selectedConnectionTarget.value) return;
  connectionTesting.value = true;
  connectionTestResult.value = null;
  try {
    const target = selectedConnectionTarget.value;
    const result = await apiClient.testTargetConnection(
      targetStatusPayload(target, false),
    );
    connectionTestResult.value = {
      ok: result.ok,
      error: result.ok ? undefined : result.message || "连接失败",
    };
  } catch (err) {
    connectionTestResult.value = {
      ok: false,
      error: err instanceof Error ? err.message : "连接失败",
    };
  } finally {
    connectionTesting.value = false;
  }
}

/** 从主机直接打开连接，单账号时直接弹连接窗，多账号时打开账号管理 */
async function handleHostConnect(host: HostView) {
  setSelectedHost(host);
  accountPage.value = 1;
  await loadSelectedHostAccounts();
  const count = accounts.value.length;
  if (count === 0) {
    ElMessage.warning('该主机下无可用账号，请先新增账号');
  } else if (count === 1) {
    openConnectionDialog(accounts.value[0]);
  } else {
    accountsDialogVisible.value = true;
    ElMessage.info('请从账号列表中选择要连接的账号');
  }
}

/** 更多操作 - 审计日志（占位） */
function handleHostAuditLog(_host: HostView) {
  ElMessage.info('审计日志功能开发中');
}

/** 更多操作 - 在线会话（占位） */
function handleHostSessions(_host: HostView) {
  ElMessage.info('在线会话功能开发中');
}

/** 更多操作 - 权限管理（占位） */
function handleHostPermissions(_host: HostView) {
  ElMessage.info('权限管理功能开发中');
}

async function copyText(value: string) {
  if (!hasValue(value)) {
    ElMessage.warning("没有可复制的内容");
    return;
  }
  try {
    if (!navigator.clipboard?.writeText)
      throw new Error("clipboard unavailable");
    await navigator.clipboard.writeText(value);
    ElMessage.success("已复制");
  } catch {
    ElMessage.warning("复制失败，请手动选择文本复制");
  }
}

function openTerminalFromDialog() {
  const target = selectedConnectionTarget.value
  if (!target) return
  const tid = String(target.id || target.resource_id || '')
  if (!tid) {
    ElMessage.warning('无法获取目标资源ID')
    return
  }
  connectionDialogVisible.value = false
  router.push({ path: '/web-terminal', query: { target_id: tid } })
}

// ════════════════════════════════════════════════════════════════
// Watchers & lifecycle
// ════════════════════════════════════════════════════════════════

watch(
  () => accountForm.username,
  () => {
    syncDefaultAccountName();
  },
);

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

watch([hostPage, hostPageSize], () => fetchHosts());
watch([accountPage, accountPageSize], () => {
  if (accountsDialogVisible.value) loadSelectedHostAccounts();
});

onMounted(() => {
  fetchHosts();
  loadGroupOptions();
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

/* Host key verification */
.host-key-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}
.host-key-field :deep(.el-radio-group) {
  display: flex;
  width: 100%;
}
.host-key-field :deep(.el-radio-button) {
  flex: 1;
}
.host-key-field :deep(.el-radio-button__inner) {
  width: 100%;
  padding-inline: 8px;
  white-space: nowrap;
}
.host-key-alert {
  line-height: 1.4;
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
.expiry-text {
  color: var(--color-text-secondary);
  font-size: 12px;
  line-height: 1.4;
}

/* Connection */
.connection-dialog {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

/* FormDialog body min-height for account edit */
:deep(.form-dialog-body) {
  min-height: 280px;
}
</style>
