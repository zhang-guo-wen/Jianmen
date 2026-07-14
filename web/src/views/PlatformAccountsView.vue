<template>
  <div class="view-stack">
    <div class="page-container">
    <DataTableCard
      :data="accounts"
      :loading="loading"
      :total="total"
      v-model:page="page"
      v-model:page-size="pageSize"
      :search-placeholder="t('platformAccounts.placeholder.search')"
      @search="onSearch"
    >
      <template #toolbar-extra>
        <el-button v-if="permission.canDo('platform_account:create')" type="primary" @click="openCreateDialog">{{ t('platformAccounts.action.new') }}</el-button>
      </template>
      <el-table-column :label="t('platformAccounts.column.platform')" width="120" show-overflow-tooltip>
        <template #default="{ row }">
          <el-tag size="small" type="primary" effect="plain">{{ row.platform_name }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('platformAccounts.column.name')" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">
          <el-button link type="primary" @click="openDetailDialog(row)">{{ row.name || row.username }}</el-button>
        </template>
      </el-table-column>
      <el-table-column :label="t('platformAccounts.column.username')" min-width="120" show-overflow-tooltip>
        <template #default="{ row }">{{ row.username }}</template>
      </el-table-column>
      <el-table-column :label="t('platformAccounts.column.group')" width="100" show-overflow-tooltip>
        <template #default="{ row }">{{ row.group || '-' }}</template>
      </el-table-column>
      <el-table-column :label="t('platformAccounts.column.category')" width="100" show-overflow-tooltip>
        <template #default="{ row }">{{ row.category || '-' }}</template>
      </el-table-column>
      <el-table-column :label="t('platformAccounts.column.visibility')" width="80" align="center">
        <template #default="{ row }">
          <span v-if="row.visibility === 'shared'">👥</span>
          <span v-else>🔒</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('platformAccounts.column.status')" width="70" align="center">
        <template #default="{ row }">
          <StatusSwitch
            v-if="permission.canDo('platform_account:update')"
            :model-value="row.status === 'active'"
            :loading="statusUpdatingId === row.id"
            @update:model-value="(val: boolean) => toggleStatus(row, val)"
          />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="240">
        <template #default="{ row }">
          <el-button v-if="permission.canDo('platform_account:use')" link type="primary" size="small" @click="openPasswordDialog(row)">{{ t('platformAccounts.action.viewPassword') }}</el-button>
          <el-button v-if="permission.canDo('platform_account:update')" link type="primary" size="small" @click="openEditDialog(row)">{{ t('platformAccounts.action.edit') }}</el-button>
          <el-button v-if="permission.canDo('platform_account:update')" link type="primary" size="small" @click="openShareDialog(row)">{{ t('platformAccounts.action.share') }}</el-button>
          <el-button v-if="permission.canDo('platform_account:delete')" link type="danger" size="small" @click="confirmDelete(row)">{{ t('platformAccounts.action.delete') }}</el-button>
        </template>
      </el-table-column>
    </DataTableCard>

    <!-- 创建/编辑弹窗 -->
    <FormDialog
      v-model:visible="dialogVisible"
      :title="editingId ? t('platformAccounts.dialog.editTitle') : t('platformAccounts.createTitle')"
      width="520px"
      :loading="submitting"
      @submit="submitForm"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item :label="t('platformAccounts.field.platform')" prop="platform_name" required>
          <el-select
            v-model="form.platform_name"
            allow-create
            filterable
            default-first-option
            :placeholder="t('platformAccounts.placeholder.platform')"
            style="width: 100%"
          >
            <el-option v-for="p in platformOptions" :key="p" :label="p" :value="p" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('platformAccounts.field.username')" prop="username" required>
          <el-input v-model="form.username" :placeholder="t('platformAccounts.placeholder.username')" />
        </el-form-item>
        <el-form-item :label="t('platformAccounts.field.password')">
          <el-input v-model="form.password" type="password" show-password :placeholder="editingId ? t('platformAccounts.placeholder.password') : ''" />
        </el-form-item>
        <el-collapse v-model="morePanels" class="more-collapse">
          <el-collapse-item :title="t('platformAccounts.moreSettings')" name="more">
            <el-form-item :label="t('platformAccounts.field.name')">
              <el-input v-model="form.name" :placeholder="t('platformAccounts.placeholder.name')" />
            </el-form-item>
            <el-form-item :label="t('platformAccounts.field.url')">
              <el-input v-model="form.url" placeholder="https://jenkins.example.com">
                <template #prefix v-if="form.url">
                  <el-link :href="form.url" target="_blank" :underline="false">🔗</el-link>
                </template>
              </el-input>
            </el-form-item>
            <el-form-item :label="t('platformAccounts.field.category')">
              <el-select v-model="form.category" allow-create clearable filterable default-first-option placeholder="CI/CD / 代码仓库 / 项目管理..." style="width: 100%">
                <el-option v-for="c in categoryOptions" :key="c" :label="c" :value="c" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('platformAccounts.field.group')">
              <el-select v-model="form.group" allow-create clearable filterable default-first-option :placeholder="t('platformAccounts.field.group')" style="width: 100%">
                <el-option v-for="g in groupOptions" :key="g" :label="g" :value="g" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('platformAccounts.field.remark')">
              <el-input v-model="form.remark" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" />
            </el-form-item>
            <el-form-item :label="t('platformAccounts.field.visibility')">
              <el-radio-group v-model="form.visibility">
                <el-radio value="private">{{ t('platformAccounts.visibility.private') }}</el-radio>
                <el-radio value="shared">{{ t('platformAccounts.visibility.shared') }}</el-radio>
              </el-radio-group>
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
    </FormDialog>

    <!-- 查看密码弹窗 -->
    <el-dialog v-model="passwordDialogVisible" :title="t('platformAccounts.password.title')" width="400px">
      <el-alert :title="t('platformAccounts.password.warning')" type="warning" :closable="false" show-icon style="margin-bottom: 16px" />
      <el-input v-model="revealedPassword" readonly>
        <template #append>
          <el-button @click="copyPassword">{{ t('platformAccounts.action.save') }}</el-button>
        </template>
      </el-input>
    </el-dialog>

    <!-- 共享管理弹窗 -->
    <el-dialog v-model="shareDialogVisible" :title="t('platformAccounts.share.title')" width="480px">
      <el-table :data="shares" size="small" style="margin-bottom: 16px">
        <el-table-column :label="t('platformAccounts.share.user')" prop="username" />
        <el-table-column :label="t('platformAccounts.share.role')" prop="role_name" />
        <el-table-column :label="t('platformAccounts.share.accessLevel')" width="100">
          <template #default="{ row }">{{ row.access_level === 'use' ? t('platformAccounts.share.canUse') : t('platformAccounts.share.viewOnly') }}</template>
        </el-table-column>
        <el-table-column :label="t('platformAccounts.column.actions')" width="80">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="revokeShare(row)">{{ t('platformAccounts.share.revoke') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-form :model="shareForm" inline>
        <el-form-item>
          <el-input v-model="shareForm.user_id" placeholder="User ID" style="width: 140px" />
        </el-form-item>
        <el-form-item>
          <el-radio-group v-model="shareForm.access_level" size="small">
            <el-radio-button value="view">{{ t('platformAccounts.share.viewOnly') }}</el-radio-button>
            <el-radio-button value="use">{{ t('platformAccounts.share.canUse') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="small" @click="addShare">{{ t('platformAccounts.share.add') }}</el-button>
        </el-form-item>
      </el-form>
    </el-dialog>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue';
import { useI18n } from '@/i18n';
import { ElMessage, ElMessageBox } from 'element-plus';
import type { FormInstance, FormRules } from 'element-plus';
import { apiClient } from '@/api/client';
import type { PlatformAccountView, PlatformAccountShareView } from '@/api/client';
import DataTableCard from '@/components/DataTableCard.vue';
import FormDialog from '@/components/FormDialog.vue';
import StatusSwitch from '@/components/StatusSwitch.vue';
import { usePermissionStore } from '@/stores/permission';

const { t } = useI18n();
const permission = usePermissionStore();

// List state
const accounts = ref<PlatformAccountView[]>([]);
const loading = ref(false);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const searchQuery = ref('');

// Form state
const dialogVisible = ref(false);
const editingId = ref('');
const submitting = ref(false);
const formRef = ref<FormInstance>();
const morePanels = ref<string[]>([]);

const form = reactive({
  platform_name: '',
  username: '',
  password: '',
  name: '',
  url: '',
  category: '',
  group: '',
  remark: '',
  visibility: 'private' as string,
});

const rules: FormRules = {
  platform_name: [{ required: true, message: () => t('platformAccounts.required.platform'), trigger: 'blur' }],
  username: [{ required: true, message: () => t('platformAccounts.required.username'), trigger: 'blur' }],
};

// Options
const platformOptions = ['Jenkins', 'GitLab', 'Jira', 'Confluence', 'Harbor', 'Nexus', 'SonarQube', 'Grafana', 'Kibana', 'Prometheus'];
const categoryOptions = ['CI/CD', '代码仓库', '项目管理', '监控', '日志', '制品库', '其他'];
const groupOptions = ref<string[]>([]);

// Password dialog
const passwordDialogVisible = ref(false);
const revealedPassword = ref('');

// Share dialog
const shareDialogVisible = ref(false);
const currentShareAccountId = ref('');
const shares = ref<PlatformAccountShareView[]>([]);
const shareForm = reactive({
  user_id: '',
  access_level: 'view',
});

// Status
const statusUpdatingId = ref('');

async function loadData() {
  loading.value = true;
  try {
    const resp = await apiClient.getPlatformAccounts({
      page: page.value,
      page_size: pageSize.value,
      q: searchQuery.value || undefined,
    });
    accounts.value = resp.items;
    total.value = resp.total;
  } catch (e: any) {
    ElMessage.error(t('platformAccounts.error.loadList'));
  } finally {
    loading.value = false;
  }
}

function onSearch(q: string) {
  searchQuery.value = q;
  page.value = 1;
  loadData();
}

function openCreateDialog() {
  editingId.value = '';
  Object.assign(form, { platform_name: '', username: '', password: '', name: '', url: '', category: '', group: '', remark: '', visibility: 'private' });
  morePanels.value = [];
  dialogVisible.value = true;
}

function openEditDialog(row: PlatformAccountView) {
  editingId.value = row.id || '';
  Object.assign(form, {
    platform_name: row.platform_name,
    username: row.username,
    password: '',
    name: row.name || '',
    url: row.url || '',
    category: row.category || '',
    group: row.group || '',
    remark: row.remark || '',
    visibility: row.visibility || 'private',
  });
  morePanels.value = [];
  dialogVisible.value = true;
}

async function submitForm() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;
  submitting.value = true;
  try {
    if (editingId.value) {
      await apiClient.updatePlatformAccount(editingId.value, form);
      ElMessage.success(t('platformAccounts.message.updated'));
    } else {
      await apiClient.createPlatformAccount(form);
      ElMessage.success(t('platformAccounts.message.created'));
    }
    dialogVisible.value = false;
    loadData();
  } catch (e: any) {
    ElMessage.error(e.message || t('platformAccounts.error.save'));
  } finally {
    submitting.value = false;
  }
}

async function confirmDelete(row: PlatformAccountView) {
  try {
    await ElMessageBox.confirm(
      t('platformAccounts.deleteConfirm').replace('{name}', row.name || row.username),
      t('platformAccounts.deleteTitle'),
      { type: 'warning' }
    );
    await apiClient.deletePlatformAccount(row.id || '');
    ElMessage.success(t('platformAccounts.message.deleted'));
    loadData();
  } catch {
    // cancelled
  }
}

async function toggleStatus(row: PlatformAccountView, active: boolean) {
  statusUpdatingId.value = row.id || '';
  try {
    await apiClient.updatePlatformAccount(row.id || '', { platform_name: row.platform_name, username: row.username, status: active ? 'active' : 'disabled' });
    row.status = active ? 'active' : 'disabled';
  } catch (e: any) {
    ElMessage.error(e.message);
  } finally {
    statusUpdatingId.value = '';
  }
}

async function openPasswordDialog(row: PlatformAccountView) {
  try {
    const resp = await apiClient.getPlatformAccountPassword(row.id || '');
    revealedPassword.value = resp.password;
    passwordDialogVisible.value = true;
  } catch (e: any) {
    ElMessage.error(e.message);
  }
}

function copyPassword() {
  navigator.clipboard.writeText(revealedPassword.value);
  ElMessage.success('已复制');
}

async function openShareDialog(row: PlatformAccountView) {
  currentShareAccountId.value = row.id || '';
  shareForm.user_id = '';
  shareForm.access_level = 'view';
  try {
    shares.value = await apiClient.getPlatformAccountShares(row.id || '');
  } catch {
    shares.value = [];
  }
  shareDialogVisible.value = true;
}

async function addShare() {
  if (!shareForm.user_id) return;
  try {
    await apiClient.createPlatformAccountShare(currentShareAccountId.value, {
      user_id: shareForm.user_id,
      access_level: shareForm.access_level,
    });
    shares.value = await apiClient.getPlatformAccountShares(currentShareAccountId.value);
    shareForm.user_id = '';
  } catch (e: any) {
    ElMessage.error(e.message);
  }
}

async function revokeShare(row: PlatformAccountShareView) {
  try {
    await apiClient.deletePlatformAccountShare(currentShareAccountId.value, row.id || '');
    shares.value = await apiClient.getPlatformAccountShares(currentShareAccountId.value);
  } catch (e: any) {
    ElMessage.error(e.message);
  }
}

function openDetailDialog(row: PlatformAccountView) {
  openEditDialog(row);
}

onMounted(loadData);
</script>
