<template>
  <el-config-provider :locale="elementLocale">
    <router-view v-if="isLoginRoute" />
    <el-container v-else class="app-shell">
      <el-aside class="app-sidebar" width="236px">
        <div class="brand">
          <div class="brand-mark">GB</div>
          <div>
            <strong>Jianmen</strong>
            <span>{{ t('app.subtitle') }}</span>
          </div>
        </div>
        <el-menu
          :default-active="activePath"
          background-color="#101828"
          class="nav-menu"
          router
          text-color="#d0d5dd"
          active-text-color="#ffffff"
        >
          <el-menu-item v-for="item in navItems" :key="item.path" :index="item.path">
            <el-icon><component :is="item.icon" /></el-icon>
            <span>{{ t(item.labelKey) }}</span>
          </el-menu-item>
        </el-menu>
      </el-aside>

      <el-container>
        <el-header class="app-header">
          <div>
            <h1>{{ pageTitle }}</h1>
            <p>{{ pageDescription }}</p>
          </div>
          <div class="app-header-actions">
            <el-select
              v-model="selectedLocale"
              class="language-select"
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
            <el-button type="primary" plain @click="logout">{{ t('common.logout') }}</el-button>
          </div>
        </el-header>
        <el-main class="app-main">
          <router-view />
        </el-main>
      </el-container>
    </el-container>
  </el-config-provider>
</template>

<script setup lang="ts">
import {
  Connection,
  DataAnalysis,
  DataBoard,
  DocumentChecked,
  Link,
  Monitor,
  Platform,
  UserFilled
} from '@element-plus/icons-vue';
import { computed, watchEffect, type Component } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { clearToken } from '@/api/client';
import { isTranslationKey, useI18n, type Locale, type TranslationKey } from '@/i18n';

const route = useRoute();
const router = useRouter();
const { elementLocale, locale, localeOptions, setLocale, t } = useI18n();

const isLoginRoute = computed(() => route.name === 'login');
const activePath = computed(() => route.path);
const selectedLocale = computed<Locale>({
  get: () => locale.value,
  set: (nextLocale) => setLocale(nextLocale)
});

const navItems: Array<{ path: string; icon: Component; labelKey: TranslationKey }> = [
  { path: '/dashboard', icon: DataBoard, labelKey: 'nav.dashboard' },
  { path: '/hosts', icon: Monitor, labelKey: 'nav.hosts' },
  { path: '/databases', icon: DataAnalysis, labelKey: 'nav.databases' },
  { path: '/quick-connect', icon: Link, labelKey: 'nav.quickConnect' },
  { path: '/sessions', icon: Connection, labelKey: 'nav.sessions' },
  { path: '/rbac', icon: UserFilled, labelKey: 'nav.rbac' },
  { path: '/audit', icon: DocumentChecked, labelKey: 'nav.audit' },
  { path: '/web-terminal', icon: Platform, labelKey: 'nav.webTerminal' }
];

function metaText(value: unknown, fallbackKey: TranslationKey): string {
  return t(isTranslationKey(value) ? value : fallbackKey);
}

const pageTitle = computed(() => metaText(route.meta.titleKey, 'route.dashboard.title'));
const pageDescription = computed(() =>
  metaText(route.meta.descriptionKey, 'route.dashboard.description')
);

watchEffect(() => {
  document.title = `${pageTitle.value} - Jianmen`;
});

function logout() {
  clearToken();
  router.push({ name: 'login' });
}
</script>

<style scoped>
.app-header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.language-select {
  width: 128px;
}

@media (max-width: 780px) {
  .app-header-actions {
    width: 100%;
    justify-content: flex-end;
  }
}
</style>
