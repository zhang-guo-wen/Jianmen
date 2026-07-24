import assert from 'node:assert/strict'
import { beforeEach, describe, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  confirm: vi.fn(),
  refreshSSHHostIdentity: vi.fn(),
}))

vi.mock('element-plus', () => ({
  ElMessageBox: {
    confirm: mocks.confirm,
  },
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    refreshSSHHostIdentity: mocks.refreshSSHHostIdentity,
  },
}))

import { useSSHHostIdentityRecovery } from './useSSHHostIdentityRecovery'

function changedError(fingerprint = 'SHA256:new') {
  return Object.assign(new Error('host key changed'), {
    code: 'SSH_HOST_KEY_CHANGED',
    details: {
      host_id: 'host-1',
      old_fingerprint: 'SHA256:old',
      new_fingerprint: fingerprint,
      host_disabled: true,
    },
  })
}

function unavailableError(fingerprint = 'SHA256:first') {
  return Object.assign(new Error('host identity unavailable'), {
    code: 'SSH_HOST_KEY_UNAVAILABLE',
    details: {
      host_id: 'host-1',
      new_fingerprint: fingerprint,
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.confirm.mockResolvedValue('confirm')
  mocks.refreshSSHHostIdentity.mockResolvedValue({
    id: 'host-1',
    name: 'host',
    address: '127.0.0.1',
    port: 22,
    status: 'active',
    identity_status: 'available',
  })
})

describe('useSSHHostIdentityRecovery', () => {
  it('shows a warning, refreshes the confirmed fingerprint, and retries once', async () => {
    const operation = vi.fn()
      .mockRejectedValueOnce(changedError())
      .mockResolvedValueOnce({ ok: true })
    const onConfirmed = vi.fn()
    const { runWithSSHHostIdentityRecovery } = useSSHHostIdentityRecovery({ onConfirmed })

    const result = await runWithSSHHostIdentityRecovery(operation, { key: 'host-1' })

    assert.deepEqual(result, {
      status: 'success',
      value: { ok: true },
      recovered: true,
    })
    assert.equal(operation.mock.calls.length, 2)
    assert.deepEqual(mocks.refreshSSHHostIdentity.mock.calls, [['host-1', 'SHA256:new']])
    assert.equal(onConfirmed.mock.calls.length, 1)
    assert.equal(mocks.confirm.mock.calls[0]?.[1], '连接确认')
    assert.equal(
      mocks.confirm.mock.calls[0]?.[0],
      '主机身份信息发生变化，请确认主机是否正常，是否继续连接',
    )
    assert.equal(mocks.confirm.mock.calls[0]?.[2]?.type, 'warning')
    assert.equal(mocks.confirm.mock.calls[0]?.[2]?.confirmButtonText, '继续连接')
  })

  it('uses the observed fingerprint for first-use identity confirmation', async () => {
    const operation = vi.fn()
      .mockRejectedValueOnce(unavailableError())
      .mockResolvedValueOnce({ ok: true })
    const { runWithSSHHostIdentityRecovery } = useSSHHostIdentityRecovery()

    await runWithSSHHostIdentityRecovery(operation, { key: 'host-1' })

    assert.deepEqual(mocks.refreshSSHHostIdentity.mock.calls, [['host-1', 'SHA256:first']])
  })

  it('does not refresh or retry when the user cancels', async () => {
    mocks.confirm.mockRejectedValueOnce('cancel')
    const operation = vi.fn().mockRejectedValueOnce(changedError())
    const onCancelledAfterDisable = vi.fn()
    const { runWithSSHHostIdentityRecovery } = useSSHHostIdentityRecovery({
      onCancelledAfterDisable,
    })

    const result = await runWithSSHHostIdentityRecovery(operation, { key: 'host-1' })

    assert.equal(result.status, 'cancelled')
    assert.equal(operation.mock.calls.length, 1)
    assert.equal(mocks.refreshSSHHostIdentity.mock.calls.length, 0)
    assert.equal(onCancelledAfterDisable.mock.calls.length, 1)
    assert.equal(onCancelledAfterDisable.mock.calls[0]?.[0]?.hostId, 'host-1')
  })

  it('asks again when refresh observes a third fingerprint', async () => {
    mocks.refreshSSHHostIdentity
      .mockRejectedValueOnce(changedError('SHA256:third'))
      .mockResolvedValueOnce({
        id: 'host-1',
        name: 'host',
        address: '127.0.0.1',
        port: 22,
        status: 'active',
        identity_status: 'available',
      })
    const operation = vi.fn()
      .mockRejectedValueOnce(changedError('SHA256:second'))
      .mockResolvedValueOnce({ ok: true })
    const { runWithSSHHostIdentityRecovery } = useSSHHostIdentityRecovery()

    const result = await runWithSSHHostIdentityRecovery(operation, { key: 'host-1' })

    assert.equal(result.status, 'success')
    assert.equal(mocks.confirm.mock.calls.length, 2)
    assert.deepEqual(mocks.refreshSSHHostIdentity.mock.calls, [
      ['host-1', 'SHA256:second'],
      ['host-1', 'SHA256:third'],
    ])
    assert.equal(operation.mock.calls.length, 2)
  })

  it('stops stale work before showing a dialog or refreshing a host', async () => {
    let current = true
    const operation = vi.fn(async () => {
      current = false
      throw changedError()
    })
    const { runWithSSHHostIdentityRecovery } = useSSHHostIdentityRecovery()

    const result = await runWithSSHHostIdentityRecovery(operation, {
      key: 'host-1',
      isCurrent: () => current,
    })

    assert.deepEqual(result, { status: 'stale' })
    assert.equal(mocks.confirm.mock.calls.length, 0)
    assert.equal(mocks.refreshSSHHostIdentity.mock.calls.length, 0)
  })

  it('rechecks current state after confirmation before refreshing', async () => {
    let current = true
    mocks.confirm.mockImplementationOnce(async () => {
      current = false
      return 'confirm'
    })
    const operation = vi.fn().mockRejectedValueOnce(changedError())
    const { runWithSSHHostIdentityRecovery } = useSSHHostIdentityRecovery()

    const result = await runWithSSHHostIdentityRecovery(operation, {
      key: 'host-1',
      isCurrent: () => current,
    })

    assert.deepEqual(result, { status: 'stale' })
    assert.equal(mocks.refreshSSHHostIdentity.mock.calls.length, 0)
  })

  it('turns a refresh permission denial into an actionable message', async () => {
    mocks.refreshSSHHostIdentity.mockRejectedValueOnce(
      Object.assign(new Error('forbidden'), { statusCode: 403 }),
    )
    const operation = vi.fn().mockRejectedValueOnce(changedError())
    const { runWithSSHHostIdentityRecovery } = useSSHHostIdentityRecovery()

    await assert.rejects(
      () => runWithSSHHostIdentityRecovery(operation, { key: 'host-1' }),
      /请联系主机管理员/,
    )
    assert.equal(mocks.confirm.mock.calls.length, 1)
  })

  it('deduplicates concurrent recovery for the same target', async () => {
    let resolveConfirmation: (value: string) => void = () => undefined
    mocks.confirm.mockImplementationOnce(() => new Promise<string>((resolve) => {
      resolveConfirmation = resolve
    }))
    const operation = vi.fn()
      .mockRejectedValueOnce(changedError())
      .mockResolvedValueOnce({ ok: true })
    const { runWithSSHHostIdentityRecovery } = useSSHHostIdentityRecovery()

    const first = runWithSSHHostIdentityRecovery(operation, { key: 'host-1' })
    await vi.waitFor(() => assert.equal(mocks.confirm.mock.calls.length, 1))
    const second = await runWithSSHHostIdentityRecovery(operation, { key: 'host-1' })
    assert.deepEqual(second, { status: 'busy' })

    resolveConfirmation('confirm')
    assert.equal((await first).status, 'success')
    assert.equal(mocks.confirm.mock.calls.length, 1)
    assert.equal(mocks.refreshSSHHostIdentity.mock.calls.length, 1)
  })
})
