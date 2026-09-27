import type {
  DatabaseGatewayClientTLSMode,
  DatabaseGatewayMode,
  DatabaseGatewayTLSTrustMode,
} from '../api/systemSettings';
import { DATABASE_CLIENT_CA_FILE_NAME } from '../config/databaseClients';

export interface DatabaseGatewayTLSIdentity {
  enabled: boolean;
  host?: string;
  client_tls_mode?: DatabaseGatewayClientTLSMode;
  tls_enabled: boolean;
  tls_trust_mode?: DatabaseGatewayTLSTrustMode;
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
  useTLS?: boolean;
}

export const DATABASE_COMMAND_PLATFORM = 'Linux/macOS/Git Bash' as const;
export const REDIS_COMMAND_UNAVAILABLE_REASON = 'redis-cli 无法验证主机名，安全命令暂不可用';

export interface DatabaseGatewayEndpoint {
  mode?: DatabaseGatewayMode;
  port?: number;
}

export interface DatabaseGatewayConnection {
  command: string | null;
  commandPlatform: typeof DATABASE_COMMAND_PLATFORM | null;
  unavailableReason: string | null;
  caFileName: string;
  caFilePath: string;
  usesTLS: boolean;
}

export function databaseGatewayTLSTrustMode(
  gateway: DatabaseGatewayTLSIdentity | null | undefined,
): DatabaseGatewayTLSTrustMode | null {
  return gateway?.tls_trust_mode === 'system' || gateway?.tls_trust_mode === 'custom'
    ? gateway.tls_trust_mode
    : null;
}

export function databaseGatewayRequiresCustomCA(
  gateway: DatabaseGatewayTLSIdentity | null | undefined,
): boolean {
  return databaseGatewayTLSTrustMode(gateway) !== 'system';
}

export function hasDatabaseGatewayTLSIdentity(gateway: DatabaseGatewayTLSIdentity | null | undefined): gateway is DatabaseGatewayTLSIdentity & Required<Pick<DatabaseGatewayTLSIdentity, 'tls_server_name' | 'tls_cert_sha256'>> {
  return Boolean(
    gateway?.enabled &&
    gateway.tls_enabled &&
    gateway.tls_server_name?.trim() &&
    gateway.tls_cert_sha256?.trim() &&
    databaseGatewayTLSTrustMode(gateway) !== null &&
    (
      databaseGatewayTLSTrustMode(gateway) === 'system'
      || gateway.tls_ca_pem?.trim()
    ),
  );
}

export function databaseGatewayCAFileName(protocol: string): string {
  void protocol;
  return DATABASE_CLIENT_CA_FILE_NAME;
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

export function resolveDatabaseGatewayClientHost(
  listenerHost: string | null | undefined,
  pageHost: string,
): string {
  const configured = listenerHost?.trim() ?? '';
  const fallback = pageHost.trim().replace(/^\[|\]$/g, '');
  if (!configured) return fallback;

  const normalizedConfigured = normalizeNetworkHost(configured);
  if (normalizedConfigured === '0.0.0.0' || normalizedConfigured === '::') {
    return fallback || configured;
  }
  if (
    fallback
    && isLoopbackHost(normalizedConfigured)
    && !isLoopbackHost(normalizeNetworkHost(fallback))
  ) {
    return fallback;
  }
  return configured;
}

export function buildDatabaseGatewayConnection(input: DatabaseGatewayConnectionInput): DatabaseGatewayConnection | null {
  const protocol = normalizedProtocol(input.protocol);
  const gateway = input.gateway;
  if (!protocol || !gateway?.enabled || !isValidPort(input.port)) return null;

  const caFileName = databaseGatewayCAFileName(protocol);
  const caFilePath = databaseGatewayCAFilePath(protocol);
  const useTLS = gateway.client_tls_mode === 'required' || input.useTLS === true;
  if (useTLS && !hasDatabaseGatewayTLSIdentity(gateway)) return null;
  const host = useTLS ? gateway.tls_server_name : gateway.host;
  if (
    !host
    || !isSafeDatabaseCommandValue('host', host)
    || !isSafeDatabaseCommandValue('username', input.username)
  ) return null;

  if (protocol === 'redis' && useTLS) {
    return {
      command: null,
      commandPlatform: null,
      unavailableReason: REDIS_COMMAND_UNAVAILABLE_REASON,
      caFileName,
      caFilePath,
      usesTLS: true,
    };
  }

  const databaseName = input.databaseName ?? 'postgres';
  if (
    (protocol === 'postgres' && !isSafeDatabaseCommandValue('databaseName', databaseName))
  ) return null;

  if (protocol === 'postgres') {
    const rootCertificateArgument = databaseGatewayTLSTrustMode(gateway) === 'system'
      ? 'sslrootcert=system'
      : `sslrootcert=${libpqQuote(caFilePath)}`;
    const conninfo = [
      `host=${libpqQuote(host)}`,
      `port=${input.port}`,
      `user=${libpqQuote(input.username)}`,
      `dbname=${libpqQuote(databaseName)}`,
      useTLS ? 'sslmode=verify-full' : 'sslmode=disable',
      ...(useTLS ? [rootCertificateArgument] : []),
      'gssencmode=disable',
    ].join(' ');
    return {
      command: `psql ${shellQuote(conninfo)}`,
      commandPlatform: DATABASE_COMMAND_PLATFORM,
      unavailableReason: null,
      caFileName,
      caFilePath,
      usesTLS: useTLS,
    };
  }

  if (protocol === 'mysql') {
    if (useTLS && !gateway.tls_ca_pem?.trim()) return null;
    return {
      command: useTLS
        ? `mysql --protocol=tcp --ssl-mode=VERIFY_IDENTITY --ssl-ca=${shellQuote(caFilePath)} -h ${shellQuote(host)} -P ${input.port} -u ${shellQuote(input.username)} -p`
        : `mysql --protocol=tcp --ssl-mode=DISABLED -h ${shellQuote(host)} -P ${input.port} -u ${shellQuote(input.username)} -p`,
      commandPlatform: DATABASE_COMMAND_PLATFORM,
      unavailableReason: null,
      caFileName,
      caFilePath,
      usesTLS: useTLS,
    };
  }

  if (protocol === 'redis') {
    return {
      command: `redis-cli -h ${shellQuote(host)} -p ${input.port} --user ${shellQuote(input.username)} --askpass`,
      commandPlatform: DATABASE_COMMAND_PLATFORM,
      unavailableReason: null,
      caFileName,
      caFilePath,
      usesTLS: false,
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

function normalizeNetworkHost(host: string): string {
  return host.trim().replace(/^\[|\]$/g, '').toLowerCase();
}

function isLoopbackHost(host: string): boolean {
  return host === 'localhost'
    || host === '::1'
    || /^127(?:\.\d{1,3}){3}$/.test(host);
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\"'\"'")}'`;
}

function libpqQuote(value: string): string {
  return `'${value.replace(/\\/g, '\\\\').replace(/'/g, "\\'")}'`;
}
