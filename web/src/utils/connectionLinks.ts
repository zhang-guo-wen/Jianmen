export interface SSHDeepLinkOptions {
  username: string;
  password?: string;
  host: string;
  port: number;
}

export function buildSSHDeepLink(options: SSHDeepLinkOptions): string {
  const username = encodeURIComponent(options.username);
  const password = options.password ? `:${encodeURIComponent(options.password)}` : '';
  const host = options.host.includes(':') && !options.host.startsWith('[')
    ? `[${options.host}]`
    : options.host;
  return `ssh://${username}${password}@${host}:${options.port}`;
}
