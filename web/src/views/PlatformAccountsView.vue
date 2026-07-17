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
          <el-button v-if="permission.canDo('platform_account:create')" type="primary" @click="openCreateDialog">
            {{ t('platformAccounts.action.new') }}
          </el-button>
        </template>
        <el-table-column :label="t('platformAccounts.column.platform')" width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.platform_name || '-' }}</template>
        </el-table-column>
        <el-table-column :label="t('platformAccounts.column.url')" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <el-button v-if="row.url" link type="primary" size="small" @click="copyText(row.url, t('platformAccounts.message.addressCopied'))">
              <el-icon><CopyDocument /></el-icon>{{ row.url }}
            </el-button>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('platformAccounts.column.username')" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.username || '-' }}</template>
        </el-table-column>
        <el-table-column :label="t('platformAccounts.column.password')" width="110" align="center">
          <template #default="{ row }">
            <el-button v-if="row.has_password && permission.canDo('platform_account:use')" link type="primary" size="small" @click="copyPassword(row)">
              <el-icon><CopyDocument /></el-icon>{{ t('platformAccounts.action.copyPassword') }}
            </el-button>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('platformAccounts.column.group')" width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.group || '-' }}</template>
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
        <el-table-column :label="t('platformAccounts.column.remark')" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || '-' }}</template>
        </el-table-column>
        <el-table-column :label="t('platformAccounts.column.actions')" width="160" fixed="right">
          <template #default="{ row }">
            <el-button v-if="permission.canDo('platform_account:update')" link type="primary" size="small" @click="openEditDialog(row)">{{ t('platformAccounts.action.edit') }}</el-button>
            <el-button v-if="permission.canDo('platform_account:delete')" link type="danger" size="small" @click="confirmDelete(row)">{{ t('platformAccounts.action.delete') }}</el-button>
          </template>
        </el-table-column>
      </DataTableCard>

      <FormDialog
        v-model:visible="dialogVisible"
        :title="editingId ? t('platformAccounts.dialog.editTitle') : t('platformAccounts.createTitle')"
        width="520px"
        :loading="submitting"
        @submit="submitForm"
      >
        <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
          <el-form-item :label="t('platformAccounts.field.url')">
            <el-input v-model="form.url" placeholder="https://jenkins.example.com" @input="handleUrlInput">
              <template #prefix v-if="form.url"><el-link :href="form.url" target="_blank" :underline="false">↗</el-link></template>
            </el-input>
          </el-form-item>

          <el-form-item :label="t('platformAccounts.field.username')" prop="username" required>
            <el-input v-model="form.username" :placeholder="t('platformAccounts.placeholder.username')" />
          </el-form-item>
          <el-form-item :label="t('platformAccounts.field.password')">
            <el-input v-model="form.password" type="password" show-password :placeholder="editingId ? t('platformAccounts.placeholder.password') : ''" />
          </el-form-item>
          <el-collapse v-model="morePanels" class="more-collapse">
            <el-collapse-item :title="t('platformAccounts.moreSettings')" name="more">
              <el-form-item :label="t('platformAccounts.field.platform')">
                <el-input v-model="form.platform_name" :placeholder="t('platformAccounts.placeholder.platform')" />
              </el-form-item>
              <el-form-item :label="t('platformAccounts.field.group')">
                <el-select v-model="form.group" allow-create clearable filterable default-first-option :placeholder="t('platformAccounts.field.group')" style="width: 100%">
                  <el-option v-for="group in groupOptions" :key="group" :label="group" :value="group" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('platformAccounts.field.remark')"><el-input v-model="form.remark" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" /></el-form-item>
            </el-collapse-item>
          </el-collapse>
        </el-form>
      </FormDialog>

    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, watch } from 'vue'
import { useI18n } from '@/i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import { apiClient } from '@/api/client'
import type { PlatformAccountView, PlatformAccountPayload } from '@/api/client'
import DataTableCard from '@/components/DataTableCard.vue'
import FormDialog from '@/components/FormDialog.vue'
import StatusSwitch from '@/components/StatusSwitch.vue'
import { usePermissionStore } from '@/stores/permission'
import { writeClipboardText } from '@/utils/clipboard'

const { t } = useI18n()
const permission = usePermissionStore()
const accounts = ref<PlatformAccountView[]>([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(50)
const searchQuery = ref('')
const dialogVisible = ref(false)
const editingId = ref('')
const submitting = ref(false)
const formRef = ref<FormInstance>()
const morePanels = ref<string[]>(['more'])
const form = reactive<PlatformAccountPayload & { password: string; group: string }>({
  platform_name: '', username: '', password: '', url: '', group: '', remark: ''
})
const rules: FormRules = {
  username: [{ required: true, message: () => t('platformAccounts.required.username'), trigger: 'blur' }]
}
const groupOptions = ref<string[]>([])
const statusUpdatingId = ref('')
const lastAutoPlatformName = ref('')

async function loadData() {
  loading.value = true
  try {
    const response = await apiClient.getPlatformAccounts({ page: page.value, page_size: pageSize.value, q: searchQuery.value || undefined })
    accounts.value = response.items
    total.value = response.total
  } catch {
    ElMessage.error(t('platformAccounts.error.loadList'))
  } finally {
    loading.value = false
  }
}

async function loadGroups() {
  try {
    const response = await apiClient.getResourceGroups({ group_type: 'account', page: 1, page_size: 200 })
    groupOptions.value = (response.items || []).map(group => group.name)
  } catch {
    groupOptions.value = []
  }
}

function onSearch(q: string) {
  searchQuery.value = q
  page.value = 1
  void loadData()
}

function handleUrlInput(value: string) {
  if (!form.platform_name || form.platform_name === lastAutoPlatformName.value) {
    form.platform_name = value
    lastAutoPlatformName.value = value
  }
}

function resetForm() {
  Object.assign(form, { platform_name: '', username: '', password: '', url: '', group: '', remark: '' })
  morePanels.value = ['more']
  lastAutoPlatformName.value = ''
}

function openCreateDialog() {
  editingId.value = ''
  resetForm()
  dialogVisible.value = true
}

function openEditDialog(row: PlatformAccountView) {
  editingId.value = row.id || ''
  Object.assign(form, { platform_name: row.platform_name || '', username: row.username, password: '', url: row.url || '', group: row.group || '', remark: row.remark || '' })
  morePanels.value = ['more']
  lastAutoPlatformName.value = ''
  dialogVisible.value = true
}

async function submitForm() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  submitting.value = true
  try {
    if (editingId.value) {
      await apiClient.updatePlatformAccount(editingId.value, form)
      ElMessage.success(t('platformAccounts.message.updated'))
    } else {
      await apiClient.createPlatformAccount(form)
      ElMessage.success(t('platformAccounts.message.created'))
    }
    dialogVisible.value = false
    await loadData()
  } catch (error: any) {
    ElMessage.error(error.message || t('platformAccounts.error.save'))
  } finally {
    submitting.value = false
  }
}

async function confirmDelete(row: PlatformAccountView) {
  try {
    await ElMessageBox.confirm(t('platformAccounts.deleteConfirm').replace('{name}', row.name || row.username), t('platformAccounts.deleteTitle'), { type: 'warning' })
    await apiClient.deletePlatformAccount(row.id || '')
    ElMessage.success(t('platformAccounts.message.deleted'))
    await loadData()
  } catch {
    // Ignore cancellation.
  }
}

async function toggleStatus(row: PlatformAccountView, active: boolean) {
  statusUpdatingId.value = row.id || ''
  try {
    await apiClient.updatePlatformAccount(row.id || '', { platform_name: row.platform_name, username: row.username, status: active ? 'active' : 'disabled' })
    row.status = active ? 'active' : 'disabled'
  } catch (error: any) {
    ElMessage.error(error.message)
  } finally {
    statusUpdatingId.value = ''
  }
}

async function copyText(value: string, successMessage: string) {
  if (!value) return
  try {
    await writeClipboardText(value)
    ElMessage.success(successMessage)
  } catch {
    ElMessage.error(t('platformAccounts.error.copy'))
  }
}

async function copyPassword(row: PlatformAccountView) {
  if (!row.id) return
  try {
    const response = await apiClient.getPlatformAccountPassword(row.id)
    await copyText(response.password, t('platformAccounts.message.passwordCopied'))
  } catch (error: any) {
    ElMessage.error(error.message || t('platformAccounts.error.copy'))
  }
}

watch([page, pageSize], () => void loadData())
onMounted(async () => { await Promise.all([loadData(), loadGroups()]) })
</script>
