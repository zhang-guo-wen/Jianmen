<template>
  <div class="view-stack temporary-access-view">
    <div class="page-container">
      <DataTableCard
        :data="accounts"
        :loading="loading"
        :total="total"
        v-model:page="page"
        v-model:page-size="pageSize"
        search-placeholder="&#x641C;&#x7D22; SessionID&#x3001;&#x5907;&#x6CE8;..."
        @search="onSearch"
      >
        <template #toolbar-extra>
          <el-button type="primary" @click="openTemporaryDialog">&#x4E34;&#x65F6;&#x6388;&#x6743;</el-button>
          <el-button type="success" plain @click="openAIDialog">AI &#x6388;&#x6743;</el-button>
        </template>

        <el-table-column label="SessionID" min-width="190" show-overflow-tooltip>
          <template #default="{ row }"><code>{{ row.session_id }}</code></template>
        </el-table-column>
        <el-table-column label="&#x7C7B;&#x578B;" width="100">
          <template #default="{ row }">
            <el-tag :type="row.type === 'ai_user' ? 'success' : 'warning'" size="small">
              {{ row.type === 'ai_user' ? 'AI 用户' : '临时用户' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="&#x6388;&#x6743;&#x7528;&#x6237;" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.authorized_user || '-' }}</template>
        </el-table-column>
        <el-table-column label="&#x6388;&#x6743;&#x6709;&#x6548;&#x671F;" width="170">
          <template #default="{ row }">
            <span :class="{ expired: isExpired(row.expires_at) }">{{ formatTime(row.expires_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="resource_name" label="&#x8D44;&#x6E90;&#x540D;&#x79F0;" min-width="130" show-overflow-tooltip />
        <el-table-column prop="account_name" label="&#x8D26;&#x6237;&#x540D;&#x79F0;" min-width="130" show-overflow-tooltip />
        <el-table-column label="&#x5F00;&#x59CB;&#x65F6;&#x95F4;" width="170">
          <template #default="{ row }">{{ formatTime(row.starts_at) }}</template>
        </el-table-column>
        <el-table-column prop="remark" label="&#x5907;&#x6CE8;" min-width="150" show-overflow-tooltip />
        <el-table-column label="&#x72B6;&#x6001;" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' && !isExpired(row.expires_at) ? 'success' : 'info'" size="small">
              {{ row.status === 'active' && !isExpired(row.expires_at) ? '有效' : '已停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="&#x64CD;&#x4F5C;" width="220" align="right" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-button link type="primary" size="small" @click="openExtendDialog(row)">&#x5EF6;&#x957F;&#x6709;&#x6548;&#x671F;</el-button>
              <el-button v-if="row.status === 'active'" link type="danger" size="small" @click="disableAccount(row)">&#x7981;&#x7528;</el-button>
              <el-dropdown trigger="click" teleported @command="(command: string) => handleMore(command, row)">
                <el-button link type="primary" size="small">&#x66F4;&#x591A;<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="audit">&#x5BA1;&#x8BA1;&#x65E5;&#x5FD7;</el-dropdown-item>
                  <el-dropdown-item command="online">&#x5728;&#x7EBF;&#x4F1A;&#x8BDD;</el-dropdown-item>
                </el-dropdown-menu>
              </template>
              </el-dropdown>
            </div>
          </template>
        </el-table-column>
      </DataTableCard>
    </div>

    <el-dialog v-model="temporaryDialogVisible" title="&#x4E34;&#x65F6;&#x6388;&#x6743;" width="640px" destroy-on-close>
      <el-alert title="&#x4E34;&#x65F6;&#x6388;&#x6743;&#x6700;&#x591A; 7 &#x5929;&#xFF1B;&#x5230;&#x671F;&#x540E;&#x81EA;&#x52A8;&#x505C;&#x7528;&#xFF0C;&#x4E0D;&#x80FD;&#x767B;&#x5F55;&#x7BA1;&#x7406;&#x7CFB;&#x7EDF;&#x3002;" type="info" show-icon :closable="false" />
      <el-form label-width="100px" class="dialog-form">

        <el-form-item label="&#x8D44;&#x6E90;&#x7C7B;&#x578B;" required>
          <el-radio-group v-model="temporaryForm.resource_type">
            <el-radio value="host_account">&#x4E3B;&#x673A;&#x8D26;&#x53F7;</el-radio>
            <el-radio value="database_account">&#x6570;&#x636E;&#x5E93;&#x8D26;&#x53F7;</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="&#x6388;&#x6743;&#x8D44;&#x6E90;" required>
          <el-select v-model="temporaryForm.resource_id" filterable placeholder="&#x9009;&#x62E9;&#x8D26;&#x53F7;&#x8D44;&#x6E90;" style="width: 100%">
            <el-option v-for="item in resourceOptions" :key="item.id" :label="item.label" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="&#x6709;&#x6548;&#x671F;" required>
          <div class="expiry-row">
            <el-segmented v-model="temporaryDuration" :options="temporaryDurations" @change="applyTemporaryDuration" />
            <el-date-picker v-model="temporaryForm.expires_at" type="datetime" placeholder="&#x9009;&#x62E9;&#x5230;&#x671F;&#x65F6;&#x95F4;" :disabled-date="disablePastDate" />
          </div>
        </el-form-item>
        <el-form-item label="&#x5907;&#x6CE8;">
          <el-input v-model="temporaryForm.remark" type="textarea" :rows="3" maxlength="300" show-word-limit placeholder="&#x8BF4;&#x660E;&#x6388;&#x6743;&#x7528;&#x9014;&#x3001;&#x5DE5;&#x5355;&#x53F7;&#x6216;&#x7533;&#x8BF7;&#x539F;&#x56E0;" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="temporaryDialogVisible = false">&#x53D6;&#x6D88;</el-button>
        <el-button type="primary" :loading="submitting" @click="submitTemporaryAuthorization">&#x786E;&#x8BA4;&#x6388;&#x6743;</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="aiDialogVisible" title="AI &#x6388;&#x6743;" width="640px" destroy-on-close>
      <template v-if="!aiResult">
        <el-alert title="&#x6388;&#x6743; AI &#x4F7F;&#x7528;&#x5F53;&#x524D;&#x7528;&#x6237;&#x7684;&#x6240;&#x6709;&#x6709;&#x6548;&#x6743;&#x9650;&#xFF0C;&#x4EC5;&#x53EF;&#x901A;&#x8FC7; AI API &#x8C03;&#x7528;&#x3002;&#x8BBF;&#x95EE;&#x4EE4;&#x724C;&#x9ED8;&#x8BA4; 48 &#x5C0F;&#x65F6;&#xFF0C;&#x5237;&#x65B0;&#x4EE4;&#x724C;&#x9ED8;&#x8BA4; 30 &#x5929;&#x3002;" type="warning" show-icon :closable="false" />
        <el-form label-width="100px" class="dialog-form">

          <el-form-item label="&#x6709;&#x6548;&#x671F;" required>
            <div class="expiry-row">
              <el-segmented v-model="aiDuration" :options="aiDurations" @change="applyAIDuration" />
              <el-date-picker v-model="aiForm.expires_at" type="datetime" placeholder="&#x9009;&#x62E9;&#x5230;&#x671F;&#x65F6;&#x95F4;" :disabled-date="disablePastDate" />
            </div>
          </el-form-item>
          <el-form-item label="&#x5907;&#x6CE8;">
            <el-input v-model="aiForm.remark" type="textarea" :rows="3" maxlength="300" show-word-limit placeholder="&#x8BF4;&#x660E; Agent &#x7528;&#x9014;&#x548C;&#x4F7F;&#x7528;&#x8303;&#x56F4;" />
          </el-form-item>
        </el-form>
      </template>
      <template v-else>
        <el-result icon="success" title="AI &#x6388;&#x6743;&#x6210;&#x529F;" sub-title="&#x4EE4;&#x724C;&#x4E0D;&#x4F1A;&#x5728;&#x9875;&#x9762;&#x4E2D;&#x660E;&#x6587;&#x5C55;&#x793A;&#xFF0C;&#x8BF7;&#x7ACB;&#x5373;&#x590D;&#x5236;&#x5E76;&#x59A5;&#x5584;&#x4FDD;&#x5B58;&#x3002;" />
        <div class="prompt-card">
          <div class="prompt-title">&#x63D0;&#x793A;&#x8BCD;</div>
          <p>{{ aiResult.prompt }}</p>
        </div>
        <div class="copy-actions">
          <el-button type="primary" @click="copyAIText(aiResult.copy_prompt || '')">&#x590D;&#x5236;&#x6587;&#x6863;&#x8DEF;&#x5F84;&#x548C;&#x4EE4;&#x724C;</el-button>
          <el-button type="success" plain @click="copyAIText(aiResult.full_prompt || '')">&#x590D;&#x5236;&#x5B8C;&#x6574;&#x63D0;&#x793A;&#x8BCD;&#x548C;&#x4EE4;&#x724C;</el-button>
        </div>
      </template>
      <template #footer>
        <el-button v-if="aiResult" @click="closeAIDialog">&#x5B8C;&#x6210;</el-button>
        <template v-else>
          <el-button @click="aiDialogVisible = false">&#x53D6;&#x6D88;</el-button>
          <el-button type="primary" :loading="submitting" @click="submitAIAuthorization">&#x751F;&#x6210;&#x6388;&#x6743;</el-button>
        </template>
      </template>
    </el-dialog>

    <el-dialog v-model="extendDialogVisible" title="&#x5EF6;&#x957F;&#x6709;&#x6548;&#x671F;" width="460px">
      <el-date-picker v-model="extendExpiresAt" type="datetime" placeholder="&#x65B0;&#x7684;&#x5230;&#x671F;&#x65F6;&#x95F4;&#xFF08;&#x6700;&#x591A; 7 &#x5929;&#xFF09;" style="width: 100%" :disabled-date="disablePastDate" />
      <template #footer>
        <el-button @click="extendDialogVisible = false">&#x53D6;&#x6D88;</el-button>
        <el-button type="primary" :loading="submitting" @click="submitExtend">&#x4FDD;&#x5B58;</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowDown } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import DataTableCard from '@/components/DataTableCard.vue'
import { apiClient, type IssuedAIAccessToken, type TemporaryAccountRecord } from '@/api/client'
import { writeClipboardText } from '@/utils/clipboard'

const router = useRouter()
const accounts = ref<TemporaryAccountRecord[]>([])
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

const temporaryForm = reactive({ resource_type: 'host_account', resource_id: '', expires_at: null as Date | null, remark: '' })
const aiForm = reactive({ expires_at: null as Date | null, remark: '' })
const temporaryDuration = ref('1d')
const aiDuration = ref('48h')
const temporaryDurations = [{ label: '1 小时', value: '1h' }, { label: '1 天', value: '1d' }, { label: '3 天', value: '3d' }, { label: '7 天', value: '7d' }]
const aiDurations = [{ label: '48 小时', value: '48h' }, { label: '3 天', value: '3d' }, { label: '7 天', value: '7d' }]
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
    ElMessage.error(error instanceof Error ? error.message : '加载临时用户失败')
  } finally { loading.value = false }
}
async function loadOptions() {
  const [targetPage, dbPage] = await Promise.all([
    apiClient.getTargets({ page: 1, page_size: 200 }),
    apiClient.getAllDBAccounts({ page: 1, page_size: 200 }),
  ])
  hostAccounts.value = (targetPage.items || []).map(item => ({ id: String(item.id || ''), label: `${item.host || '主机'} / ${item.name || item.username || item.id}` }))
  databaseAccounts.value = (dbPage.items || []).map(item => ({ id: String(item.id || ''), label: `${item.instance_name || '数据库'} / ${item.unique_name || item.username || item.id}` }))
}
function openTemporaryDialog() {
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
  aiForm.remark = ''
  aiDuration.value = '48h'
  applyAIDuration()
  aiDialogVisible.value = true
}
async function submitTemporaryAuthorization() {
  if (!temporaryForm.resource_id || !temporaryForm.expires_at) return ElMessage.warning('请完整填写授权信息')
  submitting.value = true
  try {
    await apiClient.createTemporaryAuthorization({
      resource_type: temporaryForm.resource_type,
      resource_id: temporaryForm.resource_id,
      expires_at: temporaryForm.expires_at.toISOString(),
      remark: temporaryForm.remark || undefined,
    })
    ElMessage.success('临时授权已创建')
    temporaryDialogVisible.value = false
    await loadAccounts()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '创建临时授权失败') }
  finally { submitting.value = false }
}
async function submitAIAuthorization() {
  if (!aiForm.expires_at) return ElMessage.warning('请选择有效期')
  submitting.value = true
  try {
    aiResult.value = await apiClient.createAIToken({ expires_at: aiForm.expires_at.toISOString(), remark: aiForm.remark || undefined })
    await loadAccounts()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '创建 AI 授权失败') }
  finally { submitting.value = false }
}
async function copyAIText(value: string) {
  if (!value) return
  await writeClipboardText(value)
  ElMessage.success('已复制，请妥善保管令牌')
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
    ElMessage.success('有效期已延长')
    extendDialogVisible.value = false
    await loadAccounts()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '延长有效期失败') }
  finally { submitting.value = false }
}
async function disableAccount(row: TemporaryAccountRecord) {
  await ElMessageBox.confirm('禁用后该临时用户的授权和 AI 令牌将立即失效。', '确认禁用', { type: 'warning' })
  await apiClient.disableTemporaryAccount(row.id)
  ElMessage.success('临时用户已禁用')
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
.table-actions { display: inline-flex; align-items: center; justify-content: flex-end; gap: 10px; width: 100%; }
.table-actions :deep(.el-button) { margin-left: 0; }
.danger-dropdown-item { color: var(--el-color-danger); }
.expired { color: var(--el-color-danger); }
.dialog-form { margin-top: 18px; }
.expiry-row { display: grid; gap: 12px; width: 100%; }
.prompt-card { padding: 16px 18px; border: 1px solid var(--el-border-color); border-radius: 12px; background: linear-gradient(135deg, var(--el-fill-color-light), var(--el-color-success-light-9)); }
.prompt-card p { margin: 8px 0 0; line-height: 1.75; color: var(--el-text-color-regular); }
.prompt-title { font-weight: 700; }
.copy-actions { display: flex; justify-content: center; gap: 12px; margin-top: 18px; }
</style>
