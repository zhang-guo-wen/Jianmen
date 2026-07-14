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
        <el-button v-if="permission.canDo('application:create')" type="primary" @click="openCreate">{{ t('application.create') }}</el-button>
      </template>

      <el-table-column prop="name" :label="t('application.column.name')" min-width="130" show-overflow-tooltip />
      <el-table-column prop="group" :label="t('application.column.group')" width="100" show-overflow-tooltip />
      <el-table-column :label="t('application.column.listenPort')" width="100">
        <template #default="{ row }">
          <el-tag size="small">{{ row.listen_port }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('application.column.target')" min-width="150" show-overflow-tooltip>
        <template #default="{ row }">{{ row.internal_scheme }}://{{ row.internal_host }}:{{ row.internal_port }}</template>
      </el-table-column>
      <el-table-column :label="t('application.column.status')" width="80" align="center">
        <template #default="{ row }">
          <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
            {{ row.status === 'active' ? t('common.enabled') : t('common.disabled') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('application.column.actions')" width="160" fixed="right">
        <template #default="{ row }">
          <el-button v-if="permission.canDo('app:connect')" link type="success" size="small" @click="visitApp(row)">{{ t('application.action.visit') }}</el-button>
          <el-button v-if="permission.canDo('application:update')" link type="primary" size="small" @click="openEdit(row)">{{ t('common.edit') }}</el-button>
          <el-button v-if="permission.canDo('application:delete')" link type="danger" size="small" @click="deleteApp(row)">{{ t('common.delete') }}</el-button>
        </template>
      </el-table-column>
    </DataTableCard>

    <FormDialog
      v-model:visible="dialogVisible"
      :title="editingId ? t('application.edit') : t('application.create')"
      width="560px"
      :loading="submitting"
      @submit="submitApp"
    >
      <el-form :model="form" label-width="80px">
        <el-form-item :label="t('application.field.name')" required>
          <el-input v-model="form.name" :placeholder="t('application.placeholder.name')" />
        </el-form-item>
        <el-form-item :label="t('application.field.host')" required>
          <el-input v-model="form.host" :placeholder="t('application.placeholder.host')" />
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item :label="t('application.field.scheme')">
              <el-select v-model="form.scheme">
                <el-option label="http" value="http" />
                <el-option label="https" value="https" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="t('application.field.port')">
              <el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item :label="t('application.field.listenPort')">
          <el-select v-model="form.listen_port" placeholder="自动分配" clearable>
            <el-option
              v-for="p in availablePorts"
              :key="p"
              :label="String(p)"
              :value="p"
            />
          </el-select>
        </el-form-item>
        <el-collapse>
          <el-collapse-item title="更多设置">
            <el-form-item :label="t('application.field.group')">
              <el-input v-model="form.group" />
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
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import DataTableCard from '@/components/DataTableCard.vue'
import FormDialog from '@/components/FormDialog.vue'
import { apiClient, type ApplicationView, type ApplicationPayload } from '@/api/client'
import { useI18n } from '@/i18n'
import { usePermissionStore } from '@/stores/permission'

const { t } = useI18n()
const permission = usePermissionStore()

const PORT_START = 47110
const PORT_END = 47199

const apps = ref<ApplicationView[]>([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const search = ref('')

const dialogVisible = ref(false)
const editingId = ref('')
const submitting = ref(false)
const form = reactive({ name: '', scheme: 'http', host: '', port: 80, listen_port: 0, group: '', remark: '' })

const usedPorts = computed(() => apps.value.map(a => a.listen_port))

const availablePorts = computed(() => {
  const ports: number[] = []
  for (let p = PORT_START; p <= PORT_END; p++) {
    if (!usedPorts.value.includes(p)) ports.push(p)
    if (ports.length >= 100) break
  }
  return ports
})

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
  fetchApps()
}

watch([page, pageSize], () => fetchApps())
onMounted(() => fetchApps())

function visitApp(app: ApplicationView) {
  window.open(`http://${window.location.hostname}:${app.listen_port}`, '_blank')
}

function openCreate() {
  editingId.value = ''
  form.name = ''
  form.scheme = 'http'
  form.host = ''
  form.port = 80
  form.listen_port = 0
  form.group = ''
  form.remark = ''
  dialogVisible.value = true
}

function openEdit(app: ApplicationView) {
  editingId.value = app.id!
  form.name = app.name
  form.scheme = app.internal_scheme
  form.host = app.internal_host
  form.port = app.internal_port
  form.listen_port = app.listen_port
  form.group = app.group || ''
  form.remark = app.remark || ''
  dialogVisible.value = true
}

async function submitApp() {
  if (!form.name.trim() || !form.host.trim()) {
    ElMessage.warning('请填写必填字段')
    return
  }
  submitting.value = true
  try {
    const payload: ApplicationPayload = {
      name: form.name.trim(),
      scheme: form.scheme,
      host: form.host.trim(),
      port: form.port,
      listen_port: form.listen_port || 0,
      group: form.group.trim() || undefined,
      remark: form.remark.trim() || undefined,
    }
    if (editingId.value) {
      await apiClient.updateApplication(editingId.value, payload)
      ElMessage.success('应用已更新')
    } else {
      await apiClient.createApplication(payload)
      ElMessage.success('应用已创建')
    }
    dialogVisible.value = false
    fetchApps()
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
      { cancelButtonText: '取消', confirmButtonText: '删除', type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await apiClient.deleteApplication(app.id!)
    ElMessage.success('应用已删除')
    fetchApps()
  } catch (e: any) {
    ElMessage.error(e.message || t('application.error.delete'))
  }
}
</script>

<style scoped>
</style>
