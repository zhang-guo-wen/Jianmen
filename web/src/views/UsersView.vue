<template>
  <div class="view-stack">
    <div class="page-container">
    <DataTableCard
      :data="users"
      :loading="loading"
      :total="total"
      v-model:page="page"
      v-model:page-size="pageSize"
      search-placeholder="搜索用户名、显示名、邮箱..."
      @search="onSearch"
    >
      <template #toolbar-extra>
        <el-button type="primary" @click="openCreateDialog">{{ t('users.create') }}</el-button>
      </template>

      <el-alert v-if="error" :title="error" type="error" show-icon style="margin-bottom: 8px" />

      <el-table-column :label="t('users.username')" min-width="140">
        <template #default="{ row }">
          <strong>{{ row.username }}</strong>
        </template>
      </el-table-column>
      <el-table-column prop="display_name" :label="t('users.displayName')" min-width="120" show-overflow-tooltip />
      <el-table-column prop="email" :label="t('users.email')" min-width="180" show-overflow-tooltip />
      <el-table-column label="状态" width="80" align="center">
        <template #default="{ row }">
          <StatusSwitch
            v-if="!row.is_super_admin"
            :model-value="row.status === 'active'"
            :loading="togglingUserId === String(row.id ?? '')"
            @update:model-value="(val: boolean) => toggleStatus(row, val)"
          />
          <el-tag v-else type="warning" size="small">内置</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('users.lastLogin')" min-width="150">
        <template #default="{ row }">
          <span class="text-muted">{{ row.last_login_at || '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('users.roles')" min-width="180">
        <template #default="{ row }">
          <div class="role-tags" v-if="userAssignedRoles(row.id).length">
            <el-tag
              v-for="ur in userAssignedRoles(row.id)"
              :key="ur.id"
              size="small"
              closable
              class="role-tag"
              @close="removeRole(ur)"
            >
              {{ ur.role?.name || ur.role_id }}
            </el-tag>
          </div>
          <span v-else class="text-muted">{{ t('users.noRoles') }}</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="openEditDialog(row)">{{ t('common.edit') }}</el-button>
          <template v-if="!row.is_super_admin">
            <el-button link type="primary" size="small" @click="openRoleDialog(row)">{{ t('users.assignRole') }}</el-button>
            <el-button link type="danger" size="small" :loading="deletingUserId === String(row.id ?? '')" @click="deleteUser(row)">
              {{ t('common.delete') }}
            </el-button>
          </template>
          <el-tag v-else type="warning" size="small">超级管理员</el-tag>
        </template>
      </el-table-column>
    </DataTableCard>

    <!-- Role Assignment Dialog -->
    <el-dialog
      v-model="roleDialogVisible"
      :close-on-click-modal="!assigningRole"
      :title="roleDialogTitle"
      class="form-dialog"
      destroy-on-close
      width="min(420px, calc(100vw - 32px))"
    >
      <div v-if="roleDialogUser" class="role-assign-body">
        <div class="role-assign-current">
          <span class="role-assign-label">已分配角色</span>
          <div class="role-tags" style="margin-top: 6px">
            <template v-if="userAssignedRoles(roleDialogUser.id).length">
              <el-tag
                v-for="ur in userAssignedRoles(roleDialogUser.id)"
                :key="ur.id"
                size="small"
                closable
                class="role-tag"
                @close="removeRole(ur)"
              >
                {{ ur.role?.name || ur.role_id }}
              </el-tag>
            </template>
            <span v-else class="text-muted">{{ t('users.noRoles') }}</span>
          </div>
        </div>
        <el-divider />
        <div class="role-assign-add">
          <span class="role-assign-label">添加角色</span>
          <el-select
            v-model="selectedNewRoleId"
            :placeholder="t('users.selectRole')"
            style="width: 100%; margin-top: 6px"
            filterable
          >
            <el-option
              v-for="role in availableRoles"
              :key="role.id"
              :label="role.name"
              :value="String(role.id ?? '')"
            >
              <span>{{ role.name }}</span>
              <span v-if="role.builtin" class="text-muted" style="margin-left: 6px">(内置)</span>
            </el-option>
          </el-select>
          <el-button
            type="primary"
            :disabled="!selectedNewRoleId"
            :loading="assigningRole"
            style="margin-top: 10px; width: 100%"
            @click="assignRole"
          >
            {{ t('users.assignRole') }}
          </el-button>
        </div>
      </div>
    </el-dialog>

    <!-- Create/Edit Dialog -->
    <FormDialog
      v-model:visible="dialogVisible"
      :title="editingUser ? t('users.edit') : t('users.create')"
      :loading="submitting"
      @submit="submitForm"
    >
      <el-form ref="formRef" :model="form" :rules="formRules" label-position="top">
        <el-form-item :label="t('users.username')" prop="username">
          <el-input v-model="form.username" :disabled="!!editingUser" :placeholder="t('users.username')" />
        </el-form-item>
        <el-form-item v-if="!editingUser" :label="t('users.password')" prop="password">
          <el-input v-model="form.password" type="password" placeholder="至少 8 位" />
        </el-form-item>
        <el-collapse v-model="morePanels" class="more-collapse">
          <el-collapse-item title="更多设置" name="more">
            <el-form-item :label="t('users.displayName')" prop="display_name">
              <el-input v-model="form.display_name" placeholder="可选" />
            </el-form-item>
            <el-form-item :label="t('users.email')" prop="email">
              <el-input v-model="form.email" type="email" placeholder="user@example.com" />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
    </FormDialog>
  </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus';

import DataTableCard from '@/components/DataTableCard.vue';
import FormDialog from '@/components/FormDialog.vue';
import StatusSwitch from '@/components/StatusSwitch.vue';
import * as api from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();

// ── List state ──
const users = ref<api.UserRecord[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const keyword = ref('');
const loading = ref(false);
const error = ref('');

// ── Form state ──
const submitting = ref(false);
const togglingUserId = ref<string>('');
const deletingUserId = ref<string>('');
const dialogVisible = ref(false);
const editingUser = ref<api.UserRecord | null>(null);
const morePanels = ref<string[]>([]);
const formRef = ref<FormInstance>();

// ── Role assignment ──
const roles = ref<api.RBACRoleRecord[]>([]);
const userRoles = ref<api.RBACUserRoleRecord[]>([]);
const roleDialogVisible = ref(false);
const roleDialogUser = ref<api.UserRecord | null>(null);
const assigningRole = ref(false);
const selectedNewRoleId = ref('');

const form = reactive<api.UserPayload & { password?: string }>({
  username: '',
  password: '',
  display_name: '',
  email: '',
});

const formRules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }, { min: 6, message: '密码至少 6 位', trigger: 'blur' }],
};

// ── Role assignment helpers ──
function userAssignedRoles(userId: string | number | undefined): api.RBACUserRoleRecord[] {
  if (!userId) return [];
  return userRoles.value.filter(ur => String(ur.user_id) === String(userId));
}

const roleDialogTitle = computed(() =>
  roleDialogUser.value ? `分配角色 — ${roleDialogUser.value.username}` : '分配角色'
);

const availableRoles = computed(() =>
  roles.value.filter(r => {
    if (!roleDialogUser.value) return false;
    const assignedIds = new Set(
      userAssignedRoles(roleDialogUser.value.id).map(ur => String(ur.role_id))
    );
    return !assignedIds.has(String(r.id ?? ''));
  })
);

function openRoleDialog(user: api.UserRecord) {
  roleDialogUser.value = user;
  selectedNewRoleId.value = '';
  roleDialogVisible.value = true;
}

async function assignRole() {
  if (!roleDialogUser.value || !selectedNewRoleId.value) return;
  assigningRole.value = true;
  try {
    await api.apiClient.createRBACUserRole({
      user_id: String(roleDialogUser.value.id ?? ''),
      role_id: selectedNewRoleId.value,
    });
    ElMessage.success('角色已分配');
    selectedNewRoleId.value = '';
    await loadUserRoles();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '角色分配失败');
  } finally {
    assigningRole.value = false;
  }
}

async function removeRole(userRole: api.RBACUserRoleRecord) {
  const id = String(userRole.id ?? '');
  try {
    await api.apiClient.deleteRBACUserRole(id);
    ElMessage.success('角色已移除');
    await loadUserRoles();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '移除角色失败');
  }
}

async function loadUserRoles() {
  try {
    const res = await api.apiClient.getRBACUserRoles();
    userRoles.value = (res as api.PageResponse<api.RBACUserRoleRecord>).items ?? [];
  } catch { /* non-critical */ }
}

async function loadRoles() {
  try {
    const res = await api.apiClient.getRBACRoles();
    roles.value = (res as api.PageResponse<api.RBACRoleRecord>).items ?? [];
  } catch { /* non-critical */ }
}

async function loadUsers() {
  loading.value = true;
  error.value = '';
  try {
    const userRes = await api.apiClient.getUsers({
      page: page.value,
      page_size: pageSize.value,
      q: keyword.value.trim() || undefined,
    });
    users.value = userRes.items ?? [];
    total.value = userRes.total ?? 0;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('users.error.load');
  } finally {
    loading.value = false;
  }
}

function resetForm() {
  form.username = '';
  form.password = '';
  form.display_name = '';
  form.email = '';
  morePanels.value = [];
}

function openCreateDialog() {
  editingUser.value = null;
  resetForm();
  dialogVisible.value = true;
}

function openEditDialog(user: api.UserRecord) {
  editingUser.value = user;
  form.username = String(user.username ?? '');
  form.display_name = String(user.display_name ?? '');
  form.email = String(user.email ?? '');
  form.password = '';
  morePanels.value = ['more'];
  dialogVisible.value = true;
}

async function submitForm() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;

  submitting.value = true;
  try {
    if (editingUser.value) {
      const payload: api.UserPayload = {};
      if (form.display_name !== (editingUser.value.display_name ?? '')) {
        payload.display_name = form.display_name;
      }
      if (form.email !== (editingUser.value.email ?? '')) {
        payload.email = form.email;
      }
      await api.apiClient.updateUser(String(editingUser.value.id ?? ''), payload);
      ElMessage.success(t('users.updated'));
    } else {
      await api.apiClient.createUser({
        username: form.username,
        password: form.password,
        display_name: form.display_name || undefined,
        email: form.email || undefined,
      });
      ElMessage.success(t('users.created'));
    }
    dialogVisible.value = false;
    await loadUsers();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : (editingUser.value ? t('users.error.update') : t('users.error.create')));
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(user: api.UserRecord, _newVal: boolean) {
  const id = String(user.id ?? '');
  const newStatus = user.status === 'disabled' ? 'active' : 'disabled';
  togglingUserId.value = id;
  try {
    await api.apiClient.updateUser(id, { status: newStatus });
    ElMessage.success(t('users.statusToggled'));
    await loadUsers();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('users.error.update'));
  } finally {
    togglingUserId.value = '';
  }
}

async function deleteUser(user: api.UserRecord) {
  const id = String(user.id ?? '');
  try {
    await ElMessageBox.confirm(
      `确认删除用户 "${user.username}"？`,
      t('common.delete'),
      { confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel'), type: 'warning' }
    );
  } catch {
    return;
  }
  deletingUserId.value = id;
  try {
    await api.apiClient.deleteUser(id);
    ElMessage.success(t('users.deleted'));
    await loadUsers();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('users.error.delete'));
  } finally {
    deletingUserId.value = '';
  }
}

function onSearch(q: string) {
  keyword.value = q;
  page.value = 1;
  loadUsers();
}

onMounted(async () => {
  await Promise.all([
    loadUsers(),
    loadUserRoles(),
    loadRoles(),
  ]);
});
</script>

<style scoped>
.text-muted { color: #64748b; font-size: 12px; }
.role-tags { display: flex; flex-wrap: wrap; gap: 5px; align-items: center; }
.role-tag { margin: 0; }
.role-assign-body { display: flex; flex-direction: column; }
.role-assign-label { font-size: 13px; font-weight: 600; color: #374151; }

.more-collapse { border-top: 1px solid #eef2f7; border-bottom: 0; }
.more-collapse :deep(.el-collapse-item__header) { color: #374151; font-size: 13px; font-weight: 700; }
.more-collapse :deep(.el-collapse-item__wrap) { border-bottom: 0; }
:global(.form-dialog .el-dialog__body) { max-height: min(66vh, 620px); overflow-y: auto; padding-right: 22px; }
</style>
