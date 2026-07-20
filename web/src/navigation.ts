import {
  DataAnalysis,
  DataLine,
  DocumentChecked,
  Key,
  Link,
  Lock,
  Monitor,
  Box,
  Setting,
  Tools,
} from '@element-plus/icons-vue';
import type { Component } from 'vue';
import type { RouteRecordRaw } from 'vue-router';

import type { TranslationKey } from '@/i18n';

export interface AppNavigationItem {
  key: string;
  path: string;
  name: string;
  icon: Component;
  labelKey: TranslationKey;
  titleKey: TranslationKey;
  descriptionKey: TranslationKey;
  component: NonNullable<RouteRecordRaw['component']>;
}

export const APP_NAV_ITEMS: AppNavigationItem[] = [
  {
    key: 'quickConnect',
    path: '/quick-connect',
    name: 'quick-connect',
    icon: Link,
    labelKey: 'nav.quickConnect',
    titleKey: 'route.quickConnect.title',
    descriptionKey: 'route.quickConnect.description',
    component: () => import('@/views/QuickConnectView.vue'),
  },
  {
    key: 'sqlConsole',
    path: '/sql-console',
    name: 'sql-console',
    icon: DataLine,
    labelKey: 'nav.sqlConsole',
    titleKey: 'route.sqlConsole.title',
    descriptionKey: 'route.sqlConsole.description',
    component: () => import('@/views/SQLConsoleView.vue'),
  },
  {
    key: 'hosts',
    path: '/hosts',
    name: 'hosts',
    icon: Monitor,
    labelKey: 'nav.hosts',
    titleKey: 'route.hosts.title',
    descriptionKey: 'route.hosts.description',
    component: () => import('@/views/HostsView.vue'),
  },
  {
    key: 'databases',
    path: '/databases',
    name: 'databases',
    icon: DataAnalysis,
    labelKey: 'nav.databases',
    titleKey: 'route.databases.title',
    descriptionKey: 'route.databases.description',
    component: () => import('@/views/DatabaseView.vue'),
  },
  {
    key: 'platformAccounts',
    path: '/platform-accounts',
    name: 'platform-accounts',
    icon: Key,
    labelKey: 'nav.platformAccounts',
    titleKey: 'route.platformAccounts.title',
    descriptionKey: 'route.platformAccounts.description',
    component: () => import('@/views/PlatformAccountsView.vue'),
  },
  {
    key: 'applications',
    path: '/applications',
    name: 'applications',
    icon: Monitor,
    labelKey: 'nav.applications',
    titleKey: 'route.applications.title',
    descriptionKey: 'route.applications.description',
    component: () => import('@/views/ApplicationsView.vue'),
  },
  {
    key: 'containers',
    path: '/containers',
    name: 'containers',
    icon: Box,
    labelKey: 'nav.containers',
    titleKey: 'route.containers.title',
    descriptionKey: 'route.containers.description',
    component: () => import('@/views/ContainersView.vue'),
  },
  {
    key: 'audit',
    path: '/audit',
    name: 'audit',
    icon: DocumentChecked,
    labelKey: 'nav.audit',
    titleKey: 'route.audit.title',
    descriptionKey: 'route.audit.description',
    component: () => import('@/views/AuditView.vue'),
  },
  {
    key: 'rbac',
    path: '/rbac',
    name: 'rbac',
    icon: Lock,
    labelKey: 'nav.rbac',
    titleKey: 'route.rbac.title',
    descriptionKey: 'route.rbac.description',
    component: () => import('@/views/UnifiedRBACView.vue'),
  },
  {
    key: 'systemSettings',
    path: '/system-settings',
    name: 'system-settings',
    icon: Tools,
    labelKey: 'nav.systemSettings',
    titleKey: 'route.systemSettings.title',
    descriptionKey: 'route.systemSettings.description',
    component: () => import('@/views/SystemSettingsView.vue'),
  },
  {
    key: 'settings',
    path: '/settings',
    name: 'settings',
    icon: Setting,
    labelKey: 'nav.settings',
    titleKey: 'route.settings.title',
    descriptionKey: 'route.settings.description',
    component: () => import('@/views/SettingsView.vue'),
  },
];
