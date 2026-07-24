export const SSH_HOST_KEY_CHANGED = 'SSH_HOST_KEY_CHANGED'
export const SSH_HOST_KEY_UNAVAILABLE = 'SSH_HOST_KEY_UNAVAILABLE'
export const SSH_HOST_IDENTITY_REFRESH_FAILED = 'SSH_HOST_IDENTITY_REFRESH_FAILED'

export const SSH_HOST_IDENTITY_CONFIRM_TITLE = '连接确认'
export const SSH_HOST_IDENTITY_CONFIRM_MESSAGE = '主机身份信息发生变化，请确认主机是否正常，是否继续连接'

export type SSHHostIdentityIssue =
  | {
      kind: 'changed'
      hostId: string
      oldFingerprint: string
      newFingerprint: string
      hostDisabled: boolean
    }
  | {
      kind: 'unavailable'
      hostId: string
      newFingerprint: string
    }
  | {
      kind: 'refresh_failed'
      hostId: string
      hostStatus: string
      identityStatus: string
    }

export function parseSSHHostIdentityIssue(error: unknown): SSHHostIdentityIssue | null {
  if (!error || typeof error !== 'object') return null
  const code = stringValue(Reflect.get(error, 'code'))
  const rawDetails = Reflect.get(error, 'details')
  const details = rawDetails && typeof rawDetails === 'object' ? rawDetails : {}
  const hostId = stringValue(Reflect.get(details, 'host_id'))

  if (code === SSH_HOST_KEY_CHANGED) {
    return {
      kind: 'changed',
      hostId,
      oldFingerprint: stringValue(Reflect.get(details, 'old_fingerprint')),
      newFingerprint: stringValue(Reflect.get(details, 'new_fingerprint')),
      hostDisabled: Reflect.get(details, 'host_disabled') === true,
    }
  }
  if (code === SSH_HOST_KEY_UNAVAILABLE) {
    return {
      kind: 'unavailable',
      hostId,
      newFingerprint: stringValue(Reflect.get(details, 'new_fingerprint')),
    }
  }
  if (code === SSH_HOST_IDENTITY_REFRESH_FAILED) {
    return {
      kind: 'refresh_failed',
      hostId,
      hostStatus: stringValue(Reflect.get(details, 'host_status')),
      identityStatus: stringValue(Reflect.get(details, 'identity_status')),
    }
  }
  return null
}

export function isConfirmableSSHHostIdentityIssue(
  issue: SSHHostIdentityIssue | null,
): issue is Extract<SSHHostIdentityIssue, { kind: 'changed' | 'unavailable' }> {
  if (!issue || (issue.kind !== 'changed' && issue.kind !== 'unavailable')) {
    return false
  }
  return Boolean(issue.hostId && issue.newFingerprint)
}

function stringValue(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number'
    ? String(value).trim()
    : ''
}
