import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router';

import { apiClient, getCSRFToken } from '@/api/client';
import type { TranslationKey } from '@/i18n';
import { APP_NAV_ITEMS } from '@/navigation';
import { usePermissionStore } from '@/stores/permission';

interface AppRouteMeta {
  public?: boolean;
  menuKey?: string;
  titleKey: TranslationKey;
  descriptionKey: TranslationKey;
}

const LoginView = () => import('@/views/LoginView.vue');
const NoAccessView = () => import('@/views/NoAccessView.vue');
const SetupView = () => import('@/views/SetupView.vue');
const WebRDPView = () => import('@/views/WebRDPView.vue');
const WebTerminalView = () => import('@/views/WebTerminalView.vue');

const protectedRoutes: RouteRecordRaw[] = APP_NAV_ITEMS.map(item => ({
  path: item.path,
  name: item.name,
  component: item.component,
  meta: {
    menuKey: item.key,
    titleKey: item.titleKey,
    descriptionKey: item.descriptionKey,
  } satisfies AppRouteMeta,
}));

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/quick-connect' },
  {
    path: '/setup',
    name: 'setup',
    component: SetupView,
    meta: {
      public: true,
      titleKey: 'setup.title',
      descriptionKey: 'setup.description',
    } satisfies AppRouteMeta,
  },
  {
    path: '/login',
    name: 'login',
    component: LoginView,
    meta: {
      public: true,
      titleKey: 'route.login.title',
      descriptionKey: 'route.login.description',
    } satisfies AppRouteMeta,
  },
  ...protectedRoutes,
  {
    path: '/web-terminal',
    name: 'web-terminal',
    component: WebTerminalView,
    meta: {
      titleKey: 'route.webTerminal.title',
      descriptionKey: 'route.webTerminal.description',
    } satisfies AppRouteMeta,
  },
  {
    path: '/web-rdp',
    name: 'web-rdp',
    component: WebRDPView,
    meta: {
      titleKey: 'route.webRDP.title',
      descriptionKey: 'route.webRDP.description',
    } satisfies AppRouteMeta,
  },
  {
    path: '/no-access',
    name: 'no-access',
    component: NoAccessView,
    meta: {
      titleKey: 'route.noAccess.title',
      descriptionKey: 'route.noAccess.description',
    } satisfies AppRouteMeta,
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
});

let initChecked = false;
let needsInit = false;

router.beforeEach(async (to, from) => {
  if (to.name === 'setup') return true;

  if (from.name === 'setup') initChecked = false;

  if (!initChecked) {
    try {
      const status = await apiClient.getInitStatus();
      needsInit = !status.initialized;
    } catch {
      needsInit = false;
    }
    initChecked = true;
  }

  if (needsInit) return { name: 'setup' };
  if (to.meta.public) return true;

	if (!getCSRFToken()) {
    return { name: 'login', query: { redirect: to.fullPath } };
  }

  const store = usePermissionStore();
  if (!store.loaded) {
    const permissionReady = await store.fetch();
    if (!permissionReady) return to.name === 'no-access' ? true : { name: 'no-access' };
  }

  const menuKey = typeof to.meta.menuKey === 'string' ? to.meta.menuKey : '';
  if (!menuKey || store.canAccessMenu(menuKey)) return true;

  const fallbackPath = store.firstAccessiblePath();
  if (fallbackPath && fallbackPath !== to.path) return fallbackPath;
  if (to.name !== 'no-access') return { name: 'no-access' };
  return true;
});

export default router;
