export interface RequestGenerationToken {
  generation: number;
  snapshot: string;
}

export interface RequestGenerationGuard {
  begin(snapshot: string): RequestGenerationToken;
  isCurrent(token: RequestGenerationToken, currentSnapshot: string): boolean;
  invalidate(): void;
}

export function createRequestGenerationGuard(): RequestGenerationGuard {
  let generation = 0;

  return {
    begin(snapshot) {
      generation += 1;
      return { generation, snapshot };
    },
    isCurrent(token, currentSnapshot) {
      return token.generation === generation && token.snapshot === currentSnapshot;
    },
    invalidate() {
      generation += 1;
    },
  };
}

export type InFlightOperation = 'copy' | 'download';
export type InFlightCounters = Record<string, Partial<Record<InFlightOperation, number>>>;

export function beginInFlight(
  counters: InFlightCounters,
  key: string,
  operation: InFlightOperation,
): void {
  const account = counters[key] ?? {};
  account[operation] = (account[operation] ?? 0) + 1;
  counters[key] = account;
}

export function beginInFlightIfIdle(
  counters: InFlightCounters,
  key: string,
  operation: InFlightOperation,
): boolean {
  if (isInFlight(counters, key, operation)) return false;
  beginInFlight(counters, key, operation);
  return true;
}

export function endInFlight(
  counters: InFlightCounters,
  key: string,
  operation: InFlightOperation,
): void {
  const account = counters[key];
  if (!account) return;
  account[operation] = Math.max(0, (account[operation] ?? 0) - 1);
}

export function isInFlight(
  counters: InFlightCounters,
  key: string,
  operation: InFlightOperation,
): boolean {
  return (counters[key]?.[operation] ?? 0) > 0;
}

export function createSingleFlight<T>() {
  let current: Promise<T> | null = null;

  return {
    run(loader: () => Promise<T>): Promise<T> {
      if (current) return current;
      current = loader().finally(() => {
        current = null;
      });
      return current;
    },
  };
}

export interface KeyedRequestToken {
  generation: number;
  key: string;
}

export interface KeyedRequest<T> {
  token: KeyedRequestToken;
  promise: Promise<T>;
}

/**
 * Coalesces identical requests while retaining latest-target and active-count
 * semantics for callers that need stale-result protection and loading state.
 */
export function createLatestKeyedRequest<T>() {
  const flights = new Map<string, Promise<T>>();
  let generation = 0;
  let latestKey = '';
  let active = 0;

  return {
    begin(key: string, loader: () => Promise<T>): KeyedRequest<T> {
      generation += 1;
      latestKey = key;
      let promise = flights.get(key);
      if (!promise) {
        active += 1;
        promise = Promise.resolve()
          .then(loader)
          .finally(() => {
            if (flights.get(key) === promise) flights.delete(key);
            active = Math.max(0, active - 1);
          });
        flights.set(key, promise);
      }
      return { token: { generation, key }, promise };
    },
    isCurrent(token: KeyedRequestToken, key: string): boolean {
      return token.generation === generation && token.key === key && latestKey === key;
    },
    invalidate(): void {
      generation += 1;
      latestKey = '';
    },
    isLoading(): boolean {
      return active > 0;
    },
  };
}
