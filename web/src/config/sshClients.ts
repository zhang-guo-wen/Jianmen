export interface SSHClientOption {
  command: string;
  label: string;
  urlArgument: string;
}

export const SSH_CLIENT_OPTIONS: SSHClientOption[] = [
  { command: 'default', label: '系统默认 SSH 协议', urlArgument: '"%1"' },
  { command: 'xshell', label: 'Xshell', urlArgument: '-url "%1"' },
  { command: 'putty', label: 'PuTTY', urlArgument: '"%1"' },
  { command: 'securecrt', label: 'SecureCRT', urlArgument: '"%1"' },
  { command: 'mobaxterm', label: 'MobaXterm', urlArgument: '"%1"' },
  { command: 'winterm', label: 'Windows Terminal', urlArgument: '"%1"' },
  { command: 'system', label: '系统 SSH (ssh.exe)', urlArgument: '"%1"' },
];

export type ClientPlatform = 'windows' | 'macos' | 'linux';

export const CLIENT_PLATFORM_OPTIONS = [
  { label: 'Windows', value: 'windows' },
  { label: 'macOS', value: 'macos' },
  { label: 'Linux', value: 'linux' },
] as const;

export function sshClientOption(command: string): SSHClientOption | undefined {
  return SSH_CLIENT_OPTIONS.find(option => option.command === command);
}

export function buildSSHProtocolRegistrationCommand(command: string, configuredPath: string, platform: ClientPlatform = 'windows'): string {
  const option = sshClientOption(command);
  if (!option || option.command === 'default') return '';
  const executable = configuredPath.trim();
  if (!isAbsoluteExecutablePath(executable)) return '';
  if (platform === 'windows') {
    const escaped = executable.replace(/\\/g, '\\\\');
    return `reg add "HKCR\\ssh" /ve /d "URL:SSH Protocol" /f && reg add "HKCR\\ssh" /v "URL Protocol" /d "" /f && reg add "HKCR\\ssh\\shell\\open\\command" /ve /d "\\"${escaped}\\" ${option.urlArgument.replace(/"/g, '\\"')}" /f`;
  }
  return '';
}

export function isAbsoluteExecutablePath(path: string): boolean {
  const value = path.trim();
  return /^[A-Za-z]:[\\/].+\.exe$/i.test(value) || /^\\\\[^\\/]+[\\/][^\\/]+[\\/].+\.exe$/i.test(value);
}
