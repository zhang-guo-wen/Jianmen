<template>
  <div class="view-stack">
    <div class="page-container">
      <DataTableCard
        :data="apps"
        :loading="loading"
        :total="total"
        v-model:page="page"
        v-model:page-size="pageSize"
        :search-placeholder="t('application.placeholder.search')"
        @search="onSearch"
      >
        <template #toolbar-extra>
          <el-button v-if="permission.canDo('application:create')" type="primary" @click="openCreate">
            {{ t('application.create') }}
          </el-button>
        </template>

        <el-table-column prop="name" :label="t('application.column.name')" min-width="130" show-overflow-tooltip />
        <el-table-column :label="t('application.column.target')" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">{{ row.address }}</template>
        </el-table-column>
        <el-table-column :label="t('application.column.accessAddress')" min-width="240">
          <template #default="{ row }">
            <el-tooltip :content="t('application.action.clickToCopy')" placement="top">
              <el-link class="access-address" type="primary" :underline="false" @click="copyAccessAddress(row)">
                {{ applicationAccessURL(row) }}
              </el-link>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column prop="group" :label="t('application.column.group')" width="100" show-overflow-tooltip />
        <el-table-column :label="t('application.column.status')" width="80" align="center">
          <template #default="{ row }">
            <StatusSwitch
              v-if="row.can_manage && permission.canDo('application:update')"
              :model-value="row.status === 'active'"
              :loading="statusUpdatingId === row.id"
              @update:model-value="(val: boolean) => toggleStatus(row, val)"
            />
          </template>
        </el-table-column>
        <el-table-column :label="t('application.column.remark')" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || '-' }}</template>
        </el-table-column>
        <el-table-column :label="t('application.column.actions')" width="160" fixed="right">
          <template #default="{ row }">
            <el-button v-if="permission.canDo('app:connect')" link type="success" size="small" @click="visitApp(row)">
              {{ t('application.action.visit') }}
            </el-button>
            <el-button v-if="row.can_manage && permission.canDo('application:update')" link type="primary" size="small" @click="openEdit(row)">
              {{ t('common.edit') }}
            </el-button>
            <el-button v-if="row.can_manage && permission.canDo('application:delete')" link type="danger" size="small" @click="deleteApp(row)">
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </DataTableCard>

      <FormDialog
        v-model:visible="dialogVisible"
        :title="editingId ? t('application.edit') : t('application.create')"
        :loading="submitting"
        @submit="submitApp"
      >
        <el-form :model="form" label-position="top">
          <el-form-item :label="t('application.field.address')" required>
            <div class="field-stack">
              <el-input v-model="form.address" :placeholder="t('application.placeholder.address')" />
              <span class="field-hint">{{ t('application.hint.address') }}</span>
            </div>
          </el-form-item>
          <el-collapse v-model="moreSections">
            <el-collapse-item :title="t('application.moreSettings')" name="more">
              <el-form-item :label="t('application.field.name')">
                <el-input v-model="form.name" :placeholder="t('application.placeholder.name')" />
              </el-form-item>
              <el-form-item :label="t('application.field.listenPort')">
                <div class="field-stack">
                  <el-input-number v-model="form.listen_port" :min="0" :max="65535" controls-position="right" style="width: 100%" />
                  <span class="field-hint">{{ t('application.hint.listenPort') }}</span>
                </div>
              </el-form-item>
              <el-form-item :label="t('application.field.group')">
                <el-select
                  v-model="form.group"
                  allow-create
                  clearable
                  filterable
                  default-first-option
                  style="width: 100%"
                >
                  <el-option v-for="group in resourceGroupOptions" :key="group" :label="group" :value="group" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('application.field.remark')">
                <el-input v-model="form.remark" type="textarea" />
              </el-form-item>
            </el-collapse-item>
          </el-collapse>
        </el-form>
      </FormDialog>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import DataTableCard from '@/components/DataTableCard.vue'
import FormDialog from '@/components/FormDialog.vue'
import StatusSwitch from '@/components/StatusSwitch.vue'
import { apiClient, type ApplicationView, type ApplicationPayload } from '@/api/client'
import { useI18n } from '@/i18n'
import { usePermissionStore } from '@/stores/permission'
import { writeClipboardText } from '@/utils/clipboard'

const { t } = useI18n()
const permission = usePermissionStore()

const apps = ref<ApplicationView[]>([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(50)
const search = ref('')
const resourceGroupOptions = ref<string[]>([])

const dialogVisible = ref(false)
const editingId = ref('')
const submitting = ref(false)
const statusUpdatingId = ref('')
const moreSections = ref<string[]>(['more'])
const form = reactive({ address: '', name: '', listen_port: 0, group: '', remark: '' })

async function fetchApps() {
  loading.value = true
  try {
    const res = await apiClient.getApplications({ page: page.value, page_size: pageSize.value, q: search.value || undefined })
    apps.value = res.items
    total.value = res.total
  } catch (e: any) {
    ElMessage.error(e.message || t('application.error.load'))
  } finally {
    loading.value = false
  }
}

function onSearch(val: string) {
  search.value = val
  page.value = 1
  void fetchApps()
}

watch([page, pageSize], () => void fetchApps())

async function loadResourceGroupOptions() {
  try {
    const res = await apiClient.getResourceGroups({ group_type: 'resource', page: 1, page_size: 200 })
    resourceGroupOptions.value = (res.items ?? []).map(group => group.name).filter(Boolean)
  } catch {
    resourceGroupOptions.value = []
  }
}

onMounted(() => {
  void fetchApps()
  void loadResourceGroupOptions()
})

function proxyHostname(): string {
  const hostname = window.location.hostname
  return hostname.includes(':') && !hostname.startsWith('[') ? `[${hostname}]` : hostname
}

function applicationAccessURL(app: ApplicationView): string {
  const entryPath = app.entry_path?.startsWith('/') ? app.entry_path : `/${app.entry_path || ''}`
  return `http://${proxyHostname()}:${app.listen_port}${entryPath}`
}

function visitApp(app: ApplicationView) {
  window.open(applicationAccessURL(app), '_blank', 'noopener')
}

async function copyAccessAddress(app: ApplicationView) {
  try {
    await writeClipboardText(applicationAccessURL(app))
    ElMessage.success(t('application.message.copied'))
  } catch {
    ElMessage.error(t('application.error.copy'))
  }
}

function openCreate() {
  editingId.value = ''
  form.address = ''
  form.name = ''
  form.listen_port = 0
  form.group = ''
  form.remark = ''
  moreSections.value = ['more']
  dialogVisible.value = true
}

function openEdit(app: ApplicationView) {
  editingId.value = app.id!
  form.address = app.address
  form.name = app.name
  form.listen_port = app.listen_port
  form.group = app.group || ''
  form.remark = app.remark || ''
  moreSections.value = ['more']
  dialogVisible.value = true
}

async function toggleStatus(app: ApplicationView, active: boolean) {
  if (!app.id) return
  statusUpdatingId.value = app.id
  try {
    await apiClient.updateApplication(app.id, {
      address: app.address,
      name: app.name,
      listen_port: app.listen_port,
      group: app.group,
      remark: app.remark,
      status: active ? 'active' : 'disabled',
    })
    app.status = active ? 'active' : 'disabled'
  } catch (e: any) {
    ElMessage.error(e.message || t('application.error.save'))
  } finally {
    statusUpdatingId.value = ''
  }
}

async function submitApp() {
  if (!form.address.trim()) {
    ElMessage.warning(t('application.error.addressRequired'))
    return
  }
  submitting.value = true
  try {
    const payload: ApplicationPayload = {
      address: form.address.trim(),
      name: form.name.trim() || undefined,
      listen_port: form.listen_port || 0,
      group: form.group.trim() || undefined,
      remark: form.remark.trim() || undefined,
    }
    if (editingId.value) {
      await apiClient.updateApplication(editingId.value, payload)
      ElMessage.success(t('application.message.updated'))
    } else {
      await apiClient.createApplication(payload)
      ElMessage.success(t('application.message.created'))
    }
    dialogVisible.value = false
    await Promise.all([fetchApps(), loadResourceGroupOptions()])
  } catch (e: any) {
    ElMessage.error(e.message || t('application.error.save'))
  } finally {
    submitting.value = false
  }
}

async function deleteApp(app: ApplicationView) {
  try {
    await ElMessageBox.confirm(
      t('application.deleteConfirm').replace('{name}', app.name),
      t('application.delete'),
      { cancelButtonText: t('common.cancel'), confirmButtonText: t('common.delete'), type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await apiClient.deleteApplication(app.id!)
    ElMessage.success(t('application.message.deleted'))
    void fetchApps()
  } catch (e: any) {
    ElMessage.error(e.message || t('application.error.delete'))
  }
}
</script>

<style scoped>
.access-address {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: middle;
}

.field-stack {
  display: grid;
  width: 100%;
  gap: 6px;
}

.field-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
}
</style>
