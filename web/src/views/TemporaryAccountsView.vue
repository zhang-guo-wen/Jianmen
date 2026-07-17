<template>
  <div class="view-stack temporary-access-view">
    <div class="page-container">
      <DataTableCard
        :data="accounts"
        :loading="loading"
        :total="total"
        v-model:page="page"
        v-model:page-size="pageSize"
        search-placeholder="?? SessionID???..."
        @search="onSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="openTemporaryDialog">????</el-button>
          <el-button type="success" plain @click="openAIDialog">AI ??</el-button>
        </template>

        <el-table-column label="SessionID" min-width="190" show-overflow-tooltip>
          <template #default="{ row }"><code>{{ row.session_id }}</code></template>
        </el-table-column>
        <el-table-column label="??" width="100">
          <template #default="{ row }">
            <el-tag :type="row.type === 'ai_user' ? 'success' : 'warning'" size="small">
              {{ row.type === 'ai_user' ? 'AI ??' : '????' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="????" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.authorized_user || '-' }}</template>
        </el-table-column>
        <el-table-column label="?????" width="170">
          <template #default="{ row }">
            <span :class="{ expired: isExpired(row.expires_at) }">{{ formatTime(row.expires_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="resource_name" label="????" min-width="130" show-overflow-tooltip />
        <el-table-column prop="account_name" label="????" min-width="130" show-overflow-tooltip />
        <el-table-column label="????" width="170">
          <template #default="{ row }">{{ formatTime(row.starts_at) }}</template>
        </el-table-column>
        <el-table-column prop="remark" label="??" min-width="150" show-overflow-tooltip />
        <el-table-column label="??" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' && !isExpired(row.expires_at) ? 'success' : 'info'" size="small">
              {{ row.status === 'active' && !isExpired(row.expires_at) ? '??' : '???' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="??" width="245" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openExtendDialog(row)">?????</el-button>
            <el-button v-if="row.status === 'active'" link type="danger" @click="disableAccount(row)">??</el-button>
            <el-dropdown trigger="click" @command="(command: string) => handleMore(command, row)">
              <el-button link type="primary">??</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="audit">????</el-dropdown-item>
                  <el-dropdown-item command="online">????</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
      </DataTableCard>
    </div>

    <el-dialog v-model="temporaryDialogVisible" title="????" width="640px" destroy-on-close>
      <el-alert title="?????? 7 ???????????????????" type="info" show-icon :closable="false" />
      <el-form label-width="100px" class="dialog-form">
        <el-form-item label="????" required>
          <el-select v-model="temporaryForm.authorized_user_id" filterable placeholder="?????????" style="width: 100%">
            <el-option v-for="user in users" :key="String(user.id)" :label="user.display_name || user.username || String(user.id)" :value="String(user.id)" />
          </el-select>
        </el-form-item>
        <el-form-item label="????" required>
          <el-radio-group v-model="temporaryForm.resource_type">
            <el-radio value="host_account">????</el-radio>
            <el-radio value="database_account">?????</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="????" required>
          <el-select v-model="temporaryForm.resource_id" filterable placeholder="??????" style="width: 100%">
            <el-option v-for="item in resourceOptions" :key="item.id" :label="item.label" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="???" required>
          <div class="expiry-row">
            <el-segmented v-model="temporaryDuration" :options="temporaryDurations" @change="applyTemporaryDuration" />
            <el-date-picker v-model="temporaryForm.expires_at" type="datetime" placeholder="??????" :disabled-date="disablePastDate" />
          </div>
        </el-form-item>
        <el-form-item label="??">
          <el-input v-model="temporaryForm.remark" type="textarea" :rows="3" maxlength="300" show-word-limit placeholder="???????????????" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="temporaryDialogVisible = false">??</el-button>
        <el-button type="primary" :loading="submitting" @click="submitTemporaryAuthorization">????</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="aiDialogVisible" title="AI ??" width="640px" destroy-on-close>
      <template v-if="!aiResult">
        <el-alert title="???????????????? AI API ???????????? 48 ????????? 30 ??" type="warning" show-icon :closable="false" />
        <el-form label-width="100px" class="dialog-form">
          <el-form-item label="????">
            <el-input v-model="aiForm.name" placeholder="????? Agent" />
          </el-form-item>
          <el-form-item label="???" required>
            <div class="expiry-row">
              <el-segmented v-model="aiDuration" :options="aiDurations" @change="applyAIDuration" />
              <el-date-picker v-model="aiForm.expires_at" type="datetime" placeholder="??????" :disabled-date="disablePastDate" />
            </div>
          </el-form-item>
          <el-form-item label="??">
            <el-input v-model="aiForm.remark" type="textarea" :rows="3" maxlength="300" show-word-limit placeholder="?? Agent ???????" />
          </el-form-item>
        </el-form>
      </template>
      <template v-else>
        <el-result icon="success" title="AI ????" sub-title="????????????????????????" />
        <div class="prompt-card">
          <div class="prompt-title">???</div>
          <p>{{ aiResult.prompt }}</p>
        </div>
        <div class="copy-actions">
          <el-button type="primary" @click="copyAIText(aiResult.copy_prompt || '')">?????????</el-button>
          <el-button type="success" plain @click="copyAIText(aiResult.full_prompt || '')">??????????</el-button>
        </div>
      </template>
      <template #footer>
        <el-button v-if="aiResult" @click="closeAIDialog">??</el-button>
        <template v-else>
          <el-button @click="aiDialogVisible = false">??</el-button>
          <el-button type="primary" :loading="submitting" @click="submitAIAuthorization">????</el-button>
        </template>
      </template>
    </el-dialog>

    <el-dialog v-model="extendDialogVisible" title="?????" width="460px">
      <el-date-picker v-model="extendExpiresAt" type="datetime" placeholder="????????? 7 ??" style="width: 100%" :disabled-date="disablePastDate" />
      <template #footer>
        <el-button @click="extendDialogVisible = false">??</el-button>
        <el-button type="primary" :loading="submitting" @click="submitExtend">??</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import DataTableCard from '@/components/DataTableCard.vue'
import { apiClient, type IssuedAIAccessToken, type TemporaryAccountRecord, type UserRecord } from '@/api/client'
import { writeClipboardText } from '@/utils/clipboard'

const router = useRouter()
const accounts = ref<TemporaryAccountRecord[]>([])
const users = ref<UserRecord[]>([])
const hostAccounts = ref<{ id: string; label: string }[]>([])
const databaseAccounts = ref<{ id: string; label: string }[]>([])
const loading = ref(false)
const submitting = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')

const temporaryDialogVisible = ref(false)
const aiDialogVisible = ref(false)
const extendDialogVisible = ref(false)
const extendTarget = ref<TemporaryAccountRecord | null>(null)
const extendExpiresAt = ref<Date | null>(null)
const aiResult = ref<IssuedAIAccessToken | null>(null)

const temporaryForm = reactive({ authorized_user_id: '', resource_type: 'host_account', resource_id: '', expires_at: null as Date | null, remark: '' })
const aiForm = reactive({ name: 'AI client', expires_at: null as Date | null, remark: '' })
const temporaryDuration = ref('1d')
const aiDuration = ref('48h')
const temporaryDurations = [{ label: '1 ??', value: '1h' }, { label: '1 ?', value: '1d' }, { label: '3 ?', value: '3d' }, { label: '7 ?', value: '7d' }]
const aiDurations = [{ label: '48 ??', value: '48h' }, { label: '3 ?', value: '3d' }, { label: '7 ?', value: '7d' }]
const resourceOptions = computed(() => temporaryForm.resource_type === 'host_account' ? hostAccounts.value : databaseAccounts.value)

function addDuration(value: string): Date {
  const hours = value.endsWith('h') ? Number(value.slice(0, -1)) : Number(value.slice(0, -1)) * 24
  return new Date(Date.now() + hours * 3600 * 1000)
}
function applyTemporaryDuration() { temporaryForm.expires_at = addDuration(temporaryDuration.value) }
function applyAIDuration() { aiForm.expires_at = addDuration(aiDuration.value) }
function disablePastDate(date: Date) { return date.getTime() < Date.now() - 86400000 }
function formatTime(value?: string) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-' }
function isExpired(value?: string) { return Boolean(value && new Date(value).getTime() <= Date.now()) }

async function loadAccounts() {
  loading.value = true
  try {
    const response = await apiClient.getTemporaryAccounts({ page: page.value, page_size: pageSize.value, q: keyword.value || undefined })
    accounts.value = response.items || []
    total.value = response.total || 0
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '????????')
  } finally { loading.value = false }
}
async function loadOptions() {
  const [userPage, targetPage, dbPage] = await Promise.all([
    apiClient.getUsers({ page: 1, page_size: 200 }),
    apiClient.getTargets({ page: 1, page_size: 200 }),
    apiClient.getAllDBAccounts({ page: 1, page_size: 200 }),
  ])
  users.value = (userPage.items || []).filter(user => !user.is_super_admin && user.status === 'active')
  hostAccounts.value = (targetPage.items || []).map(item => ({ id: String(item.id || ''), label: `${item.host || '??'} / ${item.name || item.username || item.id}` }))
  databaseAccounts.value = (dbPage.items || []).map(item => ({ id: String(item.id || ''), label: `${item.instance_name || '???'} / ${item.unique_name || item.username || item.id}` }))
}
function openTemporaryDialog() {
  temporaryForm.authorized_user_id = ''
  temporaryForm.resource_type = 'host_account'
  temporaryForm.resource_id = ''
  temporaryForm.remark = ''
  temporaryDuration.value = '1d'
  applyTemporaryDuration()
  temporaryDialogVisible.value = true
  void loadOptions()
}
function openAIDialog() {
  aiResult.value = null
  aiForm.name = 'AI client'
  aiForm.remark = ''
  aiDuration.value = '48h'
  applyAIDuration()
  aiDialogVisible.value = true
}
async function submitTemporaryAuthorization() {
  if (!temporaryForm.authorized_user_id || !temporaryForm.resource_id || !temporaryForm.expires_at) return ElMessage.warning('?????????')
  submitting.value = true
  try {
    await apiClient.createTemporaryAuthorization({
      authorized_user_id: temporaryForm.authorized_user_id,
      resource_type: temporaryForm.resource_type,
      resource_id: temporaryForm.resource_id,
      expires_at: temporaryForm.expires_at.toISOString(),
      remark: temporaryForm.remark || undefined,
    })
    ElMessage.success('???????')
    temporaryDialogVisible.value = false
    await loadAccounts()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '????????') }
  finally { submitting.value = false }
}
async function submitAIAuthorization() {
  if (!aiForm.expires_at) return ElMessage.warning('??????')
  submitting.value = true
  try {
    aiResult.value = await apiClient.createAIToken({ name: aiForm.name || 'AI client', expires_at: aiForm.expires_at.toISOString(), remark: aiForm.remark || undefined })
    await loadAccounts()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '?? AI ????') }
  finally { submitting.value = false }
}
async function copyAIText(value: string) {
  if (!value) return
  await writeClipboardText(value)
  ElMessage.success('???????????')
}
function closeAIDialog() { aiDialogVisible.value = false; aiResult.value = null }
function openExtendDialog(row: TemporaryAccountRecord) {
  extendTarget.value = row
  extendExpiresAt.value = addDuration('1d')
  extendDialogVisible.value = true
}
async function submitExtend() {
  if (!extendTarget.value || !extendExpiresAt.value) return
  submitting.value = true
  try {
    await apiClient.extendTemporaryAccount(extendTarget.value.id, extendExpiresAt.value.toISOString())
    ElMessage.success('??????')
    extendDialogVisible.value = false
    await loadAccounts()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '???????') }
  finally { submitting.value = false }
}
async function disableAccount(row: TemporaryAccountRecord) {
  await ElMessageBox.confirm('???????????? AI ????????', '????', { type: 'warning' })
  await apiClient.disableTemporaryAccount(row.id)
  ElMessage.success('???????')
  await loadAccounts()
}
function handleMore(command: string, row: TemporaryAccountRecord) {
  const query = { q: row.session_id, resource_type: row.resource_type || undefined }
  void router.push({ path: '/audit', query: command === 'online' ? { ...query, scope: 'online' } : { ...query, scope: row.resource_type === 'database_account' ? 'db' : 'ssh' } })
}
function onSearch(value: string) { keyword.value = value; page.value = 1; void loadAccounts() }
watch([page, pageSize], loadAccounts)
watch(() => temporaryForm.resource_type, () => { temporaryForm.resource_id = '' })
onMounted(loadAccounts)
</script>

<style scoped>
.temporary-access-view code { font-family: "JetBrains Mono", monospace; font-size: 12px; color: var(--el-color-primary); }
.expired { color: var(--el-color-danger); }
.dialog-form { margin-top: 18px; }
.expiry-row { display: grid; gap: 12px; width: 100%; }
.prompt-card { padding: 16px 18px; border: 1px solid var(--el-border-color); border-radius: 12px; background: linear-gradient(135deg, var(--el-fill-color-light), var(--el-color-success-light-9)); }
.prompt-card p { margin: 8px 0 0; line-height: 1.75; color: var(--el-text-color-regular); }
.prompt-title { font-weight: 700; }
.copy-actions { display: flex; justify-content: center; gap: 12px; margin-top: 18px; }
</style>
