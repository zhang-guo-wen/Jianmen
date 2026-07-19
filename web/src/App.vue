<template>
  <el-config-provider :locale="elementLocale">
    <router-view v-if="isStandaloneRoute" />
    <el-container v-else class="app-shell">
      <a class="skip-link" href="#main-content">跳转到主内容</a>
      <el-aside class="app-sidebar" width="var(--sidebar-width)">
        <div class="brand">
          <div class="brand-mark" aria-hidden="true">JM</div>
          <div class="brand-copy">
            <strong>Jianmen</strong>
            <span>{{ t("app.subtitle") }}</span>
          </div>
        </div>
        <nav class="app-nav" aria-label="主导航">
          <el-menu
            :default-active="activePath"
            class="nav-menu"
            router
            text-color="#cbd5e1"
            active-text-color="#ffffff"
          >
            <el-menu-item
              v-for="item in navItems"
              :key="item.path"
              :index="item.path"
            >
              <el-icon aria-hidden="true"><component :is="item.icon" /></el-icon>
              <span>{{ t(item.labelKey) }}</span>
            </el-menu-item>
          </el-menu>
        </nav>
        <div class="sidebar-footer">
          <el-button
            class="sidebar-logout"
            type="primary"
            plain
            @click="logout"
          >
            <el-icon aria-hidden="true"><SwitchButton /></el-icon>
            {{ t("common.logout") }}
          </el-button>
        </div>
      </el-aside>

      <el-container class="app-content">
        <el-header class="app-header">
          <div class="page-heading">
            <h1>{{ pageTitle }}</h1>
            <p>{{ pageDescription }}</p>
          </div>
        </el-header>
        <el-main id="main-content" class="app-main" tabindex="-1">
          <div v-if="permission.error" class="permission-banner" role="status" aria-live="polite">
            <span>{{ permission.error }}</span>
            <el-button
              link
              type="primary"
              :loading="permission.loading"
              @click="retryPermissions"
              >重试</el-button
            >
          </div>
          <router-view />
        </el-main>
      </el-container>
    </el-container>
  </el-config-provider>
</template>

<script setup lang="ts">
import { SwitchButton } from "@element-plus/icons-vue";
import { computed, onMounted, watchEffect } from "vue";
import { useRoute, useRouter } from "vue-router";

import { apiClient, clearCSRFToken, getCSRFToken } from "@/api/client";
import { APP_NAV_ITEMS } from "@/navigation";
import { usePermissionStore } from "@/stores/permission";
import { usePreferencesStore } from "@/stores/preferences";
import {
  isTranslationKey,
  useI18n,
  type TranslationKey,
} from "@/i18n";

const route = useRoute();
const router = useRouter();
const { elementLocale, t } = useI18n();

const isStandaloneRoute = computed(
  () => route.name === "login" || route.name === "setup" || route.meta.immersive === true,
);
const activePath = computed(() => route.path);
const permission = usePermissionStore();
const preferences = usePreferencesStore();
const navItems = computed(() =>
  APP_NAV_ITEMS.filter((item) => permission.canAccessMenu(item.key)),
);

onMounted(async () => {
	if (!isStandaloneRoute.value && getCSRFToken()) {
    await Promise.all([permission.fetch(), preferences.fetch().catch(() => undefined)]);
  }
});

function metaText(value: unknown, fallbackKey: TranslationKey): string {
  return t(isTranslationKey(value) ? value : fallbackKey);
}

const pageTitle = computed(() =>
  metaText(route.meta.titleKey, "route.quickConnect.title"),
);
const pageDescription = computed(() =>
  metaText(route.meta.descriptionKey, "route.quickConnect.description"),
);

watchEffect(() => {
  document.title = `${pageTitle.value} - Jianmen`;
});

async function logout() {
  permission.reset();
  preferences.reset();
	try { await apiClient.logout(); } catch { /* clear local anti-CSRF state even when session has expired */ }
	clearCSRFToken();
  router.push({ name: "login" });
}

async function retryPermissions() {
  await permission.fetch({ force: true });
}
</script>
