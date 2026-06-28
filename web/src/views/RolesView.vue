<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-input
        v-model="searchQuery"
        clearable
        :placeholder="t('roles.search')"
        class="search-input"
        @input="onSearch"
      />
      <el-button type="primary" @click="openCreateDialog">{{ t('roles.create') }}</el-button>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon />

    <el-table v-else v-loading="loading" :data="filteredRoles" height="420" row-key="id">
      <el-table-column :label="t('roles.name')" min-width="160">
        <template #default="{ row }">
          <div>
            <strong>{{ row.name }}</strong>
            <div class="col-mono">{{ row.id }}</div>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="description" :label="t('roles.description')" min-width="200" show-overflow-tooltip />
      <el-table-column :label="t('roles.builtin')" width="80">
        <template #default="{ row }">
          <el-tag :type="row.builtin ? 'warning' : 'info'" size="small">
            {{ row.builtin ? t('common.yes') : t('common.no') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.status')" width="90">
        <template #default="{ row }">
          <el-tag :type="row.status === 'disabled' ? 'info' : 'success'" size="small">
            {{ row.status === 'active' ? '已启用' : '已禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('roles.permissionCount')" width="80">
        <template #default="{ row }">
          <span class="perm-count">{{ rolePermCount(row.id) }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" fixed="right" width="260">
        <template #default="{ row }">
          <el-button link type="primary" @click="openPermDialog(row)">分配权限</el-button>
          <template v-if="!row.builtin">
            <el-button
              link
              :type="row.status === 'disabled' ? 'success' : 'warning'"
              :loading="togglingRoleId === row.id"
              @click="toggleStatus(row)"
            >
              {{ row.status === 'disabled' ? '启用' : '禁用' }}
            </el-button>
            <el-button link type="danger" :loading="deletingRoleId === row.id" @click="deleteRole(row)">
              {{ t('common.delete') }}
            </el-button>
          </template>
          <el-tooltip v-else content="内置角色不可删除" placement="top">
            <span class="builtin-hint">内置角色</span>
          </el-tooltip>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!loading && !filteredRoles.length && !error" :description="t('roles.empty')" />

    <!-- Create Role Dialog -->
    <el-dialog
      v-model="createDialogVisible"
      :close-on-click-modal="!submitting"
      :title="t('roles.create')"
      class="form-dialog"
      destroy-on-close
      width="min(440px, calc(100vw - 32px))"
    >
      <el-form ref="createFormRef" :model="createForm" :rules="createFormRules" label-position="top">
        <el-form-item :label="t('roles.name')" prop="name">
          <el-input v-model="createForm.name" placeholder="如：运维工程师" />
        </el-form-item>
        <el-collapse v-model="createMorePanels" class="more-collapse">
          <el-collapse-item title="更多设置" name="more">
            <el-form-item :label="t('roles.description')" prop="description">
              <el-input v-model="createForm.description" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" placeholder="角色用途说明" />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
      <template #footer>
        <el-button :disabled="submitting" @click="createDialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button :loading="submitting" type="primary" @click="submitCreateRole">{{ t('common.create') }}</el-button>
      </template>
    </el-dialog>

    <!-- Permission Assignment Dialog -->
    <el-dialog
      v-model="permDialogVisible"
      :close-on-click-modal="!savingPerms"
      :title="permDialogTitle"
      class="form-dialog"
      destroy-on-close
      width="min(680px, calc(100vw - 32px))"
    >
      <div class="perm-dialog-header">
        <span class="perm-count-label">{{ selectedCountText }}</span>
      </div>
      <div class="perm-groups">
        <div v-for="group in permGroups" :key="group.resource" class="perm-group">
          <div class="perm-group-title">
            <span class="perm-resource-icon">{{ group.icon }}</span>
            {{ group.resource }}
          </div>
          <div class="perm-group-actions">
            <el-button link size="small" @click="toggleGroup(group, true)">全选</el-button>
            <el-button link size="small" @click="toggleGroup(group, false)">取消全选</el-button>
          </div>
          <div class="perm-check-grid">
            <el-checkbox
              v-for="perm in group.permissions"
              :key="perm.action"
              v-model="selectedPerms"
              :label="perm.action"
              :value="perm.action"
              class="perm-check-item"
            >
              <span class="perm-action-label">{{ perm.action }}</span>
              <span class="perm-action-desc">{{ perm.desc }}</span>
            </el-checkbox>
          </div>
        </div>
      </div>
      <template #footer>
        <el-button :disabled="savingPerms" @click="permDialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button :loading="savingPerms" type="primary" @click="savePermissions">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus';

import {
  apiClient,
  type RBACPermissionRecord,
  type RBACRolePermissionRecord,
  type RBACRoleRecord,
  type RBACRolePayload,
} from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();

interface PermItem { action: string; label: string; desc: string; }
interface PermGroup { resource: string; icon: string; permissions: PermItem[]; }

const PERM_GROUPS: PermGroup[] = [
  {
    resource: '主机 (host)', icon: '🖥️', permissions: [
      { action: 'host:view',   label: '查看主机',   desc: '浏览主机列表与详情' },
      { action: 'host:create', label: '创建主机',   desc: '新增纳管主机' },
      { action: 'host:update', label: '编辑主机',   desc: '修改主机配置' },
      { action: 'host:delete', label: '删除主机',   desc: '移除纳管主机' },
    ],
  },
  {
    resource: '数据库 (database)', icon: '🗄️', permissions: [
      { action: 'dbproxy:view',   label: '查看实例',       desc: '浏览数据库实例' },
      { action: 'dbproxy:create', label: '创建实例',       desc: '新增数据库代理实例' },
      { action: 'dbproxy:update', label: '编辑实例',       desc: '修改数据库代理配置' },
      { action: 'dbproxy:delete', label: '删除实例',       desc: '删除数据库代理实例' },
      { action: 'db:connect',     label: '连接数据库',      desc: '通过代理连接' },
      { action: 'db:audit:view',  label: '查看数据库审计',  desc: '浏览数据库审计记录' },
    ],
  },
  {
    resource: '会话 (session)', icon: '📡', permissions: [
      { action: 'session:connect', label: '连接会话', desc: '建立SSH会话' },
      { action: 'session:view',    label: '查看会话', desc: '浏览活跃和历史会话' },
    ],
  },
  {
    resource: 'SFTP', icon: '📁', permissions: [
      { action: 'sftp:read',  label: '读取文件', desc: '通过SFTP下载' },
      { action: 'sftp:write', label: '写入文件', desc: '通过SFTP上传' },
    ],
  },
  {
    resource: '审计 (audit)', icon: '📋', permissions: [
      { action: 'audit:view', label: '查看审计', desc: '浏览SSH审计日志' },
    ],
  },
  {
    resource: '权限管理 (rbac)', icon: '🔐', permissions: [
      { action: 'rbac:manage', label: '管理RBAC', desc: '管理角色、权限和绑定' },
    ],
  },
  {
    resource: '目标 (target)', icon: '🎯', permissions: [
      { action: 'target:view',   label: '查看目标', desc: '浏览目标资产' },
      { action: 'target:create', label: '创建目标', desc: '新增目标资产' },
      { action: 'target:update', label: '编辑目标', desc: '修改目标配置' },
      { action: 'target:delete', label: '删除目标', desc: '移除目标资产' },
    ],
  },
];

const roles = ref<RBACRoleRecord[]>([]);
const loading = ref(false);
const error = ref('');
const searchQuery = ref('');
const submitting = ref(false);
const togglingRoleId = ref<string>('');
const deletingRoleId = ref<string>('');

// Create role
const createDialogVisible = ref(false);
const createFormRef = ref<FormInstance>();
const createMorePanels = ref<string[]>([]);
const createForm = reactive<RBACRolePayload>({ name: '', description: '', status: 'active' });
const createFormRules: FormRules = {
  name: [{ required: true, message: '请输入角色名称', trigger: 'blur' }],
};

// Permission dialog
const permDialogVisible = ref(false);
const savingPerms = ref(false);
const currentPermRole = ref<RBACRoleRecord | null>(null);
const selectedPerms = ref<string[]>([]);
const existingBindings = ref<RBACRolePermissionRecord[]>([]);
const allPermissions = ref<RBACPermissionRecord[]>([]);

const permGroups = PERM_GROUPS;

const filteredRoles = computed(() => {
  const q = searchQuery.value.trim().toLowerCase();
  if (!q) return roles.value;
  return roles.value.filter(r =>
    [r.name, r.description, String(r.id ?? '')].some(v => String(v ?? '').toLowerCase().includes(q))
  );
});

const permDialogTitle = computed(() =>
  currentPermRole.value ? `分配权限 — ${currentPermRole.value.name}` : '分配权限'
);

const selectedCountText = computed(() =>
  `${selectedPerms.value.length} 项已选`
);

const rolePermCountMap = computed(() => {
  const map: Record<string, number> = {};
  for (const b of existingBindings.value) {
    const rid = String(b.role_id ?? '');
    map[rid] = (map[rid] || 0) + 1;
  }
  return map;
});

function rolePermCount(roleId: string | number | undefined): number {
  if (!roleId) return 0;
  return rolePermCountMap.value[String(roleId)] || 0;
}

async function loadRoles() {
  loading.value = true;
  error.value = '';
  try {
    const res = await apiClient.getRBACRoles();
    roles.value = Array.isArray(res) ? res : (res as any).data ?? [];
    // Load all role-permission bindings
    const bindings = await apiClient.getRBACRolePermissions();
    existingBindings.value = Array.isArray(bindings) ? bindings : (bindings as any).data ?? [];
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('roles.error.load');
  } finally {
    loading.value = false;
  }
}

// ── Create role ──
function openCreateDialog() {
  createForm.name = '';
  createForm.description = '';
  createMorePanels.value = [];
  createDialogVisible.value = true;
}

async function submitCreateRole() {
  const valid = await createFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  submitting.value = true;
  try {
    await apiClient.createRBACRole({
      name: createForm.name,
      description: createForm.description,
      status: 'active',
    });
    ElMessage.success(t('roles.created'));
    createDialogVisible.value = false;
    await loadRoles();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('roles.error.create'));
  } finally {
    submitting.value = false;
  }
}

// ── Toggle / Delete ──
async function toggleStatus(role: RBACRoleRecord) {
  const id = String(role.id ?? '');
  const newStatus = role.status === 'disabled' ? 'active' : 'disabled';
  togglingRoleId.value = id;
  try {
    await apiClient.updateRBACRole(id, {
      id: id,
      name: String(role.name ?? id),
      description: String(role.description ?? ''),
      status: newStatus,
    });
    ElMessage.success(t('roles.statusToggled'));
    await loadRoles();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '操作失败');
  } finally {
    togglingRoleId.value = '';
  }
}

async function deleteRole(role: RBACRoleRecord) {
  const id = String(role.id ?? '');
  try {
    await ElMessageBox.confirm(
      `确认删除角色 "${role.name}"？`,
      t('common.delete'),
      { confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel'), type: 'warning' }
    );
  } catch { return; }
  deletingRoleId.value = id;
  try {
    await apiClient.deleteRBACRole(id);
    ElMessage.success(t('roles.deleted'));
    await loadRoles();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('roles.error.delete'));
  } finally {
    deletingRoleId.value = '';
  }
}

// ── Permission dialog ──
async function openPermDialog(role: RBACRoleRecord) {
  currentPermRole.value = role;
  // Load current bindings for this role
  const roleId = String(role.id ?? '');
  const rolePerms = existingBindings.value.filter(b => String(b.role_id) === roleId);
  selectedPerms.value = rolePerms.map(b => {
    const permId = String(b.permission_id);
    const perm = allPermissions.value.find(p => String(p.id) === permId);
    return perm?.action ?? '';
  }).filter(Boolean);
  permDialogVisible.value = true;
}

function toggleGroup(group: PermGroup, select: boolean) {
  const actions = group.permissions.map(p => p.action);
  if (select) {
    for (const a of actions) {
      if (!selectedPerms.value.includes(a)) selectedPerms.value.push(a);
    }
  } else {
    selectedPerms.value = selectedPerms.value.filter(a => !actions.includes(a));
  }
}

async function savePermissions() {
  if (!currentPermRole.value) return;
  const roleId = String(currentPermRole.value.id ?? '');

  savingPerms.value = true;
  try {
    // 先确保所有选中 action 都有对应的 Permission 记录（不存在则自动创建）
    await ensurePermissionsExist(selectedPerms.value);

    // 重新加载权限列表
    const permsRes = await apiClient.getRBACPermissions();
    allPermissions.value = Array.isArray(permsRes) ? permsRes : (permsRes as any).data ?? [];

    // Build action → permission_id map
    const actionToPermId = new Map<string, string>();
    for (const p of allPermissions.value) {
      if (p.action) actionToPermId.set(p.action, String(p.id ?? ''));
    }

    // Get current bindings
    const current = existingBindings.value.filter(b => String(b.role_id) === roleId);

    const currentActions = new Set(
      current.map(b => {
        const perm = allPermissions.value.find(p => String(p.id) === String(b.permission_id));
        return perm?.action ?? '';
      }).filter(Boolean)
    );

    const desiredActions = new Set(selectedPerms.value);

    // Add new bindings
    for (const action of desiredActions) {
      if (!currentActions.has(action)) {
        const permId = actionToPermId.get(action);
        if (permId) {
          await apiClient.createRBACRolePermission({ role_id: roleId, permission_id: permId });
        }
      }
    }

    // Remove deselected bindings
    for (const binding of current) {
      const perm = allPermissions.value.find(p => String(p.id) === String(binding.permission_id));
      if (perm?.action && !desiredActions.has(perm.action)) {
        await apiClient.deleteRBACRolePermission(String(binding.id ?? ''));
      }
    }

    // Reload
    const bindings = await apiClient.getRBACRolePermissions();
    existingBindings.value = Array.isArray(bindings) ? bindings : (bindings as any).data ?? [];

    ElMessage.success(`权限已更新（${selectedPerms.value.length} 项）`);
    permDialogVisible.value = false;
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '保存权限失败');
  } finally {
    savingPerms.value = false;
  }
}

async function ensurePermissionsExist(actions: string[]) {
  // 构建已存在 action 的集合
  const existing = new Set(allPermissions.value.map(p => p.action).filter(Boolean) as string[]);
  for (const action of actions) {
    if (!existing.has(action)) {
      await apiClient.createRBACPermission({ action, effect: 'allow' });
      existing.add(action);
    }
  }
}

function onSearch() {}
onMounted(async () => {
  await loadRoles();
  // Also load all permissions once for the dialog mapping
  try {
    const res = await apiClient.getRBACPermissions();
    allPermissions.value = Array.isArray(res) ? res : (res as any).data ?? [];
  } catch { /* non-critical */ }
});
</script>

<style scoped>
.view-stack { display: grid; gap: 14px; }
.toolbar { display: flex; gap: 10px; align-items: center; }
.search-input { max-width: 280px; }
.col-mono { font-family: "SF Mono", "Cascadia Code", "Consolas", monospace; font-size: 11px; color: #64748b; }
.perm-count { font-weight: 600; color: var(--el-color-primary); }
.builtin-hint { font-size: 12px; color: #94a3b8; cursor: default; }

.perm-dialog-header { margin-bottom: 14px; }
.perm-count-label { font-size: 13px; color: #64748b; font-weight: 500; }

.perm-groups { max-height: 55vh; overflow-y: auto; }
.perm-group { margin-bottom: 16px; padding-bottom: 12px; border-bottom: 1px solid #eef2f7; }
.perm-group:last-child { border-bottom: none; margin-bottom: 0; }
.perm-group-title { font-size: 13px; font-weight: 600; color: #1e293b; margin-bottom: 4px; display: flex; align-items: center; gap: 6px; }
.perm-group-actions { margin-bottom: 4px; padding-left: 22px; }
.perm-check-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 2px 8px; padding-left: 22px; }
.perm-check-item { margin-right: 0; }
.perm-action-label { font-family: "SF Mono", "Cascadia Code", "Consolas", monospace; font-size: 12px; color: #334155; }
.perm-action-desc { font-size: 11px; color: #94a3b8; margin-left: 6px; }

.more-collapse { border-top: 1px solid #eef2f7; border-bottom: 0; }
.more-collapse :deep(.el-collapse-item__header) { color: #374151; font-size: 13px; font-weight: 700; }
.more-collapse :deep(.el-collapse-item__wrap) { border-bottom: 0; }
:global(.form-dialog .el-dialog__body) { max-height: min(66vh, 620px); overflow-y: auto; padding-right: 22px; }
</style>
