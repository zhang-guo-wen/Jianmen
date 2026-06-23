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
  [key: string]: unknown;
}

export interface UserRecord {
  id?: string | number;
  username?: string;
  name?: string;
  role?: string;
  status?: string;
  [key: string]: unknown;
}

export interface TargetRecord {
  id?: string | number;
  name?: string;
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
  name: string;
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

export interface SessionRecord {
  id?: string | number;
  user?: string;
  target?: string;
  status?: string;
  startedAt?: string;
  [key: string]: unknown;
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
  getSessions: () => request<ApiEnvelope<SessionRecord[]> | SessionRecord[]>('/api/sessions')
};
