import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router';

import { apiClient, getToken } from '@/api/client';
import type { TranslationKey } from '@/i18n';
import { usePermissionStore } from '@/stores/permission';

type AppRouteMeta = {
  public?: boolean;
  titleKey: TranslationKey;
  descriptionKey: TranslationKey;
};

const routeMenuMap: Record<string, string> = {

  '/applications': 'applications',
  '/hosts': 'hosts',
  '/databases': 'databases',
  '/platform-accounts': 'platformAccounts',
  '/quick-connect': 'quickConnect',
  '/audit': 'audit',
  '/rbac': 'rbac',
};

const ApplicationsView = () => import('@/views/ApplicationsView.vue');
const AuditView = () => import('@/views/AuditView.vue');
const DatabaseView = () => import('@/views/DatabaseView.vue');
const HostsView = () => import('@/views/HostsView.vue');
const LoginView = () => import('@/views/LoginView.vue');
const PlatformAccountsView = () => import('@/views/PlatformAccountsView.vue');
const QuickConnectView = () => import('@/views/QuickConnectView.vue');
const UnifiedRBACView = () => import('@/views/UnifiedRBACView.vue');
const SetupView = () => import('@/views/SetupView.vue');
const WebTerminalView = () => import('@/views/WebTerminalView.vue');

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/quick-connect'
  },
  {
    path: '/setup',
    name: 'setup',
    component: SetupView,
    meta: {
      public: true,
      titleKey: 'setup.title',
      descriptionKey: 'setup.description'
    } satisfies AppRouteMeta
  },
  {
    path: '/login',
    name: 'login',
    component: LoginView,
    meta: {
      public: true,
      titleKey: 'route.login.title',
      descriptionKey: 'route.login.description'
    } satisfies AppRouteMeta
  },

  {
    path: '/hosts',
    name: 'hosts',
    component: HostsView,
    meta: {
      titleKey: 'route.hosts.title',
      descriptionKey: 'route.hosts.description'
    } satisfies AppRouteMeta
  },
  {
    path: '/databases',
    name: 'databases',
    component: DatabaseView,
    meta: {
      titleKey: 'route.databases.title',
      descriptionKey: 'route.databases.description'
    } satisfies AppRouteMeta
  },
  {
    path: '/platform-accounts',
    name: 'platform-accounts',
    component: PlatformAccountsView,
    meta: {
      titleKey: 'route.platformAccounts.title',
      descriptionKey: 'route.platformAccounts.description'
    } satisfies AppRouteMeta
  },
  {
    path: '/applications',
    name: 'applications',
    component: ApplicationsView,
    meta: {
      titleKey: 'route.applications.title',
      descriptionKey: 'route.applications.description'
    } satisfies AppRouteMeta
  },
  {
    path: '/quick-connect',
    name: 'quick-connect',
    component: QuickConnectView,
    meta: {
      titleKey: 'route.quickConnect.title',
      descriptionKey: 'route.quickConnect.description'
    } satisfies AppRouteMeta
  },

  {
    path: '/rbac',
    name: 'rbac',
    component: UnifiedRBACView,
    meta: {
      titleKey: 'route.rbac.title',
      descriptionKey: 'route.rbac.description'
    } satisfies AppRouteMeta
  },
  {
    path: '/audit',
    name: 'audit',
    component: AuditView,
    meta: {
      titleKey: 'route.audit.title',
      descriptionKey: 'route.audit.description'
    } satisfies AppRouteMeta
  },

  {
    path: '/web-terminal',
    name: 'web-terminal',
    component: WebTerminalView,
    meta: {
      titleKey: 'route.webTerminal.title',
      descriptionKey: 'route.webTerminal.description',
    } satisfies AppRouteMeta
  },

  {
    path: '/:pathMatch(.*)*',
    redirect: '/quick-connect'
  }
];

const router = createRouter({
  history: createWebHistory(),
  routes
});

let initChecked = false;
let needsInit = false;

router.beforeEach(async (to, from) => {
  // setup 页面始终可访问
  if (to.name === 'setup') return true;

  // 离开 setup 时重新检查初始化状态（用户可能刚完成初始化）
  if (from.name === 'setup') {
    initChecked = false;
  }

  // 首次检查初始化状态
  if (!initChecked) {
    try {
      const status = await apiClient.getInitStatus();
      needsInit = !status.initialized;
    } catch {
      // 如果检查失败（网络问题），允许继续（用户可能已登录）
      needsInit = false;
    }
    initChecked = true;
  }

  // 未初始化则跳转 setup
  if (needsInit) {
    return { name: 'setup' };
  }

  // 公开路由或已登录
  if (to.meta.public || getToken()) {
    // Check permission for non-public routes
    if (!to.meta.public) {
      const store = usePermissionStore();
      if (!store.loaded) {
        const permissionReady = await store.fetch();
        if (!permissionReady) {
          return to.name === 'quick-connect' ? true : { name: 'quick-connect' };
        }
      }
      const menuKey = routeMenuMap[to.path];
      if (menuKey && !store.canAccessMenu(menuKey)) {
        // 避免无限重定向：已在 fallback 页面则不再跳转
        if (to.name !== 'quick-connect') {
          return { name: 'quick-connect' };
        }
      }
    }
    return true;
  }

  return {
    name: 'login',
    query: { redirect: to.fullPath },
  };
});

export default router;
