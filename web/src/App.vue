<template>
  <el-config-provider :locale="elementLocale">
    <router-view v-if="isLoginRoute" />
    <el-container v-else class="app-shell">
      <el-aside :class="['app-sidebar', { collapsed }]" :width="collapsed ? '64px' : '190px'">
        <div class="brand">
          <div class="brand-mark">GB</div>
          <div v-show="!collapsed">
            <strong>Jianmen</strong>
            <span>{{ t('app.subtitle') }}</span>
          </div>
        </div>
        <el-menu
          :default-active="activePath"
          :collapse="collapsed"
          background-color="#101828"
          class="nav-menu"
          router
          text-color="#d0d5dd"
          active-text-color="#ffffff"
        >
          <template v-for="item in navItems" :key="item.labelKey">
            <el-sub-menu v-if="'children' in item" :index="item.labelKey">
              <template #title>
                <el-icon><component :is="item.icon" /></el-icon>
                <span>{{ t(item.labelKey) }}</span>
              </template>
              <el-menu-item v-for="child in item.children" :key="child.path" :index="child.path">
                {{ t(child.labelKey) }}
              </el-menu-item>
            </el-sub-menu>
            <el-menu-item v-else :index="item.path!">
              <el-icon><component :is="item.icon" /></el-icon>
              <span>{{ t(item.labelKey) }}</span>
            </el-menu-item>
          </template>
        </el-menu>

        <div class="sidebar-bottom">
          <el-button
            class="collapse-btn"
            :icon="collapsed ? Expand : Fold"
            text
            @click="collapsed = !collapsed"
          />
          <div v-show="!collapsed" class="sidebar-actions">
            <el-select
              v-model="selectedLocale"
              size="small"
              :aria-label="t('app.language')"
              style="width: 100%"
            >
              <el-option
                v-for="option in localeOptions"
                :key="option.value"
                :label="option.label"
                :value="option.value"
              />
            </el-select>
            <el-button size="small" @click="logout">{{ t('common.logout') }}</el-button>
          </div>
        </div>
      </el-aside>

      <el-container>
        <el-header class="app-header">
          <div>
            <h1>{{ pageTitle }}</h1>
            <p>{{ pageDescription }}</p>
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
import { ref } from 'vue';
import {
  Connection,
  DataBoard,
  DocumentChecked,
  Expand,
  Fold,
  Goods,
  Link,
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

const collapsed = ref(false);
const isLoginRoute = computed(() => route.name === 'login');
const activePath = computed(() => route.path);
const selectedLocale = computed<Locale>({
  get: () => locale.value,
  set: (nextLocale) => setLocale(nextLocale)
});

type NavItem =
  | { path: string; icon: Component; labelKey: TranslationKey }
  | { icon: Component; labelKey: TranslationKey; children: { path: string; labelKey: TranslationKey }[] };

const navItems: NavItem[] = [
  { path: '/dashboard', icon: DataBoard, labelKey: 'nav.dashboard' },
  {
    icon: Goods,
    labelKey: 'nav.assets' as TranslationKey,
    children: [
      { path: '/hosts', labelKey: 'nav.hosts' },
      { path: '/databases', labelKey: 'nav.databases' },
    ]
  },
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
.app-sidebar {
  display: flex;
  flex-direction: column;
  height: 100vh;
  transition: width 0.2s;
  overflow: hidden;
}

.nav-menu {
  flex: 1;
  border-right: none;
}

.nav-menu:not(.el-menu--collapse) {
  width: 190px;
}

.sidebar-bottom {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border-top: 1px solid #1d2939;
}

.collapse-btn {
  align-self: flex-end;
  color: #98a2b3;
}

.sidebar-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

@media (max-width: 780px) {
  .app-sidebar {
    width: 64px !important;
  }
  .app-sidebar:not(.collapsed) {
    width: 190px !important;
  }
}
</style>
