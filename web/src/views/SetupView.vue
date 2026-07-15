<template>
  <div class="setup-container">
    <el-card class="setup-card">
      <template v-if="loadingStatus">
        <div class="setup-success">
          <el-skeleton :rows="4" animated />
        </div>
      </template>

      <!-- Already initialized -->
      <template v-else-if="alreadyInitialized">
        <div class="setup-success">
          <el-icon :size="48" color="#409eff"><CircleCheckFilled /></el-icon>
          <h3>{{ t('setup.alreadyInitialized') }}</h3>
          <p class="setup-desc">{{ t('setup.alreadyInitializedHint') }}</p>
        </div>
        <div class="admin-summary">
          <div class="admin-summary-row">
            <span>{{ t('setup.username') }}</span>
            <strong>{{ initAdmin.username || '-' }}</strong>
          </div>
          <div class="admin-summary-row">
            <span>{{ t('users.displayName') }}</span>
            <strong>{{ initAdmin.display_name || '-' }}</strong>
          </div>
          <div class="admin-summary-row">
            <span>{{ t('setup.email') }}</span>
            <strong>{{ initAdmin.email || '-' }}</strong>
          </div>
        </div>
        <el-button type="primary" class="setup-submit-btn" @click="handleFinish">
          {{ t('setup.goToLogin') }}
        </el-button>
      </template>

      <!-- Step 1: Create admin user -->
      <template v-else-if="step === 1 && !encryptionKeyNeeded">
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
          <el-form-item :label="t('users.displayName')" prop="display_name">
            <el-input v-model="form.display_name" placeholder="可选，如：超级管理员" />
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

      <!-- Step 2: Complete — show encryption key and guide to login -->
      <template v-else-if="step === 2">
        <div class="setup-success">
          <el-icon :size="48" color="#67c23a"><CircleCheckFilled /></el-icon>
          <h3>{{ t('setup.adminCreated') }}</h3>
          <p style="color: #667085; margin-top: 8px;">{{ t('setup.loginHint') }}</p>
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

      <!-- Step 1.5: Admin created, retry getting encryption key -->
      <template v-else-if="encryptionKeyNeeded">
        <div class="setup-success">
          <el-icon :size="48" color="#67c23a"><CircleCheckFilled /></el-icon>
          <h3>{{ t('setup.adminCreated') }}</h3>
        </div>

        <el-alert
          type="warning"
          :title="t('setup.keyRetryHint')"
          :closable="false"
          show-icon
          style="margin-bottom: 16px"
        />

        <el-button
          type="primary"
          class="setup-submit-btn"
          :loading="submitting"
          @click="retryGetKey"
        >
          {{ t('setup.retryGetKey') }}
        </el-button>
      </template>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { CircleCheckFilled } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import type { FormInstance, FormRules } from 'element-plus';

import { apiClient, clearToken, setToken } from '@/api/client';
import { useI18n } from '@/i18n';
import { writeClipboardText } from '@/utils/clipboard';

const { t } = useI18n();
const router = useRouter();

const step = ref(1);
const loadingStatus = ref(true);
const alreadyInitialized = ref(false);
const initAdmin = reactive({
  username: '',
  display_name: '',
  email: '',
});
const submitting = ref(false);
const encryptionKey = ref('');
const encryptionKeyNeeded = ref(false);
const formRef = ref<FormInstance>();

const form = reactive({
  username: '',
  display_name: '',
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

onMounted(() => {
  void loadInitStatus();
});

async function loadInitStatus() {
  loadingStatus.value = true;
  try {
    const status = await apiClient.getInitStatus();
    alreadyInitialized.value = status.initialized;
    initAdmin.username = status.admin?.username ?? '';
    initAdmin.display_name = status.admin?.display_name ?? '';
    initAdmin.email = status.admin?.email ?? '';
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : t('setup.error.status');
    ElMessage.error(message);
  } finally {
    loadingStatus.value = false;
  }
}

async function handleSetup() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;

  submitting.value = true;
  try {
    const setupResult = await apiClient.setup({
      username: form.username.trim(),
      password: form.password,
      email: form.email.trim(),
      display_name: form.display_name.trim() || undefined,
    });
    if (!setupResult.token) {
      throw new Error(t('setup.error.setup'));
    }
    setToken(setupResult.token);

    try {
      const keyResult = await apiClient.getEncryptionKey();
      encryptionKey.value = keyResult.key ?? '';
      step.value = 2;
    } catch {
      encryptionKeyNeeded.value = true;
    }
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : t('setup.error.setup');
    ElMessage.error(message);
  } finally {
    submitting.value = false;
  }
}

async function retryGetKey() {
  submitting.value = true;
  try {
    const keyResult = await apiClient.getEncryptionKey();
    encryptionKey.value = keyResult.key ?? '';
    encryptionKeyNeeded.value = false;
    step.value = 2;
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : t('setup.error.getKey');
    ElMessage.error(message);
  } finally {
    submitting.value = false;
  }
}

async function copyKey() {
  try {
    await writeClipboardText(encryptionKey.value);
    ElMessage.success(t('quickConnect.message.copied'));
  } catch {
    ElMessage.error(t('quickConnect.error.copy'));
  }
}

function handleFinish() {
  clearToken();
  router.replace('/login');
}
</script>

<style scoped>
.setup-container {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: #f5f7fb;
}

.setup-card {
  width: min(480px, 100%);
}

.setup-header {
  margin-bottom: 24px;
}

.setup-header h2 {
  margin: 0 0 8px;
  font-size: 24px;
}

.setup-desc {
  margin: 0;
  color: #667085;
}

.setup-success {
  text-align: center;
  margin-bottom: 24px;
}

.admin-summary {
  margin-bottom: 20px;
  padding: 16px;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  background: #f8fafc;
}

.admin-summary-row {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 8px 0;
  color: #667085;
}

.admin-summary-row + .admin-summary-row {
  border-top: 1px solid #edf0f5;
}

.admin-summary-row strong {
  color: #1f2937;
  text-align: right;
  word-break: break-all;
}

.setup-success h3 {
  margin: 12px 0 0;
}

.key-section {
  margin-bottom: 24px;
}

.key-section h4 {
  margin: 0 0 4px;
}

.key-hint {
  margin: 0 0 8px;
  font-size: 13px;
  color: #667085;
}

.key-hint.warning {
  color: #e6a23c;
}

.key-display {
  display: flex;
  gap: 8px;
  align-items: center;
}

.key-display code {
  flex: 1;
  padding: 8px 12px;
  background: #f9fafb;
  border: 1px solid #e5e7eb;
  border-radius: 4px;
  font-size: 12px;
  word-break: break-all;
  user-select: all;
}

.setup-submit-btn {
  width: 100%;
}
</style>
