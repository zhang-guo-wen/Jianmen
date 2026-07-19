import type { DatabaseGatewayMode } from '../api/systemSettings';

export interface DatabaseGatewayTLSIdentity {
  enabled: boolean;
  tls_enabled: boolean;
  tls_server_name?: string;
  tls_ca_pem?: string;
  tls_cert_sha256?: string;
}

export interface DatabaseGatewayConnectionInput {
  protocol: string;
  gateway: DatabaseGatewayTLSIdentity | null | undefined;
  port: number;
  username: string;
  databaseName?: string;
}

export const DATABASE_COMMAND_PLATFORM = 'Linux/macOS/Git Bash' as const;
export const REDIS_COMMAND_UNAVAILABLE_REASON = 'redis-cli 无法验证主机名，安全命令暂不可用';
export const DEFAULT_MYSQL_DETECTION_DELAY_MS = 200;

export interface DatabaseGatewayEndpoint {
  mode?: DatabaseGatewayMode;
  port?: number;
  mysql_detection_delay_ms?: number;
}

export interface DatabaseGatewayConnection {
  command: string | null;
  commandPlatform: typeof DATABASE_COMMAND_PLATFORM | null;
  unavailableReason: string | null;
  caFileName: string;
  caFilePath: string;
}

export function hasDatabaseGatewayTLSIdentity(gateway: DatabaseGatewayTLSIdentity | null | undefined): gateway is DatabaseGatewayTLSIdentity & Required<Pick<DatabaseGatewayTLSIdentity, 'tls_server_name' | 'tls_ca_pem' | 'tls_cert_sha256'>> {
  return Boolean(
    gateway?.enabled &&
    gateway.tls_enabled &&
    gateway.tls_server_name?.trim() &&
    gateway.tls_ca_pem?.trim() &&
    gateway.tls_cert_sha256?.trim(),
  );
}

export function databaseGatewayCAFileName(protocol: string): string {
  return `jianmen-${normalizedProtocol(protocol)}-gateway-ca.pem`;
}

export function databaseGatewayCAFilePath(protocol: string): string {
  return `./${databaseGatewayCAFileName(protocol)}`;
}

export function databaseGatewayFallbackPort(
  protocol: string,
  mode: DatabaseGatewayMode | undefined,
): number {
  if (mode !== 'independent') return 33060;
  const normalized = normalizedProtocol(protocol);
  if (normalized === 'postgres') return 33062;
  if (normalized === 'redis') return 33063;
  return 33061;
}

export function resolveDatabaseGatewayPort(
  protocol: string,
  gateway: DatabaseGatewayEndpoint | null | undefined,
): number {
  const port = gateway?.port;
  if (Number.isInteger(port) && Number(port) >= 1 && Number(port) <= 65535) {
    return Number(port);
  }
  return databaseGatewayFallbackPort(protocol, gateway?.mode);
}

export function unifiedMySQLDetectionNotice(
  protocol: string,
  gateway: DatabaseGatewayEndpoint | null | undefined,
): string {
  if (normalizedProtocol(protocol) !== 'mysql' || gateway?.mode === 'independent') return '';
  const configuredDelay = gateway?.mysql_detection_delay_ms;
  const delay = Number.isInteger(configuredDelay) && Number(configuredDelay) > 0
    ? Number(configuredDelay)
    : DEFAULT_MYSQL_DETECTION_DELAY_MS;
  return `统一入口需要短暂等待以识别连接协议，MySQL 每次连接会增加约 ${delay}ms 建连时间。`;
}

export function buildDatabaseGatewayConnection(input: DatabaseGatewayConnectionInput): DatabaseGatewayConnection | null {
  const protocol = normalizedProtocol(input.protocol);
  if (!protocol || !hasDatabaseGatewayTLSIdentity(input.gateway) || !isValidPort(input.port)) return null;

  const caFileName = databaseGatewayCAFileName(protocol);
  const caFilePath = databaseGatewayCAFilePath(protocol);
  if (protocol === 'redis') {
    return {
      command: null,
      commandPlatform: null,
      unavailableReason: REDIS_COMMAND_UNAVAILABLE_REASON,
      caFileName,
      caFilePath,
    };
  }

  const databaseName = input.databaseName ?? 'postgres';
  if (
    !isSafeDatabaseCommandValue('host', input.gateway.tls_server_name) ||
    !isSafeDatabaseCommandValue('username', input.username) ||
    (protocol === 'postgres' && !isSafeDatabaseCommandValue('databaseName', databaseName))
  ) return null;

  if (protocol === 'postgres') {
    const conninfo = [
      `host=${libpqQuote(input.gateway.tls_server_name)}`,
      `port=${input.port}`,
      `user=${libpqQuote(input.username)}`,
      `dbname=${libpqQuote(databaseName)}`,
      'sslmode=verify-full',
      `sslrootcert=${libpqQuote(caFilePath)}`,
      'gssencmode=disable',
    ].join(' ');
    return {
      command: `psql ${shellQuote(conninfo)}`,
      commandPlatform: DATABASE_COMMAND_PLATFORM,
      unavailableReason: null,
      caFileName,
      caFilePath,
    };
  }

  if (protocol === 'mysql') {
    return {
      command: `mysql --protocol=tcp --ssl-mode=VERIFY_IDENTITY --ssl-ca=${shellQuote(caFilePath)} -h ${shellQuote(input.gateway.tls_server_name)} -P ${input.port} -u ${shellQuote(input.username)} -p`,
      commandPlatform: DATABASE_COMMAND_PLATFORM,
      unavailableReason: null,
      caFileName,
      caFilePath,
    };
  }

  return null;
}

export function isSafeDatabaseCommandValue(
  kind: 'host' | 'username' | 'databaseName',
  value: string,
): boolean {
  if (!value) return false;
  if (kind === 'host') return /^[A-Za-z0-9._:-]+$/.test(value);
  if (kind === 'username') return /^[A-Za-z0-9._@:+-]+$/.test(value);
  return /^[A-Za-z0-9_-]+$/.test(value);
}

function normalizedProtocol(protocol: string): 'mysql' | 'postgres' | 'redis' | '' {
  const value = protocol.trim().toLowerCase();
  if (value === 'postgresql') return 'postgres';
  return value === 'mysql' || value === 'postgres' || value === 'redis' ? value : '';
}

function isValidPort(port: number): boolean {
  return Number.isInteger(port) && port >= 1 && port <= 65535;
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\"'\"'")}'`;
}

function libpqQuote(value: string): string {
  return `'${value.replace(/\\/g, '\\\\').replace(/'/g, "\\'")}'`;
}
