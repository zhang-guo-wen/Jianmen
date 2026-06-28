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
import { computed, onMounted, watchEffect, type Component } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { clearToken, getToken } from '@/api/client';
import { usePermissionStore } from '@/stores/permission';
import { isTranslationKey, useI18n, type Locale, type TranslationKey } from '@/i18n';

const route = useRoute();
const router = useRouter();
const { elementLocale, locale, localeOptions, setLocale, t } = useI18n();

const isLoginRoute = computed(() => route.name === 'login' || route.name === 'setup');
const activePath = computed(() => route.path);
const selectedLocale = computed<Locale>({
  get: () => locale.value,
  set: (nextLocale) => setLocale(nextLocale)
});

const ALL_NAV_ITEMS: Array<{ path: string; icon: Component; labelKey: TranslationKey; menuKey: string }> = [
  { path: '/dashboard', icon: DataBoard, labelKey: 'nav.dashboard', menuKey: 'dashboard' },
  { path: '/hosts', icon: Monitor, labelKey: 'nav.hosts', menuKey: 'hosts' },
  { path: '/databases', icon: DataAnalysis, labelKey: 'nav.databases', menuKey: 'databases' },
  { path: '/quick-connect', icon: Link, labelKey: 'nav.quickConnect', menuKey: 'quickConnect' },
  { path: '/sessions', icon: Connection, labelKey: 'nav.sessions', menuKey: 'sessions' },
  { path: '/users', icon: UserFilled, labelKey: 'nav.users', menuKey: 'rbac' },
  { path: '/roles', icon: UserFilled, labelKey: 'nav.roles', menuKey: 'rbac' },
  { path: '/audit', icon: DocumentChecked, labelKey: 'nav.audit', menuKey: 'audit' },
  { path: '/web-terminal', icon: Platform, labelKey: 'nav.webTerminal', menuKey: 'webTerminal' },
];

const permission = usePermissionStore();
const navItems = computed(() =>
  ALL_NAV_ITEMS.filter(item => permission.canAccessMenu(item.menuKey))
);

onMounted(async () => {
  if (!isLoginRoute.value && getToken()) {
    await permission.fetch();
  }
});

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
