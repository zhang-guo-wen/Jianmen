export interface ConnectionTargetConnectionInfo {
  host: string;
  port: number;
  compactUser: string;
}

export interface DatabaseConnectionPlan {
  command?: string | null;
  commandPlatform?: string | null;
  unavailableReason?: string | null;
}

export interface ConnectionCommandInput {
  resourceType: 'host' | 'database';
  allowSsh?: boolean;
  allowSftp?: boolean;
  connectionInfo: ConnectionTargetConnectionInfo | null;
  databaseConnection: DatabaseConnectionPlan | null;
}

export type CommandItem = {
  label: string;
  value: string;
};

export function buildConnectionCommands({
  resourceType,
  allowSsh = true,
  allowSftp = false,
  connectionInfo,
  databaseConnection,
}: ConnectionCommandInput): CommandItem[] {
  if (!connectionInfo) return [];

  const { host, port, compactUser } = connectionInfo;
  if (resourceType === 'host') {
    const values: CommandItem[] = [];
    if (allowSsh) values.push({ label: 'SSH 命令', value: `ssh ${compactUser}@${host} -p ${port}` });
    if (allowSftp) values.push({ label: 'XFTP/SFTP 命令', value: `sftp -P ${port} ${compactUser}@${host}` });
    return values;
  }

  const command = databaseConnection?.command;
  if (!command) return [];
  const platform = databaseConnection?.commandPlatform || '';
  return [{ label: `${platform} 连接命令`, value: command }];
}
