<template>
  <div class="view-stack">
    <el-alert v-if="error" :title="error" type="error" show-icon closable @close="error = ''" />

    <DataTableCard
    :data="roles"
    :loading="loading"
    :total="total"
    v-model:page="page"
    v-model:page-size="pageSize"
    search-placeholder="搜索角色名称、备注…"
    @search="onSearch"
  >
    <template #toolbar-extra>
      <el-button type="primary" @click="openCreateDialog">{{ t('roles.create') }}</el-button>
    </template>

    <el-table-column :label="t('roles.name')" min-width="160">
      <template #default="{ row }">
        <strong>{{ row.name }}</strong>
      </template>
    </el-table-column>
    <el-table-column :label="t('roles.builtin')" width="80">
      <template #default="{ row }">
        <el-tag :type="row.builtin ? 'warning' : 'info'" size="small">
          {{ row.builtin ? t('common.yes') : t('common.no') }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column v-bind="TABLE_COLUMNS.status" :label="t('common.status')">
      <template #default="{ row }">
        <StatusSwitch
          :model-value="row.status === 'active'"
          :loading="togglingRoleId === String(row.id ?? '')"
          @update:model-value="(val: boolean) => toggleStatus(row, val)"
        />
      </template>
    </el-table-column>
    <el-table-column v-bind="TABLE_COLUMNS.number" :label="t('roles.permissionCount')">
      <template #default="{ row }">
        <span class="perm-count">{{ rolePermCount(row.id) }}</span>
      </template>
    </el-table-column>
    <el-table-column v-bind="TABLE_COLUMNS.note" :label="t('common.remark')">
      <template #default="{ row }">
        {{ row.description || '-' }}
      </template>
    </el-table-column>
    <el-table-column v-bind="TABLE_COLUMNS.actions" :label="t('common.actions')">
      <template #default="{ row }">
        <el-button link type="primary" size="small" @click="openPermDialog(row)">分配权限</el-button>
        <template v-if="!row.builtin">
          <el-button
            link
            type="danger"
            size="small"
            :loading="deletingRoleId === String(row.id ?? '')"
            @click="deleteRole(row)"
          >
            {{ t('common.delete') }}
          </el-button>
        </template>
        <el-tooltip v-else content="内置角色不可删除" placement="top">
          <span class="builtin-hint">内置</span>
        </el-tooltip>
      </template>
    </el-table-column>
  </DataTableCard>

  <!-- Create Role Dialog -->
  <FormDialog
    v-model:visible="createDialogVisible"
    :title="t('roles.create')"
    :loading="submitting"
    @submit="submitCreateRole"
  >
    <el-form ref="createFormRef" :model="createForm" :rules="createFormRules" label-position="top">
      <el-form-item :label="t('roles.name')" prop="name">
        <el-input v-model="createForm.name" placeholder="如：运维工程师" />
      </el-form-item>
      <el-collapse v-model="createMorePanels" class="more-collapse">
        <el-collapse-item title="更多设置" name="more">
          <el-form-item :label="t('common.remark')" prop="description">
            <el-input v-model="createForm.description" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" placeholder="填写角色用途、适用范围等备注" />
          </el-form-item>
        </el-collapse-item>
      </el-collapse>
    </el-form>
  </FormDialog>

  <!-- Permission Assignment Dialog -->
  <FormDialog
    v-model:visible="permDialogVisible"
    :title="permDialogTitle"
    :loading="savingPerms"
    submit-text="保存"
    @submit="savePermissions"
  >
    <div class="perm-dialog-header">
      <span class="perm-count-label">{{ selectedCountText }}</span>
      <span class="perm-help">勾选页面可选择全部操作，半选表示仅授权部分操作</span>
    </div>
    <el-tree
      ref="permissionTreeRef"
      class="permission-tree"
      :data="permissionTree"
      node-key="key"
      show-checkbox
      default-expand-all
      :props="{ children: 'children', label: 'label' }"
      @check="handlePermissionCheck"
    >
      <template #default="scope">
        <div
          v-if="scope?.data"
          :class="['permission-node', `permission-node--${scope.data.type}`]"
        >
          <span class="permission-node__label">{{ scope.data.label }}</span>
          <code v-if="scope.data.action" class="permission-node__action">{{ scope.data.action }}</code>
          <span v-if="scope.data.description" class="permission-node__description">
            {{ scope.data.description }}
          </span>
        </div>
      </template>
    </el-tree>
  </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue';
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus';

import DataTableCard from '@/components/DataTableCard.vue';
import FormDialog from '@/components/FormDialog.vue';
import StatusSwitch from '@/components/StatusSwitch.vue';
import * as api from '@/api/client';
import { TABLE_COLUMNS } from '@/config/tableColumns';
import { useI18n } from '@/i18n';

const { t } = useI18n();
const { apiClient } = api;

type RBACRoleRecord = api.RBACRoleRecord;
type RBACRolePayload = api.RBACRolePayload;
type RBACPermissionPageDefinition = api.RBACPermissionPageDefinition;

interface PermissionTreeNode {
  key: string;
  label: string;
  type: 'page' | 'action';
  action?: string;
  description?: string;
  children?: PermissionTreeNode[];
}

interface PermissionTreeRef {
  setCheckedKeys(keys: string[]): void;
  getCheckedKeys(leafOnly?: boolean): Array<string | number>;
}

const roles = ref<RBACRoleRecord[]>([]);
const loading = ref(false);
const error = ref('');
const submitting = ref(false);
const togglingRoleId = ref('');
const deletingRoleId = ref('');

// Pagination
const page = ref(1);
const pageSize = ref(50);
const total = ref(0);
const keyword = ref('');

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
const permissionTreeRef = ref<PermissionTreeRef>();
const catalogPages = ref<RBACPermissionPageDefinition[]>([]);
const rolePermCountMap = ref<Record<string, number>>({});

const permissionTree = computed<PermissionTreeNode[]>(() =>
  [...catalogPages.value]
    .sort((left, right) => left.order - right.order)
    .map(page => ({
      key: `page:${page.key}`,
      label: page.label,
      type: 'page' as const,
      children: page.actions
        .filter(action => action.assignable)
        .map(action => ({
          key: action.action,
          label: action.label,
          type: 'action' as const,
          action: action.action,
          description: action.description,
        })),
    })),
);

const permDialogTitle = computed(() =>
  currentPermRole.value ? `分配权限 — ${currentPermRole.value.name}` : '分配权限',
);

const selectedCountText = computed(() => `${selectedPerms.value.length} 项操作已选择`);

function rolePermCount(roleId: string | number | undefined): number {
  if (!roleId) return 0;
  return rolePermCountMap.value[String(roleId)] || 0;
}

async function loadRoleActionCounts() {
  const entries = await Promise.all(
    roles.value.map(async role => {
      const roleId = String(role.id ?? '');
      if (!roleId) return [roleId, 0] as const;
      try {
        const response = await apiClient.getRBACRoleActions(roleId);
        return [roleId, response.actions?.length ?? 0] as const;
      } catch {
        return [roleId, 0] as const;
      }
    }),
  );
  rolePermCountMap.value = Object.fromEntries(entries);
}

async function loadRoles() {
  loading.value = true;
  error.value = '';
  try {
    const res = await apiClient.getRBACRoles({
      page: page.value,
      page_size: pageSize.value,
      q: keyword.value || undefined,
    });
    roles.value = res.items ?? [];
    total.value = res.total ?? 0;
    await loadRoleActionCounts();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('roles.error.load');
  } finally {
    loading.value = false;
  }
}

function onSearch(q: string) {
  keyword.value = q;
  page.value = 1;
  loadRoles();
}

watch([page, pageSize], () => loadRoles());

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
async function toggleStatus(role: RBACRoleRecord, val: boolean) {
  const id = String(role.id ?? '');
  const newStatus = val ? 'active' : 'disabled';
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
      { confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel'), type: 'warning' },
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
  const roleId = String(role.id ?? '');
  try {
    const response = await apiClient.getRBACRoleActions(roleId);
    selectedPerms.value = response.actions ?? [];
    permDialogVisible.value = true;
    await nextTick();
    permissionTreeRef.value?.setCheckedKeys(selectedPerms.value);
  } catch (err) {
    currentPermRole.value = null;
    ElMessage.error(err instanceof Error ? err.message : '加载角色权限失败');
  }
}

watch(permDialogVisible, (val) => {
  if (!val) {
    selectedPerms.value = [];
    currentPermRole.value = null;
  }
});

function handlePermissionCheck() {
  const checkedKeys = permissionTreeRef.value?.getCheckedKeys(true) ?? [];
  selectedPerms.value = checkedKeys.map(String);
}

async function savePermissions() {
  if (!currentPermRole.value) return;
  const roleId = String(currentPermRole.value.id ?? '');

  savingPerms.value = true;
  try {
    const response = await apiClient.replaceRBACRoleActions(roleId, selectedPerms.value);
    selectedPerms.value = response.actions ?? [];
    rolePermCountMap.value = {
      ...rolePermCountMap.value,
      [roleId]: selectedPerms.value.length,
    };

    ElMessage.success(`权限已更新（${selectedPerms.value.length} 项）`);
    permDialogVisible.value = false;
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '保存权限失败');
  } finally {
    savingPerms.value = false;
  }
}

onMounted(async () => {
  await loadRoles();
  try {
    const response = await apiClient.getRBACCatalog();
    catalogPages.value = response.pages ?? [];
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载权限目录失败';
  }
});
</script>

<style scoped>
.perm-count { font-weight: 600; color: var(--el-color-primary); }
.builtin-hint { font-size: 12px; color: #94a3b8; cursor: default; }

.perm-dialog-header { margin-bottom: 14px; display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.perm-count-label { font-size: 13px; color: #475569; font-weight: 600; }
.perm-help { font-size: 12px; color: #94a3b8; }
.permission-tree { max-height: 56vh; overflow-y: auto; padding: 8px 4px; }
.permission-tree :deep(.el-tree-node__content) { min-height: 38px; height: auto; padding: 5px 8px; border-radius: 8px; }
.permission-tree :deep(.el-tree-node__content:hover) { background: #f1f5f9; }
.permission-node { min-width: 0; display: flex; align-items: center; gap: 10px; }
.permission-node--page .permission-node__label { font-weight: 700; color: #0f172a; }
.permission-node__label { color: #334155; font-size: 13px; white-space: nowrap; }
.permission-node__action { color: #0369a1; background: #e0f2fe; border-radius: 5px; padding: 2px 6px; font-size: 11px; }
.permission-node__description { color: #94a3b8; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.more-collapse { border-top: 1px solid #eef2f7; border-bottom: 0; }
.more-collapse :deep(.el-collapse-item__header) { color: #374151; font-size: 13px; font-weight: 700; }
.more-collapse :deep(.el-collapse-item__wrap) { border-bottom: 0; }
</style>
