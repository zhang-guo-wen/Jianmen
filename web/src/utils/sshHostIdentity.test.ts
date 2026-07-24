import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  isConfirmableSSHHostIdentityIssue,
  parseSSHHostIdentityIssue,
  SSH_HOST_IDENTITY_CONFIRM_MESSAGE,
  SSH_HOST_IDENTITY_CONFIRM_TITLE,
} from './sshHostIdentity'

test('parses a changed SSH host key with its server-observed fingerprint', () => {
  const issue = parseSSHHostIdentityIssue({
    code: 'SSH_HOST_KEY_CHANGED',
    details: {
      host_id: 'host-1',
      old_fingerprint: 'SHA256:old',
      new_fingerprint: 'SHA256:new',
      host_disabled: true,
    },
  })

  assert.deepEqual(issue, {
    kind: 'changed',
    hostId: 'host-1',
    oldFingerprint: 'SHA256:old',
    newFingerprint: 'SHA256:new',
    hostDisabled: true,
  })
  assert.equal(isConfirmableSSHHostIdentityIssue(issue), true)
})

test('parses unavailable identity and ignores non-confirmable errors', () => {
  const unavailable = parseSSHHostIdentityIssue({
    code: 'SSH_HOST_KEY_UNAVAILABLE',
    details: {
      host_id: 'host-2',
      new_fingerprint: 'SHA256:first',
    },
  })
  assert.deepEqual(unavailable, {
    kind: 'unavailable',
    hostId: 'host-2',
    newFingerprint: 'SHA256:first',
  })
  assert.equal(isConfirmableSSHHostIdentityIssue(unavailable), true)

  const refreshFailed = parseSSHHostIdentityIssue({
    code: 'SSH_HOST_IDENTITY_REFRESH_FAILED',
    details: {
      host_id: 'host-3',
      host_status: 'disabled',
      identity_status: 'unavailable',
    },
  })
  assert.deepEqual(refreshFailed, {
    kind: 'refresh_failed',
    hostId: 'host-3',
    hostStatus: 'disabled',
    identityStatus: 'unavailable',
  })
  assert.equal(isConfirmableSSHHostIdentityIssue(refreshFailed), false)
  assert.equal(parseSSHHostIdentityIssue({ code: 'VALIDATION_ERROR' }), null)
  assert.equal(parseSSHHostIdentityIssue(new Error('network failure')), null)
})

test('requires a fingerprint and exposes the warning confirmation copy', () => {
  const missingFingerprint = parseSSHHostIdentityIssue({
    code: 'SSH_HOST_KEY_UNAVAILABLE',
    details: { host_id: 'host-2' },
  })

  assert.equal(isConfirmableSSHHostIdentityIssue(missingFingerprint), false)
  assert.equal(SSH_HOST_IDENTITY_CONFIRM_TITLE, '连接确认')
  assert.equal(
    SSH_HOST_IDENTITY_CONFIRM_MESSAGE,
    '主机身份信息发生变化，请确认主机是否正常，是否继续连接',
  )
})

test('quick-connect web and local client actions use guarded identity recovery', () => {
  const source = readFileSync(new URL('../views/QuickConnectView.vue', import.meta.url), 'utf8')
  const webStart = source.indexOf('async function openWebConnection')
  const clientStart = source.indexOf('async function openClientConnection')
  const preflightStart = source.indexOf('async function preflightSSHConnection')
  const preflightEnd = source.indexOf('watch([targetPage', preflightStart)
  const clientSource = source.slice(clientStart, preflightStart)
  const preflightSource = source.slice(preflightStart, preflightEnd)

  assert.ok(webStart >= 0 && clientStart > webStart && preflightStart > clientStart)
  assert.match(source.slice(webStart, clientStart), /await preflightSSHConnection\(target\)/)
  assert.match(clientSource, /await preflightSSHConnection\(target\)/)
  assert.ok(
    clientSource.indexOf('!preferences.hasSSHClient || !preferences.sshProtocolRegistered')
      < clientSource.indexOf('preflightSSHConnection(target)'),
    'SSH client configuration and registration should be checked before connection preflight',
  )
  assert.match(clientSource, /openClientSettings\('ssh'\)/)
  assert.match(clientSource, /isCurrentSSHQuickTarget\(targetID\)/)
  assert.match(preflightSource, /runWithSSHHostIdentityRecovery/)
  assert.match(preflightSource, /apiClient\.testTargetConnection\(\{ id: targetID \}\)/)
  assert.match(preflightSource, /key: `quick-connect:\$\{targetID\}`/)
  assert.match(preflightSource, /isCurrent: isCurrentRequest/)
  assert.match(preflightSource, /recovery\.status !== 'success'/)
  assert.doesNotMatch(preflightSource, /ElMessageBox\.alert/)
  assert.match(source, /function isCurrentSSHQuickTarget[\s\S]*quickConnectViewActive/)
  assert.match(source, /onBeforeUnmount\(\(\) => \{[\s\S]*quickConnectViewActive = false/)
})

test('host management derives account state from API status and rejects stale account pages', () => {
  const source = readFileSync(new URL('../views/HostsView.vue', import.meta.url), 'utf8')

  assert.match(source, /const hostPageSize = ref\(20\)/)
  assert.match(source, /function targetEnabled\(target: TargetRecord\)/)
  assert.match(source, /disabled: !targetEnabled\(target\)/)
  assert.doesNotMatch(source, /:model-value="!row\.disabled"/)
  assert.match(source, /const requestSequence = \+\+accountRequestSequence/)
  assert.match(source, /requestSequence !== accountRequestSequence/)
  assert.match(source, /accountPage\.value === requestedPage/)
  assert.match(source, /accountPageSize\.value === requestedPageSize/)
})

test('host account dialogs reject stale details and keep connectable accounts paginated', () => {
  const source = readFileSync(new URL('../views/HostsView.vue', import.meta.url), 'utf8')
  const loadStart = source.indexOf('async function loadSelectedHostAccounts')
  const editStart = source.indexOf('async function openEditAccountDialog')
  const submitStart = source.indexOf('async function submitAccount', editStart)
  const connectStart = source.indexOf('async function handleHostConnect')
  const auditStart = source.indexOf('function handleHostAuditLog', connectStart)

  assert.ok(loadStart >= 0 && editStart > loadStart && submitStart > editStart)
  assert.ok(connectStart > submitStart && auditStart > connectStart)

  const loader = source.slice(loadStart, editStart)
  assert.match(loader, /const requestedConnectableOnly = accountsConnectableOnly\.value/)
  assert.match(loader, /connectable: requestedConnectableOnly \|\| undefined/)
  assert.match(loader, /accountsConnectableOnly\.value === requestedConnectableOnly/)

  const editor = source.slice(editStart, submitStart)
  assert.match(editor, /const requestSequence = \+\+accountDetailRequestSequence/)
  assert.match(editor, /editingAccountId\.value === id/)
  assert.match(editor, /accountFormVisible\.value/)
  assert.match(editor, /if \(!isCurrentRequest\(\)\) return/)

  const connector = source.slice(connectStart, auditStart)
  assert.match(connector, /page_size: 2/)
  assert.doesNotMatch(connector, /page_size: 200/)
  assert.match(connector, /accountsConnectableOnly\.value = true/)
  assert.match(connector, /await loadSelectedHostAccounts\(\)/)

  assert.match(source, /watch\(accountFormVisible,[\s\S]*accountDetailRequestSequence \+= 1/)
  assert.match(source, /watch\(accountsDialogVisible,[\s\S]*accountsConnectableOnly\.value = false/)
})

test('host account connection test snapshots payload and guards stale form state', () => {
  const source = readFileSync(new URL('../views/HostsView.vue', import.meta.url), 'utf8')
  const testStart = source.indexOf('async function testConnection')
  const testEnd = source.indexOf('function handleHostIdentityChanged', testStart)
  const testSource = source.slice(testStart, testEnd)

  assert.match(testSource, /if \(testingConnection\.value\) return/)
  assert.match(testSource, /const payload = buildAccountPayload\(\)/)
  assert.match(testSource, /runWithSSHHostIdentityRecovery/)
  assert.match(testSource, /accountFormVisible\.value/)
  assert.match(testSource, /hostsViewActive/)
  assert.match(testSource, /requestSequence === accountConnectionTestSequence/)
  assert.match(testSource, /recovery\.status !== "success"/)
  assert.match(source, /watch\(accountFormVisible,[\s\S]*accountConnectionTestSequence \+= 1/)
  assert.match(source, /onBeforeUnmount\(\(\) => \{[\s\S]*hostsViewActive = false[\s\S]*accountConnectionTestSequence \+= 1/)
})
