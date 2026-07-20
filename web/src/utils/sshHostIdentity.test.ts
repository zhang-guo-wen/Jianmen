import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  parseSSHHostIdentityIssue,
  sshHostIdentityNotice,
} from './sshHostIdentity'

test('parses a changed SSH host key without losing disable state', () => {
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
  const notice = sshHostIdentityNotice(issue!)
  assert.match(notice.message, /SHA256:old/)
  assert.match(notice.message, /SHA256:new/)
  assert.match(notice.message, /已自动停用/)
})

test('parses unavailable identity and ignores unrelated API errors', () => {
  const unavailable = parseSSHHostIdentityIssue({
    code: 'SSH_HOST_KEY_UNAVAILABLE',
    details: { host_id: 'host-2' },
  })
  assert.deepEqual(unavailable, { kind: 'unavailable', hostId: 'host-2' })
  assert.match(sshHostIdentityNotice(unavailable!).message, /重新启用/)
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
  assert.match(sshHostIdentityNotice(refreshFailed!).message, /仍保持停用/)
  const activeRefreshFailed = parseSSHHostIdentityIssue({
    code: 'SSH_HOST_IDENTITY_REFRESH_FAILED',
    details: {
      host_id: 'host-4',
      host_status: 'active',
      identity_status: 'available',
    },
  })
  assert.match(sshHostIdentityNotice(activeRefreshFailed!).message, /本次修改没有生效/)
  assert.equal(parseSSHHostIdentityIssue({ code: 'VALIDATION_ERROR' }), null)
  assert.equal(parseSSHHostIdentityIssue(new Error('network failure')), null)
})

test('quick-connect web and local client actions preflight SSH host identity', () => {
  const source = readFileSync(new URL('../views/QuickConnectView.vue', import.meta.url), 'utf8')
  const webStart = source.indexOf('async function openWebConnection')
  const clientStart = source.indexOf('async function openClientConnection')
  const preflightStart = source.indexOf('async function preflightSSHConnection')
  const preflightEnd = source.indexOf('function handleQuickHostIdentityChanged', preflightStart)

  assert.ok(webStart >= 0 && clientStart > webStart && preflightStart > clientStart)
  assert.match(source.slice(webStart, clientStart), /await preflightSSHConnection\(target\)/)
  assert.match(source.slice(clientStart, preflightStart), /await preflightSSHConnection\(target\)/)
  assert.match(source.slice(preflightStart, preflightEnd), /apiClient\.testTargetConnection\(\{ id: targetID \}\)/)
  assert.match(source.slice(preflightStart, preflightEnd), /ElMessageBox\.alert/)
  assert.match(source.slice(preflightStart, preflightEnd), /await loadTargets\(\)/)
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
