<template>
  <main class="login-page">
    <el-card class="login-card" shadow="never">
      <div class="login-card-header">
        <div>
          <h1 class="login-title">Jianmen</h1>
          <p class="login-subtitle">{{ t('login.subtitle') }}</p>
        </div>
        <el-select
          v-model="selectedLocale"
          class="login-language"
          size="small"
          :aria-label="t('app.language')"
        >
          <el-option
            v-for="option in localeOptions"
            :key="option.value"
            :label="option.label"
            :value="option.value"
          />
        </el-select>
      </div>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('login.tokenLabel')">
          <el-input
            v-model="token"
            clearable
            :placeholder="t('login.tokenPlaceholder')"
            show-password
            type="password"
          />
        </el-form-item>
        <el-button class="login-button" native-type="submit" type="primary">
          {{ t('login.signIn') }}
        </el-button>
      </el-form>
    </el-card>
  </main>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { setToken } from '@/api/client';
import { useI18n, type Locale } from '@/i18n';

const route = useRoute();
const router = useRouter();
const { locale, localeOptions, setLocale, t } = useI18n();
const token = ref('');
const selectedLocale = computed<Locale>({
  get: () => locale.value,
  set: (nextLocale) => setLocale(nextLocale)
});

function submit() {
  setToken(token.value.trim());
  router.push(typeof route.query.redirect === 'string' ? route.query.redirect : '/dashboard');
}
</script>

<style scoped>
.login-card-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.login-language {
  width: 120px;
}

.login-button {
  width: 100%;
}
</style>
