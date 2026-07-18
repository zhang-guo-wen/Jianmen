export interface SSHDeepLinkOptions {
  username: string;
  host: string;
  port: number;
}

export function buildSSHDeepLink(options: SSHDeepLinkOptions): string {
  const username = encodeURIComponent(options.username);
  const host = options.host.includes(':') && !options.host.startsWith('[')
    ? `[${options.host}]`
    : options.host;
  return `ssh://${username}@${host}:${options.port}`;
}
