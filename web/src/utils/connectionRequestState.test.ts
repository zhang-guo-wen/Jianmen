import assert from 'node:assert/strict';
import test from 'node:test';

import {
  beginInFlight,
  beginInFlightIfIdle,
  createLatestKeyedRequest,
  createRequestGenerationGuard,
  createSingleFlight,
  endInFlight,
  isInFlight,
  type InFlightCounters,
} from './connectionRequestState.ts';

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

test('request generation guard rejects stale generations and changed snapshots', () => {
  const guard = createRequestGenerationGuard();
  const first = guard.begin('account-1');

  assert.equal(guard.isCurrent(first, 'account-1'), true);
  assert.equal(guard.isCurrent(first, 'account-2'), false);

  const second = guard.begin('account-2');
  assert.equal(guard.isCurrent(first, 'account-1'), false);
  assert.equal(guard.isCurrent(second, 'account-2'), true);

  guard.invalidate();
  assert.equal(guard.isCurrent(second, 'account-2'), false);
});

test('in-flight counters track operations independently and clean up idle keys', () => {
  const counters: InFlightCounters = {};

  beginInFlight(counters, 'account-1', 'copy');
  beginInFlight(counters, 'account-1', 'copy');
  beginInFlight(counters, 'account-1', 'download');

  assert.equal(isInFlight(counters, 'account-1', 'copy'), true);
  assert.equal(isInFlight(counters, 'account-1', 'download'), true);
  assert.equal(beginInFlightIfIdle(counters, 'account-1', 'copy'), false);
  assert.equal(beginInFlightIfIdle(counters, 'account-2', 'copy'), true);

  endInFlight(counters, 'account-1', 'copy');
  assert.equal(isInFlight(counters, 'account-1', 'copy'), true);
  endInFlight(counters, 'account-1', 'copy');
  assert.equal(isInFlight(counters, 'account-1', 'copy'), false);
  assert.equal(isInFlight(counters, 'account-1', 'download'), true);
  endInFlight(counters, 'account-1', 'download');
  assert.equal('account-1' in counters, false);

  endInFlight(counters, 'missing', 'copy');
  endInFlight(counters, 'account-2', 'copy');
  assert.deepEqual(counters, {});
});

test('single flight coalesces concurrent work and resets after success or failure', async () => {
  const flight = createSingleFlight<number>();
  const firstResult = deferred<number>();
  let calls = 0;

  const first = flight.run(() => {
    calls += 1;
    return firstResult.promise;
  });
  const duplicate = flight.run(async () => {
    calls += 1;
    return 99;
  });

  assert.strictEqual(duplicate, first);
  assert.equal(calls, 1);
  firstResult.resolve(7);
  assert.equal(await duplicate, 7);

  const failure = new Error('failed');
  await assert.rejects(flight.run(async () => {
    throw failure;
  }), failure);
  assert.equal(await flight.run(async () => 8), 8);
});

test('latest keyed request coalesces equal keys and tracks the latest target', async () => {
  const requests = createLatestKeyedRequest<number>();
  const accountOne = deferred<number>();
  const accountTwo = deferred<number>();
  let accountOneCalls = 0;

  const first = requests.begin('account-1', () => {
    accountOneCalls += 1;
    return accountOne.promise;
  });
  const duplicate = requests.begin('account-1', async () => 99);

  assert.strictEqual(first.promise, duplicate.promise);
  assert.equal(accountOneCalls, 0);
  assert.equal(requests.isCurrent(first.token, 'account-1'), false);
  assert.equal(requests.isCurrent(duplicate.token, 'account-1'), true);
  assert.equal(requests.isLoading(), true);

  const second = requests.begin('account-2', () => accountTwo.promise);
  assert.equal(requests.isCurrent(duplicate.token, 'account-1'), false);
  assert.equal(requests.isCurrent(second.token, 'account-2'), true);

  accountTwo.resolve(2);
  assert.equal(await second.promise, 2);
  assert.equal(requests.isLoading(), true);

  accountOne.resolve(1);
  assert.equal(await duplicate.promise, 1);
  assert.equal(accountOneCalls, 1);
  assert.equal(requests.isLoading(), false);

  requests.invalidate();
  assert.equal(requests.isCurrent(second.token, 'account-2'), false);
});
