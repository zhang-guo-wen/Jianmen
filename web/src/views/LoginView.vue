<template>
  <main class="login-page">
    <el-card class="login-card" shadow="never">
      <div class="login-card-header">
        <div>
          <h1 class="login-title">Jianmen</h1>
          <p class="login-subtitle">{{ t('login.subtitle') }}</p>
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
        <div v-if="loginError" class="login-error">{{ loginError }}</div>
        <el-button class="login-button" :loading="submitting" native-type="submit" type="primary">
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

import { apiClient, setToken } from '@/api/client';
import { useI18n } from '@/i18n';
import { usePreferencesStore } from '@/stores/preferences';
import { resolveLoginRedirect } from '@/utils/loginRedirect';

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const preferences = usePreferencesStore();
const formRef = ref<FormInstance>();
const submitting = ref(false);
const loginError = ref('');

const form = reactive({
  username: '',
  password: '',
});

const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
};

async function submit() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;

  submitting.value = true;
  loginError.value = '';
  try {
    const resp = await apiClient.login(form.username.trim(), form.password);
    const token = resp.token;
    if (!token) {
      loginError.value = '登录失败：未获取到凭证';
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
    loginError.value = err?.message || '登录失败，请检查用户名和密码';
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

.login-button {
  width: 100%;
}
</style>
