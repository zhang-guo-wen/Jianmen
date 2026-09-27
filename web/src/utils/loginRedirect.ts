export interface LoginRedirect {
  target: string
  external: boolean
}

export function resolveLoginRedirect(value: unknown, currentURL = window.location.href): LoginRedirect {
  const fallback: LoginRedirect = { target: '/quick-connect', external: false }
  if (typeof value !== 'string' || !value.trim()) return fallback

  try {
    const current = new URL(currentURL)
    const target = new URL(value, current.origin)
    if (target.protocol !== 'http:' && target.protocol !== 'https:') return fallback
    if (target.hostname !== current.hostname) return fallback

    if (target.origin === current.origin) {
      return {
        target: `${target.pathname}${target.search}${target.hash}`,
        external: false,
      }
    }
    return { target: target.href, external: true }
  } catch {
    return fallback
  }
}
