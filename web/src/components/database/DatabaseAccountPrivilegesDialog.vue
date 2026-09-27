<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Loading } from '@element-plus/icons-vue'

import {
  apiClient,
  type DBAccountPrivilegeGrant,
  type DBAccountPrivilegesResponse,
  type DBAccountRecord,
  type DatabaseInstanceView,
} from '@/api/client'
import { createLatestKeyedRequest } from '@/utils/connectionRequestState'

const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  instance: DatabaseInstanceView | null
  account: DBAccountRecord | null
}>()

const loading = ref(false)
const error = ref('')
const result = ref<DBAccountPrivilegesResponse | null>(null)
const requests = createLatestKeyedRequest<DBAccountPrivilegesResponse>()

const title = computed(() => {
  const username = props.account?.username || props.account?.unique_name || '账号'
  const instanceName = props.instance?.name || ''
  return instanceName ? `${instanceName} · ${username} · 权限` : `${username} · 权限`
})

const grants = computed<DBAccountPrivilegeGrant[]>(() => result.value?.grants ?? [])

const privilegeMeta = (privilege: string): { label: string; type: 'success' | 'primary' | 'info' } => {
  switch (privilege) {
    case 'read':
      return { label: '读', type: 'primary' }
    case 'readwrite':
      return { label: '读写', type: 'success' }
    default:
      return { label: '其它', type: 'info' }
  }
}

watch(
  () => [visible.value, String(props.account?.id || '')] as const,
  ([isVisible, accountID]) => {
    if (!isVisible || !accountID) {
      if (!isVisible) {
        loading.value = false
        error.value = ''
        result.value = null
        requests.invalidate()
      }
      return
    }
    void loadPrivileges(accountID)
  },
  { immediate: true },
)

async function loadPrivileges(accountID: string) {
  requests.invalidate()
  const key = accountID
  const request = requests.begin(key, () => apiClient.getDBAccountPrivileges(accountID))
  loading.value = true
  error.value = ''
  result.value = null
  try {
    const response = await request.promise
    if (!visible.value || String(props.account?.id || '') !== accountID || !requests.isCurrent(request.token, key)) {
      return
    }
    result.value = response
  } catch (err) {
    if (!visible.value || String(props.account?.id || '') !== accountID || !requests.isCurrent(request.token, key)) {
      return
    }
    error.value = err instanceof Error ? err.message : '查询账号权限失败'
  } finally {
    if (visible.value && String(props.account?.id || '') === accountID && requests.isCurrent(request.token, key)) {
      loading.value = false
    }
  }
}

function formatPrivileges(privileges: string[]): string {
  if (privileges.length === 0) return '-'
  return privileges.join(', ')
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="title"
    class="crud-form-dialog"
    destroy-on-close
    width="min(760px, calc(100vw - 32px))"
  >
    <div class="account-privileges">
      <div v-if="loading" class="privileges-loading">
        <el-icon class="is-loading" :size="24"><Loading /></el-icon>
        <p>正在查询账号权限…</p>
      </div>

      <el-alert
        v-else-if="error"
        type="error"
        :title="error"
        :closable="false"
        show-icon
      />

      <template v-else-if="result">
        <div class="privileges-summary">
          <el-descriptions :column="2" size="small" border>
            <el-descriptions-item label="登录账号">{{ result.username || '-' }}</el-descriptions-item>
            <el-descriptions-item label="协议">{{ result.protocol || '-' }}</el-descriptions-item>
          </el-descriptions>
        </div>

        <el-alert
          v-if="result.warnings?.length"
          class="privileges-warning"
          type="warning"
          :title="result.warnings.join('；')"
          :closable="false"
          show-icon
        />

        <div v-if="grants.length" class="privileges-table-wrap">
          <el-table :data="grants" size="small" max-height="360">
            <el-table-column prop="database" label="数据库" show-overflow-tooltip />
            <el-table-column label="权限" width="120" align="center">
              <template #default="{ row }">
                <el-tag size="small" :type="privilegeMeta(row.privilege).type" effect="light">
                  {{ privilegeMeta(row.privilege).label }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="具体权限" min-width="220">
              <template #default="{ row }">{{ formatPrivileges(row.privileges) }}</template>
            </el-table-column>
          </el-table>
        </div>

        <el-empty
          v-else
          description="该账号未查询到按数据库授权，可能在数据库上只有全局（*.*）或表级授权"
          :image-size="72"
        />
      </template>
      <el-empty v-else description="没有可查询的账号" :image-size="72" />
    </div>

    <template #footer>
      <el-button type="primary" @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.account-privileges {
  min-width: 0;
}

.privileges-loading {
  padding: 30px 0;
  color: var(--color-text-secondary);
  text-align: center;
}

.privileges-loading p {
  margin: 10px 0 0;
}

.privileges-summary {
  margin-bottom: 12px;
}

.privileges-warning {
  margin-bottom: 12px;
}

.privileges-table-wrap {
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  overflow: hidden;
}
</style>
