import type { DatabaseTLSMode } from '../api/client'

export const DEFAULT_DATABASE_UPSTREAM_TLS_MODE: DatabaseTLSMode = 'disable'

export function normalizeDatabaseUpstreamTLSMode(value: unknown): DatabaseTLSMode {
  if (value === 'disable' || value === 'verify-ca' || value === 'verify-full') {
    return value
  }
  return DEFAULT_DATABASE_UPSTREAM_TLS_MODE
}

export interface DatabaseUpstreamTLSPayload {
  tls_mode: DatabaseTLSMode
  tls_server_name?: string
  tls_ca_pem?: string
}

export function buildDatabaseUpstreamTLSPayload(
  modeValue: unknown,
  serverName?: string,
  caPEM?: string,
): DatabaseUpstreamTLSPayload {
  const mode = normalizeDatabaseUpstreamTLSMode(modeValue)
  const payload: DatabaseUpstreamTLSPayload = { tls_mode: mode }
  if (mode === 'verify-full' && serverName?.trim()) {
    payload.tls_server_name = serverName.trim()
  }
  if (mode !== 'disable' && caPEM?.trim()) {
    payload.tls_ca_pem = caPEM.trim()
  }
  return payload
}
