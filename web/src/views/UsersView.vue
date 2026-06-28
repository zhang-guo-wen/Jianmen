<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-input
        v-model="searchQuery"
        clearable
        :placeholder="t('users.search')"
        class="search-input"
        @input="onSearch"
      />
      <el-button type="primary" @click="openCreateDialog">{{ t('users.create') }}</el-button>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon />

    <el-table v-else v-loading="loading" :data="filteredUsers" height="420" row-key="id">
      <el-table-column :label="t('users.username')" min-width="140">
        <template #default="{ row }">
          <div>
            <strong>{{ row.username }}</strong>
            <div class="col-mono">{{ row.id }}</div>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="display_name" :label="t('users.displayName')" min-width="120" />
      <el-table-column prop="email" :label="t('users.email')" min-width="180" />
      <el-table-column :label="t('users.status')" width="100">
        <template #default="{ row }">
          <el-tag :type="row.status === 'disabled' ? 'info' : 'success'" size="small">
            {{ row.status === 'active' ? '已启用' : '已禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('users.lastLogin')" min-width="150">
        <template #default="{ row }">
          <span class="text-muted">{{ row.last_login_at || '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" fixed="right" width="220">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEditDialog(row)">{{ t('common.edit') }}</el-button>
          <el-button
            link
            :type="row.status === 'disabled' ? 'success' : 'warning'"
            :loading="togglingUserId === row.id"
            @click="toggleStatus(row)"
          >
            {{ row.status === 'disabled' ? '启用' : '禁用' }}
          </el-button>
          <el-button link type="danger" :loading="deletingUserId === row.id" @click="deleteUser(row)">
            {{ t('common.delete') }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!loading && !filteredUsers.length && !error" :description="t('users.empty')" />

    <!-- Create/Edit Dialog -->
    <el-dialog
      v-model="dialogVisible"
      :close-on-click-modal="!submitting"
      :title="editingUser ? t('users.edit') : t('users.create')"
      class="form-dialog"
      destroy-on-close
      width="min(440px, calc(100vw - 32px))"
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
      <template #footer>
        <el-button :disabled="submitting" @click="dialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button :loading="submitting" type="primary" @click="submitForm">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus';

import { apiClient, type UserRecord, type UserPayload } from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();

const users = ref<UserRecord[]>([]);
const loading = ref(false);
const error = ref('');
const searchQuery = ref('');
const submitting = ref(false);
const togglingUserId = ref<string | number>('');
const deletingUserId = ref<string | number>('');
const dialogVisible = ref(false);
const editingUser = ref<UserRecord | null>(null);
const morePanels = ref<string[]>([]);
const formRef = ref<FormInstance>();

const form = reactive<UserPayload & { password?: string }>({
  username: '',
  password: '',
  display_name: '',
  email: '',
});

const formRules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }, { min: 6, message: '密码至少 6 位', trigger: 'blur' }],
};

const filteredUsers = computed(() => {
  const q = searchQuery.value.trim().toLowerCase();
  if (!q) return users.value;
  return users.value.filter(u =>
    [u.username, u.display_name, u.email].some(v => String(v ?? '').toLowerCase().includes(q))
  );
});

async function loadUsers() {
  loading.value = true;
  error.value = '';
  try {
    const res = await apiClient.getUsers();
    users.value = Array.isArray(res) ? res : (res as any).data ?? [];
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

function openEditDialog(user: UserRecord) {
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
      const payload: UserPayload = {};
      if (form.display_name !== (editingUser.value.display_name ?? '')) {
        payload.display_name = form.display_name;
      }
      if (form.email !== (editingUser.value.email ?? '')) {
        payload.email = form.email;
      }
      await apiClient.updateUser(String(editingUser.value.id ?? ''), payload);
      ElMessage.success(t('users.updated'));
    } else {
      await apiClient.createUser({
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

async function toggleStatus(user: UserRecord) {
  const id = String(user.id ?? '');
  const newStatus = user.status === 'disabled' ? 'active' : 'disabled';
  togglingUserId.value = id;
  try {
    await apiClient.updateUser(id, { status: newStatus });
    ElMessage.success(t('users.statusToggled'));
    await loadUsers();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('users.error.update'));
  } finally {
    togglingUserId.value = '';
  }
}

async function deleteUser(user: UserRecord) {
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
    await apiClient.deleteUser(id);
    ElMessage.success(t('users.deleted'));
    await loadUsers();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('users.error.delete'));
  } finally {
    deletingUserId.value = '';
  }
}

function onSearch() {
  // reactive filtering via computed
}

onMounted(loadUsers);
</script>

<style scoped>
.view-stack { display: grid; gap: 14px; }
.toolbar { display: flex; gap: 10px; align-items: center; }
.search-input { max-width: 280px; }
.col-mono { font-family: "SF Mono", "Cascadia Code", "Consolas", monospace; font-size: 11px; color: #64748b; }
.text-muted { color: #64748b; font-size: 12px; }
.more-collapse { border-top: 1px solid #eef2f7; border-bottom: 0; }
.more-collapse :deep(.el-collapse-item__header) { color: #374151; font-size: 13px; font-weight: 700; }
.more-collapse :deep(.el-collapse-item__wrap) { border-bottom: 0; }
:global(.form-dialog .el-dialog__body) { max-height: min(66vh, 620px); overflow-y: auto; padding-right: 22px; }
</style>
