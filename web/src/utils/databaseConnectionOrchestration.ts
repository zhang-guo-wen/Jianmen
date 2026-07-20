export interface DatabaseConnectionResourceLoaders<Gateway, Session, Credential> {
  protocol: string;
  targetID: string;
  getGateway(protocol: string): Promise<Gateway>;
  validateGateway?(gateway: Gateway): void;
  createSession(targetID: string): Promise<Session>;
  createPassword(targetID: string): Promise<Credential>;
}

export interface DatabaseConnectionResources<Gateway, Session, Credential> {
  gateway: Gateway;
  session: Session | null;
  credential: Credential | null;
}

export function isGatewayOnlyDatabaseProtocol(protocol: string): boolean {
  return protocol.trim().toLowerCase() === 'redis';
}

export async function loadDatabaseConnectionResources<Gateway, Session, Credential>(
  loaders: DatabaseConnectionResourceLoaders<Gateway, Session, Credential>,
): Promise<DatabaseConnectionResources<Gateway, Session, Credential>> {
  const protocol = loaders.protocol.trim().toLowerCase();
  const gateway = await loaders.getGateway(protocol);
  loaders.validateGateway?.(gateway);
  if (isGatewayOnlyDatabaseProtocol(protocol)) {
    return {
      gateway,
      session: null,
      credential: null,
    };
  }

  const [session, credential] = await Promise.all([
    loaders.createSession(loaders.targetID),
    loaders.createPassword(loaders.targetID),
  ]);
  return { gateway, session, credential };
}
