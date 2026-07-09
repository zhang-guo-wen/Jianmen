<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-tabs v-model="activeTab" class="page-tabs" @tab-change="onTabChange">
        <el-tab-pane :label="t('rbac.tab.roles')" name="roles" />
        <el-tab-pane :label="t('rbac.tab.permissions')" name="permissions" />
        <el-tab-pane :label="t('rbac.tab.userRoles')" name="userRoles" />
        <el-tab-pane :label="t('rbac.tab.rolePermissions')" name="rolePermissions" />
        <el-tab-pane :label="t('rbac.tab.effective')" name="effective" />
      </el-tabs>
      <el-button :loading="anyLoading" type="primary" @click="refreshActiveTab">
        {{ t('common.refresh') }}
      </el-button>
    </div>

    <!-- Tab 1: Roles -->
    <div v-if="activeTab === 'roles'" class="page-container">
      <el-alert v-if="errors.roles" :title="errors.roles" type="error" show-icon style="margin-bottom: 12px" />
      <DataTableCard
        :data="roles"
        :loading="loading.roles"
        :total="roleTotal"
        v-model:page="rolePage"
        v-model:page-size="rolePageSize"
        row-key="id"
        :search-placeholder="t('rbac.placeholder.searchRoles')"
        @search="onRoleSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="openRoleDialog">{{ t('rbac.action.newRole') }}</el-button>
        </template>
        <el-table-column prop="id" :label="t('common.id')" min-width="150" />
        <el-table-column prop="name" :label="t('common.name')" min-width="160" />
        <el-table-column prop="description" :label="t('common.description')" min-width="220" show-overflow-tooltip />
        <el-table-column :label="t('common.status')" width="130">
          <template #default="{ row }">
            <el-tag :type="row.status === 'disabled' ? 'info' : 'success'">
              {{ row.status || 'active' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('rbac.column.builtin')" width="120">
          <template #default="{ row }">
            <el-tag :type="row.builtin ? 'warning' : 'info'">
              {{ row.builtin ? t('common.yes') : t('common.no') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" fixed="right" width="200">
          <template #default="{ row }">
            <el-button
              :disabled="row.builtin"
              :loading="statusUpdatingRoleId === recordId(row)"
              link
              size="small"
              :type="row.status === 'disabled' ? 'success' : 'warning'"
              @click="toggleRoleStatus(row)"
            >
              {{ row.status === 'disabled' ? '启用' : '禁用' }}
            </el-button>
            <el-button
              :disabled="row.builtin"
              :loading="deleting.roleId === recordId(row)"
              link
              size="small"
              type="danger"
              @click="deleteRole(row)"
            >
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>
    </div>

    <!-- Tab 2: Permissions -->
    <div v-if="activeTab === 'permissions'" class="page-container">
      <el-alert v-if="errors.permissions" :title="errors.permissions" type="error" show-icon style="margin-bottom: 12px" />
      <DataTableCard
        :data="permissions"
        :loading="loading.permissions"
        :total="permissionTotal"
        v-model:page="permissionPage"
        v-model:page-size="permissionPageSize"
        row-key="id"
        :search-placeholder="t('rbac.placeholder.searchPermissions')"
        @search="onPermissionSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="openPermissionDialog">{{ t('rbac.action.newPermission') }}</el-button>
        </template>
        <el-table-column prop="id" :label="t('common.id')" min-width="150" />
        <el-table-column prop="name" :label="t('common.name')" min-width="150" />
        <el-table-column prop="action" :label="t('rbac.column.action')" min-width="170" />
        <el-table-column prop="resource_type" :label="t('rbac.column.resourceType')" min-width="150" />
        <el-table-column prop="resource_id" :label="t('rbac.column.resourceId')" min-width="150" />
        <el-table-column :label="t('rbac.column.effect')" width="120">
          <template #default="{ row }">
            <el-tag :type="row.effect === 'deny' ? 'danger' : 'success'">
              {{ row.effect || 'allow' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" :label="t('common.description')" min-width="220" show-overflow-tooltip />
        <el-table-column :label="t('common.actions')" fixed="right" width="120">
          <template #default="{ row }">
            <el-button
              :loading="deleting.permissionId === recordId(row)"
              link
              size="small"
              type="danger"
              @click="deletePermission(row)"
            >
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>
    </div>

    <!-- Tab 3: User-Role Bindings -->
    <div v-if="activeTab === 'userRoles'" class="page-container">
      <el-form
        ref="userRoleFormRef"
        :model="userRoleForm"
        :rules="userRoleRules"
        class="inline-form"
        label-position="top"
      >
        <el-form-item :label="t('rbac.field.user')" prop="user_id">
          <el-select
            v-model="userRoleForm.user_id"
            allow-create
            default-first-option
            filterable
            :placeholder="t('rbac.placeholder.userId')"
          >
            <el-option
              v-for="user in users"
              :key="userValue(user)"
              :label="userLabel(user)"
              :value="userValue(user)"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('rbac.field.role')" prop="role_id">
          <el-select
            v-model="userRoleForm.role_id"
            allow-create
            default-first-option
            filterable
            :placeholder="t('rbac.placeholder.roleId')"
          >
            <el-option v-for="role in allRoles" :key="recordId(role)" :label="roleLabel(role)" :value="recordId(role)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('rbac.field.expiresAt')" prop="expires_at">
          <el-input v-model="userRoleForm.expires_at" :placeholder="t('rbac.placeholder.expiresAt')" />
        </el-form-item>
        <el-form-item class="inline-form-actions">
          <el-button :loading="submitting.userRole" type="primary" @click="submitUserRole">
            {{ t('common.create') }}
          </el-button>
        </el-form-item>
      </el-form>
      <el-alert v-if="errors.userRoles" :title="errors.userRoles" type="error" show-icon style="margin-bottom: 12px" />
      <DataTableCard
        :data="userRoles"
        :loading="loading.userRoles"
        :total="userRoleTotal"
        v-model:page="userRolePage"
        v-model:page-size="userRolePageSize"
        row-key="id"
        :show-search="false"
      >
        <template #toolbar-extra>
          <el-button :loading="loading.userRoles" @click="fetchUserRoles">{{ t('common.refresh') }}</el-button>
        </template>
        <el-table-column prop="id" :label="t('common.id')" min-width="150" />
        <el-table-column :label="t('rbac.field.user')" min-width="180">
          <template #default="{ row }">
            {{ userNameForId(row.user_id) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('rbac.field.role')" min-width="180">
          <template #default="{ row }">
            {{ roleNameForId(row.role_id) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('rbac.field.expiresAt')" min-width="180">
          <template #default="{ row }">{{ formatTime(row.expires_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('common.createdAt')" min-width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" fixed="right" width="120">
          <template #default="{ row }">
            <el-button
              :loading="deleting.userRoleId === recordId(row)"
              link
              size="small"
              type="danger"
              @click="deleteUserRole(row)"
            >
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>
    </div>

    <!-- Tab 4: Role-Permission Bindings -->
    <div v-if="activeTab === 'rolePermissions'" class="page-container">
      <el-form
        ref="rolePermissionFormRef"
        :model="rolePermissionForm"
        :rules="rolePermissionRules"
        class="inline-form"
        label-position="top"
      >
        <el-form-item :label="t('rbac.field.role')" prop="role_id">
          <el-select
            v-model="rolePermissionForm.role_id"
            allow-create
            default-first-option
            filterable
            :placeholder="t('rbac.placeholder.roleId')"
          >
            <el-option v-for="role in allRoles" :key="recordId(role)" :label="roleLabel(role)" :value="recordId(role)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('rbac.field.permission')" prop="permission_id">
          <el-select
            v-model="rolePermissionForm.permission_id"
            allow-create
            default-first-option
            filterable
            :placeholder="t('rbac.placeholder.permissionId')"
          >
            <el-option
              v-for="permission in allPermissions"
              :key="recordId(permission)"
              :label="permissionLabel(permission)"
              :value="recordId(permission)"
            />
          </el-select>
        </el-form-item>
        <el-form-item class="inline-form-actions">
          <el-button :loading="submitting.rolePermission" type="primary" @click="submitRolePermission">
            {{ t('common.create') }}
          </el-button>
        </el-form-item>
      </el-form>
      <el-alert v-if="errors.rolePermissions" :title="errors.rolePermissions" type="error" show-icon style="margin-bottom: 12px" />
      <DataTableCard
        :data="rolePermissions"
        :loading="loading.rolePermissions"
        :total="rolePermissionTotal"
        v-model:page="rolePermissionPage"
        v-model:page-size="rolePermissionPageSize"
        row-key="id"
        :show-search="false"
      >
        <template #toolbar-extra>
          <el-button :loading="loading.rolePermissions" @click="fetchRolePermissions">{{ t('common.refresh') }}</el-button>
        </template>
        <el-table-column prop="id" :label="t('common.id')" min-width="150" />
        <el-table-column :label="t('rbac.field.role')" min-width="180">
          <template #default="{ row }">
            {{ roleNameForId(row.role_id) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('rbac.field.permission')" min-width="260">
          <template #default="{ row }">
            {{ permissionNameForId(row.permission_id) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('common.createdAt')" min-width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" fixed="right" width="120">
          <template #default="{ row }">
            <el-button
              :loading="deleting.rolePermissionId === recordId(row)"
              link
              size="small"
              type="danger"
              @click="deleteRolePermission(row)"
            >
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>
    </div>

    <!-- Tab 5: Effective Check -->
    <el-card v-if="activeTab === 'effective'" shadow="never">
      <template #header>
        <span>{{ t('rbac.title.effectiveCheck') }}</span>
      </template>
      <el-form
        ref="effectiveFormRef"
        :model="effectiveForm"
        :rules="effectiveRules"
        class="inline-form effective-form"
        label-position="top"
      >
        <el-form-item :label="t('rbac.field.user')" prop="user_id">
          <el-select
            v-model="effectiveForm.user_id"
            allow-create
            default-first-option
            filterable
            :placeholder="t('rbac.placeholder.userId')"
          >
            <el-option
              v-for="user in users"
              :key="userValue(user)"
              :label="userLabel(user)"
              :value="userValue(user)"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('rbac.column.action')" prop="action">
          <el-input v-model="effectiveForm.action" placeholder="session:connect" />
        </el-form-item>
        <el-form-item :label="t('quickConnect.column.resource')">
          <el-select
            :model-value="resourceSelectionValue(effectiveForm)"
            clearable
            filterable
            :loading="loading.resources"
            :placeholder="t('rbac.placeholder.anyResource')"
            @change="selectEffectiveResource"
          >
            <el-option-group
              v-for="group in resourceOptionGroups"
              :key="group.label"
              :label="group.label"
            >
              <el-option
                v-for="option in group.options"
                :key="option.value"
                :label="option.label"
                :value="option.value"
              >
                <div class="resource-option">
                  <span class="resource-option-main">{{ option.name }}</span>
                  <span class="resource-option-meta">{{ option.resource_type }}:{{ option.resource_id }}</span>
                </div>
              </el-option>
            </el-option-group>
          </el-select>
        </el-form-item>
        <el-form-item :label="t('rbac.column.resourceType')" prop="resource_type">
          <el-input v-model="effectiveForm.resource_type" placeholder="host_account" />
        </el-form-item>
        <el-form-item :label="t('rbac.column.resourceId')" prop="resource_id">
          <el-input v-model="effectiveForm.resource_id" :placeholder="t('rbac.placeholder.anyResource')" />
        </el-form-item>
        <el-form-item class="inline-form-actions">
          <el-button :loading="submitting.effective" type="primary" @click="submitEffectiveCheck">
            {{ t('rbac.action.check') }}
          </el-button>
        </el-form-item>
      </el-form>
      <el-alert v-if="errors.resources" :title="errors.resources" type="warning" show-icon />
      <el-alert v-if="errors.effective" :title="errors.effective" type="error" show-icon />
      <div v-if="effectiveResult" class="result-panel">
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('rbac.column.decision')">
            <el-tag :type="effectiveAllowed ? 'success' : 'danger'">
              {{ effectiveDecisionLabel }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('rbac.column.reason')">
            {{ effectiveResult.reason || t('common.none') }}
          </el-descriptions-item>
        </el-descriptions>
        <pre class="json-preview">{{ JSON.stringify(effectiveResult, null, 2) }}</pre>
      </div>
      <el-empty v-else-if="!errors.effective" :description="t('rbac.empty.effective')" />
    </el-card>

    <!-- FormDialog: 新增角色 -->
    <FormDialog
      v-model:visible="roleDialogVisible"
      :title="t('rbac.action.newRole')"
      width="min(440px, calc(100vw - 32px))"
      :loading="submitting.role"
      @submit="submitRole"
    >
      <el-form ref="roleFormRef" :model="roleForm" :rules="roleRules" label-position="top">
        <el-form-item :label="t('common.name')" prop="name">
          <el-input v-model="roleForm.name" :placeholder="t('rbac.placeholder.roleName')" />
        </el-form-item>
        <el-collapse v-model="roleMorePanels" class="more-collapse">
          <el-collapse-item title="更多设置" name="more">
            <el-form-item :label="t('common.id')" prop="id">
              <el-input v-model="roleForm.id" :placeholder="t('rbac.placeholder.optionalId')" />
            </el-form-item>
            <el-form-item :label="t('common.description')" prop="description">
              <el-input v-model="roleForm.description" :autosize="{ minRows: 3, maxRows: 5 }" type="textarea" />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
    </FormDialog>

    <!-- FormDialog: 新增权限 -->
    <FormDialog
      v-model:visible="permissionDialogVisible"
      :title="t('rbac.action.newPermission')"
      width="min(580px, calc(100vw - 32px))"
      :loading="submitting.permission"
      @submit="submitPermission"
    >
      <el-form ref="permissionFormRef" :model="permissionForm" :rules="permissionRules" label-position="top">
        <div class="form-grid">
          <el-form-item :label="t('rbac.column.action')" prop="action">
            <el-input v-model="permissionForm.action" placeholder="session:connect" />
          </el-form-item>
          <el-form-item :label="t('rbac.column.effect')" prop="effect">
            <el-select v-model="permissionForm.effect">
              <el-option label="allow" value="allow" />
              <el-option label="deny" value="deny" />
            </el-select>
          </el-form-item>
          <el-form-item class="form-grid-full" :label="t('quickConnect.column.resource')">
            <el-select
              :model-value="resourceSelectionValue(permissionForm)"
              clearable
              filterable
              :loading="loading.resources"
              :placeholder="t('rbac.placeholder.anyResource')"
              @change="selectPermissionResource"
            >
              <el-option-group
                v-for="group in resourceOptionGroups"
                :key="group.label"
                :label="group.label"
              >
                <el-option
                  v-for="option in group.options"
                  :key="option.value"
                  :label="option.label"
                  :value="option.value"
                >
                  <div class="resource-option">
                    <span class="resource-option-main">{{ option.name }}</span>
                    <span class="resource-option-meta">{{ option.resource_type }}:{{ option.resource_id }}</span>
                  </div>
                </el-option>
              </el-option-group>
            </el-select>
          </el-form-item>
        </div>
        <el-collapse v-model="permissionMorePanels" class="more-collapse">
          <el-collapse-item title="更多设置" name="more">
            <div class="form-grid">
              <el-form-item :label="t('common.id')" prop="id">
                <el-input v-model="permissionForm.id" :placeholder="t('rbac.placeholder.optionalId')" />
              </el-form-item>
              <el-form-item :label="t('common.name')" prop="name">
                <el-input v-model="permissionForm.name" :placeholder="t('rbac.placeholder.permissionName')" />
              </el-form-item>
              <el-form-item :label="t('rbac.column.resourceType')" prop="resource_type">
                <el-input v-model="permissionForm.resource_type" placeholder="host_account" />
              </el-form-item>
              <el-form-item :label="t('rbac.column.resourceId')" prop="resource_id">
                <el-input v-model="permissionForm.resource_id" :placeholder="t('rbac.placeholder.anyResource')" />
              </el-form-item>
              <el-form-item class="form-grid-full" :label="t('common.description')" prop="description">
                <el-input
                  v-model="permissionForm.description"
                  :autosize="{ minRows: 3, maxRows: 5 }"
                  type="textarea"
                />
              </el-form-item>
            </div>
          </el-collapse-item>
        </el-collapse>
      </el-form>
      <el-alert v-if="errors.resources" :title="errors.resources" type="warning" show-icon style="margin-top: 8px" />
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue';
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus';

import {
  apiClient,
  type DBAccountRecord,
  type DatabaseInstanceView,
  type PageResponse,
  type RBACEffectiveCheckPayload,
  type RBACEffectiveCheckResult,
  type RBACPermissionPayload,
  type RBACPermissionRecord,
  type RBACRolePayload,
  type RBACRolePermissionPayload,
  type RBACRolePermissionRecord,
  type RBACRoleRecord,
  type RBACUserRolePayload,
  type RBACUserRoleRecord,
  type TargetRecord,
  type UserRecord
} from '@/api/client';
import DataTableCard from '@/components/DataTableCard.vue';
import FormDialog from '@/components/FormDialog.vue';
import { useI18n } from '@/i18n';

type RBACTab = 'roles' | 'permissions' | 'userRoles' | 'rolePermissions' | 'effective';

interface ResourceIdentity {
  resource_type: string;
  resource_id: string;
}

interface ResourceForm {
  resource_type?: string;
  resource_id?: string;
}

interface ResourceOption extends ResourceIdentity {
  value: string;
  label: string;
  name: string;
}

interface ResourceOptionGroup {
  label: string;
  options: ResourceOption[];
}

function formatTime(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '-'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

const { t } = useI18n();
const activeTab = ref<RBACTab>('roles');

// ── Full lists (for form selects) ──
const users = ref<UserRecord[]>([]);
const allRoles = ref<RBACRoleRecord[]>([]);
const allPermissions = ref<RBACPermissionRecord[]>([]);
const allRolesLoaded = ref(false);
const allPermissionsLoaded = ref(false);

// ── Paginated list state ──
const roles = ref<RBACRoleRecord[]>([]);
const roleTotal = ref(0);
const rolePage = ref(1);
const rolePageSize = ref(20);
const roleKeyword = ref('');

const permissions = ref<RBACPermissionRecord[]>([]);
const permissionTotal = ref(0);
const permissionPage = ref(1);
const permissionPageSize = ref(20);
const permissionKeyword = ref('');

const userRoles = ref<RBACUserRoleRecord[]>([]);
const userRoleTotal = ref(0);
const userRolePage = ref(1);
const userRolePageSize = ref(20);

const rolePermissions = ref<RBACRolePermissionRecord[]>([]);
const rolePermissionTotal = ref(0);
const rolePermissionPage = ref(1);
const rolePermissionPageSize = ref(20);

// ── Resource state (for resource picker) ──
const targets = ref<TargetRecord[]>([]);
const dbInstances = ref<DatabaseInstanceView[]>([]);
const dbAccounts = ref<DBAccountRecord[]>([]);

const effectiveResult = ref<RBACEffectiveCheckResult | null>(null);

const loading = reactive({
  users: false,
  resources: false,
  roles: false,
  permissions: false,
  userRoles: false,
  rolePermissions: false
});
const submitting = reactive({
  role: false,
  permission: false,
  userRole: false,
  rolePermission: false,
  effective: false
});
const deleting = reactive({
  roleId: '',
  permissionId: '',
  userRoleId: '',
  rolePermissionId: ''
});
const errors = reactive({
  users: '',
  resources: '',
  roles: '',
  permissions: '',
  userRoles: '',
  rolePermissions: '',
  effective: ''
});

const roleDialogVisible = ref(false);
const permissionDialogVisible = ref(false);
const roleMorePanels = ref<string[]>([]);
const permissionMorePanels = ref<string[]>([]);
const statusUpdatingRoleId = ref('');
const roleFormRef = ref<FormInstance>();
const permissionFormRef = ref<FormInstance>();
const userRoleFormRef = ref<FormInstance>();
const rolePermissionFormRef = ref<FormInstance>();
const effectiveFormRef = ref<FormInstance>();

const roleForm = reactive<RBACRolePayload>(emptyRoleForm());
const permissionForm = reactive<RBACPermissionPayload>(emptyPermissionForm());
const userRoleForm = reactive<RBACUserRolePayload>({
  user_id: '',
  role_id: '',
  expires_at: ''
});
const rolePermissionForm = reactive<RBACRolePermissionPayload>({
  role_id: '',
  permission_id: ''
});
const effectiveForm = reactive<RBACEffectiveCheckPayload>({
  user_id: '',
  action: '',
  resource_type: '',
  resource_id: ''
});

const anyLoading = computed(() => Object.values(loading).some(Boolean));

const effectiveAllowed = computed(() => {
  const result = effectiveResult.value;
  if (!result) return false;
  if (typeof result.allowed === 'boolean') return result.allowed;
  return String(result.decision ?? '').toLowerCase() === 'allow';
});
const effectiveDecisionLabel = computed(() => {
  const result = effectiveResult.value;
  if (!result) return t('dashboard.unknown');
  if (typeof result.allowed === 'boolean') {
    return result.allowed ? t('rbac.result.allowed') : t('rbac.result.denied');
  }
  return String(result.decision ?? t('dashboard.unknown'));
});
const resourceOptionGroups = computed<ResourceOptionGroup[]>(() => {
  const hostOptions = uniqueResourceOptions(targets.value.map(hostResourceOption).filter(isResourceOption));
  const instanceMap = new Map(dbInstances.value.map((inst) => [inst.id, inst]));
  const databaseOptions = uniqueResourceOptions(
    dbAccounts.value
      .map((account) => {
        const inst = instanceMap.get(account.instance_id ?? '');
        return inst ? databaseResourceOption(inst, account) : null;
      })
      .filter(isResourceOption)
  );

  return [
    {
      label: `${t('quickConnect.column.host')} ${t('quickConnect.column.account')}`,
      options: hostOptions
    },
    {
      label: t('audit.column.dbAccounts'),
      options: databaseOptions
    }
  ].filter((group) => group.options.length);
});
const resourceOptionValues = computed(
  () => new Set(resourceOptionGroups.value.flatMap((group) => group.options.map((option) => option.value)))
);

const roleRules: FormRules<RBACRolePayload> = {
  name: [{ required: true, message: () => t('rbac.required.roleName'), trigger: 'blur' }]
};
const permissionRules: FormRules<RBACPermissionPayload> = {
  action: [{ required: true, message: () => t('rbac.required.action'), trigger: 'blur' }],
  effect: [{ required: true, message: () => t('rbac.required.effect'), trigger: 'change' }]
};
const userRoleRules: FormRules<RBACUserRolePayload> = {
  user_id: [{ required: true, message: () => t('rbac.required.user'), trigger: 'change' }],
  role_id: [{ required: true, message: () => t('rbac.required.role'), trigger: 'change' }]
};
const rolePermissionRules: FormRules<RBACRolePermissionPayload> = {
  role_id: [{ required: true, message: () => t('rbac.required.role'), trigger: 'change' }],
  permission_id: [{ required: true, message: () => t('rbac.required.permission'), trigger: 'change' }]
};
const effectiveRules: FormRules<RBACEffectiveCheckPayload> = {
  user_id: [{ required: true, message: () => t('rbac.required.user'), trigger: 'change' }],
  action: [{ required: true, message: () => t('rbac.required.action'), trigger: 'blur' }]
};

// ════════════════════════════════════════════════════════════════
// Helpers
// ════════════════════════════════════════════════════════════════

function trim(value: unknown): string {
  return String(value ?? '').trim();
}

function optionalString(value: unknown): string | undefined {
  const text = trim(value);
  return text || undefined;
}

function recordId(record: { id?: string | number }): string {
  return String(record.id ?? '');
}

function resourceValue(resourceType: string, resourceId: string): string {
  return JSON.stringify([resourceType, resourceId]);
}

function parseResourceValue(value: unknown): ResourceIdentity | null {
  const text = trim(value);
  if (!text) return null;
  try {
    const parsed = JSON.parse(text) as unknown;
    if (!Array.isArray(parsed) || parsed.length !== 2) return null;
    const resourceType = trim(parsed[0]);
    const resourceId = trim(parsed[1]);
    if (!resourceType || !resourceId) return null;
    return { resource_type: resourceType, resource_id: resourceId };
  } catch {
    return null;
  }
}

function isResourceOption(option: ResourceOption | null): option is ResourceOption {
  return option !== null;
}

function makeResourceOption(
  resourceType: string,
  resourceId: string,
  name: string,
  detail = ''
): ResourceOption | null {
  const type = trim(resourceType);
  const id = trim(resourceId);
  if (!type || !id) return null;
  const displayName = trim(name) || id;
  const resourceLabel = `${type}:${id}`;
  const label = [displayName, resourceLabel, trim(detail)].filter(Boolean).join(' / ');
  return { resource_type: type, resource_id: id, value: resourceValue(type, id), label, name: displayName };
}

function uniqueResourceOptions(options: ResourceOption[]): ResourceOption[] {
  const seen = new Set<string>();
  return options.filter((option) => {
    if (seen.has(option.value)) return false;
    seen.add(option.value);
    return true;
  });
}

function hostAccountLabel(target: TargetRecord): string {
  const account = trim(target.username ?? target.account ?? target.user);
  const host = trim(target.host ?? target.address ?? target.hostname);
  const port = trim(target.port);
  const endpoint = host && port ? `${host}:${port}` : host;
  if (account && endpoint) return `${account}@${endpoint}`;
  return account || endpoint;
}

function hostResourceOption(target: TargetRecord): ResourceOption | null {
  const resourceType = trim(target.resource_type) || 'host_account';
  const resourceId = trim(target.resource_id) || recordId(target);
  const accountLabel = hostAccountLabel(target);
  const name = [trim(target.name), accountLabel].filter(Boolean).join(' - ') || resourceId;
  return makeResourceOption(resourceType, resourceId, name, trim(target.source));
}

function databaseResourceOption(instance: DatabaseInstanceView, account: DBAccountRecord): ResourceOption | null {
  const resourceType = 'database_account';
  const resourceId = trim(account.id) || trim(account.unique_name);
  const accountName = trim(account.unique_name) || trim(account.upstream_username) || resourceId;
  const instanceName = trim(instance.name);
  const name = [accountName, instanceName].filter(Boolean).join(' @ ') || resourceId;
  const detail = [trim(instance.protocol), trim(instance.address)].filter(Boolean).join(' / ');
  return makeResourceOption(resourceType, resourceId, name, detail);
}

function resourceSelectionValue(form: ResourceForm): string {
  const resourceType = trim(form.resource_type);
  const resourceId = trim(form.resource_id);
  if (!resourceType || !resourceId) return '';
  const value = resourceValue(resourceType, resourceId);
  return resourceOptionValues.value.has(value) ? value : '';
}

function applyResourceSelection(form: ResourceForm, value: unknown) {
  const resource = parseResourceValue(value);
  form.resource_type = resource?.resource_type ?? '';
  form.resource_id = resource?.resource_id ?? '';
}

function selectPermissionResource(value: unknown) {
  applyResourceSelection(permissionForm, value);
}

function selectEffectiveResource(value: unknown) {
  applyResourceSelection(effectiveForm, value);
}

function userValue(user: UserRecord): string {
  return String(user.id ?? user.username ?? '');
}

function userLabel(user: UserRecord): string {
  const value = userValue(user);
  const name = trim(user.display_name ?? user.name);
  return name && name !== value ? `${name} (${value})` : value;
}

function roleLabel(role: RBACRoleRecord): string {
  const id = recordId(role);
  return role.name && role.name !== id ? `${role.name} (${id})` : id;
}

function permissionLabel(permission: RBACPermissionRecord): string {
  const id = recordId(permission);
  const action = trim(permission.action);
  const scope = [permission.resource_type, permission.resource_id].filter(Boolean).join(':');
  const label = scope ? `${action} / ${scope}` : action;
  return label && label !== id ? `${label} (${id})` : id;
}

function userNameForId(id: string | undefined): string {
  const fallback = trim(id);
  const user = users.value.find((item) => userValue(item) === fallback);
  return user ? userLabel(user) : fallback;
}

function roleNameForId(id: string | undefined): string {
  const fallback = trim(id);
  const role = allRoles.value.find((item) => recordId(item) === fallback);
  return role ? roleLabel(role) : fallback;
}

function permissionNameForId(id: string | undefined): string {
  const fallback = trim(id);
  const permission = allPermissions.value.find((item) => recordId(item) === fallback);
  return permission ? permissionLabel(permission) : fallback;
}

function formatText(template: string, values: Record<string, string>): string {
  return Object.entries(values).reduce((text, [key, value]) => text.split(`{${key}}`).join(value), template);
}

// ════════════════════════════════════════════════════════════════
// Form factories
// ════════════════════════════════════════════════════════════════

function emptyRoleForm(): RBACRolePayload {
  return { id: '', name: '', description: '', status: 'active' };
}

function emptyPermissionForm(): RBACPermissionPayload {
  return { id: '', name: '', action: '', resource_type: '', resource_id: '', effect: 'allow', description: '' };
}

function resetRoleForm() {
  Object.assign(roleForm, emptyRoleForm());
}

function resetPermissionForm() {
  Object.assign(permissionForm, emptyPermissionForm());
}

// ════════════════════════════════════════════════════════════════
// Payload builders
// ════════════════════════════════════════════════════════════════

function buildRolePayload(): RBACRolePayload {
  const payload: RBACRolePayload = { name: trim(roleForm.name), status: trim(roleForm.status) || 'active' };
  const id = optionalString(roleForm.id);
  const description = optionalString(roleForm.description);
  if (id) payload.id = id;
  if (description) payload.description = description;
  return payload;
}

function roleStatusPayload(role: RBACRoleRecord, status: string): RBACRolePayload {
  return {
    id: recordId(role) || undefined,
    name: trim(role.name) || recordId(role),
    description: optionalString(role.description),
    status
  };
}

function buildPermissionPayload(): RBACPermissionPayload {
  const payload: RBACPermissionPayload = {
    action: trim(permissionForm.action),
    effect: trim(permissionForm.effect) || 'allow'
  };
  const id = optionalString(permissionForm.id);
  const name = optionalString(permissionForm.name);
  const resourceType = optionalString(permissionForm.resource_type);
  const resourceId = optionalString(permissionForm.resource_id);
  const description = optionalString(permissionForm.description);
  if (id) payload.id = id;
  if (name) payload.name = name;
  if (resourceType) payload.resource_type = resourceType;
  if (resourceId) payload.resource_id = resourceId;
  if (description) payload.description = description;
  return payload;
}

function buildUserRolePayload(): RBACUserRolePayload {
  const payload: RBACUserRolePayload = {
    user_id: trim(userRoleForm.user_id),
    role_id: trim(userRoleForm.role_id)
  };
  const expiresAt = optionalString(userRoleForm.expires_at);
  if (expiresAt) payload.expires_at = expiresAt;
  return payload;
}

function buildRolePermissionPayload(): RBACRolePermissionPayload {
  return {
    role_id: trim(rolePermissionForm.role_id),
    permission_id: trim(rolePermissionForm.permission_id)
  };
}

function buildEffectivePayload(): RBACEffectiveCheckPayload {
  const payload: RBACEffectiveCheckPayload = {
    user_id: trim(effectiveForm.user_id),
    action: trim(effectiveForm.action)
  };
  const resourceType = optionalString(effectiveForm.resource_type);
  const resourceId = optionalString(effectiveForm.resource_id);
  if (resourceType) payload.resource_type = resourceType;
  if (resourceId) payload.resource_id = resourceId;
  return payload;
}

// ════════════════════════════════════════════════════════════════
// Data fetching
// ════════════════════════════════════════════════════════════════

async function loadUsers() {
  loading.users = true;
  errors.users = '';
  try {
    const res = await apiClient.getUsers({ page: 1, page_size: 9999 });
    users.value = res.items ?? [];
  } catch (err) {
    errors.users = err instanceof Error ? err.message : t('rbac.error.loadUsers');
  } finally {
    loading.users = false;
  }
}

async function loadResources() {
  loading.resources = true;
  errors.resources = '';
  try {
    const [targetsResult, dbInstancesResult] = await Promise.allSettled([
      apiClient.getTargets(),
      apiClient.getDBInstances()
    ]);
    const messages: string[] = [];

    if (targetsResult.status === 'fulfilled') {
      targets.value = Array.isArray(targetsResult.value)
        ? targetsResult.value
        : (targetsResult.value as { data?: TargetRecord[] }).data ?? [];
    } else {
      messages.push(
        targetsResult.reason instanceof Error ? targetsResult.reason.message : t('quickConnect.error.loadTargets')
      );
    }

    if (dbInstancesResult.status === 'fulfilled') {
      dbInstances.value = Array.isArray(dbInstancesResult.value)
        ? dbInstancesResult.value
        : (dbInstancesResult.value as { data?: DatabaseInstanceView[] }).data ?? [];
      // Load accounts for all instances
      const accountResults = await Promise.allSettled(
        dbInstances.value.map((inst) =>
          inst.id ? apiClient.getDBAccounts(inst.id) : Promise.resolve([] as DBAccountRecord[])
        )
      );
      const allAccounts: DBAccountRecord[] = [];
      for (const result of accountResults) {
        if (result.status === 'fulfilled') {
          allAccounts.push(
            ...(Array.isArray(result.value) ? result.value : (result.value as { data?: DBAccountRecord[] }).data ?? [])
          );
        }
      }
      dbAccounts.value = allAccounts;
    } else {
      messages.push(
        dbInstancesResult.reason instanceof Error
          ? dbInstancesResult.reason.message
          : 'Unable to load database account resources'
      );
    }

    errors.resources = messages.join('; ');
  } finally {
    loading.resources = false;
  }
}

async function loadAllRoles() {
  if (allRolesLoaded.value) return;
  try {
    const res = await apiClient.getRBACRoles({ page: 1, page_size: 9999 });
    allRoles.value = res.items ?? [];
    allRolesLoaded.value = true;
  } catch {
    // non-fatal: form selects will just be empty
  }
}

async function loadAllPermissions() {
  if (allPermissionsLoaded.value) return;
  try {
    const res = await apiClient.getRBACPermissions({ page: 1, page_size: 9999 });
    allPermissions.value = res.items ?? [];
    allPermissionsLoaded.value = true;
  } catch {
    // non-fatal
  }
}

async function refreshAllRoles() {
  allRolesLoaded.value = false;
  await loadAllRoles();
}

async function refreshAllPermissions() {
  allPermissionsLoaded.value = false;
  await loadAllPermissions();
}

async function fetchRoles() {
  loading.roles = true;
  errors.roles = '';
  try {
    const res: PageResponse<RBACRoleRecord> = await apiClient.getRBACRoles({
      page: rolePage.value,
      page_size: rolePageSize.value,
      q: roleKeyword.value.trim() || undefined
    });
    roles.value = res.items ?? [];
    roleTotal.value = res.total ?? 0;
  } catch (err) {
    errors.roles = err instanceof Error ? err.message : t('rbac.error.loadRoles');
  } finally {
    loading.roles = false;
  }
}

function onRoleSearch(q: string) {
  roleKeyword.value = q;
  rolePage.value = 1;
  fetchRoles();
}

async function fetchPermissions() {
  loading.permissions = true;
  errors.permissions = '';
  try {
    const res: PageResponse<RBACPermissionRecord> = await apiClient.getRBACPermissions({
      page: permissionPage.value,
      page_size: permissionPageSize.value,
      q: permissionKeyword.value.trim() || undefined
    });
    permissions.value = res.items ?? [];
    permissionTotal.value = res.total ?? 0;
  } catch (err) {
    errors.permissions = err instanceof Error ? err.message : t('rbac.error.loadPermissions');
  } finally {
    loading.permissions = false;
  }
}

function onPermissionSearch(q: string) {
  permissionKeyword.value = q;
  permissionPage.value = 1;
  fetchPermissions();
}

async function fetchUserRoles() {
  loading.userRoles = true;
  errors.userRoles = '';
  try {
    const res: PageResponse<RBACUserRoleRecord> = await apiClient.getRBACUserRoles({
      page: userRolePage.value,
      page_size: userRolePageSize.value
    });
    userRoles.value = res.items ?? [];
    userRoleTotal.value = res.total ?? 0;
  } catch (err) {
    errors.userRoles = err instanceof Error ? err.message : t('rbac.error.loadUserRoles');
  } finally {
    loading.userRoles = false;
  }
}

async function fetchRolePermissions() {
  loading.rolePermissions = true;
  errors.rolePermissions = '';
  try {
    const res: PageResponse<RBACRolePermissionRecord> = await apiClient.getRBACRolePermissions({
      page: rolePermissionPage.value,
      page_size: rolePermissionPageSize.value
    });
    rolePermissions.value = res.items ?? [];
    rolePermissionTotal.value = res.total ?? 0;
  } catch (err) {
    errors.rolePermissions = err instanceof Error ? err.message : t('rbac.error.loadRolePermissions');
  } finally {
    loading.rolePermissions = false;
  }
}

// ════════════════════════════════════════════════════════════════
// Tab & refresh orchestration
// ════════════════════════════════════════════════════════════════

async function onTabChange(tab: RBACTab) {
  switch (tab) {
    case 'roles':
      await fetchRoles();
      break;
    case 'permissions':
      await fetchPermissions();
      break;
    case 'userRoles':
      await Promise.all([loadAllRoles(), fetchUserRoles()]);
      break;
    case 'rolePermissions':
      await Promise.all([loadAllRoles(), loadAllPermissions(), fetchRolePermissions()]);
      break;
    case 'effective':
      // effective is form-based, no list fetch needed
      break;
  }
}

async function refreshActiveTab() {
  switch (activeTab.value) {
    case 'roles':
      await fetchRoles();
      break;
    case 'permissions':
      await fetchPermissions();
      break;
    case 'userRoles':
      await Promise.all([refreshAllRoles(), fetchUserRoles()]);
      break;
    case 'rolePermissions':
      await Promise.all([refreshAllRoles(), refreshAllPermissions(), fetchRolePermissions()]);
      break;
    case 'effective':
      // no list to refresh; user re-submits form
      break;
  }
}

// ════════════════════════════════════════════════════════════════
// Form dialogs
// ════════════════════════════════════════════════════════════════

async function openRoleDialog() {
  roleMorePanels.value = [];
  resetRoleForm();
  roleDialogVisible.value = true;
  await nextTick();
  roleFormRef.value?.clearValidate();
}

async function openPermissionDialog() {
  permissionMorePanels.value = [];
  resetPermissionForm();
  permissionDialogVisible.value = true;
  await nextTick();
  permissionFormRef.value?.clearValidate();
}

// ════════════════════════════════════════════════════════════════
// Submit handlers
// ════════════════════════════════════════════════════════════════

async function submitRole() {
  const valid = await roleFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  submitting.role = true;
  try {
    await apiClient.createRBACRole(buildRolePayload());
    ElMessage.success(t('rbac.message.roleCreated'));
    roleDialogVisible.value = false;
    await Promise.all([fetchRoles(), refreshAllRoles()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('rbac.error.saveRole'));
  } finally {
    submitting.role = false;
  }
}

async function submitPermission() {
  const valid = await permissionFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  submitting.permission = true;
  try {
    await apiClient.createRBACPermission(buildPermissionPayload());
    ElMessage.success(t('rbac.message.permissionCreated'));
    permissionDialogVisible.value = false;
    await Promise.all([fetchPermissions(), refreshAllPermissions()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('rbac.error.savePermission'));
  } finally {
    submitting.permission = false;
  }
}

async function submitUserRole() {
  const valid = await userRoleFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  submitting.userRole = true;
  try {
    await apiClient.createRBACUserRole(buildUserRolePayload());
    ElMessage.success(t('rbac.message.userRoleCreated'));
    userRoleForm.user_id = '';
    userRoleForm.role_id = '';
    userRoleForm.expires_at = '';
    await fetchUserRoles();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('rbac.error.saveUserRole'));
  } finally {
    submitting.userRole = false;
  }
}

async function submitRolePermission() {
  const valid = await rolePermissionFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  submitting.rolePermission = true;
  try {
    await apiClient.createRBACRolePermission(buildRolePermissionPayload());
    ElMessage.success(t('rbac.message.rolePermissionCreated'));
    rolePermissionForm.role_id = '';
    rolePermissionForm.permission_id = '';
    await fetchRolePermissions();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('rbac.error.saveRolePermission'));
  } finally {
    submitting.rolePermission = false;
  }
}

async function submitEffectiveCheck() {
  const valid = await effectiveFormRef.value?.validate().catch(() => false);
  if (!valid) return;
  submitting.effective = true;
  errors.effective = '';
  effectiveResult.value = null;
  try {
    const result = await apiClient.checkRBACEffective(buildEffectivePayload());
    effectiveResult.value = ((result as { data?: RBACEffectiveCheckResult }).data ?? result) as RBACEffectiveCheckResult;
  } catch (err) {
    errors.effective = err instanceof Error ? err.message : t('rbac.error.checkEffective');
  } finally {
    submitting.effective = false;
  }
}

// ════════════════════════════════════════════════════════════════
// Delete & status toggle
// ════════════════════════════════════════════════════════════════

async function confirmDelete(label: string): Promise<boolean> {
  try {
    await ElMessageBox.confirm(
      formatText(t('rbac.deleteConfirm'), { name: label }),
      t('rbac.deleteTitle'),
      { cancelButtonText: t('common.cancel'), confirmButtonText: t('common.delete'), type: 'warning' }
    );
    return true;
  } catch {
    return false;
  }
}

async function toggleRoleStatus(role: RBACRoleRecord) {
  const id = recordId(role);
  if (!id) return;
  const status = role.status === 'disabled' ? 'active' : 'disabled';
  statusUpdatingRoleId.value = id;
  try {
    await apiClient.updateRBACRole(id, roleStatusPayload(role, status));
    ElMessage.success(status === 'disabled' ? '角色已禁用' : '角色已启用');
    await Promise.all([fetchRoles(), refreshAllRoles()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('rbac.error.saveRole'));
  } finally {
    statusUpdatingRoleId.value = '';
  }
}

async function deleteRole(role: RBACRoleRecord) {
  const id = recordId(role);
  if (!id || !(await confirmDelete(roleLabel(role)))) return;
  deleting.roleId = id;
  try {
    await apiClient.deleteRBACRole(id);
    ElMessage.success(t('rbac.message.deleted'));
    await Promise.all([fetchRoles(), refreshAllRoles()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('rbac.error.delete'));
  } finally {
    deleting.roleId = '';
  }
}

async function deletePermission(permission: RBACPermissionRecord) {
  const id = recordId(permission);
  if (!id || !(await confirmDelete(permissionLabel(permission)))) return;
  deleting.permissionId = id;
  try {
    await apiClient.deleteRBACPermission(id);
    ElMessage.success(t('rbac.message.deleted'));
    await Promise.all([fetchPermissions(), refreshAllPermissions()]);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('rbac.error.delete'));
  } finally {
    deleting.permissionId = '';
  }
}

async function deleteUserRole(binding: RBACUserRoleRecord) {
  const id = recordId(binding);
  if (!id || !(await confirmDelete(`${userNameForId(binding.user_id)} -> ${roleNameForId(binding.role_id)}`))) return;
  deleting.userRoleId = id;
  try {
    await apiClient.deleteRBACUserRole(id);
    ElMessage.success(t('rbac.message.deleted'));
    await fetchUserRoles();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('rbac.error.delete'));
  } finally {
    deleting.userRoleId = '';
  }
}

async function deleteRolePermission(binding: RBACRolePermissionRecord) {
  const id = recordId(binding);
  if (!id || !(await confirmDelete(`${roleNameForId(binding.role_id)} -> ${permissionNameForId(binding.permission_id)}`))) return;
  deleting.rolePermissionId = id;
  try {
    await apiClient.deleteRBACRolePermission(id);
    ElMessage.success(t('rbac.message.deleted'));
    await fetchRolePermissions();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('rbac.error.delete'));
  } finally {
    deleting.rolePermissionId = '';
  }
}

// ════════════════════════════════════════════════════════════════
// Watchers & lifecycle
// ════════════════════════════════════════════════════════════════

watch([rolePage, rolePageSize], () => {
  if (activeTab.value === 'roles') fetchRoles();
});
watch([permissionPage, permissionPageSize], () => {
  if (activeTab.value === 'permissions') fetchPermissions();
});
watch([userRolePage, userRolePageSize], () => {
  if (activeTab.value === 'userRoles') fetchUserRoles();
});
watch([rolePermissionPage, rolePermissionPageSize], () => {
  if (activeTab.value === 'rolePermissions') fetchRolePermissions();
});

onMounted(async () => {
  await Promise.all([loadUsers(), loadResources()]);
  // Load initial tab data
  await onTabChange(activeTab.value);
});
</script>

<style scoped>
.page-tabs {
  flex: 1;
  min-width: 280px;
}

.page-tabs :deep(.el-tabs__header) {
  margin: 0;
}

.inline-form {
  display: grid;
  grid-template-columns: repeat(4, minmax(180px, 1fr)) auto;
  gap: 0 14px;
  align-items: end;
  margin-bottom: 16px;
}

.inline-form-actions {
  align-self: end;
}

.inline-form-actions :deep(.el-form-item__content) {
  align-items: flex-end;
}

.effective-form {
  grid-template-columns: repeat(5, minmax(160px, 1fr)) auto;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}

.form-grid-full {
  grid-column: 1 / -1;
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

.resource-option {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
}

.resource-option-main {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.resource-option-meta {
  flex: none;
  color: #667085;
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", monospace;
  font-size: 12px;
}

.result-panel {
  display: grid;
  gap: 14px;
}

.json-preview {
  overflow: auto;
  max-height: 320px;
  margin: 0;
  padding: 14px;
  color: #344054;
  background: #f9fafb;
  border: 1px solid #eaecf0;
  border-radius: 8px;
}

@media (max-width: 1080px) {
  .inline-form {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .inline-form,
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
