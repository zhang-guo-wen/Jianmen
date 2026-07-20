export interface ProvisionGrant {
  database: string;
  privilege: string;
}

export interface ProvisionRequest {
  admin_account_id: string;
  grants: ProvisionGrant[];
  group?: string;
  remark?: string;
  expires_at?: string;
}

export function normalizeProvisionRequest(request: ProvisionRequest): string {
  const grants = request.grants
    .map(grant => ({
      database: grant.database.trim(),
      privilege: grant.privilege.trim(),
    }))
    .sort((left, right) => `${left.database}\u0000${left.privilege}`.localeCompare(`${right.database}\u0000${right.privilege}`));

  return JSON.stringify({
    admin_account_id: request.admin_account_id.trim(),
    grants,
    ...(request.group?.trim() ? { group: request.group.trim() } : {}),
    ...(request.remark?.trim() ? { remark: request.remark.trim() } : {}),
    ...(request.expires_at?.trim() ? { expires_at: request.expires_at.trim() } : {}),
  });
}

export interface ProvisionIdempotencySession {
  keyFor(request: ProvisionRequest, scope?: string): string;
  markFailed(): void;
  markSucceeded(): void;
  reset(): void;
}

export function createProvisionIdempotencySession(
  ...keyFactories: Array<() => string>
): ProvisionIdempotencySession {
  let current: { fingerprint: string; key: string } | null = null;
  let factoryIndex = 0;
  const nextKey = () => {
    const factory = keyFactories[factoryIndex++];
    return factory ? factory() : createIdempotencyKey();
  };

  return {
    keyFor(request, scope = '') {
      const fingerprint = `${scope}\u0000${normalizeProvisionRequest(request)}`;
      if (!current || current.fingerprint !== fingerprint) {
        current = { fingerprint, key: nextKey() };
      }
      return current.key;
    },
    markFailed() {
      // A failed request is retryable with the same key so the server can resume the saga.
    },
    markSucceeded() {
      current = null;
    },
    reset() {
      current = null;
    },
  };
}

export function createIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `provision-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export function withIdempotencyKey(init: RequestInit, key: string): RequestInit {
  const headers = new Headers(init.headers);
  headers.set('Idempotency-Key', key);
  return { ...init, headers };
}
