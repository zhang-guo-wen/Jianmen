const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '');
const TOKEN_KEY = 'jianmen_token';

// ── 统一响应格式 ──────────────────────────────────────────────────

export interface ApiEnvelope<T = unknown> {
  code: number;        // 0 = 成功
  data: T;
  message: string;
  request_id: string;
  timestamp: string;
}

export interface ApiErrorBody {
  code: string;
  message: string;
  details?: unknown;
}

export interface ApiErrorEnvelope {
  code: number;        // HTTP 状态码
  error: ApiErrorBody;
  request_id: string;
  timestamp: string;
}

export class ApiError extends Error {
  code: string;
  statusCode: number;
  requestId: string;
  details?: unknown;

  constructor(statusCode: number, errorCode: string, message: string, requestId: string, details?: unknown) {
    super(message);
    this.name = 'ApiError';
    this.statusCode = statusCode;
    this.code = errorCode;
    this.requestId = requestId;
    this.details = details;
  }
}

export interface PageResponse<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface HealthResponse {
  status?: string;
  version?: string;
  time?: string;
  [key: string]: unknown;
}

export interface LoginCaptchaChallenge {
  algorithm: string;
  challenge: string;
  maxNumber: number;
  salt: string;
  signature: string;
}

export interface InitStatusResponse {
  initialized: boolean;
  admin?: {
    username?: string;
    display_name?: string;
    email?: string;
  };
}

export interface UserRecord {
  id?: string | number;
  username?: string;
  name?: string;
  display_name?: string;
  email?: string;
  role?: string;
  status?: string;
  is_super_admin?: boolean;
  last_login_at?: string;
  expires_at?: string;
  created_at?: string;
  updated_at?: string;
  [key: string]: unknown;
}

export interface UserPayload {
  username?: string;
  password?: string;
  display_name?: string;
  email?: string;
  status?: string;
  expires_at?: string;
  permanent?: boolean;
}

export interface AccessPage {
  key: string;
  path: string;
  order: number;
}

export interface MyAccessContextResponse {
  actions: string[];
  pages: AccessPage[];
}

export interface AIAccessTokenRecord {
  id: string;
  name: string;
  access_expires_at: string;
  refresh_expires_at: string;
  last_used_at?: string;
  revoked_at?: string;
  created_at: string;
  has_secret?: boolean;
}

export interface IssuedAIAccessToken extends AIAccessTokenRecord {
  access_token: string;
  refresh_token: string;
  temporary_account_id?: string;
  temporary_expires_at?: string;
  prompt?: string;
  copy_prompt?: string;
  full_prompt?: string;
}

export interface UserPreferences {
  theme: 'system' | 'light' | 'dark';
  ssh_client: string;
  ssh_client_path: string;
  terminal_font_family: string;
  terminal_font_size: number;
}

export type UserPreferencesUpdate = Partial<UserPreferences>;

// ── Target / HostAccount ──────────────────────────────────────────────

export interface TargetRecord {
  id?: string | number;
  host_id?: string;
  resource_type?: string;
  resource_id?: string;
  resource_seq?: number;
  host_resource_id?: string;
  name?: string;
  group?: string;
  remark?: string;
  expires_at?: string;
  host?: string;
  port?: number;
  username?: string;
  auth_methods?: string[];
  insecure_ignore_host_key?: boolean;
  host_key_fingerprint?: string;
  known_hosts_path?: string;
  status?: string;
  [key: string]: unknown;
  can_manage?: boolean;
}

export interface TargetPayload {
  id: string;
  host_id?: string;
  name: string;
  group?: string;
  remark?: string;
  disabled?: boolean;
  expires_at?: string;
  host: string;
  port: number;
  username: string;
  password: string;
  private_key_path: string;
  private_key_pem: string;
  passphrase: string;
  insecure_ignore_host_key: boolean;
  host_key_fingerprint: string;
  known_hosts_path: string;
}

// ── Host ───────────────────────────────────────────────────────────────

export interface HostView {
  id?: string;
  name: string;
  group?: string;
  address: string;
  port: number;
  remark?: string;
  status?: string;
  account_count?: number;
  created_at?: string;
  updated_at?: string;
  can_manage?: boolean;
}

export interface HostPayload {
  id?: string;
  name: string;
  group?: string;
  address: string;
  port: number;
  remark?: string;
  status?: string;
  account_count?: number;
  created_at?: string;
  updated_at?: string;
}

// ── Sessions ───────────────────────────────────────────────────────────

export interface SessionRecord {
  id?: string | number;
  username?: string;
  user_id?: string;
  user_username?: string;
  target_name?: string;
  target_address?: string;
  target_id?: string;
  account_name?: string;
  account_username?: string;
  client_ip?: string;
  status?: string;
  state?: string;
  startedAt?: string;
  started_at?: string;
  ended_at?: string;
  protocol?: string;
  protocol_subtype?: string;
  replay_dir?: string;
  log_count?: number;
  [key: string]: unknown;
}

export interface OnlineSessionRecord {
  id: string;
  audit_session_id: string;
  resource_type: 'host' | 'database_instance';
  resource_id: string;
  account_id?: string;
  instance: string;
  protocol: string;
  protocol_subtype?: string;
  account: string;
  operator: string;
  started_at: string;
  has_replay: boolean;
}

export interface LoginAuditRecord {
  id: string;
  user_id?: string;
  username: string;
  outcome: 'success' | 'failure' | 'blocked' | string;
  reason?: string;
  client_ip: string;
  user_agent?: string;
  created_at: string;
}

export interface OperationAuditRecord {
  id: string;
  actor_id: string;
  actor_username: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  resource_name?: string;
  detail?: string;
  client_ip?: string;
  created_at: string;
}

export interface ConnectionPasswordRecord {
  password: string;
  expires_at: string;
  expires_in_seconds: number;
  reusable: boolean;
}

export interface UserSessionRecord {
  id?: string;
  session_id?: string;
  session_seq?: number;
  type?: string;
  status?: string;
  resource_id?: string;
  resource_type?: string;
  compact_username?: string;
  [key: string]: unknown;
}

export interface SessionMetaRecord {
  session_id?: string;
  id?: string;
  username?: string;
  target_name?: string;
  client_ip?: string;
  started_at?: string;
  ended_at?: string;
  [key: string]: unknown;
}

export interface SessionCommandRecord {
  seq?: number;
  command?: string;
  output?: string;
  confidence?: string;
  timestamp?: string;
  started_at?: string;
  ended_at?: string;
  offset_ms?: number;
  [key: string]: unknown;
}

export interface SessionFileEventRecord {
  seq?: number;
  action?: string;
  path?: string;
  path2?: string;
  result?: string;
  timestamp?: string;
  started_at?: string;
  ended_at?: string;
  size?: number;
  [key: string]: unknown;
}

// ── Database ───────────────────────────────────────────────────────────

export interface DBConnectionRecord {
  id?: string;
  username?: string;
  target_name?: string;
  account_name?: string;
  name?: string;
  protocol?: string;
  client_addr?: string;
  upstream_addr?: string;
  started_at?: string;
  ended_at?: string;
  duration_ms?: number;
  log_count?: number;
  [key: string]: unknown;
}

export interface DBGatewayConfig {
  enabled: boolean;
  listen_addr: string;
  host: string;
  port: number;
}

export interface DatabaseInstanceView {
  id?: string;
  name?: string;
  protocol?: string;
  address?: string;
  port?: number;
  group?: string;
  remark?: string;
  status?: string;
  account_count?: number;
  created_at?: string;
  updated_at?: string;
  [key: string]: unknown;
  can_manage?: boolean;
}

export interface DBAccountRecord {
  id?: string;
  instance_id?: string;
  unique_name?: string;
  username?: string;
  group?: string;
  remark?: string;
  expires_at?: string;
  status?: string;
  resource_id?: string;
  resource_seq?: number;
  created_at?: string;
  updated_at?: string;
  instance_name?: string;
  instance_address?: string;
  [key: string]: unknown;
  can_manage?: boolean;
}

export interface DBInstancePayload {
  name: string;
  protocol: string;
  address: string;
  port?: number;
  group?: string;
  remark?: string;
}

export interface DBAccountPayload {
  username: string;
  password: string;
  group?: string;
  remark?: string;
  expires_at?: string;
}

export interface DBAccountTestPayload {
  instance_id: string;
  username: string;
  password: string;
}

export interface DBAccountUpdatePayload {
  username: string;
  password?: string;
  group?: string;
  remark?: string;
  expires_at?: string;
  status?: string;
}

export interface DBConnectionMetaRecord extends DBConnectionRecord {
  ended_at?: string;
  duration_ms?: number;
  account_name?: string;
  instance_name?: string;
  auth_user?: string;
  database?: string;
  application_name?: string;
  mysql_connect_attrs?: Record<string, string>;
  auth_observation?: string;
  allowed_users_enforced?: boolean;
}

export interface DBQueryEventRecord {
  type?: string;
  connection_id?: string;
  seq?: number;
  protocol?: string;
  sql?: string;
  query_kind?: string;
  detail?: Record<string, unknown>;
  started_at?: number;
  completed_at?: number;
  duration_ms?: number;
  status?: string;
  error_code?: string;
  error_message?: string;
  rows_affected?: number | null;
  rows?: number | null;
  [key: string]: unknown;
}

// ── Application (Web App Proxy) ─────────────────────────────────────────

export interface ApplicationView {
  id?: string;
  name: string;
  group?: string;
  listen_port: number;
  address: string;
  entry_path: string;
  internal_scheme: string;
  internal_host: string;
  internal_port: number;
  remark?: string;
  status?: string;
  created_at?: string;
  updated_at?: string;
  can_manage?: boolean;
}

export interface ApplicationPayload {
  name?: string;
  address: string;
  listen_port?: number;
  group?: string;
  remark?: string;
}

// ── PlatformAccount ────────────────────────────────────────────────────

export interface ContainerEndpointView {
  id?: string;
  name: string;
  group?: string;
  runtime: 'docker' | 'containerd' | string;
  connection_mode: 'ssh' | 'docker_api' | 'containerd' | string;
  address: string;
  port?: number;
  host_id?: string;
  host_name?: string;
  host_account_id?: string;
  host_account_name?: string;
  remark?: string;
  status?: string;
  created_at?: string;
  updated_at?: string;
  can_manage?: boolean;
}

export interface ContainerEndpointPayload {
  id?: string;
  name?: string;
  group?: string;
  runtime: string;
  connection_mode: string;
  address: string;
  port?: number;
  host_id?: string;
  host_account_id?: string;
  remark?: string;
  status?: string;
}

export interface ContainerRecord {
  id: string;
  name: string;
  image?: string;
  state?: string;
  status?: string;
  ports?: string;
  created?: string;
}

export interface PlatformAccountView {
  id?: string;
  name?: string;
  platform_name: string;
  url?: string;
  group?: string;
  username: string;
  has_password?: boolean;
  remark?: string;
  owner_id?: string;
  owner_name?: string;
  status?: string;
  expires_at?: string;
  created_at?: string;
  updated_at?: string;
  [key: string]: unknown;
}

export interface PlatformAccountPayload {
  name?: string;
  platform_name?: string;
  url?: string;
  group?: string;
  username: string;
  password?: string;
  remark?: string;
  expires_at?: string;
}

// ?? RBAC ???????????????????????????????????????????????????????????????

export interface RBACRoleRecord {
  id?: string;
  name?: string;
  description?: string;
  builtin?: boolean;
  status?: string;
  created_at?: string;
  updated_at?: string;
  [key: string]: unknown;
}

export interface RBACPermissionRecord {
  id?: string;
  name?: string;
  action?: string;
  resource_type?: string;
  resource_id?: string;
  effect?: 'allow' | 'deny' | string;
  description?: string;
  created_at?: string;
  updated_at?: string;
  [key: string]: unknown;
}

export interface RBACUserRoleRecord {
  id?: string;
  user_id?: string;
  role_id?: string;
  expires_at?: string;
  created_at?: string;
  user?: UserRecord;
  role?: RBACRoleRecord;
  [key: string]: unknown;
}

export interface RBACRolePermissionRecord {
  id?: string;
  role_id?: string;
  permission_id?: string;
  created_at?: string;
  role?: RBACRoleRecord;
  permission?: RBACPermissionRecord;
  [key: string]: unknown;
}

export interface RBACPermissionDefinition {
  action: string;
  label: string;
  description: string;
  resource_types?: string[];
  assignable: boolean;
}

export interface RBACPermissionPageDefinition {
  key: string;
  label: string;
  path: string;
  order: number;
  actions: RBACPermissionDefinition[];
}

export interface RBACCatalogResponse {
  pages: RBACPermissionPageDefinition[];
}

export interface RBACRoleActionsResponse {
  role_id: string;
  actions: string[];
}

export interface RBACRolePayload {
  id?: string;
  name: string;
  description?: string;
  status?: string;
}

export interface RBACPermissionPayload {
  id?: string;
  name?: string;
  action: string;
  resource_type?: string;
  resource_id?: string;
  effect: 'allow' | 'deny' | string;
  description?: string;
}

export interface RBACUserRolePayload {
  user_id: string;
  role_id: string;
  expires_at?: string;
}

export interface RBACRolePermissionPayload {
  role_id: string;
  permission_id: string;
}

export interface RBACEffectiveCheckPayload {
  user_id: string;
  action: string;
  resource_type?: string;
  resource_id?: string;
}

export interface RBACEffectiveCheckResult {
  allowed?: boolean;
  decision?: string;
  reason?: string;
  matched_permissions?: RBACPermissionRecord[];
  [key: string]: unknown;
}

// ── User Group types ────────────────────────────────────────────────

export interface UserGroupRecord {
  id: string;
  name: string;
  description?: string;
  created_at: string;
  updated_at: string;
}

export interface UserGroupMemberRecord {
  id: string;
  group_id: string;
  user_id: string;
  created_at: string;
}

export interface UserGroupPayload {
  name: string;
  description?: string;
}

// ── Resource Grant types ────────────────────────────────────────────

export interface ResourceGrantRecord {
  id: string;
  principal_type: 'user' | 'user_group';
  principal_id: string;
  resource_type: string;
  resource_id: string;
  effect: 'allow' | 'deny';
  expires_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ResourceGrantPayload {
  principal_type: 'user' | 'user_group';
  principal_id: string;
  resource_type: string;
  resource_id: string;
  effect?: 'allow' | 'deny';
  expires_at?: string;
}

export interface TemporaryConnectionRecord {
  address: string;
  host: string;
  port: number;
  username: string;
  password: string;
  protocol: string;
  expires_at: string;
}

export interface TemporaryAccountRecord {
  id: string;
  session_id: string;
  type: 'temporary_user' | 'ai_user' | string;
  authorized_user_id?: string;
  authorized_user?: string;
  status: string;
  starts_at: string;
  expires_at?: string;
  resource_type?: string;
  resource_name?: string;
  account_name?: string;
  remark?: string;
  created_at: string;
  connection?: TemporaryConnectionRecord;
}

// ── Resource Group types ────────────────────────────────────────────

export interface ResourceGroupRecord {
  id: string;
  name: string;
  group_type: string;
  description?: string;
  host_count: number;
  database_count: number;
  application_count: number;
  platform_count: number;
  account_count: number;
  created_at: string;
  updated_at: string;
}

export interface ResourceGroupPayload {
  name: string;
  group_type?: string;
  description?: string;
}

export interface TestConnectionResult {
  ok: boolean;
  message?: string;
  latency_ms?: number;
  error?: string;
}

// ── helpers ────────────────────────────────────────────────────────────

function buildQS(params?: Record<string, string | number | boolean | undefined>): string {
  if (!params) return '';
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') {
      qs.set(k, String(v));
    }
  }
  const s = qs.toString();
  return s ? `?${s}` : '';
}

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? '';
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = getToken();
  const headers = new Headers(init.headers);

  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json');
  }

  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }

  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers
  });

  // 204 No Content
  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get('content-type') ?? '';
  if (!contentType.includes('application/json')) {
    // 非 JSON 响应（如 asciicast replay 文件）
    if (!response.ok) {
      throw new ApiError(response.status, 'UNKNOWN', response.statusText, '');
    }
    return (await response.text()) as unknown as T;
  }

  const payload = await response.json();

  // 401 表示 token 过期或无效，清除 token 并跳转登录
  if (response.status === 401) {
    clearToken();
    if (window.location.pathname !== '/login') {
      window.location.href = '/login';
    }
    const errBody = (payload?.error as ApiErrorBody | undefined);
    throw new ApiError(
      response.status,
      errBody?.code || 'UNAUTHORIZED',
      errBody?.message || 'Unauthorized',
      payload?.request_id || ''
    );
  }

  if (!response.ok) {
    // 新格式：{code, error: {code, message, details}, request_id, timestamp}
    if (payload && typeof payload === 'object' && 'error' in payload) {
      const errBody = payload.error as ApiErrorBody;
      throw new ApiError(
        response.status,
        errBody.code || 'UNKNOWN',
        errBody.message || response.statusText,
        payload.request_id || '',
        errBody.details
      );
    }
    throw new ApiError(response.status, 'UNKNOWN', response.statusText, '');
  }

  // 成功：从统一响应格式 {code: 0, data: ..., message: "ok", request_id: "...", timestamp: "..."} 中提取 data
  if (payload && typeof payload === 'object' && 'code' in payload && payload.code === 0) {
    return payload.data as T;
  }

  // 兼容旧格式：直接用原始 payload（逐步淘汰）
  return payload as T;
}

// ── API client ─────────────────────────────────────────────────────────

export const apiClient = {
  // health
  getHealth: () => request<HealthResponse>('/api/health'),

  // users
  getUsers: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<UserRecord>>(`/api/users${buildQS(params as Record<string, string | number | undefined>)}`),
  createUser: (payload: UserPayload) =>
    request<{ user: UserRecord; token: string }>('/api/users', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  updateUser: (id: string | number, payload: UserPayload) =>
    request<UserRecord>(`/api/users/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  deleteUser: (id: string | number) =>
    request<void>(`/api/users/${encodeURIComponent(String(id))}`, {
      method: 'DELETE',
    }),

  // me
  getMyAccessContext: () =>
    request<MyAccessContextResponse>('/api/me/access-context'),
  getMyPreferences: () => request<UserPreferences>('/api/me/preferences'),
  updateMyPreferences: (payload: UserPreferencesUpdate) =>
    request<UserPreferences>('/api/me/preferences', {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),

  // AI access tokens
  getAITokens: () => request<AIAccessTokenRecord[]>('/api/ai/tokens'),
  getAIToken: (id: string) => request<IssuedAIAccessToken>(`/api/ai/tokens/${encodeURIComponent(id)}`),
  getAIDocs: () => request<string>('/api/ai/docs'),
  createAIToken: (payload: { name?: string; access_ttl_seconds?: number; refresh_ttl_seconds?: number; expires_at?: string; permanent?: boolean; remark?: string }) =>
    request<IssuedAIAccessToken>('/api/ai/tokens', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  revokeAIToken: (id: string) =>
    request<void>(`/api/ai/tokens/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  // hosts
  getHosts: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<HostView>>(`/api/hosts${buildQS(params as Record<string, string | number | undefined>)}`),
  createHost: (payload: HostPayload) =>
    request<HostView>('/api/hosts', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateHost: (id: string | number, payload: HostPayload) =>
    request<HostView>(`/api/hosts/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteHost: (id: string | number) =>
    request<void>(`/api/hosts/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),

  // host accounts
  getHostAccounts: (id: string | number, params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<TargetRecord>>(`/api/hosts/${encodeURIComponent(String(id))}/accounts${buildQS(params as Record<string, string | number | undefined>)}`),

  // targets
  getTargets: (params?: { page?: number; page_size?: number; q?: string; connectable?: boolean }) =>
    request<PageResponse<TargetRecord>>(`/api/targets${buildQS(params)}`),
  getTarget: (id: string | number) =>
    request<TargetRecord>(`/api/targets/${encodeURIComponent(String(id))}`),
  createTarget: (payload: TargetPayload) =>
    request<TargetRecord>('/api/targets', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateTarget: (id: string | number, payload: TargetPayload) =>
    request<TargetRecord>(`/api/targets/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteTarget: (id: string | number) =>
    request<void>(`/api/targets/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),
  testTargetConnection: (payload: TargetPayload) =>
    request<TestConnectionResult>('/api/targets/test-connection', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),

  // sessions
  createUserSession: (targetId: string) =>
    request<UserSessionRecord>('/api/user-sessions', {
      method: 'POST',
      body: JSON.stringify({ target_id: targetId })
    }),
  createConnectionPassword: (targetId: string) =>
    request<ConnectionPasswordRecord>('/api/connection-passwords', {
      method: 'POST',
      body: JSON.stringify({ target_id: targetId })
    }),
  getOnlineSessions: (params?: { page?: number; page_size?: number; q?: string; resource_type?: string; resource_id?: string }) =>
    request<PageResponse<OnlineSessionRecord>>(`/api/online-sessions${buildQS(params as Record<string, string | number | undefined>)}`),
  disconnectOnlineSession: (id: string) =>
    request<void>(`/api/online-sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  getSessions: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<SessionRecord>>(`/api/audit/ssh${buildQS(params as Record<string, string | number | undefined>)}`),
  getLoginAuditLogs: (params?: { page?: number; page_size?: number; q?: string; outcome?: string; date?: string }) =>
    request<PageResponse<LoginAuditRecord>>(`/api/audit/logins${buildQS(params as Record<string, string | number | undefined>)}`),
  getOperationAuditLogs: (params?: { page?: number; page_size?: number; q?: string; action?: string; resource_type?: string; date?: string }) =>
    request<PageResponse<OperationAuditRecord>>(`/api/audit/operations${buildQS(params as Record<string, string | number | undefined>)}`),
  getSessionMeta: (id: string | number) =>
    request<SessionMetaRecord>(
      `/api/audit/ssh/${encodeURIComponent(String(id))}`
    ),
  getSessionCommands: (id: string | number) =>
    request<SessionCommandRecord[]>(
      `/api/audit/ssh/${encodeURIComponent(String(id))}/commands`
    ),
  getSessionFiles: (id: string | number) =>
    request<SessionFileEventRecord[]>(
      `/api/audit/ssh/${encodeURIComponent(String(id))}/files`
    ),
  getSessionFileSummary: (id: string | number) =>
    request<Record<string, unknown>>(
      `/api/audit/ssh/${encodeURIComponent(String(id))}/file-summary`
    ),
  getSessionReplay: (id: string | number) =>
    request<string>(`/api/audit/ssh/${encodeURIComponent(String(id))}/replay`),

  // database gateway & instances
  getDBGateway: () => request<DBGatewayConfig>('/api/db/gateway'),

  getDBInstances: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<DatabaseInstanceView>>(`/api/db/instances${buildQS(params as Record<string, string | number | undefined>)}`),
  createDBInstance: (payload: DBInstancePayload) =>
    request<DatabaseInstanceView>('/api/db/instances', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateDBInstance: (id: string, payload: DBInstancePayload & { status?: string }) =>
    request<DatabaseInstanceView>(`/api/db/instances/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteDBInstance: (id: string) =>
    request<void>(`/api/db/instances/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),

  // database accounts
  getAllDBAccounts: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<DBAccountRecord>>(`/api/db/accounts${buildQS(params as Record<string, string | number | undefined>)}`),
  getDBAccounts: (instanceID: string, params?: { page?: number; page_size?: number; q?: string; connectable?: boolean }) =>
    request<PageResponse<DBAccountRecord>>(`/api/db/instances/${encodeURIComponent(instanceID)}/accounts${buildQS(params)}`),
  createDBAccount: (instanceID: string, payload: DBAccountPayload) =>
    request<DBAccountRecord>(
      `/api/db/instances/${encodeURIComponent(instanceID)}/accounts`,
      {
        method: 'POST',
        body: JSON.stringify(payload)
      }
    ),
  getDBAccount: (id: string) =>
    request<DBAccountRecord>(`/api/db/accounts/${encodeURIComponent(id)}`),
  updateDBAccount: (id: string, payload: DBAccountUpdatePayload) =>
    request<DBAccountRecord>(`/api/db/accounts/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteDBAccount: (id: string) =>
    request<void>(`/api/db/accounts/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),
  testDBConnection: (id: string) =>
    request<{ ok: boolean; error?: string; latency_ms: number }>(
      `/api/db/accounts/test/${encodeURIComponent(id)}`,
      { method: 'POST' }
    ),
  testDBConnectionPayload: (payload: DBAccountTestPayload) =>
    request<{ ok: boolean; error?: string; latency_ms: number }>('/api/db/accounts/test', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),

  // auto-provision
  listDBDatabases: (instanceId: string, adminAccountId: string) =>
    request<{ databases: string[] }>(`/api/db/instances/${encodeURIComponent(instanceId)}/databases?admin_account_id=${encodeURIComponent(adminAccountId)}`),

  provisionDBAccount: (instanceId: string, payload: {
    admin_account_id: string
    new_username?: string
    password?: string
    host?: string
    grants: Array<{ database: string; privilege: string }>
    group?: string
    remark?: string
    expires_at?: string
  }) =>
    request<{ ok: boolean; account: any; generated_password: string }>(
      `/api/db/instances/${encodeURIComponent(instanceId)}/provision-account`,
      {
        method: 'POST',
        body: JSON.stringify(payload),
      }
    ),

  // database connections (audit)
  getDBConnections: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<DBConnectionRecord>>(`/api/audit/db${buildQS(params as Record<string, string | number | undefined>)}`),
  getDBConnectionMeta: (id: string | number) =>
    request<DBConnectionMetaRecord>(
      `/api/audit/db/${encodeURIComponent(String(id))}`
    ),
  getDBConnectionQueries: (id: string | number) =>
    request<DBQueryEventRecord[]>(
      `/api/audit/db/${encodeURIComponent(String(id))}/queries`
    ),

  // applications (web app proxy)
  getApplications: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<ApplicationView>>(`/api/applications${buildQS(params as Record<string, string | number | undefined>)}`),
  createApplication: (payload: ApplicationPayload) =>
    request<ApplicationView>('/api/applications', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateApplication: (id: string, payload: ApplicationPayload & { status?: string }) =>
    request<ApplicationView>(`/api/applications/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteApplication: (id: string) =>
    request<void>(`/api/applications/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),

  // containers
  getContainerEndpoints: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<ContainerEndpointView>>(`/api/containers/endpoints${buildQS(params as Record<string, string | number | undefined>)}`),
  createContainerEndpoint: (payload: ContainerEndpointPayload) =>
    request<ContainerEndpointView>('/api/containers/endpoints', { method: 'POST', body: JSON.stringify(payload) }),
  updateContainerEndpoint: (id: string, payload: ContainerEndpointPayload) =>
    request<ContainerEndpointView>(`/api/containers/endpoints/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteContainerEndpoint: (id: string) =>
    request<void>(`/api/containers/endpoints/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  testContainerConnection: (payload: ContainerEndpointPayload) =>
    request<{ ok: boolean; message?: string; latency_ms: number }>('/api/containers/test', { method: 'POST', body: JSON.stringify(payload) }),
  listContainers: (endpointId: string, signal?: AbortSignal) =>
    request<{ items: ContainerRecord[] }>(`/api/containers/endpoints/${encodeURIComponent(endpointId)}/containers`, { signal }),
  getContainerLogs: (endpointId: string, containerId: string, tail = 200, signal?: AbortSignal) =>
    request<{ logs: string }>(`/api/containers/endpoints/${encodeURIComponent(endpointId)}/containers/${encodeURIComponent(containerId)}/logs?tail=${tail}`, { signal }),

  // platform accounts
  getPlatformAccounts: (params?: { page?: number; page_size?: number; q?: string; platform?: string }) =>
    request<PageResponse<PlatformAccountView>>(`/api/platform-accounts${buildQS(params as Record<string, string | number | undefined>)}`),
  getPlatformAccount: (id: string) =>
    request<PlatformAccountView>(`/api/platform-accounts/${encodeURIComponent(id)}`),
  createPlatformAccount: (payload: PlatformAccountPayload) =>
    request<PlatformAccountView>('/api/platform-accounts', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updatePlatformAccount: (id: string, payload: PlatformAccountPayload & { status?: string }) =>
    request<PlatformAccountView>(`/api/platform-accounts/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deletePlatformAccount: (id: string) =>
    request<void>(`/api/platform-accounts/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),
  getPlatformAccountPassword: (id: string) =>
    request<{ password: string }>(`/api/platform-accounts/${encodeURIComponent(id)}/password`),

  // rbac
  getRBACCatalog: () =>
    request<RBACCatalogResponse>('/api/rbac/catalog'),
  getRBACRoles: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<RBACRoleRecord>>(`/api/rbac/roles${buildQS(params as Record<string, string | number | undefined>)}`),
  createRBACRole: (payload: RBACRolePayload) =>
    request<RBACRoleRecord>('/api/rbac/roles', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateRBACRole: (id: string | number, payload: RBACRolePayload) =>
    request<RBACRoleRecord>(`/api/rbac/roles/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteRBACRole: (id: string | number) =>
    request<void>(`/api/rbac/roles/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),
  getRBACRoleActions: (id: string | number) =>
    request<RBACRoleActionsResponse>(
      `/api/rbac/roles/${encodeURIComponent(String(id))}/actions`
    ),
  replaceRBACRoleActions: (id: string | number, actions: string[]) =>
    request<RBACRoleActionsResponse>(
      `/api/rbac/roles/${encodeURIComponent(String(id))}/actions`,
      {
        method: 'PUT',
        body: JSON.stringify({ actions })
      }
    ),

  getRBACPermissions: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<RBACPermissionRecord>>(`/api/rbac/permissions${buildQS(params as Record<string, string | number | undefined>)}`),
  createRBACPermission: (payload: RBACPermissionPayload) =>
    request<RBACPermissionRecord>('/api/rbac/permissions', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  deleteRBACPermission: (id: string | number) =>
    request<void>(
      `/api/rbac/permissions/${encodeURIComponent(String(id))}`,
      {
        method: 'DELETE'
      }
    ),

  getRBACUserRoles: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<RBACUserRoleRecord>>(`/api/rbac/user-roles${buildQS(params as Record<string, string | number | undefined>)}`),
  createRBACUserRole: (payload: RBACUserRolePayload) =>
    request<RBACUserRoleRecord>('/api/rbac/user-roles', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  deleteRBACUserRole: (id: string | number) =>
    request<void>(`/api/rbac/user-roles/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),

  getRBACRolePermissions: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<RBACRolePermissionRecord>>(`/api/rbac/role-permissions${buildQS(params as Record<string, string | number | undefined>)}`),
  createRBACRolePermission: (payload: RBACRolePermissionPayload) =>
    request<RBACRolePermissionRecord>(
      '/api/rbac/role-permissions',
      {
        method: 'POST',
        body: JSON.stringify(payload)
      }
    ),
  deleteRBACRolePermission: (id: string | number) =>
    request<void>(
      `/api/rbac/role-permissions/${encodeURIComponent(String(id))}`,
      {
        method: 'DELETE'
      }
    ),
  checkRBACEffective: (payload: RBACEffectiveCheckPayload) => {
    const params = new URLSearchParams({
      user_id: payload.user_id,
      action: payload.action
    });
    if (payload.resource_type) {
      params.set('resource_type', payload.resource_type);
    }
    if (payload.resource_id) {
      params.set('resource_id', payload.resource_id);
    }
    return request<RBACEffectiveCheckResult>(
      `/api/rbac/effective?${params.toString()}`
    );
  },

  // ── User Groups ─────────────────────────────────────────────────────
  getUserGroups: (params?: { q?: string; page?: number; page_size?: number }) =>
    request<{ items: UserGroupRecord[]; total: number }>(`/api/user-groups${buildQS(params as Record<string, string | number | undefined>)}`),
  createUserGroup: (payload: UserGroupPayload) =>
    request<UserGroupRecord>('/api/user-groups', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateUserGroup: (id: string, payload: UserGroupPayload) =>
    request<UserGroupRecord>(`/api/user-groups/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteUserGroup: (id: string) =>
    request<void>(`/api/user-groups/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),
  getUserGroupMembers: (groupId: string) =>
    request<UserGroupMemberRecord[]>(`/api/user-groups/${encodeURIComponent(groupId)}/members`),
  addUserGroupMember: (groupId: string, userId: string) =>
    request<UserGroupMemberRecord>(`/api/user-groups/${encodeURIComponent(groupId)}/members`, {
      method: 'POST',
      body: JSON.stringify({ user_id: userId })
    }),
  removeUserGroupMember: (groupId: string, userId: string) =>
    request<void>(`/api/user-groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(userId)}`, {
      method: 'DELETE'
    }),

  // ── Resource Grants ─────────────────────────────────────────────────
  getResourceGrants: (params?: { q?: string; page?: number; page_size?: number }) =>
    request<{ items: ResourceGrantRecord[]; total: number }>(`/api/resource-grants${buildQS(params as Record<string, string | number | undefined>)}`),
  createResourceGrant: (payload: ResourceGrantPayload) =>
    request<ResourceGrantRecord>('/api/resource-grants', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  deleteResourceGrant: (id: string) =>
    request<void>(`/api/resource-grants/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),
  checkResourceGrant: (userId: string, resourceType: string, resourceId: string) =>
    request<{ allowed: boolean }>(`/api/resource-grants/check?user_id=${encodeURIComponent(userId)}&resource_type=${encodeURIComponent(resourceType)}&resource_id=${encodeURIComponent(resourceId)}`),

  // ?? Temporary authorizations ???????????????????????????????????????
  getTemporaryAccounts: (params?: { q?: string; page?: number; page_size?: number }) =>
    request<PageResponse<TemporaryAccountRecord>>(`/api/temporary-accounts${buildQS(params as Record<string, string | number | undefined>)}`),
  createTemporaryAuthorization: (payload: { resource_type: string; resource_id: string; expires_at: string; remark?: string }) =>
    request<TemporaryAccountRecord>('/api/temporary-accounts', { method: 'POST', body: JSON.stringify(payload) }),
  extendTemporaryAccount: (id: string, expires_at: string) =>
    request<TemporaryAccountRecord>(`/api/temporary-accounts/${encodeURIComponent(id)}/extend`, { method: 'POST', body: JSON.stringify({ expires_at }) }),
  disableTemporaryAccount: (id: string) =>
    request<TemporaryAccountRecord>(`/api/temporary-accounts/${encodeURIComponent(id)}/disable`, { method: 'POST' }),

  // ── Resource Groups ────────────────────────────────────────────────
  getResourceGroups: (params?: { group_type?: string; q?: string; page?: number; page_size?: number }) =>
    request<{ items: ResourceGroupRecord[]; total: number }>(`/api/resource-groups${buildQS(params as Record<string, string | number | undefined>)}`),
  createResourceGroup: (payload: ResourceGroupPayload) =>
    request<ResourceGroupRecord>('/api/resource-groups', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  getResourceGroup: (id: string) =>
    request<ResourceGroupRecord>(`/api/resource-groups/${encodeURIComponent(id)}`),
  updateResourceGroup: (id: string, payload: ResourceGroupPayload) =>
    request<ResourceGroupRecord>(`/api/resource-groups/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteResourceGroup: (id: string) =>
    request<void>(`/api/resource-groups/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),

  // auth & init
  getLoginCaptchaChallenge: () =>
    request<LoginCaptchaChallenge>('/api/login/challenge'),
  login: (username: string, password: string, captchaPayload: string) =>
    request<{ token: string }>('/api/login', {
      method: 'POST',
      body: JSON.stringify({ username, password, captcha_payload: captchaPayload }),
    }),
  getInitStatus: () => request<InitStatusResponse>('/api/init/status'),
  setup: (payload: { username: string; password: string; email: string; display_name?: string }) =>
    request<{ token: string }>('/api/init/setup', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  getEncryptionKey: () => request<{ key: string }>('/api/init/encryption-key')
};
