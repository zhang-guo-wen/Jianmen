import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router';

import { apiClient, getToken } from '@/api/client';
import type { TranslationKey } from '@/i18n';
import { usePermissionStore } from '@/stores/permission';
import AuditView from '@/views/AuditView.vue';

import DatabaseView from '@/views/DatabaseView.vue';
import HostsView from '@/views/HostsView.vue';
import LoginView from '@/views/LoginView.vue';
import QuickConnectView from '@/views/QuickConnectView.vue';
import SetupView from '@/views/SetupView.vue';
import RBACView from '@/views/RBACView.vue';
import RolesView from '@/views/RolesView.vue';

import UsersView from '@/views/UsersView.vue';

type AppRouteMeta = {
  public?: boolean;
  titleKey: TranslationKey;
  descriptionKey: TranslationKey;
};

const routeMenuMap: Record<string, string> = {

  '/hosts': 'hosts',
  '/databases': 'databases',
  '/quick-connect': 'quickConnect',
  '/audit': 'audit',
  '/users': 'users',
  '/roles': 'roles',
};

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
    component: RBACView,
    meta: {
      titleKey: 'route.rbac.title',
      descriptionKey: 'route.rbac.description'
    } satisfies AppRouteMeta
  },
  {
    path: '/users',
    name: 'users',
    component: UsersView,
    meta: {
      titleKey: 'route.users.title',
      descriptionKey: 'route.users.description',
    } satisfies AppRouteMeta,
  },
  {
    path: '/roles',
    name: 'roles',
    component: RolesView,
    meta: {
      titleKey: 'route.roles.title',
      descriptionKey: 'route.roles.description',
    } satisfies AppRouteMeta,
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
        await store.fetch();
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
