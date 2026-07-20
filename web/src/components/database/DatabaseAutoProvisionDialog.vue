<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { Loading } from '@element-plus/icons-vue'

import { apiClient, type DBAccountRecord, type DatabaseInstanceView } from '@/api/client'
import { createLatestKeyedRequest } from '@/utils/connectionRequestState'
import {
  createProvisionIdempotencySession,
  type ProvisionRequest,
} from '@/utils/provisioningRequest'

interface DBGrantRow {
  database: string
  privilege: '' | 'read' | 'readwrite'
}

const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  instance: DatabaseInstanceView | null
}>()

const emit = defineEmits<{
  created: []
}>()

const adminAccounts = ref<DBAccountRecord[]>([])
const selectedAdminAccountId = shallowRef('')
const databaseGrants = ref<DBGrantRow[]>([])
const loadingAccounts = shallowRef(false)
const loadingDatabases = shallowRef(false)
const provisioning = shallowRef(false)
const databaseError = shallowRef('')
const provisionError = shallowRef('')
const provisionedAccount = shallowRef<DBAccountRecord | null>(null)
const adminAccountRequests = createLatestKeyedRequest<DBAccountRecord[]>()
const databaseRequests = createLatestKeyedRequest<string[]>()
const provisionIdempotency = createProvisionIdempotencySession()

const hasDatabases = computed(() => databaseGrants.value.length > 0)
const hasSelectedGrant = computed(() => (
  databaseGrants.value.some(row => row.privilege !== '')
))
const canProvision = computed(() => (
  Boolean(selectedAdminAccountId.value)
  && hasDatabases.value
  && hasSelectedGrant.value
  && !loadingAccounts.value
  && !loadingDatabases.value
  && !provisioning.value
))

watch(
  () => [visible.value, String(props.instance?.id || '')] as const,
  ([isVisible, instanceID]) => {
    if (!isVisible && provisioning.value) {
      visible.value = true
      return
    }
    if (!isVisible || !instanceID) {
      resetDialog()
      return
    }
    void initializeDialog(instanceID)
  },
  { immediate: true },
)

watch(selectedAdminAccountId, accountID => {
  const instanceID = String(props.instance?.id || '')
  if (!visible.value || !instanceID || !accountID) {
    databaseRequests.invalidate()
    databaseGrants.value = []
    databaseError.value = ''
    return
  }
  void loadDatabases(instanceID, accountID)
})

async function fetchAllAccounts(instanceID: string): Promise<DBAccountRecord[]> {
  const pageSize = 200
  const accounts: DBAccountRecord[] = []
  let page = 1
  let total = 0
  do {
    const response = await apiClient.getDBAccounts(instanceID, {
      page,
      page_size: pageSize,
    })
    accounts.push(...(response.items ?? []))
    total = response.total ?? accounts.length
    page += 1
    if (!response.items?.length) break
  } while (accounts.length < total)
  return accounts
}

async function initializeDialog(instanceID: string) {
  resetDialogState()
  const request = adminAccountRequests.begin(
    instanceID,
    () => fetchAllAccounts(instanceID),
  )
  loadingAccounts.value = true
  try {
    const accounts = await request.promise
    if (
      !visible.value
      || String(props.instance?.id || '') !== instanceID
      || !adminAccountRequests.isCurrent(request.token, instanceID)
    ) {
      return
    }
    adminAccounts.value = accounts.filter(account => account.status === 'active')
    selectedAdminAccountId.value = String(adminAccounts.value[0]?.id || '')
  } catch (error) {
    if (!adminAccountRequests.isCurrent(request.token, instanceID)) return
    adminAccounts.value = []
    databaseError.value = errorMessage(error, '管理员凭据加载失败')
  } finally {
    if (
      visible.value
      && String(props.instance?.id || '') === instanceID
      && adminAccountRequests.isCurrent(request.token, instanceID)
    ) {
      loadingAccounts.value = false
    }
  }
}

async function loadDatabases(instanceID: string, accountID: string) {
  databaseGrants.value = []
  databaseError.value = ''
  provisionError.value = ''
  provisionedAccount.value = null
  const key = `${instanceID}:${accountID}`
  const request = databaseRequests.begin(
    key,
    async () => {
      const response = await apiClient.listDBDatabases(instanceID, accountID)
      return response.databases ?? []
    },
  )
  loadingDatabases.value = true
  try {
    const databases = await request.promise
    if (
      !visible.value
      || String(props.instance?.id || '') !== instanceID
      || selectedAdminAccountId.value !== accountID
      || !databaseRequests.isCurrent(request.token, key)
    ) {
      return
    }
    databaseGrants.value = databases.map(database => ({
      database,
      privilege: '',
    }))
  } catch (error) {
    if (!databaseRequests.isCurrent(request.token, key)) return
    databaseError.value = errorMessage(error, '获取数据库列表失败')
  } finally {
    if (
      visible.value
      && String(props.instance?.id || '') === instanceID
      && selectedAdminAccountId.value === accountID
      && databaseRequests.isCurrent(request.token, key)
    ) {
      loadingDatabases.value = false
    }
  }
}

function setAllDatabaseGrants(privilege: DBGrantRow['privilege']) {
  databaseGrants.value.forEach(row => {
    row.privilege = privilege
  })
}

async function provisionAccount() {
  const instanceID = String(props.instance?.id || '')
  const adminAccountID = selectedAdminAccountId.value
  if (!instanceID || !adminAccountID || !canProvision.value) return

  const payload: ProvisionRequest = {
    admin_account_id: adminAccountID,
    grants: databaseGrants.value
      .filter(row => row.privilege !== '')
      .map(row => ({
        database: row.database,
        privilege: row.privilege,
      })),
  }
  const idempotencyKey = provisionIdempotency.keyFor(payload, instanceID)
  provisioning.value = true
  provisionError.value = ''
  try {
    const response = await apiClient.provisionDBAccount(
      instanceID,
      payload,
      idempotencyKey,
    )
    if (response.ok === false) throw new Error('自动创建未成功，请重试')
    provisionedAccount.value = response.account
    provisionIdempotency.markSucceeded()
    emit('created')
  } catch (error) {
    provisionIdempotency.markFailed()
    provisionError.value = errorMessage(error, '自动创建失败')
  } finally {
    provisioning.value = false
  }
}

function finishProvisioning() {
  visible.value = false
}

function resetDialogState() {
  adminAccountRequests.invalidate()
  databaseRequests.invalidate()
  provisionIdempotency.reset()
  adminAccounts.value = []
  selectedAdminAccountId.value = ''
  databaseGrants.value = []
  loadingAccounts.value = false
  loadingDatabases.value = false
  provisioning.value = false
  databaseError.value = ''
  provisionError.value = ''
  provisionedAccount.value = null
}

function resetDialog() {
  if (
    adminAccounts.value.length === 0
    && !selectedAdminAccountId.value
    && databaseGrants.value.length === 0
    && !loadingAccounts.value
    && !loadingDatabases.value
    && !provisioning.value
    && !databaseError.value
    && !provisionError.value
    && !provisionedAccount.value
  ) {
    return
  }
  resetDialogState()
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}
</script>

<template>
  <el-dialog
    v-model="visible"
    title="自动创建 MySQL 账号"
    class="crud-form-dialog"
    destroy-on-close
    :close-on-click-modal="!provisioning"
    :close-on-press-escape="!provisioning"
    :show-close="!provisioning"
  >
    <el-form class="auto-provision-form" label-position="top">
      <el-form-item label="管理员凭据" required>
        <el-select
          v-model="selectedAdminAccountId"
          :loading="loadingAccounts"
          :disabled="provisioning"
          placeholder="选择用于创建账号的凭据"
        >
          <el-option
            v-for="account in adminAccounts"
            :key="account.id"
            :label="`${account.username} (${account.unique_name})`"
            :value="account.id"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="数据库权限">
        <div class="database-grants">
          <div v-if="loadingDatabases" class="provision-loading">
            <el-icon class="is-loading" :size="24"><Loading /></el-icon>
            <p>正在获取数据库列表…</p>
          </div>

          <el-alert
            v-else-if="databaseError"
            type="error"
            :title="databaseError"
            :closable="false"
            show-icon
          />

          <template v-else-if="hasDatabases">
            <div class="grant-actions">
              <el-button
                size="small"
                :disabled="provisioning"
                @click="setAllDatabaseGrants('readwrite')"
              >
                全部读写
              </el-button>
              <el-button
                size="small"
                :disabled="provisioning"
                @click="setAllDatabaseGrants('read')"
              >
                全部只读
              </el-button>
              <el-button
                size="small"
                :disabled="provisioning"
                @click="setAllDatabaseGrants('')"
              >
                全部无
              </el-button>
            </div>
            <el-table
              class="database-grants-table"
              :data="databaseGrants"
              size="small"
              max-height="340"
            >
              <el-table-column prop="database" label="数据库" show-overflow-tooltip />
              <el-table-column label="权限" width="180" align="center">
                <template #default="{ row }">
                  <el-radio-group
                    v-model="row.privilege"
                    size="small"
                    :disabled="provisioning"
                  >
                    <el-radio-button value="">无</el-radio-button>
                    <el-radio-button value="read">读</el-radio-button>
                    <el-radio-button value="readwrite">读写</el-radio-button>
                  </el-radio-group>
                </template>
              </el-table-column>
            </el-table>
          </template>

          <el-empty
            v-else
            :description="selectedAdminAccountId ? '该凭据未读取到数据库' : '请选择管理员凭据'"
            :image-size="72"
          />
        </div>
      </el-form-item>
    </el-form>

    <el-alert
      v-if="provisionError"
      class="provision-feedback"
      type="error"
      :title="provisionError"
      :closable="false"
      show-icon
    />
    <el-alert
      v-else-if="provisionedAccount"
      class="provision-feedback"
      type="success"
      title="账号创建成功"
      :description="`资源标识：${provisionedAccount.resource_id || '-'}`"
      :closable="false"
      show-icon
    />

    <template #footer>
      <el-button v-if="provisionedAccount" type="primary" @click="finishProvisioning">
        完成
      </el-button>
      <template v-else>
        <el-button :disabled="provisioning" @click="visible = false">取消</el-button>
        <el-button
          type="primary"
          :disabled="!canProvision"
          :loading="provisioning"
          @click="provisionAccount"
        >
          创建
        </el-button>
      </template>
    </template>
  </el-dialog>
</template>

<style scoped>
.auto-provision-form :deep(.el-select) {
  width: 100%;
}

.database-grants {
  min-width: 0;
  width: 100%;
}

.grant-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 8px;
}

.grant-actions :deep(.el-button) {
  margin: 0;
}

.database-grants-table {
  width: 100%;
}

.provision-loading {
  padding: 30px 0;
  color: var(--color-text-secondary);
  text-align: center;
}

.provision-loading p {
  margin: 10px 0 0;
}

.provision-feedback {
  margin-top: 12px;
}
</style>
