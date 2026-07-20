export type DatabaseClientPlatform = 'windows' | 'macos' | 'linux';

export interface DatabaseClientSettings {
  client: '' | 'dbeaver';
  platform: DatabaseClientPlatform;
  executablePath: string;
  protocolRegistered: boolean;
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

export interface DatabaseClientLaunchInput {
  protocol: string;
  host: string;
  port: number;
  username: string;
  databaseName?: string;
  connectionName?: string;
}

interface DatabaseClientLaunchPayload {
  v: 1;
  driver: 'mysql' | 'postgresql';
  host: string;
  port: number;
  database: string;
  user: string;
  name: string;
  tls: 'verify-full';
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
  if (!value || value.length > 1024 || /[\r\n\0]/.test(value)) return false;
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

  const payload = normalizeLaunchPayload({
    protocol,
    host,
    port,
    username,
    databaseName,
    connectionName,
  });
  if (!isValidDatabaseClientExecutablePath(executablePath, platform) || !payload) return '';

  const connection = [
    ...dbeaverConnectionFields(payload),
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

export function buildDatabaseProtocolURL(input: DatabaseClientLaunchInput): string {
  const payload = normalizeLaunchPayload(input);
  if (!payload) return '';
  return `jianmen-db://connect/${encodeBase64URL(JSON.stringify(payload))}`;
}

export function buildDatabaseProtocolRegistrationCommand(
  settings: DatabaseClientSettings,
): string {
  if (
    settings.client !== 'dbeaver'
    || settings.platform !== 'windows'
    || !isValidDatabaseClientExecutablePath(settings.executablePath, 'windows')
  ) {
    return '';
  }

  const executable = quotePowerShellLiteral(settings.executablePath.trim());
  const script = String.raw`param(
  [Parameter(Mandatory=$true, Position=0)]
  [string]$JianmenUrl
)
$ErrorActionPreference = 'Stop'
try {
  if ($args.Count -ne 0) { exit 1 }
  $uri = [Uri]$JianmenUrl
  if ($uri.Scheme -cne 'jianmen-db' -or $uri.Host -cne 'connect') { exit 2 }
  $encoded = $uri.AbsolutePath.Trim('/')
  if ($encoded -notmatch '^[A-Za-z0-9_-]{1,4096}$') { exit 3 }
  $encoded = $encoded.Replace('-', '+').Replace('_', '/')
  while (($encoded.Length % 4) -ne 0) { $encoded += '=' }
  $bytes = [Convert]::FromBase64String($encoded)
  if ($bytes.Length -gt 3072) { exit 4 }
  $data = [Text.Encoding]::UTF8.GetString($bytes) | ConvertFrom-Json
  $allowed = @('v', 'driver', 'host', 'port', 'database', 'user', 'name', 'tls')
  $properties = @($data.PSObject.Properties.Name)
  if ($properties.Count -ne $allowed.Count -or @($properties | Where-Object { $_ -notin $allowed }).Count -ne 0) { exit 5 }
  if ([string]$data.v -cne '1' -or [string]$data.tls -cne 'verify-full') { exit 6 }
  if ([string]$data.driver -cnotin @('mysql', 'postgresql')) { exit 7 }
  if ([string]$data.host -notmatch '^[A-Za-z0-9._:\[\]-]{1,255}$') { exit 8 }
  if ([string]$data.user -notmatch '^[\p{L}\p{N}._@+-]{1,256}$') { exit 9 }
  if ([string]$data.database -notmatch '^[\p{L}\p{N} ._-]{0,128}$') { exit 10 }
  if ([string]$data.name -notmatch "^[\p{L}\p{N} ._()'/-]{1,128}$") { exit 11 }
  $port = 0
  if (-not [int]::TryParse([string]$data.port, [ref]$port) -or $port -lt 1 -or $port -gt 65535) { exit 12 }
  $parts = @(
    "driver=$($data.driver)",
    "host=$($data.host)",
    "port=$port",
    "database=$($data.database)",
    "user=$($data.user)",
    "name=$($data.name)",
    'savePassword=false',
    'create=true',
    'save=false',
    'connect=false'
  )
  if ([string]$data.driver -ceq 'mysql') {
    $parts += 'prop.sslMode=VERIFY_IDENTITY'
  } else {
    $parts += @('prop.ssl=true', 'prop.sslmode=verify-full')
  }
  $connection = $parts -join '|'
  Start-Process -FilePath '${executable}' -ArgumentList ('-con "' + $connection + '"')
} catch {
  exit 13
}`;
  const encodedBroker = encodeUTF8Base64(script);
  const powershellPath = '%SystemRoot%\\System32\\WindowsPowerShell\\v1.0\\powershell.exe';
  const brokerDirectory = '%LOCALAPPDATA%\\Jianmen';
  const brokerPath = `${brokerDirectory}\\database-protocol.ps1`;
  const installBrokerScript = [
    "$ErrorActionPreference='Stop'",
    "$dir=Join-Path $env:LOCALAPPDATA 'Jianmen'",
    "[void](New-Item -ItemType Directory -Force -Path $dir)",
    "$path=Join-Path $dir 'database-protocol.ps1'",
    `$content=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('${encodedBroker}'))`,
    "Set-Content -LiteralPath $path -Value $content -Encoding UTF8",
  ].join(';');
  const installBroker = `"${powershellPath}" -NoProfile -NonInteractive -Command "${installBrokerScript}"`;
  const launcher = `\\"${powershellPath}\\" -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy RemoteSigned -File \\"${brokerPath}\\" \\"%1\\"`;
  const protocolRoot = 'HKCU\\Software\\Classes\\jianmen-db';

  return [
    installBroker,
    `reg.exe add "${protocolRoot}" /ve /d "URL:Jianmen Database Connection" /f`,
    `reg.exe add "${protocolRoot}" /v "URL Protocol" /d "" /f`,
    `reg.exe add "${protocolRoot}\\shell\\open\\command" /ve /d "${launcher}" /f`,
  ].join(' && ');
}

function defaultDatabaseName(protocol: string): string {
  return ['postgres', 'postgresql'].includes(protocol.trim().toLowerCase()) ? 'postgres' : '';
}

function databaseDriver(protocol: string): DatabaseClientLaunchPayload['driver'] | '' {
  const normalized = protocol.trim().toLowerCase();
  if (normalized === 'postgres' || normalized === 'postgresql') return 'postgresql';
  if (normalized === 'mysql') return 'mysql';
  return '';
}

function normalizeLaunchPayload(input: DatabaseClientLaunchInput): DatabaseClientLaunchPayload | null {
  const driver = databaseDriver(input.protocol);
  const host = input.host.trim();
  const username = input.username.trim();
  const database = (input.databaseName ?? defaultDatabaseName(input.protocol)).trim();
  const name = sanitizeConnectionName(input.connectionName || 'Jianmen 临时连接');
  if (
    !driver
    || !Number.isInteger(input.port)
    || input.port < 1
    || input.port > 65535
    || !/^[A-Za-z0-9._:[\]\-]{1,255}$/.test(host)
    || !/^[\p{L}\p{N}._@+\-]{1,256}$/u.test(username)
    || !/^[\p{L}\p{N} ._\-]{0,128}$/u.test(database)
    || !name
  ) {
    return null;
  }
  return {
    v: 1,
    driver,
    host,
    port: input.port,
    database,
    user: username,
    name,
    tls: 'verify-full',
  };
}

function dbeaverConnectionFields(payload: DatabaseClientLaunchPayload): string[] {
  return [
    `driver=${payload.driver}`,
    `host=${payload.host}`,
    `port=${payload.port}`,
    `database=${payload.database}`,
    `user=${payload.user}`,
    `name=${payload.name}`,
  ];
}

function sanitizeConnectionName(value: string): string {
  return String(value)
    .replace(/[^\p{L}\p{N} ._()'\/\-]/gu, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 128);
}

function encodeBase64URL(value: string): string {
  return encodeUTF8Base64(value).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function encodeUTF8Base64(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = '';
  bytes.forEach(byte => {
    binary += String.fromCharCode(byte);
  });
  return btoa(binary);
}

function quotePowerShellLiteral(value: string): string {
  return value.replace(/'/g, "''");
}

function quotePowerShell(value: string): string {
  return `'${value.replace(/'/g, "''")}'`;
}

function quotePOSIX(value: string): string {
  return `'${value.replace(/'/g, "'\"'\"'")}'`;
}
