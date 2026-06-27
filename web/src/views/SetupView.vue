<template>
  <div class="setup-container">
    <el-card class="setup-card">
      <!-- 步骤 1: 创建管理员 -->
      <template v-if="step === 1">
        <div class="setup-header">
          <h2>{{ t('setup.title') }}</h2>
          <p class="setup-desc">{{ t('setup.description') }}</p>
        </div>
        <el-form
          ref="formRef"
          :model="form"
          :rules="rules"
          label-position="top"
          @submit.prevent="handleSetup"
        >
          <el-form-item :label="t('setup.username')" prop="username">
            <el-input v-model="form.username" autocomplete="username" />
          </el-form-item>
          <el-form-item :label="t('setup.email')" prop="email">
            <el-input v-model="form.email" type="email" autocomplete="email" />
          </el-form-item>
          <el-form-item :label="t('setup.password')" prop="password">
            <el-input
              v-model="form.password"
              type="password"
              show-password
              autocomplete="new-password"
            />
          </el-form-item>
          <el-form-item :label="t('setup.confirmPassword')" prop="confirmPassword">
            <el-input
              v-model="form.confirmPassword"
              type="password"
              show-password
              autocomplete="new-password"
            />
          </el-form-item>
          <el-form-item>
            <el-button
              type="primary"
              native-type="submit"
              :loading="submitting"
              class="setup-submit-btn"
            >
              {{ t('setup.submit') }}
            </el-button>
          </el-form-item>
        </el-form>
      </template>

      <!-- 步骤 2: 显示加密密钥和 API Token -->
      <template v-else-if="step === 2">
        <div class="setup-success">
          <el-icon :size="48" color="#67c23a"><CircleCheckFilled /></el-icon>
          <h3>{{ t('setup.success') }}</h3>
        </div>

        <div class="key-section">
          <h4>{{ t('setup.apiToken') }}</h4>
          <p class="key-hint">{{ t('setup.apiTokenHint') }}</p>
          <div class="key-display">
            <code>{{ apiToken }}</code>
            <el-button size="small" @click="copyToken">
              {{ t('setup.copy') }}
            </el-button>
          </div>
        </div>

        <div class="key-section">
          <h4>{{ t('setup.encryptionKey') }}</h4>
          <p class="key-hint warning">{{ t('setup.encryptionKeyHint') }}</p>
          <div class="key-display">
            <code>{{ encryptionKey }}</code>
            <el-button size="small" type="warning" @click="copyKey">
              {{ t('setup.copy') }}
            </el-button>
          </div>
          <el-alert
            type="warning"
            :title="t('setup.encryptionKeyWarning')"
            :closable="false"
            show-icon
            style="margin-top: 12px"
          />
        </div>

        <el-button type="primary" class="setup-submit-btn" @click="handleFinish">
          {{ t('setup.goToLogin') }}
        </el-button>
      </template>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue';
import { useRouter } from 'vue-router';
import { CircleCheckFilled } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import type { FormInstance, FormRules } from 'element-plus';

import { apiClient, setToken } from '@/api/client';
import { useI18n } from '@/i18n';

const { t } = useI18n();
const router = useRouter();

const step = ref(1);
const submitting = ref(false);
const apiToken = ref('');
const encryptionKey = ref('');
const formRef = ref<FormInstance>();

const form = reactive({
  username: '',
  email: '',
  password: '',
  confirmPassword: '',
});

const validateConfirm = (_rule: unknown, value: string, callback: (err?: Error) => void) => {
  if (value !== form.password) {
    callback(new Error(t('setup.passwordMismatch')));
  } else {
    callback();
  }
};

const rules: FormRules = {
  username: [
    { required: true, message: t('setup.validation.required'), trigger: 'blur' },
    { min: 2, max: 64, message: t('setup.validation.usernameLength'), trigger: 'blur' },
  ],
  email: [
    { type: 'email', message: t('setup.validation.email'), trigger: 'blur' },
  ],
  password: [
    { required: true, message: t('setup.validation.required'), trigger: 'blur' },
    { min: 8, message: t('setup.validation.passwordLength'), trigger: 'blur' },
  ],
  confirmPassword: [
    { required: true, message: t('setup.validation.required'), trigger: 'blur' },
    { validator: validateConfirm, trigger: 'blur' },
  ],
};

async function handleSetup() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;

  submitting.value = true;
  try {
    const result = await apiClient.setup({
      username: form.username.trim(),
      password: form.password,
      email: form.email.trim(),
    });

    apiToken.value = result.token ?? '';

    const keyResult = await apiClient.getEncryptionKey();
    encryptionKey.value = keyResult.key ?? '';

    step.value = 2;
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : t('setup.error.setup');
    ElMessage.error(message);
  } finally {
    submitting.value = false;
  }
}

async function copyToken() {
  try {
    await navigator.clipboard.writeText(apiToken.value);
    ElMessage.success(t('quickConnect.message.copied'));
  } catch {
    ElMessage.error(t('quickConnect.error.copy'));
  }
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(encryptionKey.value);
    ElMessage.success(t('quickConnect.message.copied'));
  } catch {
    ElMessage.error(t('quickConnect.error.copy'));
  }
}

function handleFinish() {
  setToken(apiToken.value);
  router.replace('/dashboard');
}
</script>

<style scoped>
.setup-container {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-bg-color-page);
}

.setup-card {
  width: 480px;
  max-width: 90vw;
}

.setup-header {
  text-align: center;
  margin-bottom: 24px;
}

.setup-header h2 {
  margin-bottom: 8px;
}

.setup-desc {
  color: var(--el-text-color-secondary);
}

.setup-submit-btn {
  width: 100%;
}

.setup-success {
  text-align: center;
  margin-bottom: 24px;
}

.setup-success h3 {
  margin-top: 12px;
}

.key-section {
  margin-bottom: 20px;
}

.key-section h4 {
  margin-bottom: 4px;
}

.key-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
}

.key-hint.warning {
  color: var(--el-color-warning);
}

.key-display {
  display: flex;
  gap: 8px;
  align-items: center;
}

.key-display code {
  flex: 1;
  padding: 8px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  font-size: 12px;
  word-break: break-all;
  user-select: all;
}
</style>
