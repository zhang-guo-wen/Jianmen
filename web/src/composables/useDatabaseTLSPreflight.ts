import { shallowRef } from 'vue'
import {
  apiClient,
  type DBTLSPreflightPayload,
  type DBTLSPreflightResult,
} from '@/api/client'

export interface DatabaseTLSPreflightResultState extends DBTLSPreflightResult {
  fingerprint: string
}

type DatabaseTLSProbe = (
  payload: DBTLSPreflightPayload,
  signal?: AbortSignal,
) => Promise<DBTLSPreflightResult>

export function databaseTLSPreflightFingerprint(payload: DBTLSPreflightPayload): string {
  return JSON.stringify({
    instance_id: payload.instance_id || '',
    protocol: payload.protocol.trim().toLowerCase(),
    address: payload.address.trim(),
    port: payload.port,
    tls_mode: payload.tls_mode,
    tls_server_name: payload.tls_server_name?.trim() || '',
    tls_ca_pem: payload.tls_ca_pem?.trim() || '',
    clear_tls_ca: Boolean(payload.clear_tls_ca),
  })
}

export function useDatabaseTLSPreflight(
  probe: DatabaseTLSProbe = apiClient.preflightDBInstanceTLS,
) {
  const checking = shallowRef(false)
  const result = shallowRef<DatabaseTLSPreflightResultState | null>(null)
  const verifiedFingerprint = shallowRef('')
  let generation = 0
  let controller: AbortController | null = null

  function isVerified(payload: DBTLSPreflightPayload): boolean {
    return verifiedFingerprint.value === databaseTLSPreflightFingerprint(payload)
  }

  function accept(payload: DBTLSPreflightPayload) {
    verifiedFingerprint.value = databaseTLSPreflightFingerprint(payload)
    result.value = null
  }

  function invalidate() {
    generation += 1
    controller?.abort()
    controller = null
    checking.value = false
    verifiedFingerprint.value = ''
    result.value = null
  }

  function reset() {
    invalidate()
  }

  async function verify(payload: DBTLSPreflightPayload): Promise<DBTLSPreflightResult> {
    const fingerprint = databaseTLSPreflightFingerprint(payload)
    if (verifiedFingerprint.value === fingerprint) {
      return { ok: true, latency_ms: result.value?.latency_ms ?? 0 }
    }

    const requestGeneration = ++generation
    controller?.abort()
    const requestController = new AbortController()
    controller = requestController
    checking.value = true
    result.value = null
    try {
      const response = await probe(payload, requestController.signal)
      if (requestGeneration !== generation) {
        return cancelledResult()
      }
      result.value = { ...response, fingerprint }
      verifiedFingerprint.value = response.ok ? fingerprint : ''
      return response
    } catch (error) {
      if (requestGeneration !== generation || requestController.signal.aborted) {
        return cancelledResult()
      }
      const message = error instanceof Error ? error.message : 'TLS 检测失败'
      const response: DBTLSPreflightResult = {
        ok: false,
        stage: 'request',
        code: 'request_failed',
        message,
        error: message,
        latency_ms: 0,
      }
      result.value = { ...response, fingerprint }
      verifiedFingerprint.value = ''
      return response
    } finally {
      if (requestGeneration === generation) {
        checking.value = false
        controller = null
      }
    }
  }

  return {
    checking,
    result,
    accept,
    invalidate,
    isVerified,
    reset,
    verify,
  }
}

function cancelledResult(): DBTLSPreflightResult {
  return {
    ok: false,
    stage: 'request',
    code: 'cancelled',
    message: 'TLS 检测已取消',
    error: 'TLS 检测已取消',
    latency_ms: 0,
  }
}
