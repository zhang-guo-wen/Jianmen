<template>
  <el-config-provider :locale="elementLocale">
    <router-view v-if="isLoginRoute" />
    <el-container v-else class="app-shell">
            <span>{{ t("app.subtitle") }}</span>
          <el-menu-item
            v-for="item in navItems"
            :key="item.path"
            :index="item.path"
          >
            <el-button
              class="logout-button"
              type="primary"
              plain
              @click="logout"
            >
              {{ t("common.logout") }}
            <el-button
              link
              type="primary"
              :loading="permission.loading"
              @click="retryPermissions"
              >重试</el-button
            >
  UserFilled,
} from "@element-plus/icons-vue";
import { computed, onMounted, watchEffect, type Component } from "vue";
import { useRoute, useRouter } from "vue-router";
import { clearToken, getToken } from "@/api/client";
import { usePermissionStore } from "@/stores/permission";
import {
  isTranslationKey,
  useI18n,
  type Locale,
  type TranslationKey,
} from "@/i18n";
const isLoginRoute = computed(
  () => route.name === "login" || route.name === "setup",
);
  set: (nextLocale) => setLocale(nextLocale),
const ALL_NAV_ITEMS: Array<{
  path: string;
  icon: Component;
  labelKey: TranslationKey;
  menuKey: string;
}> = [
  { path: "/hosts", icon: Monitor, labelKey: "nav.hosts", menuKey: "hosts" },
  {
    path: "/databases",
    icon: DataAnalysis,
    labelKey: "nav.databases",
    menuKey: "databases",
  },
  {
    path: "/applications",
    icon: Monitor,
    labelKey: "nav.applications",
    menuKey: "applications",
  },
  { path: "/users", icon: UserFilled, labelKey: "nav.users", menuKey: "users" },
  { path: "/roles", icon: Lock, labelKey: "nav.roles", menuKey: "roles" },
  {
    path: "/audit",
    icon: DocumentChecked,
    labelKey: "nav.audit",
    menuKey: "audit",
  },
  {
    path: "/quick-connect",
    icon: Link,
    labelKey: "nav.quickConnect",
    menuKey: "quickConnect",
  },
  ALL_NAV_ITEMS.filter((item) => permission.canAccessMenu(item.menuKey)),
const pageTitle = computed(() =>
  metaText(route.meta.titleKey, "route.quickConnect.title"),
);
  metaText(route.meta.descriptionKey, "route.quickConnect.description"),
  router.push({ name: "login" });
            <el-button link type="primary" :loading="permission.loading" @click="retryPermissions">重试</el-button>
          </div>
          <router-view />
        </el-main>
      </el-container>
    </el-container>
  </el-config-provider>
</template>

<script setup lang="ts">
import {
  DataAnalysis,
  DocumentChecked,
  Link,
  Monitor,
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
  { path: '/hosts', icon: Monitor, labelKey: 'nav.hosts', menuKey: 'hosts' },
  { path: '/databases', icon: DataAnalysis, labelKey: 'nav.databases', menuKey: 'databases' },
  { path: '/applications', icon: Monitor, labelKey: 'nav.applications', menuKey: 'applications' },
  { path: '/users', icon: UserFilled, labelKey: 'nav.users', menuKey: 'users' },
  { path: '/roles', icon: UserFilled, labelKey: 'nav.roles', menuKey: 'roles' },
  { path: '/audit', icon: DocumentChecked, labelKey: 'nav.audit', menuKey: 'audit' },
  { path: '/quick-connect', icon: Link, labelKey: 'nav.quickConnect', menuKey: 'quickConnect' },
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

const pageTitle = computed(() => metaText(route.meta.titleKey, 'route.quickConnect.title'));
const pageDescription = computed(() =>
  metaText(route.meta.descriptionKey, 'route.quickConnect.description')
);

watchEffect(() => {
  document.title = `${pageTitle.value} - Jianmen`;
});

function logout() {
  permission.reset();
  clearToken();
  router.push({ name: 'login' });
}

async function retryPermissions() {
  await permission.fetch({ force: true });
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

.permission-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
  padding: 10px 12px;
  border: 1px solid #fed7aa;
  border-radius: 8px;
  background: #fff7ed;
  color: #9a3412;
  font-size: 13px;
  line-height: 1.4;
}

@media (max-width: 780px) {
  .app-header-actions {
    width: 100%;
    justify-content: flex-end;
  }
}
</style>
