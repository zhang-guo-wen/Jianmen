import assert from 'node:assert/strict'
import test from 'node:test'

import {
  buildDatabaseUpstreamTLSPayload,
  DEFAULT_DATABASE_UPSTREAM_TLS_MODE,
  normalizeDatabaseUpstreamTLSMode,
} from './databaseUpstreamTLS'

test('database upstream TLS defaults to disabled', () => {
  assert.equal(DEFAULT_DATABASE_UPSTREAM_TLS_MODE, 'disable')
  assert.equal(normalizeDatabaseUpstreamTLSMode(undefined), 'disable')
  assert.equal(normalizeDatabaseUpstreamTLSMode(''), 'disable')
  assert.equal(normalizeDatabaseUpstreamTLSMode('unsupported'), 'disable')
})

test('database upstream TLS preserves explicit supported modes', () => {
  assert.equal(normalizeDatabaseUpstreamTLSMode('disable'), 'disable')
  assert.equal(normalizeDatabaseUpstreamTLSMode('verify-ca'), 'verify-ca')
  assert.equal(normalizeDatabaseUpstreamTLSMode('verify-full'), 'verify-full')
})

test('database upstream TLS payload omits fields that do not apply to its mode', () => {
  assert.deepEqual(
    buildDatabaseUpstreamTLSPayload('disable', 'old.internal', 'old CA'),
    { tls_mode: 'disable' },
  )
  assert.deepEqual(
    buildDatabaseUpstreamTLSPayload('verify-ca', 'ignored.internal', '  CA PEM  '),
    { tls_mode: 'verify-ca', tls_ca_pem: 'CA PEM' },
  )
  assert.deepEqual(
    buildDatabaseUpstreamTLSPayload('verify-full', ' db.internal ', '  CA PEM  '),
    {
      tls_mode: 'verify-full',
      tls_server_name: 'db.internal',
      tls_ca_pem: 'CA PEM',
    },
  )
})
