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
            <span :class="{ expired: isExpired(row.expires_at) }">{{ row.expires_at ? formatTime(row.expires_at) : '&#x6C38;&#x4E45;' }}</span>
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

    <el-dialog v-model="temporaryResultDialogVisible" title="&#x4E34;&#x65F6;&#x8D26;&#x53F7;&#x4FE1;&#x606F;" width="560px" destroy-on-close>
      <el-alert title="&#x4EE5;&#x4E0B;&#x51ED;&#x636E;&#x5DF2;&#x81EA;&#x52A8;&#x590D;&#x5236;&#xFF0C;&#x6709;&#x6548;&#x671F;&#x4E0E;&#x6388;&#x6743;&#x4E00;&#x81F4;&#x3002;" type="success" show-icon :closable="false" />
      <div v-if="temporaryResult?.connection" class="credential-card">
        <div class="credential-row"><span>&#x5730;&#x5740;</span><code>{{ temporaryResult.connection.address }}</code></div>
        <div class="credential-row"><span>&#x8D26;&#x53F7;</span><code>{{ temporaryResult.connection.username }}</code></div>
        <div class="credential-row"><span>&#x5BC6;&#x7801;</span><code>{{ temporaryResult.connection.password }}</code></div>
        <div class="credential-row"><span>&#x6709;&#x6548;&#x671F;&#x81F3;</span><span>{{ formatTime(temporaryResult.connection.expires_at) }}</span></div>
      </div>
      <template #footer>
        <el-button @click="copyTemporaryConnection">&#x518D;&#x6B21;&#x590D;&#x5236;</el-button>
        <el-button type="primary" @click="temporaryResultDialogVisible = false">&#x5B8C;&#x6210;</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="aiDialogVisible" title="AI &#x6388;&#x6743;" width="720px" class="ai-result-dialog" destroy-on-close>
      <template v-if="!aiResult">
        <el-alert title="&#x6388;&#x6743; AI &#x4F7F;&#x7528;&#x5F53;&#x524D;&#x7528;&#x6237;&#x7684;&#x8D44;&#x6E90;&#x7684;&#x6743;&#x9650;&#xFF0C;&#x8BBF;&#x95EE;&#x4EE4;&#x724C;&#x9ED8;&#x8BA4; 48 &#x5C0F;&#x65F6;&#xFF0C;&#x5237;&#x65B0;&#x4EE4;&#x724C;&#x9ED8;&#x8BA4; 30 &#x5929;&#x3002;" type="warning" show-icon :closable="false" />
        <el-form label-width="100px" class="dialog-form">

          <el-form-item label="&#x6709;&#x6548;&#x671F;" required>
            <div class="expiry-row">
              <el-segmented v-model="aiDuration" :options="aiDurations" @change="applyAIDuration" />
              <el-date-picker v-if="aiDuration !== 'permanent'" v-model="aiForm.expires_at" type="datetime" placeholder="&#x9009;&#x62E9;&#x5230;&#x671F;&#x65F6;&#x95F4;" :disabled-date="disablePastDate" />
              <span v-else class="permanent-option">&#x6C38;&#x4E45;&#x6709;&#x6548;</span>
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
        <div class="ai-docs-card">
          <div class="ai-docs-header">
            <div>
              <div class="prompt-title">AI &#x6587;&#x6863;</div>
              <el-link
                v-if="aiResult.docs_url"
                :href="aiResult.docs_url"
                target="_blank"
                rel="noopener noreferrer"
                type="primary"
                class="ai-docs-link"
              >{{ aiResult.docs_url }}</el-link>
            </div>
            <el-button v-if="aiResult.docs_content" link type="primary" @click="copyAIText(aiResult.docs_content)">&#x590D;&#x5236;&#x6587;&#x6863;</el-button>
          </div>
          <div class="ai-docs-scroll">
            <pre>{{ aiResult.docs_content || '&#x6682;&#x65E0;&#x6587;&#x6863;&#x5185;&#x5BB9;' }}</pre>
          </div>
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
const temporaryResultDialogVisible = ref(false)
const aiDialogVisible = ref(false)
const extendDialogVisible = ref(false)
const extendTarget = ref<TemporaryAccountRecord | null>(null)
const extendExpiresAt = ref<Date | null>(null)
const aiResult = ref<IssuedAIAccessToken | null>(null)
const temporaryResult = ref<TemporaryAccountRecord | null>(null)

const temporaryForm = reactive({ resource_type: 'host_account', resource_id: '', expires_at: null as Date | null, remark: '' })
const aiForm = reactive({ expires_at: null as Date | null, remark: '' })
const temporaryDuration = ref('1d')
const aiDuration = ref('7d')
const temporaryDurations = [{ label: '\u0031 \u5c0f\u65f6', value: '1h' }, { label: '\u0031 \u5929', value: '1d' }, { label: '\u0033 \u5929', value: '3d' }, { label: '\u0037 \u5929', value: '7d' }]
const aiDurations = [{ label: '\u0037 \u5929', value: '7d' }, { label: '\u0033\u0030 \u5929', value: '30d' }, { label: '\u0031 \u5e74', value: '1y' }, { label: '\u6c38\u4e45', value: 'permanent' }]
const resourceOptions = computed(() => temporaryForm.resource_type === 'host_account' ? hostAccounts.value : databaseAccounts.value)

function addDuration(value: string): Date {
  if (value === '1y') return new Date(Date.now() + 365 * 24 * 3600 * 1000)
  const hours = value.endsWith('h') ? Number(value.slice(0, -1)) : Number(value.slice(0, -1)) * 24
  return new Date(Date.now() + hours * 3600 * 1000)
}
function applyTemporaryDuration() { temporaryForm.expires_at = addDuration(temporaryDuration.value) }
function applyAIDuration() { aiForm.expires_at = aiDuration.value === 'permanent' ? null : addDuration(aiDuration.value) }
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
  temporaryResult.value = null
  aiForm.remark = ''
  aiDuration.value = '7d'
  applyAIDuration()
  aiDialogVisible.value = true
}
async function submitTemporaryAuthorization() {
  if (!temporaryForm.resource_id || !temporaryForm.expires_at) return ElMessage.warning('请完整填写授权信息')
  submitting.value = true
  try {
    const result = await apiClient.createTemporaryAuthorization({
      resource_type: temporaryForm.resource_type,
      resource_id: temporaryForm.resource_id,
      expires_at: temporaryForm.expires_at.toISOString(),
      remark: temporaryForm.remark || undefined,
    })
    temporaryResult.value = result
    temporaryDialogVisible.value = false
    temporaryResultDialogVisible.value = true
    await copyTemporaryConnection()
    await loadAccounts()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '创建临时授权失败') }
  finally { submitting.value = false }
}
async function submitAIAuthorization() {
  if (aiDuration.value !== 'permanent' && !aiForm.expires_at) return ElMessage.warning('\u8bf7\u9009\u62e9\u6709\u6548\u671f')
  submitting.value = true
  try {
    aiResult.value = await apiClient.createAIToken({
      expires_at: aiForm.expires_at?.toISOString(),
      permanent: aiDuration.value === 'permanent',
      remark: aiForm.remark || undefined,
    })
    await loadAccounts()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '创建 AI 授权失败') }
  finally { submitting.value = false }
}
function temporaryConnectionText(): string {
  const connection = temporaryResult.value?.connection
  if (!connection) return ''
  return `\u5730\u5740\uff1a${connection.address}\n\u8d26\u53f7\uff1a${connection.username}\n\u5bc6\u7801\uff1a${connection.password}`
}
async function copyTemporaryConnection() {
  const value = temporaryConnectionText()
  if (!value) return
  try {
    await writeClipboardText(value)
    ElMessage.success('\u4e34\u65f6\u8d26\u53f7\u4fe1\u606f\u5df2\u81ea\u52a8\u590d\u5236')
  } catch {
    ElMessage.warning('\u81ea\u52a8\u590d\u5236\u5931\u8d25\uff0c\u8bf7\u70b9\u51fb\u201c\u518d\u6b21\u590d\u5236\u201d')
  }
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
    const result = await apiClient.extendTemporaryAccount(extendTarget.value.id, extendExpiresAt.value.toISOString())
    if (result.connection) {
      temporaryResult.value = result
      temporaryResultDialogVisible.value = true
      await copyTemporaryConnection()
    }
    ElMessage.success('\u6709\u6548\u671f\u5df2\u5ef6\u957f')
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
.permanent-option { color: var(--el-color-success); font-weight: 600; }
.credential-card { display: grid; gap: 12px; margin-top: 18px; padding: 16px 18px; border: 1px solid var(--el-border-color); border-radius: 12px; background: var(--el-fill-color-light); }
.credential-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.credential-row > span:first-child { color: var(--el-text-color-secondary); }
.credential-row code { color: var(--el-text-color-primary); word-break: break-all; }
.prompt-card { padding: 16px 18px; border: 1px solid var(--el-border-color); border-radius: 12px; background: linear-gradient(135deg, var(--el-fill-color-light), var(--el-color-success-light-9)); }
.prompt-card p { margin: 8px 0 0; line-height: 1.75; color: var(--el-text-color-regular); }
.prompt-title { font-weight: 700; }
.ai-result-dialog :deep(.el-dialog__body) { max-height: calc(100vh - 220px); overflow-y: auto; }
.ai-docs-card { margin-top: 16px; padding: 16px 18px; border: 1px solid var(--el-border-color); border-radius: 12px; background: var(--el-fill-color-light); }
.ai-docs-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.ai-docs-link { display: block; max-width: 560px; margin-top: 6px; word-break: break-all; }
.ai-docs-scroll { max-height: 360px; margin-top: 14px; overflow: auto; border: 1px solid var(--el-border-color-lighter); border-radius: 8px; background: var(--el-bg-color); }
.ai-docs-scroll pre { margin: 0; padding: 14px 16px; color: var(--el-text-color-regular); font: 12px/1.7 "JetBrains Mono", "Microsoft YaHei", monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.copy-actions { display: flex; flex-wrap: wrap; justify-content: center; gap: 12px; margin-top: 18px; }
</style>
