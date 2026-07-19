export type DatabaseClientPlatform = 'windows' | 'macos' | 'linux';

export interface DatabaseClientSettings {
  client: '' | 'dbeaver';
  platform: DatabaseClientPlatform;
  executablePath: string;
}

export interface DBeaverConfigurationInput {
  platform: DatabaseClientPlatform;
  executablePath: string;
  protocol: string;
  host: string;
  port: number;
  username: string;
  databaseName?: string;
  connectionName?: string;
}

export const DATABASE_CLIENT_OPTIONS = [
  { label: 'DBeaver Desktop', value: 'dbeaver' },
] as const;

export const DATABASE_CLIENT_PLATFORM_OPTIONS = [
  { label: 'Windows', value: 'windows' },
  { label: 'macOS', value: 'macos' },
  { label: 'Linux', value: 'linux' },
] as const;

export function detectDatabaseClientPlatform(userAgent = navigator.userAgent): DatabaseClientPlatform {
  const normalized = userAgent.toLowerCase();
  if (normalized.includes('mac')) return 'macos';
  if (normalized.includes('linux')) return 'linux';
  return 'windows';
}

export function databaseClientExecutableExample(platform: DatabaseClientPlatform): string {
  if (platform === 'macos') return '/Applications/DBeaver.app/Contents/MacOS/dbeaver';
  if (platform === 'linux') return '/usr/bin/dbeaver-ce';
  return 'C:\\Program Files\\DBeaver\\dbeaverc.exe';
}

export function isValidDatabaseClientExecutablePath(
  path: string,
  platform: DatabaseClientPlatform,
): boolean {
  const value = path.trim();
  if (!value || /[\r\n\0]/.test(value)) return false;
  if (platform === 'windows') {
    return /^[A-Za-z]:[\\/](?:.*[\\/])?(?:dbeaver|dbeaverc)\.exe$/i.test(value);
  }
  if (!value.startsWith('/')) return false;
  return platform === 'macos'
    ? /\/dbeaver$/i.test(value)
    : /\/(?:dbeaver|dbeaver-ce)$/i.test(value);
}

export function buildDBeaverConfigurationCommand(
  input: DBeaverConfigurationInput,
): string {
  const {
    platform,
    executablePath,
    protocol,
    host,
    port,
    username,
    databaseName = defaultDatabaseName(protocol),
    connectionName = 'Jianmen 临时连接',
  } = input;

  if (!isValidDatabaseClientExecutablePath(executablePath, platform)) return '';
  if (!Number.isInteger(port) || port < 1 || port > 65535) return '';

  const values = {
    driver: databaseDriver(protocol),
    host: sanitizeDBeaverValue(host),
    port: String(port),
    database: sanitizeDBeaverValue(databaseName),
    user: sanitizeDBeaverValue(username),
    name: sanitizeDBeaverValue(connectionName),
  };
  if (!values.driver || !values.host || !values.user) return '';

  const connection = [
    `driver=${values.driver}`,
    `host=${values.host}`,
    `port=${values.port}`,
    `database=${values.database}`,
    `user=${values.user}`,
    `name=${values.name}`,
    'savePassword=false',
    'create=true',
    'save=false',
    'connect=false',
  ].join('|');

  if (platform === 'windows') {
    return `& ${quotePowerShell(executablePath.trim())} -con ${quotePowerShell(connection)}`;
  }
  return `${quotePOSIX(executablePath.trim())} -con ${quotePOSIX(connection)}`;
}

function defaultDatabaseName(protocol: string): string {
  return ['postgres', 'postgresql'].includes(protocol.trim().toLowerCase()) ? 'postgres' : '';
}

function databaseDriver(protocol: string): string {
  const normalized = protocol.trim().toLowerCase();
  if (normalized === 'postgres' || normalized === 'postgresql') return 'postgresql';
  if (normalized === 'mysql') return 'mysql';
  return '';
}

function sanitizeDBeaverValue(value: string): string {
  return String(value).replace(/[|\r\n\0]/g, ' ').trim();
}

function quotePowerShell(value: string): string {
  return `'${value.replace(/'/g, "''")}'`;
}

function quotePOSIX(value: string): string {
  return `'${value.replace(/'/g, "'\"'\"'")}'`;
}
