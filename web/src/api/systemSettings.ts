export const BYTES_PER_GIB = 1024 ** 3;
export const BYTES_PER_MIB = 1024 ** 2;
export const DATABASE_MAX_CLIENT_MESSAGE_BYTES_MIN = 64 * 1024;
export const DATABASE_MAX_CLIENT_MESSAGE_BYTES_MAX = 16 * BYTES_PER_MIB;
export const DATABASE_MAX_CLIENT_MESSAGE_BYTES_DEFAULT = 10 * BYTES_PER_MIB;

export type DatabaseGatewayMode = 'unified' | 'independent';

export interface SystemSettingsValues {
  database_gateway_mode: DatabaseGatewayMode;
  web_rdp_enabled: boolean;
  web_rdp_connect_timeout_seconds: number;
  web_rdp_allow_unrecorded: boolean;
  database_max_client_message_bytes: number;
  recording_enabled: boolean;
  recording_record_input: boolean;
  recording_record_commands: boolean;
  recording_retention_days: number;
  recording_max_replay_bytes: number;
  recording_cleanup_batch_size: number;
}

export const SYSTEM_SETTINGS_FIELDS = [
  'database_gateway_mode',
  'web_rdp_enabled',
  'web_rdp_connect_timeout_seconds',
  'web_rdp_allow_unrecorded',
  'database_max_client_message_bytes',
  'recording_enabled',
  'recording_record_input',
  'recording_record_commands',
  'recording_retention_days',
  'recording_max_replay_bytes',
  'recording_cleanup_batch_size',
] as const satisfies ReadonlyArray<keyof SystemSettingsValues>;

export interface SystemSettingsGuacdInfrastructure {
  address: string;
}

export interface SystemSettingsDirectoryInfrastructure {
  spool_dir: string;
  guacd_recording_root: string;
  local_drive_root: string;
  guacd_drive_root: string;
  replay_dir: string;
}

export interface SystemSettingsObjectStorageInfrastructure {
  provider: string;
  local_dir: string;
  endpoint: string;
  bucket: string;
  region: string;
  prefix: string;
  secure: boolean;
  path_style: boolean;
  auto_create_bucket: boolean;
  access_key_id_configured: boolean;
  secret_access_key_configured: boolean;
  session_token_configured: boolean;
  credentials_configured: boolean;
}

export interface SystemSettingsInfrastructure {
  guacd: SystemSettingsGuacdInfrastructure;
  directories: SystemSettingsDirectoryInfrastructure;
  object_storage: SystemSettingsObjectStorageInfrastructure;
}

export interface SystemSettingsState {
  desired: SystemSettingsValues;
  effective: SystemSettingsValues;
  revision: number;
  effective_revision: number;
  pending_restart: boolean;
  updated_by_id?: string;
  updated_by_username?: string;
  updated_at?: string;
  applied_at?: string;
  infrastructure: SystemSettingsInfrastructure;
}

export interface SystemSettingsUpdatePayload {
  settings: SystemSettingsValues;
  expected_revision: number;
  confirm_risk: boolean;
}

export interface SystemSettingsDiagnosticResult {
  ok: boolean;
  message: string;
  latency_ms: number;
}

export interface SystemSettingsRevision {
  id?: string;
  revision: number;
  snapshot?: SystemSettingsValues;
  changed_fields: string[];
  updated_by_id?: string;
  updated_by_username?: string;
  actor_username?: string;
  created_at: string;
}

export interface SystemSettingsRevisionResponse {
  items: SystemSettingsRevision[];
  total?: number;
}

export function replayBytesToGiB(bytes: number): number {
  if (!Number.isFinite(bytes) || bytes <= 0) return 0;
  return Number((bytes / BYTES_PER_GIB).toFixed(2));
}

export function replayGiBToBytes(gib: number): number {
  if (!Number.isFinite(gib) || gib <= 0) return 0;
  return Math.round(gib * BYTES_PER_GIB);
}

export function changedSystemSettingsFields(
  current: SystemSettingsValues,
  next: SystemSettingsValues,
): Array<keyof SystemSettingsValues> {
  return SYSTEM_SETTINGS_FIELDS.filter(field => current[field] !== next[field]);
}

export function clientMessageBytesToMiB(bytes: number): number {
  if (!Number.isFinite(bytes) || bytes <= 0) return 0;
  return Number((bytes / BYTES_PER_MIB).toFixed(4));
}

export function clientMessageMiBToBytes(mib: number): number {
  if (!Number.isFinite(mib) || mib <= 0) return 0;
  return Math.round(mib * BYTES_PER_MIB);
}

export function formatClientMessageBytes(bytes: number): string {
  if (!Number.isSafeInteger(bytes) || bytes <= 0) return '0 MiB';
  const mib = clientMessageBytesToMiB(bytes);
  const roundedBytes = clientMessageMiBToBytes(mib);
  return roundedBytes === bytes
    ? `${mib} MiB`
    : `${mib} MiB（${bytes} 字节）`;
}

export function weakerProtectionReasons(
  current: SystemSettingsValues,
  next: SystemSettingsValues,
): string[] {
  const reasons: string[] = [];

  if (!current.web_rdp_allow_unrecorded && next.web_rdp_allow_unrecorded) {
    reasons.push('允许录制失败时继续建立 Web RDP 会话');
  }
  if (current.recording_enabled && !next.recording_enabled) {
    reasons.push('关闭会话录制');
  }
  if (!current.recording_record_input && next.recording_record_input) {
    reasons.push('开启原始输入记录（可能包含敏感信息）');
  }
  if (current.recording_record_commands && !next.recording_record_commands) {
    reasons.push('关闭命令记录');
  }
  if (next.recording_retention_days < current.recording_retention_days) {
    reasons.push(`审计保留期从 ${current.recording_retention_days} 天缩短为 ${next.recording_retention_days} 天`);
  }
  if (
    (current.recording_max_replay_bytes === 0 && next.recording_max_replay_bytes > 0)
    || (
      current.recording_max_replay_bytes > 0
      && next.recording_max_replay_bytes > 0
      && next.recording_max_replay_bytes < current.recording_max_replay_bytes
    )
  ) {
    reasons.push('降低本地回放容量上限，可能触发更积极的旧录像清理');
  }
  if (
    current.database_max_client_message_bytes
    !== next.database_max_client_message_bytes
  ) {
    reasons.push(
      `数据库与 Redis 客户端报文上限从 ${
        formatClientMessageBytes(current.database_max_client_message_bytes)
      } 调整为 ${
        formatClientMessageBytes(next.database_max_client_message_bytes)
      }`,
    );
  }

  return reasons;
}
