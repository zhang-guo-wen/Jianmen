export type DatabaseClientPlatform = 'windows' | 'macos' | 'linux';

export interface DatabaseClientSettings {
  client: '' | 'dbeaver';
  platform: DatabaseClientPlatform;
  executablePath: string;
  caFilePath: string;
  protocolRegistered: boolean;
}

export interface DatabaseClientLaunchInput {
  protocol: string;
  host: string;
  port: number;
  username: string;
  password: string;
  databaseName?: string;
  connectionName?: string;
}

interface DatabaseClientLaunchPayload {
  v: 2;
  driver: 'mysql' | 'postgresql';
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
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

export const DATABASE_CLIENT_CA_FILE_NAME = 'jianmen-database-gateway-ca.pem';
export const DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION = 2;

export function isCurrentDatabaseClientProtocolRegistration(
  registered: unknown,
  version: unknown,
): boolean {
  return registered === true && version === DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION;
}

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

export function databaseClientCAFileExample(platform: DatabaseClientPlatform): string {
  if (platform === 'macos') return `/Users/your-name/Downloads/${DATABASE_CLIENT_CA_FILE_NAME}`;
  if (platform === 'linux') return `/home/your-name/Downloads/${DATABASE_CLIENT_CA_FILE_NAME}`;
  return `C:\\Users\\your-name\\Downloads\\${DATABASE_CLIENT_CA_FILE_NAME}`;
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

export function isValidDatabaseClientCAFilePath(
  path: string,
  platform: DatabaseClientPlatform,
): boolean {
  const value = path.trim();
  if (
    !value
    || value.length > 1024
    || /[\r\n\0|]/.test(value)
    || !/\.(?:pem|crt|cer)$/i.test(value)
  ) {
    return false;
  }
  if (platform === 'windows') {
    if (!/^[A-Za-z]:[\\/]/.test(value)) return false;
    return !/[<>:"?*]/.test(value.slice(3));
  }
  return value.startsWith('/');
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
    || !isValidDatabaseClientCAFilePath(settings.caFilePath, 'windows')
  ) {
    return '';
  }

  const executable = quotePowerShellLiteral(settings.executablePath.trim());
  const caFile = quotePowerShellLiteral(settings.caFilePath.trim());
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
  $allowed = @('v', 'driver', 'host', 'port', 'database', 'user', 'password', 'name', 'tls')
  $properties = @($data.PSObject.Properties.Name)
  if ($properties.Count -ne $allowed.Count -or @($properties | Where-Object { $_ -notin $allowed }).Count -ne 0) { exit 5 }
  if ([string]$data.v -cne '2' -or [string]$data.tls -cne 'verify-full') { exit 6 }
  if ([string]$data.driver -cnotin @('mysql', 'postgresql')) { exit 7 }
  if ([string]$data.host -notmatch '^[A-Za-z0-9._:\[\]-]{1,255}$') { exit 8 }
  if ([string]$data.user -notmatch '^[\p{L}\p{N}._@+-]{1,256}$') { exit 9 }
  if ([string]$data.password -notmatch '^[A-Za-z0-9_-]{16,128}$') { exit 10 }
  if ([string]$data.database -notmatch '^[\p{L}\p{N} ._-]{0,128}$') { exit 11 }
  if ([string]$data.name -notmatch "^[\p{L}\p{N} ._()'/-]{1,128}$") { exit 12 }
  $port = 0
  if (-not [int]::TryParse([string]$data.port, [ref]$port) -or $port -lt 1 -or $port -gt 65535) { exit 13 }
  $caFile = '${caFile}'
  if (-not (Test-Path -LiteralPath $caFile -PathType Leaf)) {
    try {
      $message = '配置的网关 CA 文件不存在：' + [Environment]::NewLine + $caFile +
        [Environment]::NewLine + '请在 Jianmen 个人设置中重新下载或修正路径。'
      [void](New-Object -ComObject WScript.Shell).Popup($message, 0, 'Jianmen 数据库连接', 16)
    } catch {}
    exit 14
  }
  $parts = @(
    "driver=$($data.driver)",
    "host=$($data.host)",
    "port=$port",
    "database=$($data.database)",
    "user=$($data.user)",
    "password=$($data.password)",
    "name=$($data.name)",
    'savePassword=true',
    'create=true',
    'save=false',
    'connect=true',
    'netHandler.ssl.method=CERTIFICATES',
    "netHandler.ssl.ca.cert=$caFile"
  )
  if ([string]$data.driver -ceq 'mysql') {
    $parts += @(
      'prop.sslMode=VERIFY_IDENTITY',
      'netHandler.ssl.require=true',
      'netHandler.ssl.verify.server=true'
    )
  } else {
    $parts += @(
      'prop.ssl=true',
      'prop.sslmode=verify-full',
      "prop.sslrootcert=$caFile",
      'netHandler.ssl.sslMode=verify-full'
    )
  }
  $connection = $parts -join '|'
  Start-Process -FilePath '${executable}' -ArgumentList ('-con "' + $connection + '"')
} catch {
  try {
    [void](New-Object -ComObject WScript.Shell).Popup(
      '无法打开 DBeaver，请检查本地客户端路径和网关 CA 配置。',
      0,
      'Jianmen 数据库连接',
      16
    )
  } catch {}
  exit 15
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
  const password = input.password.trim();
  const database = (input.databaseName ?? defaultDatabaseName(input.protocol)).trim();
  const name = sanitizeConnectionName(input.connectionName || 'Jianmen 临时连接');
  if (
    !driver
    || !Number.isInteger(input.port)
    || input.port < 1
    || input.port > 65535
    || !/^[A-Za-z0-9._:[\]\-]{1,255}$/.test(host)
    || !/^[\p{L}\p{N}._@+\-]{1,256}$/u.test(username)
    || !/^[A-Za-z0-9_-]{16,128}$/.test(password)
    || !/^[\p{L}\p{N} ._\-]{0,128}$/u.test(database)
    || !name
  ) {
    return null;
  }
  return {
    v: 2,
    driver,
    host,
    port: input.port,
    database,
    user: username,
    password,
    name,
    tls: 'verify-full',
  };
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
