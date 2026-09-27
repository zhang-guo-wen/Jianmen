import { describe, expect, it, vi } from 'vitest'
import type { DBTLSPreflightPayload, DBTLSPreflightResult } from '@/api/client'
import {
  databaseTLSPreflightFingerprint,
  useDatabaseTLSPreflight,
} from './useDatabaseTLSPreflight'

const basePayload: DBTLSPreflightPayload = {
  protocol: 'mysql',
  address: 'db.example.com',
  port: 3306,
  tls_mode: 'verify-full',
  tls_server_name: 'db.example.com',
}

describe('useDatabaseTLSPreflight', () => {
  it('only marks the exact successful configuration as verified', async () => {
    const probe = vi.fn(async () => ({ ok: true, latency_ms: 12 }))
    const preflight = useDatabaseTLSPreflight(probe)

    expect(await preflight.verify(basePayload)).toMatchObject({ ok: true })
    expect(preflight.isVerified(basePayload)).toBe(true)
    expect(preflight.isVerified({ ...basePayload, port: 3307 })).toBe(false)
    await preflight.verify(basePayload)
    expect(probe).toHaveBeenCalledTimes(1)

    preflight.invalidate()
    expect(preflight.isVerified(basePayload)).toBe(false)
  })

  it('keeps a failed probe unverified with its safe response', async () => {
    const preflight = useDatabaseTLSPreflight(async () => ({
      ok: false,
      code: 'ca_untrusted',
      error: '证书不受信任',
      latency_ms: 8,
    }))

    expect(await preflight.verify(basePayload)).toMatchObject({ ok: false, code: 'ca_untrusted' })
    expect(preflight.result.value).toMatchObject({ ok: false, error: '证书不受信任' })
    expect(preflight.isVerified(basePayload)).toBe(false)
  })

  it('ignores an older response after a newer probe starts', async () => {
    const requests: Array<ReturnType<typeof deferred<DBTLSPreflightResult>>> = []
    const preflight = useDatabaseTLSPreflight(() => {
      const request = deferred<DBTLSPreflightResult>()
      requests.push(request)
      return request.promise
    })
    const newerPayload = { ...basePayload, port: 3307 }

    const older = preflight.verify(basePayload)
    const newer = preflight.verify(newerPayload)
    requests[0].resolve({ ok: true, latency_ms: 30 })
    expect(await older).toMatchObject({ ok: false, code: 'cancelled' })
    expect(preflight.checking.value).toBe(true)
    requests[1].resolve({ ok: true, latency_ms: 4 })
    expect(await newer).toMatchObject({ ok: true })
    expect(preflight.isVerified(basePayload)).toBe(false)
    expect(preflight.isVerified(newerPayload)).toBe(true)
    expect(preflight.checking.value).toBe(false)
  })

  it('reset aborts the active request and prevents late state updates', async () => {
    const request = deferred<DBTLSPreflightResult>()
    const preflight = useDatabaseTLSPreflight(() => request.promise)
    const pending = preflight.verify(basePayload)

    preflight.reset()
    request.resolve({ ok: true, latency_ms: 2 })
    expect(await pending).toMatchObject({ ok: false, code: 'cancelled' })
    expect(preflight.result.value).toBeNull()
    expect(preflight.checking.value).toBe(false)
  })

  it('fingerprints retained and cleared CA states independently', () => {
    const retained = { ...basePayload, instance_id: 'db-1' }
    const cleared = { ...retained, clear_tls_ca: true }
    const replacement = { ...retained, tls_ca_pem: 'replacement-ca' }

    expect(databaseTLSPreflightFingerprint(retained)).not.toBe(databaseTLSPreflightFingerprint(cleared))
    expect(databaseTLSPreflightFingerprint(retained)).not.toBe(databaseTLSPreflightFingerprint(replacement))
  })
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}
