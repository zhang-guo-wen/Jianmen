<script setup lang="ts">
import { computed } from 'vue'

import FormDialog from '@/components/FormDialog.vue'

interface ConnectionTestResult {
  ok: boolean
  error?: string
  latency_ms?: number
}

const visible = defineModel<boolean>('visible', { required: true })
const username = defineModel<string>('username', { required: true })
const password = defineModel<string>('password', { required: true })
const group = defineModel<string>('group', { required: true })
const remark = defineModel<string>('remark', { required: true })
const expiresAt = defineModel<Date | null>('expiresAt', { required: true })
const morePanels = defineModel<string[]>('morePanels', { required: true })

const props = withDefaults(defineProps<{
  editing: boolean
  protocol?: string
  loading?: boolean
  testing?: boolean
  testResult?: ConnectionTestResult | null
  groupOptions?: string[]
}>(), {
  protocol: 'mysql',
  loading: false,
  testing: false,
  testResult: null,
  groupOptions: () => [],
})

const emit = defineEmits<{
  submit: []
  test: []
}>()

const isRedis = computed(() => props.protocol === 'redis')
const isPermanent = computed(() => expiresAt.value === null)
const usernamePlaceholder = computed(() => (
  isRedis.value
    ? 'Redis ACL 用户名（可选，留空则使用单一密码认证）'
    : '数据库登录账号'
))
const passwordPlaceholder = computed(() => (
  props.editing ? '留空则保留原密码' : '数据库登录密码'
))

function setPermanentExpiry() {
  expiresAt.value = null
}
</script>

<template>
  <FormDialog
    v-model:visible="visible"
    :title="editing ? '编辑账号' : '新增账号'"
    :loading="loading"
    @submit="emit('submit')"
  >
    <el-form class="database-account-form" label-position="top">
      <el-form-item label="登录账号" :required="!isRedis">
        <el-input v-model="username" :placeholder="usernamePlaceholder" />
      </el-form-item>

      <el-form-item label="登录密码">
        <el-input
          v-model="password"
          type="password"
          show-password
          :placeholder="passwordPlaceholder"
        />
      </el-form-item>

      <el-form-item label="连接测试">
        <div class="test-connection-row">
          <el-button :loading="testing" @click="emit('test')">测试连接</el-button>
          <template v-if="testResult">
            <el-tag :type="testResult.ok ? 'success' : 'danger'" size="small">
              {{ testResult.ok ? '可达' : '不可达' }}
            </el-tag>
            <span v-if="testResult.latency_ms !== undefined" class="test-connection-meta">
              延迟 {{ testResult.latency_ms }}ms
            </span>
            <span v-if="testResult.error" class="test-connection-error">
              {{ testResult.error }}
            </span>
          </template>
        </div>
      </el-form-item>

      <el-form-item label="有效期">
        <div class="expiry-control">
          <el-date-picker
            v-model="expiresAt"
            type="datetime"
            class="expiry-picker"
            clearable
            format="YYYY-MM-DD HH:mm"
            placeholder="选择过期时间"
          />
          <el-button
            size="small"
            :type="isPermanent ? 'primary' : undefined"
            @click="setPermanentExpiry"
          >
            永久
          </el-button>
        </div>
      </el-form-item>

      <el-collapse v-model="morePanels">
        <el-collapse-item name="more" title="更多设置">
          <el-form-item label="分组">
            <el-select
              v-model="group"
              allow-create
              clearable
              default-first-option
              filterable
              placeholder="选择或输入分组"
            >
              <el-option
                v-for="option in groupOptions"
                :key="option"
                :label="option"
                :value="option"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="备注">
            <el-input v-model="remark" type="textarea" placeholder="备注信息" />
          </el-form-item>
        </el-collapse-item>
      </el-collapse>
    </el-form>
  </FormDialog>
</template>

<style scoped>
.database-account-form :deep(.el-select),
.database-account-form :deep(.el-date-editor) {
  width: 100%;
}

.expiry-control {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) auto;
  gap: 8px;
  align-items: center;
  width: 100%;
}

.expiry-picker {
  width: 100%;
}

.test-connection-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.test-connection-meta {
  color: #667085;
  font-size: 12px;
}

.test-connection-error {
  color: var(--el-color-danger);
  font-size: 12px;
}

@media (max-width: 720px) {
  .expiry-control {
    grid-template-columns: 1fr;
  }
}
</style>
