export interface DatabaseClientOption {
  command: string;
  label: string;
}

export interface DatabaseConnectionLaunchPayload {
  protocol: string;
  host: string;
  port: number;
  username: string;
  password: string;
  name: string;
}

export const DATABASE_CLIENT_OPTIONS: DatabaseClientOption[] = [
  { command: 'dbeaver', label: 'DBeaver' },
];

export function databaseClientOption(command: string): DatabaseClientOption | undefined {
  return DATABASE_CLIENT_OPTIONS.find(option => option.command === command);
}

export function buildDatabaseProtocolRegistrationCommand(command: string, configuredPath: string): string {
  if (!databaseClientOption(command) || !isAbsoluteExecutablePath(configuredPath)) return '';

  const executable = configuredPath.trim().replace(/'/g, "''");
  const script = [
    '$uri = [Uri]$env:JIANMEN_DB_URL;',
    "$payload = $uri.AbsolutePath.Trim('/').Split('/')[-1];",
    "$payload = $payload.Replace('-', '+').Replace('_', '/');",
    "while (($payload.Length % 4) -ne 0) { $payload += '=' };",
    '$connection = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($payload));',
    `Start-Process -FilePath '${executable}' -ArgumentList ('-con "' + $connection + '"');`,
  ].join(' ');
  const encodedScript = encodePowerShell(script);
  const registryCommand = `cmd.exe /d /s /c "set JIANMEN_DB_URL=%1&& powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -EncodedCommand ${encodedScript}"`;
  const registryData = registryCommand.replace(/"/g, '""');

  return [
    'reg add "HKCR\\jianmen-db" /ve /d "URL:Jianmen Database Connection" /f',
    'reg add "HKCR\\jianmen-db" /v "URL Protocol" /d "" /f',
    `reg add "HKCR\\jianmen-db\\shell\\open\\command" /ve /d "${registryData}" /f`,
  ].join(' && ');
}

export function buildDatabaseProtocolURL(payload: DatabaseConnectionLaunchPayload): string {
  const connection = [
    `driver=${dbeaverValue(databaseDriver(payload.protocol))}`,
    `host=${dbeaverValue(payload.host)}`,
    `port=${payload.port}`,
    `name=${dbeaverValue(payload.name)}`,
    `user=${dbeaverValue(payload.username)}`,
    `password=${dbeaverValue(payload.password)}`,
    'savePassword=true',
    'create=true',
    'save=false',
    'connect=true',
  ].join('|');
  const bytes = new TextEncoder().encode(connection);
  let binary = '';
  bytes.forEach(byte => { binary += String.fromCharCode(byte); });
  const encoded = btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
  return `jianmen-db:///connect/${encoded}`;
}

function encodePowerShell(script: string): string {
  let binary = '';
  for (let index = 0; index < script.length; index += 1) {
    const code = script.charCodeAt(index);
    binary += String.fromCharCode(code & 0xff, code >>> 8);
  }
  return btoa(binary);
}

function dbeaverValue(value: string): string {
  return String(value).replace(/[|\r\n"]/g, ' ');
}

function databaseDriver(protocol: string): string {
  const value = protocol.trim().toLowerCase();
  return value === 'postgres' ? 'postgresql' : value || 'mysql';
}

function isAbsoluteExecutablePath(path: string): boolean {
  const value = path.trim();
  return /^[A-Za-z]:[\\/].+\.exe$/i.test(value) || /^\\\\[^\\/]+[\\/][^\\/]+[\\/].+\.exe$/i.test(value);
}
