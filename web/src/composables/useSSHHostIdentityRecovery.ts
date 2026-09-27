import { readonly, shallowRef } from 'vue'
import { ElMessageBox } from 'element-plus'

import { apiClient, type HostView } from '@/api/client'
import {
  isConfirmableSSHHostIdentityIssue,
  parseSSHHostIdentityIssue,
  SSH_HOST_IDENTITY_CONFIRM_MESSAGE,
  SSH_HOST_IDENTITY_CONFIRM_TITLE,
  type SSHHostIdentityIssue,
} from '@/utils/sshHostIdentity'

type ConfirmableSSHHostIdentityIssue = Extract<
  SSHHostIdentityIssue,
  { kind: 'changed' | 'unavailable' }
>

export type SSHHostIdentityRecoveryResult<T> =
  | {
      status: 'success'
      value: T
      recovered: boolean
    }
  | {
      status: 'cancelled'
      issue: ConfirmableSSHHostIdentityIssue
    }
  | {
      status: 'busy' | 'stale'
    }

interface UseSSHHostIdentityRecoveryOptions {
  onConfirmed?: (host: HostView) => void | Promise<void>
  onCancelledAfterDisable?: (
    issue: Extract<ConfirmableSSHHostIdentityIssue, { kind: 'changed' }>,
  ) => void | Promise<void>
}

interface SSHHostIdentityRecoveryRunOptions {
  key: string
  isCurrent?: () => boolean
}

const MAX_IDENTITY_CONFIRMATIONS = 3
const HOST_IDENTITY_PERMISSION_MESSAGE = '没有更新主机身份的权限，请联系主机管理员处理'

export function useSSHHostIdentityRecovery(
  options: UseSSHHostIdentityRecoveryOptions = {},
) {
  const recovering = shallowRef(false)
  const activeKeys = new Set<string>()
  const recoveryKeys = new Set<string>()

  async function runWithSSHHostIdentityRecovery<T>(
    operation: () => Promise<T>,
    runOptions: SSHHostIdentityRecoveryRunOptions,
  ): Promise<SSHHostIdentityRecoveryResult<T>> {
    const key = runOptions.key.trim()
    if (!key || activeKeys.has(key)) return { status: 'busy' }
    activeKeys.add(key)

    const isCurrent = () => runOptions.isCurrent?.() ?? true
    let issue: ConfirmableSSHHostIdentityIssue
    try {
      try {
        const value = await operation()
        return isCurrent()
          ? { status: 'success', value, recovered: false }
          : { status: 'stale' }
      } catch (error) {
        const parsedIssue = parseSSHHostIdentityIssue(error)
        if (!isConfirmableSSHHostIdentityIssue(parsedIssue)) throw error
        if (!isCurrent()) return { status: 'stale' }
        issue = parsedIssue
      }

      recoveryKeys.add(key)
      recovering.value = true
      for (let attempt = 0; attempt < MAX_IDENTITY_CONFIRMATIONS; attempt += 1) {
        if (!isCurrent()) return { status: 'stale' }
        const confirmed = await ElMessageBox.confirm(
          SSH_HOST_IDENTITY_CONFIRM_MESSAGE,
          SSH_HOST_IDENTITY_CONFIRM_TITLE,
          {
            type: 'warning',
            confirmButtonText: '继续连接',
            cancelButtonText: '取消',
            distinguishCancelAndClose: true,
          },
        ).then(
          () => true,
          () => false,
        )
        if (!confirmed) {
          if (issue.kind === 'changed' && issue.hostDisabled && isCurrent()) {
            try {
              await options.onCancelledAfterDisable?.(issue)
            } catch {
              // The cancellation remains effective even if the list refresh fails.
            }
          }
          return isCurrent()
            ? { status: 'cancelled', issue }
            : { status: 'stale' }
        }
        if (!isCurrent()) return { status: 'stale' }

        let host: HostView
        try {
          host = await apiClient.refreshSSHHostIdentity(
            issue.hostId,
            issue.newFingerprint,
          )
        } catch (error) {
          if (apiStatusCode(error) === 403) {
            throw new Error(HOST_IDENTITY_PERMISSION_MESSAGE)
          }
          const nextIssue = parseSSHHostIdentityIssue(error)
          if (
            attempt + 1 < MAX_IDENTITY_CONFIRMATIONS
            && isConfirmableSSHHostIdentityIssue(nextIssue)
          ) {
            issue = nextIssue
            continue
          }
          throw error
        }
        if (!isCurrent()) return { status: 'stale' }
        try {
          await options.onConfirmed?.(host)
        } catch {
          // A list refresh failure must not undo a confirmed identity update.
        }
        if (!isCurrent()) return { status: 'stale' }

        try {
          const value = await operation()
          return isCurrent()
            ? { status: 'success', value, recovered: true }
            : { status: 'stale' }
        } catch (error) {
          const nextIssue = parseSSHHostIdentityIssue(error)
          if (
            attempt + 1 < MAX_IDENTITY_CONFIRMATIONS
            && isConfirmableSSHHostIdentityIssue(nextIssue)
          ) {
            issue = nextIssue
            continue
          }
          throw error
        }
      }
      throw new Error('SSH 主机身份连续发生变化，请确认主机和网络状态后重试')
    } finally {
      activeKeys.delete(key)
      recoveryKeys.delete(key)
      recovering.value = recoveryKeys.size > 0
    }
  }

  return {
    recovering: readonly(recovering),
    runWithSSHHostIdentityRecovery,
  }
}

function apiStatusCode(error: unknown): number {
  if (!error || typeof error !== 'object') return 0
  const value = Reflect.get(error, 'statusCode')
  return typeof value === 'number' ? value : 0
}
