import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router';

import { getToken } from '@/api/client';
import type { TranslationKey } from '@/i18n';
import AuditView from '@/views/AuditView.vue';
import DashboardView from '@/views/DashboardView.vue';
import DatabaseView from '@/views/DatabaseView.vue';
import HostsView from '@/views/HostsView.vue';
import LoginView from '@/views/LoginView.vue';
import QuickConnectView from '@/views/QuickConnectView.vue';
import SetupView from '@/views/SetupView.vue';
import RBACView from '@/views/RBACView.vue';
import SessionsView from '@/views/SessionsView.vue';
import WebTerminalView from '@/views/WebTerminalView.vue';

type AppRouteMeta = {
  public?: boolean;
  titleKey: TranslationKey;
  descriptionKey: TranslationKey;
};

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/dashboard'
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
    path: '/dashboard',
    name: 'dashboard',
    component: DashboardView,
    meta: {
      titleKey: 'route.dashboard.title',
      descriptionKey: 'route.dashboard.description'
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
    path: '/sessions',
    name: 'sessions',
    component: SessionsView,
    meta: {
      titleKey: 'route.sessions.title',
      descriptionKey: 'route.sessions.description'
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
      descriptionKey: 'route.webTerminal.description'
    } satisfies AppRouteMeta
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/dashboard'
  }
];

const router = createRouter({
  history: createWebHistory(),
  routes
});

router.beforeEach((to) => {
  if (to.meta.public || getToken()) {
    return true;
  }

  return {
    name: 'login',
    query: {
      redirect: to.fullPath
    }
  };
});

export default router;
