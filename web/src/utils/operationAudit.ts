import type { OperationAuditRecord } from '@/api/client';
import type { TranslationKey } from '@/i18n';

type Translate = (key: TranslationKey) => string;

/** 后端 operationKey() 输出的业务动作键。新增键必须同时补 i18n 文案。 */
const OPERATION_KEYS: Record<string, TranslationKey> = {
  issue_credential: 'audit.operation.issue_credential',
  issue_session: 'audit.operation.issue_session',
  refresh_ai_token: 'audit.operation.refresh_ai_token',
  test_connection: 'audit.operation.test_connection',
  grant_access: 'audit.operation.grant_access',
  revoke_access: 'audit.operation.revoke_access',
  update_settings: 'audit.operation.update_settings',
  update_preferences: 'audit.operation.update_preferences',
  issue_terminal_ticket: 'audit.operation.issue_terminal_ticket',
  open_sql_console: 'audit.operation.open_sql_console',
  execute_sql: 'audit.operation.execute_sql',
  close_sql_console: 'audit.operation.close_sql_console',
  disconnect_session: 'audit.operation.disconnect_session',
  reveal_platform_password: 'audit.operation.reveal_platform_password',
};

const VERB_KEYS: Record<string, TranslationKey> = {
  create: 'audit.action.create',
  update: 'audit.action.update',
  delete: 'audit.action.delete',
  revoke: 'audit.action.revoke',
  test: 'audit.action.test',
  refresh: 'audit.action.refresh',
};

const FAMILY_KEYS: Record<string, TranslationKey> = {
  host: 'audit.family.host',
  host_account: 'audit.family.host_account',
  target: 'audit.family.target',
  user: 'audit.family.user',
  role: 'audit.family.role',
  permission: 'audit.family.permission',
  user_role: 'audit.family.user_role',
  role_permission: 'audit.family.role_permission',
  resource_grant: 'audit.family.resource_grant',
  resource_group: 'audit.family.resource_group',
  user_group: 'audit.family.user_group',
  application: 'audit.family.application',
  container_endpoint: 'audit.family.container_endpoint',
  platform_account: 'audit.family.platform_account',
  temporary_account: 'audit.family.temporary_account',
  connection_password: 'audit.family.connection_password',
  online_session: 'audit.family.online_session',
  system_setting: 'audit.family.system_setting',
  ai_token: 'audit.family.ai_token',
  database_instance: 'audit.family.database_instance',
  database_account: 'audit.family.database_account',
  database_connection: 'audit.family.database_connection',
  preference: 'audit.family.preference',
  web_terminal: 'audit.family.web_terminal',
};

/** operationResourceType() 的取值 → 资源族（历史行没有业务动作时用）。 */
const RESOURCE_TYPE_FAMILY: Record<string, string> = {
  hosts: 'host',
  targets: 'target',
  users: 'user',
  host_account: 'host_account',
  database_account: 'database_account',
  'db/instances': 'database_instance',
  'db/accounts': 'database_account',
  'db/connections': 'database_connection',
  'rbac/roles': 'role',
  'rbac/permissions': 'permission',
  'rbac/user-roles': 'user_role',
  'rbac/role-permissions': 'role_permission',
  'resource-grants': 'resource_grant',
  'resource-groups': 'resource_group',
  'user-groups': 'user_group',
  applications: 'application',
  'containers/endpoints': 'container_endpoint',
  'platform-accounts': 'platform_account',
  'temporary-accounts': 'temporary_account',
  'connection-passwords': 'connection_password',
  'online-sessions': 'online_session',
  'system-settings': 'system_setting',
  ai: 'ai_token',
  me: 'preference',
  'web-terminal': 'web_terminal',
};

/** 业务动作文案；历史记录回退到通用动作名，再回退到原始值。 */
export function operationLabel(
  row: Pick<OperationAuditRecord, 'operation' | 'action'>,
  t: Translate,
): string {
  const key = String(row.operation ?? '').trim();
  if (!key) return legacyActionLabel(row.action, t);
  const known = OPERATION_KEYS[key];
  if (known) return t(known);
  const [verb, family] = key.split(':');
  if (family) {
    const verbText = VERB_KEYS[verb] ? t(VERB_KEYS[verb]) : verb;
    const familyText = FAMILY_KEYS[family] ? t(FAMILY_KEYS[family]) : family;
    // 中文动词直接接中文字段（新增主机）；后接拉丁字母时补一个空格（新增 AI 令牌）。
    const separator = /^[A-Za-z0-9]/.test(familyText) ? ' ' : '';
    return `${verbText}${separator}${familyText}`;
  }
  return t('audit.operation.uncategorized');
}

export function legacyActionLabel(action: unknown, t: Translate): string {
  const key = String(action ?? '').trim().toLowerCase();
  const known = VERB_KEYS[key];
  return known ? t(known) : String(action || '-');
}

/** 操作对象：优先用写入时的可读名，否则「资源族 · 短 ID」。 */
export function operationTargetLabel(
  row: Pick<OperationAuditRecord, 'operation' | 'target_label' | 'resource_type' | 'resource_id' | 'resource_name'>,
  t: Translate,
): string {
  const label = String(row.target_label ?? '').trim();
  if (label) return label;
  const family = familyOf(row);
  const familyText = family && FAMILY_KEYS[family] ? t(FAMILY_KEYS[family]) : '';
  const id = String(row.resource_id ?? '').trim();
  const shortId = id.length > 12 ? `${id.slice(0, 12)}…` : id;
  if (familyText && shortId) return `${familyText} · ${shortId}`;
  if (familyText) return familyText;
  return shortId || t('common.none');
}

export function familyOf(
  row: Pick<OperationAuditRecord, 'operation' | 'resource_type'>,
): string {
  const key = String(row.operation ?? '').trim();
  const [, family] = key.split(':');
  if (family) return family;
  return RESOURCE_TYPE_FAMILY[String(row.resource_type ?? '').trim()] ?? '';
}

export function channelLabel(channel: unknown, t: Translate): string {
  switch (String(channel ?? '').trim()) {
    case 'ai_api':
      return t('audit.channel.aiApi');
    case 'admin_console':
      return t('audit.channel.adminConsole');
    default:
      return t('audit.channel.unknown');
  }
}

/** 业务动作筛选项：只列固定语义的动作，通用「动词:资源」不铺开。 */
export function operationFilterOptions(t: Translate): Array<{ value: string; label: string }> {
  return Object.entries(OPERATION_KEYS).map(([value, key]) => ({ value, label: t(key) }));
}

/** 通用动作筛选项：按动词一次筛出所有「新增 / 修改 / 删除」类资源变更。 */
export function operationVerbFilterOptions(t: Translate): Array<{ value: string; label: string }> {
  return (['create', 'update', 'delete'] as const).map(value => ({ value, label: t(VERB_KEYS[value]) }));
}

/** 下拉值编码：verb:create 与 op:issue_credential 两种前缀，避免两套下拉占位置。 */
export const OPERATION_FILTER_SEPARATOR = ':';

export function parseOperationFilter(value: string): { verb?: string; operation?: string } {
  const index = value.indexOf(OPERATION_FILTER_SEPARATOR);
  if (index <= 0) return {};
  const kind = value.slice(0, index);
  const target = value.slice(index + 1);
  if (!target) return {};
  if (kind === 'verb') return { verb: target };
  if (kind === 'op') return { operation: target };
  return {};
}

export function channelFilterOptions(t: Translate): Array<{ value: string; label: string }> {
  return [
    { value: 'ai_api', label: t('audit.channel.aiApi') },
    { value: 'admin_console', label: t('audit.channel.adminConsole') },
  ];
}
