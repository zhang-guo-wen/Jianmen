const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '');
const TOKEN_KEY = 'jianmen_token';

export interface ApiEnvelope<T> {
  data?: T;
  error?: string;
  message?: string;
}

export interface HealthResponse {
  status?: string;
  version?: string;
  time?: string;
  [key: string]: unknown;
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
}

export interface MyMenusResponse {
  menus: string[];
}

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
  disabled?: boolean;
  expires_at?: string;
  host?: string;
  port?: number;
  username?: string;
  auth_methods?: string[];
  auth_type?: string;
  password?: string;
  private_key_path?: string;
  private_key_pem?: string;
  passphrase?: string;
  insecure_ignore_host_key?: boolean;
  host_key_fingerprint?: string;
  known_hosts_path?: string;
  address?: string;
  status?: string;
  source?: string;
  static?: boolean;
  readonly?: boolean;
  deletable?: boolean;
  [key: string]: unknown;
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

export interface HostRecord {
  id?: string;
  name?: string;
  group?: string;
  host?: string;
  port?: number;
  remark?: string;
  disabled?: boolean;
  status?: string;
  account_count?: number;
  static?: boolean;
  [key: string]: unknown;
}

export interface HostPayload {
  id?: string;
  name: string;
  group?: string;
  host: string;
  port: number;
  remark?: string;
  disabled?: boolean;
}

export interface PagedHostRecord {
  data?: HostRecord[];
  page?: number;
  page_size?: number;
  total?: number;
}

export interface SessionRecord {
  id?: string | number;
  user?: string;
  user_id?: string;
  user_username?: string;
  target?: string;
  target_id?: string;
  client_ip?: string;
  status?: string;
  state?: string;
  startedAt?: string;
  started_at?: string;
  ended_at?: string;
  path?: string;
  has_replay?: boolean;
  replay_size?: number;
  duration_seconds?: number;
  protocol?: string;
  protocol_subtype?: string;
  [key: string]: unknown;
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
  user?: string;
  target?: string;
  client_ip?: string;
  started_at?: string;
  ended_at?: string;
  [key: string]: unknown;
}

export interface SessionCommandRecord {
  seq?: number;
  command?: string;
  preview?: string;
  confidence?: string;
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
  started_at?: string;
  ended_at?: string;
  size?: number;
  [key: string]: unknown;
}

export interface DBConnectionRecord {
  id?: string;
  name?: string;
  protocol?: string;
  client_addr?: string;
  upstream_addr?: string;
  started_at?: string;
  path?: string;
  [key: string]: unknown;
}

export interface DBInstanceRecord {
  id?: string;
  name?: string;
  protocol?: string;
  address?: string;
  group_name?: string;
  remark?: string;
  disabled?: boolean;
  account_count?: number;
  created_at?: string;
  updated_at?: string;
}

export interface DBAccountRecord {
  id?: string;
  instance_id?: string;
  unique_name?: string;
  upstream_username?: string;
  group_name?: string;
  remark?: string;
  expires_at?: string;
  disabled?: boolean;
  resource_id?: string;
  resource_seq?: number;
  created_at?: string;
  updated_at?: string;
}

export interface DBInstancePayload {
  name: string;
  protocol: string;
  address: string;
  group_name?: string;
  remark?: string;
}

export interface DBAccountPayload {
  upstream_username: string;
  upstream_password: string;
  group_name?: string;
  remark?: string;
  expires_at?: string;
}

export interface DBAccountUpdatePayload {
  upstream_username: string;
  upstream_password?: string;
  group_name?: string;
  remark?: string;
  expires_at?: string;
  disabled?: boolean;
}

export interface DBConnectionMetaRecord extends DBConnectionRecord {
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

export interface TestConnectionResult {
  ok: boolean;
  message: string;
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

  const contentType = response.headers.get('content-type') ?? '';
  const payload =
    response.status === 204
      ? undefined
      : contentType.includes('application/json')
        ? await response.json()
        : await response.text();

  if (!response.ok) {
    const message =
      typeof payload === 'object' && payload !== null && 'error' in payload
        ? String(payload.error)
        : typeof payload === 'object' && payload !== null && 'message' in payload
        ? String(payload.message)
        : response.statusText;
    throw new Error(message || `Request failed with ${response.status}`);
  }

  return payload as T;
}

export const apiClient = {
  getHealth: () => request<HealthResponse>('/api/health'),
  getUsers: () => request<ApiEnvelope<UserRecord[]> | UserRecord[]>('/api/users'),
  createUser: (payload: UserPayload) =>
    request<ApiEnvelope<{ user: UserRecord; token: string }> | { user: UserRecord; token: string }>('/api/users', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  updateUser: (id: string | number, payload: UserPayload) =>
    request<ApiEnvelope<UserRecord> | UserRecord>(`/api/users/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  deleteUser: (id: string | number) =>
    request<ApiEnvelope<unknown> | unknown>(`/api/users/${encodeURIComponent(String(id))}`, {
      method: 'DELETE',
    }),
  getMyMenus: () =>
    request<MyMenusResponse>('/api/me/menus'),
  getMyPermissions: () =>
    request<{ actions: string[] }>('/api/me/permissions'),
  getHosts: (params: { page?: number; page_size?: number; q?: string } = {}) => {
    const search = new URLSearchParams();
    if (params.page) {
      search.set('page', String(params.page));
    }
    if (params.page_size) {
      search.set('page_size', String(params.page_size));
    }
    if (params.q) {
      search.set('q', params.q);
    }
    const suffix = search.toString() ? `?${search.toString()}` : '';
    return request<PagedHostRecord | ApiEnvelope<HostRecord[]> | HostRecord[]>(`/api/hosts${suffix}`);
  },
  createHost: (payload: HostPayload) =>
    request<ApiEnvelope<HostRecord> | HostRecord>('/api/hosts', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateHost: (id: string | number, payload: HostPayload) =>
    request<ApiEnvelope<HostRecord> | HostRecord>(`/api/hosts/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteHost: (id: string | number) =>
    request<ApiEnvelope<unknown> | unknown>(`/api/hosts/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),
  getHostAccounts: (id: string | number) =>
    request<ApiEnvelope<TargetRecord[]> | TargetRecord[]>(`/api/hosts/${encodeURIComponent(String(id))}/accounts`),
  getTargets: () => request<ApiEnvelope<TargetRecord[]> | TargetRecord[]>('/api/targets'),
  getTarget: (id: string | number) =>
    request<ApiEnvelope<TargetRecord> | TargetRecord>(`/api/targets/${encodeURIComponent(String(id))}`),
  createTarget: (payload: TargetPayload) =>
    request<ApiEnvelope<TargetRecord> | TargetRecord>('/api/targets', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateTarget: (id: string | number, payload: TargetPayload) =>
    request<ApiEnvelope<TargetRecord> | TargetRecord>(`/api/targets/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteTarget: (id: string | number) =>
    request<ApiEnvelope<unknown> | unknown>(`/api/targets/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),
  testTargetConnection: (payload: TargetPayload) =>
    request<TestConnectionResult>('/api/targets/test-connection', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  createUserSession: (targetId: string) =>
    request<UserSessionRecord>('/api/user-sessions', {
      method: 'POST',
      body: JSON.stringify({ target_id: targetId })
    }),
  getSessions: () => request<ApiEnvelope<SessionRecord[]> | SessionRecord[]>('/api/sessions'),
  getSessionMeta: (id: string | number) =>
    request<ApiEnvelope<SessionMetaRecord> | SessionMetaRecord>(
      `/api/sessions/${encodeURIComponent(String(id))}/meta`
    ),
  getSessionCommands: (id: string | number) =>
    request<ApiEnvelope<SessionCommandRecord[]> | SessionCommandRecord[]>(
      `/api/sessions/${encodeURIComponent(String(id))}/commands`
    ),
  getSessionFiles: (id: string | number) =>
    request<ApiEnvelope<SessionFileEventRecord[]> | SessionFileEventRecord[]>(
      `/api/sessions/${encodeURIComponent(String(id))}/files`
    ),
  getSessionFileSummary: (id: string | number) =>
    request<ApiEnvelope<Record<string, unknown>> | Record<string, unknown>>(
      `/api/sessions/${encodeURIComponent(String(id))}/file-summary`
    ),
  getSessionReplay: (id: string | number) =>
    request<string>(`/api/sessions/${encodeURIComponent(String(id))}/replay`),
  getDBInstances: () => request<ApiEnvelope<DBInstanceRecord[]> | DBInstanceRecord[]>('/api/db/instances'),
  createDBInstance: (payload: DBInstancePayload) =>
    request<ApiEnvelope<DBInstanceRecord> | DBInstanceRecord>('/api/db/instances', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateDBInstance: (id: string, payload: DBInstancePayload & { disabled?: boolean }) =>
    request<ApiEnvelope<DBInstanceRecord> | DBInstanceRecord>(`/api/db/instances/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteDBInstance: (id: string) =>
    request<ApiEnvelope<unknown> | unknown>(`/api/db/instances/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),
  getDBAccounts: (instanceID: string, params?: { page?: number; size?: number; search?: string }) => {
    let path = `/api/db/instances/${encodeURIComponent(instanceID)}/accounts`;
    if (params) {
      const searchParams = new URLSearchParams();
      if (params.page !== undefined) searchParams.set('page', String(params.page));
      if (params.size !== undefined) searchParams.set('size', String(params.size));
      if (params.search !== undefined) searchParams.set('search', params.search);
      const suffix = searchParams.toString();
      if (suffix) path += '?' + suffix;
    }
    return request<ApiEnvelope<DBAccountRecord[]> | DBAccountRecord[]>(path);
  },
  createDBAccount: (instanceID: string, payload: DBAccountPayload) =>
    request<ApiEnvelope<DBAccountRecord> | DBAccountRecord>(
      `/api/db/instances/${encodeURIComponent(instanceID)}/accounts`,
      {
        method: 'POST',
        body: JSON.stringify(payload)
      }
    ),
  getDBAccount: (id: string) =>
    request<ApiEnvelope<DBAccountRecord> | DBAccountRecord>(`/api/db/accounts/${encodeURIComponent(id)}`),
  updateDBAccount: (id: string, payload: DBAccountUpdatePayload) =>
    request<ApiEnvelope<DBAccountRecord> | DBAccountRecord>(`/api/db/accounts/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteDBAccount: (id: string) =>
    request<ApiEnvelope<unknown> | unknown>(`/api/db/accounts/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),
  testDBConnection: (id: string) =>
    request<ApiEnvelope<{ ok: boolean; error?: string; latency_ms: number }>>(
      `/api/db/accounts/test/${encodeURIComponent(id)}`,
      { method: 'POST' }
    ),
  getDBConnections: () =>
    request<ApiEnvelope<DBConnectionRecord[]> | DBConnectionRecord[]>('/api/db/connections'),
  getDBConnectionMeta: (id: string | number) =>
    request<ApiEnvelope<DBConnectionMetaRecord> | DBConnectionMetaRecord>(
      `/api/db/connections/${encodeURIComponent(String(id))}/meta`
    ),
  getDBConnectionQueries: (id: string | number) =>
    request<ApiEnvelope<DBQueryEventRecord[]> | DBQueryEventRecord[]>(
      `/api/db/connections/${encodeURIComponent(String(id))}/queries`
    ),
  getRBACRoles: () => request<ApiEnvelope<RBACRoleRecord[]> | RBACRoleRecord[]>('/api/rbac/roles'),
  createRBACRole: (payload: RBACRolePayload) =>
    request<ApiEnvelope<RBACRoleRecord> | RBACRoleRecord>('/api/rbac/roles', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateRBACRole: (id: string | number, payload: RBACRolePayload) =>
    request<ApiEnvelope<RBACRoleRecord> | RBACRoleRecord>(`/api/rbac/roles/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteRBACRole: (id: string | number) =>
    request<ApiEnvelope<unknown> | unknown>(`/api/rbac/roles/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),
  getRBACPermissions: () =>
    request<ApiEnvelope<RBACPermissionRecord[]> | RBACPermissionRecord[]>('/api/rbac/permissions'),
  createRBACPermission: (payload: RBACPermissionPayload) =>
    request<ApiEnvelope<RBACPermissionRecord> | RBACPermissionRecord>('/api/rbac/permissions', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  deleteRBACPermission: (id: string | number) =>
    request<ApiEnvelope<unknown> | unknown>(
      `/api/rbac/permissions/${encodeURIComponent(String(id))}`,
      {
        method: 'DELETE'
      }
    ),
  getRBACUserRoles: () =>
    request<ApiEnvelope<RBACUserRoleRecord[]> | RBACUserRoleRecord[]>('/api/rbac/user-roles'),
  createRBACUserRole: (payload: RBACUserRolePayload) =>
    request<ApiEnvelope<RBACUserRoleRecord> | RBACUserRoleRecord>('/api/rbac/user-roles', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  deleteRBACUserRole: (id: string | number) =>
    request<ApiEnvelope<unknown> | unknown>(`/api/rbac/user-roles/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),
  getRBACRolePermissions: () =>
    request<ApiEnvelope<RBACRolePermissionRecord[]> | RBACRolePermissionRecord[]>(
      '/api/rbac/role-permissions'
    ),
  createRBACRolePermission: (payload: RBACRolePermissionPayload) =>
    request<ApiEnvelope<RBACRolePermissionRecord> | RBACRolePermissionRecord>(
      '/api/rbac/role-permissions',
      {
        method: 'POST',
        body: JSON.stringify(payload)
      }
    ),
  deleteRBACRolePermission: (id: string | number) =>
    request<ApiEnvelope<unknown> | unknown>(
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
    return request<ApiEnvelope<RBACEffectiveCheckResult> | RBACEffectiveCheckResult>(
      `/api/rbac/effective?${params.toString()}`
    );
  },
  login: (username: string, password: string) =>
    request<{ token: string }>('/api/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  getInitStatus: () => request<{ initialized: boolean }>('/api/init/status'),
  setup: (payload: { username: string; password: string; email: string; display_name?: string }) =>
    request<{ token: string }>('/api/init/setup', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  getEncryptionKey: () => request<{ key: string }>('/api/init/encryption-key')
};
