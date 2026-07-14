export interface SSHClientOption {
  command: string;
  label: string;
  defaultPath: string;
  urlArgument: string;
}

export const SSH_CLIENT_OPTIONS: SSHClientOption[] = [
  { command: 'default', label: '系统默认 SSH 协议', defaultPath: '', urlArgument: '"%1"' },
  { command: 'xshell', label: 'Xshell', defaultPath: 'C:\\Program Files (x86)\\NetSarang\\Xshell 7\\Xshell.exe', urlArgument: '-url "%1"' },
  { command: 'putty', label: 'PuTTY', defaultPath: 'C:\\Program Files\\PuTTY\\putty.exe', urlArgument: '"%1"' },
  { command: 'securecrt', label: 'SecureCRT', defaultPath: 'C:\\Program Files\\VanDyke Software\\SecureCRT\\SecureCRT.exe', urlArgument: '"%1"' },
  { command: 'mobaxterm', label: 'MobaXterm', defaultPath: 'C:\\Program Files (x86)\\Mobatek\\MobaXterm\\MobaXterm.exe', urlArgument: '"%1"' },
  { command: 'winterm', label: 'Windows Terminal', defaultPath: 'wt.exe', urlArgument: '"%1"' },
  { command: 'system', label: '系统 SSH (ssh.exe)', defaultPath: 'ssh.exe', urlArgument: '"%1"' },
];

export function sshClientOption(command: string): SSHClientOption | undefined {
  return SSH_CLIENT_OPTIONS.find(option => option.command === command);
}

export function buildSSHProtocolRegistrationCommand(command: string, configuredPath: string): string {
  const option = sshClientOption(command);
  if (!option || option.command === 'default') return '';
  const executable = configuredPath.trim() || option.defaultPath;
  if (!executable) return '';
  const escaped = executable.replace(/\\/g, '\\\\');
  return `reg add "HKCR\\ssh" /ve /d "URL:SSH Protocol" /f && reg add "HKCR\\ssh" /v "URL Protocol" /d "" /f && reg add "HKCR\\ssh\\shell\\open\\command" /ve /d "\\"${escaped}\\" ${option.urlArgument.replace(/"/g, '\\"')}" /f`;
}
