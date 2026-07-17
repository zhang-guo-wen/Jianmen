<template>
  <main class="login-page">
    <el-card class="login-card" shadow="never">
      <div class="login-card-header">
        <div>
          <h1 class="login-title">Jianmen</h1>
          <p class="login-subtitle">{{ t('login.subtitle') }}</p>
          <p class="login-description">{{ t('login.description') }}</p>
        </div>
      </div>
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('login.username')" prop="username">
          <el-input
            v-model="form.username"
            autocomplete="username"
            :placeholder="t('login.usernamePlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="t('login.password')" prop="password">
          <el-input
            v-model="form.password"
            autocomplete="current-password"
            :placeholder="t('login.passwordPlaceholder')"
            show-password
            type="password"
            @keyup.enter="submit"
          />
        </el-form-item>
        <div class="login-captcha-section">
          <div class="login-captcha-label">{{ t('login.captchaLabel') }}</div>
          <altcha-widget
            ref="captchaRef"
            auto="off"
            challenge="/api/login/challenge"
            language="zh-cn"
            type="checkbox"
            @statechange="handleCaptchaStateChange"
            @verified="handleCaptchaVerified"
          />
          <p class="login-captcha-hint">{{ t('login.captchaHint') }}</p>
        </div>
        <div v-if="loginError" class="login-error">{{ loginError }}</div>
        <el-button
          class="login-button"
          :disabled="!captchaPayload"
          :loading="submitting"
          native-type="submit"
          type="primary"
        >
          {{ t('login.signIn') }}
        </el-button>
      </el-form>
    </el-card>
  </main>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import type { FormInstance, FormRules } from 'element-plus';
import 'altcha';
import 'altcha/i18n/zh-cn';
import 'altcha/altcha.css';

import { apiClient, setToken } from '@/api/client';
import { useI18n } from '@/i18n';
import { usePreferencesStore } from '@/stores/preferences';
import { resolveLoginRedirect } from '@/utils/loginRedirect';

interface LoginCaptchaWidget extends HTMLElement {
  reset: () => void;
}

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const preferences = usePreferencesStore();
const formRef = ref<FormInstance>();
const captchaRef = ref<LoginCaptchaWidget>();
const captchaPayload = ref('');
const submitting = ref(false);
const loginError = ref('');

const form = reactive({
  username: '',
  password: '',
});

const rules: FormRules = {
  username: [{ required: true, message: t('login.usernameRequired'), trigger: 'blur' }],
  password: [{ required: true, message: t('login.passwordRequired'), trigger: 'blur' }],
};

function handleCaptchaVerified(event: Event) {
  const payload = (event as CustomEvent<{ payload?: string }>).detail?.payload;
  captchaPayload.value = payload ?? '';
  if (captchaPayload.value) {
    loginError.value = '';
  }
}

function handleCaptchaStateChange(event: Event) {
  const state = (event as CustomEvent<{ state?: string }>).detail?.state;
  if (state !== 'verified') {
    captchaPayload.value = '';
  }
}

function resetCaptcha() {
  captchaPayload.value = '';
  captchaRef.value?.reset();
}

async function submit() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;
  if (!captchaPayload.value) {
    loginError.value = t('login.captchaRequired');
    return;
  }

  submitting.value = true;
  loginError.value = '';
  try {
    const resp = await apiClient.login(form.username.trim(), form.password, captchaPayload.value);
    const token = resp.token;
    if (!token) {
      loginError.value = t('login.tokenMissing');
      resetCaptcha();
      return;
    }
    setToken(token);
    await preferences.fetch({ force: true }).catch(() => undefined);
    const redirect = resolveLoginRedirect(route.query.redirect);
    if (redirect.external) {
      window.location.assign(redirect.target);
      return;
    }
    await router.push(redirect.target);
  } catch (err: any) {
    resetCaptcha();
    loginError.value = err?.message || t('login.error');
  } finally {
    submitting.value = false;
  }
}
</script>

<style scoped>
.login-error {
  margin-bottom: 16px;
  padding: 8px 12px;
  border-radius: 4px;
  background: #fef2f2;
  color: #dc2626;
  font-size: 13px;
}

.login-card-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.login-description {
  margin: -12px 0 24px;
  color: var(--color-text-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.login-captcha-section {
  margin: 4px 0 18px;
  padding: 14px 16px;
  border: 1px solid var(--color-border);
  border-radius: 14px;
  background: var(--color-surface-muted);
}

.login-captcha-label {
  margin-bottom: 8px;
  color: var(--color-text-primary);
  font-size: 13px;
  font-weight: 600;
}

.login-captcha-hint {
  margin: 8px 0 0;
  color: var(--color-text-secondary);
  font-size: 12px;
}

.login-button {
  width: 100%;
}
</style>
