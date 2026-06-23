import elementEn from 'element-plus/es/locale/lang/en';
import elementZhCn from 'element-plus/es/locale/lang/zh-cn';
import { computed, inject, ref, type App, type InjectionKey } from 'vue';

export const supportedLocales = ['zh-CN', 'en-US'] as const;
export type Locale = (typeof supportedLocales)[number];

const storageKey = 'jianmen.locale';
const defaultLocale: Locale = 'zh-CN';

const zhCN = {
  'hosts.action.delete': '删除',
  'hosts.action.edit': '编辑',
  'hosts.action.new': '新增主机',
  'hosts.action.save': '保存',
  'hosts.address': '地址',
  'hosts.auth.method': '认证方式',
  'hosts.auth.none': '未配置',
  'hosts.auth.password': '密码',
  'hosts.auth.privateKeyPath': '私钥路径',
  'hosts.auth.privateKeyPem': '私钥 PEM',
  'hosts.cancel': '取消',
  'hosts.column.actions': '操作',
  'hosts.column.auth': '认证方式',
  'hosts.column.hostCheck': '主机校验',
  'hosts.column.id': 'ID',
  'hosts.column.name': '名称',
  'hosts.column.username': '账号',
  'hosts.createTitle': '新增主机',
  'hosts.deleteConfirm': '确认删除主机“{name}”？',
  'hosts.deleteTitle': '删除主机',
  'hosts.dialog.editTitle': '编辑主机',
  'hosts.empty': '暂无主机数据',
  'hosts.error.delete': '删除主机失败',
  'hosts.error.loadDetail': '加载主机详情失败',
  'hosts.error.loadList': '加载主机列表失败',
  'hosts.error.missingId': '缺少主机 ID',
  'hosts.error.save': '保存主机失败',
  'hosts.field.host': '主机地址',
  'hosts.field.hostKeyFingerprint': '主机指纹',
  'hosts.field.id': 'ID',
  'hosts.field.knownHostsPath': 'known_hosts 文件',
  'hosts.field.name': '名称',
  'hosts.field.passphrase': '私钥口令',
  'hosts.field.password': '密码',
  'hosts.field.port': '端口',
  'hosts.field.privateKeyPath': '私钥路径',
  'hosts.field.privateKeyPem': '私钥 PEM',
  'hosts.field.username': '登录账号',
  'hosts.hostKey.fingerprint': '指纹校验',
  'hosts.hostKey.ignore': '忽略校验',
  'hosts.hostKey.knownHosts': 'known_hosts 文件',
  'hosts.hostKey.method': '主机校验方式',
  'hosts.message.created': '主机已创建',
  'hosts.message.deleted': '主机已删除',
  'hosts.message.staticDeleteBlocked': '静态配置主机不能删除',
  'hosts.message.updated': '主机已更新',
  'hosts.placeholder.host': '10.0.0.10',
  'hosts.placeholder.id': 'target-id',
  'hosts.placeholder.keepSecret': '留空表示不修改',
  'hosts.placeholder.knownHostsPath': 'data/known_hosts',
  'hosts.placeholder.name': '生产主机',
  'hosts.placeholder.optional': '可选',
  'hosts.placeholder.required': '必填',
  'hosts.placeholder.search': '搜索主机',
  'hosts.placeholder.username': 'root',
  'hosts.required.authMethod': '请选择认证方式',
  'hosts.required.host': '请输入主机地址',
  'hosts.required.hostKeyFingerprint': '请输入主机指纹',
  'hosts.required.hostKeyMode': '请选择主机校验方式',
  'hosts.required.id': '请输入 ID',
  'hosts.required.idPattern': 'ID 不能包含斜杠、反斜杠或点号',
  'hosts.required.knownHostsPath': '请输入 known_hosts 文件路径',
  'hosts.required.name': '请输入名称',
  'hosts.required.password': '请输入密码',
  'hosts.required.port': '端口必须在 1-65535 之间',
  'hosts.required.privateKeyPath': '请输入私钥路径',
  'hosts.required.privateKeyPem': '请输入私钥 PEM',
  'hosts.required.username': '请输入登录账号',
  'hosts.warning.credentialRequired': '{method} 为必填项',
  'hosts.warning.passphraseNeedsKey': '要更新私钥口令，需要同时重新填写私钥',
  'app.language': '语言',
  'app.subtitle': '管理控制台',
  'common.export': '导出',
  'common.logout': '退出登录',
  'common.pending': '待处理',
  'common.refresh': '刷新',
  'nav.audit': '审计',
  'nav.dashboard': '仪表盘',
  'nav.hosts': '主机',
  'nav.rbac': '权限管理',
  'nav.sessions': '会话',
  'nav.webTerminal': 'Web 终端',
  'route.audit.description': '安全事件与合规审计轨迹',
  'route.audit.title': '审计',
  'route.dashboard.description': '服务健康状态与运行概览',
  'route.dashboard.title': '仪表盘',
  'route.hosts.description': '已纳管目标资产清单',
  'route.hosts.title': '主机',
  'route.login.description': '登录管理控制台',
  'route.login.title': '登录',
  'route.rbac.description': '用户、角色与访问策略',
  'route.rbac.title': '权限管理',
  'route.sessions.description': '活跃与历史访问会话',
  'route.sessions.title': '会话',
  'route.webTerminal.description': '基于浏览器的目标访问工作区',
  'route.webTerminal.title': 'Web 终端',
  'audit.actor.operator': 'operator',
  'audit.actor.system': 'system',
  'audit.column.actor': '操作者',
  'audit.column.event': '事件',
  'audit.column.result': '结果',
  'audit.column.time': '时间',
  'audit.event.auditPlaceholder': '审计日志占位数据',
  'audit.event.policyChangePlaceholder': '策略变更占位数据',
  'audit.filter.all': '全部',
  'audit.filter.login': '登录',
  'audit.filter.policy': '策略',
  'audit.filter.session': '会话',
  'audit.pendingApi': '等待 API',
  'audit.result.allowed': '已允许',
  'audit.result.review': '待复核',
  'dashboard.loadError': '无法加载健康状态',
  'dashboard.metric.activeSessions': '活跃会话',
  'dashboard.metric.apiHealth': 'API 健康状态',
  'dashboard.metric.targets': '目标资产',
  'dashboard.operationalOverview': '运行概览',
  'dashboard.unknown': '未知',
  'login.signIn': '登录',
  'login.subtitle': '管理控制台登录',
  'login.tokenLabel': '令牌',
  'login.tokenPlaceholder': '粘贴 Bearer 令牌',
  'rbac.column.name': '姓名',
  'rbac.column.role': '角色',
  'rbac.column.status': '状态',
  'rbac.column.username': '用户名',
  'rbac.empty': '暂无用户数据',
  'rbac.loadError': '无法加载用户数据',
  'rbac.mode.policies': '策略',
  'rbac.mode.roles': '角色',
  'rbac.mode.users': '用户',
  'sessions.column.id': '会话 ID',
  'sessions.column.started': '开始时间',
  'sessions.column.status': '状态',
  'sessions.column.target': '目标',
  'sessions.column.user': '用户',
  'sessions.empty': '暂无会话数据',
  'sessions.filter.active': '活跃',
  'sessions.filter.all': '全部会话',
  'sessions.filter.closed': '已关闭',
  'sessions.loadError': '无法加载会话数据',
  'webTerminal.connect': '连接',
  'webTerminal.pendingTarget': '待绑定目标',
  'webTerminal.targetPlaceholder': '选择目标',
  'webTerminal.terminalText':
    'jianmen$ ssh target\n连接流程占位。\n在此接入 xterm.js 或 WebSocket 终端流。'
} satisfies Record<string, string>;

export type TranslationKey = keyof typeof zhCN;

const enUS = {
  'hosts.action.delete': 'Delete',
  'hosts.action.edit': 'Edit',
  'hosts.action.new': 'New host',
  'hosts.action.save': 'Save',
  'hosts.address': 'Address',
  'hosts.auth.method': 'Authentication method',
  'hosts.auth.none': 'Not set',
  'hosts.auth.password': 'Password',
  'hosts.auth.privateKeyPath': 'Private key path',
  'hosts.auth.privateKeyPem': 'Private key PEM',
  'hosts.cancel': 'Cancel',
  'hosts.column.actions': 'Actions',
  'hosts.column.auth': 'Auth method',
  'hosts.column.hostCheck': 'Host check',
  'hosts.column.id': 'ID',
  'hosts.column.name': 'Name',
  'hosts.column.username': 'Username',
  'hosts.createTitle': 'New host',
  'hosts.deleteConfirm': 'Delete host "{name}"?',
  'hosts.deleteTitle': 'Delete host',
  'hosts.dialog.editTitle': 'Edit host',
  'hosts.empty': 'No host data loaded',
  'hosts.error.delete': 'Unable to delete host',
  'hosts.error.loadDetail': 'Unable to load target detail',
  'hosts.error.loadList': 'Unable to load targets',
  'hosts.error.missingId': 'Target ID is missing',
  'hosts.error.save': 'Unable to save host',
  'hosts.field.host': 'Host',
  'hosts.field.hostKeyFingerprint': 'Host key fingerprint',
  'hosts.field.id': 'ID',
  'hosts.field.knownHostsPath': 'Known hosts path',
  'hosts.field.name': 'Name',
  'hosts.field.passphrase': 'Passphrase',
  'hosts.field.password': 'Password',
  'hosts.field.port': 'Port',
  'hosts.field.privateKeyPath': 'Private key path',
  'hosts.field.privateKeyPem': 'Private key PEM',
  'hosts.field.username': 'Username',
  'hosts.hostKey.fingerprint': 'Fingerprint',
  'hosts.hostKey.ignore': 'Ignore verification',
  'hosts.hostKey.knownHosts': 'known_hosts file',
  'hosts.hostKey.method': 'Host key verification',
  'hosts.message.created': 'Host created',
  'hosts.message.deleted': 'Host deleted',
  'hosts.message.staticDeleteBlocked': 'Static targets cannot be deleted',
  'hosts.message.updated': 'Host updated',
  'hosts.placeholder.host': '10.0.0.10',
  'hosts.placeholder.id': 'target-id',
  'hosts.placeholder.keepSecret': 'Leave blank to keep unchanged',
  'hosts.placeholder.knownHostsPath': 'data/known_hosts',
  'hosts.placeholder.name': 'Production host',
  'hosts.placeholder.optional': 'Optional',
  'hosts.placeholder.required': 'Required',
  'hosts.placeholder.search': 'Search hosts',
  'hosts.placeholder.username': 'root',
  'hosts.required.authMethod': 'Authentication method is required',
  'hosts.required.host': 'Host is required',
  'hosts.required.hostKeyFingerprint': 'Host key fingerprint is required',
  'hosts.required.hostKeyMode': 'Host key verification is required',
  'hosts.required.id': 'ID is required',
  'hosts.required.idPattern': 'ID cannot contain slash, backslash, or dot',
  'hosts.required.knownHostsPath': 'Known hosts path is required',
  'hosts.required.name': 'Name is required',
  'hosts.required.password': 'Password is required',
  'hosts.required.port': 'Port must be 1-65535',
  'hosts.required.privateKeyPath': 'Private key path is required',
  'hosts.required.privateKeyPem': 'Private key PEM is required',
  'hosts.required.username': 'Username is required',
  'hosts.warning.credentialRequired': '{method} is required',
  'hosts.warning.passphraseNeedsKey': 'Enter the private key again to update its passphrase',
  'app.language': 'Language',
  'app.subtitle': 'Admin Console',
  'common.export': 'Export',
  'common.logout': 'Logout',
  'common.pending': 'Pending',
  'common.refresh': 'Refresh',
  'nav.audit': 'Audit',
  'nav.dashboard': 'Dashboard',
  'nav.hosts': 'Hosts',
  'nav.rbac': 'RBAC',
  'nav.sessions': 'Sessions',
  'nav.webTerminal': 'Web Terminal',
  'route.audit.description': 'Security events and compliance trail',
  'route.audit.title': 'Audit',
  'route.dashboard.description': 'Service health and operational overview',
  'route.dashboard.title': 'Dashboard',
  'route.hosts.description': 'Managed target inventory',
  'route.hosts.title': 'Hosts',
  'route.login.description': 'Sign in to the admin console',
  'route.login.title': 'Login',
  'route.rbac.description': 'Users, roles, and access policy',
  'route.rbac.title': 'RBAC',
  'route.sessions.description': 'Active and historical access sessions',
  'route.sessions.title': 'Sessions',
  'route.webTerminal.description': 'Browser-based target access workspace',
  'route.webTerminal.title': 'Web Terminal',
  'audit.actor.operator': 'operator',
  'audit.actor.system': 'system',
  'audit.column.actor': 'Actor',
  'audit.column.event': 'Event',
  'audit.column.result': 'Result',
  'audit.column.time': 'Time',
  'audit.event.auditPlaceholder': 'Audit log placeholder',
  'audit.event.policyChangePlaceholder': 'Policy change placeholder',
  'audit.filter.all': 'All',
  'audit.filter.login': 'Login',
  'audit.filter.policy': 'Policy',
  'audit.filter.session': 'Session',
  'audit.pendingApi': 'Pending API',
  'audit.result.allowed': 'Allowed',
  'audit.result.review': 'Review',
  'dashboard.loadError': 'Unable to load health status',
  'dashboard.metric.activeSessions': 'Active Sessions',
  'dashboard.metric.apiHealth': 'API Health',
  'dashboard.metric.targets': 'Targets',
  'dashboard.operationalOverview': 'Operational Overview',
  'dashboard.unknown': 'Unknown',
  'login.signIn': 'Sign in',
  'login.subtitle': 'Admin console sign in',
  'login.tokenLabel': 'Token',
  'login.tokenPlaceholder': 'Paste bearer token',
  'rbac.column.name': 'Name',
  'rbac.column.role': 'Role',
  'rbac.column.status': 'Status',
  'rbac.column.username': 'Username',
  'rbac.empty': 'No user data loaded',
  'rbac.loadError': 'Unable to load users',
  'rbac.mode.policies': 'Policies',
  'rbac.mode.roles': 'Roles',
  'rbac.mode.users': 'Users',
  'sessions.column.id': 'Session ID',
  'sessions.column.started': 'Started',
  'sessions.column.status': 'Status',
  'sessions.column.target': 'Target',
  'sessions.column.user': 'User',
  'sessions.empty': 'No session data loaded',
  'sessions.filter.active': 'Active',
  'sessions.filter.all': 'All sessions',
  'sessions.filter.closed': 'Closed',
  'sessions.loadError': 'Unable to load sessions',
  'webTerminal.connect': 'Connect',
  'webTerminal.pendingTarget': 'Pending target binding',
  'webTerminal.targetPlaceholder': 'Select target',
  'webTerminal.terminalText':
    'jianmen$ ssh target\nConnection workflow placeholder.\nAttach xterm.js or a websocket terminal stream here.'
} satisfies Record<TranslationKey, string>;

const dictionaries: Record<Locale, Record<TranslationKey, string>> = {
  'en-US': enUS,
  'zh-CN': zhCN
};

const elementLocales = {
  'en-US': elementEn,
  'zh-CN': elementZhCn
};

export const localeOptions = [
  { label: '中文', value: 'zh-CN' },
  { label: 'English', value: 'en-US' }
] satisfies Array<{ label: string; value: Locale }>;

function hasOwnKey<T extends object>(target: T, value: PropertyKey): value is keyof T {
  return Object.prototype.hasOwnProperty.call(target, value);
}

export function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && supportedLocales.includes(value as Locale);
}

export function isTranslationKey(value: unknown): value is TranslationKey {
  return typeof value === 'string' && hasOwnKey(zhCN, value);
}

function readStoredLocale(): Locale {
  if (typeof window === 'undefined') {
    return defaultLocale;
  }

  try {
    const storedLocale = window.localStorage.getItem(storageKey);
    return isLocale(storedLocale) ? storedLocale : defaultLocale;
  } catch {
    return defaultLocale;
  }
}

function writeStoredLocale(nextLocale: Locale) {
  if (typeof window === 'undefined') {
    return;
  }

  try {
    window.localStorage.setItem(storageKey, nextLocale);
  } catch {
    // Ignore storage failures so language switching still works for the session.
  }
}

function syncDocumentLocale(nextLocale: Locale) {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = nextLocale;
  }
}

const locale = ref<Locale>(readStoredLocale());
const elementLocale = computed(() => elementLocales[locale.value]);

export function t(key: TranslationKey): string {
  return dictionaries[locale.value][key] ?? dictionaries[defaultLocale][key] ?? key;
}

export function setLocale(nextLocale: Locale) {
  locale.value = nextLocale;
  writeStoredLocale(nextLocale);
  syncDocumentLocale(nextLocale);
}

const i18n = {
  elementLocale,
  locale,
  localeOptions,
  setLocale,
  t
};

type I18nContext = typeof i18n;

const i18nKey: InjectionKey<I18nContext> = Symbol('jianmen-i18n');

export function useI18n(): I18nContext {
  return inject(i18nKey, i18n);
}

export default {
  install(app: App) {
    syncDocumentLocale(locale.value);
    app.provide(i18nKey, i18n);
  }
};
