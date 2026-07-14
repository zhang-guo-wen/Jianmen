<template>
  <el-config-provider :locale="elementLocale">
    <router-view v-if="isLoginRoute" />
    <el-container v-else class="app-shell">
      <el-aside class="app-sidebar" width="174px">
        <div class="brand">
          <div class="brand-mark">JM</div>
          <div class="brand-copy">
            <strong>Jianmen</strong>
            <span>{{ t("app.subtitle") }}</span>
          </div>
        </div>
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
            <el-icon><component :is="item.icon" /></el-icon>
            <span>{{ t(item.labelKey) }}</span>
          </el-menu-item>
        </el-menu>
        <div class="sidebar-footer">
          <el-select
            v-model="selectedLocale"
            class="sidebar-locale"
            :aria-label="t('app.language')"
          >
            <el-option
              v-for="option in localeOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
          <el-button
            class="sidebar-logout"
            type="primary"
            plain
            @click="logout"
          >
            <el-icon><SwitchButton /></el-icon>
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
        <el-main class="app-main">
          <div v-if="permission.error" class="permission-banner" role="status">
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

import { clearToken, getToken } from "@/api/client";
import { APP_NAV_ITEMS } from "@/navigation";
import { usePermissionStore } from "@/stores/permission";
import { usePreferencesStore } from "@/stores/preferences";
import {
  isTranslationKey,
  useI18n,
  type Locale,
  type TranslationKey,
} from "@/i18n";

const route = useRoute();
const router = useRouter();
const { elementLocale, locale, localeOptions, setLocale, t } = useI18n();

const isLoginRoute = computed(
  () => route.name === "login" || route.name === "setup",
);
const activePath = computed(() => route.path);
const selectedLocale = computed<Locale>({
  get: () => locale.value,
  set: (nextLocale) => setLocale(nextLocale),
});

const permission = usePermissionStore();
const preferences = usePreferencesStore();
const navItems = computed(() =>
  APP_NAV_ITEMS.filter((item) => permission.canAccessMenu(item.key)),
);

onMounted(async () => {
  if (!isLoginRoute.value && getToken()) {
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

function logout() {
  permission.reset();
  preferences.reset();
  clearToken();
  router.push({ name: "login" });
}

async function retryPermissions() {
  await permission.fetch({ force: true });
}
</script>
