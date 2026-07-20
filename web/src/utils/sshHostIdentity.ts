export const SSH_HOST_KEY_CHANGED = 'SSH_HOST_KEY_CHANGED'
export const SSH_HOST_KEY_UNAVAILABLE = 'SSH_HOST_KEY_UNAVAILABLE'
export const SSH_HOST_IDENTITY_REFRESH_FAILED = 'SSH_HOST_IDENTITY_REFRESH_FAILED'

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
    }
  | {
      kind: 'refresh_failed'
      hostId: string
      hostStatus: string
      identityStatus: string
    }

export interface SSHHostIdentityNotice {
  title: string
  message: string
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
    return { kind: 'unavailable', hostId }
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

export function sshHostIdentityNotice(issue: SSHHostIdentityIssue): SSHHostIdentityNotice {
  if (issue.kind === 'refresh_failed') {
    const remainedDisabled = issue.hostStatus === '' || issue.hostStatus === 'disabled'
    return {
      title: 'SSH 主机身份更新失败',
      message: remainedDisabled
        ? [
            '系统未能从目标地址重新采集主机密钥，该主机仍保持停用。',
            '请确认主机地址、端口和网络连通性后再次启用。',
          ].join('\n')
        : [
            '系统未能从新地址重新采集主机密钥，本次修改没有生效。',
            '原主机配置和启用状态保持不变；请检查新地址、端口和网络连通性后重试。',
          ].join('\n'),
    }
  }
  if (issue.kind === 'unavailable') {
    return {
      title: 'SSH 主机身份尚未就绪',
      message: [
        '当前没有可用于安全校验的主机密钥记录，连接已被阻止。',
        '请在主机管理中重新启用该主机；系统会自动采集指纹和 known_hosts 记录，采集成功后才会启用。',
      ].join('\n'),
    }
  }

  const fingerprints = [
    issue.oldFingerprint ? `原指纹：${issue.oldFingerprint}` : '',
    issue.newFingerprint ? `新指纹：${issue.newFingerprint}` : '',
  ].filter(Boolean)
  const status = issue.hostDisabled
    ? '该主机已自动停用。确认目标主机身份后，可在主机管理中重新启用；重新启用时会采集并使用新密钥。'
    : '连接已被阻止，但自动停用未能确认。请联系管理员检查主机状态后再继续。'
  return {
    title: 'SSH 主机密钥发生变化',
    message: [
      '检测到目标主机密钥与已保存记录不一致。为防止连接到错误主机，本次连接已终止。',
      ...fingerprints,
      status,
    ].join('\n'),
  }
}

function stringValue(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number'
    ? String(value).trim()
    : ''
}
